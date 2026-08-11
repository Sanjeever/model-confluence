package gateway

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"github.com/Sanjeever/model-confluence/internal/config"
	"github.com/Sanjeever/model-confluence/internal/httpx"
	"github.com/Sanjeever/model-confluence/internal/protocol"
	"github.com/Sanjeever/model-confluence/internal/store"
)

type Handler struct {
	store    *store.Store
	clientIP *httpx.ClientIPResolver
	maxBody  int64
	client   *http.Client
}

func NewHandler(cfg config.Config, database *store.Store, clientIP *httpx.ClientIPResolver) *Handler {
	dialer := &net.Dialer{Timeout: cfg.ConnectTimeout, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment, DialContext: dialer.DialContext, ForceAttemptHTTP2: true,
		ResponseHeaderTimeout: cfg.ResponseHeaderTimeout, IdleConnTimeout: 90 * time.Second,
	}
	return &Handler{store: database, clientIP: clientIP, maxBody: cfg.MaxRequestBytes, client: &http.Client{Transport: transport}}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/models", h.models)
	mux.HandleFunc("POST /v1/chat/completions", h.generate)
	mux.HandleFunc("POST /v1/responses", h.generate)
	mux.HandleFunc("POST /v1/messages", h.generate)
}

func (h *Handler) models(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.authenticate(w, r, protocolFromPath(r.URL.Path)); !ok {
		return
	}
	names, err := h.store.EnabledModelNames()
	if err != nil {
		h.writeError(w, protocolChat, http.StatusInternalServerError, "gateway_error", err.Error(), "")
		return
	}
	data := make([]map[string]any, 0, len(names))
	for _, name := range names {
		data = append(data, map[string]any{"id": name, "object": "model", "owned_by": "model-confluence"})
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"object": "list", "data": data})
}

func (h *Handler) generate(w http.ResponseWriter, r *http.Request) {
	inboundProtocol := protocolFromPath(r.URL.Path)
	accessKey, ok := h.authenticate(w, r, inboundProtocol)
	if !ok {
		return
	}
	h.generateAuthorized(w, r, inboundProtocol, &accessKey.ID, accessKey.Name)
}

func (h *Handler) TestModel(ctx context.Context, model, prompt string) (int, []byte, string) {
	body, _ := json.Marshal(map[string]any{
		"model":      model,
		"messages":   []map[string]string{{"role": "user", "content": prompt}},
		"max_tokens": 256,
		"stream":     false,
	})
	request := httptest.NewRequestWithContext(ctx, http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "model-confluence-admin-test")
	recorder := httptest.NewRecorder()
	h.generateAuthorized(recorder, request, protocolChat, nil, "管理后台测试")
	return recorder.Code, recorder.Body.Bytes(), recorder.Header().Get("X-Request-ID")
}

func (h *Handler) generateAuthorized(w http.ResponseWriter, r *http.Request, inboundProtocol string, accessKeyID *int64, accessKeyName string) {
	started := time.Now()
	body, err := readBody(w, r, h.maxBody)
	if err != nil {
		return
	}
	requestMeta, err := inspectRequest(body, inboundProtocol)
	requestID, tokenErr := store.NewToken(18)
	if tokenErr != nil {
		h.writeError(w, inboundProtocol, http.StatusInternalServerError, "gateway_error", tokenErr.Error(), "")
		return
	}
	requestID = "req_" + requestID
	w.Header().Set("X-Request-ID", requestID)

	headersJSON := marshalHeaders(r.Header)
	if startErr := h.store.StartRequest(store.RequestStart{
		ID: requestID, AccessKeyID: accessKeyID, AccessKeyName: accessKeyName, VirtualModel: requestMeta.Model,
		InboundProtocol: inboundProtocol, InboundEndpoint: r.URL.Path, Stream: requestMeta.Stream, ReasoningEffort: requestMeta.Effort,
		ClientIP: h.clientIP.Resolve(r), UserAgent: r.UserAgent(), Headers: headersJSON, Body: body, CreatedAt: started.UTC(),
	}); startErr != nil {
		h.writeError(w, inboundProtocol, http.StatusInternalServerError, "logging_error", startErr.Error(), requestID)
		return
	}
	if err != nil {
		h.finishError(w, inboundProtocol, requestID, started, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	routes, err := h.store.ResolveRoutes(store.RoutingRequirements{
		VirtualModel: requestMeta.Model, Protocol: inboundProtocol, Stream: requestMeta.Stream,
		Tools: requestMeta.Tools, ParallelTools: requestMeta.ParallelTools, Effort: requestMeta.Effort,
	})
	if err != nil {
		status := http.StatusInternalServerError
		code := "gateway_error"
		if errors.Is(err, store.ErrNoRoute) || errors.Is(err, sql.ErrNoRows) {
			status, code = http.StatusServiceUnavailable, "no_eligible_route"
		}
		h.finishError(w, inboundProtocol, requestID, started, status, code, err.Error())
		return
	}

	position := 0
	for index := 0; index < len(routes); index++ {
		route := routes[index]
		var upstreamBody []byte
		if route.UpstreamProtocol == inboundProtocol {
			upstreamBody, err = rewriteModel(body, route.UpstreamModel)
		} else {
			upstreamBody, err = protocol.ConvertRequest(body, inboundProtocol, route.UpstreamProtocol, route.UpstreamModel, routeDefaultMax(route))
		}
		if err != nil {
			h.finishError(w, inboundProtocol, requestID, started, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
		if requestMeta.Stream && route.UpstreamProtocol == protocol.Chat && route.ProtocolConfig.SupportsStreamUsage {
			upstreamBody, err = includeStreamUsage(upstreamBody)
			if err != nil {
				h.finishError(w, inboundProtocol, requestID, started, http.StatusBadRequest, "invalid_request", err.Error())
				return
			}
		}
		outbound, err := http.NewRequestWithContext(r.Context(), http.MethodPost, route.UpstreamEndpoint, bytes.NewReader(upstreamBody))
		if err != nil {
			h.finishError(w, inboundProtocol, requestID, started, http.StatusInternalServerError, "gateway_error", err.Error())
			return
		}
		copyRequestHeaders(outbound.Header, r.Header)
		if route.UpstreamProtocol != inboundProtocol {
			stripSourceProtocolHeaders(outbound.Header)
			if route.UpstreamProtocol == protocol.Messages && outbound.Header.Get("anthropic-version") == "" {
				outbound.Header.Set("anthropic-version", "2023-06-01")
			}
		}
		applyProviderHeaders(outbound.Header, route.Provider, route.Key.Secret)
		attemptStarted := time.Now()
		attemptID, err := h.store.StartAttempt(store.AttemptStart{
			RequestID: requestID, Position: position, ProviderID: route.Provider.ID, ProviderName: route.Provider.Name,
			UpstreamKeyID: route.Key.ID, UpstreamKeyName: route.Key.Name, CandidateID: route.CandidateID,
			UpstreamModel: route.UpstreamModel, Protocol: route.UpstreamProtocol, Endpoint: route.UpstreamEndpoint,
			Headers: marshalHeaders(outbound.Header), Body: upstreamBody, CreatedAt: attemptStarted.UTC(),
		})
		if err != nil {
			h.finishError(w, inboundProtocol, requestID, started, http.StatusInternalServerError, "logging_error", err.Error())
			return
		}
		position++
		response, err := h.client.Do(outbound)
		if err != nil {
			h.completeAttemptFailure(attemptID, attemptStarted, err)
			index = skipCandidate(routes, index, route.CandidateID)
			continue
		}
		firstByte := time.Since(attemptStarted).Milliseconds()
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			responseBody, readErr := io.ReadAll(response.Body)
			response.Body.Close()
			if readErr != nil {
				h.completeAttemptFailure(attemptID, attemptStarted, readErr)
				continue
			}
			h.store.CompleteAttempt(store.AttemptResult{ID: attemptID, Status: "failed", ResponseStatus: response.StatusCode, ResponseHeaders: marshalHeaders(response.Header), ResponseBody: responseBody, FirstByteMS: &firstByte, TotalMS: time.Since(attemptStarted).Milliseconds(), ErrorMessage: string(responseBody), CompletedAt: time.Now().UTC()})
			if shouldTryNextKey(response.StatusCode) {
				h.updateKeyAfterError(route, response.StatusCode, responseBody, response.Header)
				continue
			}
			if response.StatusCode >= 500 {
				index = skipCandidate(routes, index, route.CandidateID)
				continue
			}
			h.finishError(w, inboundProtocol, requestID, started, response.StatusCode, "upstream_error", string(responseBody))
			return
		}

		_ = h.store.TouchUpstreamKey(route.Key.ID)
		if requestMeta.Stream {
			h.proxyStream(w, r.Context(), response, route.UpstreamProtocol, inboundProtocol, route.VirtualModel, requestID, attemptID, started, attemptStarted, firstByte)
			return
		}
		h.proxyBuffered(w, response, route.UpstreamProtocol, inboundProtocol, route.VirtualModel, requestID, attemptID, started, attemptStarted, firstByte)
		return
	}
	h.finishError(w, inboundProtocol, requestID, started, http.StatusServiceUnavailable, "upstream_unavailable", "所有可用密钥或候选均调用失败")
}

func (h *Handler) proxyBuffered(w http.ResponseWriter, response *http.Response, upstreamProtocol, inboundProtocol, virtualModel, requestID string, attemptID int64, started, attemptStarted time.Time, firstByte int64) {
	defer response.Body.Close()
	rawBody, err := io.ReadAll(response.Body)
	if err != nil {
		h.completeAttemptFailure(attemptID, attemptStarted, err)
		h.finishError(w, inboundProtocol, requestID, started, http.StatusBadGateway, "upstream_read_error", err.Error())
		return
	}
	var clientBody []byte
	if upstreamProtocol == inboundProtocol {
		clientBody, err = rewriteModel(rawBody, virtualModel)
	} else {
		clientBody, err = protocol.ConvertResponse(rawBody, upstreamProtocol, inboundProtocol, virtualModel)
	}
	if err != nil {
		h.completeAttemptFailure(attemptID, attemptStarted, err)
		h.finishError(w, inboundProtocol, requestID, started, http.StatusBadGateway, "invalid_upstream_response", err.Error())
		return
	}
	usage, rawUsage, usageErr := protocol.ExtractUsage(rawBody, upstreamProtocol, false)
	copyResponseHeaders(w.Header(), response.Header)
	w.Header().Del("Content-Length")
	w.WriteHeader(response.StatusCode)
	_, writeErr := w.Write(clientBody)
	status := "completed"
	errorMessage := ""
	if writeErr != nil {
		status, errorMessage = "cancelled", writeErr.Error()
	} else if usageErr != nil {
		errorMessage = "解析上游用量失败：" + usageErr.Error()
	}
	total := time.Since(started).Milliseconds()
	h.store.CompleteAttempt(store.AttemptResult{ID: attemptID, Status: status, ResponseStatus: response.StatusCode, ResponseHeaders: marshalHeaders(response.Header), ResponseBody: rawBody, RawUsageJSON: rawUsage, FirstByteMS: &firstByte, TotalMS: time.Since(attemptStarted).Milliseconds(), ErrorMessage: errorMessage, CompletedAt: time.Now().UTC()})
	h.store.CompleteRequest(requestResult(requestID, status, response.StatusCode, marshalHeaders(w.Header()), clientBody, nil, total, errorMessage, usage))
}

func (h *Handler) proxyStream(w http.ResponseWriter, ctx context.Context, response *http.Response, upstreamProtocol, inboundProtocol, virtualModel, requestID string, attemptID int64, started, attemptStarted time.Time, firstByte int64) {
	defer response.Body.Close()
	copyResponseHeaders(w.Header(), response.Header)
	w.Header().Del("Content-Length")
	if upstreamProtocol != inboundProtocol {
		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	}
	w.Header().Set("X-Accel-Buffering", "no")
	flusher, _ := w.(http.Flusher)
	reader := bufio.NewReader(response.Body)
	var upstreamLog, clientLog bytes.Buffer
	var firstContent *int64
	status := "completed"
	errorMessage := ""
	clientResponseStatus := response.StatusCode
	if upstreamProtocol == inboundProtocol {
		w.WriteHeader(response.StatusCode)
		status, errorMessage = proxyPassthroughEvents(w, ctx, reader, flusher, virtualModel, started, &upstreamLog, &clientLog, &firstContent)
	} else {
		converter, err := protocol.NewStreamConverter(upstreamProtocol, inboundProtocol, virtualModel)
		committed := false
		if err != nil {
			status, errorMessage = "failed", err.Error()
		} else {
			status, errorMessage, committed = proxyConvertedEvents(w, ctx, reader, flusher, converter, response.StatusCode, started, &upstreamLog, &clientLog, &firstContent)
		}
		if status == "failed" && !committed {
			clientResponseStatus = http.StatusBadGateway
			clientLog.Write(h.writeError(w, inboundProtocol, clientResponseStatus, "invalid_upstream_response", errorMessage, requestID))
		}
	}
	usage, rawUsage, usageErr := protocol.ExtractUsage(upstreamLog.Bytes(), upstreamProtocol, true)
	if usageErr != nil && errorMessage == "" {
		errorMessage = "解析上游用量失败：" + usageErr.Error()
	}
	total := time.Since(started).Milliseconds()
	h.store.CompleteAttempt(store.AttemptResult{ID: attemptID, Status: status, ResponseStatus: response.StatusCode, ResponseHeaders: marshalHeaders(response.Header), ResponseBody: upstreamLog.Bytes(), RawUsageJSON: rawUsage, FirstByteMS: &firstByte, FirstContentMS: firstContent, TotalMS: time.Since(attemptStarted).Milliseconds(), ErrorMessage: errorMessage, CompletedAt: time.Now().UTC()})
	h.store.CompleteRequest(requestResult(requestID, status, clientResponseStatus, marshalHeaders(w.Header()), clientLog.Bytes(), firstContent, total, errorMessage, usage))
}

func requestResult(id, status string, responseStatus int, headers string, body []byte, firstContent *int64, total int64, errorMessage string, usage protocol.Usage) store.RequestResult {
	return store.RequestResult{
		ID: id, Status: status, ResponseStatus: responseStatus, ResponseHeaders: headers, ResponseBody: body,
		InputTokens: int64Pointer(usage.InputTokens), CacheReadTokens: int64Pointer(usage.CacheReadTokens), CacheWriteTokens: int64Pointer(usage.CacheWriteTokens),
		OutputTokens: int64Pointer(usage.OutputTokens), ReasoningTokens: int64Pointer(usage.ReasoningTokens), TotalTokens: int64Pointer(usage.TotalTokens),
		FirstContentMS: firstContent, TotalMS: total, ErrorMessage: errorMessage, CompletedAt: time.Now().UTC(),
	}
}

func int64Pointer(value *int) *int64 {
	if value == nil {
		return nil
	}
	converted := int64(*value)
	return &converted
}

func proxyPassthroughEvents(w http.ResponseWriter, ctx context.Context, reader *bufio.Reader, flusher http.Flusher, virtualModel string, started time.Time, upstreamLog, clientLog *bytes.Buffer, firstContent **int64) (string, string) {
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			upstreamLog.Write(line)
			clientLine := rewriteSSEModel(line, virtualModel)
			clientLog.Write(clientLine)
			if *firstContent == nil && semanticSSELine(clientLine) {
				value := time.Since(started).Milliseconds()
				*firstContent = &value
			}
			if _, writeErr := w.Write(clientLine); writeErr != nil {
				return "cancelled", writeErr.Error()
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		if err != nil {
			if !errors.Is(err, io.EOF) {
				return "failed", err.Error()
			}
			return "completed", ""
		}
		select {
		case <-ctx.Done():
			return "cancelled", ctx.Err().Error()
		default:
		}
	}
}

func proxyConvertedEvents(w http.ResponseWriter, ctx context.Context, reader *bufio.Reader, flusher http.Flusher, converter *protocol.StreamConverter, responseStatus int, started time.Time, upstreamLog, clientLog *bytes.Buffer, firstContent **int64) (string, string, bool) {
	committed := false
	for {
		event, err := protocol.ReadSSEEvent(reader)
		if len(event.Raw) > 0 {
			upstreamLog.Write(event.Raw)
			chunks, semantic, done, convertErr := converter.Convert(event)
			if convertErr != nil {
				return "failed", convertErr.Error(), committed
			}
			if *firstContent == nil && semantic {
				value := time.Since(started).Milliseconds()
				*firstContent = &value
			}
			for _, chunk := range chunks {
				if !committed {
					w.WriteHeader(responseStatus)
					committed = true
				}
				clientLog.Write(chunk)
				if _, writeErr := w.Write(chunk); writeErr != nil {
					return "cancelled", writeErr.Error(), committed
				}
				if flusher != nil {
					flusher.Flush()
				}
			}
			if done {
				return "completed", "", committed
			}
		}
		if err != nil {
			if !errors.Is(err, io.EOF) {
				return "failed", err.Error(), committed
			}
			return "failed", "upstream stream ended before completion", committed
		}
		select {
		case <-ctx.Done():
			return "cancelled", ctx.Err().Error(), committed
		default:
		}
	}
}

func (h *Handler) authenticate(w http.ResponseWriter, r *http.Request, protocol string) (store.AccessKey, bool) {
	var bearer string
	if authorization := r.Header.Get("Authorization"); strings.HasPrefix(authorization, "Bearer ") {
		bearer = strings.TrimSpace(strings.TrimPrefix(authorization, "Bearer "))
	}
	apiKey := strings.TrimSpace(r.Header.Get("x-api-key"))
	if bearer != "" && apiKey != "" && bearer != apiKey {
		h.store.CreateSecurityEvent("auth_failed", h.clientIP.Resolve(r), r.UserAgent(), r.URL.Path, "conflicting authentication headers")
		h.writeError(w, protocol, http.StatusUnauthorized, "invalid_api_key", "Authorization 与 x-api-key 不一致", "")
		return store.AccessKey{}, false
	}
	secret := bearer
	if secret == "" {
		secret = apiKey
	}
	if secret == "" {
		h.store.CreateSecurityEvent("auth_failed", h.clientIP.Resolve(r), r.UserAgent(), r.URL.Path, "missing API key")
		h.writeError(w, protocol, http.StatusUnauthorized, "invalid_api_key", "缺少访问密钥", "")
		return store.AccessKey{}, false
	}
	key, err := h.store.AuthenticateAccessKey(secret)
	if err != nil {
		h.store.CreateSecurityEvent("auth_failed", h.clientIP.Resolve(r), r.UserAgent(), r.URL.Path, err.Error())
		h.writeError(w, protocol, http.StatusUnauthorized, "invalid_api_key", "访问密钥无效", "")
		return store.AccessKey{}, false
	}
	return key, true
}

func (h *Handler) finishError(w http.ResponseWriter, protocol, requestID string, started time.Time, status int, code, message string) {
	body := h.writeError(w, protocol, status, code, message, requestID)
	h.store.CompleteRequest(store.RequestResult{ID: requestID, Status: "failed", ResponseStatus: status, ResponseHeaders: marshalHeaders(w.Header()), ResponseBody: body, TotalMS: time.Since(started).Milliseconds(), ErrorMessage: message, CompletedAt: time.Now().UTC()})
}

func (h *Handler) writeError(w http.ResponseWriter, protocol string, status int, code, message, requestID string) []byte {
	var payload any
	if protocol == protocolMessages {
		payload = map[string]any{"type": "error", "error": map[string]string{"type": code, "message": message}, "request_id": requestID}
	} else {
		payload = map[string]any{"error": map[string]any{"message": message, "type": code, "code": code}, "request_id": requestID}
	}
	body, _ := json.Marshal(payload)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(append(body, '\n'))
	return append(body, '\n')
}

func (h *Handler) completeAttemptFailure(id int64, started time.Time, err error) {
	h.store.CompleteAttempt(store.AttemptResult{ID: id, Status: "failed", TotalMS: time.Since(started).Milliseconds(), ErrorMessage: err.Error(), CompletedAt: time.Now().UTC()})
}

func (h *Handler) updateKeyAfterError(route store.ResolvedRoute, status int, body []byte, headers http.Header) {
	if status == http.StatusUnauthorized {
		h.store.MarkUpstreamKey(route.Key.ID, "auth_invalid", string(body), nil)
		return
	}
	if status != http.StatusTooManyRequests {
		return
	}
	code := extractErrorCode(body)
	if contains(route.Provider.QuotaCodes, code) {
		h.store.MarkUpstreamKey(route.Key.ID, "quota_exhausted", code, nil)
		return
	}
	recoverAt := time.Now().Add(60 * time.Second)
	if retryAfter, err := time.ParseDuration(headers.Get("Retry-After") + "s"); err == nil {
		recoverAt = time.Now().Add(retryAfter)
	}
	h.store.MarkUpstreamKey(route.Key.ID, "rate_limited", code, &recoverAt)
}

type requestMetadata struct {
	Model         string
	Stream        bool
	Tools         bool
	ParallelTools bool
	Effort        string
}

func inspectRequest(body []byte, protocol string) (requestMetadata, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return requestMetadata{}, err
	}
	var result requestMetadata
	if err := json.Unmarshal(raw["model"], &result.Model); err != nil || result.Model == "" {
		return requestMetadata{}, errors.New("model 不能为空")
	}
	_ = json.Unmarshal(raw["stream"], &result.Stream)
	var tools []json.RawMessage
	_ = json.Unmarshal(raw["tools"], &tools)
	result.Tools = len(tools) > 0
	_ = json.Unmarshal(raw["parallel_tool_calls"], &result.ParallelTools)
	if protocol == protocolResponses {
		var reasoning struct {
			Effort string `json:"effort"`
		}
		_ = json.Unmarshal(raw["reasoning"], &reasoning)
		result.Effort = reasoning.Effort
	} else if protocol == protocolMessages {
		var outputConfig struct {
			Effort string `json:"effort"`
		}
		_ = json.Unmarshal(raw["output_config"], &outputConfig)
		result.Effort = outputConfig.Effort
	} else {
		_ = json.Unmarshal(raw["reasoning_effort"], &result.Effort)
	}
	return result, nil
}

func rewriteModel(body []byte, model string) ([]byte, error) {
	var value map[string]json.RawMessage
	if err := json.Unmarshal(body, &value); err != nil {
		return nil, err
	}
	encoded, _ := json.Marshal(model)
	value["model"] = encoded
	return json.Marshal(value)
}

func includeStreamUsage(body []byte) ([]byte, error) {
	var value map[string]json.RawMessage
	if err := json.Unmarshal(body, &value); err != nil {
		return nil, err
	}
	var options map[string]any
	if len(value["stream_options"]) > 0 {
		if err := json.Unmarshal(value["stream_options"], &options); err != nil {
			return nil, err
		}
	}
	if options == nil {
		options = make(map[string]any)
	}
	options["include_usage"] = true
	encoded, err := json.Marshal(options)
	if err != nil {
		return nil, err
	}
	value["stream_options"] = encoded
	return json.Marshal(value)
}

func rewriteSSEModel(line []byte, model string) []byte {
	prefix := []byte("data:")
	trimmed := bytes.TrimSpace(line)
	if !bytes.HasPrefix(trimmed, prefix) {
		return line
	}
	data := bytes.TrimSpace(bytes.TrimPrefix(trimmed, prefix))
	if bytes.Equal(data, []byte("[DONE]")) {
		return line
	}
	rewritten, err := rewriteModel(data, model)
	if err != nil {
		return line
	}
	return append(append([]byte("data: "), rewritten...), '\n')
}

func semanticSSELine(line []byte) bool {
	value := string(line)
	return (strings.Contains(value, `"content"`) && !strings.Contains(value, `"content":""`)) || strings.Contains(value, `"tool_calls"`) || strings.Contains(value, `"function_call"`) || strings.Contains(value, `"content_block_delta"`) || strings.Contains(value, `"output_text.delta"`)
}

func applyProviderHeaders(header http.Header, provider store.Provider, secret string) {
	header.Del("Authorization")
	header.Del("x-api-key")
	switch provider.AuthType {
	case "bearer":
		header.Set("Authorization", "Bearer "+secret)
	case "x-api-key":
		header.Set("x-api-key", secret)
	case "custom":
		header.Set(provider.AuthHeader, secret)
	}
	for key, value := range provider.StaticHeaders {
		header.Set(key, value)
	}
}

func copyRequestHeaders(destination, source http.Header) {
	for key, values := range source {
		if hopByHop(key) {
			continue
		}
		for _, value := range values {
			destination.Add(key, value)
		}
	}
	destination.Del("Content-Length")
	destination.Del("Accept-Encoding")
}

func stripSourceProtocolHeaders(header http.Header) {
	header.Del("anthropic-version")
	header.Del("anthropic-beta")
	header.Del("openai-beta")
	header.Del("openai-organization")
	header.Set("Content-Type", "application/json")
}

func copyResponseHeaders(destination, source http.Header) {
	for key, values := range source {
		if hopByHop(key) {
			continue
		}
		for _, value := range values {
			destination.Add(key, value)
		}
	}
}

func hopByHop(name string) bool {
	switch strings.ToLower(name) {
	case "connection", "keep-alive", "proxy-authenticate", "proxy-authorization", "te", "trailer", "transfer-encoding", "upgrade":
		return true
	default:
		return false
	}
}

func marshalHeaders(header http.Header) string {
	value, _ := json.Marshal(header)
	return string(value)
}

func extractErrorCode(body []byte) string {
	var payload struct {
		Error struct {
			Code string `json:"code"`
			Type string `json:"type"`
		} `json:"error"`
		Code string `json:"code"`
		Type string `json:"type"`
	}
	if json.Unmarshal(body, &payload) != nil {
		return ""
	}
	for _, value := range []string{payload.Error.Code, payload.Error.Type, payload.Code, payload.Type} {
		if value != "" && value != "error" {
			return value
		}
	}
	return ""
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func shouldTryNextKey(status int) bool {
	return status == http.StatusUnauthorized || status == http.StatusForbidden || status == http.StatusTooManyRequests
}

func skipCandidate(routes []store.ResolvedRoute, index int, candidateID int64) int {
	for index+1 < len(routes) && routes[index+1].CandidateID == candidateID {
		index++
	}
	return index
}

func readBody(w http.ResponseWriter, r *http.Request, limit int64) ([]byte, error) {
	reader := http.MaxBytesReader(w, r.Body, limit)
	body, err := io.ReadAll(reader)
	if err != nil {
		http.Error(w, "request body exceeds configured limit", http.StatusRequestEntityTooLarge)
		return nil, err
	}
	return body, nil
}

const (
	protocolChat      = "chat_completions"
	protocolResponses = "responses"
	protocolMessages  = "messages"
)

func protocolFromPath(path string) string {
	if strings.HasSuffix(path, "/responses") {
		return protocolResponses
	}
	if strings.HasSuffix(path, "/messages") {
		return protocolMessages
	}
	return protocolChat
}

func routeDefaultMax(route store.ResolvedRoute) int {
	return route.DefaultMaxOutputTokens
}

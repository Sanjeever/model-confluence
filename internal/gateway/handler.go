package gateway

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Sanjeever/model-confluence/internal/config"
	"github.com/Sanjeever/model-confluence/internal/httpx"
	"github.com/Sanjeever/model-confluence/internal/protocol"
	"github.com/Sanjeever/model-confluence/internal/store"
)

type Handler struct {
	store             *store.Store
	clientIP          *httpx.ClientIPResolver
	maxBody           int64
	streamIdleTimeout time.Duration
	client            *http.Client
	candidateMu       sync.Mutex
	candidates        map[int64]candidateFailure
}

type candidateFailure struct {
	count         int
	cooldownUntil time.Time
	probing       bool
}

func NewHandler(cfg config.Config, database *store.Store, clientIP *httpx.ClientIPResolver) *Handler {
	dialer := &net.Dialer{Timeout: cfg.ConnectTimeout, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment, DialContext: dialer.DialContext, ForceAttemptHTTP2: true,
		ResponseHeaderTimeout: cfg.ResponseHeaderTimeout, IdleConnTimeout: 90 * time.Second,
	}
	return &Handler{store: database, clientIP: clientIP, maxBody: cfg.MaxRequestBytes, streamIdleTimeout: cfg.StreamIdleTimeout, client: &http.Client{Transport: transport}, candidates: make(map[int64]candidateFailure)}
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

var (
	// ErrProviderNoEndpoint 表示供应商没有配置任何协议端点，无法推导模型列表地址。
	ErrProviderNoEndpoint = errors.New("供应商尚未配置协议端点")
	// ErrProviderNoKey 表示供应商没有可用的上游密钥。
	ErrProviderNoKey = errors.New("供应商没有可用的上游密钥")

	errUpstreamUnauthorized = errors.New("上游密钥未通过鉴权")
)

const maxModelsBodyBytes = 8 << 20

var modelsEndpointSuffixes = []string{"/chat/completions", "/responses", "/messages"}

// ListProviderModels 从供应商的模型列表端点拉取真实模型名，供管理后台选择。
// 端点地址由生成端点 URL 去掉 /chat/completions、/responses 或 /messages 后缀再拼接 /models 推导而来。
func (h *Handler) ListProviderModels(ctx context.Context, providerID int64) ([]string, error) {
	provider, err := h.store.ProviderByID(providerID)
	if err != nil {
		return nil, err
	}
	endpoint, fromMessages := providerModelsEndpoint(provider)
	if endpoint == "" {
		return nil, ErrProviderNoEndpoint
	}
	var lastErr error
	for _, key := range provider.Keys {
		if !store.KeyAvailable(key) {
			continue
		}
		models, err := h.fetchProviderModels(ctx, endpoint, fromMessages, provider, key)
		if err == nil {
			return models, nil
		}
		if !errors.Is(err, errUpstreamUnauthorized) {
			return nil, err
		}
		lastErr = err
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, ErrProviderNoKey
}

func providerModelsEndpoint(provider store.Provider) (endpoint string, fromMessages bool) {
	for _, name := range []string{protocolChat, protocolResponses, protocolMessages} {
		endpoint := provider.Endpoints[name]
		if endpoint == "" {
			continue
		}
		for _, suffix := range modelsEndpointSuffixes {
			endpoint = strings.TrimSuffix(endpoint, suffix)
		}
		return endpoint + "/models", name == protocolMessages
	}
	return "", false
}

func (h *Handler) fetchProviderModels(ctx context.Context, endpoint string, fromMessages bool, provider store.Provider, key store.UpstreamKey) ([]string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	if fromMessages {
		// Anthropic 风格端点默认每页 20 条，需要显式放大分页。
		query := request.URL.Query()
		query.Set("limit", "1000")
		request.URL.RawQuery = query.Encode()
	}
	applyProviderHeaders(request.Header, provider, key.Secret)
	response, err := h.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("请求上游失败: %w", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxModelsBodyBytes))
	if err != nil {
		return nil, fmt.Errorf("读取上游响应失败: %w", err)
	}
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("%w（HTTP %d）", errUpstreamUnauthorized, response.StatusCode)
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("上游返回 HTTP %d: %s", response.StatusCode, errorBodySnippet(body))
	}
	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("模型列表响应不是有效 JSON: %w", err)
	}
	models := make([]string, 0, len(payload.Data))
	for _, item := range payload.Data {
		if item.ID != "" {
			models = append(models, item.ID)
		}
	}
	sort.Strings(models)
	return models, nil
}

func errorBodySnippet(body []byte) string {
	snippet := strings.TrimSpace(string(body))
	if len(snippet) > 200 {
		snippet = snippet[:200] + "…"
	}
	return snippet
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
	retryIndex := 0
	var invalidResponseErr error
	for index := 0; index < len(routes); index++ {
		route := routes[index]
		if (index == 0 || routes[index-1].CandidateID != route.CandidateID) && !h.candidateReady(route.CandidateID) {
			index = skipCandidate(routes, index, route.CandidateID)
			continue
		}
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
			if r.Context().Err() != nil {
				h.completeCancelled(attemptID, requestID, attemptStarted, started, r.Context().Err())
				return
			}
			h.completeAttemptFailure(attemptID, attemptStarted, err)
			h.candidateFailed(route.CandidateID)
			index = skipCandidate(routes, index, route.CandidateID)
			if !h.waitBeforeRetry(r.Context(), &retryIndex, index+1 < len(routes), requestID, started) {
				return
			}
			continue
		}
		firstByte := time.Since(attemptStarted).Milliseconds()
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			responseBody, readErr := io.ReadAll(response.Body)
			response.Body.Close()
			if readErr != nil {
				h.completeAttemptFailure(attemptID, attemptStarted, readErr)
				if !h.waitBeforeRetry(r.Context(), &retryIndex, index+1 < len(routes), requestID, started) {
					return
				}
				continue
			}
			h.store.CompleteAttempt(store.AttemptResult{ID: attemptID, Status: "failed", ResponseStatus: response.StatusCode, ResponseHeaders: marshalHeaders(response.Header), ResponseBody: responseBody, FirstByteMS: &firstByte, TotalMS: time.Since(attemptStarted).Milliseconds(), ErrorMessage: string(responseBody), CompletedAt: time.Now().UTC()})
			code := extractErrorCode(responseBody)
			if code == "model_not_found" || code == "model_not_supported" {
				_ = h.store.MarkModelCandidate(route.CandidateID, "config_error", code)
				h.candidateSucceeded(route.CandidateID)
				index = skipCandidate(routes, index, route.CandidateID)
				if !h.waitBeforeRetry(r.Context(), &retryIndex, index+1 < len(routes), requestID, started) {
					return
				}
				continue
			}
			if shouldTryNextKey(response.StatusCode) {
				h.candidateSucceeded(route.CandidateID)
				h.updateKeyAfterError(route, response.StatusCode, responseBody, response.Header)
				if !h.waitBeforeRetry(r.Context(), &retryIndex, index+1 < len(routes), requestID, started) {
					return
				}
				continue
			}
			if response.StatusCode >= 500 {
				h.candidateFailed(route.CandidateID)
				index = skipCandidate(routes, index, route.CandidateID)
				if !h.waitBeforeRetry(r.Context(), &retryIndex, index+1 < len(routes), requestID, started) {
					return
				}
				continue
			}
			h.candidateSucceeded(route.CandidateID)
			h.finishError(w, inboundProtocol, requestID, started, response.StatusCode, "upstream_error", string(responseBody))
			return
		}

		_ = h.store.TouchUpstreamKey(route.Key.ID)
		if requestMeta.Stream {
			err = h.proxyStream(w, r.Context(), response, route.UpstreamProtocol, inboundProtocol, route.VirtualModel, requestID, attemptID, started, attemptStarted, firstByte)
		} else {
			err = h.proxyBuffered(w, response, route.UpstreamProtocol, inboundProtocol, route.VirtualModel, requestID, attemptID, started, attemptStarted, firstByte)
		}
		if err == nil {
			h.candidateSucceeded(route.CandidateID)
			return
		}
		invalidResponseErr = err
		index = skipCandidate(routes, index, route.CandidateID)
		if !h.waitBeforeRetry(r.Context(), &retryIndex, index+1 < len(routes), requestID, started) {
			return
		}
	}
	if invalidResponseErr != nil {
		h.finishError(w, inboundProtocol, requestID, started, http.StatusBadGateway, "invalid_upstream_response", invalidResponseErr.Error())
		return
	}
	h.finishError(w, inboundProtocol, requestID, started, http.StatusServiceUnavailable, "upstream_unavailable", "所有可用密钥或候选均调用失败")
}

func (h *Handler) waitBeforeRetry(ctx context.Context, retryIndex *int, hasNext bool, requestID string, started time.Time) bool {
	if !hasNext {
		return true
	}
	delay := retryBackoff(*retryIndex)
	*retryIndex++
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		h.store.CompleteRequest(store.RequestResult{ID: requestID, Status: "cancelled", TotalMS: time.Since(started).Milliseconds(), ErrorMessage: ctx.Err().Error(), CompletedAt: time.Now().UTC()})
		return false
	}
}

func retryBackoff(retryIndex int) time.Duration {
	limit := 100 * time.Millisecond
	for range min(retryIndex, 5) {
		limit *= 2
	}
	if limit > 2*time.Second {
		limit = 2 * time.Second
	}
	floor := limit / 2
	return floor + time.Duration(rand.Int64N(int64(limit-floor)+1))
}

func (h *Handler) candidateReady(candidateID int64) bool {
	h.candidateMu.Lock()
	defer h.candidateMu.Unlock()
	state, ok := h.candidates[candidateID]
	if !ok || state.cooldownUntil.IsZero() {
		return true
	}
	if time.Now().Before(state.cooldownUntil) || state.probing {
		return false
	}
	state.probing = true
	h.candidates[candidateID] = state
	return true
}

func (h *Handler) candidateFailed(candidateID int64) {
	h.candidateMu.Lock()
	defer h.candidateMu.Unlock()
	if h.candidates == nil {
		h.candidates = make(map[int64]candidateFailure)
	}
	state := h.candidates[candidateID]
	state.count++
	if state.count >= 3 {
		state.cooldownUntil = time.Now().Add(30 * time.Second)
		state.probing = false
	}
	h.candidates[candidateID] = state
}

func (h *Handler) candidateSucceeded(candidateID int64) {
	h.candidateMu.Lock()
	delete(h.candidates, candidateID)
	h.candidateMu.Unlock()
}

func (h *Handler) completeCancelled(attemptID int64, requestID string, attemptStarted, started time.Time, err error) {
	now := time.Now().UTC()
	h.store.CompleteAttempt(store.AttemptResult{ID: attemptID, Status: "cancelled", TotalMS: time.Since(attemptStarted).Milliseconds(), ErrorMessage: err.Error(), CompletedAt: now})
	h.store.CompleteRequest(store.RequestResult{ID: requestID, Status: "cancelled", TotalMS: time.Since(started).Milliseconds(), ErrorMessage: err.Error(), CompletedAt: now})
}

func (h *Handler) proxyBuffered(w http.ResponseWriter, response *http.Response, upstreamProtocol, inboundProtocol, virtualModel, requestID string, attemptID int64, started, attemptStarted time.Time, firstByte int64) error {
	defer response.Body.Close()
	var body io.Reader = response.Body
	if h.streamIdleTimeout > 0 {
		body = newIdleTimeoutReader(response.Body, h.streamIdleTimeout)
	}
	rawBody, err := io.ReadAll(body)
	if err != nil {
		h.completeAttemptFailure(attemptID, attemptStarted, err)
		return err
	}
	var clientBody []byte
	if upstreamProtocol == inboundProtocol {
		clientBody, err = rewriteModel(rawBody, virtualModel)
	} else {
		clientBody, err = protocol.ConvertResponse(rawBody, upstreamProtocol, inboundProtocol, virtualModel)
	}
	if err != nil {
		h.completeAttemptFailure(attemptID, attemptStarted, err)
		return err
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
	return nil
}

func (h *Handler) proxyStream(w http.ResponseWriter, ctx context.Context, response *http.Response, upstreamProtocol, inboundProtocol, virtualModel, requestID string, attemptID int64, started, attemptStarted time.Time, firstByte int64) error {
	var body io.ReadCloser = response.Body
	if h.streamIdleTimeout > 0 {
		body = newIdleTimeoutReader(response.Body, h.streamIdleTimeout)
	}
	defer body.Close()
	w.Header().Set("X-Accel-Buffering", "no")
	flusher, _ := w.(http.Flusher)
	reader := bufio.NewReader(body)
	var upstreamLog, clientLog bytes.Buffer
	var firstContent *int64
	status := "completed"
	errorMessage := ""
	clientResponseStatus := response.StatusCode
	committed := false
	if upstreamProtocol == inboundProtocol {
		copyResponseHeaders(w.Header(), response.Header)
		w.Header().Del("Content-Length")
		w.WriteHeader(response.StatusCode)
		committed = true
		status, errorMessage = proxyPassthroughEvents(w, ctx, reader, flusher, virtualModel, started, &upstreamLog, &clientLog, &firstContent)
	} else {
		converter, err := protocol.NewStreamConverter(upstreamProtocol, inboundProtocol, virtualModel)
		if err != nil {
			status, errorMessage = "failed", err.Error()
		} else {
			status, errorMessage, committed = proxyConvertedEvents(w, ctx, reader, flusher, converter, response.StatusCode, response.Header, started, &upstreamLog, &clientLog, &firstContent)
		}
	}
	usage, rawUsage, usageErr := protocol.ExtractUsage(upstreamLog.Bytes(), upstreamProtocol, true)
	if usageErr != nil && errorMessage == "" {
		errorMessage = "解析上游用量失败：" + usageErr.Error()
	}
	total := time.Since(started).Milliseconds()
	h.store.CompleteAttempt(store.AttemptResult{ID: attemptID, Status: status, ResponseStatus: response.StatusCode, ResponseHeaders: marshalHeaders(response.Header), ResponseBody: upstreamLog.Bytes(), RawUsageJSON: rawUsage, FirstByteMS: &firstByte, FirstContentMS: firstContent, TotalMS: time.Since(attemptStarted).Milliseconds(), ErrorMessage: errorMessage, CompletedAt: time.Now().UTC()})
	if status == "failed" && !committed {
		return errors.New(errorMessage)
	}
	h.store.CompleteRequest(requestResult(requestID, status, clientResponseStatus, marshalHeaders(w.Header()), clientLog.Bytes(), firstContent, total, errorMessage, usage))
	return nil
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

func proxyConvertedEvents(w http.ResponseWriter, ctx context.Context, reader *bufio.Reader, flusher http.Flusher, converter *protocol.StreamConverter, responseStatus int, responseHeaders http.Header, started time.Time, upstreamLog, clientLog *bytes.Buffer, firstContent **int64) (string, string, bool) {
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
					copyResponseHeaders(w.Header(), responseHeaders)
					w.Header().Del("Content-Length")
					w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
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
	if status == http.StatusPaymentRequired {
		h.store.MarkUpstreamKey(route.Key.ID, "quota_exhausted", extractErrorCode(body), nil)
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
	now := time.Now()
	recoverAt := now.Add(60 * time.Second)
	if retryAfter, ok := parseRetryAfter(headers.Get("Retry-After"), now); ok {
		recoverAt = retryAfter
	}
	h.store.MarkUpstreamKey(route.Key.ID, "rate_limited", code, &recoverAt)
}

func parseRetryAfter(value string, now time.Time) (time.Time, bool) {
	if seconds, err := time.ParseDuration(value + "s"); err == nil && seconds >= 0 {
		return now.Add(seconds), true
	}
	parsed, err := http.ParseTime(value)
	if err != nil || parsed.Before(now) {
		return time.Time{}, false
	}
	return parsed, true
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
	return status == http.StatusUnauthorized || status == http.StatusForbidden || status == http.StatusPaymentRequired || status == http.StatusTooManyRequests
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

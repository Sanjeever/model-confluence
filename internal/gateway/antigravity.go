package gateway

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/Sanjeever/model-confluence/internal/antigravity"
	"github.com/Sanjeever/model-confluence/internal/protocol"
	"github.com/Sanjeever/model-confluence/internal/store"
)

type AntigravityImportResult struct {
	Accounts []store.UpstreamKey      `json:"accounts"`
	Errors   []AntigravityImportError `json:"errors"`
}

type AntigravityImportError struct {
	Email string `json:"email"`
	Error string `json:"error"`
}

func (h *Handler) ImportAntigravityAccounts(ctx context.Context, providerID int64, fileName string, data []byte) (AntigravityImportResult, error) {
	result := AntigravityImportResult{Accounts: []store.UpstreamKey{}, Errors: []AntigravityImportError{}}
	provider, err := h.store.ProviderByID(providerID)
	if err != nil {
		return result, err
	}
	if provider.Kind != store.ProviderAntigravityOAuth {
		return result, errors.New("供应商不是 Antigravity OAuth")
	}
	entries, err := antigravity.ParseImport(data)
	if err != nil {
		return result, err
	}
	if h.antigravityOAuth.ID == "" || h.antigravityOAuth.Secret == "" {
		return result, errors.New("请配置 MODEL_CONFLUENCE_ANTIGRAVITY_OAUTH_CLIENT_ID 和 MODEL_CONFLUENCE_ANTIGRAVITY_OAUTH_CLIENT_SECRET")
	}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		credential, err := antigravity.Refresh(ctx, h.client, h.antigravityOAuth, antigravity.Credential{ClientID: entry.ClientID, RefreshToken: entry.RefreshToken})
		if err == nil {
			credential, err = antigravity.Discover(ctx, h.client, credential)
		}
		if err == nil && entry.Email != "" && !strings.EqualFold(entry.Email, credential.Email) {
			err = errors.New("文件邮箱与 Google 返回的账号身份不一致")
		}
		var key store.UpstreamKey
		if err == nil {
			key, err = h.store.ImportAntigravityCredential(providerID, fileName, credential)
		}
		if err != nil {
			result.Errors = append(result.Errors, AntigravityImportError{Email: entry.Email, Error: err.Error()})
			continue
		}
		result.Accounts = append(result.Accounts, key)
	}
	return result, nil
}

func (h *Handler) antigravityCredential(ctx context.Context, id, rejectedRevision int64) (store.AntigravityCredential, bool, error) {
	lock, _ := h.oauthLocks.LoadOrStore(id, make(chan struct{}, 1))
	ch := lock.(chan struct{})
	select {
	case ch <- struct{}{}:
	case <-ctx.Done():
		return store.AntigravityCredential{}, false, ctx.Err()
	}
	defer func() { <-ch }()
	c, err := h.store.AntigravityCredential(id)
	if err != nil {
		return c, false, err
	}
	if c.ClientID != h.antigravityOAuth.ID {
		return c, false, errors.New("Antigravity 账号 OAuth client_id 与运行配置不一致")
	}
	if time.Until(c.ExpiresAt) > time.Minute && (rejectedRevision == 0 || c.Revision != rejectedRevision) {
		return c, rejectedRevision != 0, nil
	}
	updated, err := antigravity.Refresh(ctx, h.client, h.antigravityOAuth, c.Credential)
	if err != nil {
		var failure *antigravity.RefreshError
		if errors.As(err, &failure) && failure.Permanent {
			if stateErr := h.store.MarkUpstreamKeyRevision(id, c.Revision, "auth_invalid", failure.Code, nil); stateErr != nil {
				return c, false, fmt.Errorf("记录 Antigravity 刷新失败: %w", stateErr)
			}
		}
		return c, false, err
	}
	if err := h.store.SaveAntigravityCredential(id, c.Revision, updated); err != nil {
		return c, false, err
	}
	c, err = h.store.AntigravityCredential(id)
	return c, true, err
}

func (h *Handler) antigravityModels(ctx context.Context, provider store.Provider) ([]string, error) {
	var lastErr error
	for _, key := range provider.Keys {
		if !store.KeyAvailable(key) {
			continue
		}
		credential, _, err := h.antigravityCredential(ctx, key.ID, 0)
		if err != nil {
			lastErr = err
			continue
		}
		return antigravity.Models(ctx, h.client, credential.Credential)
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, ErrProviderNoKey
}

type antigravityAttemptResult struct {
	finished      bool
	status        int
	nextKey       bool
	retryRevision int64
	err           error
}

func (h *Handler) attemptAntigravity(w http.ResponseWriter, r *http.Request, route store.ResolvedRoute, inbound, requestID string, started time.Time, body []byte, stream *streamResponse, rejectedRevision int64, position *int, history map[string]store.AntigravityToolHistory) antigravityAttemptResult {
	request, ignored, err := protocol.PrepareAntigravityRequest(body, inbound, route.UpstreamModel, route.DefaultMaxOutputTokens, route.MaxOutputTokens)
	if err != nil {
		return antigravityAttemptResult{status: 400, err: err}
	}
	turns := make(map[string]protocol.AntigravityToolTurn)
	for id, item := range history {
		turns[id] = item.Turn
	}
	if err := protocol.RestoreAntigravityHistory(&request, turns); err != nil {
		return antigravityAttemptResult{status: 400, err: err}
	}
	// 内容编码和历史校验先于 OAuth 刷新，拒绝不支持的请求时不访问上游。
	if _, err := protocol.EncodeAntigravityRequest(request, "", requestID); err != nil {
		return antigravityAttemptResult{status: 400, err: err}
	}
	credential, refreshed, err := h.antigravityCredential(r.Context(), route.Key.ID, rejectedRevision)
	if err != nil {
		if r.Context().Err() != nil {
			h.store.CompleteRequest(store.RequestResult{ID: requestID, Status: "cancelled", TotalMS: time.Since(started).Milliseconds(), ErrorMessage: r.Context().Err().Error(), CompletedAt: time.Now().UTC()})
			return antigravityAttemptResult{finished: true}
		}
		return antigravityAttemptResult{nextKey: true, err: err}
	}
	route.Key.CredentialRevision = credential.Revision
	upstreamBody, err := protocol.EncodeAntigravityRequest(request, credential.ProjectID, requestID)
	if err != nil {
		return antigravityAttemptResult{status: 400, err: err}
	}
	outbound, err := http.NewRequestWithContext(r.Context(), http.MethodPost, route.UpstreamEndpoint, bytes.NewReader(upstreamBody))
	if err != nil {
		return antigravityAttemptResult{status: 500, err: err}
	}
	// 订阅后端的请求头由适配器构造，客户端凭据与协议私有头不进入此连接。
	antigravity.ApplyHeaders(outbound.Header, credential.AccessToken)
	attemptStarted := time.Now()
	attemptID, err := h.store.StartAttempt(store.AttemptStart{RequestID: requestID, Position: *position, ProviderID: route.Provider.ID, ProviderName: route.Provider.Name, UpstreamKeyID: route.Key.ID, UpstreamKeyName: route.Key.Name, CandidateID: route.CandidateID, UpstreamModel: route.UpstreamModel, Protocol: protocol.Antigravity, Endpoint: route.UpstreamEndpoint, UpstreamStream: true, IgnoredParameters: ignored, Headers: marshalHeaders(outbound.Header), Body: upstreamBody, CreatedAt: attemptStarted.UTC()})
	if err != nil {
		return antigravityAttemptResult{status: 500, err: fmt.Errorf("建立上游调用日志: %w", err)}
	}
	*position++
	response, err := h.doUpstream(w, outbound, nil)
	if err != nil {
		if r.Context().Err() != nil {
			h.completeCancelled(w, stream, attemptID, requestID, attemptStarted, started, r.Context().Err())
			return antigravityAttemptResult{finished: true}
		}
		if logErr := h.store.CompleteAttempt(store.AttemptResult{ID: attemptID, Status: "failed", TotalMS: time.Since(attemptStarted).Milliseconds(), ErrorMessage: err.Error(), CompletedAt: time.Now().UTC()}); logErr != nil {
			return antigravityAttemptResult{status: 500, err: logErr}
		}
		return antigravityAttemptResult{err: err}
	}
	firstByte := time.Since(attemptStarted).Milliseconds()
	if response.StatusCode < 200 || response.StatusCode >= 300 || !strings.HasPrefix(strings.ToLower(response.Header.Get("Content-Type")), "text/event-stream") {
		responseBody, readErr := io.ReadAll(response.Body)
		response.Body.Close()
		failure := antigravity.ClassifyError(responseBody, response.StatusCode, time.Now())
		err := error(failure)
		status := "failed"
		if readErr != nil {
			err = readErr
		}
		if r.Context().Err() != nil {
			status, err = "cancelled", r.Context().Err()
		}
		if logErr := h.store.CompleteAttempt(store.AttemptResult{ID: attemptID, Status: status, ResponseStatus: response.StatusCode, ResponseHeaders: marshalHeaders(response.Header), ResponseBody: responseBody, FirstByteMS: &firstByte, TotalMS: time.Since(attemptStarted).Milliseconds(), ErrorMessage: err.Error(), CompletedAt: time.Now().UTC()}); logErr != nil {
			return antigravityAttemptResult{status: 500, err: logErr}
		}
		if status == "cancelled" {
			h.store.CompleteRequest(store.RequestResult{ID: requestID, Status: status, TotalMS: time.Since(started).Milliseconds(), ErrorMessage: err.Error(), CompletedAt: time.Now().UTC()})
			return antigravityAttemptResult{finished: true}
		}
		if readErr != nil {
			return antigravityAttemptResult{err: readErr}
		}
		return h.antigravityFailure(route, failure, response.Header, refreshed, rejectedRevision)
	}
	_ = h.store.TouchUpstreamKey(route.Key.ID)
	err = h.proxyAntigravity(w, r.Context(), response, route, inbound, requestID, attemptID, started, attemptStarted, firstByte, stream, clientWantsStreamUsage(body))
	if errors.Is(err, errAntigravityLogging) {
		return antigravityAttemptResult{status: 500, err: err}
	}
	if err == nil || errors.Is(err, errAntigravityResponseFinished) {
		if err == nil {
			h.candidateSucceeded(route.CandidateID, route.CandidateRevision)
		}
		return antigravityAttemptResult{finished: true}
	}
	var failure *antigravity.UpstreamError
	if errors.As(err, &failure) {
		return h.antigravityFailure(route, failure, response.Header, refreshed, rejectedRevision)
	}
	return antigravityAttemptResult{err: err}
}

func (h *Handler) antigravityFailure(route store.ResolvedRoute, failure *antigravity.UpstreamError, headers http.Header, refreshed bool, rejectedRevision int64) antigravityAttemptResult {
	if failure.Status == 401 && !refreshed && rejectedRevision == 0 {
		return antigravityAttemptResult{retryRevision: route.Key.CredentialRevision, err: failure}
	}
	var err error
	switch {
	case failure.Status == 401:
		err = h.store.MarkUpstreamKeyRevision(route.Key.ID, route.Key.CredentialRevision, "auth_invalid", failure.Code, nil)
	case failure.Status == 429:
		recoverAt := time.Now().Add(time.Minute)
		if failure.RecoverAt != nil {
			recoverAt = *failure.RecoverAt
		} else if reset, ok := parseRetryAfter(headers.Get("Retry-After"), time.Now()); ok {
			recoverAt = reset
		}
		err = h.store.MarkAntigravityModel(route.Key.ID, route.Key.CredentialRevision, route.UpstreamModel, failure.Code, recoverAt)
	case failure.Code == "MODEL_NOT_FOUND" || failure.Code == "MODEL_NOT_SUPPORTED":
		err = h.store.MarkModelCandidate(route.CandidateID, route.CandidateRevision, "config_error", failure.Code)
	}
	if err != nil {
		return antigravityAttemptResult{status: 500, err: err}
	}
	if shouldTryNextKey(failure.Status) {
		return antigravityAttemptResult{nextKey: true, err: failure}
	}
	if failure.Code == "MODEL_NOT_FOUND" || failure.Code == "MODEL_NOT_SUPPORTED" || failure.Status >= 500 {
		return antigravityAttemptResult{err: failure}
	}
	return antigravityAttemptResult{status: failure.Status, err: failure}
}

var errAntigravityResponseFinished = errors.New("Antigravity response already recorded")
var errAntigravityLogging = errors.New("记录 Antigravity 响应失败")

func (h *Handler) proxyAntigravity(w http.ResponseWriter, ctx context.Context, response *http.Response, route store.ResolvedRoute, inbound, requestID string, attemptID int64, started, attemptStarted time.Time, firstByte int64, stream *streamResponse, includeUsage bool) error {
	var reader io.ReadCloser = response.Body
	if h.streamIdleTimeout > 0 {
		reader = newIdleTimeoutReader(response.Body, h.streamIdleTimeout)
	}
	defer reader.Close()
	converter, err := protocol.NewStreamConverter(protocol.Antigravity, inbound, route.VirtualModel)
	if err != nil {
		return err
	}
	converter.SetChatUsage(includeUsage)
	converter.SetAntigravityToolStore(func(turn protocol.AntigravityToolTurn) error {
		if err := h.store.SaveAntigravityToolTurn(requestID, route, turn); err != nil {
			return fmt.Errorf("%w: 保存工具历史: %v", errAntigravityLogging, err)
		}
		return nil
	})
	readCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	events := readStreamEvents(readCtx, bufio.NewReader(reader))
	var raw, pending bytes.Buffer
	var firstContent *int64
	var heartbeat <-chan time.Time
	if stream != nil {
		heartbeat = stream.Heartbeat()
	}
	done := false
	for !done && err == nil {
		select {
		case event, ok := <-events:
			if !ok {
				err = io.ErrUnexpectedEOF
				break
			}
			raw.Write(event.event.Raw)
			if event.err != nil {
				err = event.err
				if errors.Is(err, io.EOF) {
					err = fmt.Errorf("Antigravity 流在 finishReason 前中断")
				}
				break
			}
			var chunks [][]byte
			var semantic bool
			chunks, semantic, done, err = converter.Convert(event.event)
			if err != nil {
				break
			}
			if semantic && firstContent == nil {
				value := time.Since(started).Milliseconds()
				firstContent = &value
			}
			if stream == nil {
				continue
			}
			for _, chunk := range chunks {
				pending.Write(chunk)
			}
			if semantic || done || stream.Committed() {
				if !stream.Committed() {
					w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
					w.Header().Set("X-Accel-Buffering", "no")
					w.Header().Set("Cache-Control", "no-cache")
				}
				err = stream.Write(w, pending.Bytes())
				pending.Reset()
			}
		case <-heartbeat:
			if stream.Committed() {
				err = stream.WriteHeartbeat(w)
			}
		case <-ctx.Done():
			err = ctx.Err()
		}
	}
	canonical, completionErr := converter.AntigravityResponse()
	usage, rawUsage := canonical.Usage, string(canonical.Usage.Raw)
	var clientBody []byte
	if err == nil {
		err = completionErr
		if err == nil {
			usage, rawUsage = canonical.Usage, string(canonical.Usage.Raw)
			canonical.Model = route.VirtualModel
			if stream == nil {
				if inbound == protocol.Messages {
					canonical.Usage = protocol.AntigravityAccountingUsage(canonical.Usage)
				}
				clientBody, err = protocol.EncodeResponse(canonical, inbound)
			}
		}
	}
	status, message := "completed", ""
	if err != nil {
		status, message = "failed", err.Error()
	}
	if ctx.Err() != nil || errors.Is(err, errClientStreamWrite) {
		status = "cancelled"
		if ctx.Err() != nil {
			err, message = ctx.Err(), ctx.Err().Error()
		}
	}
	if logErr := h.store.CompleteAttempt(store.AttemptResult{ID: attemptID, Status: status, ResponseStatus: response.StatusCode, ResponseHeaders: marshalHeaders(response.Header), ResponseBody: raw.Bytes(), RawUsageJSON: rawUsage, FirstByteMS: &firstByte, FirstContentMS: firstContent, TotalMS: time.Since(attemptStarted).Milliseconds(), ErrorMessage: message, CompletedAt: time.Now().UTC()}); logErr != nil {
		if stream != nil && stream.Committed() {
			h.finishStreamError(w, inbound, requestID, started, stream, "logging_error", logErr.Error())
			return errAntigravityResponseFinished
		}
		return fmt.Errorf("%w: %v", errAntigravityLogging, logErr)
	}
	if status == "failed" && (stream == nil || !stream.Committed()) {
		return err
	}
	clientStatus := 200
	if stream != nil {
		if status == "failed" {
			var failure *antigravity.UpstreamError
			if errors.As(err, &failure) {
				result := h.antigravityFailure(route, failure, response.Header, true, route.Key.CredentialRevision)
				if result.status == 500 {
					message = result.err.Error()
				}
			}
			stream.WriteError(w, inbound, "upstream_error", message, requestID)
		}
		clientBody, clientStatus = stream.Body(), stream.Status()
	} else if status == "completed" {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(200)
		if _, writeErr := w.Write(clientBody); writeErr != nil {
			status, message = "cancelled", writeErr.Error()
		}
	}
	if logErr := h.store.CompleteRequest(requestResult(requestID, status, clientStatus, marshalHeaders(w.Header()), clientBody, firstContent, time.Since(started).Milliseconds(), message, protocol.AntigravityAccountingUsage(usage))); logErr != nil {
		slog.Error("record Antigravity response", "request_id", requestID, "error", logErr)
		return errAntigravityResponseFinished
	}
	if status != "completed" {
		return errAntigravityResponseFinished
	}
	return nil
}

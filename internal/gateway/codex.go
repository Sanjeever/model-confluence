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
	"time"

	"github.com/Sanjeever/model-confluence/internal/codex"
	"github.com/Sanjeever/model-confluence/internal/protocol"
	"github.com/Sanjeever/model-confluence/internal/store"
)

var errCodexResponseFinished = errors.New("Codex response already recorded")

func (h *Handler) codexCredential(ctx context.Context, id, rejectedRevision int64) (store.OAuthCredential, bool, error) {
	lock, _ := h.oauthLocks.LoadOrStore(id, make(chan struct{}, 1))
	ch := lock.(chan struct{})
	select {
	case ch <- struct{}{}:
	case <-ctx.Done():
		return store.OAuthCredential{}, false, ctx.Err()
	}
	defer func() { <-ch }()
	c, err := h.store.OAuthCredential(id)
	if err != nil {
		return c, false, err
	}
	due := time.Until(c.Auth.ExpiresAt) <= time.Minute
	if !due && (rejectedRevision == 0 || c.Revision != rejectedRevision) {
		return c, rejectedRevision != 0, nil
	}
	endpoint := codex.TokenEndpoint
	if h.oauthTokenEndpoint != "" {
		endpoint = h.oauthTokenEndpoint
	}
	data, err := codex.Refresh(ctx, h.client, endpoint, c.Auth)
	if err != nil {
		var failure *codex.RefreshError
		status := "available"
		if errors.As(err, &failure) && failure.Permanent {
			status = "auth_invalid"
		}
		if stateErr := h.store.MarkUpstreamKeyRevision(id, c.Revision, status, err.Error(), nil); stateErr != nil {
			return c, false, fmt.Errorf("记录 OAuth 刷新失败: %w", stateErr)
		}
		return c, false, err
	}
	if err := h.store.SaveOAuthCredential(id, c.Revision, data); err != nil {
		return c, false, err
	}
	c, err = h.store.OAuthCredential(id)
	return c, true, err
}

func (h *Handler) proxyCodex(w http.ResponseWriter, ctx context.Context, response *http.Response, route store.ResolvedRoute, inboundProtocol, requestID string, attemptID int64, started, attemptStarted time.Time, firstByte int64, stream *streamResponse, includeUsage bool) error {
	model := route.VirtualModel
	var body io.ReadCloser = response.Body
	if h.streamIdleTimeout > 0 {
		body = newIdleTimeoutReader(response.Body, h.streamIdleTimeout)
	}
	defer body.Close()
	readCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	events := readStreamEvents(readCtx, bufio.NewReader(body))
	var state codex.Stream
	var upstreamLog, pending bytes.Buffer
	var firstContent *int64
	var converter *protocol.StreamConverter
	var err error
	if stream != nil && inboundProtocol != protocol.Responses {
		converter, err = protocol.NewStreamConverter(protocol.Responses, inboundProtocol, model)
		if err != nil {
			return err
		}
	}
	var heartbeat <-chan time.Time
	if stream != nil {
		heartbeat = stream.Heartbeat()
	}
	for !state.Done && err == nil {
		select {
		case result, ok := <-events:
			if !ok {
				err = ctx.Err()
				if err == nil {
					err = io.ErrUnexpectedEOF
				}
				break
			}
			upstreamLog.Write(result.event.Raw)
			if result.err != nil {
				err = result.err
				if errors.Is(err, io.EOF) {
					err = fmt.Errorf("Codex 流在终止事件前中断")
				}
				break
			}
			var event protocol.SSEEvent
			var semantic bool
			event, semantic, err = state.Consume(result.event, model)
			if err != nil {
				break
			}
			if semantic && firstContent == nil {
				elapsed := time.Since(started).Milliseconds()
				firstContent = &elapsed
			}
			if stream == nil {
				continue
			}
			chunks := [][]byte{event.Raw}
			if converter != nil {
				chunks, semantic, _, err = converter.Convert(event)
				if err != nil {
					break
				}
				if inboundProtocol == protocol.Chat && !includeUsage {
					chunks = withoutChatUsage(chunks)
				}
			}
			for _, chunk := range chunks {
				pending.Write(chunk)
			}
			if semantic || state.Done || stream.Committed() {
				if !stream.Committed() {
					copyResponseHeaders(w.Header(), response.Header)
					w.Header().Del("Content-Length")
					w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
					w.Header().Set("X-Accel-Buffering", "no")
				}
				err = stream.Write(w, pending.Bytes())
				pending.Reset()
			}
		case <-heartbeat:
			// 首内容前保留可切换边界，提交后继续发送客户端心跳。
			if stream.Committed() {
				err = stream.WriteHeartbeat(w)
			}
		case <-ctx.Done():
			err = ctx.Err()
		}
	}
	usage, rawUsage, usageErr := protocol.ExtractUsage(upstreamLog.Bytes(), protocol.Responses, true)
	status := "completed"
	message := ""
	if err != nil {
		status = "failed"
		message = err.Error()
		if ctx.Err() != nil || errors.Is(err, errClientStreamWrite) {
			status = "cancelled"
		}
	} else if usageErr != nil {
		err = usageErr
		status = "failed"
		message = "解析 Codex 用量失败: " + err.Error()
	}
	var clientBody []byte
	if err == nil && stream == nil {
		if inboundProtocol == protocol.Responses {
			clientBody = state.Response
		} else {
			clientBody, err = protocol.ConvertResponse(state.Response, protocol.Responses, inboundProtocol, model)
		}
		if err != nil {
			status = "failed"
			message = err.Error()
		}
	}
	result := store.AttemptResult{ID: attemptID, Status: status, ResponseStatus: response.StatusCode, ResponseHeaders: marshalHeaders(response.Header), ResponseBody: upstreamLog.Bytes(), RawUsageJSON: rawUsage, FirstByteMS: &firstByte, FirstContentMS: firstContent, TotalMS: time.Since(attemptStarted).Milliseconds(), ErrorMessage: message, CompletedAt: time.Now().UTC()}
	if logErr := h.store.CompleteAttempt(result); logErr != nil {
		return fmt.Errorf("记录 Codex 响应失败: %w", logErr)
	}
	if err != nil && status != "cancelled" && (stream == nil || !stream.Committed()) {
		return err
	}
	if err != nil && status == "failed" {
		var failure *codex.UpstreamError
		if errors.As(err, &failure) {
			if stateErr := h.updateCodexKeyAfterError(route, failure, response.Header); stateErr != nil {
				return stateErr
			}
		}
		h.candidateFailed(route.CandidateID, route.CandidateRevision)
	}
	clientStatus := http.StatusOK
	if stream != nil {
		if err != nil && status == "failed" {
			stream.WriteError(w, inboundProtocol, "upstream_error", message, requestID)
		}
		clientBody = stream.Body()
		clientStatus = stream.Status()
	} else if err == nil {
		copyResponseHeaders(w.Header(), response.Header)
		w.Header().Del("Content-Length")
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		if _, writeErr := w.Write(clientBody); writeErr != nil {
			status = "cancelled"
			message = writeErr.Error()
		}
	}
	if logErr := h.store.CompleteRequest(requestResult(requestID, status, clientStatus, marshalHeaders(w.Header()), clientBody, firstContent, time.Since(started).Milliseconds(), message, usage)); logErr != nil {
		if stream == nil && err == nil {
			slog.Error("record Codex client response", "request_id", requestID, "error", logErr)
			return fmt.Errorf("%w: %v", errCodexResponseFinished, logErr)
		}
		return fmt.Errorf("记录客户端响应失败: %w", logErr)
	}
	if status != "completed" {
		return errCodexResponseFinished
	}
	if err := h.store.MarkUpstreamKeyRevision(route.Key.ID, route.Key.CredentialRevision, "available", "", nil); err != nil {
		slog.Error("record Codex account recovery", "request_id", requestID, "error", err)
		return fmt.Errorf("%w: %v", errCodexResponseFinished, err)
	}
	return nil
}

func withoutChatUsage(chunks [][]byte) [][]byte {
	filtered := chunks[:0]
	for _, chunk := range chunks {
		// Chat 转换器将 usage 编码为 choices 为空的独立 chunk。
		if bytes.Contains(chunk, []byte(`"choices":[]`)) && bytes.Contains(chunk, []byte(`"usage":`)) {
			continue
		}
		filtered = append(filtered, chunk)
	}
	return filtered
}

func (h *Handler) updateCodexKeyAfterError(route store.ResolvedRoute, failure *codex.UpstreamError, headers http.Header) error {
	status := "available"
	recoverAt := failure.RecoverAt
	switch {
	case failure.Status == 401:
		status = "auth_invalid"
	case failure.Code == "usage_limit_reached" || (failure.Status == 402 || failure.Status == 429) && contains(route.Provider.QuotaCodes, failure.Code):
		status = "quota_exhausted"
	case failure.Status == 429:
		status = "rate_limited"
		reset := time.Now().Add(time.Minute)
		if parsed, ok := parseRetryAfter(headers.Get("Retry-After"), time.Now()); ok {
			reset = parsed
		}
		recoverAt = &reset
	default:
		return nil
	}
	return h.store.MarkUpstreamKeyRevision(route.Key.ID, route.Key.CredentialRevision, status, failure.Code, recoverAt)
}

package gateway

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/Sanjeever/model-confluence/internal/protocol"
	"github.com/Sanjeever/model-confluence/internal/store"
)

var errClientStreamWrite = errors.New("write client stream")

type streamResponse struct {
	ticker    *time.Ticker
	committed bool
	status    int
	body      bytes.Buffer
}

func newStreamResponse(interval time.Duration) *streamResponse {
	response := &streamResponse{}
	if interval > 0 {
		response.ticker = time.NewTicker(interval)
	}
	return response
}

func (r *streamResponse) Close() {
	if r.ticker != nil {
		r.ticker.Stop()
	}
}

func (r *streamResponse) Heartbeat() <-chan time.Time {
	if r.ticker == nil {
		return nil
	}
	return r.ticker.C
}

func (r *streamResponse) Committed() bool {
	return r.committed
}

func (r *streamResponse) Commit(w http.ResponseWriter, status int) {
	if r.committed {
		return
	}
	w.WriteHeader(status)
	r.committed = true
	r.status = status
}

func (r *streamResponse) Status() int {
	return r.status
}

func (r *streamResponse) Body() []byte {
	return r.body.Bytes()
}

func (r *streamResponse) WriteHeartbeat(w http.ResponseWriter) error {
	if !r.committed {
		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("X-Accel-Buffering", "no")
		r.Commit(w, http.StatusOK)
	}
	return r.Write(w, []byte(": keep-alive\n\n"))
}

func (r *streamResponse) Write(w http.ResponseWriter, body []byte) error {
	if !r.committed {
		r.Commit(w, http.StatusOK)
	}
	written, err := w.Write(body)
	if written > 0 {
		r.body.Write(body[:written])
	}
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
	if err != nil {
		return fmt.Errorf("%w: %v", errClientStreamWrite, err)
	}
	return nil
}

func (r *streamResponse) WriteError(w http.ResponseWriter, inboundProtocol, code, message, requestID string) {
	var event []byte
	switch inboundProtocol {
	case protocol.Messages:
		payload, _ := json.Marshal(map[string]any{"type": "error", "error": map[string]string{"type": code, "message": message}, "request_id": requestID})
		event = append(append([]byte("event: error\ndata: "), payload...), []byte("\n\n")...)
	case protocol.Responses:
		payload, _ := json.Marshal(map[string]any{"type": "error", "code": code, "message": message, "param": nil, "request_id": requestID})
		event = append(append([]byte("event: error\ndata: "), payload...), []byte("\n\n")...)
	default:
		payload, _ := json.Marshal(map[string]any{"error": map[string]any{"message": message, "type": code, "code": code}, "request_id": requestID})
		event = append(append([]byte("data: "), payload...), []byte("\n\n")...)
	}
	_ = r.Write(w, event)
}

type upstreamResult struct {
	response *http.Response
	err      error
}

func (h *Handler) doUpstream(w http.ResponseWriter, request *http.Request, stream *streamResponse) (*http.Response, error) {
	if stream == nil || stream.ticker == nil {
		return h.client.Do(request)
	}
	results := make(chan upstreamResult, 1)
	done := make(chan struct{})
	defer close(done)
	go func() {
		response, err := h.client.Do(request)
		select {
		case results <- upstreamResult{response: response, err: err}:
		case <-done:
			if response != nil {
				response.Body.Close()
			}
		}
	}()
	for {
		select {
		case result := <-results:
			return result.response, result.err
		case <-stream.Heartbeat():
			if err := stream.WriteHeartbeat(w); err != nil {
				return nil, err
			}
		case <-request.Context().Done():
			return nil, request.Context().Err()
		}
	}
}

type streamLineResult struct {
	line []byte
	err  error
}

func readStreamLines(ctx context.Context, reader *bufio.Reader) <-chan streamLineResult {
	results := make(chan streamLineResult)
	go func() {
		defer close(results)
		for {
			line, err := reader.ReadBytes('\n')
			select {
			case results <- streamLineResult{line: line, err: err}:
			case <-ctx.Done():
				return
			}
			if err != nil {
				return
			}
		}
	}()
	return results
}

type streamEventResult struct {
	event protocol.SSEEvent
	err   error
}

func readStreamEvents(ctx context.Context, reader *bufio.Reader) <-chan streamEventResult {
	results := make(chan streamEventResult)
	go func() {
		defer close(results)
		for {
			event, err := protocol.ReadSSEEvent(reader)
			select {
			case results <- streamEventResult{event: event, err: err}:
			case <-ctx.Done():
				return
			}
			if err != nil {
				return
			}
		}
	}()
	return results
}

func (h *Handler) finishStreamError(w http.ResponseWriter, inboundProtocol, requestID string, started time.Time, stream *streamResponse, code, message string) {
	stream.WriteError(w, inboundProtocol, code, message, requestID)
	h.store.CompleteRequest(store.RequestResult{ID: requestID, Status: "failed", ResponseStatus: stream.Status(), ResponseHeaders: marshalHeaders(w.Header()), ResponseBody: stream.Body(), TotalMS: time.Since(started).Milliseconds(), ErrorMessage: message, CompletedAt: time.Now().UTC()})
}

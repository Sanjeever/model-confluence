package gateway

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Sanjeever/model-confluence/internal/protocol"
	"github.com/Sanjeever/model-confluence/internal/store"
)

type recordingResponseWriter struct {
	header    http.Header
	committed bool
	body      bytes.Buffer
	flushes   int
	flushed   chan struct{}
}

func (w *recordingResponseWriter) Header() http.Header {
	return w.header
}

func (w *recordingResponseWriter) Write(body []byte) (int, error) {
	if !w.committed {
		w.WriteHeader(http.StatusOK)
	}
	return w.body.Write(body)
}

func (w *recordingResponseWriter) WriteHeader(int) {
	w.committed = true
}

func (w *recordingResponseWriter) Flush() {
	w.flushes++
	if w.flushed != nil {
		select {
		case w.flushed <- struct{}{}:
		default:
		}
	}
}

func TestProxyConvertedEventsDelaysCommitBeforeConversionError(t *testing.T) {
	converter, err := protocol.NewStreamConverter(protocol.Messages, protocol.Chat, "virtual-model")
	if err != nil {
		t.Fatal(err)
	}
	stream := strings.Join([]string{
		"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_test\",\"model\":\"upstream-model\"}}\n\n",
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"image\"}}\n\n",
	}, "")
	w := &recordingResponseWriter{header: make(http.Header)}
	streamResponse := newStreamResponse(0)
	defer streamResponse.Close()
	var upstreamLog bytes.Buffer
	var firstContent *int64

	status, message, committed := proxyConvertedEvents(w, context.Background(), bufio.NewReader(strings.NewReader(stream)), streamResponse, converter, http.StatusOK, nil, time.Now(), &upstreamLog, &firstContent)

	if status != "failed" || message != `unsupported Messages stream block "image"` {
		t.Fatalf("unexpected result: status=%q message=%q", status, message)
	}
	if committed || w.committed {
		t.Fatal("response was committed before the conversion error")
	}
	if len(streamResponse.Body()) != 0 {
		t.Fatalf("client received partial stream: %s", streamResponse.Body())
	}
}

func TestDoUpstreamWritesHeartbeatWhileWaitingForHeaders(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(30 * time.Millisecond)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	request, err := http.NewRequest(http.MethodPost, upstream.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	w := &recordingResponseWriter{header: make(http.Header)}
	stream := newStreamResponse(5 * time.Millisecond)
	defer stream.Close()
	h := &Handler{client: upstream.Client()}

	response, err := h.doUpstream(w, request, stream)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()

	if !strings.Contains(w.body.String(), ": keep-alive\n\n") {
		t.Fatalf("heartbeat was not written: %q", w.body.String())
	}
	if w.flushes == 0 {
		t.Fatal("heartbeat was not flushed")
	}
}

func TestProxyPassthroughWritesHeartbeatWhileWaitingForFirstEvent(t *testing.T) {
	reader, writer := io.Pipe()
	w := &recordingResponseWriter{header: make(http.Header), flushed: make(chan struct{}, 1)}
	stream := newStreamResponse(5 * time.Millisecond)
	defer stream.Close()
	go func() {
		<-w.flushed
		_, _ = io.WriteString(writer, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\n")
		_ = writer.Close()
	}()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var upstreamLog bytes.Buffer
	var firstContent *int64

	status, message := proxyPassthroughEvents(w, ctx, bufio.NewReader(reader), stream, "virtual-model", time.Now(), &upstreamLog, &firstContent)

	if status != "completed" || message != "" {
		t.Fatalf("unexpected result: status=%q message=%q", status, message)
	}
	body := w.body.String()
	heartbeatIndex := strings.Index(body, ": keep-alive\n\n")
	contentIndex := strings.Index(body, `"content":"ok"`)
	if heartbeatIndex < 0 || contentIndex < 0 || heartbeatIndex > contentIndex {
		t.Fatalf("heartbeat did not precede content: %q", body)
	}
}

func TestShouldTryNextKeyOnPaymentRequired(t *testing.T) {
	if !shouldTryNextKey(http.StatusPaymentRequired) {
		t.Fatal("402 should try the next upstream route")
	}
}

func TestRetryBackoffStaysWithinExponentialCap(t *testing.T) {
	for retryIndex, limit := range []time.Duration{100 * time.Millisecond, 200 * time.Millisecond, 400 * time.Millisecond, 800 * time.Millisecond, 1600 * time.Millisecond, 2 * time.Second, 2 * time.Second} {
		for range 20 {
			delay := retryBackoff(retryIndex)
			if delay < 0 || delay > limit {
				t.Fatalf("retry %d returned delay %s above limit %s", retryIndex, delay, limit)
			}
		}
	}
}

func TestCandidateCooldownAllowsSingleProbe(t *testing.T) {
	h := &Handler{candidates: make(map[int64]candidateFailure)}
	h.candidateFailed(7, 1)
	h.candidateFailed(7, 1)
	h.candidateFailed(7, 1)
	if h.candidateReady(7, 1) {
		t.Fatal("candidate should be cooling down")
	}
	h.candidateMu.Lock()
	state := h.candidates[7]
	state.cooldownUntil = time.Now().Add(-time.Second)
	h.candidates[7] = state
	h.candidateMu.Unlock()
	if !h.candidateReady(7, 1) {
		t.Fatal("candidate should allow a probe after cooldown")
	}
	if h.candidateReady(7, 1) {
		t.Fatal("candidate should allow only one concurrent probe")
	}
	h.candidateSucceeded(7, 1)
	if !h.candidateReady(7, 1) {
		t.Fatal("successful probe should reset candidate state")
	}
}

func TestCandidateCooldownDoesNotCarryAcrossRevision(t *testing.T) {
	h := &Handler{candidates: make(map[int64]candidateFailure)}
	h.candidateFailed(7, 1)
	h.candidateFailed(7, 1)
	h.candidateFailed(7, 1)
	if !h.candidateReady(7, 2) {
		t.Fatal("updated candidate inherited the previous revision cooldown")
	}

	h.candidateFailed(7, 2)
	h.candidateFailed(7, 2)
	h.candidateFailed(7, 2)
	h.candidateSucceeded(7, 1)
	if h.candidateReady(7, 2) {
		t.Fatal("stale candidate success cleared the current revision cooldown")
	}
}

func TestParseRetryAfterSupportsSecondsAndHTTPDate(t *testing.T) {
	now := time.Date(2026, 8, 24, 8, 0, 0, 0, time.UTC)
	seconds, ok := parseRetryAfter("45", now)
	if !ok || !seconds.Equal(now.Add(45*time.Second)) {
		t.Fatalf("unexpected seconds result: %s, %t", seconds, ok)
	}
	httpDate := now.Add(2 * time.Minute).Format(http.TimeFormat)
	parsed, ok := parseRetryAfter(httpDate, now)
	if !ok || !parsed.Equal(now.Add(2*time.Minute)) {
		t.Fatalf("unexpected HTTP date result: %s, %t", parsed, ok)
	}
}

func TestUpdateKeyAfterPaymentRequiredMarksQuota(t *testing.T) {
	database, err := store.Open(filepath.Join(t.TempDir(), "model-confluence.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	_, err = database.CreateProvider(store.CreateProviderInput{
		Name:      "DeepSeek",
		AuthType:  "bearer",
		Endpoints: map[string]string{protocol.Chat: "https://example.test/chat/completions"},
		Keys:      []store.CreateUpstreamKeyInput{{Name: "primary", Secret: "test-secret"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	providers, err := database.ListProviders()
	if err != nil {
		t.Fatal(err)
	}

	h := &Handler{store: database}
	route := store.ResolvedRoute{Provider: providers[0], Key: providers[0].Keys[0]}
	h.updateKeyAfterError(route, http.StatusPaymentRequired, []byte(`{"error":{"type":"unknown_error","code":"invalid_request_error"}}`), nil)
	providers, err = database.ListProviders()
	if err != nil {
		t.Fatal(err)
	}
	key := providers[0].Keys[0]
	if key.RuntimeStatus != "quota_exhausted" {
		t.Fatalf("unexpected key status: %q", key.RuntimeStatus)
	}
	if key.RuntimeReason != "invalid_request_error" {
		t.Fatalf("unexpected key reason: %q", key.RuntimeReason)
	}
}

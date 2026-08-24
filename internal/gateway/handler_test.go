package gateway

import (
	"bufio"
	"bytes"
	"context"
	"net/http"
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
}

func (w *recordingResponseWriter) Header() http.Header {
	return w.header
}

func (w *recordingResponseWriter) Write(body []byte) (int, error) {
	if !w.committed {
		w.WriteHeader(http.StatusOK)
	}
	return len(body), nil
}

func (w *recordingResponseWriter) WriteHeader(int) {
	w.committed = true
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
	var upstreamLog, clientLog bytes.Buffer
	var firstContent *int64

	status, message, committed := proxyConvertedEvents(w, context.Background(), bufio.NewReader(strings.NewReader(stream)), nil, converter, http.StatusOK, nil, time.Now(), &upstreamLog, &clientLog, &firstContent)

	if status != "failed" || message != `unsupported Messages stream block "image"` {
		t.Fatalf("unexpected result: status=%q message=%q", status, message)
	}
	if committed || w.committed {
		t.Fatal("response was committed before the conversion error")
	}
	if clientLog.Len() != 0 {
		t.Fatalf("client received partial stream: %s", clientLog.Bytes())
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
	h.candidateFailed(7)
	h.candidateFailed(7)
	h.candidateFailed(7)
	if h.candidateReady(7) {
		t.Fatal("candidate should be cooling down")
	}
	h.candidateMu.Lock()
	state := h.candidates[7]
	state.cooldownUntil = time.Now().Add(-time.Second)
	h.candidates[7] = state
	h.candidateMu.Unlock()
	if !h.candidateReady(7) {
		t.Fatal("candidate should allow a probe after cooldown")
	}
	if h.candidateReady(7) {
		t.Fatal("candidate should allow only one concurrent probe")
	}
	h.candidateSucceeded(7)
	if !h.candidateReady(7) {
		t.Fatal("successful probe should reset candidate state")
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

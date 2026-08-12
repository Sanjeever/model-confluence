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

	status, message, committed := proxyConvertedEvents(w, context.Background(), bufio.NewReader(strings.NewReader(stream)), nil, converter, http.StatusOK, time.Now(), &upstreamLog, &clientLog, &firstContent)

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

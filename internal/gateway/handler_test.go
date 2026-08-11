package gateway

import (
	"bufio"
	"bytes"
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Sanjeever/model-confluence/internal/protocol"
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

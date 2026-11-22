package gateway

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Sanjeever/model-confluence/internal/config"
	"github.com/Sanjeever/model-confluence/internal/httpx"
	"github.com/Sanjeever/model-confluence/internal/protocol"
	"github.com/Sanjeever/model-confluence/internal/store"
)

type liveStreamRecorder struct {
	*httptest.ResponseRecorder
	sawMetadata bool
	waiting     chan struct{}
}

func (w *liveStreamRecorder) Write(p []byte) (int, error) {
	if bytes.Contains(p, []byte(`"role":"assistant"`)) {
		w.sawMetadata = true
	}
	if w.sawMetadata && w.waiting != nil && bytes.HasPrefix(p, []byte(": keep-alive")) {
		close(w.waiting)
		w.waiting = nil
	}
	return w.ResponseRecorder.Write(p)
}

func TestLiveRoutingKeepsRequestThroughFailover(t *testing.T) {
	t.Run("cancelled", func(t *testing.T) { testLiveRoutingFailover(t, "cancelled") })
	t.Run("failed", func(t *testing.T) { testLiveRoutingFailover(t, "failed") })
}

func testLiveRoutingFailover(t *testing.T, terminal string) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	t.Cleanup(primary.Close)

	sendContent := make(chan struct{})
	stopStream := make(chan struct{})
	backup := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		if terminal == "failed" {
			w.Header().Set("Content-Length", "8192")
		}
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"role\":\"assistant\",\"content\":null,\"tool_calls\":[]}}]}\n\n")
		w.(http.Flusher).Flush()
		select {
		case <-sendContent:
			_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"live\"}}]}\n\n")
			w.(http.Flusher).Flush()
		case <-r.Context().Done():
			return
		}
		select {
		case <-stopStream:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(backup.Close)

	candidates := make([]store.CreateCandidateInput, 0, 2)
	for _, item := range []struct{ name, url string }{{"primary", primary.URL}, {"backup", backup.URL}} {
		provider, err := db.CreateProvider(store.CreateProviderInput{
			Name: item.name, AuthType: "bearer", Endpoints: map[string]string{protocol.Chat: item.url + "/v1/chat/completions"},
			Keys: []store.CreateUpstreamKeyInput{{Secret: item.name + "-secret"}},
		})
		if err != nil {
			t.Fatal(err)
		}
		candidates = append(candidates, store.CreateCandidateInput{
			ProviderID: provider.ID, UpstreamModel: item.name + "-model",
			Protocols: []store.CandidateProtocol{{Protocol: protocol.Chat, SupportsStream: true}},
		})
	}
	if _, err := db.CreateVirtualModel(store.CreateVirtualModelInput{Name: "virtual", Candidates: candidates}); err != nil {
		t.Fatal(err)
	}
	key, err := db.CreateAccessKey("client", nil)
	if err != nil {
		t.Fatal(err)
	}

	resolver, _ := httpx.NewClientIPResolver(nil)
	handler := NewHandler(config.Config{MaxRequestBytes: 1 << 20, StreamHeartbeatInterval: 5 * time.Millisecond}, db, resolver)
	mux := http.NewServeMux()
	handler.Register(mux)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(
		`{"model":"virtual","stream":true,"messages":[{"role":"user","content":"private prompt"}]}`,
	)).WithContext(ctx)
	req.Header.Set("Authorization", "Bearer "+key.Secret)
	waitingForContent := make(chan struct{})
	recorder := &liveStreamRecorder{ResponseRecorder: httptest.NewRecorder(), waiting: waitingForContent}
	done := make(chan struct{})
	go func() {
		defer close(done)
		mux.ServeHTTP(recorder, req)
	}()
	t.Cleanup(func() { cancel(); <-done })

	select {
	case <-waitingForContent:
	case <-ctx.Done():
		t.Fatal("gateway did not forward metadata followed by a heartbeat")
	}
	snapshot := handler.LiveRouting()
	if len(snapshot.Active) != 1 {
		t.Fatalf("active requests: %+v", snapshot.Active)
	}
	waiting := snapshot.Active[0]
	if waiting.Stage != liveWaiting || waiting.Upstream.ProviderName != "backup" || waiting.Attempts != 2 ||
		waiting.AccessKeyID == nil || *waiting.AccessKeyID != key.ID {
		t.Fatalf("waiting request: %+v", waiting)
	}
	if !slices.ContainsFunc(waiting.Events, func(event LiveRouteEvent) bool {
		return event.Kind == liveAttemptFailed && event.Upstream != nil && event.Upstream.ProviderName == "primary" &&
			event.ResponseStatus == http.StatusTooManyRequests
	}) || !slices.ContainsFunc(waiting.Events, func(event LiveRouteEvent) bool { return event.Kind == liveRetrying }) {
		t.Fatalf("missing failover events: %+v", waiting.Events)
	}

	close(sendContent)
	for {
		snapshot = handler.LiveRouting()
		if len(snapshot.Active) == 1 && snapshot.Active[0].Stage == liveStreaming {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatalf("first content did not update the stage: %+v", snapshot)
		case <-time.After(time.Millisecond):
		}
	}
	if terminal == "cancelled" {
		cancel()
	} else {
		close(stopStream)
	}
	select {
	case <-done:
	case <-ctx.Done():
		if terminal != "cancelled" {
			t.Fatal("failed stream did not terminate")
		}
		<-done
	}
	snapshot = handler.LiveRouting()
	if len(snapshot.Active) != 0 || len(snapshot.Recent) != 1 ||
		snapshot.Recent[0].ID != waiting.ID || snapshot.Recent[0].Stage != terminal ||
		snapshot.Recent[0].Upstream.ProviderName != "backup" {
		t.Fatalf("%s request: %+v", terminal, snapshot)
	}
	if last := waiting.Events[len(waiting.Events)-1]; last.Kind != liveAttemptStarted {
		t.Fatalf("snapshot events changed after further writes: %+v", waiting.Events)
	}
}

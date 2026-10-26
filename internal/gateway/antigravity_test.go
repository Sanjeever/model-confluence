package gateway

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Sanjeever/model-confluence/internal/antigravity"
	"github.com/Sanjeever/model-confluence/internal/httpx"
	"github.com/Sanjeever/model-confluence/internal/protocol"
	"github.com/Sanjeever/model-confluence/internal/store"
)

func antigravityGateway(t *testing.T, server *httptest.Server) (*Handler, *store.Store, store.UpstreamKey) {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	p, err := s.CreateProvider(store.CreateProviderInput{Name: "agy", Kind: store.ProviderAntigravityOAuth})
	if err != nil {
		t.Fatal(err)
	}
	k, err := s.ImportAntigravityCredential(p.ID, "account.json", antigravity.Credential{Identity: antigravity.Identity{Subject: "user", Email: "test@example.com", ProjectID: "project"}, ClientID: "test-client", AccessToken: "test-access", RefreshToken: "test-refresh", ExpiresAt: time.Now().Add(time.Hour), LastRefresh: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.CreateVirtualModel(store.CreateVirtualModelInput{Name: "virtual", Candidates: []store.CreateCandidateInput{{ProviderID: p.ID, UpstreamModel: "real", DefaultMaxOutputTokens: 100, MaxOutputTokens: 1000, Protocols: []store.CandidateProtocol{{Protocol: protocol.Antigravity, SupportsStream: true, SupportsTools: true, SupportsParallelTools: true}}}}})
	if err != nil {
		t.Fatal(err)
	}
	resolver, _ := httpx.NewClientIPResolver(nil)
	target, _ := url.Parse(server.URL)
	transport := server.Client().Transport
	client := &http.Client{Transport: codexTestTransport(func(r *http.Request) (*http.Response, error) {
		copy := r.Clone(r.Context())
		u := *r.URL
		copy.URL = &u
		copy.URL.Host, copy.URL.Scheme = target.Host, target.Scheme
		return transport.RoundTrip(copy)
	})}
	return &Handler{store: s, client: client, clientIP: resolver, maxBody: 1 << 20, streamIdleTimeout: time.Second, antigravityOAuth: antigravity.OAuthClient{ID: "test-client", Secret: "test-secret"}}, s, k
}

func TestAntigravityGatewayProtocols(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-access" || r.Header.Get("Cookie") != "" {
			t.Error("incorrect upstream authentication")
		}
		body, _ := io.ReadAll(r.Body)
		if !bytes.Contains(body, []byte(`"project":"project"`)) || !bytes.Contains(body, []byte(`"model":"real"`)) {
			t.Error("missing upstream envelope")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: {\"response\":{\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"hello\"}]},\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"promptTokenCount\":2,\"candidatesTokenCount\":1,\"totalTokenCount\":3}}}\n\n")
	}))
	defer server.Close()
	h, s, _ := antigravityGateway(t, server)
	for _, source := range []string{protocol.Chat, protocol.Responses, protocol.Messages} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/%t", source, stream), func(t *testing.T) {
				body := fmt.Sprintf(`{"model":"virtual","messages":[{"role":"user","content":"hi"}],"stream":%t}`, stream)
				if source == protocol.Responses {
					body = fmt.Sprintf(`{"model":"virtual","input":"hi","stream":%t}`, stream)
				}
				r := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
				r.Header.Set("Cookie", "client-cookie")
				w := httptest.NewRecorder()
				h.generateAuthorized(w, r, source, nil, "test")
				if w.Code != 200 || !strings.Contains(w.Body.String(), "hello") || !strings.Contains(w.Body.String(), "virtual") {
					t.Fatalf("%d %s", w.Code, w.Body.String())
				}
				detail, err := s.RequestDetail(w.Header().Get("X-Request-ID"))
				if err != nil {
					t.Fatal(err)
				}
				if len(detail.Attempts) != 1 || !detail.Attempts[0].UpstreamStream || detail.Attempts[0].UpstreamProtocol != protocol.Antigravity {
					t.Fatalf("incorrect attempts: %+v", detail.Attempts)
				}
			})
		}
	}
}

func TestAntigravityGatewayRefreshAndStreamFailure(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"access_token":"renewed-access","expires_in":3600}`)
			return
		}
		if calls.Add(1) == 1 {
			w.WriteHeader(401)
			io.WriteString(w, `{"error":{"code":401,"status":"UNAUTHENTICATED"}}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: {\"response\":{\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"partial\"}]}}]}}\n\n")
	}))
	defer server.Close()
	h, s, k := antigravityGateway(t, server)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"virtual","messages":[{"role":"user","content":"hi"}],"stream":true}`))
	h.generateAuthorized(w, r, protocol.Chat, nil, "test")
	if calls.Load() != 2 || !strings.Contains(w.Body.String(), "partial") || !strings.Contains(w.Body.String(), "error") {
		t.Fatalf("%d %s", calls.Load(), w.Body.String())
	}
	c, err := s.AntigravityCredential(k.ID)
	if err != nil || c.AccessToken != "renewed-access" || c.RefreshToken != "test-refresh" {
		t.Fatalf("refresh: %+v %v", c.Identity, err)
	}
}

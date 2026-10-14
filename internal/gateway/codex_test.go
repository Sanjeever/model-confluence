package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Sanjeever/model-confluence/internal/codex"
	"github.com/Sanjeever/model-confluence/internal/httpx"
	"github.com/Sanjeever/model-confluence/internal/protocol"
	"github.com/Sanjeever/model-confluence/internal/store"
	"github.com/golang-jwt/jwt/v5"
)

type codexTestTransport func(*http.Request) (*http.Response, error)

func (f codexTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func gatewayAuthFile(t *testing.T, user string, expires time.Time) []byte {
	t.Helper()
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"sub": user, "exp": expires.Unix(), "https://api.openai.com/auth": map[string]string{"chatgpt_account_id": "account"}}).SignedString([]byte("test"))
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(map[string]any{"tokens": codex.Tokens{IDToken: token, AccessToken: token, RefreshToken: "test-refresh", AccountID: "account"}})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func codexGateway(t *testing.T, server *httptest.Server, expired bool) (*Handler, *store.Store, store.UpstreamKey) {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	p, err := s.CreateProvider(store.CreateProviderInput{Name: "codex", Kind: store.ProviderCodexOAuth})
	if err != nil {
		t.Fatal(err)
	}
	exp := time.Now().Add(time.Hour)
	if expired {
		exp = time.Now().Add(-time.Hour)
	}
	k, err := s.ImportOAuthCredential(p.ID, "auth.json", gatewayAuthFile(t, "user", exp))
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.CreateVirtualModel(store.CreateVirtualModelInput{Name: "virtual", Candidates: []store.CreateCandidateInput{{ProviderID: p.ID, UpstreamModel: "real", Protocols: []store.CandidateProtocol{{Protocol: protocol.Responses, SupportsStream: true, SupportsTools: true, SupportsParallelTools: true}}}}})
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
		if u.Host == "chatgpt.com" {
			copy.URL.Scheme = target.Scheme
			copy.URL.Host = target.Host
			copy.URL.Path = "/responses"
		}
		return transport.RoundTrip(copy)
	})}
	h := &Handler{store: s, client: client, clientIP: resolver, maxBody: 1 << 20, streamIdleTimeout: time.Second, streamHeartbeatInterval: 5 * time.Millisecond, oauthTokenEndpoint: server.URL + "/oauth"}
	return h, s, k
}

func codexTextStream() string {
	return "data: " + `{"type":"response.created","response":{"id":"resp_test","model":"real"}}` + "\n\n" +
		"data: " + `{"type":"response.output_text.delta","output_index":0,"delta":"hello"}` + "\n\n" +
		"data: " + `{"type":"response.output_item.done","output_index":0,"item":{"type":"message","id":"msg_test","role":"assistant","content":[{"type":"output_text","text":"hello"}]}}` + "\n\n" +
		"data: " + `{"type":"response.completed","response":{"id":"resp_test","model":"real","status":"completed","output":[],"usage":{"input_tokens":10,"output_tokens":2,"total_tokens":12}}}` + "\n\n"
}

func TestCodexClientEntrypoints(t *testing.T) {
	for _, source := range []string{protocol.Chat, protocol.Responses, protocol.Messages} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stream=%t", source, stream), func(t *testing.T) {
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					var payload map[string]any
					if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
						t.Error(err)
					}
					if payload["stream"] != true || payload["store"] != false || payload["model"] != "real" || payload["max_tokens"] != nil || payload["temperature"] != nil {
						t.Errorf("bad upstream body: %v", payload)
					}
					if r.Header.Get("ChatGPT-Account-ID") != "account" || r.Header.Get("Accept") != "text/event-stream" {
						t.Error("bad OAuth headers")
					}
					w.Header().Set("Content-Type", "text/event-stream")
					io.WriteString(w, codexTextStream())
				}))
				defer srv.Close()
				h, s, _ := codexGateway(t, srv, false)
				var body string
				switch source {
				case protocol.Responses:
					body = fmt.Sprintf(`{"model":"virtual","input":"hi","stream":%t,"max_output_tokens":20,"temperature":0.5}`, stream)
				case protocol.Messages:
					body = fmt.Sprintf(`{"model":"virtual","messages":[{"role":"user","content":"hi"}],"stream":%t,"max_tokens":20,"temperature":0.5}`, stream)
				default:
					body = fmt.Sprintf(`{"model":"virtual","messages":[{"role":"user","content":"hi"}],"stream":%t,"max_tokens":20,"temperature":0.5}`, stream)
				}
				r := httptest.NewRequest("POST", "/test", strings.NewReader(body))
				r.Header.Set("ChatGPT-Account-ID", "untrusted-client-account")
				w := httptest.NewRecorder()
				h.generateAuthorized(w, r, source, nil, "test")
				if w.Code != 200 || !strings.Contains(w.Body.String(), "hello") || strings.Contains(w.Body.String(), `"model":"real"`) {
					t.Fatalf("client: %d %s", w.Code, w.Body)
				}
				if !stream && !json.Valid(w.Body.Bytes()) {
					t.Fatal("buffered client received SSE")
				}
				d, err := s.RequestDetail(w.Header().Get("X-Request-ID"))
				if err != nil {
					t.Fatal(err)
				}
				if d.Status != "completed" || d.Stream != stream || len(d.Attempts) != 1 || !d.Attempts[0].UpstreamStream || len(d.Attempts[0].IgnoredParameters) != 2 || d.InputTokens == nil || *d.InputTokens != 10 || d.FirstContentMS == nil {
					t.Fatalf("bad logs: %+v", d)
				}
				if !strings.Contains(d.RequestBody, "temperature") || !strings.Contains(d.Attempts[0].ResponseBody, "response.completed") {
					t.Fatal("raw payload lost")
				}
			})
		}
	}
}

func TestCodexConcurrentRefresh(t *testing.T) {
	var refreshes atomic.Int32
	nextData := gatewayAuthFile(t, "user", time.Now().Add(time.Hour))
	next, _ := codex.ParseAuth(nextData)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		refreshes.Add(1)
		time.Sleep(30 * time.Millisecond)
		json.NewEncoder(w).Encode(next.Tokens)
	}))
	defer srv.Close()
	h, s, key := codexGateway(t, srv, true)
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, _, err := h.codexCredential(context.Background(), key.ID, 0)
			if err != nil {
				t.Error(err)
			} else if c.Auth.ExpiresAt.Before(time.Now()) {
				t.Error("expired token used")
			}
		}()
	}
	wg.Wait()
	if refreshes.Load() != 1 {
		t.Fatalf("refresh count: %d", refreshes.Load())
	}
	c, err := s.OAuthCredential(key.ID)
	if err != nil || c.Revision != 2 {
		t.Fatalf("persisted credential: %v %+v", err, c)
	}
}

func TestCodex401RetryAttempts(t *testing.T) {
	for _, continuous := range []bool{false, true} {
		t.Run(fmt.Sprintf("continuous=%t", continuous), func(t *testing.T) {
			var calls, refreshes atomic.Int32
			nextData := gatewayAuthFile(t, "user", time.Now().Add(2*time.Hour))
			next, _ := codex.ParseAuth(nextData)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/oauth" {
					refreshes.Add(1)
					json.NewEncoder(w).Encode(next.Tokens)
					return
				}
				if calls.Add(1) == 1 || continuous {
					w.WriteHeader(401)
					io.WriteString(w, `{"error":{"code":"unauthorized"}}`)
					return
				}
				io.WriteString(w, codexTextStream())
			}))
			defer srv.Close()
			h, s, key := codexGateway(t, srv, false)
			status, _, id := h.TestModel(context.Background(), "virtual", "hi")
			if refreshes.Load() != 1 || calls.Load() != 2 {
				t.Fatalf("unbounded retry: refresh=%d calls=%d", refreshes.Load(), calls.Load())
			}
			d, err := s.RequestDetail(id)
			if err != nil {
				t.Fatal(err)
			}
			if len(d.Attempts) != 2 {
				t.Fatalf("attempts=%d", len(d.Attempts))
			}
			_, err = s.OAuthCredential(key.ID)
			if continuous {
				if status != 503 || !errors.Is(err, store.ErrCredentialChanged) {
					t.Fatalf("continuous 401: %d %v", status, err)
				}
			} else if status != 200 || err != nil {
				t.Fatalf("retry failed: %d %v", status, err)
			}
		})
	}
}

func TestCodexCommitBoundaryAndErrors(t *testing.T) {
	for _, afterContent := range []bool{false, true} {
		t.Run(fmt.Sprintf("committed=%t", afterContent), func(t *testing.T) {
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if calls.Add(1) > 1 {
					io.WriteString(w, codexTextStream())
					return
				}
				io.WriteString(w, "data: "+`{"type":"response.created","response":{"id":"resp_test","model":"real"}}`+"\n\n")
				if afterContent {
					io.WriteString(w, "data: "+`{"type":"response.output_text.delta","output_index":0,"delta":"partial"}`+"\n\n")
				}
				io.WriteString(w, "data: "+`{"type":"response.failed","response":{"status":"failed","error":{"type":"usage_limit_reached","resets_in_seconds":60}}}`+"\n\n")
			}))
			defer srv.Close()
			h, s, key := codexGateway(t, srv, false)
			p, err := s.ListProviders()
			if err != nil {
				t.Fatal(err)
			}
			if _, err := s.ImportOAuthCredential(p[0].ID, "second.json", gatewayAuthFile(t, "second-user", time.Now().Add(time.Hour))); err != nil {
				t.Fatal(err)
			}
			w := httptest.NewRecorder()
			r := httptest.NewRequest("POST", "/v1/responses", strings.NewReader(`{"model":"virtual","input":"hi","stream":true}`))
			h.generateAuthorized(w, r, protocol.Responses, nil, "test")
			d, err := s.RequestDetail(w.Header().Get("X-Request-ID"))
			if err != nil {
				t.Fatal(err)
			}
			if afterContent {
				if calls.Load() != 1 || d.Status != "failed" || !strings.Contains(w.Body.String(), "partial") || strings.Contains(w.Body.String(), "hello") {
					t.Fatalf("replayed committed stream: %d %s", calls.Load(), w.Body)
				}
			} else if calls.Load() != 2 || d.Status != "completed" || strings.Contains(w.Body.String(), "usage_limit_reached") {
				t.Fatalf("precontent fallback failed: %d %s", calls.Load(), w.Body)
			}
			p, err = s.ListProviders()
			if err != nil {
				t.Fatal(err)
			}
			for _, k := range p[0].Keys {
				if k.ID == key.ID && (k.RuntimeStatus != "quota_exhausted" || k.RecoverAt == nil) {
					t.Fatalf("quota reset missing: %+v", k)
				}
			}
		})
	}
}

func TestCodexRefreshConfigurationConflict(t *testing.T) {
	for _, action := range []string{"delete", "disable", "provider_disable"} {
		t.Run(action, func(t *testing.T) {
			entered, release := make(chan struct{}), make(chan struct{})
			nextData := gatewayAuthFile(t, "user", time.Now().Add(time.Hour))
			next, _ := codex.ParseAuth(nextData)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				close(entered)
				<-release
				json.NewEncoder(w).Encode(next.Tokens)
			}))
			defer srv.Close()
			h, s, key := codexGateway(t, srv, true)
			result := make(chan error, 1)
			go func() { _, _, err := h.codexCredential(context.Background(), key.ID, 0); result <- err }()
			<-entered
			var err error
			switch action {
			case "delete":
				err = s.DeleteOAuthCredential(1, key.ID)
			case "disable":
				err = s.SetUpstreamKeyEnabled(key.ID, false)
			default:
				err = s.SetProviderEnabled(1, false)
			}
			if err != nil {
				t.Fatal(err)
			}
			close(release)
			if err := <-result; !errors.Is(err, store.ErrCredentialChanged) {
				t.Fatalf("stale refresh accepted: %v", err)
			}
			p, err := s.ProviderByID(1)
			if err != nil {
				t.Fatal(err)
			}
			if action == "delete" && len(p.Keys) != 0 || action == "disable" && p.Keys[0].Enabled || action == "provider_disable" && p.Enabled {
				t.Fatal("refresh changed admin state")
			}
		})
	}
}

func TestCodexRefreshFailureStatus(t *testing.T) {
	for _, permanent := range []bool{false, true} {
		t.Run(fmt.Sprint(permanent), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if permanent {
					w.WriteHeader(400)
					io.WriteString(w, `{"error":"refresh_token_reused"}`)
				} else {
					w.WriteHeader(503)
					io.WriteString(w, `{"error":"temporary_failure"}`)
				}
			}))
			defer srv.Close()
			h, s, _ := codexGateway(t, srv, true)
			status, _, id := h.TestModel(context.Background(), "virtual", "hi")
			if status != 502 {
				t.Fatalf("refresh request status: %d", status)
			}
			d, err := s.RequestDetail(id)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(d.ErrorMessage, "OAuth") || len(d.Attempts) != 0 {
				t.Fatalf("refresh failure not visible: %+v", d)
			}
			p, err := s.ProviderByID(1)
			if err != nil {
				t.Fatal(err)
			}
			if (p.Keys[0].RuntimeStatus == "auth_invalid") != permanent || p.Keys[0].RuntimeReason == "" {
				t.Fatalf("bad runtime status: %+v", p.Keys[0])
			}
			if permanent && s.ResetUpstreamKeyRuntime(p.Keys[0].ID) == nil {
				t.Fatal("reset repaired invalid auth")
			}
		})
	}
}

func TestCodexFallbackPreservesGenericParameters(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/responses" {
			w.WriteHeader(500)
			return
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		if payload["max_tokens"] != float64(256) || payload["temperature"] != 0.5 {
			t.Errorf("generic parameters changed: %v", payload)
		}
		io.WriteString(w, `{"id":"chat_test","model":"generic-real","choices":[{"message":{"role":"assistant","content":"generic hello"},"finish_reason":"stop"}]}`)
	}))
	defer srv.Close()
	h, s, _ := codexGateway(t, srv, false)
	p, err := s.CreateProvider(store.CreateProviderInput{Name: "generic", AuthType: "bearer", Endpoints: map[string]string{protocol.Chat: srv.URL + "/generic"}, Keys: []store.CreateUpstreamKeyInput{{Secret: "test"}}})
	if err != nil {
		t.Fatal(err)
	}
	models, err := s.ListVirtualModels()
	if err != nil {
		t.Fatal(err)
	}
	old := models[0].Candidates[0]
	if err := s.UpdateVirtualModel(models[0].ID, store.CreateVirtualModelInput{Name: "virtual", Candidates: []store.CreateCandidateInput{{ID: old.ID, ProviderID: old.ProviderID, UpstreamModel: old.UpstreamModel, Protocols: old.Protocols}, {ProviderID: p.ID, UpstreamModel: "generic-real", Protocols: []store.CandidateProtocol{{Protocol: protocol.Chat}}}}}); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"virtual","messages":[{"role":"user","content":"hi"}],"max_tokens":256,"temperature":0.5}`))
	h.generateAuthorized(w, r, protocol.Chat, nil, "test")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "generic hello") {
		t.Fatalf("fallback failed: %d %s", w.Code, w.Body)
	}
	d, err := s.RequestDetail(w.Header().Get("X-Request-ID"))
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Attempts) != 2 || len(d.Attempts[1].IgnoredParameters) != 0 || d.Attempts[1].UpstreamStream {
		t.Fatal("Codex exception affected generic route")
	}
}

func TestCodexToolNextRound(t *testing.T) {
	for _, source := range []string{protocol.Chat, protocol.Responses, protocol.Messages} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stream=%t", source, stream), func(t *testing.T) {
				var calls atomic.Int32
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					body, err := io.ReadAll(r.Body)
					if err != nil {
						t.Error(err)
					}
					if calls.Add(1) > 1 {
						if !strings.Contains(string(body), "function_call_output") || !strings.Contains(string(body), "tool-result") {
							t.Errorf("next-round tool history missing: %s", body)
						}
						io.WriteString(w, codexTextStream())
						return
					}
					w.Header().Set("Content-Type", "text/event-stream")
					for _, event := range []string{
						`{"type":"response.created","response":{"id":"resp_tool","model":"real"}}`,
						`{"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","id":"fc_test","call_id":"call_test","name":"lookup","arguments":""}}`,
						`{"type":"response.function_call_arguments.delta","output_index":0,"delta":"{}"}`,
						`{"type":"response.output_item.done","output_index":0,"item":{"type":"function_call","id":"fc_test","call_id":"call_test","name":"lookup","arguments":"{}"}}`,
						`{"type":"response.completed","response":{"id":"resp_tool","model":"real","status":"completed","output":[]}}`,
					} {
						io.WriteString(w, "data: "+event+"\n\n")
					}
				}))
				defer srv.Close()
				h, _, _ := codexGateway(t, srv, false)
				var first, next string
				switch source {
				case protocol.Responses:
					first = `{"model":"virtual","input":"hi","tools":[{"type":"function","name":"lookup","parameters":{"type":"object"}}]}`
					next = `{"model":"virtual","input":[{"type":"function_call","call_id":"call_test","name":"lookup","arguments":"{}"},{"type":"function_call_output","call_id":"call_test","output":"tool-result"}]}`
				case protocol.Messages:
					first = `{"model":"virtual","messages":[{"role":"user","content":"hi"}],"max_tokens":256,"tools":[{"name":"lookup","input_schema":{"type":"object"}}]}`
					next = `{"model":"virtual","messages":[{"role":"assistant","content":[{"type":"tool_use","id":"call_test","name":"lookup","input":{}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"call_test","content":"tool-result"}]}],"max_tokens":256}`
				default:
					first = `{"model":"virtual","messages":[{"role":"user","content":"hi"}],"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object"}}}]}`
					next = `{"model":"virtual","messages":[{"role":"assistant","tool_calls":[{"id":"call_test","type":"function","function":{"name":"lookup","arguments":"{}"}}]},{"role":"tool","tool_call_id":"call_test","content":"tool-result"}]}`
				}
				for index, body := range []string{first, next} {
					var payload map[string]any
					if err := json.Unmarshal([]byte(body), &payload); err != nil {
						t.Fatal(err)
					}
					payload["stream"] = stream
					data, _ := json.Marshal(payload)
					w := httptest.NewRecorder()
					r := httptest.NewRequest("POST", "/test", strings.NewReader(string(data)))
					h.generateAuthorized(w, r, source, nil, "test")
					if w.Code != 200 || index == 0 && (!strings.Contains(w.Body.String(), "call_test") || !strings.Contains(w.Body.String(), "lookup")) || index == 1 && !strings.Contains(w.Body.String(), "hello") {
						t.Fatalf("round %d: %d %s", index, w.Code, w.Body)
					}
				}
			})
		}
	}
}

func TestCodexUnexpectedEOFAndCancellation(t *testing.T) {
	for _, cancelClient := range []bool{false, true} {
		t.Run(fmt.Sprintf("cancel=%t", cancelClient), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				io.WriteString(w, "data: "+`{"type":"response.created","response":{"id":"resp_test","model":"real"}}`+"\n\n")
				w.(http.Flusher).Flush()
				if cancelClient {
					<-r.Context().Done()
				}
			}))
			defer srv.Close()
			h, s, _ := codexGateway(t, srv, false)
			ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
			defer cancel()
			status, _, id := h.TestModel(ctx, "virtual", "hi")
			d, err := s.RequestDetail(id)
			if err != nil {
				t.Fatal(err)
			}
			if len(d.Attempts) != 1 || !strings.Contains(d.Attempts[0].ResponseBody, "response.created") {
				t.Fatal("partial payload lost")
			}
			if cancelClient {
				if d.Status != "cancelled" {
					t.Fatalf("cancel status: %s", d.Status)
				}
			} else if status != 502 || d.Status != "failed" {
				t.Fatalf("EOF status: %d %s", status, d.Status)
			}
		})
	}
}

func TestCodexCancellationWhileReadingHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		io.WriteString(w, `{"error":`)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer srv.Close()
	h, s, _ := codexGateway(t, srv, false)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, _, id := h.TestModel(ctx, "virtual", "hi")
	d, err := s.RequestDetail(id)
	if err != nil {
		t.Fatal(err)
	}
	if d.Status != "cancelled" || len(d.Attempts) != 1 || d.Attempts[0].Status != "cancelled" || d.Attempts[0].ResponseBody != `{"error":` {
		t.Fatalf("cancelled error response not recorded: %+v", d)
	}
}

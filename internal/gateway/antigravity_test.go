package gateway

import (
	"bytes"
	"encoding/json"
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

func TestAntigravitySignedToolRoundTrip(t *testing.T) {
	for _, source := range []string{protocol.Chat, protocol.Responses, protocol.Messages} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/%t", source, stream), func(t *testing.T) {
				var calls atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					body, _ := io.ReadAll(r.Body)
					step := calls.Add(1)
					if step > 1 {
						var envelope struct {
							Request struct {
								Contents []struct {
									Role  string
									Parts []json.RawMessage
								}
							}
						}
						if err := json.Unmarshal(body, &envelope); err != nil {
							t.Error(err)
						}
						contents := envelope.Request.Contents
						for round := 0; round < int(step)-1; round++ {
							model := contents[1+round*2]
							if model.Role != "model" || len(model.Parts) != 3 || !bytes.Contains(model.Parts[0], []byte(`"thought":true`)) || !bytes.Contains(model.Parts[1], []byte(fmt.Sprintf(`"thoughtSignature":"signature-%d"`, round+1))) || bytes.Contains(model.Parts[2], []byte("thoughtSignature")) {
								t.Errorf("original signed parts not restored: %s", body)
							}
							results := contents[2+round*2]
							if len(results.Parts) != 2 || !bytes.Contains(results.Parts[0], []byte(`"id":"upstream-a"`)) || bytes.Contains(results.Parts[1], []byte(`"id"`)) {
								t.Errorf("incorrect parallel results: %s", body)
							}
						}
						if bytes.Contains(body, []byte("call_ag_")) {
							t.Error("gateway IDs leaked upstream")
						}
					}
					w.Header().Set("Content-Type", "text/event-stream")
					if step == 3 {
						io.WriteString(w, "data: {\"response\":{\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"finished\"}]},\"finishReason\":\"STOP\"}]}}\n\n")
						return
					}
					io.WriteString(w, "data: {\"response\":{\"candidates\":[{\"content\":{\"parts\":[{\"thought\":true,\"text\":\"\"}]}}]}}\n\n")
					fmt.Fprintf(w, "data: {\"response\":{\"candidates\":[{\"content\":{\"parts\":[{\"thoughtSignature\":\"signature-%d\",\"functionCall\":{\"id\":\"upstream-a\",\"name\":\"lookup\",\"args\":{\"n\":1}}},{\"functionCall\":{\"name\":\"lookup\",\"args\":{\"n\":2}}}]}}]}}\n\n", step)
					io.WriteString(w, "data: {\"response\":{\"candidates\":[{\"finishReason\":\"STOP\"}]}}\n\n")
				}))
				defer server.Close()
				h, s, _ := antigravityGateway(t, server)
				request := protocol.Request{Model: "virtual", Stream: stream, ParallelToolCalls: true, Messages: []protocol.Message{{Role: "user", Blocks: []protocol.Block{{Type: "text", Text: "test"}}}}, Tools: []protocol.Tool{{Kind: "function", Name: "lookup", Parameters: json.RawMessage(`{"type":"object","properties":{"n":{"type":"integer"}}}`)}}}
				for round := 0; round < 3; round++ {
					body, err := protocol.EncodeRequest(request, source)
					if err != nil {
						t.Fatal(err)
					}
					w := httptest.NewRecorder()
					h.generateAuthorized(w, httptest.NewRequest(http.MethodPost, "/test", bytes.NewReader(body)), source, nil, "test")
					if w.Code != 200 {
						t.Fatalf("round %d: %d %s", round, w.Code, w.Body.String())
					}
					if round == 2 {
						if !strings.Contains(w.Body.String(), "finished") {
							t.Fatal(w.Body.String())
						}
						break
					}
					var blocks []protocol.Block
					if stream {
						summary := protocol.SummarizeStream(w.Body.Bytes(), source)
						if !summary.Completed {
							t.Fatalf("incomplete: %+v", summary)
						}
						for _, block := range summary.Blocks {
							if block.Type == "tool_call" {
								blocks = append(blocks, protocol.Block{Type: "tool_call", ToolCall: &protocol.ToolCall{ID: block.CallID, Name: block.Name, Arguments: json.RawMessage(block.Arguments)}})
							}
						}
					} else {
						response, err := protocol.DecodeResponse(w.Body.Bytes(), source)
						if err != nil {
							t.Fatal(err)
						}
						blocks = response.Blocks
					}
					if len(blocks) != 2 {
						t.Fatalf("expected parallel calls: %s", w.Body.String())
					}
					request.Messages = append(request.Messages, protocol.Message{Role: "assistant", Blocks: blocks})
					for _, block := range blocks {
						if !strings.HasPrefix(block.ToolCall.ID, "call_ag_") {
							t.Fatal("missing gateway ID")
						}
						if _, err := s.AntigravityToolHistory(nil, block.ToolCall.ID); err != nil {
							t.Fatalf("tool emitted without persistence: %v", err)
						}
						request.Messages = append(request.Messages, protocol.Message{Role: "user", Blocks: []protocol.Block{{Type: "tool_result", ToolResult: &protocol.ToolResult{CallID: block.ToolCall.ID, Content: "ok"}}}})
					}
				}
				if calls.Load() != 3 {
					t.Fatal(calls.Load())
				}
			})
		}
	}
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

func TestAntigravityToolHistoryRejectsInvalidContinuation(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: {\"response\":{\"candidates\":[{\"content\":{\"parts\":[{\"functionCall\":{\"id\":\"up\",\"name\":\"lookup\",\"args\":{\"n\":9007199254740992}},\"thoughtSignature\":\"signed\"}]},\"finishReason\":\"STOP\"}]}}\n\n")
	}))
	defer server.Close()
	h, s, _ := antigravityGateway(t, server)
	owner, err := s.CreateAccessKey("owner", nil)
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.CreateAccessKey("other", nil)
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	h.generateAuthorized(w, httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(`{"model":"virtual","messages":[{"role":"user","content":"test"}]}`)), protocol.Chat, &owner.ID, "owner")
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	response, err := protocol.DecodeResponse(w.Body.Bytes(), protocol.Chat)
	if err != nil {
		t.Fatal(err)
	}
	id := response.Blocks[0].ToolCall.ID
	body := fmt.Sprintf(`{"model":"virtual","messages":[{"role":"user","content":"test"},{"role":"assistant","content":"","tool_calls":[{"id":%q,"type":"function","function":{"name":"lookup","arguments":"{\"n\":9007199254740992}"}}]},{"role":"tool","tool_call_id":%q,"content":"ok"}]}`, id, id)
	for _, test := range []struct {
		name, body string
		key        *int64
	}{
		{"unknown", strings.ReplaceAll(body, id, "call_ag_missing"), &owner.ID},
		{"wrong_key", body, &other.ID},
		{"changed_arguments", strings.ReplaceAll(body, "9007199254740992", "9007199254740993"), &owner.ID},
		{"changed_text", strings.Replace(body, `"content":"","tool_calls"`, `"content":"changed","tool_calls"`, 1), &owner.ID},
		{"orphan_result", fmt.Sprintf(`{"model":"virtual","messages":[{"role":"user","content":"test"},{"role":"tool","tool_call_id":%q,"content":"ok"}]}`, id), &owner.ID},
	} {
		t.Run(test.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			h.generateAuthorized(w, httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(test.body)), protocol.Chat, test.key, "test")
			if w.Code != 400 || calls.Load() != 1 {
				t.Fatalf("invalid history reached upstream: %d %s calls=%d", w.Code, w.Body.String(), calls.Load())
			}
		})
	}
	history, err := s.AntigravityToolHistory(&owner.ID, id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateVirtualModel(store.CreateVirtualModelInput{Name: "other-model", Candidates: []store.CreateCandidateInput{{ProviderID: history.ProviderID, UpstreamModel: "different", DefaultMaxOutputTokens: 100, MaxOutputTokens: 1000, Protocols: []store.CandidateProtocol{{Protocol: protocol.Antigravity, SupportsTools: true, SupportsStream: true}}}}}); err != nil {
		t.Fatal(err)
	}
	w = httptest.NewRecorder()
	h.generateAuthorized(w, httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(strings.Replace(body, `"model":"virtual"`, `"model":"other-model"`, 1))), protocol.Chat, &owner.ID, "owner")
	if w.Code != 400 || calls.Load() != 1 {
		t.Fatalf("incompatible model reached upstream: %d %s", w.Code, w.Body.String())
	}
}

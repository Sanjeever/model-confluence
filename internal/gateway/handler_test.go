package gateway

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Sanjeever/model-confluence/internal/httpx"
	"github.com/Sanjeever/model-confluence/internal/protocol"
	"github.com/Sanjeever/model-confluence/internal/store"
)

func TestRewriteGroqReasoning(t *testing.T) {
	for _, tc := range []struct {
		name     string
		endpoint string
		body     string
		want     string
	}{
		{"rename", "https://api.groq.com/openai/v1/chat/completions", `{"messages":[{"role":"assistant","reasoning_content":"分析","content":"回答","vendor_field":9007199254740993}],"vendor_option":true}`, `{"messages":[{"role":"assistant","reasoning":"分析","content":"回答","vendor_field":9007199254740993}],"vendor_option":true}`},
		{"already reasoning", "https://api.groq.com/openai/v1/chat/completions", `{"messages":[{"role":"assistant","reasoning":"原值"}]}`, ""},
		{"both fields", "https://api.groq.com/openai/v1/chat/completions", `{"messages":[{"role":"assistant","reasoning":"原值","reasoning_content":"旧值"}]}`, `{"messages":[{"role":"assistant","reasoning":"原值"}]}`},
		{"existing null", "https://api.groq.com/openai/v1/chat/completions", `{"messages":[{"role":"assistant","reasoning":null,"reasoning_content":"旧值"}]}`, `{"messages":[{"role":"assistant","reasoning":null}]}`},
		{"null source", "https://api.groq.com/openai/v1/chat/completions", `{"messages":[{"role":"assistant","reasoning_content":null}]}`, `{"messages":[{"role":"assistant","reasoning":null}]}`},
		{"other role", "https://api.groq.com/openai/v1/chat/completions", `{"messages":[{"role":"user","content":"问题","reasoning_content":"扩展"}]}`, ""},
		{"no reasoning", "https://api.groq.com/openai/v1/chat/completions", `{"messages":[{"role":"user","content":"问题"}]}`, ""},
		{"other provider", "https://example.com/v1/chat/completions", `{"messages":[{"role":"assistant","reasoning_content":"保留"}]}`, ""},
		{"lookalike host", "https://api.groq.com.example.com/v1/chat/completions", `{"messages":[{"role":"assistant","reasoning_content":"保留"}]}`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := []byte(tc.body)
			got, err := rewriteGroqReasoning(body, tc.endpoint)
			if err != nil {
				t.Fatal(err)
			}
			if string(body) != tc.body {
				t.Fatal("inbound body was modified")
			}
			if tc.want == "" {
				if string(got) != tc.body {
					t.Fatalf("unchanged request was rewritten: %s", got)
				}
				return
			}
			var actual, want any
			decoder := json.NewDecoder(bytes.NewReader(got))
			decoder.UseNumber()
			if err := decoder.Decode(&actual); err != nil {
				t.Fatal(err)
			}
			decoder = json.NewDecoder(strings.NewReader(tc.want))
			decoder.UseNumber()
			if err := decoder.Decode(&want); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(actual, want) {
				t.Fatalf("upstream body=%s, want %s", got, tc.want)
			}
		})
	}
}

func TestGroqReasoningReachesUpstream(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprintf("stream=%t", stream), func(t *testing.T) {
			received := make(chan []byte, 1)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				received <- body
				if stream {
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = io.WriteString(w, "data: {\"id\":\"chat_test\",\"model\":\"real\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"},\"finish_reason\":null}]}\n\ndata: {\"id\":\"chat_test\",\"model\":\"real\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
				} else {
					w.Header().Set("Content-Type", "application/json")
					_, _ = io.WriteString(w, `{"id":"chat_test","model":"real","choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`)
				}
			}))
			defer upstream.Close()
			database, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer database.Close()
			provider, err := database.CreateProvider(store.CreateProviderInput{
				Name: "custom-name", AuthType: "bearer", Endpoints: map[string]string{protocol.Chat: "https://api.groq.com/openai/v1/chat/completions"},
				Keys: []store.CreateUpstreamKeyInput{{Secret: "test-secret"}},
			})
			if err != nil {
				t.Fatal(err)
			}
			_, err = database.CreateVirtualModel(store.CreateVirtualModelInput{
				Name: "virtual", Candidates: []store.CreateCandidateInput{{
					ProviderID: provider.ID, UpstreamModel: "real",
					Protocols: []store.CandidateProtocol{{Protocol: protocol.Chat, SupportsStream: true, SupportsTools: true}},
				}},
			})
			if err != nil {
				t.Fatal(err)
			}
			resolver, err := httpx.NewClientIPResolver(nil)
			if err != nil {
				t.Fatal(err)
			}
			target, err := url.Parse(upstream.URL)
			if err != nil {
				t.Fatal(err)
			}
			transport := upstream.Client().Transport
			client := &http.Client{Transport: codexTestTransport(func(r *http.Request) (*http.Response, error) {
				copy := r.Clone(r.Context())
				copy.URL.Scheme, copy.URL.Host = target.Scheme, target.Host
				return transport.RoundTrip(copy)
			})}
			h := &Handler{store: database, client: client, clientIP: resolver, maxBody: 1 << 20}
			body := fmt.Sprintf(`{"model":"virtual","stream":%t,"messages":[{"role":"user","content":"问题"},{"role":"assistant","content":null,"reasoning_content":"分析","tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{}"}}]},{"role":"tool","tool_call_id":"call_1","content":"结果"}]}`, stream)
			r := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
			w := httptest.NewRecorder()
			h.generateAuthorized(w, r, protocol.Chat, nil, "test")
			if w.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
			upstreamBody := <-received
			var actual struct {
				Messages []map[string]json.RawMessage `json:"messages"`
			}
			if err := json.Unmarshal(upstreamBody, &actual); err != nil {
				t.Fatal(err)
			}
			if len(actual.Messages) != 3 || string(actual.Messages[1]["reasoning"]) != `"分析"` || len(actual.Messages[1]["reasoning_content"]) != 0 {
				t.Fatalf("unexpected upstream messages: %s", upstreamBody)
			}
			if !bytes.Contains(actual.Messages[1]["tool_calls"], []byte(`"call_1"`)) || string(actual.Messages[2]["tool_call_id"]) != `"call_1"` {
				t.Fatalf("tool history changed: %s", upstreamBody)
			}
			detail, err := database.RequestDetail(w.Header().Get("X-Request-ID"))
			if err != nil {
				t.Fatal(err)
			}
			if detail.RequestBody != body || len(detail.Attempts) != 1 || detail.Attempts[0].RequestBody != string(upstreamBody) {
				t.Fatalf("request logging differs from inbound/upstream bodies: %+v", detail)
			}
		})
	}
}

func TestReasoningEffortReachesUpstream(t *testing.T) {
	requests := map[string]string{
		protocol.Chat:      `{"model":"virtual","messages":[{"role":"user","content":"hello"}],"reasoning_effort":%q}`,
		protocol.Responses: `{"model":"virtual","input":"hello","reasoning":{"effort":%q}}`,
		protocol.Messages:  `{"model":"virtual","messages":[{"role":"user","content":"hello"}],"max_tokens":100,"output_config":{"effort":%q}}`,
	}
	responses := map[string]string{
		protocol.Chat:      `{"id":"chat_test","model":"hy4","choices":[{"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}]}`,
		protocol.Responses: `{"id":"resp_test","model":"hy4","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hello"}]}]}`,
		protocol.Messages:  `{"id":"msg_test","model":"hy4","role":"assistant","content":[{"type":"text","text":"hello"}],"stop_reason":"end_turn"}`,
	}
	for _, target := range []string{protocol.Chat, protocol.Responses, protocol.Messages} {
		t.Run(target, func(t *testing.T) {
			received := make(chan requestMetadata, 1)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				metadata, err := inspectRequest(body, target)
				if err != nil {
					t.Error(err)
				}
				received <- metadata
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, responses[target])
			}))
			defer upstream.Close()
			database, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer database.Close()
			provider, err := database.CreateProvider(store.CreateProviderInput{
				Name: "test", AuthType: "bearer", Endpoints: map[string]string{target: upstream.URL},
				Keys: []store.CreateUpstreamKeyInput{{Secret: "test-secret"}},
			})
			if err != nil {
				t.Fatal(err)
			}
			_, err = database.CreateVirtualModel(store.CreateVirtualModelInput{
				Name: "virtual", Candidates: []store.CreateCandidateInput{{
					ProviderID: provider.ID, UpstreamModel: "hy4", DefaultMaxOutputTokens: 100, MaxOutputTokens: 1000,
					Protocols: []store.CandidateProtocol{{Protocol: target, EffortLevels: []string{"low", "medium", "high", "xhigh"}}},
				}},
			})
			if err != nil {
				t.Fatal(err)
			}
			resolver, err := httpx.NewClientIPResolver(nil)
			if err != nil {
				t.Fatal(err)
			}
			h := &Handler{store: database, client: upstream.Client(), clientIP: resolver, maxBody: 1 << 20}
			for _, source := range []string{protocol.Chat, protocol.Responses, protocol.Messages} {
				for _, effort := range []string{"none", "minimal", "low", "medium", "high", "xhigh", "max", "no_think", "default", "ultra", "persistent", "vendor_future"} {
					t.Run(source+"/"+effort, func(t *testing.T) {
						body := fmt.Sprintf(requests[source], effort)
						r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
						w := httptest.NewRecorder()
						h.generateAuthorized(w, r, source, nil, "test")
						if w.Code != http.StatusOK {
							t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
						}
						metadata := <-received
						if metadata.Model != "hy4" || metadata.Effort != effort {
							t.Fatalf("unexpected upstream request: %+v", metadata)
						}
						detail, err := database.RequestDetail(w.Header().Get("X-Request-ID"))
						if err != nil {
							t.Fatal(err)
						}
						if detail.ReasoningEffort != effort {
							t.Fatalf("logged effort=%q, want %q", detail.ReasoningEffort, effort)
						}
					})
				}
			}
		})
	}
}

func TestCopyRequestHeadersFiltersProxyHeaders(t *testing.T) {
	source := http.Header{
		"Forwarded":          {"for=192.0.2.1;proto=https;host=gateway.example"},
		"X-Forwarded-For":    {"192.0.2.1, 192.0.2.2"},
		"x-forwarded-host":   {"gateway.example"},
		"X-Forwarded-Proto":  {"https"},
		"X-Forwarded-Port":   {"443"},
		"x-ReAl-Ip":          {"192.0.2.2"},
		"Cf-Connecting-Ip":   {"192.0.2.1"},
		"Cf-Ipcountry":       {"CN"},
		"Cf-Ray":             {"test-ray"},
		"Cf-Visitor":         {`{"scheme":"https"}`},
		"cf-worker":          {"gateway.example"},
		"CDN-LOOP":           {"cloudflare; loops=1"},
		"Connection":         {"close"},
		"Content-Length":     {"123"},
		"Accept-Encoding":    {"gzip, br"},
		"Content-Type":       {"application/json"},
		"Authorization":      {"Bearer test-key"},
		"User-Agent":         {"test-client"},
		"Anthropic-Version":  {"2023-06-01"},
		"Openai-Beta":        {"responses=v1"},
		"X-Client-Extension": {"first", "second"},
	}
	original := source.Clone()
	destination := make(http.Header)

	copyRequestHeaders(destination, source)

	want := http.Header{
		"Content-Type":       {"application/json"},
		"Authorization":      {"Bearer test-key"},
		"User-Agent":         {"test-client"},
		"Anthropic-Version":  {"2023-06-01"},
		"Openai-Beta":        {"responses=v1"},
		"X-Client-Extension": {"first", "second"},
	}
	if !reflect.DeepEqual(destination, want) {
		t.Fatalf("unexpected upstream headers: got %v, want %v", destination, want)
	}
	if !reflect.DeepEqual(source, original) {
		t.Fatal("inbound headers were modified")
	}
}

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

func TestUpdateKeyAfterPaymentRequiredRequiresConfiguredQuotaCode(t *testing.T) {
	database, err := store.Open(filepath.Join(t.TempDir(), "model-confluence.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	_, err = database.CreateProvider(store.CreateProviderInput{
		Name:       "DeepSeek",
		AuthType:   "bearer",
		Endpoints:  map[string]string{protocol.Chat: "https://example.test/chat/completions"},
		QuotaCodes: []string{"insufficient_quota"},
		Keys:       []store.CreateUpstreamKeyInput{{Name: "primary", Secret: "test-secret"}},
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
	if key.RuntimeStatus != "available" {
		t.Fatalf("unconfigured 402 code changed key status: %q", key.RuntimeStatus)
	}

	h.updateKeyAfterError(route, http.StatusPaymentRequired, []byte(`{"error":{"type":"billing_error","code":"insufficient_quota"}}`), nil)
	providers, err = database.ListProviders()
	if err != nil {
		t.Fatal(err)
	}
	key = providers[0].Keys[0]
	if key.RuntimeStatus != "quota_exhausted" {
		t.Fatalf("unexpected key status: %q", key.RuntimeStatus)
	}
	if key.RuntimeReason != "insufficient_quota" {
		t.Fatalf("unexpected key reason: %q", key.RuntimeReason)
	}
}

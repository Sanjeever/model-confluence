package protocol

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestAntigravityRequestBoundaries(t *testing.T) {
	for _, test := range []struct {
		source, body string
		fail         bool
	}{
		{Chat, `{"model":"v","messages":[{"role":"user","content":"hello"}]}`, false},
		{Responses, `{"model":"v","input":"hello","store":false}`, false},
		{Messages, `{"model":"v","messages":[{"role":"user","content":"hello"}],"max_tokens":100}`, false},
		{Responses, `{"model":"v","input":"hello","previous_response_id":"old"}`, true},
		{Chat, `{"model":"v","messages":[{"role":"user","content":"hello"}],"unknown":true}`, true},
		{Messages, `{"model":"v","messages":[{"role":"assistant","content":[{"type":"thinking","thinking":"secret","signature":"x"}]}]}`, true},
		{Chat, `{"model":"v","messages":[{"role":"user","content":"hello"},{"role":"system","content":"late"}]}`, true},
	} {
		r, _, err := PrepareAntigravityRequest([]byte(test.body), test.source, "real", 100, 1000)
		if (err != nil) != test.fail {
			t.Fatalf("%s: %v", test.body, err)
		}
		if !test.fail {
			body, err := EncodeAntigravityRequest(r, "project", "req")
			if err != nil || !bytes.Contains(body, []byte(`"model":"real"`)) || !bytes.Contains(body, []byte(`"project":"project"`)) {
				t.Fatalf("encoded: %s %v", body, err)
			}
		}
	}
	request := Request{Model: "real", Messages: []Message{{Role: "assistant", Blocks: []Block{{Type: "tool_call", ToolCall: &ToolCall{ID: "call_test", Name: "lookup", Arguments: json.RawMessage(`{"q":"x"}`)}}}}, {Role: "user", Blocks: []Block{{Type: "tool_result", ToolResult: &ToolResult{CallID: "call_test", Content: "ok"}}}}}}
	body, err := EncodeAntigravityRequest(request, "project", "req")
	if err != nil || !bytes.Contains(body, []byte(`"functionResponse":{"id":"call_test","name":"lookup"`)) {
		t.Fatalf("tool history: %s %v", body, err)
	}
}

func TestAntigravityStreamConversion(t *testing.T) {
	for _, target := range []string{Chat, Responses, Messages} {
		c, err := NewStreamConverter(Antigravity, target, "virtual")
		if err != nil {
			t.Fatal(err)
		}
		var result bytes.Buffer
		for _, data := range []string{
			`{"response":{"responseId":"upstream","candidates":[{"content":{"parts":[{"text":"hello"}]}}]}}`,
			`{"response":{"candidates":[{"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":10,"cachedContentTokenCount":3,"candidatesTokenCount":2,"thoughtsTokenCount":4,"totalTokenCount":16}}}`,
		} {
			chunks, _, _, err := c.Convert(SSEEvent{Data: []byte(data)})
			if err != nil {
				t.Fatal(err)
			}
			for _, chunk := range chunks {
				result.Write(chunk)
			}
		}
		response, err := c.AntigravityResponse()
		if err != nil || *response.Usage.OutputTokens != 6 || *response.Usage.TotalTokens != 16 || !strings.Contains(result.String(), "virtual") || !strings.Contains(result.String(), "hello") {
			t.Fatalf("%s: %s %v", target, result.String(), err)
		}
	}
	c, _ := NewStreamConverter(Antigravity, Chat, "virtual")
	if _, _, _, err := c.Convert(SSEEvent{Data: []byte(`{"response":{"candidates":[{"content":{"parts":[{"functionCall":{"name":"tool","args":{}},"thoughtSignature":"protected"}]}}]}}`)}); err == nil {
		t.Fatal("signed call silently accepted")
	}
	c, _ = NewStreamConverter(Antigravity, Chat, "virtual")
	if _, _, _, err := c.Convert(SSEEvent{Data: []byte(`{"error":{"code":429,"status":"RESOURCE_EXHAUSTED","message":"quota"}}`)}); err == nil {
		t.Fatal("HTTP 200 error accepted")
	}
}

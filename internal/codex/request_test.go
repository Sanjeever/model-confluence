package codex

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/Sanjeever/model-confluence/internal/protocol"
)

func TestPrepareRequest(t *testing.T) {
	for _, tc := range []struct{ source, body string }{
		{protocol.Responses, `{"model":"virtual","input":[{"type":"reasoning","encrypted_content":"test-encrypted"},{"type":"function_call","call_id":"call_test","name":"lookup","arguments":"{}"},{"type":"function_call_output","call_id":"call_test","output":"ok"}],"tools":[{"type":"function","name":"lookup","parameters":{"type":"object"}}],"parallel_tool_calls":false,"tool_choice":"none","max_output_tokens":100,"temperature":0.5,"extension":{"value":true}}`},
		{protocol.Chat, `{"model":"virtual","messages":[{"role":"user","content":[{"type":"text","text":"hello"},{"type":"image_url","image_url":{"url":"https://example.test/image.png"}}]},{"role":"assistant","tool_calls":[{"id":"call_test","type":"function","function":{"name":"lookup","arguments":"{}"}}]},{"role":"tool","tool_call_id":"call_test","content":"ok"}],"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object"}}}],"max_tokens":100,"temperature":0.5}`},
		{protocol.Messages, `{"model":"virtual","messages":[{"role":"user","content":[{"type":"text","text":"hello"},{"type":"image","source":{"type":"base64","media_type":"image/png","data":"aGVsbG8="}}]},{"role":"assistant","content":[{"type":"tool_use","id":"call_test","name":"lookup","input":{}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"call_test","content":"ok"}]}],"tools":[{"name":"lookup","input_schema":{"type":"object"}}],"max_tokens":100,"temperature":0.5}`},
	} {
		t.Run(tc.source, func(t *testing.T) {
			data, ignored, err := PrepareRequest([]byte(tc.body), tc.source, "real-model")
			if err != nil {
				t.Fatal(err)
			}
			var raw map[string]json.RawMessage
			if err := json.Unmarshal(data, &raw); err != nil {
				t.Fatal(err)
			}
			if string(raw["stream"]) != "true" || string(raw["store"]) != "false" || string(raw["model"]) != `"real-model"` || string(raw["instructions"]) != `""` {
				t.Fatalf("bad adapted request: %s", data)
			}
			if len(ignored) != 2 || strings.Contains(string(data), "max_tokens") || strings.Contains(string(data), "temperature") {
				t.Fatalf("ignored: %v %s", ignored, data)
			}
			if !strings.Contains(string(data), "function_call_output") || !strings.Contains(string(data), "lookup") {
				t.Fatalf("tool history lost: %s", data)
			}
			if tc.source == protocol.Responses {
				if !reflect.DeepEqual(raw["extension"], json.RawMessage(`{"value":true}`)) || !strings.Contains(string(data), "test-encrypted") || string(raw["parallel_tool_calls"]) != "false" || string(raw["tool_choice"]) != `"none"` {
					t.Fatalf("Responses semantics changed: %s", data)
				}
			} else if !strings.Contains(string(data), "input_image") {
				t.Fatalf("image lost: %s", data)
			}
			if sourceFields := []string{"messages", "system", "stop_sequences", "output_config"}; tc.source != protocol.Responses {
				for _, field := range sourceFields {
					if _, ok := raw[field]; ok {
						t.Fatalf("source field %s leaked to Responses: %s", field, data)
					}
				}
			}
		})
	}
}

func TestPrepareRequestStateAndUnknownFields(t *testing.T) {
	for _, field := range []string{`"previous_response_id":"resp_test"`, `"conversation":{"id":"conv_test"}`, `"background":true`, `"store":true`} {
		if _, _, err := PrepareRequest([]byte(`{"input":"hello",`+field+`}`), protocol.Responses, "model"); err == nil {
			t.Fatalf("state accepted: %s", field)
		}
	}
	data, _, err := PrepareRequest([]byte(`{"input":"hello","store":false,"background":false,"unknown":true}`), protocol.Responses, "model")
	if err != nil || !strings.Contains(string(data), `"unknown":true`) {
		t.Fatalf("unknown Responses field lost: %s %v", data, err)
	}
}

func TestPrepareRequestLongToolCallID(t *testing.T) {
	const id = "dfac9e45-a328-46c6-aed1-bad21b078e3f:call_4c686ec701d84e73be48d5de03fd475d"
	for _, stream := range []string{"false", "true"} {
		t.Run("stream="+stream, func(t *testing.T) {
			body := []byte(`{"model":"virtual","stream":` + stream + `,"messages":[{"role":"assistant","tool_calls":[{"id":"` + id + `","type":"function","function":{"name":"read","arguments":"{}"}}]},{"role":"tool","tool_call_id":"` + id + `","content":"tool-result"}]}`)
			data, _, err := PrepareRequest(body, protocol.Chat, "real-model")
			if err != nil {
				t.Fatal(err)
			}
			var request struct {
				Model  string `json:"model"`
				Stream bool   `json:"stream"`
				Input  []struct {
					Type      string `json:"type"`
					CallID    string `json:"call_id"`
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
					Output    string `json:"output"`
				} `json:"input"`
			}
			if err := json.Unmarshal(data, &request); err != nil {
				t.Fatal(err)
			}
			if request.Model != "real-model" || !request.Stream || len(request.Input) != 2 {
				t.Fatalf("unexpected Codex request: %s", data)
			}
			call, result := request.Input[0], request.Input[1]
			if call.CallID == "" || len(call.CallID) > 64 || call.CallID != result.CallID {
				t.Fatalf("invalid Codex call IDs: %s", data)
			}
			if call.Type != "function_call" || call.Name != "read" || call.Arguments != "{}" || result.Type != "function_call_output" || result.Output != "tool-result" {
				t.Fatalf("tool history changed: %s", data)
			}
		})
	}
}

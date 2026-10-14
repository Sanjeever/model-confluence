package protocol

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"
)

const testImageDataURL = "data:image/png;base64,iVBORw0KGgo="

func TestConvertRequestResponsesToolCallIDs(t *testing.T) {
	for _, from := range []string{Chat, Messages} {
		for _, streaming := range []struct {
			name    string
			enabled bool
		}{{"non-stream", false}, {"stream", true}} {
			for _, tc := range []struct {
				name string
				ids  []string
			}{
				{"reported ID", []string{"dfac9e45-a328-46c6-aed1-bad21b078e3f:call_4c686ec701d84e73be48d5de03fd475d"}},
				{"64 characters", []string{strings.Repeat("a", 64)}},
				{"65 characters", []string{strings.Repeat("a", 65)}},
				{"64 Unicode characters", []string{strings.Repeat("中", 64)}},
				{"shared prefix", []string{strings.Repeat("a", 64) + "1", strings.Repeat("a", 64) + "2"}},
				{"reserved ID", []string{strings.Repeat("a", 65), "call_mc_1"}},
			} {
				t.Run(from+"/"+streaming.name+"/"+tc.name, func(t *testing.T) {
					calls := Message{Role: "assistant"}
					results := Message{Role: "user"}
					arguments := json.RawMessage(`{"path":"/builtin-skills/reports/SKILL.md"}`)
					for _, id := range tc.ids {
						calls.Blocks = append(calls.Blocks, Block{Type: "tool_call", ToolCall: &ToolCall{ID: id, Name: "read", Arguments: arguments}})
					}
					// 并行调用的结果可以按不同顺序返回。
					for i := len(tc.ids) - 1; i >= 0; i-- {
						id := tc.ids[i]
						results.Blocks = append(results.Blocks, Block{Type: "tool_result", ToolResult: &ToolResult{CallID: id, Content: "result_" + id}})
					}
					body, err := EncodeRequest(Request{Model: "source", Stream: streaming.enabled, Messages: []Message{calls, results}}, from)
					if err != nil {
						t.Fatal(err)
					}
					converted, err := ConvertRequest(body, from, Responses, "target", 1024)
					if err != nil {
						t.Fatal(err)
					}
					var response struct {
						Stream bool `json:"stream"`
						Input  []struct {
							Type      string `json:"type"`
							CallID    string `json:"call_id"`
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
							Output    string `json:"output"`
						} `json:"input"`
					}
					if err := json.Unmarshal(converted, &response); err != nil {
						t.Fatal(err)
					}
					if response.Stream != streaming.enabled || len(response.Input) != 2*len(tc.ids) {
						t.Fatalf("unexpected Responses request: %s", converted)
					}
					seen := make(map[string]bool)
					for i, id := range tc.ids {
						call := response.Input[i]
						result := response.Input[len(response.Input)-1-i]
						if call.Type != "function_call" || result.Type != "function_call_output" || call.CallID != result.CallID {
							t.Fatalf("tool call and result do not match: %s", converted)
						}
						if call.CallID == "" || utf8.RuneCountInString(call.CallID) > 64 || seen[call.CallID] {
							t.Fatalf("invalid or duplicate call ID %q", call.CallID)
						}
						seen[call.CallID] = true
						if utf8.RuneCountInString(id) <= 64 && call.CallID != id {
							t.Fatalf("short call ID changed: %q to %q", id, call.CallID)
						}
						if call.Name != "read" || call.Arguments != string(arguments) || result.Output != "result_"+id {
							t.Fatalf("tool content changed: %s", converted)
						}
					}
				})
			}
		}
	}
}

func TestConvertRequestImageContentOrder(t *testing.T) {
	body := []byte(`{"model":"source","messages":[{"role":"user","content":[{"type":"text","text":"before"},{"type":"image_url","image_url":{"url":"` + testImageDataURL + `"}},{"type":"text","text":"after"}]}]}`)

	converted, err := ConvertRequest(body, Chat, Responses, "target", 1024)
	if err != nil {
		t.Fatal(err)
	}
	var responses map[string]any
	if err := json.Unmarshal(converted, &responses); err != nil {
		t.Fatal(err)
	}
	input := responses["input"].([]any)
	content := input[0].(map[string]any)["content"].([]any)
	if got := content[0].(map[string]any)["type"]; got != "input_text" {
		t.Fatalf("unexpected first Responses block: %v", got)
	}
	if got := content[1].(map[string]any)["type"]; got != "input_image" {
		t.Fatalf("unexpected second Responses block: %v", got)
	}
	if got := content[2].(map[string]any)["type"]; got != "input_text" {
		t.Fatalf("unexpected third Responses block: %v", got)
	}
	if got := content[1].(map[string]any)["image_url"]; got != testImageDataURL {
		t.Fatalf("unexpected Responses image: %v", got)
	}

	converted, err = ConvertRequest(body, Chat, Messages, "target", 1024)
	if err != nil {
		t.Fatal(err)
	}
	var messages map[string]any
	if err := json.Unmarshal(converted, &messages); err != nil {
		t.Fatal(err)
	}
	messageContent := messages["messages"].([]any)[0].(map[string]any)["content"].([]any)
	if got := messageContent[0].(map[string]any)["type"]; got != "text" {
		t.Fatalf("unexpected first Messages block: %v", got)
	}
	if got := messageContent[1].(map[string]any)["type"]; got != "image" {
		t.Fatalf("unexpected second Messages block: %v", got)
	}
	if got := messageContent[2].(map[string]any)["type"]; got != "text" {
		t.Fatalf("unexpected third Messages block: %v", got)
	}
	source := messageContent[1].(map[string]any)["source"].(map[string]any)
	if source["type"] != "base64" || source["media_type"] != "image/png" || source["data"] != "iVBORw0KGgo=" {
		t.Fatalf("unexpected Messages image source: %v", source)
	}
}

func TestConvertRequestImagesAllDirections(t *testing.T) {
	tests := []struct {
		name string
		from string
		to   string
		body string
	}{
		{
			name: "responses to chat",
			from: Responses,
			to:   Chat,
			body: `{"model":"source","input":[{"role":"user","content":[{"type":"input_text","text":"describe"},{"type":"input_image","image_url":"https://example.com/image.png"}]}]}`,
		},
		{
			name: "responses to messages",
			from: Responses,
			to:   Messages,
			body: `{"model":"source","input":[{"role":"user","content":[{"type":"input_image","image_url":"https://example.com/image.png"}]}]}`,
		},
		{
			name: "messages to chat",
			from: Messages,
			to:   Chat,
			body: `{"model":"source","max_tokens":10,"messages":[{"role":"user","content":[{"type":"image","source":{"type":"base64","media_type":"image/jpeg","data":"aGVsbG8="}}]}]}`,
		},
		{
			name: "messages to responses",
			from: Messages,
			to:   Responses,
			body: `{"model":"source","max_tokens":10,"messages":[{"role":"user","content":[{"type":"image","source":{"type":"url","url":"https://example.com/image.webp"}}]}]}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			converted, err := ConvertRequest([]byte(test.body), test.from, test.to, "target", 1024)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(converted), "image") {
				t.Fatalf("converted request does not contain an image block: %s", converted)
			}
		})
	}
}

func TestConvertRequestRejectsUnsupportedImageInputs(t *testing.T) {
	tests := []struct {
		name string
		from string
		to   string
		body string
		want string
	}{
		{
			name: "Responses file id",
			from: Responses,
			to:   Messages,
			body: `{"model":"source","input":[{"role":"user","content":[{"type":"input_image","file_id":"file_123"}]}]}`,
			want: "file_id is not supported",
		},
		{
			name: "Messages file id",
			from: Messages,
			to:   Chat,
			body: `{"model":"source","max_tokens":10,"messages":[{"role":"user","content":[{"type":"image","source":{"type":"file","file_id":"file_123"}}]}]}`,
			want: "file_id is not supported",
		},
		{
			name: "detail high to Messages",
			from: Chat,
			to:   Messages,
			body: `{"model":"source","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"` + testImageDataURL + `","detail":"high"}}]}]}`,
			want: "cannot be represented",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ConvertRequest([]byte(test.body), test.from, test.to, "target", 1024)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

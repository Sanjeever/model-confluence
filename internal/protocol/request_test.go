package protocol

import (
	"encoding/json"
	"strings"
	"testing"
)

const testImageDataURL = "data:image/png;base64,iVBORw0KGgo="

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

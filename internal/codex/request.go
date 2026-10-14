package codex

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"

	"github.com/Sanjeever/model-confluence/internal/protocol"
)

// 此集合是 Codex 专属契约；普通候选仍使用入站原文。
var ignoredFields = []string{"max_tokens", "max_completion_tokens", "max_output_tokens", "temperature", "top_p", "truncation", "user", "prompt_cache_options", "prompt_cache_retention", "safety_identifier", "stream_options"}

func PrepareRequest(body []byte, source, model string) ([]byte, []string, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, nil, err
	}
	ignored := map[string]bool{}
	strip := func() {
		for _, field := range ignoredFields {
			if _, ok := raw[field]; ok {
				delete(raw, field)
				ignored["$."+field] = true
			}
		}
	}
	// 状态字段在强制 store=false 前检查，避免抹掉客户端语义。
	for _, field := range []string{"previous_response_id", "conversation"} {
		if v := bytes.TrimSpace(raw[field]); len(v) > 0 && !bytes.Equal(v, []byte("null")) && !bytes.Equal(v, []byte(`""`)) {
			return nil, nil, fmt.Errorf("Codex OAuth 不支持 $.%s", field)
		}
	}
	for _, field := range []string{"background", "store"} {
		if v := bytes.TrimSpace(raw[field]); len(v) > 0 && !bytes.Equal(v, []byte("null")) {
			var enabled bool
			if err := json.Unmarshal(v, &enabled); err != nil {
				return nil, nil, fmt.Errorf("$.%s 必须为布尔值", field)
			}
			if enabled {
				return nil, nil, fmt.Errorf("Codex OAuth 不支持 $.%s=true", field)
			}
		}
	}
	strip()
	if source != protocol.Responses {
		data, err := json.Marshal(raw)
		if err != nil {
			return nil, nil, err
		}
		converted, err := protocol.ConvertRequest(data, source, protocol.Responses, model, 0)
		if err != nil {
			return nil, nil, err
		}
		if err := json.Unmarshal(converted, &raw); err != nil {
			return nil, nil, err
		}
		strip()
	}
	var input []map[string]json.RawMessage
	if v := bytes.TrimSpace(raw["input"]); len(v) > 0 && v[0] == '"' {
		var text string
		if err := json.Unmarshal(v, &text); err != nil {
			return nil, nil, err
		}
		item, err := json.Marshal(map[string]any{"type": "message", "role": "user", "content": []map[string]string{{"type": "input_text", "text": text}}})
		if err != nil {
			return nil, nil, err
		}
		raw["input"] = append(append([]byte{'['}, item...), ']')
	}
	if err := json.Unmarshal(raw["input"], &input); err != nil || input == nil {
		return nil, nil, fmt.Errorf("Codex $.input 必须为字符串或数组")
	}
	changed := false
	for _, item := range input {
		if bytes.Equal(item["role"], []byte(`"system"`)) {
			item["role"] = json.RawMessage(`"developer"`)
			changed = true
		}
	}
	if changed {
		data, err := json.Marshal(input)
		if err != nil {
			return nil, nil, err
		}
		raw["input"] = data
	}
	if v := bytes.TrimSpace(raw["instructions"]); len(v) == 0 || bytes.Equal(v, []byte("null")) {
		raw["instructions"] = json.RawMessage(`""`)
	}
	modelJSON, err := json.Marshal(model)
	if err != nil {
		return nil, nil, err
	}
	raw["model"] = modelJSON
	raw["stream"] = json.RawMessage("true")
	raw["store"] = json.RawMessage("false")
	paths := make([]string, 0, len(ignored))
	for path := range ignored {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	data, err := json.Marshal(raw)
	return data, paths, err
}

func ApplyHeaders(headers http.Header, auth Auth, model string, body []byte) {
	headers.Del("x-api-key")
	headers.Del("anthropic-version")
	headers.Del("anthropic-beta")
	headers.Set("Authorization", "Bearer "+auth.Tokens.AccessToken)
	headers.Set("ChatGPT-Account-ID", auth.Identity.AccountID)
	headers.Set("Content-Type", "application/json")
	headers.Set("Accept", "text/event-stream")
	headers.Set("Originator", "codex_cli_rs")
	headers.Set("User-Agent", "model-confluence")
	hint := "model=" + model
	var payload struct {
		ServiceTier string `json:"service_tier"`
	}
	if json.Unmarshal(body, &payload) == nil && payload.ServiceTier != "" {
		hint += ";tier=" + payload.ServiceTier
	}
	headers.Set("X-Codex-Routing-Hint", hint)
}

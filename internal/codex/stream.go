package codex

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/Sanjeever/model-confluence/internal/protocol"
)

type UpstreamError struct {
	Status    int
	Code      string
	Message   string
	Body      []byte
	RecoverAt *time.Time
}

func (e *UpstreamError) Error() string {
	return fmt.Sprintf("Codex 上游失败（%s）：%s", e.Code, e.Message)
}

func ClassifyError(body []byte, status int, now time.Time) *UpstreamError {
	var payload struct {
		Code            string          `json:"code"`
		Type            string          `json:"type"`
		Message         string          `json:"message"`
		ResetsAt        int64           `json:"resets_at"`
		ResetsInSeconds *int64          `json:"resets_in_seconds"`
		Error           json.RawMessage `json:"error"`
	}
	e := &UpstreamError{Status: status, Body: body}
	if err := json.Unmarshal(body, &payload); err != nil {
		e.Code = "invalid_response"
		e.Message = "上游错误格式无效"
		return e
	}
	if v := bytes.TrimSpace(payload.Error); len(v) > 0 && v[0] == '{' {
		if err := json.Unmarshal(v, &payload); err != nil {
			e.Code = "invalid_response"
			return e
		}
	}
	e.Code = payload.Code
	if e.Code == "" {
		e.Code = payload.Type
	}
	e.Message = payload.Message
	switch e.Code {
	case "unauthorized", "authentication_error", "invalid_api_key", "token_expired":
		e.Status = http.StatusUnauthorized
	case "permission_error", "forbidden", "permission_denied":
		e.Status = http.StatusForbidden
	case "usage_limit_reached", "rate_limit_error", "rate_limit_exceeded":
		e.Status = http.StatusTooManyRequests
	case "model_not_found", "model_not_supported", "not_found_error":
		e.Status = http.StatusNotFound
	case "invalid_request_error", "bad_request_error":
		e.Status = http.StatusBadRequest
	}
	if e.Code == "usage_limit_reached" {
		var reset time.Time
		if payload.ResetsAt > 0 {
			reset = time.Unix(payload.ResetsAt, 0)
		} else if payload.ResetsInSeconds != nil && *payload.ResetsInSeconds >= 0 {
			reset = now.Add(time.Duration(*payload.ResetsInSeconds) * time.Second)
		}
		if !reset.IsZero() {
			e.RecoverAt = &reset
		}
	}
	return e
}

type Stream struct {
	items    map[int]json.RawMessage
	Response []byte
	Done     bool
}

func (s *Stream) Consume(event protocol.SSEEvent, model string) (protocol.SSEEvent, bool, error) {
	if len(bytes.TrimSpace(event.Data)) == 0 {
		return event, false, nil
	}
	if s.Done {
		return event, false, fmt.Errorf("Codex 终止后仍有事件")
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(event.Data, &payload); err != nil {
		return event, false, fmt.Errorf("Codex SSE 格式无效: %w", err)
	}
	var kind string
	if v, ok := payload["type"]; ok {
		if err := json.Unmarshal(v, &kind); err != nil {
			return event, false, err
		}
	} else {
		kind = event.Name
	}
	if kind == "" {
		return event, false, fmt.Errorf("Codex SSE 缺少事件类型")
	}
	if kind == "error" || kind == "response.failed" {
		body := event.Data
		if kind == "response.failed" {
			var response map[string]json.RawMessage
			if err := json.Unmarshal(payload["response"], &response); err != nil {
				return event, false, err
			}
			var err error
			body, err = json.Marshal(map[string]json.RawMessage{"error": response["error"]})
			if err != nil {
				return event, false, err
			}
		}
		return event, false, ClassifyError(body, http.StatusBadGateway, time.Now())
	}
	semantic := false
	switch kind {
	case "response.output_text.delta", "response.reasoning_summary_text.delta", "response.reasoning_text.delta", "response.function_call_arguments.delta", "response.custom_tool_call_input.delta":
		var delta string
		if err := json.Unmarshal(payload["delta"], &delta); err != nil {
			return event, false, err
		}
		semantic = delta != ""
	case "response.output_item.added", "response.output_item.done":
		var item struct {
			Type    string `json:"type"`
			CallID  string `json:"call_id"`
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		}
		if err := json.Unmarshal(payload["item"], &item); err != nil {
			return event, false, err
		}
		semantic = item.CallID != ""
		for _, part := range item.Content {
			semantic = semantic || part.Text != ""
		}
		if kind == "response.output_item.done" {
			var index int
			if err := json.Unmarshal(payload["output_index"], &index); err != nil {
				return event, false, err
			}
			if s.items == nil {
				s.items = map[int]json.RawMessage{}
			}
			s.items[index] = payload["item"]
		}
	case "response.completed", "response.incomplete":
		var response map[string]json.RawMessage
		if err := json.Unmarshal(payload["response"], &response); err != nil || response == nil {
			return event, false, fmt.Errorf("Codex 终止事件缺少 response")
		}
		var status string
		if err := json.Unmarshal(response["status"], &status); err != nil {
			return event, false, err
		}
		if kind == "response.completed" && status != "completed" || kind == "response.incomplete" && status != "incomplete" {
			return event, false, fmt.Errorf("Codex 终止状态不匹配")
		}
		if v := bytes.TrimSpace(response["error"]); len(v) > 0 && !bytes.Equal(v, []byte("null")) {
			body, err := json.Marshal(map[string]json.RawMessage{"error": v})
			if err != nil {
				return event, false, err
			}
			return event, false, ClassifyError(body, 502, time.Now())
		}
		if kind == "response.incomplete" {
			var details struct {
				Reason string `json:"reason"`
			}
			if err := json.Unmarshal(response["incomplete_details"], &details); err != nil {
				return event, false, err
			}
			if details.Reason != "max_output_tokens" && details.Reason != "content_filter" {
				return event, false, fmt.Errorf("Codex 未完成响应：%s", details.Reason)
			}
		}
		var output []json.RawMessage
		if v := bytes.TrimSpace(response["output"]); len(v) > 0 && !bytes.Equal(v, []byte("null")) {
			if err := json.Unmarshal(v, &output); err != nil {
				return event, false, err
			}
		}
		if len(output) == 0 && len(s.items) > 0 {
			indexes := make([]int, 0, len(s.items))
			for index := range s.items {
				indexes = append(indexes, index)
			}
			sort.Ints(indexes)
			for _, index := range indexes {
				output = append(output, s.items[index])
			}
			data, err := json.Marshal(output)
			if err != nil {
				return event, false, err
			}
			response["output"] = data
		}
		if output == nil {
			response["output"] = json.RawMessage("[]")
		}
		response["model"], _ = json.Marshal(model)
		data, err := json.Marshal(response)
		if err != nil {
			return event, false, err
		}
		s.Response = data
		s.Done = true
		payload["response"] = data
	}
	if kind != "response.completed" && kind != "response.incomplete" {
		if v, ok := payload["response"]; ok {
			var response map[string]json.RawMessage
			if err := json.Unmarshal(v, &response); err != nil {
				return event, false, err
			}
			if response != nil {
				response["model"], _ = json.Marshal(model)
				data, err := json.Marshal(response)
				if err != nil {
					return event, false, err
				}
				payload["response"] = data
			}
		}
	}
	if _, ok := payload["model"]; ok {
		payload["model"], _ = json.Marshal(model)
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return event, false, err
	}
	event.Data = data
	var raw bytes.Buffer
	if event.Name != "" {
		fmt.Fprintf(&raw, "event: %s\n", event.Name)
	}
	raw.WriteString("data: ")
	raw.Write(data)
	raw.WriteString("\n\n")
	event.Raw = raw.Bytes()
	return event, semantic, nil
}

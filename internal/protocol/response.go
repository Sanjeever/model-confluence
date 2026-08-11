package protocol

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

func ConvertResponse(body []byte, from, to, virtualModel string) ([]byte, error) {
	response, err := DecodeResponse(body, from)
	if err != nil {
		return nil, err
	}
	response.Model = virtualModel
	return EncodeResponse(response, to)
}

func DecodeResponse(body []byte, protocol string) (Response, error) {
	switch protocol {
	case Chat:
		return decodeChatResponse(body)
	case Responses:
		return decodeResponsesResponse(body)
	case Messages:
		return decodeMessagesResponse(body)
	default:
		return Response{}, fmt.Errorf("unsupported protocol %q", protocol)
	}
}

func EncodeResponse(response Response, protocol string) ([]byte, error) {
	switch protocol {
	case Chat:
		return encodeChatResponse(response)
	case Responses:
		return encodeResponsesResponse(response)
	case Messages:
		return encodeMessagesResponse(response)
	default:
		return nil, fmt.Errorf("unsupported protocol %q", protocol)
	}
}

func decodeChatResponse(body []byte) (Response, error) {
	var source struct {
		ID      string `json:"id"`
		Model   string `json:"model"`
		Choices []struct {
			Message struct {
				Content          string `json:"content"`
				ReasoningContent string `json:"reasoning_content"`
				ToolCalls        []struct {
					ID       string `json:"id"`
					Function struct {
						Name      string          `json:"name"`
						Arguments json.RawMessage `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			PromptTokens        *int `json:"prompt_tokens"`
			CompletionTokens    *int `json:"completion_tokens"`
			TotalTokens         *int `json:"total_tokens"`
			PromptTokensDetails struct {
				CachedTokens *int `json:"cached_tokens"`
			} `json:"prompt_tokens_details"`
			CompletionTokensDetails struct {
				ReasoningTokens *int `json:"reasoning_tokens"`
			} `json:"completion_tokens_details"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &source); err != nil {
		return Response{}, err
	}
	if len(source.Choices) == 0 {
		return Response{}, errors.New("Chat response has no choices")
	}
	choice := source.Choices[0]
	response := Response{ID: source.ID, Model: source.Model, StopReason: chatStopToCanonical(choice.FinishReason), Usage: Usage{InputTokens: source.Usage.PromptTokens, CacheReadTokens: source.Usage.PromptTokensDetails.CachedTokens, OutputTokens: source.Usage.CompletionTokens, ReasoningTokens: source.Usage.CompletionTokensDetails.ReasoningTokens, TotalTokens: source.Usage.TotalTokens, Raw: append(json.RawMessage(nil), body...)}}
	if choice.Message.ReasoningContent != "" {
		response.Blocks = append(response.Blocks, Block{Type: "reasoning", Text: choice.Message.ReasoningContent})
	}
	if choice.Message.Content != "" {
		response.Blocks = append(response.Blocks, Block{Type: "text", Text: choice.Message.Content})
	}
	for _, call := range choice.Message.ToolCalls {
		response.Blocks = append(response.Blocks, Block{Type: "tool_call", ToolCall: &ToolCall{ID: call.ID, Name: call.Function.Name, Arguments: normalizeArguments(call.Function.Arguments)}})
	}
	return response, nil
}

func decodeResponsesResponse(body []byte) (Response, error) {
	var source struct {
		ID                string `json:"id"`
		Model             string `json:"model"`
		Status            string `json:"status"`
		IncompleteDetails *struct {
			Reason string `json:"reason"`
		} `json:"incomplete_details"`
		Output []map[string]json.RawMessage `json:"output"`
		Usage  struct {
			InputTokens        *int `json:"input_tokens"`
			OutputTokens       *int `json:"output_tokens"`
			TotalTokens        *int `json:"total_tokens"`
			InputTokensDetails struct {
				CachedTokens *int `json:"cached_tokens"`
			} `json:"input_tokens_details"`
			OutputTokensDetails struct {
				ReasoningTokens *int `json:"reasoning_tokens"`
			} `json:"output_tokens_details"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &source); err != nil {
		return Response{}, err
	}
	response := Response{ID: source.ID, Model: source.Model, StopReason: "end_turn", Usage: Usage{InputTokens: source.Usage.InputTokens, CacheReadTokens: source.Usage.InputTokensDetails.CachedTokens, OutputTokens: source.Usage.OutputTokens, ReasoningTokens: source.Usage.OutputTokensDetails.ReasoningTokens, TotalTokens: source.Usage.TotalTokens, Raw: append(json.RawMessage(nil), body...)}}
	if source.Status == "incomplete" && source.IncompleteDetails != nil {
		response.StopReason = "max_tokens"
	}
	for _, item := range source.Output {
		var itemType string
		_ = json.Unmarshal(item["type"], &itemType)
		switch itemType {
		case "message":
			var content []map[string]json.RawMessage
			if err := json.Unmarshal(item["content"], &content); err != nil {
				return Response{}, err
			}
			for _, part := range content {
				var partType, text string
				_ = json.Unmarshal(part["type"], &partType)
				if partType == "refusal" {
					return Response{}, errors.New("cross-protocol refusal blocks are not supported")
				}
				if partType != "output_text" && partType != "text" {
					return Response{}, fmt.Errorf("unsupported Responses output content %q", partType)
				}
				_ = json.Unmarshal(part["text"], &text)
				response.Blocks = append(response.Blocks, Block{Type: "text", Text: text})
			}
		case "function_call":
			var callID, name string
			var arguments json.RawMessage
			_ = json.Unmarshal(item["call_id"], &callID)
			_ = json.Unmarshal(item["name"], &name)
			_ = json.Unmarshal(item["arguments"], &arguments)
			response.Blocks = append(response.Blocks, Block{Type: "tool_call", ToolCall: &ToolCall{ID: callID, Name: name, Arguments: normalizeArguments(arguments)}})
			response.StopReason = "tool_use"
		case "reasoning":
			var summary []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			}
			if len(item["summary"]) > 0 {
				if err := json.Unmarshal(item["summary"], &summary); err != nil {
					return Response{}, err
				}
			}
			for _, part := range summary {
				if part.Type != "summary_text" {
					return Response{}, fmt.Errorf("unsupported Responses reasoning summary %q", part.Type)
				}
				if part.Text != "" {
					response.Blocks = append(response.Blocks, Block{Type: "reasoning", Text: part.Text})
				}
			}
		default:
			return Response{}, fmt.Errorf("unsupported Responses output item %q", itemType)
		}
	}
	return response, nil
}

func decodeMessagesResponse(body []byte) (Response, error) {
	var source struct {
		ID         string                       `json:"id"`
		Model      string                       `json:"model"`
		Content    []map[string]json.RawMessage `json:"content"`
		StopReason string                       `json:"stop_reason"`
		Usage      struct {
			InputTokens              *int `json:"input_tokens"`
			OutputTokens             *int `json:"output_tokens"`
			CacheReadInputTokens     *int `json:"cache_read_input_tokens"`
			CacheCreationInputTokens *int `json:"cache_creation_input_tokens"`
			OutputTokensDetails      struct {
				ThinkingTokens *int `json:"thinking_tokens"`
			} `json:"output_tokens_details"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &source); err != nil {
		return Response{}, err
	}
	response := Response{ID: source.ID, Model: source.Model, StopReason: messagesStopToCanonical(source.StopReason), Usage: Usage{InputTokens: source.Usage.InputTokens, CacheReadTokens: source.Usage.CacheReadInputTokens, CacheWriteTokens: source.Usage.CacheCreationInputTokens, OutputTokens: source.Usage.OutputTokens, ReasoningTokens: source.Usage.OutputTokensDetails.ThinkingTokens, Raw: append(json.RawMessage(nil), body...)}}
	response.Usage.TotalTokens = sumPointers(response.Usage.InputTokens, response.Usage.CacheReadTokens, response.Usage.CacheWriteTokens, response.Usage.OutputTokens)
	for _, item := range source.Content {
		var itemType string
		_ = json.Unmarshal(item["type"], &itemType)
		switch itemType {
		case "text":
			var text string
			_ = json.Unmarshal(item["text"], &text)
			response.Blocks = append(response.Blocks, Block{Type: "text", Text: text})
		case "tool_use":
			var id, name string
			var input json.RawMessage
			_ = json.Unmarshal(item["id"], &id)
			_ = json.Unmarshal(item["name"], &name)
			_ = json.Unmarshal(item["input"], &input)
			response.Blocks = append(response.Blocks, Block{Type: "tool_call", ToolCall: &ToolCall{ID: id, Name: name, Arguments: normalizeArguments(input)}})
		case "thinking":
			var thinking string
			_ = json.Unmarshal(item["thinking"], &thinking)
			if thinking != "" {
				response.Blocks = append(response.Blocks, Block{Type: "reasoning", Text: thinking})
			}
		case "redacted_thinking":
		default:
			return Response{}, fmt.Errorf("unsupported Messages response content %q", itemType)
		}
	}
	return response, nil
}

func encodeChatResponse(response Response) ([]byte, error) {
	var text, reasoning string
	toolCalls := make([]any, 0)
	for _, block := range response.Blocks {
		if block.Type == "text" {
			text += block.Text
		} else if block.Type == "reasoning" {
			reasoning += block.Text
		} else if block.Type == "tool_call" {
			toolCalls = append(toolCalls, map[string]any{"id": block.ToolCall.ID, "type": "function", "function": map[string]any{"name": block.ToolCall.Name, "arguments": string(block.ToolCall.Arguments)}})
		}
	}
	message := map[string]any{"role": "assistant", "content": text}
	if reasoning != "" {
		message["reasoning_content"] = reasoning
	}
	if len(toolCalls) > 0 {
		message["tool_calls"] = toolCalls
	}
	result := map[string]any{
		"id": idOr(response.ID, "chatcmpl"), "object": "chat.completion", "created": time.Now().Unix(), "model": response.Model,
		"choices": []any{map[string]any{"index": 0, "message": message, "finish_reason": canonicalStopToChat(response.StopReason)}},
		"usage":   encodeChatUsage(response.Usage),
	}
	return json.Marshal(result)
}

func encodeResponsesResponse(response Response) ([]byte, error) {
	responseID := idOr(response.ID, "resp")
	output := make([]any, 0)
	var text, reasoning string
	for _, block := range response.Blocks {
		if block.Type == "text" {
			text += block.Text
		} else if block.Type == "reasoning" {
			reasoning += block.Text
		}
	}
	if reasoning != "" {
		output = append(output, map[string]any{"id": newID("rs"), "type": "reasoning", "summary": []any{map[string]any{"type": "summary_text", "text": reasoning}}})
	}
	if text != "" {
		output = append(output, map[string]any{"id": newID("msg"), "type": "message", "status": "completed", "role": "assistant", "content": []any{map[string]any{"type": "output_text", "text": text, "annotations": []any{}}}})
	}
	for _, block := range response.Blocks {
		if block.Type == "tool_call" {
			output = append(output, map[string]any{"id": newID("fc"), "type": "function_call", "status": "completed", "call_id": block.ToolCall.ID, "name": block.ToolCall.Name, "arguments": string(block.ToolCall.Arguments)})
		}
	}
	status := "completed"
	var incomplete any
	if response.StopReason == "max_tokens" {
		status = "incomplete"
		incomplete = map[string]string{"reason": "max_output_tokens"}
	}
	result := map[string]any{
		"id": responseID, "object": "response", "created_at": time.Now().Unix(), "completed_at": time.Now().Unix(), "status": status,
		"error": nil, "incomplete_details": incomplete, "model": response.Model, "output": output, "parallel_tool_calls": true,
		"usage": encodeResponsesUsage(response.Usage),
	}
	return json.Marshal(result)
}

func encodeMessagesResponse(response Response) ([]byte, error) {
	content := make([]any, 0, len(response.Blocks))
	for _, block := range response.Blocks {
		switch block.Type {
		case "reasoning":
			content = append(content, map[string]any{"type": "thinking", "thinking": block.Text})
		case "text":
			content = append(content, map[string]any{"type": "text", "text": block.Text})
		case "tool_call":
			content = append(content, map[string]any{"type": "tool_use", "id": block.ToolCall.ID, "name": block.ToolCall.Name, "input": rawOrObject(block.ToolCall.Arguments)})
		}
	}
	result := map[string]any{
		"id": idOr(response.ID, "msg"), "type": "message", "role": "assistant", "model": response.Model,
		"content": content, "stop_reason": canonicalStopToMessages(response.StopReason), "stop_sequence": nil,
		"usage": encodeMessagesUsage(response.Usage),
	}
	return json.Marshal(result)
}

func encodeChatUsage(usage Usage) map[string]any {
	return map[string]any{
		"prompt_tokens": valueOrZero(usage.InputTokens), "completion_tokens": valueOrZero(usage.OutputTokens), "total_tokens": valueOrZero(usage.TotalTokens),
		"prompt_tokens_details":     map[string]int{"cached_tokens": valueOrZero(usage.CacheReadTokens)},
		"completion_tokens_details": map[string]int{"reasoning_tokens": valueOrZero(usage.ReasoningTokens)},
	}
}

func encodeResponsesUsage(usage Usage) map[string]any {
	return map[string]any{
		"input_tokens": valueOrZero(usage.InputTokens), "output_tokens": valueOrZero(usage.OutputTokens), "total_tokens": valueOrZero(usage.TotalTokens),
		"input_tokens_details":  map[string]int{"cached_tokens": valueOrZero(usage.CacheReadTokens)},
		"output_tokens_details": map[string]int{"reasoning_tokens": valueOrZero(usage.ReasoningTokens)},
	}
}

func encodeMessagesUsage(usage Usage) map[string]any {
	return map[string]any{
		"input_tokens": valueOrZero(usage.InputTokens), "output_tokens": valueOrZero(usage.OutputTokens),
		"cache_read_input_tokens": valueOrZero(usage.CacheReadTokens), "cache_creation_input_tokens": valueOrZero(usage.CacheWriteTokens),
	}
}

func valueOrZero(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}

func sumPointers(values ...*int) *int {
	total := 0
	found := false
	for _, value := range values {
		if value != nil {
			total += *value
			found = true
		}
	}
	if !found {
		return nil
	}
	return &total
}

func idOr(id, prefix string) string {
	if id != "" {
		return id
	}
	return newID(prefix)
}

func newID(prefix string) string {
	value := make([]byte, 12)
	_, _ = rand.Read(value)
	return prefix + "_" + hex.EncodeToString(value)
}

func chatStopToCanonical(reason string) string {
	switch reason {
	case "tool_calls", "function_call":
		return "tool_use"
	case "length":
		return "max_tokens"
	case "content_filter":
		return "content_filter"
	default:
		return "end_turn"
	}
}

func messagesStopToCanonical(reason string) string {
	switch reason {
	case "tool_use":
		return "tool_use"
	case "max_tokens":
		return "max_tokens"
	case "stop_sequence":
		return "stop_sequence"
	case "refusal":
		return "refusal"
	default:
		return "end_turn"
	}
}

func canonicalStopToChat(reason string) string {
	switch reason {
	case "tool_use":
		return "tool_calls"
	case "max_tokens":
		return "length"
	case "content_filter", "refusal":
		return "content_filter"
	default:
		return "stop"
	}
}

func canonicalStopToMessages(reason string) string {
	switch reason {
	case "tool_use":
		return "tool_use"
	case "max_tokens":
		return "max_tokens"
	case "stop_sequence":
		return "stop_sequence"
	case "refusal", "content_filter":
		return "refusal"
	default:
		return "end_turn"
	}
}

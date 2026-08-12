package protocol

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

func ConvertRequest(body []byte, from, to, upstreamModel string, defaultMaxOutputTokens int) ([]byte, error) {
	request, err := DecodeRequest(body, from)
	if err != nil {
		return nil, err
	}
	request.Model = upstreamModel
	if request.MaxOutputTokens == nil && to == Messages {
		request.MaxOutputTokens = &defaultMaxOutputTokens
	}
	return EncodeRequest(request, to)
}

func DecodeRequest(body []byte, protocol string) (Request, error) {
	switch protocol {
	case Chat:
		return decodeChatRequest(body)
	case Responses:
		return decodeResponsesRequest(body)
	case Messages:
		return decodeMessagesRequest(body)
	default:
		return Request{}, fmt.Errorf("unsupported protocol %q", protocol)
	}
}

func EncodeRequest(request Request, protocol string) ([]byte, error) {
	switch protocol {
	case Chat:
		return encodeChatRequest(request)
	case Responses:
		return encodeResponsesRequest(request)
	case Messages:
		return encodeMessagesRequest(request)
	default:
		return nil, fmt.Errorf("unsupported protocol %q", protocol)
	}
}

func decodeChatRequest(body []byte) (Request, error) {
	var source struct {
		Model             string          `json:"model"`
		Messages          json.RawMessage `json:"messages"`
		Tools             json.RawMessage `json:"tools"`
		ToolChoice        json.RawMessage `json:"tool_choice"`
		Stream            bool            `json:"stream"`
		ParallelToolCalls bool            `json:"parallel_tool_calls"`
		MaxTokens         *int            `json:"max_tokens"`
		MaxCompletion     *int            `json:"max_completion_tokens"`
		Temperature       *float64        `json:"temperature"`
		TopP              *float64        `json:"top_p"`
		Stop              json.RawMessage `json:"stop"`
		Effort            string          `json:"reasoning_effort"`
	}
	if err := json.Unmarshal(body, &source); err != nil {
		return Request{}, err
	}
	if source.Model == "" {
		return Request{}, errors.New("model is required")
	}
	request := Request{Model: source.Model, Stream: source.Stream, ParallelToolCalls: source.ParallelToolCalls, MaxOutputTokens: source.MaxCompletion, Temperature: source.Temperature, TopP: source.TopP, Effort: source.Effort}
	if request.MaxOutputTokens == nil {
		request.MaxOutputTokens = source.MaxTokens
	}
	if err := decodeStop(source.Stop, &request.Stop); err != nil {
		return Request{}, err
	}
	var messages []struct {
		Role      string          `json:"role"`
		Content   json.RawMessage `json:"content"`
		ToolCalls []struct {
			ID       string `json:"id"`
			Type     string `json:"type"`
			Function struct {
				Name      string          `json:"name"`
				Arguments json.RawMessage `json:"arguments"`
			} `json:"function"`
		} `json:"tool_calls"`
		ToolCallID string `json:"tool_call_id"`
	}
	if len(source.Messages) == 0 || bytes.Equal(source.Messages, []byte("null")) {
		return Request{}, errors.New("messages is required")
	}
	if err := json.Unmarshal(source.Messages, &messages); err != nil {
		return Request{}, err
	}
	seenConversation := false
	for index, message := range messages {
		path := fmt.Sprintf("messages[%d]", index)
		if message.Role == "system" || message.Role == "developer" {
			if seenConversation {
				return Request{}, errors.New("system/developer messages are only supported before conversation messages")
			}
			text, err := decodeTextContent(message.Content)
			if err != nil {
				return Request{}, err
			}
			request.Instructions = append(request.Instructions, text)
			continue
		}
		seenConversation = true
		if message.Role == "tool" {
			text, err := decodeTextContent(message.Content)
			if err != nil {
				return Request{}, err
			}
			request.Messages = append(request.Messages, Message{Role: "user", Blocks: []Block{{Type: "tool_result", ToolResult: &ToolResult{CallID: message.ToolCallID, Content: text}}}})
			continue
		}
		if message.Role != "user" && message.Role != "assistant" {
			return Request{}, fmt.Errorf("unsupported chat role %q", message.Role)
		}
		canonical := Message{Role: message.Role}
		if len(message.Content) > 0 && !bytes.Equal(message.Content, []byte("null")) {
			blocks, err := decodeChatContentBlocks(message.Content, path+".content")
			if err != nil {
				return Request{}, err
			}
			canonical.Blocks = append(canonical.Blocks, blocks...)
		}
		for _, call := range message.ToolCalls {
			if call.Type != "" && call.Type != "function" {
				return Request{}, fmt.Errorf("unsupported chat tool call type %q", call.Type)
			}
			canonical.Blocks = append(canonical.Blocks, Block{Type: "tool_call", ToolCall: &ToolCall{ID: call.ID, Name: call.Function.Name, Arguments: normalizeArguments(call.Function.Arguments)}})
		}
		request.Messages = append(request.Messages, canonical)
	}
	if err := decodeChatTools(source.Tools, &request.Tools); err != nil {
		return Request{}, err
	}
	request.ToolChoice = decodeChatToolChoice(source.ToolChoice)
	return request, nil
}

func decodeResponsesRequest(body []byte) (Request, error) {
	var source struct {
		Model             string          `json:"model"`
		Instructions      json.RawMessage `json:"instructions"`
		Input             json.RawMessage `json:"input"`
		Tools             json.RawMessage `json:"tools"`
		ToolChoice        json.RawMessage `json:"tool_choice"`
		Stream            bool            `json:"stream"`
		ParallelToolCalls bool            `json:"parallel_tool_calls"`
		MaxOutputTokens   *int            `json:"max_output_tokens"`
		Temperature       *float64        `json:"temperature"`
		TopP              *float64        `json:"top_p"`
		Reasoning         struct {
			Effort string `json:"effort"`
		} `json:"reasoning"`
		PreviousResponseID *string `json:"previous_response_id"`
		Conversation       any     `json:"conversation"`
		Background         bool    `json:"background"`
		Store              *bool   `json:"store"`
	}
	if err := json.Unmarshal(body, &source); err != nil {
		return Request{}, err
	}
	if source.Model == "" {
		return Request{}, errors.New("model is required")
	}
	if source.PreviousResponseID != nil || source.Conversation != nil || source.Background || source.Store != nil && *source.Store {
		return Request{}, errors.New("stateful Responses fields are not supported")
	}
	request := Request{Model: source.Model, Stream: source.Stream, ParallelToolCalls: source.ParallelToolCalls, MaxOutputTokens: source.MaxOutputTokens, Temperature: source.Temperature, TopP: source.TopP, Effort: source.Reasoning.Effort}
	if len(source.Instructions) > 0 && !bytes.Equal(source.Instructions, []byte("null")) {
		instruction, err := decodeTextContent(source.Instructions)
		if err != nil {
			return Request{}, err
		}
		request.Instructions = append(request.Instructions, instruction)
	}
	if len(source.Input) == 0 {
		return Request{}, errors.New("input is required")
	}
	if source.Input[0] == '"' {
		var text string
		if err := json.Unmarshal(source.Input, &text); err != nil {
			return Request{}, err
		}
		request.Messages = append(request.Messages, Message{Role: "user", Blocks: []Block{{Type: "text", Text: text}}})
	} else {
		var items []map[string]json.RawMessage
		if err := json.Unmarshal(source.Input, &items); err != nil {
			return Request{}, err
		}
		for index, item := range items {
			var itemType, role string
			_ = json.Unmarshal(item["type"], &itemType)
			_ = json.Unmarshal(item["role"], &role)
			path := fmt.Sprintf("input[%d]", index)
			switch itemType {
			case "", "message":
				if role == "system" || role == "developer" {
					text, err := decodeResponsesContent(item["content"])
					if err != nil {
						return Request{}, err
					}
					request.Instructions = append(request.Instructions, text)
					continue
				}
				if role != "user" && role != "assistant" {
					return Request{}, fmt.Errorf("unsupported Responses role %q", role)
				}
				blocks, err := decodeResponsesContentBlocks(item["content"], path+".content")
				if err != nil {
					return Request{}, err
				}
				request.Messages = append(request.Messages, Message{Role: role, Blocks: blocks})
			case "input_text":
				var text string
				if err := json.Unmarshal(item["text"], &text); err != nil {
					return Request{}, fmt.Errorf("%s.text: %w", path, err)
				}
				appendBlock(&request.Messages, "user", Block{Type: "text", Text: text})
			case "input_image":
				image, err := decodeResponsesImage(item, path)
				if err != nil {
					return Request{}, err
				}
				appendBlock(&request.Messages, "user", Block{Type: "image", Image: &image})
			case "function_call":
				var callID, name string
				var arguments json.RawMessage
				_ = json.Unmarshal(item["call_id"], &callID)
				_ = json.Unmarshal(item["name"], &name)
				_ = json.Unmarshal(item["arguments"], &arguments)
				appendBlock(&request.Messages, "assistant", Block{Type: "tool_call", ToolCall: &ToolCall{ID: callID, Name: name, Arguments: normalizeArguments(arguments)}})
			case "function_call_output":
				var callID string
				_ = json.Unmarshal(item["call_id"], &callID)
				text, err := decodeTextContent(item["output"])
				if err != nil {
					return Request{}, err
				}
				appendBlock(&request.Messages, "user", Block{Type: "tool_result", ToolResult: &ToolResult{CallID: callID, Content: text}})
			default:
				return Request{}, fmt.Errorf("unsupported Responses input item type %q", itemType)
			}
		}
	}
	if err := decodeResponsesTools(source.Tools, &request.Tools); err != nil {
		return Request{}, err
	}
	request.ToolChoice = decodeResponsesToolChoice(source.ToolChoice)
	return request, nil
}

func decodeMessagesRequest(body []byte) (Request, error) {
	var source struct {
		Model         string          `json:"model"`
		System        json.RawMessage `json:"system"`
		Messages      json.RawMessage `json:"messages"`
		Tools         json.RawMessage `json:"tools"`
		ToolChoice    json.RawMessage `json:"tool_choice"`
		Stream        bool            `json:"stream"`
		MaxTokens     *int            `json:"max_tokens"`
		Temperature   *float64        `json:"temperature"`
		TopP          *float64        `json:"top_p"`
		StopSequences []string        `json:"stop_sequences"`
		OutputConfig  struct {
			Effort string `json:"effort"`
		} `json:"output_config"`
	}
	if err := json.Unmarshal(body, &source); err != nil {
		return Request{}, err
	}
	if source.Model == "" {
		return Request{}, errors.New("model is required")
	}
	request := Request{Model: source.Model, Stream: source.Stream, MaxOutputTokens: source.MaxTokens, Temperature: source.Temperature, TopP: source.TopP, Stop: source.StopSequences, Effort: source.OutputConfig.Effort}
	if len(source.System) > 0 && !bytes.Equal(source.System, []byte("null")) {
		text, err := decodeAnthropicContent(source.System, false)
		if err != nil {
			return Request{}, err
		}
		request.Instructions = append(request.Instructions, text)
	}
	var messages []struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(source.Messages, &messages); err != nil {
		return Request{}, err
	}
	for index, message := range messages {
		if message.Role != "user" && message.Role != "assistant" && message.Role != "system" {
			return Request{}, fmt.Errorf("unsupported Messages role %q", message.Role)
		}
		canonical, err := decodeMessagesContent(message.Role, message.Content, fmt.Sprintf("messages[%d].content", index))
		if err != nil {
			return Request{}, err
		}
		request.Messages = append(request.Messages, canonical)
	}
	if err := decodeMessagesTools(source.Tools, &request.Tools); err != nil {
		return Request{}, err
	}
	request.ToolChoice = decodeMessagesToolChoice(source.ToolChoice)
	return request, nil
}

func encodeChatRequest(request Request) ([]byte, error) {
	messages := make([]any, 0, len(request.Messages)+1)
	if len(request.Instructions) > 0 {
		messages = append(messages, map[string]any{"role": "system", "content": strings.Join(request.Instructions, "\n\n")})
	}
	for messageIndex, message := range request.Messages {
		content, err := encodeChatContent(message.Blocks, fmt.Sprintf("messages[%d].content", messageIndex))
		if err != nil {
			return nil, err
		}
		reasoningParts := make([]string, 0)
		toolCalls := make([]any, 0)
		for _, block := range message.Blocks {
			if block.Type == "reasoning" {
				reasoningParts = append(reasoningParts, block.Text)
			}
			if block.Type == "tool_call" {
				toolCalls = append(toolCalls, map[string]any{"id": block.ToolCall.ID, "type": "function", "function": map[string]any{"name": block.ToolCall.Name, "arguments": string(block.ToolCall.Arguments)}})
			}
			if block.Type == "tool_result" {
				messages = append(messages, map[string]any{"role": "tool", "tool_call_id": block.ToolResult.CallID, "content": block.ToolResult.Content})
			}
		}
		if content != nil || len(reasoningParts) > 0 || len(toolCalls) > 0 {
			value := map[string]any{"role": message.Role, "content": content}
			if len(reasoningParts) > 0 {
				value["reasoning_content"] = strings.Join(reasoningParts, "")
			}
			if len(toolCalls) > 0 {
				value["tool_calls"] = toolCalls
			}
			messages = append(messages, value)
		}
	}
	result := map[string]any{"model": request.Model, "messages": messages, "stream": request.Stream}
	applyCommonRequestFields(result, request, "max_completion_tokens")
	if len(request.Tools) > 0 {
		tools := make([]any, 0, len(request.Tools))
		for _, tool := range request.Tools {
			function := map[string]any{"name": tool.Name, "parameters": rawOrObject(tool.Parameters)}
			if tool.Description != "" {
				function["description"] = tool.Description
			}
			if tool.Strict != nil {
				function["strict"] = *tool.Strict
			}
			tools = append(tools, map[string]any{"type": "function", "function": function})
		}
		result["tools"] = tools
		result["parallel_tool_calls"] = request.ParallelToolCalls
	}
	if choice := encodeChatToolChoice(request.ToolChoice); choice != nil {
		result["tool_choice"] = choice
	}
	if request.Effort != "" {
		result["reasoning_effort"] = request.Effort
	}
	return json.Marshal(result)
}

func encodeResponsesRequest(request Request) ([]byte, error) {
	input := make([]any, 0, len(request.Messages)*2)
	for messageIndex, message := range request.Messages {
		content, err := encodeResponsesContent(message.Blocks, message.Role, fmt.Sprintf("input[%d].content", messageIndex))
		if err != nil {
			return nil, err
		}
		for _, block := range message.Blocks {
			if block.Type == "tool_call" {
				input = append(input, map[string]any{"type": "function_call", "call_id": block.ToolCall.ID, "name": block.ToolCall.Name, "arguments": string(block.ToolCall.Arguments)})
			}
			if block.Type == "tool_result" {
				input = append(input, map[string]any{"type": "function_call_output", "call_id": block.ToolResult.CallID, "output": block.ToolResult.Content})
			}
		}
		if content != nil {
			input = append(input, map[string]any{"type": "message", "role": message.Role, "content": content})
		}
	}
	result := map[string]any{"model": request.Model, "input": input, "stream": request.Stream, "store": false}
	if len(request.Instructions) > 0 {
		result["instructions"] = strings.Join(request.Instructions, "\n\n")
	}
	applyCommonRequestFields(result, request, "max_output_tokens")
	if len(request.Tools) > 0 {
		tools := make([]any, 0, len(request.Tools))
		for _, tool := range request.Tools {
			value := map[string]any{"type": "function", "name": tool.Name, "parameters": rawOrObject(tool.Parameters)}
			if tool.Description != "" {
				value["description"] = tool.Description
			}
			if tool.Strict != nil {
				value["strict"] = *tool.Strict
			}
			tools = append(tools, value)
		}
		result["tools"] = tools
		result["parallel_tool_calls"] = request.ParallelToolCalls
	}
	if choice := encodeResponsesToolChoice(request.ToolChoice); choice != nil {
		result["tool_choice"] = choice
	}
	if request.Effort != "" {
		result["reasoning"] = map[string]any{"effort": request.Effort, "summary": "auto"}
	}
	return json.Marshal(result)
}

func encodeMessagesRequest(request Request) ([]byte, error) {
	messages := make([]any, 0, len(request.Messages))
	for messageIndex, message := range request.Messages {
		content := make([]any, 0, len(message.Blocks))
		for blockIndex, block := range message.Blocks {
			switch block.Type {
			case "text":
				content = append(content, map[string]any{"type": "text", "text": block.Text})
			case "image":
				image, err := encodeImage(block.Image, Messages, fmt.Sprintf("messages[%d].content[%d]", messageIndex, blockIndex))
				if err != nil {
					return nil, err
				}
				content = append(content, image)
			case "tool_call":
				content = append(content, map[string]any{"type": "tool_use", "id": block.ToolCall.ID, "name": block.ToolCall.Name, "input": rawOrObject(block.ToolCall.Arguments)})
			case "tool_result":
				content = append(content, map[string]any{"type": "tool_result", "tool_use_id": block.ToolResult.CallID, "content": block.ToolResult.Content, "is_error": block.ToolResult.IsError})
			}
		}
		messages = append(messages, map[string]any{"role": message.Role, "content": content})
	}
	result := map[string]any{"model": request.Model, "messages": messages, "stream": request.Stream}
	if len(request.Instructions) > 0 {
		result["system"] = strings.Join(request.Instructions, "\n\n")
	}
	applyCommonRequestFields(result, request, "max_tokens")
	delete(result, "stop")
	if len(request.Stop) > 0 {
		result["stop_sequences"] = request.Stop
	}
	if len(request.Tools) > 0 {
		tools := make([]any, 0, len(request.Tools))
		for _, tool := range request.Tools {
			value := map[string]any{"name": tool.Name, "input_schema": rawOrObject(tool.Parameters)}
			if tool.Description != "" {
				value["description"] = tool.Description
			}
			tools = append(tools, value)
		}
		result["tools"] = tools
	}
	if choice := encodeMessagesToolChoice(request.ToolChoice); choice != nil {
		result["tool_choice"] = choice
	}
	if request.Effort != "" {
		result["output_config"] = map[string]any{"effort": request.Effort}
	}
	return json.Marshal(result)
}

func decodeChatContentBlocks(raw json.RawMessage, path string) ([]Block, error) {
	value := bytes.TrimSpace(raw)
	if len(value) == 0 || bytes.Equal(value, []byte("null")) {
		return nil, nil
	}
	if value[0] == '"' {
		var text string
		if err := json.Unmarshal(value, &text); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		return []Block{{Type: "text", Text: text}}, nil
	}
	var blocks []map[string]json.RawMessage
	if err := json.Unmarshal(value, &blocks); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	result := make([]Block, 0, len(blocks))
	for index, block := range blocks {
		blockPath := fmt.Sprintf("%s[%d]", path, index)
		var blockType string
		if err := json.Unmarshal(block["type"], &blockType); err != nil {
			return nil, fmt.Errorf("%s.type: %w", blockPath, err)
		}
		switch blockType {
		case "text":
			var text string
			if err := json.Unmarshal(block["text"], &text); err != nil {
				return nil, fmt.Errorf("%s.text: %w", blockPath, err)
			}
			result = append(result, Block{Type: "text", Text: text})
		case "image_url":
			image, err := decodeChatImage(block["image_url"], blockPath+".image_url")
			if err != nil {
				return nil, err
			}
			result = append(result, Block{Type: "image", Image: &image})
		default:
			return nil, fmt.Errorf("unsupported Chat content block %q at %s.type", blockType, blockPath)
		}
	}
	return result, nil
}

func decodeResponsesContentBlocks(raw json.RawMessage, path string) ([]Block, error) {
	value := bytes.TrimSpace(raw)
	if len(value) == 0 || bytes.Equal(value, []byte("null")) {
		return nil, nil
	}
	if value[0] == '"' {
		var text string
		if err := json.Unmarshal(value, &text); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		return []Block{{Type: "text", Text: text}}, nil
	}
	var blocks []map[string]json.RawMessage
	if err := json.Unmarshal(value, &blocks); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	result := make([]Block, 0, len(blocks))
	for index, block := range blocks {
		blockPath := fmt.Sprintf("%s[%d]", path, index)
		var blockType string
		if err := json.Unmarshal(block["type"], &blockType); err != nil {
			return nil, fmt.Errorf("%s.type: %w", blockPath, err)
		}
		switch blockType {
		case "input_text", "output_text", "text":
			var text string
			if err := json.Unmarshal(block["text"], &text); err != nil {
				return nil, fmt.Errorf("%s.text: %w", blockPath, err)
			}
			result = append(result, Block{Type: "text", Text: text})
		case "input_image":
			image, err := decodeResponsesImage(block, blockPath)
			if err != nil {
				return nil, err
			}
			result = append(result, Block{Type: "image", Image: &image})
		default:
			return nil, fmt.Errorf("unsupported Responses content block %q at %s.type", blockType, blockPath)
		}
	}
	return result, nil
}

func decodeMessagesContent(role string, raw json.RawMessage, path string) (Message, error) {
	value := bytes.TrimSpace(raw)
	message := Message{Role: role}
	if len(value) == 0 || bytes.Equal(value, []byte("null")) {
		return message, nil
	}
	if value[0] == '"' {
		var text string
		if err := json.Unmarshal(value, &text); err != nil {
			return Message{}, fmt.Errorf("%s: %w", path, err)
		}
		message.Blocks = append(message.Blocks, Block{Type: "text", Text: text})
		return message, nil
	}
	var blocks []map[string]json.RawMessage
	if err := json.Unmarshal(value, &blocks); err != nil {
		return Message{}, fmt.Errorf("%s: %w", path, err)
	}
	for index, block := range blocks {
		blockPath := fmt.Sprintf("%s[%d]", path, index)
		var blockType string
		if err := json.Unmarshal(block["type"], &blockType); err != nil {
			return Message{}, fmt.Errorf("%s.type: %w", blockPath, err)
		}
		switch blockType {
		case "text":
			var text string
			if err := json.Unmarshal(block["text"], &text); err != nil {
				return Message{}, fmt.Errorf("%s.text: %w", blockPath, err)
			}
			message.Blocks = append(message.Blocks, Block{Type: "text", Text: text})
		case "image":
			image, err := decodeMessagesImage(block["source"], blockPath+".source")
			if err != nil {
				return Message{}, err
			}
			message.Blocks = append(message.Blocks, Block{Type: "image", Image: &image})
		case "tool_use":
			var id, name string
			_ = json.Unmarshal(block["id"], &id)
			_ = json.Unmarshal(block["name"], &name)
			message.Blocks = append(message.Blocks, Block{Type: "tool_call", ToolCall: &ToolCall{ID: id, Name: name, Arguments: normalizeArguments(block["input"])}})
		case "tool_result":
			var callID string
			var isError bool
			_ = json.Unmarshal(block["tool_use_id"], &callID)
			_ = json.Unmarshal(block["is_error"], &isError)
			text, err := decodeAnthropicContent(block["content"], false)
			if err != nil {
				return Message{}, err
			}
			message.Blocks = append(message.Blocks, Block{Type: "tool_result", ToolResult: &ToolResult{CallID: callID, Content: text, IsError: isError}})
		case "thinking":
			var thinking string
			_ = json.Unmarshal(block["thinking"], &thinking)
			if thinking != "" {
				message.Blocks = append(message.Blocks, Block{Type: "reasoning", Text: thinking})
			}
		case "redacted_thinking":
		default:
			return Message{}, fmt.Errorf("unsupported Messages content block %q", blockType)
		}
	}
	return message, nil
}

func encodeChatContent(blocks []Block, path string) (any, error) {
	var text strings.Builder
	parts := make([]any, 0, len(blocks))
	hasImage := false
	for index, block := range blocks {
		switch block.Type {
		case "text":
			if block.Text == "" {
				continue
			}
			if hasImage {
				parts = append(parts, map[string]any{"type": "text", "text": block.Text})
			} else {
				text.WriteString(block.Text)
			}
		case "image":
			if !hasImage {
				if text.Len() > 0 {
					parts = append(parts, map[string]any{"type": "text", "text": text.String()})
					text.Reset()
				}
				hasImage = true
			}
			image, err := encodeImage(block.Image, Chat, fmt.Sprintf("%s[%d]", path, index))
			if err != nil {
				return nil, err
			}
			parts = append(parts, image)
		}
	}
	if !hasImage {
		if text.Len() == 0 {
			return nil, nil
		}
		return text.String(), nil
	}
	return parts, nil
}

func encodeResponsesContent(blocks []Block, role, path string) (any, error) {
	var text strings.Builder
	parts := make([]any, 0, len(blocks))
	hasImage := false
	textType := "input_text"
	if role == "assistant" {
		textType = "output_text"
	}
	for index, block := range blocks {
		switch block.Type {
		case "text":
			if block.Text == "" {
				continue
			}
			if hasImage {
				parts = append(parts, map[string]any{"type": textType, "text": block.Text})
			} else {
				text.WriteString(block.Text)
			}
		case "image":
			if !hasImage {
				if text.Len() > 0 {
					parts = append(parts, map[string]any{"type": textType, "text": text.String()})
					text.Reset()
				}
				hasImage = true
			}
			image, err := encodeImage(block.Image, Responses, fmt.Sprintf("%s[%d]", path, index))
			if err != nil {
				return nil, err
			}
			parts = append(parts, image)
		}
	}
	if !hasImage {
		if text.Len() == 0 {
			return nil, nil
		}
		return text.String(), nil
	}
	return parts, nil
}

func applyCommonRequestFields(result map[string]any, request Request, maxField string) {
	if request.MaxOutputTokens != nil {
		result[maxField] = *request.MaxOutputTokens
	}
	if request.Temperature != nil {
		result["temperature"] = *request.Temperature
	}
	if request.TopP != nil {
		result["top_p"] = *request.TopP
	}
	if len(request.Stop) == 1 {
		result["stop"] = request.Stop[0]
	} else if len(request.Stop) > 1 {
		result["stop"] = request.Stop
	}
}

func decodeTextContent(raw json.RawMessage) (string, error) {
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return "", nil
	}
	if raw[0] == '"' {
		var text string
		return text, json.Unmarshal(raw, &text)
	}
	return decodeResponsesContent(raw)
}

func decodeResponsesContent(raw json.RawMessage) (string, error) {
	if len(raw) == 0 {
		return "", nil
	}
	if raw[0] == '"' {
		var text string
		return text, json.Unmarshal(raw, &text)
	}
	var blocks []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return "", err
	}
	var text strings.Builder
	for _, block := range blocks {
		var blockType, value string
		_ = json.Unmarshal(block["type"], &blockType)
		if blockType != "input_text" && blockType != "output_text" && blockType != "text" {
			return "", fmt.Errorf("unsupported text content block %q", blockType)
		}
		_ = json.Unmarshal(block["text"], &value)
		text.WriteString(value)
	}
	return text.String(), nil
}

func decodeAnthropicContent(raw json.RawMessage, allowTool bool) (string, error) {
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return "", nil
	}
	if raw[0] == '"' {
		var text string
		return text, json.Unmarshal(raw, &text)
	}
	var blocks []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return "", err
	}
	var text strings.Builder
	for _, block := range blocks {
		var blockType, value string
		_ = json.Unmarshal(block["type"], &blockType)
		if blockType != "text" {
			if allowTool {
				continue
			}
			return "", fmt.Errorf("unsupported Anthropic content block %q", blockType)
		}
		_ = json.Unmarshal(block["text"], &value)
		text.WriteString(value)
	}
	return text.String(), nil
}

func decodeStop(raw json.RawMessage, destination *[]string) error {
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil
	}
	if raw[0] == '"' {
		var value string
		if err := json.Unmarshal(raw, &value); err != nil {
			return err
		}
		*destination = []string{value}
		return nil
	}
	return json.Unmarshal(raw, destination)
}

func normalizeArguments(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return json.RawMessage(`{}`)
	}
	if raw[0] == '"' {
		var value string
		if json.Unmarshal(raw, &value) == nil && json.Valid([]byte(value)) {
			return json.RawMessage(value)
		}
	}
	return raw
}

func rawOrObject(raw json.RawMessage) any {
	if len(raw) == 0 {
		return json.RawMessage(`{}`)
	}
	return raw
}

func appendBlock(messages *[]Message, role string, block Block) {
	if len(*messages) > 0 && (*messages)[len(*messages)-1].Role == role {
		(*messages)[len(*messages)-1].Blocks = append((*messages)[len(*messages)-1].Blocks, block)
		return
	}
	*messages = append(*messages, Message{Role: role, Blocks: []Block{block}})
}

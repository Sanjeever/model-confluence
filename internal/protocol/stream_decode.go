package protocol

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
)

type streamDecoder struct {
	protocol      string
	started       bool
	chatTools     map[int]string
	messageBlocks map[int]messageStreamBlock
	responseTools map[int]string
	hadTools      bool
}

type messageStreamBlock struct {
	kind   string
	callID string
	name   string
}

func newStreamDecoder(protocol string) *streamDecoder {
	return &streamDecoder{
		protocol:      protocol,
		chatTools:     make(map[int]string),
		messageBlocks: make(map[int]messageStreamBlock),
		responseTools: make(map[int]string),
	}
}

func (d *streamDecoder) Decode(input SSEEvent) ([]streamEvent, error) {
	if len(bytes.TrimSpace(input.Data)) == 0 {
		return nil, nil
	}
	switch d.protocol {
	case Chat:
		return d.decodeChat(input.Data)
	case Messages:
		return d.decodeMessages(input)
	case Responses:
		return d.decodeResponses(input)
	default:
		return nil, fmt.Errorf("unsupported stream protocol %q", d.protocol)
	}
}

func (d *streamDecoder) decodeChat(data []byte) ([]streamEvent, error) {
	if bytes.Equal(bytes.TrimSpace(data), []byte("[DONE]")) {
		return []streamEvent{{Kind: streamDone}}, nil
	}
	var source struct {
		ID      string `json:"id"`
		Model   string `json:"model"`
		Choices []struct {
			Delta struct {
				Role             string  `json:"role"`
				Content          *string `json:"content"`
				ReasoningContent *string `json:"reasoning_content"`
				ToolCalls        []struct {
					Index    int    `json:"index"`
					ID       string `json:"id"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"delta"`
			FinishReason *string `json:"finish_reason"`
		} `json:"choices"`
		Usage chatStreamUsage `json:"usage"`
	}
	if err := json.Unmarshal(data, &source); err != nil {
		return nil, err
	}
	var events []streamEvent
	if !d.started {
		d.started = true
		events = append(events, streamEvent{Kind: streamStart, ID: source.ID, Model: source.Model})
	}
	for _, choice := range source.Choices {
		if choice.Delta.ReasoningContent != nil && *choice.Delta.ReasoningContent != "" {
			events = append(events, streamEvent{Kind: streamReasoning, Delta: *choice.Delta.ReasoningContent})
		}
		if choice.Delta.Content != nil && *choice.Delta.Content != "" {
			events = append(events, streamEvent{Kind: streamText, Delta: *choice.Delta.Content})
		}
		for _, tool := range choice.Delta.ToolCalls {
			callID, exists := d.chatTools[tool.Index]
			if !exists {
				callID = tool.ID
				if callID == "" {
					callID = fmt.Sprintf("call_%d", tool.Index)
				}
				d.chatTools[tool.Index] = callID
				d.hadTools = true
				events = append(events, streamEvent{Kind: streamToolStart, Index: tool.Index, CallID: callID, Name: tool.Function.Name})
			}
			if tool.Function.Arguments != "" {
				events = append(events, streamEvent{Kind: streamToolDelta, Index: tool.Index, CallID: callID, Delta: tool.Function.Arguments})
			}
		}
		if choice.FinishReason != nil {
			indexes := make([]int, 0, len(d.chatTools))
			for index := range d.chatTools {
				indexes = append(indexes, index)
			}
			sort.Ints(indexes)
			for _, index := range indexes {
				callID := d.chatTools[index]
				events = append(events, streamEvent{Kind: streamToolEnd, Index: index, CallID: callID})
			}
			d.chatTools = make(map[int]string)
			events = append(events, streamEvent{Kind: streamFinish, StopReason: normalizeChatStop(*choice.FinishReason)})
		}
	}
	if usage := source.Usage.canonical(); usagePresent(usage) {
		events = append(events, streamEvent{Kind: streamUsage, Usage: usage})
	}
	return events, nil
}

func (d *streamDecoder) decodeMessages(input SSEEvent) ([]streamEvent, error) {
	var raw struct {
		Type  string `json:"type"`
		Index int    `json:"index"`
	}
	if err := json.Unmarshal(input.Data, &raw); err != nil {
		return nil, err
	}
	eventType := raw.Type
	if eventType == "" {
		eventType = input.Name
	}
	switch eventType {
	case "ping":
		return nil, nil
	case "message_start":
		var source struct {
			Message struct {
				ID    string              `json:"id"`
				Model string              `json:"model"`
				Usage messagesStreamUsage `json:"usage"`
			} `json:"message"`
		}
		if err := json.Unmarshal(input.Data, &source); err != nil {
			return nil, err
		}
		d.started = true
		events := []streamEvent{{Kind: streamStart, ID: source.Message.ID, Model: source.Message.Model}}
		if usage := source.Message.Usage.canonical(); usagePresent(usage) {
			events = append(events, streamEvent{Kind: streamUsage, Usage: usage})
		}
		return events, nil
	case "content_block_start":
		var source struct {
			Index        int `json:"index"`
			ContentBlock struct {
				Type     string          `json:"type"`
				Text     string          `json:"text"`
				Thinking string          `json:"thinking"`
				ID       string          `json:"id"`
				Name     string          `json:"name"`
				Input    json.RawMessage `json:"input"`
			} `json:"content_block"`
		}
		if err := json.Unmarshal(input.Data, &source); err != nil {
			return nil, err
		}
		d.messageBlocks[source.Index] = messageStreamBlock{kind: source.ContentBlock.Type, callID: source.ContentBlock.ID, name: source.ContentBlock.Name}
		switch source.ContentBlock.Type {
		case "text":
			if source.ContentBlock.Text == "" {
				return nil, nil
			}
			return []streamEvent{{Kind: streamText, Index: source.Index, Delta: source.ContentBlock.Text}}, nil
		case "thinking":
			if source.ContentBlock.Thinking == "" {
				return nil, nil
			}
			return []streamEvent{{Kind: streamReasoning, Index: source.Index, Delta: source.ContentBlock.Thinking}}, nil
		case "redacted_thinking":
			return nil, nil
		case "tool_use":
		default:
			return nil, fmt.Errorf("unsupported Messages stream block %q", source.ContentBlock.Type)
		}
		d.hadTools = true
		events := []streamEvent{{Kind: streamToolStart, Index: source.Index, CallID: source.ContentBlock.ID, Name: source.ContentBlock.Name}}
		if len(source.ContentBlock.Input) > 0 && !bytes.Equal(bytes.TrimSpace(source.ContentBlock.Input), []byte("{}")) {
			events = append(events, streamEvent{Kind: streamToolDelta, Index: source.Index, CallID: source.ContentBlock.ID, Delta: string(source.ContentBlock.Input)})
		}
		return events, nil
	case "content_block_delta":
		var source struct {
			Index int `json:"index"`
			Delta struct {
				Type        string `json:"type"`
				Text        string `json:"text"`
				Thinking    string `json:"thinking"`
				PartialJSON string `json:"partial_json"`
			} `json:"delta"`
		}
		if err := json.Unmarshal(input.Data, &source); err != nil {
			return nil, err
		}
		block := d.messageBlocks[source.Index]
		switch source.Delta.Type {
		case "text_delta":
			return []streamEvent{{Kind: streamText, Index: source.Index, Delta: source.Delta.Text}}, nil
		case "input_json_delta":
			return []streamEvent{{Kind: streamToolDelta, Index: source.Index, CallID: block.callID, Delta: source.Delta.PartialJSON}}, nil
		case "thinking_delta":
			if block.kind != "thinking" {
				return nil, fmt.Errorf("unexpected Messages stream delta %q for block %q", source.Delta.Type, block.kind)
			}
			return []streamEvent{{Kind: streamReasoning, Index: source.Index, Delta: source.Delta.Thinking}}, nil
		case "signature_delta":
			if block.kind != "thinking" {
				return nil, fmt.Errorf("unexpected Messages stream delta %q for block %q", source.Delta.Type, block.kind)
			}
			return nil, nil
		default:
			return nil, fmt.Errorf("unsupported Messages stream delta %q", source.Delta.Type)
		}
	case "content_block_stop":
		block := d.messageBlocks[raw.Index]
		delete(d.messageBlocks, raw.Index)
		if block.kind == "tool_use" {
			return []streamEvent{{Kind: streamToolEnd, Index: raw.Index, CallID: block.callID}}, nil
		}
		return nil, nil
	case "message_delta":
		var source struct {
			Delta struct {
				StopReason string `json:"stop_reason"`
			} `json:"delta"`
			Usage messagesStreamUsage `json:"usage"`
		}
		if err := json.Unmarshal(input.Data, &source); err != nil {
			return nil, err
		}
		events := []streamEvent{{Kind: streamFinish, StopReason: normalizeMessagesStop(source.Delta.StopReason)}}
		if usage := source.Usage.canonical(); usagePresent(usage) {
			events = append(events, streamEvent{Kind: streamUsage, Usage: usage})
		}
		return events, nil
	case "message_stop":
		return []streamEvent{{Kind: streamDone}}, nil
	case "error":
		return nil, fmt.Errorf("Messages stream returned an error: %s", input.Data)
	default:
		return nil, fmt.Errorf("unsupported Messages stream event %q", eventType)
	}
}

func (d *streamDecoder) decodeResponses(input SSEEvent) ([]streamEvent, error) {
	var raw struct {
		Type        string `json:"type"`
		OutputIndex int    `json:"output_index"`
	}
	if err := json.Unmarshal(input.Data, &raw); err != nil {
		return nil, err
	}
	eventType := raw.Type
	if eventType == "" {
		eventType = input.Name
	}
	switch eventType {
	case "response.created", "response.in_progress", "response.queued":
		if d.started {
			return nil, nil
		}
		var source struct {
			Response struct {
				ID    string `json:"id"`
				Model string `json:"model"`
			} `json:"response"`
		}
		if err := json.Unmarshal(input.Data, &source); err != nil {
			return nil, err
		}
		d.started = true
		return []streamEvent{{Kind: streamStart, ID: source.Response.ID, Model: source.Response.Model}}, nil
	case "response.output_item.added":
		var source struct {
			OutputIndex int `json:"output_index"`
			Item        struct {
				Type      string `json:"type"`
				ID        string `json:"id"`
				CallID    string `json:"call_id"`
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			} `json:"item"`
		}
		if err := json.Unmarshal(input.Data, &source); err != nil {
			return nil, err
		}
		if source.Item.Type != "function_call" {
			return nil, nil
		}
		d.hadTools = true
		d.responseTools[source.OutputIndex] = source.Item.CallID
		events := []streamEvent{{Kind: streamToolStart, Index: source.OutputIndex, CallID: source.Item.CallID, Name: source.Item.Name}}
		if source.Item.Arguments != "" {
			events = append(events, streamEvent{Kind: streamToolDelta, Index: source.OutputIndex, CallID: source.Item.CallID, Delta: source.Item.Arguments})
		}
		return events, nil
	case "response.output_text.delta":
		var source struct {
			OutputIndex int    `json:"output_index"`
			Delta       string `json:"delta"`
		}
		if err := json.Unmarshal(input.Data, &source); err != nil {
			return nil, err
		}
		return []streamEvent{{Kind: streamText, Index: source.OutputIndex, Delta: source.Delta}}, nil
	case "response.reasoning_summary_text.delta", "response.reasoning_text.delta":
		var source struct {
			OutputIndex int    `json:"output_index"`
			Delta       string `json:"delta"`
		}
		if err := json.Unmarshal(input.Data, &source); err != nil {
			return nil, err
		}
		return []streamEvent{{Kind: streamReasoning, Index: source.OutputIndex, Delta: source.Delta}}, nil
	case "response.function_call_arguments.delta":
		var source struct {
			OutputIndex int    `json:"output_index"`
			Delta       string `json:"delta"`
		}
		if err := json.Unmarshal(input.Data, &source); err != nil {
			return nil, err
		}
		return []streamEvent{{Kind: streamToolDelta, Index: source.OutputIndex, CallID: d.responseTools[source.OutputIndex], Delta: source.Delta}}, nil
	case "response.output_item.done":
		var source struct {
			OutputIndex int `json:"output_index"`
			Item        struct {
				Type   string `json:"type"`
				CallID string `json:"call_id"`
			} `json:"item"`
		}
		if err := json.Unmarshal(input.Data, &source); err != nil {
			return nil, err
		}
		if source.Item.Type != "function_call" {
			return nil, nil
		}
		callID := source.Item.CallID
		if callID == "" {
			callID = d.responseTools[source.OutputIndex]
		}
		return []streamEvent{{Kind: streamToolEnd, Index: source.OutputIndex, CallID: callID}}, nil
	case "response.completed", "response.incomplete", "response.failed":
		var source struct {
			Response struct {
				Status            string `json:"status"`
				IncompleteDetails struct {
					Reason string `json:"reason"`
				} `json:"incomplete_details"`
				Usage responsesStreamUsage `json:"usage"`
			} `json:"response"`
		}
		if err := json.Unmarshal(input.Data, &source); err != nil {
			return nil, err
		}
		stop := "stop"
		if d.hadTools {
			stop = "tool_calls"
		}
		if source.Response.Status == "incomplete" && source.Response.IncompleteDetails.Reason == "max_output_tokens" {
			stop = "length"
		}
		events := []streamEvent{{Kind: streamFinish, StopReason: stop}}
		if usage := source.Response.Usage.canonical(); usagePresent(usage) {
			events = append(events, streamEvent{Kind: streamUsage, Usage: usage})
		}
		events = append(events, streamEvent{Kind: streamDone})
		return events, nil
	case "error":
		return nil, fmt.Errorf("Responses stream returned an error: %s", input.Data)
	case "response.content_part.added", "response.content_part.done", "response.output_text.done", "response.function_call_arguments.done",
		"response.reasoning_summary_part.added", "response.reasoning_summary_part.done", "response.reasoning_summary_text.done", "response.reasoning_text.done",
		"response.output_text.annotation.added", "response.output_text.annotation.done":
		return nil, nil
	default:
		return nil, fmt.Errorf("unsupported Responses stream event %q", eventType)
	}
}

type chatStreamUsage struct {
	PromptTokens     *int `json:"prompt_tokens"`
	CompletionTokens *int `json:"completion_tokens"`
	TotalTokens      *int `json:"total_tokens"`
	PromptDetails    struct {
		CachedTokens *int `json:"cached_tokens"`
	} `json:"prompt_tokens_details"`
	CompletionDetails struct {
		ReasoningTokens *int `json:"reasoning_tokens"`
	} `json:"completion_tokens_details"`
}

func (u chatStreamUsage) canonical() Usage {
	return Usage{InputTokens: u.PromptTokens, CacheReadTokens: u.PromptDetails.CachedTokens, OutputTokens: u.CompletionTokens, ReasoningTokens: u.CompletionDetails.ReasoningTokens, TotalTokens: u.TotalTokens}
}

type messagesStreamUsage struct {
	InputTokens              *int `json:"input_tokens"`
	OutputTokens             *int `json:"output_tokens"`
	CacheReadInputTokens     *int `json:"cache_read_input_tokens"`
	CacheCreationInputTokens *int `json:"cache_creation_input_tokens"`
}

func (u messagesStreamUsage) canonical() Usage {
	return Usage{InputTokens: u.InputTokens, CacheReadTokens: u.CacheReadInputTokens, CacheWriteTokens: u.CacheCreationInputTokens, OutputTokens: u.OutputTokens}
}

type responsesStreamUsage struct {
	InputTokens  *int `json:"input_tokens"`
	OutputTokens *int `json:"output_tokens"`
	TotalTokens  *int `json:"total_tokens"`
	InputDetails struct {
		CachedTokens *int `json:"cached_tokens"`
	} `json:"input_tokens_details"`
	OutputDetails struct {
		ReasoningTokens *int `json:"reasoning_tokens"`
	} `json:"output_tokens_details"`
}

func (u responsesStreamUsage) canonical() Usage {
	return Usage{InputTokens: u.InputTokens, CacheReadTokens: u.InputDetails.CachedTokens, OutputTokens: u.OutputTokens, ReasoningTokens: u.OutputDetails.ReasoningTokens, TotalTokens: u.TotalTokens}
}

func usagePresent(usage Usage) bool {
	return usage.InputTokens != nil || usage.CacheReadTokens != nil || usage.CacheWriteTokens != nil || usage.OutputTokens != nil || usage.ReasoningTokens != nil || usage.TotalTokens != nil
}

func normalizeChatStop(reason string) string {
	switch reason {
	case "length":
		return "length"
	case "tool_calls", "function_call":
		return "tool_calls"
	default:
		return "stop"
	}
}

func normalizeMessagesStop(reason string) string {
	switch reason {
	case "max_tokens":
		return "length"
	case "tool_use":
		return "tool_calls"
	default:
		return "stop"
	}
}

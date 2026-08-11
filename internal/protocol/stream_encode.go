package protocol

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

type streamEncoder struct {
	protocol         string
	model            string
	id               string
	created          int64
	sequence         int
	started          bool
	done             bool
	stopReason       string
	usage            Usage
	textStarted      bool
	textClosed       bool
	textIndex        int
	textItemID       string
	text             strings.Builder
	reasoningStarted bool
	reasoningClosed  bool
	reasoningIndex   int
	reasoningItemID  string
	reasoning        strings.Builder
	nextBlockIndex   int
	nextOutputIndex  int
	tools            map[string]*encodedTool
	toolOrder        []string
}

type encodedTool struct {
	key         string
	callID      string
	name        string
	targetIndex int
	blockIndex  int
	itemID      string
	arguments   strings.Builder
	closed      bool
}

func newStreamEncoder(protocol, model string) *streamEncoder {
	return &streamEncoder{
		protocol:       protocol,
		model:          model,
		created:        time.Now().Unix(),
		textIndex:      -1,
		reasoningIndex: -1,
		tools:          make(map[string]*encodedTool),
	}
}

func (e *streamEncoder) Encode(event streamEvent) ([][]byte, error) {
	if e.done {
		return nil, nil
	}
	var output [][]byte
	if event.Kind == streamStart {
		e.setID(event.ID)
		return nil, nil
	}
	if event.Kind == streamUsage {
		e.mergeUsage(event.Usage)
		return nil, nil
	}
	if event.Kind == streamFinish {
		e.stopReason = event.StopReason
		return nil, nil
	}
	if event.Kind == streamDone {
		return e.finish()
	}
	started, err := e.ensureStarted()
	if err != nil {
		return nil, err
	}
	output = append(output, started...)
	if (e.protocol == Responses || e.protocol == Messages) && event.Kind != streamReasoning {
		closed, err := e.closeReasoning()
		if err != nil {
			return nil, err
		}
		output = append(output, closed...)
	}
	var encoded [][]byte
	switch event.Kind {
	case streamText:
		encoded, err = e.encodeText(event.Delta)
	case streamReasoning:
		encoded, err = e.encodeReasoning(event.Delta)
	case streamToolStart:
		encoded, err = e.encodeToolStart(event)
	case streamToolDelta:
		encoded, err = e.encodeToolDelta(event)
	case streamToolEnd:
		encoded, err = e.encodeToolEnd(event)
	}
	if err != nil {
		return nil, err
	}
	return append(output, encoded...), nil
}

func (e *streamEncoder) encodeReasoning(delta string) ([][]byte, error) {
	if delta == "" {
		return nil, nil
	}
	switch e.protocol {
	case Chat:
		value := map[string]any{
			"id": e.id, "object": "chat.completion.chunk", "created": e.created, "model": e.model,
			"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"reasoning_content": delta}, "finish_reason": nil}},
		}
		encoded, err := dataSSE(value)
		return bytesResult(encoded, err)
	case Messages:
		var output [][]byte
		if !e.reasoningStarted {
			e.reasoningStarted = true
			e.reasoningIndex = e.nextBlockIndex
			e.nextBlockIndex++
			start, err := encodeSSE("content_block_start", map[string]any{"type": "content_block_start", "index": e.reasoningIndex, "content_block": map[string]any{"type": "thinking", "thinking": ""}})
			if err != nil {
				return nil, err
			}
			output = append(output, start)
		}
		e.reasoning.WriteString(delta)
		encoded, err := encodeSSE("content_block_delta", map[string]any{"type": "content_block_delta", "index": e.reasoningIndex, "delta": map[string]any{"type": "thinking_delta", "thinking": delta}})
		if err != nil {
			return nil, err
		}
		return append(output, encoded), nil
	case Responses:
		var output [][]byte
		if !e.reasoningStarted {
			e.reasoningStarted = true
			e.reasoningIndex = e.nextOutputIndex
			e.nextOutputIndex++
			e.reasoningItemID = "rs_" + strconv.FormatInt(time.Now().UnixNano(), 36)
			added, err := e.responsesEvent("response.output_item.added", map[string]any{"output_index": e.reasoningIndex, "item": e.responseReasoningItem("")})
			if err != nil {
				return nil, err
			}
			part, err := e.responsesEvent("response.reasoning_summary_part.added", map[string]any{"item_id": e.reasoningItemID, "output_index": e.reasoningIndex, "summary_index": 0, "part": map[string]any{"type": "summary_text", "text": ""}})
			if err != nil {
				return nil, err
			}
			output = append(output, added, part)
		}
		e.reasoning.WriteString(delta)
		encoded, err := e.responsesEvent("response.reasoning_summary_text.delta", map[string]any{"item_id": e.reasoningItemID, "output_index": e.reasoningIndex, "summary_index": 0, "delta": delta})
		if err != nil {
			return nil, err
		}
		return append(output, encoded), nil
	}
	return nil, nil
}

func (e *streamEncoder) closeReasoning() ([][]byte, error) {
	if !e.reasoningStarted || e.reasoningClosed {
		return nil, nil
	}
	e.reasoningClosed = true
	if e.protocol == Messages {
		closed, err := encodeSSE("content_block_stop", map[string]any{"type": "content_block_stop", "index": e.reasoningIndex})
		return bytesResult(closed, err)
	}
	text := e.reasoning.String()
	done, err := e.responsesEvent("response.reasoning_summary_text.done", map[string]any{"item_id": e.reasoningItemID, "output_index": e.reasoningIndex, "summary_index": 0, "text": text})
	if err != nil {
		return nil, err
	}
	part, err := e.responsesEvent("response.reasoning_summary_part.done", map[string]any{"item_id": e.reasoningItemID, "output_index": e.reasoningIndex, "summary_index": 0, "part": map[string]any{"type": "summary_text", "text": text}})
	if err != nil {
		return nil, err
	}
	item, err := e.responsesEvent("response.output_item.done", map[string]any{"output_index": e.reasoningIndex, "item": e.responseReasoningItem(text)})
	if err != nil {
		return nil, err
	}
	return [][]byte{done, part, item}, nil
}

func (e *streamEncoder) setID(source string) {
	if e.id != "" {
		return
	}
	prefix := "chatcmpl_"
	if e.protocol == Messages {
		prefix = "msg_"
	} else if e.protocol == Responses {
		prefix = "resp_"
	}
	if strings.HasPrefix(source, prefix) {
		e.id = source
		return
	}
	e.id = prefix + strconv.FormatInt(time.Now().UnixNano(), 36)
}

func (e *streamEncoder) ensureStarted() ([][]byte, error) {
	if e.started {
		return nil, nil
	}
	e.started = true
	e.setID("")
	switch e.protocol {
	case Chat:
		value := map[string]any{
			"id": e.id, "object": "chat.completion.chunk", "created": e.created, "model": e.model,
			"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"role": "assistant", "content": ""}, "finish_reason": nil}},
		}
		encoded, err := dataSSE(value)
		return bytesResult(encoded, err)
	case Messages:
		value := map[string]any{
			"type": "message_start",
			"message": map[string]any{
				"id": e.id, "type": "message", "role": "assistant", "model": e.model, "content": []any{},
				"stop_reason": nil, "stop_sequence": nil, "usage": messagesUsage(e.usage),
			},
		}
		encoded, err := encodeSSE("message_start", value)
		return bytesResult(encoded, err)
	case Responses:
		created := e.responseObject("in_progress", []any{}, nil)
		first, err := e.responsesEvent("response.created", map[string]any{"response": created})
		if err != nil {
			return nil, err
		}
		second, err := e.responsesEvent("response.in_progress", map[string]any{"response": created})
		if err != nil {
			return nil, err
		}
		return [][]byte{first, second}, nil
	default:
		return nil, fmt.Errorf("unsupported target stream protocol %q", e.protocol)
	}
}

func (e *streamEncoder) encodeText(delta string) ([][]byte, error) {
	if delta == "" {
		return nil, nil
	}
	e.text.WriteString(delta)
	switch e.protocol {
	case Chat:
		value := map[string]any{
			"id": e.id, "object": "chat.completion.chunk", "created": e.created, "model": e.model,
			"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"content": delta}, "finish_reason": nil}},
		}
		encoded, err := dataSSE(value)
		return bytesResult(encoded, err)
	case Messages:
		var output [][]byte
		if !e.textStarted {
			e.textStarted = true
			e.textIndex = e.nextBlockIndex
			e.nextBlockIndex++
			start, err := encodeSSE("content_block_start", map[string]any{"type": "content_block_start", "index": e.textIndex, "content_block": map[string]any{"type": "text", "text": ""}})
			if err != nil {
				return nil, err
			}
			output = append(output, start)
		}
		encoded, err := encodeSSE("content_block_delta", map[string]any{"type": "content_block_delta", "index": e.textIndex, "delta": map[string]any{"type": "text_delta", "text": delta}})
		if err != nil {
			return nil, err
		}
		return append(output, encoded), nil
	case Responses:
		var output [][]byte
		if !e.textStarted {
			e.textStarted = true
			e.textIndex = e.nextOutputIndex
			e.nextOutputIndex++
			e.textItemID = "msg_" + strconv.FormatInt(time.Now().UnixNano(), 36)
			added, err := e.responsesEvent("response.output_item.added", map[string]any{"output_index": e.textIndex, "item": map[string]any{"id": e.textItemID, "type": "message", "status": "in_progress", "role": "assistant", "content": []any{}}})
			if err != nil {
				return nil, err
			}
			part, err := e.responsesEvent("response.content_part.added", map[string]any{"item_id": e.textItemID, "output_index": e.textIndex, "content_index": 0, "part": map[string]any{"type": "output_text", "text": "", "annotations": []any{}}})
			if err != nil {
				return nil, err
			}
			output = append(output, added, part)
		}
		encoded, err := e.responsesEvent("response.output_text.delta", map[string]any{"item_id": e.textItemID, "output_index": e.textIndex, "content_index": 0, "delta": delta, "logprobs": []any{}})
		if err != nil {
			return nil, err
		}
		return append(output, encoded), nil
	}
	return nil, nil
}

func (e *streamEncoder) encodeToolStart(event streamEvent) ([][]byte, error) {
	tool := e.tool(event)
	if tool.name == "" {
		tool.name = event.Name
	}
	switch e.protocol {
	case Chat:
		value := map[string]any{
			"id": e.id, "object": "chat.completion.chunk", "created": e.created, "model": e.model,
			"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"tool_calls": []any{map[string]any{"index": tool.targetIndex, "id": tool.callID, "type": "function", "function": map[string]any{"name": tool.name, "arguments": ""}}}}, "finish_reason": nil}},
		}
		encoded, err := dataSSE(value)
		return bytesResult(encoded, err)
	case Messages:
		encoded, err := encodeSSE("content_block_start", map[string]any{"type": "content_block_start", "index": tool.blockIndex, "content_block": map[string]any{"type": "tool_use", "id": tool.callID, "name": tool.name, "input": map[string]any{}}})
		return bytesResult(encoded, err)
	case Responses:
		encoded, err := e.responsesEvent("response.output_item.added", map[string]any{"output_index": tool.targetIndex, "item": map[string]any{"id": tool.itemID, "type": "function_call", "status": "in_progress", "arguments": "", "call_id": tool.callID, "name": tool.name}})
		return bytesResult(encoded, err)
	}
	return nil, nil
}

func (e *streamEncoder) encodeToolDelta(event streamEvent) ([][]byte, error) {
	tool := e.tool(event)
	if event.Delta == "" {
		return nil, nil
	}
	tool.arguments.WriteString(event.Delta)
	switch e.protocol {
	case Chat:
		value := map[string]any{
			"id": e.id, "object": "chat.completion.chunk", "created": e.created, "model": e.model,
			"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"tool_calls": []any{map[string]any{"index": tool.targetIndex, "function": map[string]any{"arguments": event.Delta}}}}, "finish_reason": nil}},
		}
		encoded, err := dataSSE(value)
		return bytesResult(encoded, err)
	case Messages:
		encoded, err := encodeSSE("content_block_delta", map[string]any{"type": "content_block_delta", "index": tool.blockIndex, "delta": map[string]any{"type": "input_json_delta", "partial_json": event.Delta}})
		return bytesResult(encoded, err)
	case Responses:
		encoded, err := e.responsesEvent("response.function_call_arguments.delta", map[string]any{"item_id": tool.itemID, "output_index": tool.targetIndex, "delta": event.Delta})
		return bytesResult(encoded, err)
	}
	return nil, nil
}

func (e *streamEncoder) encodeToolEnd(event streamEvent) ([][]byte, error) {
	tool := e.tool(event)
	return e.closeTool(tool)
}

func (e *streamEncoder) closeTool(tool *encodedTool) ([][]byte, error) {
	if tool.closed {
		return nil, nil
	}
	tool.closed = true
	switch e.protocol {
	case Messages:
		encoded, err := encodeSSE("content_block_stop", map[string]any{"type": "content_block_stop", "index": tool.blockIndex})
		return bytesResult(encoded, err)
	case Responses:
		done, err := e.responsesEvent("response.function_call_arguments.done", map[string]any{"item_id": tool.itemID, "output_index": tool.targetIndex, "arguments": tool.arguments.String()})
		if err != nil {
			return nil, err
		}
		item, err := e.responsesEvent("response.output_item.done", map[string]any{"output_index": tool.targetIndex, "item": e.responseToolItem(tool, "completed")})
		if err != nil {
			return nil, err
		}
		return [][]byte{done, item}, nil
	default:
		return nil, nil
	}
}

func (e *streamEncoder) tool(event streamEvent) *encodedTool {
	key := event.CallID
	if key == "" {
		key = fmt.Sprintf("index:%d", event.Index)
	}
	if tool := e.tools[key]; tool != nil {
		if tool.callID == "" && event.CallID != "" {
			tool.callID = event.CallID
		}
		if tool.name == "" && event.Name != "" {
			tool.name = event.Name
		}
		return tool
	}
	callID := event.CallID
	if callID == "" {
		callID = "call_" + strconv.Itoa(len(e.toolOrder))
	}
	tool := &encodedTool{key: key, callID: callID, name: event.Name, targetIndex: len(e.toolOrder), blockIndex: e.nextBlockIndex, itemID: "fc_" + strconv.FormatInt(time.Now().UnixNano()+int64(len(e.toolOrder)), 36)}
	if e.protocol == Messages {
		e.nextBlockIndex++
	}
	if e.protocol == Responses {
		tool.targetIndex = e.nextOutputIndex
		e.nextOutputIndex++
	}
	e.tools[key] = tool
	e.toolOrder = append(e.toolOrder, key)
	return tool
}

func (e *streamEncoder) finish() ([][]byte, error) {
	if e.done {
		return nil, nil
	}
	e.done = true
	var output [][]byte
	started, err := e.ensureStarted()
	if err != nil {
		return nil, err
	}
	output = append(output, started...)
	for _, key := range e.toolOrder {
		closed, err := e.closeTool(e.tools[key])
		if err != nil {
			return nil, err
		}
		output = append(output, closed...)
	}
	if e.stopReason == "" {
		e.stopReason = "stop"
	}
	switch e.protocol {
	case Chat:
		value := map[string]any{
			"id": e.id, "object": "chat.completion.chunk", "created": e.created, "model": e.model,
			"choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": chatStop(e.stopReason)}},
		}
		if usagePresent(e.usage) {
			value["usage"] = chatUsage(e.usage)
		}
		final, err := dataSSE(value)
		if err != nil {
			return nil, err
		}
		output = append(output, final, []byte("data: [DONE]\n\n"))
	case Messages:
		closed, err := e.closeReasoning()
		if err != nil {
			return nil, err
		}
		output = append(output, closed...)
		if e.textStarted && !e.textClosed {
			e.textClosed = true
			closed, err := encodeSSE("content_block_stop", map[string]any{"type": "content_block_stop", "index": e.textIndex})
			if err != nil {
				return nil, err
			}
			output = append(output, closed)
		}
		delta, err := encodeSSE("message_delta", map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": messagesStop(e.stopReason), "stop_sequence": nil}, "usage": messagesUsage(e.usage)})
		if err != nil {
			return nil, err
		}
		stopped, err := encodeSSE("message_stop", map[string]any{"type": "message_stop"})
		if err != nil {
			return nil, err
		}
		output = append(output, delta, stopped)
	case Responses:
		closed, err := e.closeReasoning()
		if err != nil {
			return nil, err
		}
		output = append(output, closed...)
		if e.textStarted && !e.textClosed {
			e.textClosed = true
			text := e.text.String()
			done, err := e.responsesEvent("response.output_text.done", map[string]any{"item_id": e.textItemID, "output_index": e.textIndex, "content_index": 0, "text": text, "logprobs": []any{}})
			if err != nil {
				return nil, err
			}
			part, err := e.responsesEvent("response.content_part.done", map[string]any{"item_id": e.textItemID, "output_index": e.textIndex, "content_index": 0, "part": map[string]any{"type": "output_text", "text": text, "annotations": []any{}}})
			if err != nil {
				return nil, err
			}
			item, err := e.responsesEvent("response.output_item.done", map[string]any{"output_index": e.textIndex, "item": e.responseMessageItem("completed")})
			if err != nil {
				return nil, err
			}
			output = append(output, done, part, item)
		}
		items := e.responseOutput()
		completed, err := e.responsesEvent("response.completed", map[string]any{"response": e.responseObject("completed", items, responsesUsage(e.usage))})
		if err != nil {
			return nil, err
		}
		output = append(output, completed)
	}
	return output, nil
}

func (e *streamEncoder) responsesEvent(name string, fields map[string]any) ([]byte, error) {
	fields["type"] = name
	fields["sequence_number"] = e.sequence
	e.sequence++
	return encodeSSE(name, fields)
}

func (e *streamEncoder) responseObject(status string, output []any, usage any) map[string]any {
	return map[string]any{
		"id": e.id, "object": "response", "created_at": e.created, "status": status, "completed_at": nil,
		"error": nil, "incomplete_details": nil, "instructions": nil, "max_output_tokens": nil, "model": e.model,
		"output": output, "parallel_tool_calls": true, "previous_response_id": nil, "reasoning": nil, "store": false,
		"temperature": nil, "text": map[string]any{"format": map[string]any{"type": "text"}}, "tool_choice": "auto",
		"tools": []any{}, "top_p": nil, "truncation": "disabled", "usage": usage, "metadata": map[string]any{},
	}
}

func (e *streamEncoder) responseMessageItem(status string) map[string]any {
	return map[string]any{"id": e.textItemID, "type": "message", "status": status, "role": "assistant", "content": []any{map[string]any{"type": "output_text", "text": e.text.String(), "annotations": []any{}}}}
}

func (e *streamEncoder) responseReasoningItem(text string) map[string]any {
	summary := make([]any, 0, 1)
	if text != "" {
		summary = append(summary, map[string]any{"type": "summary_text", "text": text})
	}
	return map[string]any{"id": e.reasoningItemID, "type": "reasoning", "summary": summary}
}

func (e *streamEncoder) responseToolItem(tool *encodedTool, status string) map[string]any {
	return map[string]any{"id": tool.itemID, "type": "function_call", "status": status, "arguments": tool.arguments.String(), "call_id": tool.callID, "name": tool.name}
}

func (e *streamEncoder) responseOutput() []any {
	items := make([]any, e.nextOutputIndex)
	if e.reasoningStarted {
		items[e.reasoningIndex] = e.responseReasoningItem(e.reasoning.String())
	}
	if e.textStarted {
		items[e.textIndex] = e.responseMessageItem("completed")
	}
	for _, key := range e.toolOrder {
		tool := e.tools[key]
		items[tool.targetIndex] = e.responseToolItem(tool, "completed")
	}
	return items
}

func (e *streamEncoder) mergeUsage(value Usage) {
	if value.InputTokens != nil {
		e.usage.InputTokens = value.InputTokens
	}
	if value.CacheReadTokens != nil {
		e.usage.CacheReadTokens = value.CacheReadTokens
	}
	if value.CacheWriteTokens != nil {
		e.usage.CacheWriteTokens = value.CacheWriteTokens
	}
	if value.OutputTokens != nil {
		e.usage.OutputTokens = value.OutputTokens
	}
	if value.ReasoningTokens != nil {
		e.usage.ReasoningTokens = value.ReasoningTokens
	}
	if value.TotalTokens != nil {
		e.usage.TotalTokens = value.TotalTokens
	}
}

func bytesResult(value []byte, err error) ([][]byte, error) {
	if err != nil {
		return nil, err
	}
	return [][]byte{value}, nil
}

func intValue(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}

func chatUsage(usage Usage) map[string]any {
	return map[string]any{
		"prompt_tokens": intValue(usage.InputTokens), "completion_tokens": intValue(usage.OutputTokens), "total_tokens": intValue(usage.TotalTokens),
		"prompt_tokens_details":     map[string]any{"cached_tokens": intValue(usage.CacheReadTokens)},
		"completion_tokens_details": map[string]any{"reasoning_tokens": intValue(usage.ReasoningTokens)},
	}
}

func messagesUsage(usage Usage) map[string]any {
	return map[string]any{
		"input_tokens": intValue(usage.InputTokens), "output_tokens": intValue(usage.OutputTokens),
		"cache_read_input_tokens": intValue(usage.CacheReadTokens), "cache_creation_input_tokens": intValue(usage.CacheWriteTokens),
	}
}

func responsesUsage(usage Usage) map[string]any {
	return map[string]any{
		"input_tokens": intValue(usage.InputTokens), "output_tokens": intValue(usage.OutputTokens), "total_tokens": intValue(usage.TotalTokens),
		"input_tokens_details":  map[string]any{"cached_tokens": intValue(usage.CacheReadTokens)},
		"output_tokens_details": map[string]any{"reasoning_tokens": intValue(usage.ReasoningTokens)},
	}
}

func chatStop(reason string) string {
	if reason == "tool_calls" || reason == "length" {
		return reason
	}
	return "stop"
}

func messagesStop(reason string) string {
	switch reason {
	case "tool_calls":
		return "tool_use"
	case "length":
		return "max_tokens"
	default:
		return "end_turn"
	}
}

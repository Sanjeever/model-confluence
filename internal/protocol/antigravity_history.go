package protocol

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
)

// 仅检查公开工具 ID 字段，避免公共解码器限制同协议透传。
func AntigravityHistoryIDs(body []byte, source string) ([]string, error) {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(body, &root); err != nil {
		return nil, err
	}
	field := "messages"
	if source == Responses {
		field = "input"
	}
	var messages []struct {
		Type       string `json:"type"`
		CallID     string `json:"call_id"`
		ToolCallID string `json:"tool_call_id"`
		ToolCalls  []struct {
			ID string `json:"id"`
		} `json:"tool_calls"`
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(root[field], &messages); err != nil {
		return nil, nil
	}
	var ids []string
	add := func(id string) {
		if strings.HasPrefix(id, "call_ag_") {
			ids = append(ids, id)
		}
	}
	for _, message := range messages {
		switch source {
		case Chat:
			add(message.ToolCallID)
			for _, call := range message.ToolCalls {
				add(call.ID)
			}
		case Responses:
			if message.Type == "function_call" || message.Type == "function_call_output" {
				add(message.CallID)
			}
		case Messages:
			var blocks []struct {
				Type      string `json:"type"`
				ID        string `json:"id"`
				ToolUseID string `json:"tool_use_id"`
			}
			if json.Unmarshal(message.Content, &blocks) == nil {
				for _, block := range blocks {
					if block.Type == "tool_use" {
						add(block.ID)
					}
					if block.Type == "tool_result" {
						add(block.ToolUseID)
					}
				}
			}
		}
	}
	return ids, nil
}

func RestoreAntigravityHistory(request *Request, history map[string]AntigravityToolTurn) error {
	for mi := range request.Messages {
		message := &request.Messages[mi]
		var calls []*ToolCall
		var text strings.Builder
		for _, block := range message.Blocks {
			if block.ToolCall != nil {
				calls = append(calls, block.ToolCall)
			}
			if block.Type == "text" {
				text.WriteString(block.Text)
			}
		}
		var turn AntigravityToolTurn
		for _, call := range calls {
			if item, ok := history[call.ID]; ok {
				turn = item
				break
			}
		}
		if len(turn.Calls) == 0 {
			continue
		}
		if message.Role != "assistant" {
			return fmt.Errorf("messages[%d]: Antigravity 工具调用必须属于 assistant", mi)
		}
		for _, block := range message.Blocks {
			if block.Type != "text" && block.Type != "tool_call" {
				return fmt.Errorf("messages[%d]: Antigravity 工具历史包含额外内容", mi)
			}
		}
		if len(calls) != len(turn.Calls) {
			return fmt.Errorf("messages[%d]: Antigravity 工具历史缺少调用或混合了不同响应", mi)
		}
		for i, call := range calls {
			original := turn.Calls[i]
			var actualArgs, originalArgs any
			actualDecoder := json.NewDecoder(bytes.NewReader(call.Arguments))
			actualDecoder.UseNumber()
			if err := actualDecoder.Decode(&actualArgs); err != nil {
				return err
			}
			originalDecoder := json.NewDecoder(bytes.NewReader(original.Arguments))
			originalDecoder.UseNumber()
			if err := originalDecoder.Decode(&originalArgs); err != nil {
				return err
			}
			if call.ID != original.ID || call.Name != original.Name || !reflect.DeepEqual(actualArgs, originalArgs) {
				return fmt.Errorf("messages[%d]: Antigravity 工具历史已修改，无法恢复签名", mi)
			}
			call.UpstreamID = turn.UpstreamIDs[i]
		}
		var content struct {
			Parts []antigravityPart `json:"parts"`
		}
		if err := json.Unmarshal(turn.Content, &content); err != nil {
			return err
		}
		var originalText strings.Builder
		for _, part := range content.Parts {
			if !part.Thought {
				originalText.WriteString(part.Text)
			}
		}
		if text.String() != originalText.String() {
			return fmt.Errorf("messages[%d]: Antigravity 响应文本已修改，无法恢复签名", mi)
		}
		message.AntigravityContent = turn.Content
	}
	return nil
}

package protocol

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

type StreamSummary struct {
	ParseStatus string               `json:"parse_status"`
	Completed   bool                 `json:"completed"`
	StopReason  *string              `json:"stop_reason"`
	Blocks      []StreamSummaryBlock `json:"blocks"`
	Warnings    []string             `json:"warnings"`
}

type StreamSummaryBlock struct {
	Type           string `json:"type"`
	Index          int    `json:"index"`
	Content        string `json:"content,omitempty"`
	CallID         string `json:"call_id,omitempty"`
	Name           string `json:"name,omitempty"`
	Arguments      string `json:"arguments,omitempty"`
	ArgumentsValid bool   `json:"arguments_valid"`
	Complete       bool   `json:"complete"`
}

func SummarizeStream(body []byte, source string) StreamSummary {
	result := StreamSummary{ParseStatus: "unavailable", Blocks: []StreamSummaryBlock{}, Warnings: []string{}}
	if len(bytes.TrimSpace(body)) == 0 {
		result.Warnings = append(result.Warnings, "响应正文为空")
		return result
	}
	if !validStreamProtocol(source) {
		result.Warnings = append(result.Warnings, "不支持聚合协议："+source)
		return result
	}
	if !looksLikeSSE(body) {
		result.Warnings = append(result.Warnings, "响应正文不是 SSE 格式")
		return result
	}

	reader := bufio.NewReader(bytes.NewReader(body))
	decoder := newStreamDecoder(source)
	builder := streamSummaryBuilder{result: result}
	eventNumber := 0
	for {
		event, err := ReadSSEEvent(reader)
		if len(event.Raw) > 0 && (len(bytes.TrimSpace(event.Data)) > 0 || event.Name != "") {
			eventNumber++
			builder.warnProtectedContent(event)
			decoded, decodeErr := decoder.Decode(event)
			if decodeErr != nil {
				builder.result.Warnings = append(builder.result.Warnings, fmt.Sprintf("第 %d 个 SSE 事件无法聚合：%v", eventNumber, decodeErr))
			} else if len(decoded) > 0 {
				builder.recognized = true
				for _, item := range decoded {
					builder.add(item)
				}
			}
		}
		if err != nil {
			if !errors.Is(err, io.EOF) {
				builder.result.Warnings = append(builder.result.Warnings, "读取 SSE 响应失败："+err.Error())
			}
			break
		}
	}

	if !builder.recognized {
		if len(builder.result.Warnings) == 0 {
			builder.result.Warnings = append(builder.result.Warnings, "未识别到可聚合的 SSE 事件")
		}
		return builder.result
	}
	if !builder.result.Completed {
		builder.result.Warnings = append(builder.result.Warnings, "流式响应未收到正常结束事件")
	}
	for index := range builder.result.Blocks {
		block := &builder.result.Blocks[index]
		if block.Type != "tool_call" {
			continue
		}
		block.ArgumentsValid = block.Arguments == "" || json.Valid([]byte(block.Arguments))
	}
	if len(builder.result.Warnings) == 0 {
		builder.result.ParseStatus = "ok"
	} else {
		builder.result.ParseStatus = "partial"
	}
	return builder.result
}

type streamSummaryBuilder struct {
	result     StreamSummary
	recognized bool
}

func (b *streamSummaryBuilder) add(event streamEvent) {
	switch event.Kind {
	case streamText:
		block := b.block("text", event.Index)
		block.Content += event.Delta
	case streamReasoning:
		block := b.block("reasoning", event.Index)
		block.Content += event.Delta
	case streamToolStart:
		b.result.Blocks = append(b.result.Blocks, StreamSummaryBlock{Type: "tool_call", Index: event.Index, CallID: event.CallID, Name: event.Name})
	case streamToolDelta:
		index := b.tool(event.Index, event.CallID)
		if index < 0 {
			b.result.Blocks = append(b.result.Blocks, StreamSummaryBlock{Type: "tool_call", Index: event.Index, CallID: event.CallID})
			index = len(b.result.Blocks) - 1
		}
		b.result.Blocks[index].Arguments += event.Delta
	case streamToolEnd:
		index := b.tool(event.Index, event.CallID)
		if index < 0 {
			b.result.Warnings = append(b.result.Warnings, fmt.Sprintf("工具调用 %q 缺少开始事件", event.CallID))
			return
		}
		b.result.Blocks[index].Complete = true
	case streamFinish:
		if event.StopReason != "" {
			stopReason := event.StopReason
			b.result.StopReason = &stopReason
		}
	case streamDone:
		b.result.Completed = true
	}
}

func (b *streamSummaryBuilder) block(kind string, index int) *StreamSummaryBlock {
	for position := len(b.result.Blocks) - 1; position >= 0; position-- {
		block := &b.result.Blocks[position]
		if block.Type == kind && block.Index == index {
			return block
		}
	}
	b.result.Blocks = append(b.result.Blocks, StreamSummaryBlock{Type: kind, Index: index})
	return &b.result.Blocks[len(b.result.Blocks)-1]
}

func (b *streamSummaryBuilder) warnProtectedContent(event SSEEvent) {
	var value struct {
		Type         string `json:"type"`
		ContentBlock struct {
			Type string `json:"type"`
		} `json:"content_block"`
		Delta struct {
			Type string `json:"type"`
		} `json:"delta"`
	}
	if json.Unmarshal(event.Data, &value) != nil {
		return
	}
	eventType := value.Type
	if eventType == "" {
		eventType = event.Name
	}
	if eventType == "content_block_start" && value.ContentBlock.Type == "redacted_thinking" {
		b.warn("响应包含 redacted_thinking，聚合内容未展示其内容，请查看原文")
	}
	if eventType == "content_block_delta" && value.Delta.Type == "signature_delta" {
		b.warn("响应包含 thinking signature，聚合内容未展示 signature，请查看原文")
	}
}

func (b *streamSummaryBuilder) warn(message string) {
	for _, warning := range b.result.Warnings {
		if warning == message {
			return
		}
	}
	b.result.Warnings = append(b.result.Warnings, message)
}

func (b *streamSummaryBuilder) tool(index int, callID string) int {
	for position := len(b.result.Blocks) - 1; position >= 0; position-- {
		block := b.result.Blocks[position]
		if block.Type != "tool_call" || block.Index != index || block.Complete {
			continue
		}
		if callID == "" || block.CallID == callID {
			return position
		}
	}
	return -1
}

func looksLikeSSE(body []byte) bool {
	return bytes.HasPrefix(body, []byte("data:")) || bytes.Contains(body, []byte("\ndata:")) ||
		bytes.HasPrefix(body, []byte("event:")) || bytes.Contains(body, []byte("\nevent:"))
}

func validStreamProtocol(source string) bool {
	return source == Chat || source == Responses || source == Messages
}

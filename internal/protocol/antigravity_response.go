package protocol

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"time"

	"github.com/Sanjeever/model-confluence/internal/antigravity"
)

var antigravityCallID = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

type antigravityDocument struct {
	ID         string `json:"responseId"`
	Model      string `json:"modelVersion"`
	Candidates []struct {
		Index   int `json:"index"`
		Content struct {
			Parts []antigravityPart `json:"parts"`
		} `json:"content"`
		FinishReason string `json:"finishReason"`
	} `json:"candidates"`
	Usage          json.RawMessage `json:"usageMetadata"`
	PromptFeedback struct {
		BlockReason string `json:"blockReason"`
	} `json:"promptFeedback"`
	Error json.RawMessage `json:"error"`
}

type antigravityPart struct {
	Text         string `json:"text"`
	Thought      bool   `json:"thought"`
	Signature    string `json:"thoughtSignature"`
	FunctionCall *struct {
		ID   string          `json:"id"`
		Name string          `json:"name"`
		Args json.RawMessage `json:"args"`
	} `json:"functionCall"`
}

func (p *antigravityPart) UnmarshalJSON(data []byte) error {
	type part antigravityPart
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	return decoder.Decode((*part)(p))
}

func decodeAntigravityDocument(data []byte) (antigravityDocument, error) {
	var wrapper struct {
		Response json.RawMessage `json:"response"`
	}
	if err := json.Unmarshal(data, &wrapper); err != nil {
		return antigravityDocument{}, fmt.Errorf("解析 Antigravity 响应: %w", err)
	}
	if agPresent(wrapper.Response) {
		data = wrapper.Response
	}
	var document antigravityDocument
	if err := json.Unmarshal(data, &document); err != nil {
		return document, err
	}
	if agPresent(document.Error) {
		return document, antigravity.ClassifyError(document.Error, 200, time.Now())
	}
	if document.PromptFeedback.BlockReason != "" {
		return document, &antigravity.UpstreamError{Status: 400, Code: document.PromptFeedback.BlockReason, Message: "上游安全检查拒绝了请求"}
	}
	return document, nil
}

func antigravityUsage(raw json.RawMessage) (Usage, error) {
	if !agPresent(raw) {
		return Usage{}, nil
	}
	var source struct {
		Input    *int `json:"promptTokenCount"`
		Cache    *int `json:"cachedContentTokenCount"`
		Output   *int `json:"candidatesTokenCount"`
		Thinking *int `json:"thoughtsTokenCount"`
		Total    *int `json:"totalTokenCount"`
	}
	if err := json.Unmarshal(raw, &source); err != nil {
		return Usage{}, err
	}
	for _, value := range []*int{source.Input, source.Cache, source.Output, source.Thinking, source.Total} {
		if value != nil && *value < 0 {
			return Usage{}, fmt.Errorf("Antigravity usage 包含负数")
		}
	}
	if source.Input != nil && source.Cache != nil && *source.Cache > *source.Input {
		return Usage{}, fmt.Errorf("Antigravity 缓存输入超过总输入")
	}
	usage := Usage{InputTokens: source.Input, CacheReadTokens: source.Cache, ReasoningTokens: source.Thinking, TotalTokens: source.Total, Raw: raw}
	// Gemini 将最终答案和思考分别计数；本项目的 output_tokens 包含思考。
	usage.OutputTokens = source.Output
	if source.Output != nil && source.Thinking != nil {
		value := *source.Output + *source.Thinking
		usage.OutputTokens = &value
	}
	if usage.TotalTokens == nil && usage.InputTokens != nil && usage.OutputTokens != nil {
		total := *usage.InputTokens + *usage.OutputTokens
		usage.TotalTokens = &total
	}
	return usage, nil
}

// AntigravityAccountingUsage 将包含缓存的 Google 输入总量转换为非缓存输入。
func AntigravityAccountingUsage(usage Usage) Usage {
	if usage.InputTokens != nil && usage.CacheReadTokens != nil {
		input := *usage.InputTokens - *usage.CacheReadTokens
		usage.InputTokens = &input
	}
	return usage
}

type antigravityStreamState struct {
	response      Response
	done          bool
	toolIndex     int
	signedThought bool
}

func (d *streamDecoder) decodeAntigravity(data []byte) ([]streamEvent, error) {
	state := &d.antigravity
	if state.done {
		return nil, fmt.Errorf("Antigravity 终止后仍有数据")
	}
	if bytes.Equal(bytes.TrimSpace(data), []byte("[DONE]")) {
		return nil, fmt.Errorf("Antigravity 在 finishReason 前返回 DONE")
	}
	document, err := decodeAntigravityDocument(data)
	if err != nil {
		return nil, err
	}
	if len(document.Candidates) > 1 {
		return nil, fmt.Errorf("Antigravity 返回多个候选，无法转换为单响应")
	}
	var events []streamEvent
	if !d.started {
		d.started = true
		state.response.ID = document.ID
		if state.response.ID == "" {
			state.response.ID = newID("agy")
		}
		state.response.Model = document.Model
		events = append(events, streamEvent{Kind: streamStart, ID: state.response.ID, Model: document.Model})
	}
	if len(document.Candidates) == 0 && !agPresent(document.Usage) {
		return nil, fmt.Errorf("Antigravity 响应缺少 candidates 或 usageMetadata")
	}
	for _, candidate := range document.Candidates {
		if candidate.Index != 0 {
			return nil, fmt.Errorf("Antigravity 返回不支持的候选索引 %d", candidate.Index)
		}
		for _, part := range candidate.Content.Parts {
			if part.Signature != "" {
				if state.toolIndex > 0 {
					return nil, fmt.Errorf("Antigravity 工具调用包含不支持的签名历史")
				}
				state.signedThought = true
			}
			if part.FunctionCall != nil {
				if state.signedThought {
					return nil, fmt.Errorf("该 Antigravity 工具调用要求 thoughtSignature 历史回传，当前版本尚不支持；请使用无需签名的模型")
				}
				call := part.FunctionCall
				if call.Name == "" {
					return nil, fmt.Errorf("Antigravity 工具调用缺少名称")
				}
				if !agPresent(call.Args) {
					call.Args = json.RawMessage(`{}`)
				}
				var args map[string]json.RawMessage
				if err := json.Unmarshal(call.Args, &args); err != nil || args == nil {
					return nil, fmt.Errorf("Antigravity 工具参数必须是 JSON 对象")
				}
				id := call.ID
				if id == "" {
					id = newID("call_ag")
				} else if !antigravityCallID.MatchString(id) {
					return nil, fmt.Errorf("Antigravity 工具调用 ID 无法安全回传")
				}
				state.response.Blocks = append(state.response.Blocks, Block{Type: "tool_call", ToolCall: &ToolCall{ID: id, Name: call.Name, Arguments: call.Args}})
				events = append(events, streamEvent{Kind: streamToolStart, Index: state.toolIndex, CallID: id, Name: call.Name}, streamEvent{Kind: streamToolDelta, Index: state.toolIndex, CallID: id, Delta: string(call.Args)}, streamEvent{Kind: streamToolEnd, Index: state.toolIndex, CallID: id})
				state.toolIndex++
			}
			if part.Text != "" {
				kind, blockType := streamText, "text"
				if part.Thought {
					kind, blockType = streamReasoning, "reasoning"
				}
				state.response.Blocks = append(state.response.Blocks, Block{Type: blockType, Text: part.Text})
				events = append(events, streamEvent{Kind: kind, Delta: part.Text})
			}
		}
		if candidate.FinishReason != "" {
			stop := "stop"
			switch candidate.FinishReason {
			case "STOP":
				state.response.StopReason = "end_turn"
			case "MAX_TOKENS":
				stop, state.response.StopReason = "length", "max_tokens"
			case "SAFETY", "RECITATION", "BLOCKLIST", "PROHIBITED_CONTENT", "SPII":
				return nil, &antigravity.UpstreamError{Status: 400, Code: candidate.FinishReason, Message: "上游内容生成被中止"}
			default:
				return nil, fmt.Errorf("Antigravity 未知结束原因 %q", candidate.FinishReason)
			}
			if state.toolIndex > 0 && stop == "stop" {
				stop, state.response.StopReason = "tool_calls", "tool_use"
			}
			state.done = true
			events = append(events, streamEvent{Kind: streamFinish, StopReason: stop})
		}
	}
	usage, err := antigravityUsage(document.Usage)
	if err != nil {
		return nil, err
	}
	mergeExtractedUsage(&state.response.Usage, usage)
	if agPresent(document.Usage) {
		state.response.Usage.Raw = append(json.RawMessage(nil), document.Usage...)
	}
	if usagePresent(usage) {
		events = append(events, streamEvent{Kind: streamUsage, Usage: usage})
	}
	if state.done {
		events = append(events, streamEvent{Kind: streamDone})
	}
	return events, nil
}

// AntigravityResponse 聚合经过同一解码器校验的上游内容，用于非流式客户端。
func (c *StreamConverter) AntigravityResponse() (Response, error) {
	if c.decoder.protocol != Antigravity {
		return Response{}, fmt.Errorf("不是 Antigravity 响应")
	}
	if !c.decoder.antigravity.done {
		return c.decoder.antigravity.response, fmt.Errorf("Antigravity 响应尚未正常结束")
	}
	return c.decoder.antigravity.response, nil
}

package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"time"

	"github.com/Sanjeever/model-confluence/internal/protocol"
)

type CompatibilityInput struct {
	Protocol         string `json:"protocol"`
	UpstreamProtocol string `json:"upstream_protocol"`
	CandidateID      int64  `json:"candidate_id"`
	Scenario         string `json:"scenario"`
	Stream           bool   `json:"stream"`
}

type CompatibilityResult struct {
	CompatibilityInput
	Model          string    `json:"model"`
	Passed         bool      `json:"passed"`
	Message        string    `json:"message"`
	RequestIDs     []string  `json:"request_ids"`
	CheckedAt      time.Time `json:"checked_at"`
	Provider       string    `json:"provider"`
	UpstreamModel  string    `json:"upstream_model"`
	ActualProtocol string    `json:"actual_protocol"`
}

func (h *Handler) CheckCompatibility(ctx context.Context, model string, input CompatibilityInput) CompatibilityResult {
	result := CompatibilityResult{CompatibilityInput: input, Model: model, RequestIDs: []string{}, CheckedAt: time.Now().UTC()}
	fail := func(err error) CompatibilityResult { result.Message = err.Error(); return result }
	if input.Protocol != protocol.Chat && input.Protocol != protocol.Responses && input.Protocol != protocol.Messages {
		return fail(fmt.Errorf("无效入站协议"))
	}
	if input.Scenario != "text" && input.Scenario != "tool" && input.Scenario != "parallel" {
		return fail(fmt.Errorf("无效测试场景"))
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	maxTokens := 512
	req := protocol.Request{Model: model, Stream: input.Stream, MaxOutputTokens: &maxTokens, Messages: []protocol.Message{{Role: "user", Blocks: []protocol.Block{{Type: "text", Text: "请简短回复 mc_test_ok。"}}}}}
	rounds := 1
	toolCount := 1
	if input.Scenario != "text" {
		rounds = 3
		if input.Scenario == "parallel" {
			toolCount = 2
			req.ParallelToolCalls = true
		}
		req.Tools = []protocol.Tool{{Kind: "function", Name: "mc_echo", Description: "返回 value", Parameters: json.RawMessage(`{"type":"object","properties":{"value":{"type":"string"}},"required":["value"],"additionalProperties":false}`)}}
		req.Messages[0].Blocks[0].Text = fmt.Sprintf("请在本轮调用 %d 次 mc_echo，value 分别使用 first 和 second（一次时使用 first），等待工具结果。", toolCount)
	}
	for round := 0; round < rounds; round++ {
		if input.Scenario != "text" {
			req.ToolChoice = protocol.ToolChoice{Mode: "required"}
			if round == rounds-1 {
				req.ToolChoice.Mode = "none"
				req.Messages = append(req.Messages, protocol.Message{Role: "user", Blocks: []protocol.Block{{Type: "text", Text: "工具测试完成，请回复 mc_test_ok，不再调用工具。"}}})
			}
		}
		body, err := protocol.EncodeRequest(req, input.Protocol)
		if err != nil {
			return fail(err)
		}
		status, responseBody, id := h.runAdminRequest(ctx, input.Protocol, body, executionOptions{CandidateID: input.CandidateID, UpstreamProtocol: input.UpstreamProtocol}, nil)
		if id != "" {
			result.RequestIDs = append(result.RequestIDs, id)
		}
		if ctx.Err() != nil {
			return fail(ctx.Err())
		}
		if status < 200 || status >= 300 {
			return fail(fmt.Errorf("HTTP %d，请查看请求记录 %s", status, id))
		}
		detail, err := h.store.RequestDetail(id)
		if err != nil {
			return fail(err)
		}
		if detail.Status != "completed" {
			return fail(fmt.Errorf("请求未正常完成：%s", detail.ErrorMessage))
		}
		if len(detail.Attempts) > 0 {
			a := detail.Attempts[len(detail.Attempts)-1]
			result.Provider, result.UpstreamModel, result.ActualProtocol = a.ProviderName, a.UpstreamModel, a.UpstreamProtocol
		}
		var blocks []protocol.Block
		if input.Stream {
			summary := protocol.SummarizeStream(responseBody, input.Protocol)
			if !summary.Completed || summary.ParseStatus != "ok" {
				return fail(fmt.Errorf("SSE 未完整结束或解析失败：%s", strings.Join(summary.Warnings, "; ")))
			}
			for _, b := range summary.Blocks {
				if b.Type == "text" {
					blocks = append(blocks, protocol.Block{Type: "text", Text: b.Content})
				}
				if b.Type == "tool_call" {
					if !b.ArgumentsValid || !b.Complete {
						return fail(fmt.Errorf("工具调用增量不完整"))
					}
					blocks = append(blocks, protocol.Block{Type: "tool_call", ToolCall: &protocol.ToolCall{ID: b.CallID, Name: b.Name, Arguments: json.RawMessage(b.Arguments)}})
				}
			}
		} else {
			response, err := protocol.DecodeResponse(responseBody, input.Protocol)
			if err != nil {
				return fail(err)
			}
			blocks = response.Blocks
		}
		if round == rounds-1 {
			for _, b := range blocks {
				if b.Type == "text" && strings.TrimSpace(b.Text) != "" {
					result.Passed = true
					result.Message = "通过"
					return result
				}
			}
			return fail(fmt.Errorf("未返回文本内容"))
		}
		calls := []protocol.Block{}
		outputs := []protocol.Block{}
		seen := map[string]bool{}
		for _, b := range blocks {
			if b.Type != "tool_call" {
				continue
			}
			call := b.ToolCall
			if call == nil || call.Name != "mc_echo" || call.ID == "" || seen[call.ID] {
				return fail(fmt.Errorf("工具名称或调用 ID 无效"))
			}
			seen[call.ID] = true
			var args struct {
				Value *string `json:"value"`
			}
			if err := json.Unmarshal(call.Arguments, &args); err != nil || args.Value == nil {
				return fail(fmt.Errorf("工具参数不符合测试 schema"))
			}
			calls = append(calls, b)
			outputs = append(outputs, protocol.Block{Type: "tool_result", ToolResult: &protocol.ToolResult{CallID: call.ID, Content: *args.Value}})
		}
		if len(calls) != toolCount {
			return fail(fmt.Errorf("预期 %d 个工具调用，实际 %d 个", toolCount, len(calls)))
		}
		req.Messages = append(req.Messages, protocol.Message{Role: "assistant", Blocks: blocks}, protocol.Message{Role: "user", Blocks: outputs})
		if round == 0 {
			req.Messages = append(req.Messages, protocol.Message{Role: "user", Blocks: []protocol.Block{{Type: "text", Text: fmt.Sprintf("再次调用 %d 次 mc_echo，value 使用 third 和 fourth（一次时使用 third）。", toolCount)}}})
		}
	}
	return fail(fmt.Errorf("测试未完成"))
}

func (h *Handler) runAdminRequest(ctx context.Context, source string, body []byte, options executionOptions, accessKeyID *int64) (int, []byte, string) {
	path := map[string]string{protocol.Chat: "/v1/chat/completions", protocol.Responses: "/v1/responses", protocol.Messages: "/v1/messages"}[source]
	r := httptest.NewRequestWithContext(withExecution(ctx, options), "POST", path, bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("User-Agent", "model-confluence-admin-debug")
	w := httptest.NewRecorder()
	h.generateAuthorized(w, r, source, accessKeyID, "管理后台调试")
	return w.Code, w.Body.Bytes(), w.Header().Get("X-Request-ID")
}

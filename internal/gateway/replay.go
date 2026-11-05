package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/Sanjeever/model-confluence/internal/protocol"
)

type ReplayInput struct {
	Body             json.RawMessage `json:"body"`
	CandidateID      int64           `json:"candidate_id"`
	UpstreamProtocol string          `json:"upstream_protocol"`
}

type ReplayResult struct {
	RequestID string `json:"request_id"`
	Status    int    `json:"status"`
}

func (h *Handler) ReplayRequest(ctx context.Context, id string, input ReplayInput) (ReplayResult, error) {
	detail, err := h.store.RequestDetail(id)
	if err != nil {
		return ReplayResult{}, err
	}
	if detail.PayloadPruned || detail.Status == "in_progress" {
		return ReplayResult{}, errors.New("载荷已清理或请求尚未结束，无法重放")
	}
	if len(input.Body) == 0 {
		return ReplayResult{}, errors.New("请提供调试请求正文")
	}
	if detail.InboundProtocol != protocol.Chat && detail.InboundProtocol != protocol.Responses && detail.InboundProtocol != protocol.Messages {
		return ReplayResult{}, errors.New("不支持此入站协议")
	}
	keyID, err := h.store.ReplayAccessKey(id)
	if err != nil {
		return ReplayResult{}, err
	}
	meta, err := inspectRequest(input.Body, detail.InboundProtocol)
	if err != nil || meta.Model == "" {
		return ReplayResult{}, errors.New("请求正文或模型无效")
	}
	var headers http.Header
	if detail.RequestHeaders != "" {
		var original http.Header
		if err := json.Unmarshal([]byte(detail.RequestHeaders), &original); err != nil {
			return ReplayResult{}, fmt.Errorf("解析原请求头: %w", err)
		}
		headers = make(http.Header)
		for _, name := range []string{"Anthropic-Version", "Anthropic-Beta", "Openai-Beta"} {
			if values := original.Values(name); len(values) > 0 {
				headers[name] = values
			}
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	status, _, requestID := h.runAdminRequest(ctx, detail.InboundProtocol, input.Body, executionOptions{CandidateID: input.CandidateID, UpstreamProtocol: input.UpstreamProtocol, ReplayOf: id, Headers: headers}, keyID)
	if requestID == "" {
		return ReplayResult{}, fmt.Errorf("无法建立重放请求记录，HTTP %d", status)
	}
	return ReplayResult{requestID, status}, nil
}

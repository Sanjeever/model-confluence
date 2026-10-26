package antigravity

import (
	"encoding/json"
	"fmt"
	"time"
)

type UpstreamError struct {
	Status    int
	Code      string
	Message   string
	RecoverAt *time.Time
}

func (e *UpstreamError) Error() string {
	return fmt.Sprintf("Antigravity 上游失败（%s，HTTP %d）: %s", e.Code, e.Status, e.Message)
}

func ClassifyError(body []byte, status int, now time.Time) *UpstreamError {
	var envelope struct {
		Error json.RawMessage `json:"error"`
	}
	if json.Unmarshal(body, &envelope) == nil && len(envelope.Error) > 0 {
		body = envelope.Error
	}
	var payload struct {
		Code    int    `json:"code"`
		Status  string `json:"status"`
		Message string `json:"message"`
		Details []struct {
			Type       string `json:"@type"`
			RetryDelay string `json:"retryDelay"`
			Reason     string `json:"reason"`
		} `json:"details"`
	}
	result := &UpstreamError{Status: status, Code: "upstream_error", Message: "上游错误响应格式无效"}
	if err := json.Unmarshal(body, &payload); err != nil {
		return result
	}
	result.Message = payload.Message
	if payload.Status != "" {
		result.Code = payload.Status
	}
	if status >= 200 && status < 300 {
		if payload.Code >= 400 && payload.Code <= 599 {
			result.Status = payload.Code
		} else {
			result.Status = 502
		}
	}
	for _, detail := range payload.Details {
		if detail.Type == "type.googleapis.com/google.rpc.RetryInfo" {
			if delay, err := time.ParseDuration(detail.RetryDelay); err == nil && delay >= 0 {
				recoverAt := now.Add(delay)
				result.RecoverAt = &recoverAt
			}
		}
		if detail.Type == "type.googleapis.com/google.rpc.ErrorInfo" && (detail.Reason == "MODEL_NOT_FOUND" || detail.Reason == "MODEL_NOT_SUPPORTED") {
			result.Code = detail.Reason
		}
	}
	return result
}

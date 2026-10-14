package codex

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const TokenEndpoint = "https://auth.openai.com/oauth/token"
const OAuthClientID = "app_EMoamEEZ73f0CkXaXp7hrann"

type RefreshError struct {
	Code      string
	Status    int
	Permanent bool
}

func (e *RefreshError) Error() string {
	return fmt.Sprintf("OAuth 刷新失败（HTTP %d，%s）", e.Status, e.Code)
}

// Refresh 不向错误或模型日志暴露认证服务返回的令牌载荷。
func Refresh(ctx context.Context, client *http.Client, endpoint string, old Auth) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	body, err := json.Marshal(map[string]string{"grant_type": "refresh_token", "client_id": OAuthClientID, "refresh_token": old.Tokens.RefreshToken})
	if err != nil {
		return nil, err
	}
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	r.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(r)
	if err != nil {
		return nil, fmt.Errorf("OAuth 刷新请求失败: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("OAuth 刷新响应读取失败: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var payload struct {
			Error json.RawMessage `json:"error"`
			Code  string          `json:"code"`
		}
		if err := json.Unmarshal(data, &payload); err != nil {
			return nil, &RefreshError{Status: resp.StatusCode, Code: "invalid_response"}
		}
		var code string
		if len(payload.Error) > 0 {
			if payload.Error[0] == '"' {
				if err := json.Unmarshal(payload.Error, &code); err != nil {
					return nil, err
				}
			} else {
				var detail struct {
					Code string `json:"code"`
					Type string `json:"type"`
				}
				if err := json.Unmarshal(payload.Error, &detail); err != nil {
					return nil, &RefreshError{Status: resp.StatusCode, Code: "invalid_response"}
				}
				code = detail.Code
				if code == "" {
					code = detail.Type
				}
			}
		}
		if code == "" {
			code = payload.Code
		}
		permanent := code == "refresh_token_expired" || code == "refresh_token_reused" || code == "refresh_token_invalidated" || code == "invalid_grant"
		if !permanent {
			code = "temporary_error"
		}
		return nil, &RefreshError{Status: resp.StatusCode, Code: code, Permanent: permanent}
	}
	var result Tokens
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("OAuth 刷新响应格式无效")
	}
	if result.AccessToken == "" {
		return nil, fmt.Errorf("OAuth 刷新响应缺少访问令牌")
	}
	if result.IDToken == "" {
		result.IDToken = old.Tokens.IDToken
	}
	if result.RefreshToken == "" {
		result.RefreshToken = old.Tokens.RefreshToken
	}
	result.AccountID = old.Identity.AccountID
	updated, err := old.Refreshed(result, time.Now())
	if err != nil {
		return nil, err
	}
	a, err := ParseAuth(updated)
	if err != nil {
		return nil, err
	}
	if a.Identity.UserID != old.Identity.UserID || a.Identity.AccountID != old.Identity.AccountID {
		return nil, fmt.Errorf("刷新后用户或工作区身份不匹配")
	}
	return updated, nil
}

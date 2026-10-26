package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const LoginRedirect = "http://127.0.0.1:1455/auth/callback"

func AuthorizationURL(state, challenge, redirect string) string {
	query := url.Values{
		"client_id": {OAuthClientID}, "response_type": {"code"}, "redirect_uri": {redirect},
		"scope": {"openid profile email offline_access"}, "state": {state},
		"code_challenge": {challenge}, "code_challenge_method": {"S256"},
		"id_token_add_organizations": {"true"}, "codex_cli_simplified_flow": {"true"}, "originator": {"codex_cli_rs"},
	}
	return "https://auth.openai.com/oauth/authorize?" + query.Encode()
}

// ExchangeLogin 返回与文件导入相同的凭据格式，不落地临时 auth.json。
func ExchangeLogin(ctx context.Context, client *http.Client, code, verifier, redirect string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	form := url.Values{"grant_type": {"authorization_code"}, "client_id": {OAuthClientID}, "code": {code}, "code_verifier": {verifier}, "redirect_uri": {redirect}}
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, TokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := client.Do(r)
	if err != nil {
		return nil, fmt.Errorf("Codex 登录令牌交换失败: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Codex 登录令牌交换返回 HTTP %d，请重新登录", response.StatusCode)
	}
	var tokens Tokens
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&tokens); err != nil {
		return nil, fmt.Errorf("Codex 登录令牌响应格式无效")
	}
	data, err := json.Marshal(struct {
		AuthMode    string    `json:"auth_mode"`
		Tokens      Tokens    `json:"tokens"`
		LastRefresh time.Time `json:"last_refresh"`
	}{"chatgpt", tokens, time.Now().UTC()})
	if err != nil {
		return nil, err
	}
	auth, err := ParseAuth(data)
	if err != nil {
		return nil, err
	}
	if !auth.ExpiresAt.After(time.Now()) {
		return nil, fmt.Errorf("Codex 登录令牌已过期，请重新登录")
	}
	return data, nil
}

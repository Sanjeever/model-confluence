package antigravity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const LoginRedirect = "http://127.0.0.1:51121/oauth-callback"

func (c OAuthClient) AuthorizationURL(state, challenge, redirect string) string {
	query := url.Values{
		"client_id": {c.ID}, "response_type": {"code"}, "redirect_uri": {redirect},
		"scope": {"openid https://www.googleapis.com/auth/userinfo.email https://www.googleapis.com/auth/cloud-platform"},
		"state": {state}, "code_challenge": {challenge}, "code_challenge_method": {"S256"},
		"access_type": {"offline"}, "prompt": {"consent"},
	}
	return "https://accounts.google.com/o/oauth2/v2/auth?" + query.Encode()
}

func ExchangeLogin(ctx context.Context, client *http.Client, oauth OAuthClient, code, verifier, redirect string) (Credential, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	form := url.Values{"grant_type": {"authorization_code"}, "client_id": {oauth.ID}, "client_secret": {oauth.Secret}, "code": {code}, "code_verifier": {verifier}, "redirect_uri": {redirect}}
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, TokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return Credential{}, err
	}
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := client.Do(r)
	if err != nil {
		return Credential{}, fmt.Errorf("Google 登录令牌交换失败: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return Credential{}, fmt.Errorf("Google 登录令牌交换返回 HTTP %d，请确认客户端与回调配置", response.StatusCode)
	}
	var token struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&token); err != nil {
		return Credential{}, errors.New("Google 登录令牌响应格式无效")
	}
	if token.AccessToken == "" || token.RefreshToken == "" || token.ExpiresIn <= 0 {
		return Credential{}, errors.New("Google 未返回有效 access_token、refresh_token 或 expires_in，请重新授权离线访问")
	}
	now := time.Now().UTC()
	credential := Credential{ClientID: oauth.ID, AccessToken: token.AccessToken, RefreshToken: token.RefreshToken, ExpiresAt: now.Add(time.Duration(token.ExpiresIn) * time.Second), LastRefresh: now}
	return Discover(ctx, client, credential)
}

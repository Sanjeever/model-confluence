package codex

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const ResponsesEndpoint = "https://chatgpt.com/backend-api/codex/responses"

type Tokens struct {
	IDToken      string `json:"id_token"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	AccountID    string `json:"account_id"`
}

type Identity struct {
	UserID    string `json:"user_id"`
	AccountID string `json:"account_id"`
	Email     string `json:"email,omitempty"`
	Plan      string `json:"plan,omitempty"`
}

type Auth struct {
	Tokens      Tokens
	Identity    Identity
	ExpiresAt   time.Time
	LastRefresh *time.Time
	raw         map[string]json.RawMessage
}

// JWT 仅用于读取元数据；不验证签名，也不代表上游鉴权成功。
func tokenMetadata(token string) (Identity, jwt.MapClaims, error) {
	claims := jwt.MapClaims{}
	if _, _, err := jwt.NewParser().ParseUnverified(token, claims); err != nil {
		return Identity{}, nil, fmt.Errorf("令牌格式无效")
	}
	value := func(m map[string]any, key string) string { v, _ := m[key].(string); return v }
	account, _ := claims["https://api.openai.com/auth"].(map[string]any)
	i := Identity{UserID: value(account, "chatgpt_user_id"), AccountID: value(account, "chatgpt_account_id"), Email: value(claims, "email"), Plan: value(account, "chatgpt_plan_type")}
	if i.UserID == "" {
		i.UserID = value(account, "user_id")
	}
	if i.UserID == "" {
		i.UserID = value(claims, "sub")
	}
	if i.Email == "" {
		profile, _ := claims["https://api.openai.com/profile"].(map[string]any)
		i.Email = value(profile, "email")
	}
	if i.UserID == "" || i.AccountID == "" {
		return i, nil, fmt.Errorf("令牌缺少用户或工作区身份")
	}
	if fedramp, _ := account["chatgpt_account_is_fedramp"].(bool); fedramp {
		return i, nil, fmt.Errorf("不支持 FedRAMP 工作区")
	}
	return i, claims, nil
}

func ParseAuth(data []byte) (Auth, error) {
	var a Auth
	var file struct {
		AuthMode    string     `json:"auth_mode"`
		APIKey      *string    `json:"OPENAI_API_KEY"`
		Tokens      Tokens     `json:"tokens"`
		LastRefresh *time.Time `json:"last_refresh"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		return a, fmt.Errorf("auth.json 格式无效: %w", err)
	}
	if file.AuthMode != "" && file.AuthMode != "chatgpt" || file.APIKey != nil && *file.APIKey != "" {
		return a, fmt.Errorf("仅支持 ChatGPT OAuth auth.json")
	}
	t := file.Tokens
	if strings.TrimSpace(t.IDToken) == "" || strings.TrimSpace(t.AccessToken) == "" || strings.TrimSpace(t.RefreshToken) == "" {
		return a, fmt.Errorf("auth.json 缺少必要令牌")
	}
	id, _, err := tokenMetadata(t.IDToken)
	if err != nil {
		return a, err
	}
	access, claims, err := tokenMetadata(t.AccessToken)
	if err != nil {
		return a, err
	}
	if access.UserID != id.UserID || access.AccountID != id.AccountID || t.AccountID != "" && t.AccountID != id.AccountID {
		return a, fmt.Errorf("用户或工作区身份不匹配")
	}
	exp, err := claims.GetExpirationTime()
	if err != nil || exp == nil {
		return a, fmt.Errorf("访问令牌缺少有效到期时间")
	}
	if err := json.Unmarshal(data, &a.raw); err != nil {
		return a, err
	}
	a.Tokens, a.Identity, a.ExpiresAt, a.LastRefresh = t, id, exp.Time, file.LastRefresh
	return a, nil
}

func (a Auth) Refreshed(tokens Tokens, now time.Time) ([]byte, error) {
	var tokenFields map[string]json.RawMessage
	if err := json.Unmarshal(a.raw["tokens"], &tokenFields); err != nil {
		return nil, err
	}
	updated, err := json.Marshal(tokens)
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(updated, &fields); err != nil {
		return nil, err
	}
	for k, v := range fields {
		tokenFields[k] = v
	}
	tokenJSON, err := json.Marshal(tokenFields)
	if err != nil {
		return nil, err
	}
	a.raw["tokens"] = tokenJSON
	a.raw["last_refresh"], err = json.Marshal(now.UTC())
	if err != nil {
		return nil, err
	}
	return json.Marshal(a.raw)
}

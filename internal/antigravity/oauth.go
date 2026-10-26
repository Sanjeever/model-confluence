package antigravity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

const (
	BaseURL          = "https://daily-cloudcode-pa.googleapis.com/v1internal"
	StreamEndpoint   = BaseURL + ":streamGenerateContent?alt=sse"
	TokenEndpoint    = "https://oauth2.googleapis.com/token"
	UserInfoEndpoint = "https://www.googleapis.com/oauth2/v2/userinfo"
)

// OAuthClient 必须与签发 refresh token 的客户端一致；客户端凭据只在运行时配置。
type OAuthClient struct {
	ID     string
	Secret string
}

type Identity struct {
	Subject   string `json:"subject"`
	Email     string `json:"email"`
	ProjectID string `json:"project_id"`
}

type Credential struct {
	Identity
	ClientID     string    `json:"client_id"`
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
	LastRefresh  time.Time `json:"last_refresh"`
}

type ImportEntry struct {
	Email        string `json:"email"`
	RefreshToken string `json:"refresh_token"`
	ClientID     string `json:"client_id"`
}

// ParseImport 接受 Manager 导出、单个导出项及完整账号文件，不信任文件中的账号身份。
func ParseImport(data []byte) ([]ImportEntry, error) {
	var document struct {
		ImportEntry
		Accounts []ImportEntry `json:"accounts"`
		Token    *ImportEntry  `json:"token"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		return nil, fmt.Errorf("解析 Antigravity 账号文件: %w", err)
	}
	entries := document.Accounts
	if entries == nil {
		entry := document.ImportEntry
		if document.Token != nil {
			entry = *document.Token
		}
		entries = []ImportEntry{entry}
	}
	if len(entries) == 0 || len(entries) > 100 {
		return nil, errors.New("账号文件须包含 1 至 100 个账号")
	}
	for i := range entries {
		entries[i].RefreshToken = strings.TrimSpace(entries[i].RefreshToken)
		if entries[i].RefreshToken == "" {
			return nil, fmt.Errorf("accounts[%d].refresh_token 不能为空", i)
		}
	}
	return entries, nil
}

type RefreshError struct {
	Code      string
	Permanent bool
}

func (e *RefreshError) Error() string { return "Google OAuth 刷新失败: " + e.Code }

func Refresh(ctx context.Context, client *http.Client, oauth OAuthClient, current Credential) (Credential, error) {
	if oauth.ID == "" || oauth.Secret == "" {
		return current, errors.New("请配置 MODEL_CONFLUENCE_ANTIGRAVITY_OAUTH_CLIENT_ID 和 MODEL_CONFLUENCE_ANTIGRAVITY_OAUTH_CLIENT_SECRET")
	}
	if current.ClientID != "" && current.ClientID != oauth.ID {
		return current, errors.New("账号 OAuth client_id 与运行配置不一致")
	}
	form := url.Values{"client_id": {oauth.ID}, "client_secret": {oauth.Secret}, "refresh_token": {current.RefreshToken}, "grant_type": {"refresh_token"}}
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, TokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return current, err
	}
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := client.Do(r)
	if err != nil {
		return current, fmt.Errorf("请求 Google OAuth: %w", err)
	}
	defer response.Body.Close()
	var token struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
		Error        string `json:"error"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&token); err != nil {
		return current, fmt.Errorf("解析 Google OAuth 响应 (HTTP %d)", response.StatusCode)
	}
	if response.StatusCode != http.StatusOK {
		code := token.Error
		if code == "" {
			code = fmt.Sprintf("HTTP %d", response.StatusCode)
		}
		return current, &RefreshError{Code: code, Permanent: code == "invalid_grant"}
	}
	if token.AccessToken == "" || token.ExpiresIn <= 0 {
		return current, errors.New("Google OAuth 响应缺少有效 access_token 或 expires_in")
	}
	current.AccessToken = token.AccessToken
	if token.RefreshToken != "" {
		current.RefreshToken = token.RefreshToken
	}
	current.ClientID = oauth.ID
	current.LastRefresh = time.Now().UTC()
	current.ExpiresAt = current.LastRefresh.Add(time.Duration(token.ExpiresIn) * time.Second)
	return current, nil
}

func ApplyHeaders(header http.Header, token string) {
	header.Del("x-api-key")
	header.Del("Cookie")
	header.Set("Authorization", "Bearer "+token)
	header.Set("Content-Type", "application/json")
	header.Set("Accept", "text/event-stream")
	header.Set("User-Agent", "antigravity")
}

func rpc(ctx context.Context, client *http.Client, token, method string, body any, result any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, BaseURL+":"+method, strings.NewReader(string(data)))
	if err != nil {
		return err
	}
	ApplyHeaders(r.Header, token)
	return decodeResponse(client, r, result)
}

func decodeResponse(client *http.Client, r *http.Request, result any) error {
	response, err := client.Do(r)
	if err != nil {
		return fmt.Errorf("请求 Antigravity 元数据: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("Antigravity 元数据接口返回 HTTP %d", response.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 8<<20)).Decode(result); err != nil {
		return fmt.Errorf("解析 Antigravity 元数据: %w", err)
	}
	return nil
}

func Discover(ctx context.Context, client *http.Client, credential Credential) (Credential, error) {
	r, err := http.NewRequestWithContext(ctx, http.MethodGet, UserInfoEndpoint, nil)
	if err != nil {
		return credential, err
	}
	r.Header.Set("Authorization", "Bearer "+credential.AccessToken)
	var user struct {
		ID       string `json:"id"`
		Email    string `json:"email"`
		Verified bool   `json:"verified_email"`
	}
	if err := decodeResponse(client, r, &user); err != nil {
		return credential, err
	}
	if user.ID == "" || user.Email == "" || !user.Verified {
		return credential, errors.New("Google 未返回已验证的账号身份")
	}
	var project struct {
		ID string `json:"cloudaicompanionProject"`
	}
	if err := rpc(ctx, client, credential.AccessToken, "loadCodeAssist", map[string]any{"metadata": map[string]string{"ideType": "ANTIGRAVITY"}}, &project); err != nil {
		return credential, err
	}
	if project.ID == "" {
		return credential, errors.New("账号未返回可用的 cloudaicompanionProject，请先在 Antigravity 完成账号初始化")
	}
	credential.Identity = Identity{Subject: user.ID, Email: user.Email, ProjectID: project.ID}
	return credential, nil
}

func Models(ctx context.Context, client *http.Client, credential Credential) ([]string, error) {
	var result struct {
		Models map[string]json.RawMessage `json:"models"`
	}
	if err := rpc(ctx, client, credential.AccessToken, "fetchAvailableModels", map[string]string{"project": credential.ProjectID}, &result); err != nil {
		return nil, err
	}
	if result.Models == nil {
		return nil, errors.New("Antigravity 模型列表缺少 models")
	}
	names := make([]string, 0, len(result.Models))
	for name := range result.Models {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

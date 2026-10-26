package oauthlogin

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const Lifetime = 5 * time.Minute

var ErrBusy = errors.New("已有待完成的登录，请先完成或取消")
var ErrNotFound = errors.New("登录会话不存在或已清理")
var ErrAlreadyHandled = errors.New("登录已处理或过期，请重新发起登录")
var ErrInvalidCallback = errors.New("回调 URL 无效或与本次登录的回调地址不匹配")
var ErrStateMismatch = errors.New("登录状态不匹配，请提交本次授权完成后的完整回调 URL")

// Exchange 只执行网络操作；返回的保存操作在取消、超时检查后串行执行。
type Exchange func(ctx context.Context, code, verifier, redirect string) (func() (int64, error), error)

type Config struct {
	Redirect  string
	Issuer    string
	Authorize func(state, challenge, redirect string) string
	Exchange  Exchange
}

type Status struct {
	ID         string    `json:"id"`
	ProviderID int64     `json:"provider_id"`
	URL        string    `json:"url,omitempty"`
	Redirect   string    `json:"redirect_uri"`
	ExpiresAt  time.Time `json:"expires_at"`
	Status     string    `json:"status"`
	Error      string    `json:"error,omitempty"`
	AccountID  int64     `json:"account_id,omitempty"`
}

type flow struct {
	Status
	owner    string
	state    string
	verifier string
	exchange Exchange
	ctx      context.Context
	cancel   context.CancelFunc
	server   *http.Server
	listener net.Listener
	issuer   string
}

type Manager struct {
	mu    sync.Mutex
	flows map[string]*flow
}

func NewManager() *Manager { return &Manager{flows: make(map[string]*flow)} }

func randomString() (string, error) {
	data := make([]byte, 32)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func (m *Manager) Start(owner string, providerID int64, config Config) (Status, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, f := range m.flows {
		if time.Now().After(f.ExpiresAt.Add(time.Minute)) {
			delete(m.flows, id)
			continue
		}
		if f.owner == owner && active(f) {
			return Status{}, ErrBusy
		}
	}
	redirect, err := url.Parse(config.Redirect)
	if err != nil || redirect.Scheme != "http" || redirect.Hostname() != "127.0.0.1" || redirect.Port() == "" {
		return Status{}, errors.New("OAuth 回调必须是带端口的 HTTP IPv4 回环地址")
	}
	id, err := randomString()
	if err != nil {
		return Status{}, err
	}
	state, err := randomString()
	if err != nil {
		return Status{}, err
	}
	verifier, err := randomString()
	if err != nil {
		return Status{}, err
	}
	listener, err := net.Listen("tcp4", redirect.Host)
	if err != nil {
		return Status{}, fmt.Errorf("无法监听 OAuth 回调 %s，请释放该端口: %w", redirect.Host, err)
	}
	redirect.Host = listener.Addr().String()
	ctx, cancel := context.WithTimeout(context.Background(), Lifetime)
	challenge := sha256.Sum256([]byte(verifier))
	f := &flow{Status: Status{ID: id, ProviderID: providerID, URL: config.Authorize(state, base64.RawURLEncoding.EncodeToString(challenge[:]), redirect.String()), Redirect: redirect.String(), ExpiresAt: time.Now().UTC().Add(Lifetime), Status: "pending"}, owner: owner, state: state, verifier: verifier, exchange: config.Exchange, ctx: ctx, cancel: cancel, listener: listener, issuer: config.Issuer}
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+redirect.Path, func(w http.ResponseWriter, r *http.Request) { m.callback(f, w, r) })
	f.server = &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	m.flows[id] = f
	go func() {
		if err := f.server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) && !errors.Is(err, net.ErrClosed) {
			m.mu.Lock()
			if active(f) {
				m.finishLocked(f, "failed", "OAuth 回调服务停止", 0)
			}
			m.mu.Unlock()
		}
	}()
	go func() {
		<-ctx.Done()
		m.mu.Lock()
		if active(f) {
			m.finishLocked(f, "expired", "登录已超时，请重新发起", 0)
		}
		m.mu.Unlock()
	}()
	return f.Status, nil
}

func active(f *flow) bool { return f.Status.Status == "pending" || f.Status.Status == "exchanging" }

func (m *Manager) finishLocked(f *flow, status, message string, accountID int64) {
	f.Status.Status, f.Error, f.AccountID = status, message, accountID
	f.URL, f.state, f.verifier, f.exchange = "", "", "", nil
	f.cancel()
	_ = f.listener.Close()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = f.server.Shutdown(ctx)
	}()
}

func (m *Manager) callback(f *flow, w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		http.Error(w, ErrInvalidCallback.Error(), 400)
		return
	}
	m.mu.Lock()
	complete, err := m.acceptLocked(f, query)
	m.mu.Unlock()
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, ErrAlreadyHandled) {
			status = http.StatusConflict
		}
		http.Error(w, err.Error(), status)
		return
	}
	if complete == nil {
		_, _ = w.Write([]byte("授权未完成，请返回模汇。"))
		return
	}
	_, _ = w.Write([]byte("授权回调已收到，请返回模汇查看登录结果。"))
	go complete()
}

// Submit 只解析地址中的授权结果，不向该地址发起 HTTP 请求。
func (m *Manager) Submit(owner string, providerID int64, id, callbackURL string) (Status, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	f, ok := m.flows[id]
	if !ok || f.owner != owner || f.ProviderID != providerID || time.Now().After(f.ExpiresAt.Add(time.Minute)) {
		return Status{}, ErrNotFound
	}
	callbackURL = strings.TrimSpace(callbackURL)
	if len(callbackURL) > 16<<10 {
		return Status{}, ErrInvalidCallback
	}
	parsed, err := url.Parse(callbackURL)
	if err != nil {
		return Status{}, ErrInvalidCallback
	}
	expected, err := url.Parse(f.Redirect)
	if err != nil || parsed.Scheme != expected.Scheme || parsed.Host != expected.Host || parsed.Path != expected.Path || parsed.User != nil || parsed.Fragment != "" || parsed.Opaque != "" {
		return Status{}, ErrInvalidCallback
	}
	query, err := url.ParseQuery(parsed.RawQuery)
	if err != nil {
		return Status{}, ErrInvalidCallback
	}
	complete, err := m.acceptLocked(f, query)
	if err != nil {
		return Status{}, err
	}
	status := f.Status
	if complete != nil {
		go complete()
	}
	return status, nil
}

// 调用方持有会话锁，使自动回调和手动提交只能有一次进入令牌交换。
func (m *Manager) acceptLocked(f *flow, query url.Values) (func(), error) {
	if f.Status.Status != "pending" || f.ctx.Err() != nil {
		return nil, ErrAlreadyHandled
	}
	if len(query["state"]) != 1 || f.state == "" || subtle.ConstantTimeCompare([]byte(query.Get("state")), []byte(f.state)) != 1 {
		return nil, ErrStateMismatch
	}
	if len(query["iss"]) > 1 || len(query["error"]) > 1 || len(query["code"]) > 1 {
		return nil, ErrInvalidCallback
	}
	if issuer := query.Get("iss"); issuer != "" && (f.issuer == "" || issuer != f.issuer) {
		return nil, errors.New("授权服务 issuer 与本次登录不匹配")
	}
	if query.Get("error") != "" {
		if query.Get("code") != "" {
			return nil, ErrInvalidCallback
		}
		m.finishLocked(f, "failed", "授权未完成，请重新登录", 0)
		return nil, nil
	}
	if query.Get("code") == "" {
		return nil, errors.New("回调 URL 缺少授权码")
	}
	f.Status.Status = "exchanging"
	exchange, verifier, redirect, code := f.exchange, f.verifier, f.Redirect, query.Get("code")
	return func() { m.complete(f, exchange, code, verifier, redirect) }, nil
}

func (m *Manager) complete(f *flow, exchange Exchange, code, verifier, redirect string) {
	save, err := exchange(f.ctx, code, verifier, redirect)
	m.mu.Lock()
	defer m.mu.Unlock()
	if !active(f) {
		return
	}
	if f.ctx.Err() != nil {
		m.finishLocked(f, "expired", "登录已超时，请重新发起", 0)
		return
	}
	if err != nil {
		m.finishLocked(f, "failed", err.Error(), 0)
		return
	}
	accountID, err := save()
	if err != nil {
		m.finishLocked(f, "failed", err.Error(), 0)
		return
	}
	m.finishLocked(f, "succeeded", "", accountID)
}

func (m *Manager) Get(owner string, providerID int64, id string) (Status, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	f, ok := m.flows[id]
	if !ok || f.owner != owner || f.ProviderID != providerID || time.Now().After(f.ExpiresAt.Add(time.Minute)) {
		return Status{}, false
	}
	return f.Status, true
}

func (m *Manager) Cancel(owner string, providerID int64, id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	f, ok := m.flows[id]
	if !ok || f.owner != owner || f.ProviderID != providerID {
		return false
	}
	if active(f) {
		m.finishLocked(f, "cancelled", "登录已取消", 0)
	}
	return true
}

// CancelOwner 的空 owner 表示取消全部登录，用于退出或修改管理员密码。
func (m *Manager) CancelOwner(owner string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, f := range m.flows {
		if (owner == "" || f.owner == owner) && active(f) {
			m.finishLocked(f, "cancelled", "登录已取消", 0)
		}
	}
}

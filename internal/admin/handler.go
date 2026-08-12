package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Sanjeever/model-confluence/internal/httpx"
	"github.com/Sanjeever/model-confluence/internal/protocol"
	"github.com/Sanjeever/model-confluence/internal/store"
)

const (
	sessionCookie = "mc_session"
	csrfCookie    = "mc_csrf"
)

type Handler struct {
	store    *store.Store
	clientIP *httpx.ClientIPResolver
	limiter  *loginLimiter
	tester   modelTester
}

type modelTester interface {
	TestModel(context.Context, string, string) (int, []byte, string)
}

func NewHandler(store *store.Store, clientIP *httpx.ClientIPResolver, tester modelTester) *Handler {
	return &Handler{store: store, clientIP: clientIP, limiter: newLoginLimiter(), tester: tester}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/admin/login", h.login)
	mux.Handle("GET /api/admin/session", h.requireSession(http.HandlerFunc(h.session)))
	mux.Handle("POST /api/admin/logout", h.requireSession(h.requireCSRF(http.HandlerFunc(h.logout))))
	mux.Handle("POST /api/admin/change-password", h.requireSession(h.requireCSRF(http.HandlerFunc(h.changePassword))))
	mux.Handle("GET /api/admin/overview", h.requireSession(http.HandlerFunc(h.overview)))
	mux.Handle("GET /api/admin/requests", h.requireSession(http.HandlerFunc(h.listRequests)))
	mux.Handle("GET /api/admin/requests/{id}", h.requireSession(http.HandlerFunc(h.requestDetail)))
	mux.Handle("GET /api/admin/access-keys", h.requireSession(http.HandlerFunc(h.listAccessKeys)))
	mux.Handle("POST /api/admin/access-keys", h.requireSession(h.requireCSRF(http.HandlerFunc(h.createAccessKey))))
	mux.Handle("PATCH /api/admin/access-keys/{id}", h.requireSession(h.requireCSRF(http.HandlerFunc(h.updateAccessKey))))
	mux.Handle("PUT /api/admin/access-keys/{id}", h.requireSession(h.requireCSRF(http.HandlerFunc(h.editAccessKey))))
	mux.Handle("DELETE /api/admin/access-keys/{id}", h.requireSession(h.requireCSRF(http.HandlerFunc(h.deleteAccessKey))))
	mux.Handle("GET /api/admin/providers", h.requireSession(http.HandlerFunc(h.listProviders)))
	mux.Handle("POST /api/admin/providers", h.requireSession(h.requireCSRF(http.HandlerFunc(h.createProvider))))
	mux.Handle("PATCH /api/admin/providers/{id}", h.requireSession(h.requireCSRF(http.HandlerFunc(h.updateProvider))))
	mux.Handle("PUT /api/admin/providers/{id}", h.requireSession(h.requireCSRF(http.HandlerFunc(h.editProvider))))
	mux.Handle("DELETE /api/admin/providers/{id}", h.requireSession(h.requireCSRF(http.HandlerFunc(h.deleteProvider))))
	mux.Handle("GET /api/admin/models", h.requireSession(http.HandlerFunc(h.listModels)))
	mux.Handle("POST /api/admin/models", h.requireSession(h.requireCSRF(http.HandlerFunc(h.createModel))))
	mux.Handle("POST /api/admin/models/{id}/test", h.requireSession(h.requireCSRF(http.HandlerFunc(h.testModel))))
	mux.Handle("PATCH /api/admin/models/{id}", h.requireSession(h.requireCSRF(http.HandlerFunc(h.updateModel))))
	mux.Handle("PUT /api/admin/models/{id}", h.requireSession(h.requireCSRF(http.HandlerFunc(h.editModel))))
	mux.Handle("DELETE /api/admin/models/{id}", h.requireSession(h.requireCSRF(http.HandlerFunc(h.deleteModel))))
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	ip := h.clientIP.Resolve(r)
	now := time.Now()
	if !h.limiter.allowed(ip, now) {
		httpx.WriteJSON(w, http.StatusTooManyRequests, map[string]string{"error": "登录失败次数过多，请稍后再试"})
		return
	}
	var input struct {
		Password string `json:"password"`
	}
	if !httpx.DecodeJSON(w, r, &input) {
		return
	}
	if err := h.store.CheckAdminPassword(input.Password); err != nil {
		h.limiter.failed(ip, now)
		httpx.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "密码错误"})
		return
	}
	session, err := h.store.CreateSession()
	if err != nil {
		httpx.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "无法创建登录会话"})
		return
	}
	csrf, err := store.NewToken(24)
	if err != nil {
		h.store.DeleteSession(session.Token)
		httpx.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "无法创建登录会话"})
		return
	}
	h.limiter.reset(ip)
	h.setCookie(w, sessionCookie, session.Token, true, time.Until(session.ExpiresAt))
	h.setCookie(w, csrfCookie, csrf, false, time.Until(session.ExpiresAt))
	httpx.WriteJSON(w, http.StatusOK, map[string]bool{"authenticated": true})
}

func (h *Handler) session(w http.ResponseWriter, _ *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, map[string]bool{"authenticated": true})
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	cookie, _ := r.Cookie(sessionCookie)
	if cookie != nil {
		_ = h.store.DeleteSession(cookie.Value)
	}
	h.clearCookie(w, sessionCookie, true)
	h.clearCookie(w, csrfCookie, false)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) changePassword(w http.ResponseWriter, r *http.Request) {
	var input struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if !httpx.DecodeJSON(w, r, &input) {
		return
	}
	if len(input.NewPassword) < 12 {
		httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "新密码至少需要 12 个字符"})
		return
	}
	if err := h.store.CheckAdminPassword(input.CurrentPassword); err != nil {
		httpx.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "当前密码错误"})
		return
	}
	if err := h.store.ResetAdminPassword(input.NewPassword); err != nil {
		httpx.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "无法修改密码"})
		return
	}
	h.clearCookie(w, sessionCookie, true)
	h.clearCookie(w, csrfCookie, false)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) overview(w http.ResponseWriter, r *http.Request) {
	createdFrom, createdTo, err := requestTimeRange(r)
	if err != nil {
		httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	overview, err := h.store.Overview(createdFrom, createdTo)
	if err != nil {
		httpx.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	httpx.WriteJSON(w, http.StatusOK, overview)
}

func (h *Handler) listRequests(w http.ResponseWriter, r *http.Request) {
	page, ok := positiveQuery(w, r, "page", 1, 0)
	if !ok {
		return
	}
	pageSize, ok := positiveQuery(w, r, "page_size", 10, 100)
	if !ok {
		return
	}
	createdFrom, createdTo, err := requestTimeRange(r)
	if err != nil {
		httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	requests, err := h.store.ListRequests(page, pageSize, strings.TrimSpace(r.URL.Query().Get("request_id")), createdFrom, createdTo)
	if err != nil {
		httpx.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	httpx.WriteJSON(w, http.StatusOK, requests)
}

func (h *Handler) requestDetail(w http.ResponseWriter, r *http.Request) {
	detail, err := h.store.RequestDetail(r.PathValue("id"))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			httpx.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "使用记录不存在"})
			return
		}
		httpx.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if detail.InputTokens == nil && detail.CacheReadTokens == nil && detail.OutputTokens == nil {
		for index := len(detail.Attempts) - 1; index >= 0; index-- {
			attempt := detail.Attempts[index]
			if attempt.Status != "completed" || attempt.ResponseBody == "" || attempt.UpstreamProtocol == "" {
				continue
			}
			usage, _, usageErr := protocol.ExtractUsage([]byte(attempt.ResponseBody), attempt.UpstreamProtocol, detail.Stream)
			if usageErr == nil {
				detail.InputTokens = toInt64Pointer(usage.InputTokens)
				detail.CacheReadTokens = toInt64Pointer(usage.CacheReadTokens)
				detail.CacheWriteTokens = toInt64Pointer(usage.CacheWriteTokens)
				detail.OutputTokens = toInt64Pointer(usage.OutputTokens)
				detail.ReasoningTokens = toInt64Pointer(usage.ReasoningTokens)
				detail.TotalTokens = toInt64Pointer(usage.TotalTokens)
			}
			break
		}
	}
	if detail.Stream && !detail.PayloadPruned {
		detail.ResponseSummary = summaryPointer(protocol.SummarizeStream([]byte(detail.ResponseBody), detail.InboundProtocol))
		for index := range detail.Attempts {
			attempt := &detail.Attempts[index]
			if !attempt.PayloadPruned {
				attempt.ResponseSummary = summaryPointer(protocol.SummarizeStream([]byte(attempt.ResponseBody), attempt.UpstreamProtocol))
			}
		}
	}
	httpx.WriteJSON(w, http.StatusOK, detail)
}

func summaryPointer(value protocol.StreamSummary) *protocol.StreamSummary {
	return &value
}

func toInt64Pointer(value *int) *int64 {
	if value == nil {
		return nil
	}
	converted := int64(*value)
	return &converted
}

func (h *Handler) listAccessKeys(w http.ResponseWriter, _ *http.Request) {
	keys, err := h.store.ListAccessKeys()
	if err != nil {
		httpx.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	httpx.WriteJSON(w, http.StatusOK, keys)
}

func (h *Handler) createAccessKey(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Name      string     `json:"name"`
		ExpiresAt *time.Time `json:"expires_at"`
	}
	if !httpx.DecodeJSON(w, r, &input) {
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" {
		httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "名称不能为空"})
		return
	}
	key, err := h.store.CreateAccessKey(input.Name, input.ExpiresAt)
	if err != nil {
		httpx.WriteJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, key)
}

func (h *Handler) updateAccessKey(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "无效的密钥 ID"})
		return
	}
	var input struct {
		Enabled *bool `json:"enabled"`
	}
	if !httpx.DecodeJSON(w, r, &input) {
		return
	}
	if input.Enabled == nil {
		httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "enabled 不能为空"})
		return
	}
	if err := h.store.SetAccessKeyEnabled(id, *input.Enabled); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			httpx.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "访问密钥不存在"})
			return
		}
		httpx.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) editAccessKey(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "无效的密钥 ID"})
		return
	}
	var input struct {
		Name      string     `json:"name"`
		ExpiresAt *time.Time `json:"expires_at"`
		Enabled   bool       `json:"enabled"`
	}
	if !httpx.DecodeJSON(w, r, &input) {
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" {
		httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "名称不能为空"})
		return
	}
	if err := h.store.UpdateAccessKey(id, input.Name, input.ExpiresAt, input.Enabled); err != nil {
		writeStoreError(w, err, "访问密钥不存在")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) deleteAccessKey(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "无效的密钥 ID"})
		return
	}
	result, err := h.store.DeleteAccessKey(id)
	if err != nil {
		writeStoreError(w, err, "访问密钥不存在")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, result)
}

func (h *Handler) listProviders(w http.ResponseWriter, _ *http.Request) {
	providers, err := h.store.ListProviders()
	if err != nil {
		httpx.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	httpx.WriteJSON(w, http.StatusOK, providers)
}

func (h *Handler) createProvider(w http.ResponseWriter, r *http.Request) {
	input, ok := decodeProviderInput(w, r)
	if !ok {
		return
	}
	provider, err := h.store.CreateProvider(input)
	if err != nil {
		httpx.WriteJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, provider)
}

func (h *Handler) updateProvider(w http.ResponseWriter, r *http.Request) {
	h.updateEnabled(w, r, h.store.SetProviderEnabled)
}

func (h *Handler) editProvider(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "无效的供应商 ID"})
		return
	}
	input, ok := decodeProviderInput(w, r)
	if !ok {
		return
	}
	if err := h.store.UpdateProvider(id, input); err != nil {
		writeStoreError(w, err, "供应商不存在")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) deleteProvider(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "无效的供应商 ID"})
		return
	}
	result, err := h.store.DeleteProvider(id)
	if errors.Is(err, store.ErrProviderInUse) {
		httpx.WriteJSON(w, http.StatusConflict, map[string]string{"error": "供应商仍被模型路由使用，请先修改或删除相关模型路由"})
		return
	}
	if err != nil {
		writeStoreError(w, err, "供应商不存在")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, result)
}

func (h *Handler) listModels(w http.ResponseWriter, _ *http.Request) {
	models, err := h.store.ListVirtualModels()
	if err != nil {
		httpx.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	httpx.WriteJSON(w, http.StatusOK, models)
}

func (h *Handler) createModel(w http.ResponseWriter, r *http.Request) {
	input, ok := decodeModelInput(w, r)
	if !ok {
		return
	}
	model, err := h.store.CreateVirtualModel(input)
	if err != nil {
		httpx.WriteJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, model)
}

func (h *Handler) updateModel(w http.ResponseWriter, r *http.Request) {
	h.updateEnabled(w, r, h.store.SetVirtualModelEnabled)
}

func (h *Handler) testModel(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "无效的模型 ID"})
		return
	}
	model, err := h.store.VirtualModelName(id)
	if err != nil {
		writeStoreError(w, err, "模型路由不存在")
		return
	}
	var input struct {
		Prompt string `json:"prompt"`
	}
	if !httpx.DecodeJSON(w, r, &input) {
		return
	}
	input.Prompt = strings.TrimSpace(input.Prompt)
	if input.Prompt == "" {
		httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "测试内容不能为空"})
		return
	}
	status, body, requestID := h.tester.TestModel(r.Context(), model, input.Prompt)
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		httpx.WriteJSON(w, status, map[string]string{"error": gatewayErrorMessage(body), "request_id": requestID})
		return
	}
	var response json.RawMessage
	if err := json.Unmarshal(body, &response); err != nil {
		httpx.WriteJSON(w, http.StatusBadGateway, map[string]string{"error": "模型返回了无效的 JSON", "request_id": requestID})
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"request_id": requestID, "response": response})
}

func gatewayErrorMessage(body []byte) string {
	var payload struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &payload) == nil && payload.Error.Message != "" {
		return payload.Error.Message
	}
	return strings.TrimSpace(string(body))
}

func (h *Handler) editModel(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "无效的模型 ID"})
		return
	}
	input, ok := decodeModelInput(w, r)
	if !ok {
		return
	}
	if err := h.store.UpdateVirtualModel(id, input); err != nil {
		writeStoreError(w, err, "模型路由不存在")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) deleteModel(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "无效的模型 ID"})
		return
	}
	result, err := h.store.DeleteVirtualModel(id)
	if err != nil {
		writeStoreError(w, err, "模型路由不存在")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, result)
}

func (h *Handler) updateEnabled(w http.ResponseWriter, r *http.Request, update func(int64, bool) error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "无效的 ID"})
		return
	}
	var input struct {
		Enabled *bool `json:"enabled"`
	}
	if !httpx.DecodeJSON(w, r, &input) {
		return
	}
	if input.Enabled == nil {
		httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "enabled 不能为空"})
		return
	}
	if err := update(id, *input.Enabled); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			httpx.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "记录不存在"})
			return
		}
		httpx.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func decodeProviderInput(w http.ResponseWriter, r *http.Request) (store.CreateProviderInput, bool) {
	var input struct {
		Name          string                         `json:"name"`
		AuthType      string                         `json:"auth_type"`
		AuthHeader    string                         `json:"auth_header"`
		StaticHeaders map[string]string              `json:"static_headers"`
		QuotaCodes    []string                       `json:"quota_codes"`
		Endpoints     map[string]string              `json:"endpoints"`
		Keys          []store.CreateUpstreamKeyInput `json:"keys"`
	}
	if !httpx.DecodeJSON(w, r, &input) {
		return store.CreateProviderInput{}, false
	}
	input.Name = strings.TrimSpace(input.Name)
	input.AuthHeader = strings.TrimSpace(input.AuthHeader)
	if input.Name == "" || len(input.Endpoints) == 0 || len(input.Keys) == 0 {
		httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "名称、至少一个协议端点和至少一把密钥不能为空"})
		return store.CreateProviderInput{}, false
	}
	if input.AuthType != "bearer" && input.AuthType != "x-api-key" && input.AuthType != "custom" {
		httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "不支持的鉴权方式"})
		return store.CreateProviderInput{}, false
	}
	if input.AuthType == "custom" && input.AuthHeader == "" {
		httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "自定义鉴权需要请求头名称"})
		return store.CreateProviderInput{}, false
	}
	for protocol, endpoint := range input.Endpoints {
		if !validProtocol(protocol) {
			httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "不支持的协议：" + protocol})
			return store.CreateProviderInput{}, false
		}
		parsed, err := url.ParseRequestURI(endpoint)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "无效的上游端点：" + endpoint})
			return store.CreateProviderInput{}, false
		}
	}
	for _, key := range input.Keys {
		if key.ID == 0 && strings.TrimSpace(key.Secret) == "" {
			httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "新增的上游密钥不能为空"})
			return store.CreateProviderInput{}, false
		}
	}
	return store.CreateProviderInput{Name: input.Name, AuthType: input.AuthType, AuthHeader: input.AuthHeader, StaticHeaders: input.StaticHeaders, QuotaCodes: input.QuotaCodes, Endpoints: input.Endpoints, Keys: input.Keys}, true
}

func decodeModelInput(w http.ResponseWriter, r *http.Request) (store.CreateVirtualModelInput, bool) {
	var input struct {
		Name       string                       `json:"name"`
		Candidates []store.CreateCandidateInput `json:"candidates"`
	}
	if !httpx.DecodeJSON(w, r, &input) {
		return store.CreateVirtualModelInput{}, false
	}
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" || len(input.Candidates) == 0 {
		httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "虚拟模型名和至少一个候选不能为空"})
		return store.CreateVirtualModelInput{}, false
	}
	for _, candidate := range input.Candidates {
		if candidate.ProviderID <= 0 || strings.TrimSpace(candidate.UpstreamModel) == "" || len(candidate.Protocols) == 0 {
			httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "候选供应商、真实模型名和协议入口不能为空"})
			return store.CreateVirtualModelInput{}, false
		}
		if candidate.DefaultMaxOutputTokens <= 0 || candidate.MaxOutputTokens < candidate.DefaultMaxOutputTokens {
			httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "最大输出 Token 配置无效"})
			return store.CreateVirtualModelInput{}, false
		}
		for _, protocol := range candidate.Protocols {
			if !validProtocol(protocol.Protocol) {
				httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "不支持的协议：" + protocol.Protocol})
				return store.CreateVirtualModelInput{}, false
			}
		}
	}
	return store.CreateVirtualModelInput{Name: input.Name, Candidates: input.Candidates}, true
}

func positiveQuery(w http.ResponseWriter, r *http.Request, name string, fallback, maximum int) (int, bool) {
	value := fallback
	if raw := r.URL.Query().Get(name); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 || (maximum > 0 && parsed > maximum) {
			httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": name + " 参数无效"})
			return 0, false
		}
		value = parsed
	}
	return value, true
}

func requestTimeRange(r *http.Request) (time.Time, time.Time, error) {
	fromValue := strings.TrimSpace(r.URL.Query().Get("created_from"))
	toValue := strings.TrimSpace(r.URL.Query().Get("created_to"))
	if fromValue == "" && toValue == "" {
		today := time.Now().UTC()
		from := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.UTC)
		return from, from.AddDate(0, 0, 1), nil
	}
	if fromValue == "" || toValue == "" {
		return time.Time{}, time.Time{}, errors.New("created_from 和 created_to 必须同时提供")
	}
	from, err := time.Parse(time.RFC3339Nano, fromValue)
	if err != nil {
		return time.Time{}, time.Time{}, errors.New("created_from 参数无效")
	}
	to, err := time.Parse(time.RFC3339Nano, toValue)
	if err != nil {
		return time.Time{}, time.Time{}, errors.New("created_to 参数无效")
	}
	from = from.UTC()
	to = to.UTC()
	if !from.Before(to) {
		return time.Time{}, time.Time{}, errors.New("created_from 必须早于 created_to")
	}
	return from, to, nil
}

func writeStoreError(w http.ResponseWriter, err error, notFound string) {
	if errors.Is(err, sql.ErrNoRows) {
		httpx.WriteJSON(w, http.StatusNotFound, map[string]string{"error": notFound})
		return
	}
	httpx.WriteJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
}

func validProtocol(protocol string) bool {
	return protocol == "chat_completions" || protocol == "responses" || protocol == "messages"
}

func (h *Handler) requireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionCookie)
		if err != nil {
			httpx.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "未登录"})
			return
		}
		if _, err := h.store.ValidateSession(cookie.Value); err != nil {
			if !errors.Is(err, sql.ErrNoRows) {
				httpx.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "无法验证登录会话"})
				return
			}
			httpx.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "登录会话已失效"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (h *Handler) requireCSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(csrfCookie)
		if err != nil || cookie.Value == "" || r.Header.Get("X-CSRF-Token") != cookie.Value {
			httpx.WriteJSON(w, http.StatusForbidden, map[string]string{"error": "CSRF 校验失败"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (h *Handler) setCookie(w http.ResponseWriter, name, value string, httpOnly bool, maxAge time.Duration) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: value, Path: "/", HttpOnly: httpOnly, Secure: true, SameSite: http.SameSiteStrictMode, MaxAge: int(maxAge.Seconds())})
}

func (h *Handler) clearCookie(w http.ResponseWriter, name string, httpOnly bool) {
	http.SetCookie(w, &http.Cookie{Name: name, Path: "/", HttpOnly: httpOnly, Secure: true, SameSite: http.SameSiteStrictMode, MaxAge: -1})
}

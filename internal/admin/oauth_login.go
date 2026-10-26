package admin

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/Sanjeever/model-confluence/internal/httpx"
	"github.com/Sanjeever/model-confluence/internal/oauthlogin"
)

func loginProviderID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	w.Header().Set("Cache-Control", "no-store")
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		httpx.WriteJSON(w, 400, map[string]string{"error": "无效的供应商 ID"})
		return 0, false
	}
	return id, true
}

func (h *Handler) startProviderLogin(w http.ResponseWriter, r *http.Request) {
	providerID, ok := loginProviderID(w, r)
	if !ok {
		return
	}
	loginProvider, ok := h.tester.(interface {
		OAuthLogin(int64) (oauthlogin.Config, error)
	})
	if !ok {
		httpx.WriteJSON(w, 503, map[string]string{"error": "OAuth 登录不可用"})
		return
	}
	config, err := loginProvider.OAuthLogin(providerID)
	if err != nil {
		writeStoreError(w, err, "供应商不存在或登录配置无效")
		return
	}
	cookie, _ := r.Cookie(sessionCookie)
	owner := cookie.Value
	exchange := config.Exchange
	config.Exchange = func(ctx context.Context, code, verifier, redirect string) (func() (int64, error), error) {
		save, err := exchange(ctx, code, verifier, redirect)
		if err != nil {
			return nil, err
		}
		return func() (int64, error) {
			if _, err := h.store.ValidateSession(owner); err != nil {
				return 0, errors.New("管理员会话已失效，请重新登录管理后台")
			}
			return save()
		}, nil
	}
	status, err := h.logins.Start(owner, providerID, config)
	if err != nil {
		code := http.StatusBadRequest
		if errors.Is(err, oauthlogin.ErrBusy) {
			code = http.StatusConflict
		}
		httpx.WriteJSON(w, code, map[string]string{"error": err.Error()})
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, status)
}

func (h *Handler) providerLoginStatus(w http.ResponseWriter, r *http.Request) {
	providerID, ok := loginProviderID(w, r)
	if !ok {
		return
	}
	cookie, _ := r.Cookie(sessionCookie)
	status, ok := h.logins.Get(cookie.Value, providerID, r.PathValue("login_id"))
	if !ok {
		httpx.WriteJSON(w, 404, map[string]string{"error": "登录会话不存在或已清理"})
		return
	}
	httpx.WriteJSON(w, 200, status)
}

func (h *Handler) cancelProviderLogin(w http.ResponseWriter, r *http.Request) {
	providerID, ok := loginProviderID(w, r)
	if !ok {
		return
	}
	cookie, _ := r.Cookie(sessionCookie)
	if !h.logins.Cancel(cookie.Value, providerID, r.PathValue("login_id")) {
		httpx.WriteJSON(w, 404, map[string]string{"error": "登录会话不存在"})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) submitProviderLogin(w http.ResponseWriter, r *http.Request) {
	providerID, ok := loginProviderID(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 32<<10)
	var input struct {
		URL string `json:"url"`
	}
	if !httpx.DecodeJSON(w, r, &input) {
		return
	}
	cookie, _ := r.Cookie(sessionCookie)
	status, err := h.logins.Submit(cookie.Value, providerID, r.PathValue("login_id"), input.URL)
	if err != nil {
		code := http.StatusBadRequest
		if errors.Is(err, oauthlogin.ErrNotFound) {
			code = http.StatusNotFound
		}
		if errors.Is(err, oauthlogin.ErrAlreadyHandled) {
			code = http.StatusConflict
		}
		httpx.WriteJSON(w, code, map[string]string{"error": err.Error()})
		return
	}
	httpx.WriteJSON(w, http.StatusAccepted, status)
}

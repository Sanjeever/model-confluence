package gateway

import (
	"context"
	"errors"

	"github.com/Sanjeever/model-confluence/internal/antigravity"
	"github.com/Sanjeever/model-confluence/internal/codex"
	"github.com/Sanjeever/model-confluence/internal/oauthlogin"
	"github.com/Sanjeever/model-confluence/internal/store"
)

func (h *Handler) OAuthLogin(providerID int64) (oauthlogin.Config, error) {
	provider, err := h.store.ProviderByID(providerID)
	if err != nil {
		return oauthlogin.Config{}, err
	}
	switch provider.Kind {
	case store.ProviderCodexOAuth:
		return oauthlogin.Config{Redirect: codex.LoginRedirect, Issuer: "https://auth.openai.com", Authorize: codex.AuthorizationURL, Exchange: func(ctx context.Context, code, verifier, redirect string) (func() (int64, error), error) {
			data, err := codex.ExchangeLogin(ctx, h.client, code, verifier, redirect)
			if err != nil {
				return nil, err
			}
			return func() (int64, error) {
				key, err := h.store.ImportOAuthCredential(providerID, "OAuth 登录", data)
				return key.ID, err
			}, nil
		}}, nil
	case store.ProviderAntigravityOAuth:
		if h.antigravityOAuth.ID == "" || h.antigravityOAuth.Secret == "" {
			return oauthlogin.Config{}, errors.New("请配置 MODEL_CONFLUENCE_ANTIGRAVITY_OAUTH_CLIENT_ID 和 MODEL_CONFLUENCE_ANTIGRAVITY_OAUTH_CLIENT_SECRET")
		}
		return oauthlogin.Config{Redirect: antigravity.LoginRedirect, Issuer: "https://accounts.google.com", Authorize: h.antigravityOAuth.AuthorizationURL, Exchange: func(ctx context.Context, code, verifier, redirect string) (func() (int64, error), error) {
			credential, err := antigravity.ExchangeLogin(ctx, h.client, h.antigravityOAuth, code, verifier, redirect)
			if err != nil {
				return nil, err
			}
			return func() (int64, error) {
				key, err := h.store.ImportAntigravityCredential(providerID, "OAuth 登录", credential)
				return key.ID, err
			}, nil
		}}, nil
	default:
		return oauthlogin.Config{}, errors.New("此供应商不支持浏览器 OAuth 登录")
	}
}

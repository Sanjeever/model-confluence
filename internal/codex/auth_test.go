package codex

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func testAuth(t *testing.T, user, account string, exp time.Time) []byte {
	t.Helper()
	claims := jwt.MapClaims{"sub": user, "email": "test@example.test", "exp": exp.Unix(), "https://api.openai.com/auth": map[string]string{"chatgpt_user_id": user, "chatgpt_account_id": account}}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte("test-signature"))
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(map[string]any{"auth_mode": "chatgpt", "OPENAI_API_KEY": nil, "tokens": Tokens{IDToken: token, AccessToken: token, RefreshToken: "test-refresh", AccountID: account}, "extension": true})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestParseAuth(t *testing.T) {
	data := testAuth(t, "user", "account", time.Now().Add(-time.Hour))
	a, err := ParseAuth(data)
	if err != nil {
		t.Fatal(err)
	}
	if a.Identity.Email != "test@example.test" || a.ExpiresAt.After(time.Now()) {
		t.Fatal("expired metadata not preserved")
	}
	var file map[string]any
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		mutate func()
	}{
		{"api key", func() { file["OPENAI_API_KEY"] = "test-api-key" }},
		{"mode", func() { file["auth_mode"] = "apikey" }},
		{"missing refresh", func() { file["tokens"].(map[string]any)["refresh_token"] = "" }},
		{"wrong workspace", func() { file["tokens"].(map[string]any)["account_id"] = "other" }},
		{"invalid jwt", func() { file["tokens"].(map[string]any)["access_token"] = "invalid" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := json.Unmarshal(data, &file); err != nil {
				t.Fatal(err)
			}
			tc.mutate()
			bad, _ := json.Marshal(file)
			if _, err := ParseAuth(bad); err == nil {
				t.Fatal("invalid file accepted")
			}
		})
	}
}

func TestRefresh(t *testing.T) {
	old, err := ParseAuth(testAuth(t, "user", "account", time.Now().Add(-time.Hour)))
	if err != nil {
		t.Fatal(err)
	}
	next, err := ParseAuth(testAuth(t, "user", "account", time.Now().Add(time.Hour)))
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]string
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
		}
		if req["refresh_token"] != "test-refresh" || req["client_id"] != OAuthClientID {
			t.Error("bad refresh request")
		}
		json.NewEncoder(w).Encode(map[string]string{"access_token": next.Tokens.AccessToken, "refresh_token": "rotated-test-refresh"})
	}))
	defer server.Close()
	data, err := Refresh(context.Background(), server.Client(), server.URL, old)
	if err != nil {
		t.Fatal(err)
	}
	a, err := ParseAuth(data)
	if err != nil {
		t.Fatal(err)
	}
	if a.Tokens.RefreshToken != "rotated-test-refresh" || a.LastRefresh == nil || !strings.Contains(string(data), `"extension":true`) {
		t.Fatal("refresh did not preserve/update file")
	}
}

func TestRefreshFailureClassification(t *testing.T) {
	a, _ := ParseAuth(testAuth(t, "user", "account", time.Now()))
	for _, code := range []string{"refresh_token_expired", "refresh_token_reused", "refresh_token_invalidated", "invalid_grant", "server_error"} {
		t.Run(code, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(400)
				json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": code, "message": "sensitive-test-response"}})
			}))
			defer srv.Close()
			_, err := Refresh(context.Background(), srv.Client(), srv.URL, a)
			var failure *RefreshError
			if !errors.As(err, &failure) || failure.Permanent != (code != "server_error") || strings.Contains(err.Error(), "sensitive-test-response") {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

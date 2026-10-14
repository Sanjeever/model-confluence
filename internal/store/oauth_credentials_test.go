package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/Sanjeever/model-confluence/internal/codex"
	"github.com/Sanjeever/model-confluence/internal/protocol"
	"github.com/golang-jwt/jwt/v5"
)

func oauthTestFile(t *testing.T) []byte {
	t.Helper()
	claims := jwt.MapClaims{"sub": "user", "exp": time.Now().Add(-time.Hour).Unix(), "https://api.openai.com/auth": map[string]string{"chatgpt_account_id": "account"}}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte("test"))
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(map[string]any{"tokens": codex.Tokens{IDToken: token, AccessToken: token, RefreshToken: "test-refresh"}})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestOAuthCredentialLifecycle(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	p, err := s.CreateProvider(CreateProviderInput{Name: "codex", Kind: ProviderCodexOAuth})
	if err != nil {
		t.Fatal(err)
	}
	if p.Endpoints[protocol.Responses] != codex.ResponsesEndpoint || p.Kind != ProviderCodexOAuth {
		t.Fatal("bad provider defaults")
	}
	key, err := s.ImportOAuthCredential(p.ID, "auth.json", oauthTestFile(t))
	if err != nil {
		t.Fatal(err)
	}
	if !KeyAvailable(key) {
		t.Fatal("expired access token excluded")
	}
	if _, err := s.ImportOAuthCredential(p.ID, "second.json", oauthTestFile(t)); !errors.Is(err, ErrDuplicateOAuthAccount) {
		t.Fatalf("duplicate: %v", err)
	}
	providers, err := s.ListProviders()
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(providers)
	var result []map[string]any
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	visible := result[0]["keys"].([]any)[0].(map[string]any)
	if _, ok := visible["auth_json"]; ok {
		t.Fatal("credentials leaked")
	}
	if _, ok := visible["secret"]; ok {
		t.Fatal("secret leaked")
	}
	if err := s.UpdateProvider(p.ID, CreateProviderInput{Name: "renamed", Kind: ProviderCodexOAuth}); err != nil {
		t.Fatal(err)
	}
	c, err := s.OAuthCredential(key.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetUpstreamKeyEnabled(key.ID, false); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveOAuthCredential(key.ID, c.Revision, oauthTestFile(t)); !errors.Is(err, ErrCredentialChanged) {
		t.Fatalf("stale refresh: %v", err)
	}
	if err := s.SetUpstreamKeyEnabled(key.ID, true); err != nil {
		t.Fatal(err)
	}
	c, err = s.OAuthCredential(key.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SaveOAuthCredential(key.ID, c.Revision, oauthTestFile(t)); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkUpstreamKeyRevision(key.ID, c.Revision, "auth_invalid", "stale failure", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.OAuthCredential(key.ID); err != nil {
		t.Fatal("old failure polluted new credentials", err)
	}
	if err := s.DeleteOAuthCredential(p.ID, key.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.OAuthCredential(key.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("deleted credential: %v", err)
	}
	var authJSON string
	if err := s.db.QueryRow(`SELECT auth_json FROM upstream_keys WHERE id = ?`, key.ID).Scan(&authJSON); err != nil || authJSON != "" {
		t.Fatal("deleted credential retained", err)
	}
	if _, err := s.ImportOAuthCredential(p.ID, "auth.json", oauthTestFile(t)); err != nil {
		t.Fatal("reimport failed", err)
	}
}

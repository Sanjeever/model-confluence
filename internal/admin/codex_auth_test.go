package admin

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Sanjeever/model-confluence/internal/codex"
	"github.com/Sanjeever/model-confluence/internal/httpx"
	"github.com/Sanjeever/model-confluence/internal/store"
	"github.com/golang-jwt/jwt/v5"
)

func TestAuthFileManagement(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	resolver, _ := httpx.NewClientIPResolver(nil)
	h := NewHandler(s, resolver, nil)
	mux := http.NewServeMux()
	h.Register(mux)
	session, err := s.CreateSession()
	if err != nil {
		t.Fatal(err)
	}
	call := func(method, path string, body *bytes.Buffer, ct string, auth, csrf bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, body)
		r.Header.Set("Content-Type", ct)
		if auth {
			r.AddCookie(&http.Cookie{Name: sessionCookie, Value: session.Token})
		}
		if csrf {
			r.AddCookie(&http.Cookie{Name: csrfCookie, Value: "test-csrf"})
			r.Header.Set("X-CSRF-Token", "test-csrf")
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	w := call("POST", "/api/admin/providers", bytes.NewBufferString(`{"name":"codex","kind":"codex_oauth"}`), "application/json", true, true)
	if w.Code != 201 {
		t.Fatalf("create: %d %s", w.Code, w.Body)
	}
	var p store.Provider
	if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	path := "/api/admin/providers/1/auth-files"
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"sub": "user", "exp": time.Now().Add(time.Hour).Unix(), "https://api.openai.com/auth": map[string]string{"chatgpt_account_id": "account"}}).SignedString([]byte("test"))
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(map[string]any{"tokens": codex.Tokens{IDToken: token, AccessToken: token, RefreshToken: "test-refresh"}})
	upload := func(auth, csrf bool, payload []byte) *httptest.ResponseRecorder {
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		file, err := writer.CreateFormFile("file", "auth.json")
		if err != nil {
			t.Fatal(err)
		}
		file.Write(payload)
		writer.Close()
		return call("POST", path, &body, writer.FormDataContentType(), auth, csrf)
	}
	if w := upload(false, false, data); w.Code != 401 {
		t.Fatalf("session: %d", w.Code)
	}
	if w := upload(true, false, data); w.Code != 403 {
		t.Fatalf("csrf: %d", w.Code)
	}
	if w := upload(true, true, []byte(`{}`)); w.Code != 400 {
		t.Fatalf("invalid file: %d %s", w.Code, w.Body)
	}
	w = upload(true, true, data)
	if w.Code != 201 {
		t.Fatalf("upload: %d %s", w.Code, w.Body)
	}
	if strings.Contains(w.Body.String(), token) || strings.Contains(w.Body.String(), "test-refresh") {
		t.Fatal("credential leaked")
	}
	if w := upload(true, true, data); w.Code != 409 {
		t.Fatalf("duplicate: %d", w.Code)
	}
	if w := call("PUT", "/api/admin/providers/1", bytes.NewBufferString(`{"name":"renamed","kind":"codex_oauth"}`), "application/json", true, true); w.Code != 204 {
		t.Fatalf("edit: %d %s", w.Code, w.Body)
	}
	if w := call("DELETE", path+"/1", &bytes.Buffer{}, "", true, false); w.Code != 403 {
		t.Fatal("delete lacks CSRF")
	}
	if w := call("DELETE", path+"/1", &bytes.Buffer{}, "", true, true); w.Code != 204 {
		t.Fatalf("delete: %d %s", w.Code, w.Body)
	}
	keys, err := s.ProviderByID(p.ID)
	if err != nil || len(keys.Keys) != 0 {
		t.Fatal("last account not removed", err)
	}
}

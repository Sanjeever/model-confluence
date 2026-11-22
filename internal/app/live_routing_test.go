package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Sanjeever/model-confluence/internal/config"
	"github.com/Sanjeever/model-confluence/internal/gateway"
	"github.com/Sanjeever/model-confluence/internal/protocol"
	"github.com/Sanjeever/model-confluence/internal/store"
)

func TestLiveRoutingAdminAPI(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	key, err := db.CreateAccessKey("client", nil)
	if err != nil {
		t.Fatal(err)
	}
	disabled, err := db.CreateAccessKey("disable-me", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetAccessKeyEnabled(disabled.ID, false); err != nil {
		t.Fatal(err)
	}
	provider, err := db.CreateProvider(store.CreateProviderInput{
		Name: "provider", AuthType: "bearer",
		Endpoints: map[string]string{protocol.Chat: "http://127.0.0.1/v1/chat/completions"},
		Keys:      []store.CreateUpstreamKeyInput{{Secret: "upstream-secret"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	model, err := db.CreateVirtualModel(store.CreateVirtualModelInput{Name: "virtual"})
	if err != nil {
		t.Fatal(err)
	}
	session, err := db.CreateSession()
	if err != nil {
		t.Fatal(err)
	}

	handler := New(config.Config{MaxRequestBytes: 1 << 20}, db)
	get := func(path string, authenticated bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		if authenticated {
			req.AddCookie(&http.Cookie{Name: "mc_session", Value: session.Token})
		}
		req.Header.Set("Authorization", "Bearer "+key.Secret)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		return w
	}
	const path = "/api/admin/live-routing"
	for _, endpoint := range []string{path, path + "/catalog"} {
		if w := get(endpoint, false); w.Code != http.StatusUnauthorized {
			t.Fatalf("access key admitted to %s: %d", endpoint, w.Code)
		}
	}
	w := get(path+"/catalog", true)
	if w.Code != http.StatusOK || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("catalog: %d %s", w.Code, w.Body)
	}
	var nodes []store.LiveRoutingNode
	if err := json.Unmarshal(w.Body.Bytes(), &nodes); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(nodes, []store.LiveRoutingNode{
		{Kind: "access_key", ID: key.ID, Name: key.Name},
		{Kind: "provider", ID: provider.ID, Name: provider.Name},
		{Kind: "virtual_model", ID: model.ID, Name: model.Name},
	}) {
		t.Fatalf("catalog: %+v", nodes)
	}

	from := time.Now().Add(-time.Second)
	generate := httptest.NewRequest(http.MethodPost, "/v1/chat/completions",
		strings.NewReader(`{"model":"not-configured","messages":[{"role":"user","content":"private-prompt"}]}`))
	generate.Header.Set("Authorization", "Bearer "+key.Secret)
	proxy := httptest.NewRecorder()
	handler.ServeHTTP(proxy, generate)
	if proxy.Code != http.StatusServiceUnavailable {
		t.Fatalf("gateway: %d %s", proxy.Code, proxy.Body)
	}
	readSnapshot := func() gateway.LiveRoutingSnapshot {
		t.Helper()
		w := get(path, true)
		if w.Code != http.StatusOK || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("snapshot: %d %s", w.Code, w.Body)
		}
		for _, value := range []string{key.Secret, "upstream-secret", "private-prompt", session.Token} {
			if strings.Contains(w.Body.String(), value) {
				t.Fatal("snapshot contains protected data")
			}
		}
		var snapshot gateway.LiveRoutingSnapshot
		if err := json.Unmarshal(w.Body.Bytes(), &snapshot); err != nil {
			t.Fatal(err)
		}
		return snapshot
	}
	snapshot := readSnapshot()
	if snapshot.Active == nil || len(snapshot.Active) != 0 || len(snapshot.Recent) != 1 ||
		snapshot.Recent[0].ID != proxy.Header().Get("X-Request-ID") || snapshot.Recent[0].Stage != "failed" {
		t.Fatalf("snapshot: %+v", snapshot)
	}
	if _, err := db.CleanupLogs(t.Context(), from, time.Now().Add(time.Second), "delete"); err != nil {
		t.Fatal(err)
	}
	if snapshot := readSnapshot(); snapshot.Recent == nil || len(snapshot.Recent) != 0 {
		t.Fatalf("deleted record is still visible: %+v", snapshot.Recent)
	}
}

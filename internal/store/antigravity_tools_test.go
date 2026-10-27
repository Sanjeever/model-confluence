package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/Sanjeever/model-confluence/internal/antigravity"
	"github.com/Sanjeever/model-confluence/internal/protocol"
)

func TestAntigravityToolHistoryPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	// 模拟升级尚无工具历史表的数据库。
	if _, err := s.db.Exec(`DROP TABLE antigravity_tool_history`); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.CreateProvider(CreateProviderInput{Name: "agy", Kind: ProviderAntigravityOAuth})
	if err != nil {
		t.Fatal(err)
	}
	k, err := s.ImportAntigravityCredential(p.ID, "test", antigravity.Credential{Identity: antigravity.Identity{Subject: "user", Email: "test@example.com", ProjectID: "project"}, ClientID: "client", AccessToken: "access", RefreshToken: "refresh", ExpiresAt: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	owner, err := s.CreateAccessKey("owner", nil)
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.CreateAccessKey("other", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.StartRequest(RequestStart{ID: "req", AccessKeyID: &owner.ID, InboundProtocol: protocol.Chat, Headers: "{}", Body: []byte(`{}`), CreatedAt: time.Now().Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
	route := ResolvedRoute{Provider: p, Key: k, UpstreamModel: "real"}
	turn := protocol.AntigravityToolTurn{Content: json.RawMessage(`{"role":"model","parts":[{"thoughtSignature":"signed","functionCall":{"id":"up","name":"test","args":{}}}]}`), Calls: []protocol.ToolCall{{ID: "call_ag_first", Name: "test", Arguments: json.RawMessage(`{}`)}, {ID: "call_ag_second", Name: "test", Arguments: json.RawMessage(`{}`)}}, UpstreamIDs: []string{"up", ""}}
	if err := s.SaveAntigravityToolTurn("req", route, turn); err != nil {
		t.Fatal(err)
	}
	if err := s.CompleteRequest(RequestResult{ID: "req", Status: "completed", CompletedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PruneLogs(context.Background(), time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, call := range turn.Calls {
		got, err := s.AntigravityToolHistory(&owner.ID, call.ID)
		if err != nil || string(got.Turn.Content) != string(turn.Content) || got.UpstreamKeyID != k.ID || got.UpstreamModel != "real" {
			t.Fatalf("persisted history: %+v %v", got, err)
		}
		for _, scope := range []*int64{nil, &other.ID} {
			if _, err := s.AntigravityToolHistory(scope, call.ID); !errors.Is(err, sql.ErrNoRows) {
				t.Fatalf("scope isolation: %v", err)
			}
		}
	}
	turn.Calls[0].ID = "call_ag_rollback"
	if err := s.SaveAntigravityToolTurn("req", route, turn); err == nil {
		t.Fatal("duplicate should fail")
	}
	if _, err := s.AntigravityToolHistory(&owner.ID, "call_ag_rollback"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("partially committed tool turn", err)
	}
}

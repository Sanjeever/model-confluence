package store

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/Sanjeever/model-confluence/internal/protocol"
)

func TestConsoleLogLifecycle(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "console.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	created := time.Now().UTC().Add(-time.Hour)
	tx, err := s.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for i := 0; i < 203; i++ {
		status := "completed"
		attemptStatus := "completed"
		if i == 201 {
			status = "in_progress"
		}
		if i == 202 {
			attemptStatus = "in_progress"
		}
		id := fmt.Sprintf("console-%d", i)
		if _, err := tx.Exec(`INSERT INTO requests(id,status,inbound_protocol,inbound_endpoint,client_ip,request_headers,request_body,created_at,input_tokens) VALUES (?, ?, ?, '/v1/chat/completions', '', '{}', ?, ?, 42)`, id, status, protocol.Chat, []byte(`{"model":"virtual"}`), formatTime(created)); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(`INSERT INTO attempts(request_id,position,status,created_at,provider_name,upstream_model,upstream_protocol) VALUES (?,0,?,?,'provider-a','real-a',?)`, id, attemptStatus, formatTime(created), protocol.Chat); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := tx.Exec(`INSERT INTO attempts(request_id,position,status,created_at,provider_name,upstream_model,upstream_protocol) VALUES ('console-0',1,'completed',?,'provider-b','real-b',?)`, formatTime(created), protocol.Messages); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveRequestRouting("console-0", "original", []RouteDecision{{CandidateID: 1, Reason: "可用"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.AddRoutingEvent("console-0", RoutingEvent{CandidateID: 1, Action: "选择路由"}); err != nil {
		t.Fatal(err)
	}
	from, to := created.Add(-time.Minute), created.Add(time.Minute)
	page, err := s.ListRequests(1, 10, "console-0", nil, from, to, RequestFilters{Provider: "provider-a", UpstreamModel: "real-b"})
	if err != nil || page.Total != 0 {
		t.Fatalf("filters matched different attempts: %+v, %v", page, err)
	}
	count, err := s.PreviewCleanup(context.Background(), from, to, "prune")
	if err != nil || count != 201 {
		t.Fatalf("preview = %d, %v", count, err)
	}
	result, err := s.CleanupLogs(context.Background(), from, to, "prune")
	if err != nil || result.Count != 201 {
		t.Fatalf("prune = %+v, %v", result, err)
	}
	detail, err := s.RequestDetail("console-0")
	if err != nil {
		t.Fatal(err)
	}
	if !detail.PayloadPruned || detail.RequestBody != "" || detail.InputTokens == nil || *detail.InputTokens != 42 || detail.ReplayOf != "original" || len(detail.RoutingEvents) != 1 {
		t.Fatalf("prune lost metadata: %+v", detail)
	}
	for _, a := range detail.Attempts {
		if !a.PayloadPruned {
			t.Fatal("attempt payload not pruned")
		}
	}
	result, err = s.CleanupLogs(context.Background(), from, to, "delete")
	if err != nil || result.Count != 201 {
		t.Fatalf("delete = %+v, %v", result, err)
	}
	var orphaned int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM request_routing`).Scan(&orphaned); err != nil || orphaned != 0 {
		t.Fatalf("routing not cascaded: %d %v", orphaned, err)
	}
	stats, err := s.StorageStats()
	if err != nil || stats.RequestCount != 2 || stats.LastCleanup == nil || stats.LastCleanup.Count != 201 {
		t.Fatalf("storage = %+v, %v", stats, err)
	}
}

func TestAttemptTransportAndMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range []RequestStart{{ID: "stream", Stream: true}, {ID: "buffered", Stream: false}} {
		r.InboundProtocol = protocol.Responses
		r.InboundEndpoint = "/v1/responses"
		r.Headers = "{}"
		r.Body = []byte(`{}`)
		r.CreatedAt = time.Now()
		if err := s.StartRequest(r); err != nil {
			t.Fatal(err)
		}
		if _, err := s.db.Exec(`INSERT INTO attempts(request_id,position,status,created_at) VALUES (?,0,'completed',?)`, r.ID, formatTime(time.Now())); err != nil {
			t.Fatal(err)
		}
	}
	// 模拟接入前的 schema；旧日志依 request.stream 回填。
	for _, stmt := range []string{"ALTER TABLE attempts DROP COLUMN upstream_stream", "ALTER TABLE attempts DROP COLUMN ignored_parameters_json", "ALTER TABLE providers DROP COLUMN kind", "DROP INDEX idx_oauth_identity", "ALTER TABLE upstream_keys DROP COLUMN auth_json", "ALTER TABLE upstream_keys DROP COLUMN file_name", "ALTER TABLE upstream_keys DROP COLUMN oauth_user_id", "ALTER TABLE upstream_keys DROP COLUMN oauth_account_id", "ALTER TABLE upstream_keys DROP COLUMN access_token_expires_at", "ALTER TABLE upstream_keys DROP COLUMN credential_revision"} {
		if _, err := s.db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, id := range []string{"stream", "buffered"} {
		d, err := s.RequestDetail(id)
		if err != nil {
			t.Fatal(err)
		}
		if len(d.Attempts) != 1 || d.Attempts[0].UpstreamStream != (id == "stream") || len(d.Attempts[0].IgnoredParameters) != 0 {
			t.Fatalf("bad migrated attempt: %+v", d.Attempts)
		}
	}
	p, err := s.CreateProvider(CreateProviderInput{Name: "generic", AuthType: "bearer", Endpoints: map[string]string{protocol.Responses: "https://example.test"}, Keys: []CreateUpstreamKeyInput{{Secret: "test"}}})
	if err != nil {
		t.Fatal(err)
	}
	p, err = s.ProviderByID(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	m, err := s.CreateVirtualModel(CreateVirtualModelInput{Name: "test", Candidates: []CreateCandidateInput{{ProviderID: p.ID, UpstreamModel: "test", Protocols: []CandidateProtocol{{Protocol: protocol.Responses}}}}})
	if err != nil {
		t.Fatal(err)
	}
	routes, err := s.ResolveRoutes(RoutingRequirements{VirtualModel: m.Name, Protocol: protocol.Responses})
	if err != nil {
		t.Fatal(err)
	}
	a, err := s.StartAttempt(AttemptStart{RequestID: "buffered", Position: 1, ProviderID: p.ID, UpstreamKeyID: p.Keys[0].ID, CandidateID: routes[0].CandidateID, UpstreamStream: true, IgnoredParameters: []string{"$.max_tokens"}, CreatedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CompleteAttempt(AttemptResult{ID: a, Status: "completed", ResponseBody: []byte("data: {}\n\n"), CompletedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	d, err := s.RequestDetail("buffered")
	if err != nil {
		t.Fatal(err)
	}
	if d.Stream || !d.Attempts[1].UpstreamStream || d.Attempts[1].IgnoredParameters[0] != "$.max_tokens" {
		t.Fatalf("bad transport detail: %+v", d)
	}
}

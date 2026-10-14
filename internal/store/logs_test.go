package store

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/Sanjeever/model-confluence/internal/protocol"
)

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

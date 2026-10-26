package store

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Sanjeever/model-confluence/internal/antigravity"
	"github.com/Sanjeever/model-confluence/internal/protocol"
)

func TestAntigravityCredentialLifecycle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.CreateProvider(CreateProviderInput{Name: "agy", Kind: ProviderAntigravityOAuth})
	if err != nil {
		t.Fatal(err)
	}
	c := antigravity.Credential{Identity: antigravity.Identity{Subject: "google-user", Email: "test@example.com", ProjectID: "project"}, ClientID: "client", AccessToken: "test-access", RefreshToken: "test-refresh", ExpiresAt: time.Now().Add(time.Hour), LastRefresh: time.Now()}
	k, err := s.ImportAntigravityCredential(p.ID, "account.json", c)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ImportAntigravityCredential(p.ID, "duplicate.json", c); !errors.Is(err, ErrDuplicateOAuthAccount) {
		t.Fatalf("duplicate: %v", err)
	}
	providers, err := s.ListProviders()
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(providers)
	if strings.Contains(string(raw), "test-access") || strings.Contains(string(raw), "test-refresh") || providers[0].Keys[0].AntigravityIdentity.Subject != c.Subject {
		t.Fatal("invalid account metadata", string(raw))
	}
	if _, err := s.CreateVirtualModel(CreateVirtualModelInput{Name: "virtual", Candidates: []CreateCandidateInput{{ProviderID: p.ID, UpstreamModel: "real", DefaultMaxOutputTokens: 100, MaxOutputTokens: 1000, Protocols: []CandidateProtocol{{Protocol: protocol.Antigravity, SupportsStream: true}}}}}); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkAntigravityModel(k.ID, k.CredentialRevision, "real", "RESOURCE_EXHAUSTED", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ResolveRoutes(RoutingRequirements{VirtualModel: "virtual", Protocol: protocol.Chat}); !errors.Is(err, ErrNoRoute) {
		t.Fatalf("model cooldown: %v", err)
	}
	if err := s.SaveAntigravityCredential(k.ID, k.CredentialRevision, c); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ResolveRoutes(RoutingRequirements{VirtualModel: "virtual", Protocol: protocol.Chat}); !errors.Is(err, ErrNoRoute) {
		t.Fatalf("refresh erased cooldown: %v", err)
	}
	if err := s.SaveAntigravityCredential(k.ID, k.CredentialRevision, c); !errors.Is(err, ErrCredentialChanged) {
		t.Fatalf("stale refresh: %v", err)
	}
	if err := s.SetUpstreamKeyEnabled(k.ID, false); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveAntigravityCredential(k.ID, k.CredentialRevision+1, c); !errors.Is(err, ErrCredentialChanged) {
		t.Fatalf("disabled refresh: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.DeleteOAuthCredential(p.ID, k.ID); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM antigravity_credentials`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("credentials not cleared: %d %v", count, err)
	}
}

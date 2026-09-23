package store

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestResetUpstreamKeyRuntimeRestoresRouteAvailability(t *testing.T) {
	database, err := Open(filepath.Join(t.TempDir(), "model-confluence.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	provider, err := database.CreateProvider(CreateProviderInput{
		Name:      "test-provider",
		AuthType:  "bearer",
		Endpoints: map[string]string{"chat_completions": "https://example.test/chat/completions"},
		Keys:      []CreateUpstreamKeyInput{{Secret: "test-secret"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	model, err := database.CreateVirtualModel(CreateVirtualModelInput{
		Name: "virtual-model",
		Candidates: []CreateCandidateInput{{
			ProviderID:             provider.ID,
			UpstreamModel:          "test-model",
			DefaultMaxOutputTokens: 256,
			MaxOutputTokens:        1024,
			Protocols:              []CandidateProtocol{{Protocol: "chat_completions"}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	routes, err := database.ResolveRoutes(RoutingRequirements{VirtualModel: model.Name, Protocol: "chat_completions"})
	if err != nil {
		t.Fatal(err)
	}
	recoverAt := time.Now().Add(time.Hour)
	if err := database.MarkUpstreamKey(routes[0].Key.ID, "rate_limited", "rate_limit", &recoverAt); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ResolveRoutes(RoutingRequirements{VirtualModel: model.Name, Protocol: "chat_completions"}); !errors.Is(err, ErrNoRoute) {
		t.Fatalf("cooling key returned error %v, want %v", err, ErrNoRoute)
	}

	if err := database.ResetUpstreamKeyRuntime(routes[0].Key.ID); err != nil {
		t.Fatal(err)
	}
	routes, err = database.ResolveRoutes(RoutingRequirements{VirtualModel: model.Name, Protocol: "chat_completions"})
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) != 1 || routes[0].Key.RuntimeStatus != "available" || routes[0].Key.RuntimeReason != "" || routes[0].Key.RecoverAt != nil {
		t.Fatalf("unexpected reset route: %+v", routes)
	}
}

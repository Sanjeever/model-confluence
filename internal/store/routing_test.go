package store

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestStaleCandidateFailureDoesNotDisableUpdatedCandidate(t *testing.T) {
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
			UpstreamModel:          "old-model",
			DefaultMaxOutputTokens: 256,
			MaxOutputTokens:        1024,
			Protocols:              []CandidateProtocol{{Protocol: "chat_completions"}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	oldRoutes, err := database.ResolveRoutes(RoutingRequirements{VirtualModel: model.Name, Protocol: "chat_completions"})
	if err != nil {
		t.Fatal(err)
	}
	oldRoute := oldRoutes[0]

	err = database.UpdateVirtualModel(model.ID, CreateVirtualModelInput{
		Name: model.Name,
		Candidates: []CreateCandidateInput{{
			ID:                     oldRoute.CandidateID,
			ProviderID:             provider.ID,
			UpstreamModel:          "new-model",
			DefaultMaxOutputTokens: 256,
			MaxOutputTokens:        1024,
			Protocols:              []CandidateProtocol{{Protocol: "chat_completions"}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.MarkModelCandidate(oldRoute.CandidateID, oldRoute.CandidateRevision, "config_error", "model_not_found"); err != nil {
		t.Fatal(err)
	}

	newRoutes, err := database.ResolveRoutes(RoutingRequirements{VirtualModel: model.Name, Protocol: "chat_completions"})
	if err != nil {
		t.Fatal(err)
	}
	if len(newRoutes) != 1 || newRoutes[0].UpstreamModel != "new-model" {
		t.Fatalf("unexpected updated routes: %+v", newRoutes)
	}
	if newRoutes[0].CandidateRevision == oldRoute.CandidateRevision {
		t.Fatal("candidate revision did not change after update")
	}

	if err := database.MarkModelCandidate(newRoutes[0].CandidateID, newRoutes[0].CandidateRevision, "config_error", "model_not_found"); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ResolveRoutes(RoutingRequirements{VirtualModel: model.Name, Protocol: "chat_completions"}); !errors.Is(err, ErrNoRoute) {
		t.Fatalf("current candidate failure returned %v, want %v", err, ErrNoRoute)
	}
}

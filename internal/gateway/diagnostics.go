package gateway

import (
	"context"
	"time"

	"github.com/Sanjeever/model-confluence/internal/store"
)

type executionKey struct{}
type executionOptions struct {
	CandidateID      int64
	UpstreamProtocol string
	ReplayOf         string
}

type CandidateCooldown struct {
	CandidateID int64     `json:"candidate_id"`
	Revision    int64     `json:"revision"`
	Failures    int       `json:"failures"`
	RecoverAt   time.Time `json:"recover_at"`
	Probing     bool      `json:"probing"`
}

func (h *Handler) CandidateCooldowns() []CandidateCooldown {
	h.candidateMu.Lock()
	defer h.candidateMu.Unlock()
	result := make([]CandidateCooldown, 0)
	for id, state := range h.candidates {
		if !state.cooldownUntil.IsZero() {
			result = append(result, CandidateCooldown{id, state.revision, state.count, state.cooldownUntil, state.probing})
		}
	}
	return result
}

func (h *Handler) routeCooldowns(decisions []store.RouteDecision) {
	h.candidateMu.Lock()
	defer h.candidateMu.Unlock()
	for i := range decisions {
		d := &decisions[i]
		state := h.candidates[d.CandidateID]
		if d.Reason == "可用" && state.revision == d.Revision && (state.probing || time.Now().Before(state.cooldownUntil)) {
			d.Reason = "候选冷却或正在探测"
			until := state.cooldownUntil
			d.RecoverAt = &until
		}
	}
}

func (h *Handler) ExplainRoutes(requirements store.RoutingRequirements) ([]store.RouteDecision, error) {
	_, decisions, err := h.store.ResolveRoutesExplained(requirements)
	if err == store.ErrNoRoute {
		err = nil
	}
	h.routeCooldowns(decisions)
	return decisions, err
}

func withExecution(ctx context.Context, options executionOptions) context.Context {
	return context.WithValue(ctx, executionKey{}, options)
}

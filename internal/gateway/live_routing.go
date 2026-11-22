package gateway

import (
	"cmp"
	"errors"
	"slices"
	"sync"
	"time"

	"github.com/Sanjeever/model-confluence/internal/antigravity"
	"github.com/Sanjeever/model-confluence/internal/codex"
	"github.com/Sanjeever/model-confluence/internal/store"
)

const (
	liveRouting        = "routing"
	liveWaiting        = "waiting"
	liveReceiving      = "receiving"
	liveAggregating    = "aggregating"
	liveStreaming      = "streaming"
	liveRetrying       = "retrying"
	liveAttemptStarted = "attempt_started"
	liveAttemptFailed  = "attempt_failed"
	liveEventLimit     = 20
	liveRecentLimit    = 10
	liveRecentTTL      = 5 * time.Minute
)

type LiveRouteTarget struct {
	ProviderID   int64  `json:"provider_id"`
	ProviderName string `json:"provider_name"`
	Model        string `json:"model"`
	Protocol     string `json:"protocol"`
}

type LiveRouteEvent struct {
	Kind           string           `json:"kind"`
	At             time.Time        `json:"at"`
	Upstream       *LiveRouteTarget `json:"upstream,omitempty"`
	ResponseStatus int              `json:"response_status,omitempty"`
}

type LiveRequest struct {
	ID              string           `json:"id"`
	AccessKeyID     *int64           `json:"access_key_id"`
	AccessKeyName   string           `json:"access_key_name"`
	VirtualModel    string           `json:"virtual_model"`
	InboundProtocol string           `json:"inbound_protocol"`
	Stream          bool             `json:"stream"`
	Stage           string           `json:"stage"`
	StartedAt       time.Time        `json:"started_at"`
	CompletedAt     *time.Time       `json:"completed_at,omitempty"`
	Attempts        int              `json:"attempts"`
	Upstream        *LiveRouteTarget `json:"upstream,omitempty"`
	Events          []LiveRouteEvent `json:"events"`
}

type LiveRoutingSnapshot struct {
	Now    time.Time     `json:"now"`
	Active []LiveRequest `json:"active"`
	Recent []LiveRequest `json:"recent"`
}

type liveRoutingActivity struct {
	mu     sync.Mutex
	active map[string]*liveRequest
	recent []LiveRequest
}

type liveRequest struct {
	LiveRequest
	lastTarget *LiveRouteTarget
}

func (h *Handler) LiveRouting() LiveRoutingSnapshot {
	a := &h.activity
	a.mu.Lock()
	now := time.Now().UTC()
	active := make([]LiveRequest, 0, len(a.active))
	for _, request := range a.active {
		item := request.LiveRequest
		item.Events = slices.Clone(item.Events)
		active = append(active, item)
	}
	recent := make([]LiveRequest, 0, len(a.recent))
	for _, request := range a.recent {
		if now.Sub(*request.CompletedAt) <= liveRecentTTL {
			recent = append(recent, request)
		}
	}
	a.mu.Unlock()
	slices.SortFunc(active, func(a, b LiveRequest) int {
		if order := a.StartedAt.Compare(b.StartedAt); order != 0 {
			return order
		}
		return cmp.Compare(a.ID, b.ID)
	})
	slices.SortFunc(recent, func(a, b LiveRequest) int { return b.CompletedAt.Compare(*a.CompletedAt) })
	return LiveRoutingSnapshot{Now: now, Active: active, Recent: recent}
}

func (a *liveRoutingActivity) begin(input store.RequestStart) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.active == nil {
		a.active = make(map[string]*liveRequest)
	}
	a.active[input.ID] = &liveRequest{LiveRequest: LiveRequest{
		ID: input.ID, AccessKeyID: input.AccessKeyID, AccessKeyName: input.AccessKeyName,
		VirtualModel: input.VirtualModel, InboundProtocol: input.InboundProtocol, Stream: input.Stream,
		Stage: liveRouting, StartedAt: input.CreatedAt, Events: []LiveRouteEvent{{Kind: liveRouting, At: input.CreatedAt}},
	}}
}

func (a *liveRoutingActivity) remove(id string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.active, id)
}

func (r *liveRequest) addEvent(kind string, at time.Time, target *LiveRouteTarget, status int) {
	if len(r.Events) == liveEventLimit {
		copy(r.Events[1:], r.Events[2:])
		r.Events = r.Events[:liveEventLimit-1]
	}
	r.Events = append(r.Events, LiveRouteEvent{Kind: kind, At: at, Upstream: target, ResponseStatus: status})
}

func (a *liveRoutingActivity) setStage(id, stage string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if r := a.active[id]; r != nil && r.Stage != stage {
		r.Stage = stage
		r.addEvent(stage, time.Now().UTC(), r.Upstream, 0)
	}
}

func (h *Handler) startAttempt(input store.AttemptStart) (int64, error) {
	id, err := h.store.StartAttempt(input)
	if err != nil {
		return 0, err
	}
	h.activity.mu.Lock()
	defer h.activity.mu.Unlock()
	if r := h.activity.active[input.RequestID]; r != nil {
		r.Upstream = &LiveRouteTarget{input.ProviderID, input.ProviderName, input.UpstreamModel, input.Protocol}
		r.lastTarget = r.Upstream
		r.Attempts++
		r.Stage = liveWaiting
		r.addEvent(liveAttemptStarted, input.CreatedAt, r.Upstream, 0)
	}
	return id, nil
}

func (h *Handler) completeAttempt(requestID string, result store.AttemptResult, cause error) error {
	err := h.store.CompleteAttempt(result)
	if result.Status != "failed" {
		return err
	}
	status := result.ResponseStatus
	var codexError *codex.UpstreamError
	var antigravityError *antigravity.UpstreamError
	switch {
	case errors.As(cause, &codexError):
		status = codexError.Status
	case errors.As(cause, &antigravityError):
		status = antigravityError.Status
	case status < 300:
		status = 0
	}
	h.activity.mu.Lock()
	defer h.activity.mu.Unlock()
	if r := h.activity.active[requestID]; r != nil {
		r.addEvent(liveAttemptFailed, result.CompletedAt, r.Upstream, status)
		if r.Stage != liveStreaming {
			r.Upstream = nil
			r.Stage = liveRouting
		}
	}
	return err
}

func (h *Handler) completeRequest(result store.RequestResult) error {
	if err := h.store.CompleteRequest(result); err != nil {
		return err
	}
	a := &h.activity
	a.mu.Lock()
	defer a.mu.Unlock()
	if r := a.active[result.ID]; r != nil {
		delete(a.active, result.ID)
		r.Stage = result.Status
		r.CompletedAt = &result.CompletedAt
		r.Upstream = r.lastTarget
		r.addEvent(r.Stage, result.CompletedAt, nil, result.ResponseStatus)
		if len(a.recent) == liveRecentLimit {
			copy(a.recent, a.recent[1:])
			a.recent = a.recent[:liveRecentLimit-1]
		}
		a.recent = append(a.recent, r.LiveRequest)
	}
	return nil
}

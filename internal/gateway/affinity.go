package gateway

import (
	"database/sql"
	"errors"
	"log/slog"
	"sort"
	"time"

	"github.com/Sanjeever/model-confluence/internal/store"
)

type affinityKey struct {
	AccessKeyID, CandidateID, Revision int64
	Session, Protocol                  string
}
type affinityBinding struct {
	KeyID     int64
	ExpiresAt time.Time
}

func (h *Handler) affinityRoutes(routes []store.ResolvedRoute, session string, accessKeyID int64) {
	h.affinityMu.Lock()
	defer h.affinityMu.Unlock()
	for start := 0; start < len(routes); {
		end := start + 1
		for end < len(routes) && routes[end].CandidateID == routes[start].CandidateID {
			end++
		}
		route := routes[start]
		key := affinityKey{accessKeyID, route.CandidateID, route.CandidateRevision, session, route.UpstreamProtocol}
		binding := h.affinities[key]
		if time.Now().Before(binding.ExpiresAt) {
			group := routes[start:end]
			sort.SliceStable(group, func(i, j int) bool { return group[i].Key.ID == binding.KeyID && group[j].Key.ID != binding.KeyID })
		} else {
			delete(h.affinities, key)
		}
		start = end
	}
}

func (h *Handler) rememberAffinity(requestID, session string, accessKeyID int64, routes []store.ResolvedRoute) {
	candidateID, keyID, err := h.store.SuccessfulRequestRoute(requestID)
	if errors.Is(err, sql.ErrNoRows) {
		return
	}
	if err != nil {
		slog.Error("读取会话路由结果失败", "request_id", requestID, "error", err)
		return
	}
	for _, route := range routes {
		if route.CandidateID != candidateID || route.Key.ID != keyID {
			continue
		}
		h.affinityMu.Lock()
		defer h.affinityMu.Unlock()
		if h.affinities == nil {
			h.affinities = make(map[affinityKey]affinityBinding)
		}
		key := affinityKey{accessKeyID, candidateID, route.CandidateRevision, session, route.UpstreamProtocol}
		if _, exists := h.affinities[key]; !exists && len(h.affinities) >= 10000 {
			var oldest affinityKey
			var earliest time.Time
			for k, binding := range h.affinities {
				if !time.Now().Before(binding.ExpiresAt) {
					delete(h.affinities, k)
					continue
				}
				if earliest.IsZero() || binding.ExpiresAt.Before(earliest) {
					earliest = binding.ExpiresAt
					oldest = k
				}
			}
			if len(h.affinities) >= 10000 {
				delete(h.affinities, oldest)
			}
		}
		h.affinities[key] = affinityBinding{keyID, time.Now().Add(time.Hour)}
		return
	}
}

func (h *Handler) SessionAffinityEnabled() bool { return h.sessionAffinity }

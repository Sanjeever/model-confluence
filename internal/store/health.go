package store

import (
	"database/sql"
	"time"
)

type UpstreamKeyHealth struct {
	ID            int64      `json:"id"`
	ProviderName  string     `json:"provider_name"`
	Name          string     `json:"name"`
	Position      int        `json:"position"`
	Enabled       bool       `json:"enabled"`
	RuntimeStatus string     `json:"runtime_status"`
	RuntimeReason string     `json:"runtime_reason,omitempty"`
	RecoverAt     *time.Time `json:"recover_at"`
	ExpiresAt     *time.Time `json:"expires_at"`
	LastUsedAt    *time.Time `json:"last_used_at"`
}

type CandidateHealth struct {
	CandidateID  int64     `json:"candidate_id"`
	VirtualModel string    `json:"virtual_model"`
	UpstreamModel string   `json:"upstream_model"`
	ProviderName string    `json:"provider_name"`
	LastFailure  string    `json:"last_failure"`
	LastFailedAt time.Time `json:"last_failed_at"`
	FailedCount  int       `json:"failed_count"`
}

type HealthOverview struct {
	Keys              []UpstreamKeyHealth `json:"keys"`
	Candidates        []CandidateHealth   `json:"candidates"`
	UnroutedModels    []string            `json:"unrouted_models"`
	AbnormalKeyCount  int                 `json:"abnormal_key_count"`
	FailedCandidates  int                 `json:"failed_candidates"`
}

// HealthStatus aggregates the runtime state of the upstream key pools and the
// recent failure history of model candidates. Candidate cooldown is not
// persisted by the gateway, so the candidate dimension reflects the latest
// actual attempt outcome rather than a synthetic state.
func (s *Store) HealthStatus() (HealthOverview, error) {
	var result HealthOverview
	result.Keys = []UpstreamKeyHealth{}
	result.Candidates = []CandidateHealth{}
	result.UnroutedModels = []string{}

	rows, err := s.db.Query(`
SELECT k.id, COALESCE(p.name, ''), COALESCE(k.name, ''), k.position, k.enabled,
  k.runtime_status, COALESCE(k.runtime_reason, ''), k.recover_at, k.expires_at, k.last_used_at
FROM upstream_keys k
JOIN providers p ON p.id = k.provider_id
WHERE k.archived_at IS NULL
ORDER BY p.name, k.position`)
	if err != nil {
		return HealthOverview{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var key UpstreamKeyHealth
		var recoverAt, expiresAt, lastUsedAt sql.NullString
		if err := rows.Scan(&key.ID, &key.ProviderName, &key.Name, &key.Position, &key.Enabled, &key.RuntimeStatus, &key.RuntimeReason, &recoverAt, &expiresAt, &lastUsedAt); err != nil {
			return HealthOverview{}, err
		}
		if key.RecoverAt, err = parseNullableTime(recoverAt); err != nil {
			return HealthOverview{}, err
		}
		if key.ExpiresAt, err = parseNullableTime(expiresAt); err != nil {
			return HealthOverview{}, err
		}
		if key.LastUsedAt, err = parseNullableTime(lastUsedAt); err != nil {
			return HealthOverview{}, err
		}
		if key.RuntimeStatus != "available" {
			result.AbnormalKeyCount++
		}
		result.Keys = append(result.Keys, key)
	}
	if err := rows.Err(); err != nil {
		return HealthOverview{}, err
	}

	failedRows, err := s.db.Query(`
SELECT a.candidate_id, COALESCE(r.virtual_model, ''), COALESCE(a.upstream_model, ''),
  COALESCE(a.provider_name, ''), a.error_message, MAX(a.completed_at) AS last_failed_at, COUNT(*) AS failed_count
FROM attempts a
JOIN requests r ON r.id = a.request_id
WHERE a.status = 'failed' AND a.error_message IS NOT NULL AND a.error_message != '' AND a.candidate_id IS NOT NULL
GROUP BY a.candidate_id, r.virtual_model, a.upstream_model, a.provider_name
ORDER BY last_failed_at DESC`)
	if err != nil {
		return HealthOverview{}, err
	}
	defer failedRows.Close()
	for failedRows.Next() {
		var candidate CandidateHealth
		var lastFailedAt string
		if err := failedRows.Scan(&candidate.CandidateID, &candidate.VirtualModel, &candidate.UpstreamModel, &candidate.ProviderName, &candidate.LastFailure, &lastFailedAt, &candidate.FailedCount); err != nil {
			return HealthOverview{}, err
		}
		candidate.LastFailedAt, err = time.Parse(timestampLayout, lastFailedAt)
		if err != nil {
			return HealthOverview{}, err
		}
		result.Candidates = append(result.Candidates, candidate)
	}
	if err := failedRows.Err(); err != nil {
		return HealthOverview{}, err
	}
	result.FailedCandidates = len(result.Candidates)

	// Unrouted models: enabled virtual models with no currently available
	// candidate route (all candidates disabled, provider disabled, or key pool
	// drained). This surfaces requests that would fail with no_eligible_route.
	routes, err := s.EnabledModelNames()
	if err != nil {
		return HealthOverview{}, err
	}
	for _, name := range routes {
		_, err := s.ResolveRoutes(RoutingRequirements{VirtualModel: name, Protocol: "chat_completions"})
		if err != nil {
			result.UnroutedModels = append(result.UnroutedModels, name)
		}
	}
	return result, nil
}

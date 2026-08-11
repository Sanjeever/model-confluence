package store

import (
	"encoding/json"
	"errors"
	"time"
)

var ErrNoRoute = errors.New("no eligible route")

type RoutingRequirements struct {
	VirtualModel  string
	Protocol      string
	Stream        bool
	Tools         bool
	ParallelTools bool
	Effort        string
}

type ResolvedRoute struct {
	VirtualModel           string
	CandidateID            int64
	UpstreamModel          string
	DefaultMaxOutputTokens int
	UpstreamProtocol       string
	UpstreamEndpoint       string
	Provider               Provider
	Key                    UpstreamKey
	ProtocolConfig         CandidateProtocol
}

func (s *Store) ResolveRoutes(requirements RoutingRequirements) ([]ResolvedRoute, error) {
	var modelID int64
	var enabled bool
	err := s.db.QueryRow(`SELECT id, enabled FROM virtual_models WHERE name = ? AND archived_at IS NULL`, requirements.VirtualModel).Scan(&modelID, &enabled)
	if err != nil {
		return nil, err
	}
	if !enabled {
		return nil, ErrNoRoute
	}
	candidates, err := s.modelCandidates(modelID)
	if err != nil {
		return nil, err
	}
	var routes []ResolvedRoute
	for _, candidate := range candidates {
		if !candidate.Enabled || candidate.RuntimeStatus != "available" {
			continue
		}
		protocol, ok := chooseProtocol(candidate.Protocols, requirements)
		if !ok {
			continue
		}
		provider, err := s.providerByID(candidate.ProviderID, true)
		if err != nil {
			return nil, err
		}
		if !provider.Enabled {
			continue
		}
		endpoint := provider.Endpoints[protocol.Protocol]
		if endpoint == "" {
			continue
		}
		for _, key := range provider.Keys {
			if !keyAvailable(key) {
				continue
			}
			routes = append(routes, ResolvedRoute{
				VirtualModel: requirements.VirtualModel, CandidateID: candidate.ID, UpstreamModel: candidate.UpstreamModel, DefaultMaxOutputTokens: candidate.DefaultMaxOutputTokens,
				UpstreamProtocol: protocol.Protocol, UpstreamEndpoint: endpoint, Provider: provider, Key: key, ProtocolConfig: protocol,
			})
		}
	}
	if len(routes) == 0 {
		return nil, ErrNoRoute
	}
	return routes, nil
}

func chooseProtocol(protocols []CandidateProtocol, requirements RoutingRequirements) (CandidateProtocol, bool) {
	eligible := make([]CandidateProtocol, 0, len(protocols))
	for _, protocol := range protocols {
		if requirements.Stream && !protocol.SupportsStream || requirements.Tools && !protocol.SupportsTools || requirements.ParallelTools && !protocol.SupportsParallelTools {
			continue
		}
		if requirements.Effort != "" && !contains(protocol.EffortLevels, requirements.Effort) {
			continue
		}
		eligible = append(eligible, protocol)
	}
	for _, protocol := range eligible {
		if protocol.Protocol == requirements.Protocol {
			return protocol, true
		}
	}
	if len(eligible) > 0 {
		return eligible[0], true
	}
	return CandidateProtocol{}, false
}

func (s *Store) providerByID(id int64, includeSecrets bool) (Provider, error) {
	var provider Provider
	var staticHeadersJSON, quotaCodesJSON, createdAt string
	err := s.db.QueryRow(`SELECT id, name, enabled, auth_type, COALESCE(auth_header, ''), static_headers_json, quota_codes_json, created_at FROM providers WHERE id = ? AND archived_at IS NULL`, id).Scan(&provider.ID, &provider.Name, &provider.Enabled, &provider.AuthType, &provider.AuthHeader, &staticHeadersJSON, &quotaCodesJSON, &createdAt)
	if err != nil {
		return Provider{}, err
	}
	if err := json.Unmarshal([]byte(staticHeadersJSON), &provider.StaticHeaders); err != nil {
		return Provider{}, err
	}
	if err := json.Unmarshal([]byte(quotaCodesJSON), &provider.QuotaCodes); err != nil {
		return Provider{}, err
	}
	provider.CreatedAt, err = time.Parse(timestampLayout, createdAt)
	if err != nil {
		return Provider{}, err
	}
	provider.Endpoints, err = s.providerEndpoints(provider.ID)
	if err != nil {
		return Provider{}, err
	}
	provider.Keys, err = s.providerKeys(provider.ID, includeSecrets)
	return provider, err
}

func keyAvailable(key UpstreamKey) bool {
	if !key.Enabled || key.RuntimeStatus == "auth_invalid" || key.RuntimeStatus == "quota_exhausted" {
		return false
	}
	if key.ExpiresAt != nil && time.Now().After(*key.ExpiresAt) {
		return false
	}
	if key.RuntimeStatus == "rate_limited" && key.RecoverAt != nil && time.Now().Before(*key.RecoverAt) {
		return false
	}
	return true
}

func (s *Store) MarkUpstreamKey(id int64, status, reason string, recoverAt *time.Time) error {
	var recover any
	if recoverAt != nil {
		recover = formatTime(*recoverAt)
	}
	_, err := s.db.Exec(`UPDATE upstream_keys SET runtime_status = ?, runtime_reason = ?, recover_at = ?, updated_at = ? WHERE id = ?`, status, nullableString(reason), recover, formatTime(time.Now()), id)
	return err
}

func (s *Store) TouchUpstreamKey(id int64) error {
	_, err := s.db.Exec(`UPDATE upstream_keys SET last_used_at = ? WHERE id = ?`, formatTime(time.Now()), id)
	return err
}

func (s *Store) EnabledModelNames() ([]string, error) {
	rows, err := s.db.Query(`SELECT name FROM virtual_models WHERE enabled = 1 AND archived_at IS NULL ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return names, rows.Err()
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

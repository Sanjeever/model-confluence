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
	templates, err := s.cachedRouteTemplates(requirements)
	if err != nil {
		return nil, err
	}
	routes := make([]ResolvedRoute, 0, len(templates))
	for _, route := range templates {
		if KeyAvailable(route.Key) {
			routes = append(routes, route)
		}
	}
	if len(routes) == 0 {
		return nil, ErrNoRoute
	}
	return routes, nil
}

func (s *Store) cachedRouteTemplates(requirements RoutingRequirements) ([]ResolvedRoute, error) {
	s.configMu.RLock()
	routes, ok := s.routeCache[requirements]
	s.configMu.RUnlock()
	if ok {
		return routes, nil
	}
	s.routeLoadMu.Lock()
	defer s.routeLoadMu.Unlock()
	s.configMu.RLock()
	routes, ok = s.routeCache[requirements]
	version := s.configVersion
	s.configMu.RUnlock()
	if ok {
		return routes, nil
	}
	routes, err := s.loadRouteTemplates(requirements)
	if err != nil {
		return nil, err
	}
	s.configMu.Lock()
	if version == s.configVersion {
		s.routeCache[requirements] = routes
	}
	s.configMu.Unlock()
	return routes, nil
}

func (s *Store) loadRouteTemplates(requirements RoutingRequirements) ([]ResolvedRoute, error) {
	var modelID int64
	var enabled bool
	err := s.db.QueryRow(`SELECT id, enabled FROM virtual_models WHERE name = ? AND archived_at IS NULL`, requirements.VirtualModel).Scan(&modelID, &enabled)
	if err != nil {
		return nil, err
	}
	if !enabled {
		return nil, nil
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
			routes = append(routes, ResolvedRoute{
				VirtualModel: requirements.VirtualModel, CandidateID: candidate.ID, UpstreamModel: candidate.UpstreamModel, DefaultMaxOutputTokens: candidate.DefaultMaxOutputTokens,
				UpstreamProtocol: protocol.Protocol, UpstreamEndpoint: endpoint, Provider: provider, Key: key, ProtocolConfig: protocol,
			})
		}
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

// ProviderByID 返回供应商完整配置，包含密钥明文，供网关侧管理探测使用。
func (s *Store) ProviderByID(id int64) (Provider, error) {
	return s.providerByID(id, true)
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

// KeyAvailable 判断上游密钥当前是否可用于路由。
func KeyAvailable(key UpstreamKey) bool {
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
	if err == nil {
		s.invalidateConfig()
	}
	return err
}

func (s *Store) MarkModelCandidate(id int64, status, reason string) error {
	_, err := s.db.Exec(`UPDATE model_candidates SET runtime_status = ?, runtime_reason = ?, updated_at = ? WHERE id = ?`, status, nullableString(reason), formatTime(time.Now()), id)
	if err == nil {
		s.invalidateConfig()
	}
	return err
}

func (s *Store) TouchUpstreamKey(id int64) error {
	now := time.Now().UTC()
	return s.touchConfig("upstream_key", id, now, func() error {
		_, err := s.db.Exec(`UPDATE upstream_keys SET last_used_at = ? WHERE id = ?`, formatTime(now), id)
		return err
	})
}

func (s *Store) EnabledModelNames() ([]string, error) {
	s.configMu.RLock()
	names := s.modelNamesCache
	ok := s.modelNamesCached
	s.configMu.RUnlock()
	if ok {
		return names, nil
	}
	s.routeLoadMu.Lock()
	defer s.routeLoadMu.Unlock()
	s.configMu.RLock()
	names = s.modelNamesCache
	ok = s.modelNamesCached
	version := s.configVersion
	s.configMu.RUnlock()
	if ok {
		return names, nil
	}
	names, err := s.loadEnabledModelNames()
	if err != nil {
		return nil, err
	}
	s.configMu.Lock()
	if version == s.configVersion {
		s.modelNamesCache = names
		s.modelNamesCached = true
	}
	s.configMu.Unlock()
	return names, nil
}

func (s *Store) loadEnabledModelNames() ([]string, error) {
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

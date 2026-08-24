package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

var ErrProviderInUse = errors.New("provider is used by model routes")

type Provider struct {
	ID            int64             `json:"id"`
	Name          string            `json:"name"`
	Enabled       bool              `json:"enabled"`
	AuthType      string            `json:"auth_type"`
	AuthHeader    string            `json:"auth_header,omitempty"`
	StaticHeaders map[string]string `json:"static_headers"`
	QuotaCodes    []string          `json:"quota_codes"`
	Endpoints     map[string]string `json:"endpoints"`
	Keys          []UpstreamKey     `json:"keys"`
	CreatedAt     time.Time         `json:"created_at"`
}

type ProviderPage struct {
	Items    []Provider `json:"items"`
	Total    int        `json:"total"`
	Page     int        `json:"page"`
	PageSize int        `json:"page_size"`
}

type ProviderOption struct {
	ID        int64             `json:"id"`
	Name      string            `json:"name"`
	Enabled   bool              `json:"enabled"`
	Endpoints map[string]string `json:"endpoints"`
}

type UpstreamKey struct {
	ID            int64      `json:"id"`
	Name          string     `json:"name"`
	Secret        string     `json:"secret,omitempty"`
	SecretHint    string     `json:"secret_hint"`
	Position      int        `json:"position"`
	Enabled       bool       `json:"enabled"`
	ExpiresAt     *time.Time `json:"expires_at"`
	RuntimeStatus string     `json:"runtime_status"`
	RuntimeReason string     `json:"runtime_reason,omitempty"`
	RecoverAt     *time.Time `json:"recover_at"`
	LastUsedAt    *time.Time `json:"last_used_at"`
}

type CreateProviderInput struct {
	Name          string
	AuthType      string
	AuthHeader    string
	StaticHeaders map[string]string
	QuotaCodes    []string
	Endpoints     map[string]string
	Keys          []CreateUpstreamKeyInput
}

type CreateUpstreamKeyInput struct {
	ID        int64      `json:"id"`
	Name      string     `json:"name"`
	Secret    string     `json:"secret"`
	ExpiresAt *time.Time `json:"expires_at"`
	Enabled   *bool      `json:"enabled"`
}

func (s *Store) ListProviders() ([]Provider, error) {
	rows, err := s.db.Query(`SELECT id, name, enabled, auth_type, COALESCE(auth_header, ''), static_headers_json, quota_codes_json, created_at FROM providers WHERE archived_at IS NULL ORDER BY id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	providers := make([]Provider, 0)
	for rows.Next() {
		provider, err := s.scanProvider(rows)
		if err != nil {
			return nil, err
		}
		providers = append(providers, provider)
	}
	return providers, rows.Err()
}

func (s *Store) ListProvidersPage(page, pageSize int, name, authType string, enabled *bool) (ProviderPage, error) {
	where := "archived_at IS NULL"
	args := []any{}
	if name != "" {
		where += " AND instr(name, ?) > 0"
		args = append(args, name)
	}
	if authType != "" {
		where += " AND auth_type = ?"
		args = append(args, authType)
	}
	if enabled != nil {
		where += " AND enabled = ?"
		args = append(args, *enabled)
	}

	var total int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM providers WHERE "+where, args...).Scan(&total); err != nil {
		return ProviderPage{}, err
	}
	listArgs := append([]any(nil), args...)
	listArgs = append(listArgs, pageSize, (page-1)*pageSize)
	rows, err := s.db.Query(`SELECT id, name, enabled, auth_type, COALESCE(auth_header, ''), static_headers_json, quota_codes_json, created_at FROM providers WHERE `+where+` ORDER BY id DESC LIMIT ? OFFSET ?`, listArgs...)
	if err != nil {
		return ProviderPage{}, err
	}
	defer rows.Close()
	providers := make([]Provider, 0)
	for rows.Next() {
		provider, err := s.scanProvider(rows)
		if err != nil {
			return ProviderPage{}, err
		}
		providers = append(providers, provider)
	}
	if err := rows.Err(); err != nil {
		return ProviderPage{}, err
	}
	return ProviderPage{Items: providers, Total: total, Page: page, PageSize: pageSize}, nil
}

func (s *Store) ListProviderOptions() ([]ProviderOption, error) {
	rows, err := s.db.Query(`SELECT id, name, enabled FROM providers WHERE archived_at IS NULL ORDER BY id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	options := make([]ProviderOption, 0)
	for rows.Next() {
		var option ProviderOption
		if err := rows.Scan(&option.ID, &option.Name, &option.Enabled); err != nil {
			return nil, err
		}
		option.Endpoints, err = s.providerEndpoints(option.ID)
		if err != nil {
			return nil, err
		}
		options = append(options, option)
	}
	return options, rows.Err()
}

func (s *Store) scanProvider(row rowScanner) (Provider, error) {
	var provider Provider
	var staticHeadersJSON, quotaCodesJSON, createdAt string
	if err := row.Scan(&provider.ID, &provider.Name, &provider.Enabled, &provider.AuthType, &provider.AuthHeader, &staticHeadersJSON, &quotaCodesJSON, &createdAt); err != nil {
		return Provider{}, err
	}
	if err := json.Unmarshal([]byte(staticHeadersJSON), &provider.StaticHeaders); err != nil {
		return Provider{}, err
	}
	if err := json.Unmarshal([]byte(quotaCodesJSON), &provider.QuotaCodes); err != nil {
		return Provider{}, err
	}
	var err error
	provider.CreatedAt, err = time.Parse(timestampLayout, createdAt)
	if err != nil {
		return Provider{}, err
	}
	provider.Endpoints, err = s.providerEndpoints(provider.ID)
	if err != nil {
		return Provider{}, err
	}
	provider.Keys, err = s.providerKeys(provider.ID, true)
	if err != nil {
		return Provider{}, err
	}
	return provider, nil
}

func (s *Store) CreateProvider(input CreateProviderInput) (Provider, error) {
	staticHeaders, err := json.Marshal(input.StaticHeaders)
	if err != nil {
		return Provider{}, err
	}
	quotaCodes, err := json.Marshal(input.QuotaCodes)
	if err != nil {
		return Provider{}, err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return Provider{}, err
	}
	defer tx.Rollback()
	now := time.Now().UTC()
	if _, err := tx.Exec(`UPDATE providers SET name = name || '#archived-' || id, updated_at = ? WHERE name = ? AND archived_at IS NOT NULL`, formatTime(now), input.Name); err != nil {
		return Provider{}, err
	}
	result, err := tx.Exec(`INSERT INTO providers (name, enabled, auth_type, auth_header, static_headers_json, quota_codes_json, created_at, updated_at) VALUES (?, 1, ?, ?, ?, ?, ?, ?)`, input.Name, input.AuthType, nullableString(input.AuthHeader), string(staticHeaders), string(quotaCodes), formatTime(now), formatTime(now))
	if err != nil {
		return Provider{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return Provider{}, err
	}
	for protocol, url := range input.Endpoints {
		if _, err := tx.Exec(`INSERT INTO provider_endpoints (provider_id, protocol, url) VALUES (?, ?, ?)`, id, protocol, url); err != nil {
			return Provider{}, err
		}
	}
	for position, key := range input.Keys {
		var expiresAt any
		if key.ExpiresAt != nil {
			expiresAt = formatTime(*key.ExpiresAt)
		}
		if _, err := tx.Exec(`INSERT INTO upstream_keys (provider_id, name, secret, position, enabled, expires_at, created_at, updated_at) VALUES (?, ?, ?, ?, 1, ?, ?, ?)`, id, nullableString(key.Name), key.Secret, position, expiresAt, formatTime(now), formatTime(now)); err != nil {
			return Provider{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return Provider{}, err
	}
	return Provider{ID: id, Name: input.Name, Enabled: true, AuthType: input.AuthType, AuthHeader: input.AuthHeader, StaticHeaders: input.StaticHeaders, QuotaCodes: input.QuotaCodes, Endpoints: input.Endpoints, CreatedAt: now}, nil
}

func (s *Store) SetProviderEnabled(id int64, enabled bool) error {
	return updateEnabled(s.db, "providers", id, enabled)
}

func (s *Store) UpdateProvider(id int64, input CreateProviderInput) error {
	staticHeaders, err := json.Marshal(input.StaticHeaders)
	if err != nil {
		return err
	}
	quotaCodes, err := json.Marshal(input.QuotaCodes)
	if err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := formatTime(time.Now())
	if _, err := tx.Exec(`UPDATE providers SET name = name || '#archived-' || id, updated_at = ? WHERE name = ? AND archived_at IS NOT NULL`, now, input.Name); err != nil {
		return err
	}
	result, err := tx.Exec(`UPDATE providers SET name = ?, auth_type = ?, auth_header = ?, static_headers_json = ?, quota_codes_json = ?, updated_at = ? WHERE id = ? AND archived_at IS NULL`, input.Name, input.AuthType, nullableString(input.AuthHeader), string(staticHeaders), string(quotaCodes), now, id)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return sql.ErrNoRows
	}
	usedProtocols, err := providerUsedProtocols(tx, id)
	if err != nil {
		return err
	}
	for protocol := range usedProtocols {
		if input.Endpoints[protocol] == "" {
			return fmt.Errorf("供应商协议端点 %s 仍被模型路由使用", protocol)
		}
	}
	if _, err := tx.Exec(`DELETE FROM provider_endpoints WHERE provider_id = ?`, id); err != nil {
		return err
	}
	for protocol, endpoint := range input.Endpoints {
		if _, err := tx.Exec(`INSERT INTO provider_endpoints (provider_id, protocol, url) VALUES (?, ?, ?)`, id, protocol, endpoint); err != nil {
			return err
		}
	}
	existing, err := providerKeyIDs(tx, id)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE upstream_keys SET position = -id WHERE provider_id = ?`, id); err != nil {
		return err
	}
	for position, key := range input.Keys {
		expiresAt := nullableTime(key.ExpiresAt)
		enabled := optionalBool(key.Enabled, true)
		if key.ID == 0 {
			if _, err := tx.Exec(`INSERT INTO upstream_keys (provider_id, name, secret, position, enabled, expires_at, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, id, nullableString(key.Name), key.Secret, position, enabled, expiresAt, now, now); err != nil {
				return err
			}
			continue
		}
		if _, ok := existing[key.ID]; !ok {
			return sql.ErrNoRows
		}
		if key.Secret == "" {
			_, err = tx.Exec(`UPDATE upstream_keys SET name = ?, position = ?, enabled = ?, expires_at = ?, updated_at = ? WHERE id = ?`, nullableString(key.Name), position, enabled, expiresAt, now, key.ID)
		} else {
			_, err = tx.Exec(`UPDATE upstream_keys SET name = ?, secret = ?, position = ?, enabled = ?, expires_at = ?, runtime_status = 'available', runtime_reason = NULL, recover_at = NULL, updated_at = ? WHERE id = ?`, nullableString(key.Name), key.Secret, position, enabled, expiresAt, now, key.ID)
		}
		if err != nil {
			return err
		}
		delete(existing, key.ID)
	}
	for keyID := range existing {
		var references int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM attempts WHERE upstream_key_id = ?`, keyID).Scan(&references); err != nil {
			return err
		}
		if references > 0 {
			_, err = tx.Exec(`UPDATE upstream_keys SET enabled = 0, archived_at = ?, updated_at = ? WHERE id = ?`, now, now, keyID)
		} else {
			_, err = tx.Exec(`DELETE FROM upstream_keys WHERE id = ?`, keyID)
		}
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func providerUsedProtocols(tx *sql.Tx, providerID int64) (map[string]struct{}, error) {
	rows, err := tx.Query(`SELECT DISTINCT cp.protocol FROM model_candidates c JOIN candidate_protocols cp ON cp.candidate_id = c.id JOIN virtual_models m ON m.id = c.virtual_model_id WHERE c.provider_id = ? AND c.archived_at IS NULL AND m.archived_at IS NULL`, providerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[string]struct{})
	for rows.Next() {
		var protocol string
		if err := rows.Scan(&protocol); err != nil {
			return nil, err
		}
		result[protocol] = struct{}{}
	}
	return result, rows.Err()
}

func (s *Store) DeleteProvider(id int64) (DeleteResult, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return DeleteResult{}, err
	}
	defer tx.Rollback()
	var exists, activeCandidates, allCandidates, attempts int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM providers WHERE id = ? AND archived_at IS NULL`, id).Scan(&exists); err != nil {
		return DeleteResult{}, err
	}
	if exists == 0 {
		return DeleteResult{}, sql.ErrNoRows
	}
	if err := tx.QueryRow(`SELECT COUNT(*) FROM model_candidates c JOIN virtual_models m ON m.id = c.virtual_model_id WHERE c.provider_id = ? AND c.archived_at IS NULL AND m.archived_at IS NULL`, id).Scan(&activeCandidates); err != nil {
		return DeleteResult{}, err
	}
	if activeCandidates > 0 {
		return DeleteResult{}, ErrProviderInUse
	}
	if err := tx.QueryRow(`SELECT COUNT(*) FROM model_candidates WHERE provider_id = ?`, id).Scan(&allCandidates); err != nil {
		return DeleteResult{}, err
	}
	if err := tx.QueryRow(`SELECT COUNT(*) FROM attempts WHERE provider_id = ?`, id).Scan(&attempts); err != nil {
		return DeleteResult{}, err
	}
	now := formatTime(time.Now())
	result := DeleteResult{Archived: allCandidates > 0 || attempts > 0}
	if result.Archived {
		if _, err := tx.Exec(`UPDATE providers SET enabled = 0, archived_at = ?, updated_at = ? WHERE id = ?`, now, now, id); err != nil {
			return DeleteResult{}, err
		}
		if _, err := tx.Exec(`UPDATE upstream_keys SET enabled = 0, archived_at = ?, updated_at = ? WHERE provider_id = ? AND archived_at IS NULL`, now, now, id); err != nil {
			return DeleteResult{}, err
		}
	} else {
		if _, err := tx.Exec(`DELETE FROM provider_endpoints WHERE provider_id = ?`, id); err != nil {
			return DeleteResult{}, err
		}
		if _, err := tx.Exec(`DELETE FROM upstream_keys WHERE provider_id = ?`, id); err != nil {
			return DeleteResult{}, err
		}
		if _, err := tx.Exec(`DELETE FROM providers WHERE id = ?`, id); err != nil {
			return DeleteResult{}, err
		}
	}
	return result, tx.Commit()
}

func providerKeyIDs(tx *sql.Tx, providerID int64) (map[int64]struct{}, error) {
	rows, err := tx.Query(`SELECT id FROM upstream_keys WHERE provider_id = ? AND archived_at IS NULL`, providerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[int64]struct{})
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		result[id] = struct{}{}
	}
	return result, rows.Err()
}

func (s *Store) providerEndpoints(providerID int64) (map[string]string, error) {
	rows, err := s.db.Query(`SELECT protocol, url FROM provider_endpoints WHERE provider_id = ?`, providerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[string]string)
	for rows.Next() {
		var protocol, url string
		if err := rows.Scan(&protocol, &url); err != nil {
			return nil, err
		}
		result[protocol] = url
	}
	return result, rows.Err()
}

func (s *Store) providerKeys(providerID int64, includeSecret bool) ([]UpstreamKey, error) {
	rows, err := s.db.Query(`SELECT id, COALESCE(name, ''), secret, position, enabled, expires_at, runtime_status, COALESCE(runtime_reason, ''), recover_at, last_used_at FROM upstream_keys WHERE provider_id = ? AND archived_at IS NULL ORDER BY position`, providerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var keys []UpstreamKey
	for rows.Next() {
		var key UpstreamKey
		var expiresAt, recoverAt, lastUsedAt sql.NullString
		if err := rows.Scan(&key.ID, &key.Name, &key.Secret, &key.Position, &key.Enabled, &expiresAt, &key.RuntimeStatus, &key.RuntimeReason, &recoverAt, &lastUsedAt); err != nil {
			return nil, err
		}
		key.SecretHint = secretHint(key.Secret)
		if !includeSecret {
			key.Secret = ""
		}
		if key.ExpiresAt, err = parseNullableTime(expiresAt); err != nil {
			return nil, err
		}
		if key.RecoverAt, err = parseNullableTime(recoverAt); err != nil {
			return nil, err
		}
		if key.LastUsedAt, err = parseNullableTime(lastUsedAt); err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	return keys, rows.Err()
}

type VirtualModel struct {
	ID         int64            `json:"id"`
	Name       string           `json:"name"`
	Enabled    bool             `json:"enabled"`
	Candidates []ModelCandidate `json:"candidates"`
	CreatedAt  time.Time        `json:"created_at"`
}

type VirtualModelPage struct {
	Items    []VirtualModel `json:"items"`
	Total    int            `json:"total"`
	Page     int            `json:"page"`
	PageSize int            `json:"page_size"`
}

type ModelCandidate struct {
	ID                     int64               `json:"id"`
	ProviderID             int64               `json:"provider_id"`
	ProviderName           string              `json:"provider_name"`
	UpstreamModel          string              `json:"upstream_model"`
	Position               int                 `json:"position"`
	Enabled                bool                `json:"enabled"`
	DefaultMaxOutputTokens int                 `json:"default_max_output_tokens"`
	MaxOutputTokens        int                 `json:"max_output_tokens"`
	RuntimeStatus          string              `json:"runtime_status"`
	Protocols              []CandidateProtocol `json:"protocols"`
}

type CandidateProtocol struct {
	Protocol              string   `json:"protocol"`
	Position              int      `json:"position"`
	SupportsStream        bool     `json:"supports_stream"`
	SupportsTools         bool     `json:"supports_tools"`
	SupportsParallelTools bool     `json:"supports_parallel_tools"`
	EffortLevels          []string `json:"effort_levels"`
	SupportsStreamUsage   bool     `json:"supports_stream_usage"`
}

type CreateVirtualModelInput struct {
	Name       string
	Candidates []CreateCandidateInput
}

type CreateCandidateInput struct {
	ID                     int64               `json:"id"`
	ProviderID             int64               `json:"provider_id"`
	UpstreamModel          string              `json:"upstream_model"`
	DefaultMaxOutputTokens int                 `json:"default_max_output_tokens"`
	MaxOutputTokens        int                 `json:"max_output_tokens"`
	Protocols              []CandidateProtocol `json:"protocols"`
}

func (s *Store) ListVirtualModels() ([]VirtualModel, error) {
	rows, err := s.db.Query(`SELECT id, name, enabled, created_at FROM virtual_models WHERE archived_at IS NULL ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	models := make([]VirtualModel, 0)
	for rows.Next() {
		model, err := s.scanVirtualModel(rows)
		if err != nil {
			return nil, err
		}
		models = append(models, model)
	}
	return models, rows.Err()
}

func (s *Store) ListVirtualModelsPage(page, pageSize int, name string, enabled *bool) (VirtualModelPage, error) {
	where := "archived_at IS NULL"
	args := []any{}
	if name != "" {
		where += " AND instr(name, ?) > 0"
		args = append(args, name)
	}
	if enabled != nil {
		where += " AND enabled = ?"
		args = append(args, *enabled)
	}

	var total int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM virtual_models WHERE "+where, args...).Scan(&total); err != nil {
		return VirtualModelPage{}, err
	}
	listArgs := append([]any(nil), args...)
	listArgs = append(listArgs, pageSize, (page-1)*pageSize)
	rows, err := s.db.Query(`SELECT id, name, enabled, created_at FROM virtual_models WHERE `+where+` ORDER BY name LIMIT ? OFFSET ?`, listArgs...)
	if err != nil {
		return VirtualModelPage{}, err
	}
	defer rows.Close()
	models := make([]VirtualModel, 0)
	for rows.Next() {
		model, err := s.scanVirtualModel(rows)
		if err != nil {
			return VirtualModelPage{}, err
		}
		models = append(models, model)
	}
	if err := rows.Err(); err != nil {
		return VirtualModelPage{}, err
	}
	return VirtualModelPage{Items: models, Total: total, Page: page, PageSize: pageSize}, nil
}

func (s *Store) scanVirtualModel(row rowScanner) (VirtualModel, error) {
	var model VirtualModel
	var createdAt string
	if err := row.Scan(&model.ID, &model.Name, &model.Enabled, &createdAt); err != nil {
		return VirtualModel{}, err
	}
	var err error
	model.CreatedAt, err = time.Parse(timestampLayout, createdAt)
	if err != nil {
		return VirtualModel{}, err
	}
	model.Candidates, err = s.modelCandidates(model.ID)
	if err != nil {
		return VirtualModel{}, err
	}
	return model, nil
}

func (s *Store) VirtualModelName(id int64) (string, error) {
	var name string
	err := s.db.QueryRow(`SELECT name FROM virtual_models WHERE id = ? AND archived_at IS NULL`, id).Scan(&name)
	return name, err
}

func (s *Store) CreateVirtualModel(input CreateVirtualModelInput) (VirtualModel, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return VirtualModel{}, err
	}
	defer tx.Rollback()
	if err := validateCandidateProtocols(tx, input.Candidates); err != nil {
		return VirtualModel{}, err
	}
	now := time.Now().UTC()
	if _, err := tx.Exec(`UPDATE virtual_models SET name = name || '#archived-' || id, updated_at = ? WHERE name = ? AND archived_at IS NOT NULL`, formatTime(now), input.Name); err != nil {
		return VirtualModel{}, err
	}
	result, err := tx.Exec(`INSERT INTO virtual_models (name, enabled, created_at, updated_at) VALUES (?, 1, ?, ?)`, input.Name, formatTime(now), formatTime(now))
	if err != nil {
		return VirtualModel{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return VirtualModel{}, err
	}
	for position, candidate := range input.Candidates {
		candidateResult, err := tx.Exec(`INSERT INTO model_candidates (virtual_model_id, provider_id, upstream_model, position, enabled, default_max_output_tokens, max_output_tokens, created_at, updated_at) VALUES (?, ?, ?, ?, 1, ?, ?, ?, ?)`, id, candidate.ProviderID, candidate.UpstreamModel, position, candidate.DefaultMaxOutputTokens, candidate.MaxOutputTokens, formatTime(now), formatTime(now))
		if err != nil {
			return VirtualModel{}, err
		}
		candidateID, err := candidateResult.LastInsertId()
		if err != nil {
			return VirtualModel{}, err
		}
		for protocolPosition, protocol := range candidate.Protocols {
			effortLevels, err := json.Marshal(protocol.EffortLevels)
			if err != nil {
				return VirtualModel{}, err
			}
			if _, err := tx.Exec(`INSERT INTO candidate_protocols (candidate_id, protocol, position, supports_stream, supports_tools, supports_parallel_tools, effort_levels_json, supports_stream_usage) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, candidateID, protocol.Protocol, protocolPosition, protocol.SupportsStream, protocol.SupportsTools, protocol.SupportsParallelTools, string(effortLevels), protocol.SupportsStreamUsage); err != nil {
				return VirtualModel{}, err
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return VirtualModel{}, err
	}
	return VirtualModel{ID: id, Name: input.Name, Enabled: true, CreatedAt: now}, nil
}

func (s *Store) SetVirtualModelEnabled(id int64, enabled bool) error {
	return updateEnabled(s.db, "virtual_models", id, enabled)
}

func (s *Store) UpdateVirtualModel(id int64, input CreateVirtualModelInput) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := validateCandidateProtocols(tx, input.Candidates); err != nil {
		return err
	}
	now := formatTime(time.Now())
	if _, err := tx.Exec(`UPDATE virtual_models SET name = name || '#archived-' || id, updated_at = ? WHERE name = ? AND archived_at IS NOT NULL`, now, input.Name); err != nil {
		return err
	}
	result, err := tx.Exec(`UPDATE virtual_models SET name = ?, updated_at = ? WHERE id = ? AND archived_at IS NULL`, input.Name, now, id)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return sql.ErrNoRows
	}
	existing, err := candidateIDs(tx, id)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE model_candidates SET position = -id WHERE virtual_model_id = ?`, id); err != nil {
		return err
	}
	for position, candidate := range input.Candidates {
		candidateID := candidate.ID
		if candidateID == 0 {
			candidateResult, err := tx.Exec(`INSERT INTO model_candidates (virtual_model_id, provider_id, upstream_model, position, enabled, default_max_output_tokens, max_output_tokens, created_at, updated_at) VALUES (?, ?, ?, ?, 1, ?, ?, ?, ?)`, id, candidate.ProviderID, candidate.UpstreamModel, position, candidate.DefaultMaxOutputTokens, candidate.MaxOutputTokens, now, now)
			if err != nil {
				return err
			}
			candidateID, err = candidateResult.LastInsertId()
			if err != nil {
				return err
			}
		} else {
			if _, ok := existing[candidateID]; !ok {
				return sql.ErrNoRows
			}
			if _, err := tx.Exec(`UPDATE model_candidates SET provider_id = ?, upstream_model = ?, position = ?, default_max_output_tokens = ?, max_output_tokens = ?, runtime_status = 'available', runtime_reason = NULL, updated_at = ? WHERE id = ?`, candidate.ProviderID, candidate.UpstreamModel, position, candidate.DefaultMaxOutputTokens, candidate.MaxOutputTokens, now, candidateID); err != nil {
				return err
			}
			delete(existing, candidateID)
		}
		if _, err := tx.Exec(`DELETE FROM candidate_protocols WHERE candidate_id = ?`, candidateID); err != nil {
			return err
		}
		if err := insertCandidateProtocols(tx, candidateID, candidate.Protocols); err != nil {
			return err
		}
	}
	for candidateID := range existing {
		var references int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM attempts WHERE candidate_id = ?`, candidateID).Scan(&references); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM candidate_protocols WHERE candidate_id = ?`, candidateID); err != nil {
			return err
		}
		if references > 0 {
			_, err = tx.Exec(`UPDATE model_candidates SET enabled = 0, archived_at = ?, updated_at = ? WHERE id = ?`, now, now, candidateID)
		} else {
			_, err = tx.Exec(`DELETE FROM model_candidates WHERE id = ?`, candidateID)
		}
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func validateCandidateProtocols(tx *sql.Tx, candidates []CreateCandidateInput) error {
	for _, candidate := range candidates {
		for _, protocol := range candidate.Protocols {
			var exists int
			if err := tx.QueryRow(`SELECT COUNT(*) FROM provider_endpoints WHERE provider_id = ? AND protocol = ?`, candidate.ProviderID, protocol.Protocol).Scan(&exists); err != nil {
				return err
			}
			if exists == 0 {
				return fmt.Errorf("供应商未配置协议端点：%s", protocol.Protocol)
			}
		}
	}
	return nil
}

func (s *Store) DeleteVirtualModel(id int64) (DeleteResult, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return DeleteResult{}, err
	}
	defer tx.Rollback()
	var name string
	if err := tx.QueryRow(`SELECT name FROM virtual_models WHERE id = ? AND archived_at IS NULL`, id).Scan(&name); err != nil {
		return DeleteResult{}, err
	}
	var requests, attempts int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM requests WHERE virtual_model = ?`, name).Scan(&requests); err != nil {
		return DeleteResult{}, err
	}
	if err := tx.QueryRow(`SELECT COUNT(*) FROM attempts WHERE candidate_id IN (SELECT id FROM model_candidates WHERE virtual_model_id = ?)`, id).Scan(&attempts); err != nil {
		return DeleteResult{}, err
	}
	result := DeleteResult{Archived: requests > 0 || attempts > 0}
	if result.Archived {
		now := formatTime(time.Now())
		if _, err := tx.Exec(`UPDATE virtual_models SET enabled = 0, archived_at = ?, updated_at = ? WHERE id = ?`, now, now, id); err != nil {
			return DeleteResult{}, err
		}
		if _, err := tx.Exec(`UPDATE model_candidates SET enabled = 0, archived_at = ?, updated_at = ? WHERE virtual_model_id = ? AND archived_at IS NULL`, now, now, id); err != nil {
			return DeleteResult{}, err
		}
	} else {
		if _, err := tx.Exec(`DELETE FROM candidate_protocols WHERE candidate_id IN (SELECT id FROM model_candidates WHERE virtual_model_id = ?)`, id); err != nil {
			return DeleteResult{}, err
		}
		if _, err := tx.Exec(`DELETE FROM model_candidates WHERE virtual_model_id = ?`, id); err != nil {
			return DeleteResult{}, err
		}
		if _, err := tx.Exec(`DELETE FROM virtual_models WHERE id = ?`, id); err != nil {
			return DeleteResult{}, err
		}
	}
	return result, tx.Commit()
}

func candidateIDs(tx *sql.Tx, modelID int64) (map[int64]struct{}, error) {
	rows, err := tx.Query(`SELECT id FROM model_candidates WHERE virtual_model_id = ? AND archived_at IS NULL`, modelID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[int64]struct{})
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		result[id] = struct{}{}
	}
	return result, rows.Err()
}

func insertCandidateProtocols(tx *sql.Tx, candidateID int64, protocols []CandidateProtocol) error {
	for position, protocol := range protocols {
		effortLevels, err := json.Marshal(protocol.EffortLevels)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO candidate_protocols (candidate_id, protocol, position, supports_stream, supports_tools, supports_parallel_tools, effort_levels_json, supports_stream_usage) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, candidateID, protocol.Protocol, position, protocol.SupportsStream, protocol.SupportsTools, protocol.SupportsParallelTools, string(effortLevels), protocol.SupportsStreamUsage); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) modelCandidates(modelID int64) ([]ModelCandidate, error) {
	rows, err := s.db.Query(`SELECT c.id, c.provider_id, p.name, c.upstream_model, c.position, c.enabled, c.default_max_output_tokens, c.max_output_tokens, c.runtime_status FROM model_candidates c JOIN providers p ON p.id = c.provider_id WHERE c.virtual_model_id = ? AND c.archived_at IS NULL ORDER BY c.position`, modelID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var candidates []ModelCandidate
	for rows.Next() {
		var candidate ModelCandidate
		if err := rows.Scan(&candidate.ID, &candidate.ProviderID, &candidate.ProviderName, &candidate.UpstreamModel, &candidate.Position, &candidate.Enabled, &candidate.DefaultMaxOutputTokens, &candidate.MaxOutputTokens, &candidate.RuntimeStatus); err != nil {
			return nil, err
		}
		candidate.Protocols, err = s.candidateProtocols(candidate.ID)
		if err != nil {
			return nil, err
		}
		candidates = append(candidates, candidate)
	}
	return candidates, rows.Err()
}

func (s *Store) candidateProtocols(candidateID int64) ([]CandidateProtocol, error) {
	rows, err := s.db.Query(`SELECT protocol, position, supports_stream, supports_tools, supports_parallel_tools, effort_levels_json, supports_stream_usage FROM candidate_protocols WHERE candidate_id = ? ORDER BY position`, candidateID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var protocols []CandidateProtocol
	for rows.Next() {
		var protocol CandidateProtocol
		var effortLevelsJSON string
		if err := rows.Scan(&protocol.Protocol, &protocol.Position, &protocol.SupportsStream, &protocol.SupportsTools, &protocol.SupportsParallelTools, &effortLevelsJSON, &protocol.SupportsStreamUsage); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(effortLevelsJSON), &protocol.EffortLevels); err != nil {
			return nil, err
		}
		protocols = append(protocols, protocol)
	}
	return protocols, rows.Err()
}

func updateEnabled(db *sql.DB, table string, id int64, enabled bool) error {
	query := fmt.Sprintf("UPDATE %s SET enabled = ?, updated_at = ? WHERE id = ? AND archived_at IS NULL", table)
	result, err := db.Exec(query, enabled, formatTime(time.Now()), id)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func parseNullableTime(value sql.NullString) (*time.Time, error) {
	if !value.Valid {
		return nil, nil
	}
	parsed, err := time.Parse(timestampLayout, value.String)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

func nullableTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return formatTime(*value)
}

func optionalBool(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}

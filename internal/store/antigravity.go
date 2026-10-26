package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Sanjeever/model-confluence/internal/antigravity"
)

type AntigravityCredential struct {
	antigravity.Credential
	Revision int64
}

func credentialDocuments(c antigravity.Credential) ([]byte, []byte, error) {
	if c.Subject == "" || c.Email == "" || c.ProjectID == "" || c.ClientID == "" || c.AccessToken == "" || c.RefreshToken == "" || c.ExpiresAt.IsZero() {
		return nil, nil, errors.New("Antigravity 凭据或已验证身份不完整")
	}
	metadata, err := json.Marshal(struct {
		antigravity.Identity
		LastRefresh time.Time `json:"last_refresh"`
	}{c.Identity, c.LastRefresh})
	if err != nil {
		return nil, nil, err
	}
	data, err := json.Marshal(c)
	return metadata, data, err
}

func (s *Store) ImportAntigravityCredential(providerID int64, fileName string, c antigravity.Credential) (UpstreamKey, error) {
	metadata, data, err := credentialDocuments(c)
	if err != nil {
		return UpstreamKey{}, err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return UpstreamKey{}, err
	}
	defer tx.Rollback()
	var kind string
	if err := tx.QueryRow(`SELECT kind FROM providers WHERE id = ? AND archived_at IS NULL`, providerID).Scan(&kind); err != nil {
		return UpstreamKey{}, err
	}
	if kind != ProviderAntigravityOAuth {
		return UpstreamKey{}, errors.New("供应商不是 Antigravity OAuth")
	}
	var count int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM antigravity_credentials WHERE subject = ?`, c.Subject).Scan(&count); err != nil {
		return UpstreamKey{}, err
	}
	if count > 0 {
		return UpstreamKey{}, ErrDuplicateOAuthAccount
	}
	now := formatTime(time.Now())
	result, err := tx.Exec(`INSERT INTO upstream_keys(provider_id, name, secret, file_name, access_token_expires_at, position, created_at, updated_at) VALUES (?, ?, '', ?, ?, (SELECT COALESCE(MAX(position), -1)+1 FROM upstream_keys WHERE provider_id = ?), ?, ?)`, providerID, c.Email, fileName, formatTime(c.ExpiresAt), providerID, now, now)
	if err != nil {
		return UpstreamKey{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return UpstreamKey{}, err
	}
	if _, err := tx.Exec(`INSERT INTO antigravity_credentials(upstream_key_id, subject, identity_json, credential_json) VALUES (?, ?, ?, ?)`, id, c.Subject, string(metadata), string(data)); err != nil {
		return UpstreamKey{}, err
	}
	if err := tx.Commit(); err != nil {
		return UpstreamKey{}, err
	}
	s.invalidateConfig()
	return UpstreamKey{ID: id, Name: c.Email, FileName: fileName, Enabled: true, RuntimeStatus: "available", AntigravityIdentity: &c.Identity, AccessTokenExpiresAt: &c.ExpiresAt, LastRefresh: &c.LastRefresh, CredentialRevision: 1}, nil
}

func (s *Store) AntigravityCredential(id int64) (AntigravityCredential, error) {
	var result AntigravityCredential
	var raw string
	var enabled, providerEnabled bool
	var status string
	var recoverAt sql.NullString
	err := s.db.QueryRow(`SELECT c.credential_json, k.credential_revision, k.enabled, p.enabled, k.runtime_status, k.recover_at FROM antigravity_credentials c JOIN upstream_keys k ON k.id = c.upstream_key_id JOIN providers p ON p.id = k.provider_id WHERE k.id = ? AND k.archived_at IS NULL AND p.archived_at IS NULL AND p.kind = ?`, id, ProviderAntigravityOAuth).Scan(&raw, &result.Revision, &enabled, &providerEnabled, &status, &recoverAt)
	if err != nil {
		return result, err
	}
	recover, err := parseNullableTime(recoverAt)
	if err != nil {
		return result, err
	}
	if !providerEnabled || !KeyAvailable(UpstreamKey{Enabled: enabled, RuntimeStatus: status, RecoverAt: recover}) {
		return result, ErrCredentialChanged
	}
	if err := json.Unmarshal([]byte(raw), &result.Credential); err != nil {
		return result, fmt.Errorf("读取 Antigravity 凭据: %w", err)
	}
	return result, nil
}

func (s *Store) SaveAntigravityCredential(id, revision int64, c antigravity.Credential) error {
	metadata, data, err := credentialDocuments(c)
	if err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.Exec(`UPDATE upstream_keys SET access_token_expires_at = ?, credential_revision = credential_revision + 1, updated_at = ? WHERE id = ? AND credential_revision = ? AND enabled = 1 AND archived_at IS NULL AND EXISTS (SELECT 1 FROM providers p WHERE p.id = upstream_keys.provider_id AND p.kind = ? AND p.enabled = 1 AND p.archived_at IS NULL) AND EXISTS (SELECT 1 FROM antigravity_credentials c WHERE c.upstream_key_id = upstream_keys.id AND c.subject = ?)`, formatTime(c.ExpiresAt), formatTime(time.Now()), id, revision, ProviderAntigravityOAuth, c.Subject)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrCredentialChanged
	}
	if _, err := tx.Exec(`UPDATE antigravity_credentials SET identity_json = ?, credential_json = ? WHERE upstream_key_id = ?`, string(metadata), string(data), id); err != nil {
		return err
	}
	// 刷新令牌不清除同一配置版本的模型限流。
	if _, err := tx.Exec(`UPDATE antigravity_model_states SET credential_revision = ? WHERE upstream_key_id = ? AND credential_revision = ?`, revision+1, id, revision); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.invalidateConfig()
	return nil
}

func (s *Store) MarkAntigravityModel(id, revision int64, model, reason string, recoverAt time.Time) error {
	_, err := s.db.Exec(`INSERT INTO antigravity_model_states(upstream_key_id, upstream_model, credential_revision, reason, recover_at) SELECT k.id, ?, ?, ?, ? FROM upstream_keys k JOIN providers p ON p.id = k.provider_id WHERE k.id = ? AND k.credential_revision = ? AND k.enabled = 1 AND k.archived_at IS NULL AND p.enabled = 1 AND p.archived_at IS NULL ON CONFLICT(upstream_key_id, upstream_model) DO UPDATE SET credential_revision = excluded.credential_revision, reason = excluded.reason, recover_at = excluded.recover_at`, model, revision, reason, formatTime(recoverAt), id, revision)
	return err
}

func (s *Store) antigravityModelAvailable(id, revision int64, model string) (bool, error) {
	var blocked int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM antigravity_model_states WHERE upstream_key_id = ? AND upstream_model = ? AND credential_revision = ? AND recover_at > ?`, id, model, revision, formatTime(time.Now())).Scan(&blocked)
	return blocked == 0, err
}

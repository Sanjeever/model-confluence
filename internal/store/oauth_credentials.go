package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/Sanjeever/model-confluence/internal/codex"
)

var ErrDuplicateOAuthAccount = errors.New("该用户与工作区已有活动账号")
var ErrCredentialChanged = errors.New("账号配置或凭据已变化")

func ensureOAuthColumns(tx *sql.Tx) error {
	columns := []struct{ table, name, definition string }{
		{"providers", "kind", "TEXT NOT NULL DEFAULT 'generic'"},
		{"upstream_keys", "auth_json", "TEXT NOT NULL DEFAULT ''"},
		{"upstream_keys", "file_name", "TEXT NOT NULL DEFAULT ''"},
		{"upstream_keys", "oauth_user_id", "TEXT NOT NULL DEFAULT ''"},
		{"upstream_keys", "oauth_account_id", "TEXT NOT NULL DEFAULT ''"},
		{"upstream_keys", "access_token_expires_at", "TEXT"},
		{"upstream_keys", "credential_revision", "INTEGER NOT NULL DEFAULT 1"},
	}
	for _, c := range columns {
		if err := ensureColumn(tx, c.table, c.name, c.definition); err != nil {
			return err
		}
	}
	_, err := tx.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_oauth_identity ON upstream_keys(oauth_user_id, oauth_account_id) WHERE archived_at IS NULL AND oauth_user_id != ''`)
	return err
}

func (s *Store) ImportOAuthCredential(providerID int64, fileName string, data []byte) (UpstreamKey, error) {
	auth, err := codex.ParseAuth(data)
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
	if kind != ProviderCodexOAuth {
		return UpstreamKey{}, fmt.Errorf("供应商不支持 OAuth 文件")
	}
	var count int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM upstream_keys WHERE oauth_user_id = ? AND oauth_account_id = ? AND archived_at IS NULL`, auth.Identity.UserID, auth.Identity.AccountID).Scan(&count); err != nil {
		return UpstreamKey{}, err
	}
	if count > 0 {
		return UpstreamKey{}, ErrDuplicateOAuthAccount
	}
	now := formatTime(time.Now())
	result, err := tx.Exec(`INSERT INTO upstream_keys(provider_id, name, secret, auth_json, file_name, oauth_user_id, oauth_account_id, access_token_expires_at, position, created_at, updated_at) VALUES (?, ?, '', ?, ?, ?, ?, ?, (SELECT COALESCE(MAX(position), -1)+1 FROM upstream_keys WHERE provider_id = ?), ?, ?)`, providerID, fileName, string(data), fileName, auth.Identity.UserID, auth.Identity.AccountID, formatTime(auth.ExpiresAt), providerID, now, now)
	if err != nil {
		return UpstreamKey{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return UpstreamKey{}, err
	}
	if err := tx.Commit(); err != nil {
		return UpstreamKey{}, err
	}
	s.invalidateConfig()
	return UpstreamKey{ID: id, Name: fileName, FileName: fileName, Enabled: true, RuntimeStatus: "available", OAuthIdentity: &auth.Identity, AccessTokenExpiresAt: &auth.ExpiresAt, LastRefresh: auth.LastRefresh, CredentialRevision: 1}, nil
}

type OAuthCredential struct {
	Auth     codex.Auth
	Revision int64
}

func (s *Store) OAuthCredential(id int64) (OAuthCredential, error) {
	var c OAuthCredential
	var raw string
	var enabled, providerEnabled bool
	var status string
	var recoverAt sql.NullString
	err := s.db.QueryRow(`SELECT k.auth_json, k.credential_revision, k.enabled, p.enabled, k.runtime_status, k.recover_at FROM upstream_keys k JOIN providers p ON p.id = k.provider_id WHERE k.id = ? AND k.archived_at IS NULL AND p.archived_at IS NULL AND p.kind = ?`, id, ProviderCodexOAuth).Scan(&raw, &c.Revision, &enabled, &providerEnabled, &status, &recoverAt)
	if err != nil {
		return c, err
	}
	recover, err := parseNullableTime(recoverAt)
	if err != nil {
		return c, err
	}
	if !providerEnabled || !KeyAvailable(UpstreamKey{Enabled: enabled, RuntimeStatus: status, RecoverAt: recover}) {
		return c, ErrCredentialChanged
	}
	c.Auth, err = codex.ParseAuth([]byte(raw))
	return c, err
}

func (s *Store) SaveOAuthCredential(id, revision int64, data []byte) error {
	auth, err := codex.ParseAuth(data)
	if err != nil {
		return err
	}
	result, err := s.db.Exec(`UPDATE upstream_keys SET auth_json = ?, access_token_expires_at = ?, credential_revision = credential_revision + 1, runtime_status = 'available', runtime_reason = NULL, recover_at = NULL, updated_at = ? WHERE id = ? AND credential_revision = ? AND archived_at IS NULL AND enabled = 1 AND oauth_user_id = ? AND oauth_account_id = ? AND EXISTS (SELECT 1 FROM providers p WHERE p.id = upstream_keys.provider_id AND p.enabled = 1 AND p.archived_at IS NULL)`, string(data), formatTime(auth.ExpiresAt), formatTime(time.Now()), id, revision, auth.Identity.UserID, auth.Identity.AccountID)
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
	s.invalidateRoutes()
	return nil
}

func (s *Store) MarkUpstreamKeyRevision(id, revision int64, status, reason string, recoverAt *time.Time) error {
	_, err := s.db.Exec(`UPDATE upstream_keys SET runtime_status = ?, runtime_reason = ?, recover_at = ?, updated_at = ? WHERE id = ? AND credential_revision = ? AND archived_at IS NULL AND enabled = 1 AND EXISTS (SELECT 1 FROM providers p WHERE p.id = upstream_keys.provider_id AND p.enabled = 1 AND p.archived_at IS NULL)`, status, nullableString(reason), nullableTime(recoverAt), formatTime(time.Now()), id, revision)
	if err == nil {
		s.invalidateRoutes()
	}
	return err
}

func (s *Store) SetUpstreamKeyEnabled(id int64, enabled bool) error {
	result, err := s.db.Exec(`UPDATE upstream_keys SET enabled = ?, credential_revision = credential_revision + 1, updated_at = ? WHERE id = ? AND archived_at IS NULL`, enabled, formatTime(time.Now()), id)
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
	s.invalidateConfig()
	return nil
}

func (s *Store) DeleteOAuthCredential(providerID, id int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var kind string
	if err := tx.QueryRow(`SELECT p.kind FROM upstream_keys k JOIN providers p ON p.id = k.provider_id WHERE k.id = ? AND k.provider_id = ? AND k.archived_at IS NULL AND p.archived_at IS NULL`, id, providerID).Scan(&kind); err != nil {
		return err
	}
	if kind != ProviderCodexOAuth {
		return fmt.Errorf("供应商不支持 OAuth 文件")
	}
	// 归档保留日志引用；当前凭据立即清空，身份索引释放。
	now := formatTime(time.Now())
	if _, err := tx.Exec(`UPDATE upstream_keys SET enabled = 0, auth_json = '', credential_revision = credential_revision + 1, archived_at = ?, updated_at = ? WHERE id = ?`, now, now, id); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.invalidateConfig()
	return nil
}

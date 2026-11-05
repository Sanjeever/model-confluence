package store

import (
	"github.com/Sanjeever/model-confluence/internal/antigravity"
	"github.com/Sanjeever/model-confluence/internal/codex"
	"time"
)

func (s *Store) ReplaceOAuthCredential(providerID, id, revision int64, data []byte) error {
	auth, err := codex.ParseAuth(data)
	if err != nil {
		return err
	}
	result, err := s.db.Exec(`UPDATE upstream_keys SET auth_json = ?, access_token_expires_at = ?, credential_revision = credential_revision + 1, runtime_status = 'available', runtime_reason = NULL, recover_at = NULL, updated_at = ? WHERE id = ? AND provider_id = ? AND credential_revision = ? AND archived_at IS NULL AND oauth_user_id = ? AND oauth_account_id = ? AND EXISTS (SELECT 1 FROM providers p WHERE p.id = upstream_keys.provider_id AND p.kind = ? AND p.archived_at IS NULL)`, string(data), formatTime(auth.ExpiresAt), formatTime(time.Now()), id, providerID, revision, auth.Identity.UserID, auth.Identity.AccountID, ProviderCodexOAuth)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrCredentialChanged
	}
	s.invalidateConfig()
	return nil
}

func (s *Store) ReplaceAntigravityCredential(providerID, id, revision int64, c antigravity.Credential) error {
	metadata, data, err := credentialDocuments(c)
	if err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.Exec(`UPDATE upstream_keys SET access_token_expires_at = ?, credential_revision = credential_revision + 1, runtime_status = 'available', runtime_reason = NULL, recover_at = NULL, updated_at = ? WHERE id = ? AND provider_id = ? AND credential_revision = ? AND archived_at IS NULL AND EXISTS (SELECT 1 FROM antigravity_credentials c WHERE c.upstream_key_id = upstream_keys.id AND c.subject = ? AND json_extract(c.identity_json, '$.project_id') = ?) AND EXISTS (SELECT 1 FROM providers p WHERE p.id = upstream_keys.provider_id AND p.kind = ? AND p.archived_at IS NULL)`, formatTime(c.ExpiresAt), formatTime(time.Now()), id, providerID, revision, c.Subject, c.ProjectID, ProviderAntigravityOAuth)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrCredentialChanged
	}
	if _, err := tx.Exec(`UPDATE antigravity_credentials SET identity_json = ?, credential_json = ? WHERE upstream_key_id = ?`, string(metadata), string(data), id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM antigravity_model_states WHERE upstream_key_id = ?`, id); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.invalidateConfig()
	return nil
}

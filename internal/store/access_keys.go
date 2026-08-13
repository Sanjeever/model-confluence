package store

import (
	"database/sql"
	"errors"
	"time"
)

type AccessKey struct {
	ID         int64      `json:"id"`
	Name       string     `json:"name"`
	Secret     string     `json:"secret,omitempty"`
	SecretHint string     `json:"secret_hint"`
	Enabled    bool       `json:"enabled"`
	ExpiresAt  *time.Time `json:"expires_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
	CreatedAt  time.Time  `json:"created_at"`
}

type DeleteResult struct {
	Archived bool `json:"archived"`
}

type AccessKeyPage struct {
	Items    []AccessKey `json:"items"`
	Total    int         `json:"total"`
	Page     int         `json:"page"`
	PageSize int         `json:"page_size"`
}

func (s *Store) ListAccessKeys() ([]AccessKey, error) {
	rows, err := s.db.Query(`SELECT id, name, secret, enabled, expires_at, last_used_at, created_at FROM access_keys WHERE archived_at IS NULL ORDER BY id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var keys []AccessKey
	for rows.Next() {
		key, err := scanAccessKey(rows)
		if err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	return keys, rows.Err()
}

func (s *Store) ListAccessKeysPage(page, pageSize int, name string, enabled *bool) (AccessKeyPage, error) {
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
	if err := s.db.QueryRow("SELECT COUNT(*) FROM access_keys WHERE "+where, args...).Scan(&total); err != nil {
		return AccessKeyPage{}, err
	}
	listArgs := append([]any(nil), args...)
	listArgs = append(listArgs, pageSize, (page-1)*pageSize)
	rows, err := s.db.Query(`SELECT id, name, secret, enabled, expires_at, last_used_at, created_at FROM access_keys WHERE `+where+` ORDER BY id DESC LIMIT ? OFFSET ?`, listArgs...)
	if err != nil {
		return AccessKeyPage{}, err
	}
	defer rows.Close()
	keys := make([]AccessKey, 0)
	for rows.Next() {
		key, err := scanAccessKey(rows)
		if err != nil {
			return AccessKeyPage{}, err
		}
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		return AccessKeyPage{}, err
	}
	return AccessKeyPage{Items: keys, Total: total, Page: page, PageSize: pageSize}, nil
}

func (s *Store) CreateAccessKey(name string, expiresAt *time.Time) (AccessKey, error) {
	secretValue, err := randomToken(32)
	if err != nil {
		return AccessKey{}, err
	}
	secret := "mc_" + secretValue
	now := time.Now().UTC()
	var expires any
	if expiresAt != nil {
		expires = formatTime(*expiresAt)
	}
	tx, err := s.db.Begin()
	if err != nil {
		return AccessKey{}, err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE access_keys SET name = name || '#archived-' || id, updated_at = ? WHERE name = ? AND archived_at IS NOT NULL`, formatTime(now), name); err != nil {
		return AccessKey{}, err
	}
	result, err := tx.Exec(`INSERT INTO access_keys (name, secret, enabled, expires_at, created_at, updated_at) VALUES (?, ?, 1, ?, ?, ?)`, name, secret, expires, formatTime(now), formatTime(now))
	if err != nil {
		return AccessKey{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return AccessKey{}, err
	}
	if err := tx.Commit(); err != nil {
		return AccessKey{}, err
	}
	return AccessKey{ID: id, Name: name, Secret: secret, SecretHint: secretHint(secret), Enabled: true, ExpiresAt: expiresAt, CreatedAt: now}, nil
}

func (s *Store) SetAccessKeyEnabled(id int64, enabled bool) error {
	result, err := s.db.Exec(`UPDATE access_keys SET enabled = ?, updated_at = ? WHERE id = ? AND archived_at IS NULL`, enabled, formatTime(time.Now()), id)
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

func (s *Store) UpdateAccessKey(id int64, name string, expiresAt *time.Time, enabled bool) error {
	var expires any
	if expiresAt != nil {
		expires = formatTime(*expiresAt)
	}
	result, err := s.db.Exec(`UPDATE access_keys SET name = ?, expires_at = ?, enabled = ?, updated_at = ? WHERE id = ? AND archived_at IS NULL`, name, expires, enabled, formatTime(time.Now()), id)
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

func (s *Store) DeleteAccessKey(id int64) (DeleteResult, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return DeleteResult{}, err
	}
	defer tx.Rollback()
	var exists, references int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM access_keys WHERE id = ? AND archived_at IS NULL`, id).Scan(&exists); err != nil {
		return DeleteResult{}, err
	}
	if exists == 0 {
		return DeleteResult{}, sql.ErrNoRows
	}
	if err := tx.QueryRow(`SELECT COUNT(*) FROM requests WHERE access_key_id = ?`, id).Scan(&references); err != nil {
		return DeleteResult{}, err
	}
	result := DeleteResult{Archived: references > 0}
	if result.Archived {
		now := formatTime(time.Now())
		_, err = tx.Exec(`UPDATE access_keys SET name = name || '#archived-' || id, enabled = 0, archived_at = ?, updated_at = ? WHERE id = ?`, now, now, id)
	} else {
		_, err = tx.Exec(`DELETE FROM access_keys WHERE id = ?`, id)
	}
	if err != nil {
		return DeleteResult{}, err
	}
	return result, tx.Commit()
}

func (s *Store) AuthenticateAccessKey(secret string) (AccessKey, error) {
	row := s.db.QueryRow(`SELECT id, name, secret, enabled, expires_at, last_used_at, created_at FROM access_keys WHERE secret = ? AND archived_at IS NULL`, secret)
	key, err := scanAccessKey(row)
	if err != nil {
		return AccessKey{}, err
	}
	if !key.Enabled {
		return AccessKey{}, errors.New("access key is disabled")
	}
	if key.ExpiresAt != nil && time.Now().After(*key.ExpiresAt) {
		return AccessKey{}, errors.New("access key is expired")
	}
	now := time.Now().UTC()
	if _, err := s.db.Exec(`UPDATE access_keys SET last_used_at = ? WHERE id = ?`, formatTime(now), key.ID); err != nil {
		return AccessKey{}, err
	}
	key.LastUsedAt = &now
	return key, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanAccessKey(row rowScanner) (AccessKey, error) {
	var key AccessKey
	var expiresAt, lastUsedAt sql.NullString
	var createdAt string
	if err := row.Scan(&key.ID, &key.Name, &key.Secret, &key.Enabled, &expiresAt, &lastUsedAt, &createdAt); err != nil {
		return AccessKey{}, err
	}
	key.SecretHint = secretHint(key.Secret)
	created, err := time.Parse(timestampLayout, createdAt)
	if err != nil {
		return AccessKey{}, err
	}
	key.CreatedAt = created
	if expiresAt.Valid {
		value, err := time.Parse(timestampLayout, expiresAt.String)
		if err != nil {
			return AccessKey{}, err
		}
		key.ExpiresAt = &value
	}
	if lastUsedAt.Valid {
		value, err := time.Parse(timestampLayout, lastUsedAt.String)
		if err != nil {
			return AccessKey{}, err
		}
		key.LastUsedAt = &value
	}
	return key, nil
}

func secretHint(secret string) string {
	if len(secret) <= 8 {
		return secret
	}
	return secret[:4] + "…" + secret[len(secret)-4:]
}

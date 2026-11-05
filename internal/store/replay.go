package store

import (
	"database/sql"
	"errors"
	"time"
)

func (s *Store) ReplayAccessKey(id string) (*int64, error) {
	var keyID sql.NullInt64
	if err := s.db.QueryRow(`SELECT access_key_id FROM requests WHERE id = ?`, id).Scan(&keyID); err != nil {
		return nil, err
	}
	if !keyID.Valid {
		return nil, nil
	}
	var enabled bool
	var expires, archived sql.NullString
	if err := s.db.QueryRow(`SELECT enabled, expires_at, archived_at FROM access_keys WHERE id = ?`, keyID.Int64).Scan(&enabled, &expires, &archived); err != nil {
		return nil, err
	}
	expiresAt, err := parseNullableTime(expires)
	if err != nil {
		return nil, err
	}
	if !enabled || archived.Valid || expiresAt != nil && !time.Now().Before(*expiresAt) {
		return nil, errors.New("原访问密钥已停用、归档或过期")
	}
	return &keyID.Int64, nil
}

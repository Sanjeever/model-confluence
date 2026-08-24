package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"
)

const timestampLayout = time.RFC3339Nano

const sessionTouchInterval = 5 * time.Minute

type Store struct {
	db                *sql.DB
	configMu          sync.RWMutex
	routeLoadMu       sync.Mutex
	accessKeyLoadMu   sync.Mutex
	configVersion     uint64
	routeCache        map[RoutingRequirements][]ResolvedRoute
	accessKeyCache    map[string]AccessKey
	modelNamesCache   []string
	modelNamesCached  bool
	touchMu           sync.Mutex
	lastConfigTouches map[configTouchKey]time.Time
}

type configTouchKey struct {
	kind string
	id   int64
}

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create data directory: %w", err)
	}
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(8)
	store := &Store{db: db, routeCache: make(map[RoutingRequirements][]ResolvedRoute), accessKeyCache: make(map[string]AccessKey), lastConfigTouches: make(map[configTouchKey]time.Time)}
	if err := store.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) invalidateConfig() {
	s.configMu.Lock()
	s.configVersion++
	s.routeCache = make(map[RoutingRequirements][]ResolvedRoute)
	s.accessKeyCache = make(map[string]AccessKey)
	s.modelNamesCache = nil
	s.modelNamesCached = false
	s.configMu.Unlock()
	s.touchMu.Lock()
	s.lastConfigTouches = make(map[configTouchKey]time.Time)
	s.touchMu.Unlock()
}

func (s *Store) touchConfig(kind string, id int64, now time.Time, update func() error) error {
	key := configTouchKey{kind: kind, id: id}
	s.touchMu.Lock()
	last := s.lastConfigTouches[key]
	if !last.IsZero() && now.Sub(last) < time.Minute {
		s.touchMu.Unlock()
		return nil
	}
	s.lastConfigTouches[key] = now
	s.touchMu.Unlock()
	if err := update(); err != nil {
		s.touchMu.Lock()
		delete(s.lastConfigTouches, key)
		s.touchMu.Unlock()
		return err
	}
	return nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) Ping() error {
	return s.db.Ping()
}

func (s *Store) migrate() error {
	if _, err := s.db.Exec(`PRAGMA journal_mode = WAL;`); err != nil {
		return fmt.Errorf("configure sqlite: %w", err)
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(schema); err != nil {
		return fmt.Errorf("apply schema: %w", err)
	}
	if err := ensureRequestStreamColumn(tx); err != nil {
		return fmt.Errorf("migrate request stream flag: %w", err)
	}
	if err := ensureLogPayloadColumns(tx); err != nil {
		return fmt.Errorf("migrate log payload columns: %w", err)
	}
	return tx.Commit()
}

func ensureRequestStreamColumn(tx *sql.Tx) error {
	rows, err := tx.Query(`PRAGMA table_info(requests)`)
	if err != nil {
		return err
	}
	found := false
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, columnType string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return err
		}
		if name == "stream" {
			found = true
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if found {
		return nil
	}
	if _, err := tx.Exec(`ALTER TABLE requests ADD COLUMN stream INTEGER NOT NULL DEFAULT 0`); err != nil {
		return err
	}
	_, err = tx.Exec(`UPDATE requests SET stream = 1 WHERE CASE WHEN json_valid(CAST(request_body AS TEXT)) THEN json_extract(CAST(request_body AS TEXT), '$.stream') ELSE 0 END = 1`)
	return err
}

func ensureLogPayloadColumns(tx *sql.Tx) error {
	columns := []struct {
		table      string
		name       string
		definition string
	}{
		{table: "requests", name: "request_body_encoding", definition: "TEXT NOT NULL DEFAULT 'identity'"},
		{table: "requests", name: "response_body_encoding", definition: "TEXT NOT NULL DEFAULT 'identity'"},
		{table: "requests", name: "payload_pruned_at", definition: "TEXT"},
		{table: "attempts", name: "request_body_encoding", definition: "TEXT NOT NULL DEFAULT 'identity'"},
		{table: "attempts", name: "response_body_encoding", definition: "TEXT NOT NULL DEFAULT 'identity'"},
		{table: "attempts", name: "payload_pruned_at", definition: "TEXT"},
		{table: "requests", name: "upstream_model", definition: "TEXT"},
		{table: "requests", name: "provider_name", definition: "TEXT"},
	}
	for _, column := range columns {
		if err := ensureColumn(tx, column.table, column.name, column.definition); err != nil {
			return err
		}
	}
	return nil
}

func ensureColumn(tx *sql.Tx, table, name, definition string) error {
	rows, err := tx.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cid, notNull, primaryKey int
		var columnName, columnType string
		var defaultValue any
		if err := rows.Scan(&cid, &columnName, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return err
		}
		if columnName == name {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	_, err = tx.Exec(`ALTER TABLE ` + table + ` ADD COLUMN ` + name + ` ` + definition)
	return err
}

func (s *Store) BootstrapAdmin(password string) error {
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM admin`).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	if password == "" {
		return errors.New("empty database requires --admin-password or MODEL_CONFLUENCE_ADMIN_PASSWORD")
	}
	return s.insertAdmin(password)
}

func (s *Store) insertAdmin(password string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(timestampLayout)
	_, err = s.db.Exec(`INSERT INTO admin (id, password_hash, created_at, updated_at) VALUES (1, ?, ?, ?)`, string(hash), now, now)
	return err
}

func (s *Store) CheckAdminPassword(password string) error {
	var hash string
	if err := s.db.QueryRow(`SELECT password_hash FROM admin WHERE id = 1`).Scan(&hash); err != nil {
		return err
	}
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
}

func (s *Store) ResetAdminPassword(password string) error {
	if password == "" {
		return errors.New("password is required")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.Exec(`UPDATE admin SET password_hash = ?, updated_at = ? WHERE id = 1`, string(hash), time.Now().UTC().Format(timestampLayout))
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return errors.New("admin is not initialized")
	}
	if _, err := tx.Exec(`DELETE FROM admin_sessions`); err != nil {
		return err
	}
	return tx.Commit()
}

type Session struct {
	Token     string
	CreatedAt time.Time
	LastSeen  time.Time
	ExpiresAt time.Time
}

func (s *Store) CreateSession() (Session, error) {
	token, err := randomToken(32)
	if err != nil {
		return Session{}, err
	}
	now := time.Now().UTC()
	session := Session{Token: token, CreatedAt: now, LastSeen: now, ExpiresAt: now.Add(7 * 24 * time.Hour)}
	_, err = s.db.Exec(`INSERT INTO admin_sessions (token, created_at, last_seen_at, expires_at) VALUES (?, ?, ?, ?)`, token, formatTime(now), formatTime(now), formatTime(session.ExpiresAt))
	return session, err
}

func (s *Store) ValidateSession(token string) (Session, error) {
	var createdAt, lastSeenAt, expiresAt string
	err := s.db.QueryRow(`SELECT created_at, last_seen_at, expires_at FROM admin_sessions WHERE token = ?`, token).Scan(&createdAt, &lastSeenAt, &expiresAt)
	if err != nil {
		return Session{}, err
	}
	created, err := time.Parse(timestampLayout, createdAt)
	if err != nil {
		return Session{}, err
	}
	lastSeen, err := time.Parse(timestampLayout, lastSeenAt)
	if err != nil {
		return Session{}, err
	}
	expires, err := time.Parse(timestampLayout, expiresAt)
	if err != nil {
		return Session{}, err
	}
	now := time.Now().UTC()
	if now.After(expires) || now.Sub(lastSeen) > 24*time.Hour {
		s.DeleteSession(token)
		return Session{}, sql.ErrNoRows
	}
	if now.Sub(lastSeen) >= sessionTouchInterval {
		if _, err := s.db.Exec(`UPDATE admin_sessions SET last_seen_at = ? WHERE token = ?`, formatTime(now), token); err != nil {
			return Session{}, err
		}
	}
	return Session{Token: token, CreatedAt: created, LastSeen: now, ExpiresAt: expires}, nil
}

func (s *Store) DeleteSession(token string) error {
	_, err := s.db.Exec(`DELETE FROM admin_sessions WHERE token = ?`, token)
	return err
}

func randomToken(bytes int) (string, error) {
	value := make([]byte, bytes)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func NewToken(bytes int) (string, error) {
	return randomToken(bytes)
}

func formatTime(value time.Time) string {
	return value.UTC().Format(timestampLayout)
}

package store

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

type UpstreamLimits struct {
	Model      string              `json:"model"`
	RequestID  string              `json:"request_id"`
	ObservedAt time.Time           `json:"observed_at"`
	Headers    map[string][]string `json:"headers"`
}

func (s *Store) ObserveUpstreamLimits(route ResolvedRoute, requestID string, headers http.Header) error {
	values := make(map[string][]string)
	for name, value := range headers {
		lower := strings.ToLower(name)
		if lower == "retry-after" || strings.HasPrefix(lower, "x-ratelimit-") || strings.HasPrefix(lower, "ratelimit") || strings.HasPrefix(lower, "anthropic-ratelimit-") || strings.HasPrefix(lower, "x-codex-") && (strings.Contains(lower, "used-percent") || strings.Contains(lower, "reset") || strings.Contains(lower, "window")) {
			values[name] = value
		}
	}
	if len(values) == 0 {
		return nil
	}
	raw, err := json.Marshal(values)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT INTO upstream_limits(upstream_key_id, upstream_model, request_id, observed_at, headers_json) SELECT id, ?, ?, ?, ? FROM upstream_keys WHERE id = ? AND credential_revision = ? AND archived_at IS NULL ON CONFLICT(upstream_key_id, upstream_model) DO UPDATE SET request_id = excluded.request_id, observed_at = excluded.observed_at, headers_json = excluded.headers_json`, route.UpstreamModel, requestID, formatTime(time.Now()), string(raw), route.Key.ID, route.Key.CredentialRevision)
	return err
}

func (s *Store) UpstreamLimits(id int64) ([]UpstreamLimits, error) {
	rows, err := s.db.Query(`SELECT upstream_model, request_id, observed_at, headers_json FROM upstream_limits WHERE upstream_key_id = ? ORDER BY observed_at DESC LIMIT 50`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []UpstreamLimits{}
	for rows.Next() {
		var item UpstreamLimits
		var observed, raw string
		if err := rows.Scan(&item.Model, &item.RequestID, &observed, &raw); err != nil {
			return nil, err
		}
		item.ObservedAt, err = time.Parse(timestampLayout, observed)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(raw), &item.Headers); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

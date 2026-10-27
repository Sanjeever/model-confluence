package store

import (
	"database/sql"
	"encoding/json"
	"time"

	"github.com/Sanjeever/model-confluence/internal/protocol"
)

type AntigravityToolHistory struct {
	ProviderID    int64
	UpstreamKeyID int64
	UpstreamModel string
	Turn          protocol.AntigravityToolTurn
}

func (s *Store) SaveAntigravityToolTurn(requestID string, route ResolvedRoute, turn protocol.AntigravityToolTurn) error {
	raw, err := json.Marshal(turn)
	if err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var accessKeyID sql.NullInt64
	if err := tx.QueryRow(`SELECT access_key_id FROM requests WHERE id = ?`, requestID).Scan(&accessKeyID); err != nil {
		return err
	}
	for _, call := range turn.Calls {
		if _, err := tx.Exec(`INSERT INTO antigravity_tool_history(call_id, access_key_id, provider_id, upstream_key_id, upstream_model, history_json, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`, call.ID, accessKeyID, route.Provider.ID, route.Key.ID, route.UpstreamModel, string(raw), formatTime(time.Now())); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) AntigravityToolHistory(accessKeyID *int64, callID string) (AntigravityToolHistory, error) {
	var result AntigravityToolHistory
	var raw string
	err := s.db.QueryRow(`SELECT provider_id, upstream_key_id, upstream_model, history_json FROM antigravity_tool_history WHERE call_id = ? AND access_key_id IS ?`, callID, accessKeyID).Scan(&result.ProviderID, &result.UpstreamKeyID, &result.UpstreamModel, &raw)
	if err != nil {
		return result, err
	}
	err = json.Unmarshal([]byte(raw), &result.Turn)
	return result, err
}

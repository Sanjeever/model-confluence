package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"
)

type CleanupResult struct {
	Mode       string    `json:"mode"`
	Count      int64     `json:"count"`
	From       time.Time `json:"from"`
	To         time.Time `json:"to"`
	FinishedAt time.Time `json:"finished_at"`
	Error      string    `json:"error,omitempty"`
}

type StorageStats struct {
	DatabaseBytes int64          `json:"database_bytes"`
	WALBytes      int64          `json:"wal_bytes"`
	ReusableBytes int64          `json:"reusable_bytes"`
	RequestCount  int64          `json:"request_count"`
	LastCleanup   *CleanupResult `json:"last_cleanup"`
}

func (s *Store) StorageStats() (StorageStats, error) {
	var result StorageStats
	info, err := os.Stat(s.databasePath)
	if err != nil {
		return result, err
	}
	result.DatabaseBytes = info.Size()
	if wal, err := os.Stat(s.databasePath + "-wal"); err == nil {
		result.WALBytes = wal.Size()
	} else if !errors.Is(err, os.ErrNotExist) {
		return result, err
	}
	var freePages, pageSize int64
	if err := s.db.QueryRow(`PRAGMA freelist_count`).Scan(&freePages); err != nil {
		return result, err
	}
	if err := s.db.QueryRow(`PRAGMA page_size`).Scan(&pageSize); err != nil {
		return result, err
	}
	result.ReusableBytes = freePages * pageSize
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM requests`).Scan(&result.RequestCount); err != nil {
		return result, err
	}
	var raw string
	if err := s.db.QueryRow(`SELECT result_json FROM storage_maintenance WHERE id = 1`).Scan(&raw); err != nil {
		return result, err
	}
	if raw != "" {
		var cleanup CleanupResult
		if err := json.Unmarshal([]byte(raw), &cleanup); err != nil {
			return result, err
		}
		result.LastCleanup = &cleanup
	}
	return result, nil
}

func cleanupCondition(mode string) (string, error) {
	where := `created_at >= ? AND created_at < ? AND status <> 'in_progress' AND NOT EXISTS (SELECT 1 FROM attempts a WHERE a.request_id = requests.id AND a.status = 'in_progress')`
	switch mode {
	case "prune":
		where += " AND payload_pruned_at IS NULL"
	case "delete":
	default:
		return "", errors.New("清理方式必须为 prune 或 delete")
	}
	return where, nil
}

func (s *Store) PreviewCleanup(ctx context.Context, from, to time.Time, mode string) (int64, error) {
	where, err := cleanupCondition(mode)
	if err != nil {
		return 0, err
	}
	var count int64
	err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM requests WHERE `+where, formatTime(from), formatTime(to)).Scan(&count)
	return count, err
}

func (s *Store) saveCleanup(result CleanupResult) error {
	data, err := json.Marshal(result)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`UPDATE storage_maintenance SET result_json = ? WHERE id = 1`, string(data))
	return err
}

func (s *Store) CleanupLogs(ctx context.Context, from, to time.Time, mode string) (result CleanupResult, resultErr error) {
	result = CleanupResult{Mode: mode, From: from, To: to}
	where, err := cleanupCondition(mode)
	if err != nil {
		return result, err
	}
	defer func() {
		result.FinishedAt = time.Now().UTC()
		if resultErr != nil {
			result.Error = resultErr.Error()
		}
		resultErr = errors.Join(resultErr, s.saveCleanup(result))
	}()
	for {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return result, err
		}
		ids := `SELECT id FROM requests WHERE ` + where + ` ORDER BY created_at LIMIT 200`
		args := []any{formatTime(from), formatTime(to)}
		var count int64
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM (`+ids+`)`, args...).Scan(&count); err != nil {
			tx.Rollback()
			return result, err
		}
		if count == 0 {
			tx.Rollback()
			return result, nil
		}
		if mode == "delete" {
			_, err = tx.ExecContext(ctx, `DELETE FROM requests WHERE id IN (`+ids+`)`, args...)
		} else {
			prunedAt := formatTime(time.Now())
			_, err = tx.ExecContext(ctx, `UPDATE attempts SET request_headers = NULL, request_body = NULL, request_body_encoding = 'identity', response_headers = NULL, response_body = NULL, response_body_encoding = 'identity', error_message = NULL, payload_pruned_at = ? WHERE request_id IN (`+ids+`)`, append([]any{prunedAt}, args...)...)
			if err == nil {
				_, err = tx.ExecContext(ctx, `UPDATE requests SET request_headers = '', request_body = zeroblob(0), request_body_encoding = 'identity', response_headers = NULL, response_body = NULL, response_body_encoding = 'identity', error_message = NULL, payload_pruned_at = ? WHERE id IN (`+ids+`)`, append([]any{prunedAt}, args...)...)
			}
		}
		if err != nil {
			tx.Rollback()
			return result, fmt.Errorf("清理失败，已处理 %d 条: %w", result.Count, err)
		}
		if err := tx.Commit(); err != nil {
			return result, err
		}
		result.Count += count
	}
}

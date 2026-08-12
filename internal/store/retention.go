package store

import (
	"context"
	"fmt"
	"time"
)

const logCleanupBatchSize = 200

func (s *Store) PruneLogs(ctx context.Context, before time.Time) (int, error) {
	cutoff := formatTime(before)
	prunedRequests := 0

	for {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return prunedRequests, err
		}
		prunedAt := formatTime(time.Now().UTC())

		if _, err := tx.ExecContext(ctx, `UPDATE attempts SET request_headers = NULL, request_body = NULL, request_body_encoding = 'identity', response_headers = NULL, response_body = NULL, response_body_encoding = 'identity', error_message = NULL, payload_pruned_at = ? WHERE payload_pruned_at IS NULL AND request_id IN (SELECT id FROM requests WHERE created_at < ? AND status <> 'in_progress' AND payload_pruned_at IS NULL ORDER BY created_at LIMIT ?)`, prunedAt, cutoff, logCleanupBatchSize); err != nil {
			tx.Rollback()
			return prunedRequests, fmt.Errorf("prune attempt payloads: %w", err)
		}
		result, err := tx.ExecContext(ctx, `UPDATE requests SET request_headers = '', request_body = zeroblob(0), request_body_encoding = 'identity', response_headers = NULL, response_body = NULL, response_body_encoding = 'identity', error_message = NULL, payload_pruned_at = ? WHERE id IN (SELECT id FROM requests WHERE created_at < ? AND status <> 'in_progress' AND payload_pruned_at IS NULL ORDER BY created_at LIMIT ?)`, prunedAt, cutoff, logCleanupBatchSize)
		if err != nil {
			tx.Rollback()
			return prunedRequests, fmt.Errorf("prune request payloads: %w", err)
		}
		requestRows, err := result.RowsAffected()
		if err != nil {
			tx.Rollback()
			return prunedRequests, fmt.Errorf("count pruned requests: %w", err)
		}
		securityResult, err := tx.ExecContext(ctx, `DELETE FROM security_events WHERE id IN (SELECT id FROM security_events WHERE created_at < ? ORDER BY created_at LIMIT ?)`, cutoff, logCleanupBatchSize)
		if err != nil {
			tx.Rollback()
			return prunedRequests, fmt.Errorf("prune security events: %w", err)
		}
		securityRows, err := securityResult.RowsAffected()
		if err != nil {
			tx.Rollback()
			return prunedRequests, fmt.Errorf("count pruned security events: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return prunedRequests, fmt.Errorf("commit log pruning: %w", err)
		}

		prunedRequests += int(requestRows)
		if requestRows == 0 && securityRows == 0 {
			return prunedRequests, nil
		}
	}
}

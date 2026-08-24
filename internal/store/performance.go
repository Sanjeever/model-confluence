package store

import (
	"database/sql"
	"time"
)

type PerformanceOverview struct {
	RequestCount        int                     `json:"request_count"`
	StatusCounts        PerformanceStatusCounts `json:"status_counts"`
	SuccessRate         *float64                `json:"success_rate"`
	FirstContentLatency PerformanceLatencyStats `json:"first_content_latency"`
	TotalLatency        PerformanceLatencyStats `json:"total_latency"`
	AttentionRequests   []PerformanceRequest    `json:"attention_requests"`
}

type PerformanceStatusCounts struct {
	Completed  int `json:"completed"`
	Failed     int `json:"failed"`
	Cancelled  int `json:"cancelled"`
	InProgress int `json:"in_progress"`
}

type PerformanceLatencyStats struct {
	P50         *int64 `json:"p50"`
	P95         *int64 `json:"p95"`
	SampleCount int    `json:"sample_count"`
}

type PerformanceRequest struct {
	ID               string    `json:"id"`
	Status           string    `json:"status"`
	VirtualModel     string    `json:"virtual_model"`
	InboundProtocol  string    `json:"inbound_protocol"`
	UpstreamProtocol string    `json:"upstream_protocol"`
	ProviderName     string    `json:"provider_name"`
	UpstreamModel    string    `json:"upstream_model"`
	ResponseStatus   *int      `json:"response_status"`
	FirstContentMS   *int64    `json:"first_content_ms"`
	TotalMS          *int64    `json:"total_ms"`
	ErrorMessage     string    `json:"error_message"`
	CreatedAt        time.Time `json:"created_at"`
}

func (s *Store) PerformanceOverview(createdFrom, createdTo time.Time) (PerformanceOverview, error) {
	result := PerformanceOverview{AttentionRequests: []PerformanceRequest{}}
	var terminalCount int
	err := s.db.QueryRow(`
SELECT
  COUNT(*),
  COALESCE(SUM(status = 'completed'), 0),
  COALESCE(SUM(status = 'failed'), 0),
  COALESCE(SUM(status = 'cancelled'), 0),
  COALESCE(SUM(status = 'in_progress'), 0)
FROM requests
WHERE created_at >= ? AND created_at < ?
`, formatTime(createdFrom), formatTime(createdTo)).Scan(
		&result.RequestCount,
		&result.StatusCounts.Completed,
		&result.StatusCounts.Failed,
		&result.StatusCounts.Cancelled,
		&result.StatusCounts.InProgress,
	)
	if err != nil {
		return PerformanceOverview{}, err
	}
	terminalCount = result.StatusCounts.Completed + result.StatusCounts.Failed + result.StatusCounts.Cancelled
	if terminalCount > 0 {
		successRate := float64(result.StatusCounts.Completed) / float64(terminalCount)
		result.SuccessRate = &successRate
	}
	result.FirstContentLatency, result.TotalLatency, err = s.performanceLatencyStats(createdFrom, createdTo)
	if err != nil {
		return PerformanceOverview{}, err
	}
	result.AttentionRequests, err = s.performanceAttentionRequests(createdFrom, createdTo)
	if err != nil {
		return PerformanceOverview{}, err
	}
	return result, nil
}

func (s *Store) performanceLatencyStats(createdFrom, createdTo time.Time) (PerformanceLatencyStats, PerformanceLatencyStats, error) {
	rows, err := s.db.Query(`
WITH latency(metric, value) AS (
  SELECT 'first_content', first_content_ms
  FROM requests
  WHERE created_at >= ? AND created_at < ? AND status = 'completed' AND first_content_ms IS NOT NULL
  UNION ALL
  SELECT 'total', total_ms
  FROM requests
  WHERE created_at >= ? AND created_at < ? AND status = 'completed' AND total_ms IS NOT NULL
), ranked AS (
  SELECT
    metric,
    value,
    ROW_NUMBER() OVER (PARTITION BY metric ORDER BY value) AS row_number,
    COUNT(*) OVER (PARTITION BY metric) AS sample_count
  FROM latency
)
SELECT
  metric,
  MAX(CASE WHEN row_number = (sample_count + 1) / 2 THEN value END),
  MAX(CASE WHEN row_number = (95 * sample_count + 99) / 100 THEN value END),
  MAX(sample_count)
FROM ranked
GROUP BY metric
`, formatTime(createdFrom), formatTime(createdTo), formatTime(createdFrom), formatTime(createdTo))
	if err != nil {
		return PerformanceLatencyStats{}, PerformanceLatencyStats{}, err
	}
	defer rows.Close()
	var firstContent, total PerformanceLatencyStats
	for rows.Next() {
		var metric string
		var p50, p95 sql.NullInt64
		var sampleCount int
		if err := rows.Scan(&metric, &p50, &p95, &sampleCount); err != nil {
			return PerformanceLatencyStats{}, PerformanceLatencyStats{}, err
		}
		stats := PerformanceLatencyStats{P50: nullableInt64Pointer(p50), P95: nullableInt64Pointer(p95), SampleCount: sampleCount}
		if metric == "first_content" {
			firstContent = stats
		} else {
			total = stats
		}
	}
	if err := rows.Err(); err != nil {
		return PerformanceLatencyStats{}, PerformanceLatencyStats{}, err
	}
	return firstContent, total, nil
}

func (s *Store) performanceAttentionRequests(createdFrom, createdTo time.Time) ([]PerformanceRequest, error) {
	rows, err := s.db.Query(`
SELECT
  r.id,
  r.status,
  COALESCE(r.virtual_model, ''),
  r.inbound_protocol,
  COALESCE(a.upstream_protocol, ''),
  COALESCE(a.provider_name, ''),
  COALESCE(a.upstream_model, ''),
  r.response_status,
  r.first_content_ms,
  r.total_ms,
  COALESCE(r.error_message, ''),
  r.created_at
FROM requests r
LEFT JOIN attempts a ON a.request_id = r.id
  AND a.position = (SELECT MAX(position) FROM attempts WHERE request_id = r.id)
WHERE r.created_at >= ? AND r.created_at < ? AND r.status != 'in_progress'
ORDER BY
  CASE WHEN r.status = 'failed' THEN 0 WHEN r.status = 'cancelled' THEN 1 ELSE 2 END,
  CASE WHEN r.status = 'failed' THEN COALESCE(r.total_ms, 0) ELSE 0 END DESC,
  COALESCE(r.total_ms, 0) DESC,
  r.created_at DESC
LIMIT 8
`, formatTime(createdFrom), formatTime(createdTo))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]PerformanceRequest, 0, 8)
	for rows.Next() {
		var item PerformanceRequest
		var responseStatus, firstContent, total sql.NullInt64
		var createdAt string
		if err := rows.Scan(&item.ID, &item.Status, &item.VirtualModel, &item.InboundProtocol, &item.UpstreamProtocol, &item.ProviderName, &item.UpstreamModel, &responseStatus, &firstContent, &total, &item.ErrorMessage, &createdAt); err != nil {
			return nil, err
		}
		var err error
		item.ResponseStatus = nullableIntPointer(responseStatus)
		item.FirstContentMS = nullableInt64Pointer(firstContent)
		item.TotalMS = nullableInt64Pointer(total)
		item.CreatedAt, err = time.Parse(timestampLayout, createdAt)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

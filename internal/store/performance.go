package store

import (
	"database/sql"
	"math"
	"sort"
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
	var firstContentValues []int64
	var totalValues []int64
	var terminalCount int

	rows, err := s.db.Query(`
SELECT status, first_content_ms, total_ms
FROM requests
WHERE created_at >= ? AND created_at < ?
`, formatTime(createdFrom), formatTime(createdTo))
	if err != nil {
		return PerformanceOverview{}, err
	}
	defer rows.Close()

	for rows.Next() {
		var status string
		var firstContent, total sql.NullInt64
		if err := rows.Scan(&status, &firstContent, &total); err != nil {
			return PerformanceOverview{}, err
		}
		result.RequestCount++
		switch status {
		case "completed":
			result.StatusCounts.Completed++
			terminalCount++
			if firstContent.Valid {
				firstContentValues = append(firstContentValues, firstContent.Int64)
			}
			if total.Valid {
				totalValues = append(totalValues, total.Int64)
			}
		case "failed":
			result.StatusCounts.Failed++
			terminalCount++
		case "cancelled":
			result.StatusCounts.Cancelled++
			terminalCount++
		case "in_progress":
			result.StatusCounts.InProgress++
		}
	}
	if err := rows.Err(); err != nil {
		return PerformanceOverview{}, err
	}

	if terminalCount > 0 {
		successRate := float64(result.StatusCounts.Completed) / float64(terminalCount)
		result.SuccessRate = &successRate
	}
	result.FirstContentLatency = performanceLatencyStats(firstContentValues)
	result.TotalLatency = performanceLatencyStats(totalValues)
	result.AttentionRequests, err = s.performanceAttentionRequests(createdFrom, createdTo)
	if err != nil {
		return PerformanceOverview{}, err
	}
	return result, nil
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

func performanceLatencyStats(values []int64) PerformanceLatencyStats {
	if len(values) == 0 {
		return PerformanceLatencyStats{}
	}
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	return PerformanceLatencyStats{
		P50:         percentileValue(values, 0.50),
		P95:         percentileValue(values, 0.95),
		SampleCount: len(values),
	}
}

func percentileValue(values []int64, percentile float64) *int64 {
	index := int(math.Ceil(percentile*float64(len(values)))) - 1
	value := values[index]
	return &value
}

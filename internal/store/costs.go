package store

import (
	"database/sql"
	"time"
)

type TokenUsageBreakdown struct {
	InputTokens       int64 `json:"input_tokens"`
	CacheReadTokens   int64 `json:"cache_read_tokens"`
	CacheWriteTokens  int64 `json:"cache_write_tokens"`
	OutputTokens      int64 `json:"output_tokens"`
	ReasoningTokens   int64 `json:"reasoning_tokens"`
	TotalTokens       int64 `json:"total_tokens"`
	InputCachedTokens int64 `json:"input_cached_tokens"`
}

type UsageGroup struct {
	Date          string              `json:"date"`
	VirtualModel  string              `json:"virtual_model"`
	UpstreamModel string              `json:"upstream_model"`
	ProviderName  string              `json:"provider_name"`
	RequestCount  int                 `json:"request_count"`
	Usage         TokenUsageBreakdown `json:"usage"`
}

type UsageSummary struct {
	RequestCount int64               `json:"request_count"`
	GroupCount   int                 `json:"group_count"`
	Usage        TokenUsageBreakdown `json:"usage"`
}

type UsagePage struct {
	Groups  []UsageGroup `json:"groups"`
	Summary UsageSummary `json:"summary"`
}

// UsageBreakdown aggregates per-day token usage across completed requests,
// grouped by virtual model and the final upstream model actually reached.
// Cached input is attributed to the cache-read bucket when the upstream
// reported it, otherwise input tokens remain in the non-cached bucket.
func (s *Store) UsageBreakdown(createdFrom, createdTo time.Time, virtualModel string) (UsagePage, error) {
	where := "r.created_at >= ? AND r.created_at < ?"
	args := []any{formatTime(createdFrom), formatTime(createdTo)}
	if virtualModel != "" {
		where += " AND r.virtual_model = ?"
		args = append(args, virtualModel)
	}
	sumPart := `COALESCE(SUM(r.input_tokens), 0),
	  COALESCE(SUM(r.cache_read_tokens), 0),
	  COALESCE(SUM(r.cache_write_tokens), 0),
	  COALESCE(SUM(r.output_tokens), 0),
	  COALESCE(SUM(r.reasoning_tokens), 0),
	  COALESCE(SUM(r.total_tokens), 0)`

	groupQuery := `SELECT date(r.created_at) AS day, COALESCE(r.virtual_model, ''), COALESCE(r.upstream_model, ''), COALESCE(r.provider_name, ''), COUNT(*), ` + sumPart + `
FROM requests r WHERE ` + where + ` AND r.status = 'completed'
GROUP BY day, r.virtual_model, r.upstream_model, r.provider_name
ORDER BY day DESC, r.virtual_model`
	rows, err := s.db.Query(groupQuery, args...)
	if err != nil {
		return UsagePage{}, err
	}
	defer rows.Close()
	groups := make([]UsageGroup, 0)
	for rows.Next() {
		var group UsageGroup
		var day string
		var input, cacheRead, cacheWrite, output, reasoning, total sql.NullInt64
		if err := rows.Scan(&day, &group.VirtualModel, &group.UpstreamModel, &group.ProviderName, &group.RequestCount, &input, &cacheRead, &cacheWrite, &output, &reasoning, &total); err != nil {
			return UsagePage{}, err
		}
		group.Date = day
		group.Usage = TokenUsageBreakdown{
			InputTokens:       input.Int64,
			CacheReadTokens:   cacheRead.Int64,
			CacheWriteTokens:  cacheWrite.Int64,
			OutputTokens:      output.Int64,
			ReasoningTokens:   reasoning.Int64,
			TotalTokens:       total.Int64,
			InputCachedTokens: cacheRead.Int64 + cacheWrite.Int64,
		}
		groups = append(groups, group)
	}
	if err := rows.Err(); err != nil {
		return UsagePage{}, err
	}

	var summary UsageSummary
	var requestCount int64
	summaryQuery := `SELECT COUNT(*), ` + sumPart + `
FROM requests r WHERE ` + where + ` AND r.status = 'completed'`
	if err := s.db.QueryRow(summaryQuery, args...).Scan(&requestCount, &summary.Usage.InputTokens, &summary.Usage.CacheReadTokens, &summary.Usage.CacheWriteTokens, &summary.Usage.OutputTokens, &summary.Usage.ReasoningTokens, &summary.Usage.TotalTokens); err != nil {
		return UsagePage{}, err
	}
	summary.RequestCount = requestCount
	summary.GroupCount = len(groups)
	summary.Usage.InputCachedTokens = summary.Usage.CacheReadTokens + summary.Usage.CacheWriteTokens
	return UsagePage{Groups: groups, Summary: summary}, nil
}

// VirtualModelNames returns the names of non-archived virtual models ordered
// by name, used to populate the usage filter dropdown.
func (s *Store) VirtualModelNames() ([]string, error) {
	rows, err := s.db.Query(`SELECT name FROM virtual_models WHERE archived_at IS NULL ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	names := make([]string, 0)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return names, rows.Err()
}

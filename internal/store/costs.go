package store

import (
	"context"
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

// UsageBreakdown 仅汇总已完成请求的已知用量，按 UTC 日期和最终路由分组；缺失指标不参与求和。
func (s *Store) UsageBreakdown(ctx context.Context, createdFrom, createdTo time.Time, virtualModel string) (UsagePage, error) {
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
FROM requests r INDEXED BY idx_requests_usage WHERE ` + where + ` AND r.status = 'completed'
GROUP BY day, r.virtual_model, r.upstream_model, r.provider_name
ORDER BY day DESC, r.virtual_model`
	rows, err := s.db.QueryContext(ctx, groupQuery, args...)
	if err != nil {
		return UsagePage{}, err
	}
	defer rows.Close()
	groups := make([]UsageGroup, 0)
	var summary UsageSummary
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
		summary.RequestCount += int64(group.RequestCount)
		summary.Usage.InputTokens += group.Usage.InputTokens
		summary.Usage.CacheReadTokens += group.Usage.CacheReadTokens
		summary.Usage.CacheWriteTokens += group.Usage.CacheWriteTokens
		summary.Usage.OutputTokens += group.Usage.OutputTokens
		summary.Usage.ReasoningTokens += group.Usage.ReasoningTokens
		summary.Usage.TotalTokens += group.Usage.TotalTokens
	}
	if err := rows.Err(); err != nil {
		return UsagePage{}, err
	}

	summary.GroupCount = len(groups)
	summary.Usage.InputCachedTokens = summary.Usage.CacheReadTokens + summary.Usage.CacheWriteTokens
	return UsagePage{Groups: groups, Summary: summary}, nil
}

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

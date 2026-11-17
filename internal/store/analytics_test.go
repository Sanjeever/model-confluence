package store

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Sanjeever/model-confluence/internal/protocol"
)

func TestAnalytics(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "analytics.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	from := time.Date(2026, 11, 17, 0, 0, 0, 0, time.UTC)
	to := from.Add(24 * time.Hour)
	for i := 1; i <= 20; i++ {
		model := "model-a"
		if i%2 == 0 {
			model = "model-b"
		}
		if _, err := s.db.Exec(`INSERT INTO requests(id,status,virtual_model,upstream_model,provider_name,inbound_protocol,inbound_endpoint,client_ip,request_headers,request_body,created_at,first_content_ms,total_ms,input_tokens,cache_read_tokens,cache_write_tokens,output_tokens,reasoning_tokens,total_tokens)
VALUES (?, 'completed', ?, 'real', 'provider', ?, '/v1/responses', '', '{}', zeroblob(16384), ?, ?, ?, ?, 10, 5, 20, 2, 120)`, fmt.Sprintf("completed-%02d", i), model, protocol.Responses, formatTime(from.Add(time.Duration(i)*time.Minute)), i, i*10, 100); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.db.Exec(`UPDATE requests SET first_content_ms = NULL WHERE id = 'completed-20'; UPDATE requests SET input_tokens = NULL WHERE id = 'completed-02'`); err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		id, status string
		created    time.Time
		total      int
	}{
		{"failed-a", "failed", from, 900},
		{"failed-b", "failed", from, 800},
		{"cancelled", "cancelled", from, 50000},
		{"active", "in_progress", from, 99999},
		{"before", "completed", from.Add(-time.Nanosecond), 99999},
		{"end", "completed", to, 99999},
	} {
		if _, err := s.db.Exec(`INSERT INTO requests(id,status,inbound_protocol,inbound_endpoint,client_ip,request_headers,request_body,created_at,total_ms,input_tokens) VALUES (?, ?, ?, '/v1/responses', '', '{}', X'', ?, ?, 99999)`, item.id, item.status, protocol.Responses, formatTime(item.created), item.total); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.db.Exec(`INSERT INTO attempts(request_id,position,status,created_at,provider_name,upstream_model,upstream_protocol) VALUES ('failed-a',0,'failed',?,'old','old',?), ('failed-a',1,'failed',?,'final','final',?)`, formatTime(from), protocol.Chat, formatTime(from), protocol.Messages); err != nil {
		t.Fatal(err)
	}

	performance, err := s.PerformanceOverview(ctx, from, to)
	if err != nil {
		t.Fatal(err)
	}
	if performance.RequestCount != 24 || performance.StatusCounts != (PerformanceStatusCounts{Completed: 20, Failed: 2, Cancelled: 1, InProgress: 1}) || performance.SuccessRate == nil || *performance.SuccessRate != 20.0/23 {
		t.Fatalf("counts = %+v", performance)
	}
	if performance.FirstContentLatency.SampleCount != 19 || *performance.FirstContentLatency.P50 != 10 || *performance.FirstContentLatency.P95 != 19 || performance.TotalLatency.SampleCount != 20 || *performance.TotalLatency.P50 != 100 || *performance.TotalLatency.P95 != 190 {
		t.Fatalf("latency = %+v, %+v", performance.FirstContentLatency, performance.TotalLatency)
	}
	var ids []string
	for _, item := range performance.AttentionRequests {
		ids = append(ids, item.ID)
	}
	if want := []string{"failed-a", "failed-b", "cancelled", "completed-20", "completed-19", "completed-18", "completed-17", "completed-16"}; !reflect.DeepEqual(ids, want) {
		t.Fatalf("attention = %v, want %v", ids, want)
	}
	if item := performance.AttentionRequests[0]; item.ProviderName != "final" || item.UpstreamModel != "final" || item.UpstreamProtocol != protocol.Messages {
		t.Fatalf("final attempt = %+v", item)
	}

	for _, item := range []struct {
		model  string
		count  int64
		groups int
		input  int64
	}{
		{"", 20, 2, 1900}, {"model-a", 10, 1, 1000}, {"model-b", 10, 1, 900}, {"missing", 0, 0, 0},
	} {
		usage, err := s.UsageBreakdown(ctx, from, to, item.model)
		if err != nil {
			t.Fatal(err)
		}
		want := UsageSummary{RequestCount: item.count, GroupCount: item.groups, Usage: TokenUsageBreakdown{InputTokens: item.input, CacheReadTokens: item.count * 10, CacheWriteTokens: item.count * 5, InputCachedTokens: item.count * 15, OutputTokens: item.count * 20, ReasoningTokens: item.count * 2, TotalTokens: item.count * 120}}
		if usage.Summary != want || len(usage.Groups) != item.groups || usage.Groups == nil {
			t.Fatalf("usage %q = %+v, want %+v", item.model, usage, want)
		}
		for _, group := range usage.Groups {
			if group.Date != "2026-11-17" || group.ProviderName != "provider" || group.UpstreamModel != "real" || group.RequestCount != 10 {
				t.Fatalf("group = %+v", group)
			}
		}
	}
	empty, err := s.PerformanceOverview(ctx, to.Add(time.Hour), to.Add(2*time.Hour))
	if err != nil || empty.RequestCount != 0 || empty.SuccessRate != nil || empty.FirstContentLatency.P50 != nil || empty.TotalLatency.P95 != nil || empty.AttentionRequests == nil || len(empty.AttentionRequests) != 0 {
		t.Fatalf("empty performance = %+v, %v", empty, err)
	}

	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := s.PerformanceOverview(cancelled, from, to); !errors.Is(err, context.Canceled) {
		t.Fatalf("performance cancellation = %v", err)
	}
	if _, _, err := s.performanceLatencyStats(cancelled, from, to); !errors.Is(err, context.Canceled) {
		t.Fatalf("latency cancellation = %v", err)
	}
	if _, err := s.performanceAttentionRequests(cancelled, from, to); !errors.Is(err, context.Canceled) {
		t.Fatalf("attention cancellation = %v", err)
	}
	if _, err := s.UsageBreakdown(cancelled, from, to, ""); !errors.Is(err, context.Canceled) {
		t.Fatalf("usage cancellation = %v", err)
	}
}

func TestAnalyticsIndexMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO requests(id,status,inbound_protocol,inbound_endpoint,client_ip,request_headers,request_body,created_at,input_tokens) VALUES ('legacy','completed',?,'/v1/responses','','{}',X'','2026-11-16T01:00:00Z',42)`, protocol.Responses); err != nil {
		s.Close()
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`DROP INDEX idx_requests_performance; DROP INDEX idx_requests_usage; ALTER TABLE requests DROP COLUMN upstream_model; ALTER TABLE requests DROP COLUMN provider_name;`); err != nil {
		s.Close()
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	from := time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC)
	usage, err := s.UsageBreakdown(context.Background(), from, from.Add(24*time.Hour), "")
	if err != nil || usage.Summary.RequestCount != 1 || usage.Summary.Usage.InputTokens != 42 {
		t.Fatalf("migrated usage = %+v, %v", usage, err)
	}
	for _, item := range []struct{ query, index string }{
		{`SELECT count(*), sum(status='completed') FROM requests INDEXED BY idx_requests_performance WHERE created_at >= ? AND created_at < ?`, "idx_requests_performance"},
		{`SELECT first_content_ms,total_ms FROM requests INDEXED BY idx_requests_performance WHERE created_at >= ? AND created_at < ? AND status='completed'`, "idx_requests_performance"},
		{`SELECT id,status,total_ms,created_at FROM requests INDEXED BY idx_requests_performance WHERE created_at >= ? AND created_at < ? AND status!='in_progress'`, "idx_requests_performance"},
		{`SELECT date(created_at),virtual_model,upstream_model,provider_name,sum(input_tokens),sum(cache_read_tokens),sum(cache_write_tokens),sum(output_tokens),sum(reasoning_tokens),sum(total_tokens) FROM requests INDEXED BY idx_requests_usage WHERE created_at >= ? AND created_at < ? AND status='completed' GROUP BY date(created_at),virtual_model,upstream_model,provider_name`, "idx_requests_usage"},
	} {
		rows, err := s.db.Query("EXPLAIN QUERY PLAN "+item.query, "2026-11-16T00:00:00Z", "2026-11-17T00:00:00Z")
		if err != nil {
			t.Fatal(err)
		}
		var plan string
		for rows.Next() {
			var id, parent, unused int
			var detail string
			if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
				t.Fatal(err)
			}
			plan += detail + "\n"
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(plan, "USING COVERING INDEX "+item.index) || !strings.Contains(plan, "created_at>? AND created_at<?") {
			t.Fatalf("query is not covered by a time range index: %s", plan)
		}
	}
}

func BenchmarkAnalyticsRangeScan(b *testing.B) {
	s, err := Open(filepath.Join(b.TempDir(), "analytics.db"))
	if err != nil {
		b.Fatal(err)
	}
	defer s.Close()
	if _, err := s.db.Exec(`WITH RECURSIVE items(n) AS (SELECT 1 UNION ALL SELECT n+1 FROM items WHERE n<4096)
INSERT INTO requests(id,status,inbound_protocol,inbound_endpoint,client_ip,request_headers,request_body,created_at,first_content_ms)
SELECT printf('request-%d',n),'completed',?,'/v1/responses','','{}',zeroblob(16384),CASE WHEN n<=32 THEN '2026-11-17T01:00:00Z' ELSE '2026-10-01T01:00:00Z' END,100 FROM items`, protocol.Responses); err != nil {
		b.Fatal(err)
	}
	for _, item := range []struct{ name, index string }{{"status_index", "idx_requests_status"}, {"covering_index", "idx_requests_performance"}} {
		b.Run(item.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				var count, total int
				if err := s.db.QueryRow(`SELECT count(*),sum(first_content_ms) FROM requests INDEXED BY `+item.index+` WHERE created_at >= '2026-11-17T00:00:00Z' AND created_at < '2026-11-18T00:00:00Z' AND status='completed'`).Scan(&count, &total); err != nil {
					b.Fatal(err)
				}
				if count != 32 || total != 3200 {
					b.Fatalf("count=%d total=%d", count, total)
				}
			}
		})
	}
}

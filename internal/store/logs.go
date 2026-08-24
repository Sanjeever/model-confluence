package store

import (
	"database/sql"
	"time"

	"github.com/Sanjeever/model-confluence/internal/protocol"
)

type RequestStart struct {
	ID              string
	AccessKeyID     *int64
	AccessKeyName   string
	VirtualModel    string
	InboundProtocol string
	InboundEndpoint string
	Stream          bool
	ReasoningEffort string
	ClientIP        string
	UserAgent       string
	Headers         string
	Body            []byte
	CreatedAt       time.Time
}

func (s *Store) StartRequest(input RequestStart) error {
	body, encoding, err := encodePayload(input.Body)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT INTO requests (id, status, access_key_id, access_key_name, virtual_model, inbound_protocol, inbound_endpoint, stream, reasoning_effort, client_ip, user_agent, request_headers, request_body, request_body_encoding, created_at) VALUES (?, 'in_progress', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, input.ID, input.AccessKeyID, input.AccessKeyName, input.VirtualModel, input.InboundProtocol, input.InboundEndpoint, input.Stream, nullableString(input.ReasoningEffort), input.ClientIP, nullableString(input.UserAgent), input.Headers, body, encoding, formatTime(input.CreatedAt))
	return err
}

type AttemptStart struct {
	RequestID       string
	Position        int
	ProviderID      int64
	ProviderName    string
	UpstreamKeyID   int64
	UpstreamKeyName string
	CandidateID     int64
	UpstreamModel   string
	Protocol        string
	Endpoint        string
	Headers         string
	Body            []byte
	CreatedAt       time.Time
}

func (s *Store) StartAttempt(input AttemptStart) (int64, error) {
	body, encoding, err := encodePayload(input.Body)
	if err != nil {
		return 0, err
	}
	result, err := s.db.Exec(`INSERT INTO attempts (request_id, position, provider_id, provider_name, upstream_key_id, upstream_key_name, candidate_id, upstream_model, upstream_protocol, upstream_endpoint, status, request_headers, request_body, request_body_encoding, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'in_progress', ?, ?, ?, ?)`, input.RequestID, input.Position, input.ProviderID, input.ProviderName, input.UpstreamKeyID, nullableString(input.UpstreamKeyName), input.CandidateID, input.UpstreamModel, input.Protocol, input.Endpoint, input.Headers, body, encoding, formatTime(input.CreatedAt))
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

type AttemptResult struct {
	ID              int64
	Status          string
	ResponseStatus  int
	ResponseHeaders string
	ResponseBody    []byte
	RawUsageJSON    string
	FirstByteMS     *int64
	FirstContentMS  *int64
	TotalMS         int64
	ErrorMessage    string
	CompletedAt     time.Time
}

func (s *Store) CompleteAttempt(result AttemptResult) error {
	body, encoding, err := encodePayload(result.ResponseBody)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`UPDATE attempts SET status = ?, response_status = ?, response_headers = ?, response_body = ?, response_body_encoding = ?, raw_usage_json = ?, first_byte_ms = ?, first_content_ms = ?, total_ms = ?, error_message = ?, completed_at = ? WHERE id = ?`, result.Status, nullableInt(result.ResponseStatus), nullableString(result.ResponseHeaders), body, encoding, nullableString(result.RawUsageJSON), result.FirstByteMS, result.FirstContentMS, result.TotalMS, nullableString(result.ErrorMessage), formatTime(result.CompletedAt), result.ID)
	return err
}

type RequestResult struct {
	ID               string
	Status           string
	ResponseStatus   int
	ResponseHeaders  string
	ResponseBody     []byte
	FirstContentMS   *int64
	TotalMS          int64
	InputTokens      *int64
	CacheReadTokens  *int64
	CacheWriteTokens *int64
	OutputTokens     *int64
	ReasoningTokens  *int64
	TotalTokens      *int64
	ErrorMessage     string
	CompletedAt      time.Time
}

func (s *Store) CompleteRequest(result RequestResult) error {
	body, encoding, err := encodePayload(result.ResponseBody)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`
WITH final_attempt AS (
  SELECT upstream_model, provider_name
  FROM attempts
  WHERE request_id = ? AND status = 'completed'
  ORDER BY position DESC
  LIMIT 1
)
UPDATE requests SET
  status = ?, response_status = ?, response_headers = ?, response_body = ?, response_body_encoding = ?,
  input_tokens = ?, cache_read_tokens = ?, cache_write_tokens = ?, output_tokens = ?, reasoning_tokens = ?, total_tokens = ?,
  first_content_ms = ?, total_ms = ?, error_message = ?, completed_at = ?,
  upstream_model = (SELECT upstream_model FROM final_attempt),
  provider_name = (SELECT provider_name FROM final_attempt)
WHERE id = ?
`, result.ID, result.Status, nullableInt(result.ResponseStatus), nullableString(result.ResponseHeaders), body, encoding, result.InputTokens, result.CacheReadTokens, result.CacheWriteTokens, result.OutputTokens, result.ReasoningTokens, result.TotalTokens, result.FirstContentMS, result.TotalMS, nullableString(result.ErrorMessage), formatTime(result.CompletedAt), result.ID)
	return err
}

func (s *Store) CreateSecurityEvent(eventType, clientIP, userAgent, endpoint, reason string) error {
	_, err := s.db.Exec(`INSERT INTO security_events (event_type, client_ip, user_agent, endpoint, reason, created_at) VALUES (?, ?, ?, ?, ?, ?)`, eventType, clientIP, nullableString(userAgent), endpoint, reason, formatTime(time.Now()))
	return err
}

// CloseInterruptedRequests marks requests and attempts left in progress by a
// previous process as failed. Only one process may own the database, so any
// in_progress row at startup was abandoned by a crash or kill; leaving it
// stranded would keep it out of retention pruning and pollute statistics.
func (s *Store) CloseInterruptedRequests(now time.Time) (int, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	completedAt := formatTime(now)
	if _, err := tx.Exec(`UPDATE attempts SET status = 'failed', error_message = 'gateway exited before completion', completed_at = ? WHERE status = 'in_progress'`, completedAt); err != nil {
		return 0, err
	}
	result, err := tx.Exec(`UPDATE requests SET status = 'failed', error_message = 'gateway exited before completion', completed_at = ? WHERE status = 'in_progress'`, completedAt)
	if err != nil {
		return 0, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return int(rows), nil
}

type RequestSummary struct {
	ID               string     `json:"id"`
	Status           string     `json:"status"`
	AccessKeyName    string     `json:"access_key_name"`
	VirtualModel     string     `json:"virtual_model"`
	InboundProtocol  string     `json:"inbound_protocol"`
	UpstreamProtocol string     `json:"upstream_protocol"`
	Stream           bool       `json:"stream"`
	ClientIP         string     `json:"client_ip"`
	UserAgent        string     `json:"user_agent"`
	ResponseStatus   *int       `json:"response_status"`
	FirstContentMS   *int64     `json:"first_content_ms"`
	TotalMS          *int64     `json:"total_ms"`
	PayloadPruned    bool       `json:"payload_pruned"`
	CreatedAt        time.Time  `json:"created_at"`
	CompletedAt      *time.Time `json:"completed_at"`
}

type RequestPage struct {
	Items    []RequestSummary `json:"items"`
	Total    int              `json:"total"`
	Page     int              `json:"page"`
	PageSize int              `json:"page_size"`
}

type RequestDetail struct {
	RequestSummary
	InboundEndpoint  string                  `json:"inbound_endpoint"`
	ReasoningEffort  string                  `json:"reasoning_effort"`
	RequestHeaders   string                  `json:"request_headers"`
	RequestBody      string                  `json:"request_body"`
	ResponseHeaders  string                  `json:"response_headers"`
	ResponseBody     string                  `json:"response_body"`
	ResponseSummary  *protocol.StreamSummary `json:"response_summary,omitempty"`
	InputTokens      *int64                  `json:"input_tokens"`
	CacheReadTokens  *int64                  `json:"cache_read_tokens"`
	CacheWriteTokens *int64                  `json:"cache_write_tokens"`
	OutputTokens     *int64                  `json:"output_tokens"`
	ReasoningTokens  *int64                  `json:"reasoning_tokens"`
	TotalTokens      *int64                  `json:"total_tokens"`
	ErrorMessage     string                  `json:"error_message"`
	Attempts         []AttemptDetail         `json:"attempts"`
}

type AttemptDetail struct {
	ID               int64                   `json:"id"`
	Position         int                     `json:"position"`
	ProviderName     string                  `json:"provider_name"`
	UpstreamKeyName  string                  `json:"upstream_key_name"`
	UpstreamModel    string                  `json:"upstream_model"`
	UpstreamProtocol string                  `json:"upstream_protocol"`
	UpstreamEndpoint string                  `json:"upstream_endpoint"`
	Status           string                  `json:"status"`
	PayloadPruned    bool                    `json:"payload_pruned"`
	RequestHeaders   string                  `json:"request_headers"`
	RequestBody      string                  `json:"request_body"`
	ResponseStatus   *int                    `json:"response_status"`
	ResponseHeaders  string                  `json:"response_headers"`
	ResponseBody     string                  `json:"response_body"`
	ResponseSummary  *protocol.StreamSummary `json:"response_summary,omitempty"`
	RawUsageJSON     string                  `json:"raw_usage_json"`
	FirstByteMS      *int64                  `json:"first_byte_ms"`
	FirstContentMS   *int64                  `json:"first_content_ms"`
	TotalMS          *int64                  `json:"total_ms"`
	ErrorMessage     string                  `json:"error_message"`
	CreatedAt        time.Time               `json:"created_at"`
	CompletedAt      *time.Time              `json:"completed_at"`
}

func (s *Store) ListRequests(page, pageSize int, requestID string, createdFrom, createdTo time.Time) (RequestPage, error) {
	where := "r.created_at >= ? AND r.created_at < ?"
	args := []any{formatTime(createdFrom), formatTime(createdTo)}
	if requestID != "" {
		where += " AND instr(r.id, ?) > 0"
		args = append(args, requestID)
	}
	var total int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM requests r WHERE "+where, args...).Scan(&total); err != nil {
		return RequestPage{}, err
	}
	listArgs := append([]any(nil), args...)
	listArgs = append(listArgs, pageSize, (page-1)*pageSize)
	rows, err := s.db.Query(`SELECT r.id, r.status, COALESCE(r.access_key_name, ''), COALESCE(r.virtual_model, ''), r.inbound_protocol, COALESCE((SELECT a.upstream_protocol FROM attempts a WHERE a.request_id = r.id ORDER BY a.position DESC LIMIT 1), ''), r.stream, r.client_ip, COALESCE(r.user_agent, ''), r.response_status, r.first_content_ms, r.total_ms, r.payload_pruned_at IS NOT NULL, r.created_at, r.completed_at FROM requests r WHERE `+where+` ORDER BY r.created_at DESC LIMIT ? OFFSET ?`, listArgs...)
	if err != nil {
		return RequestPage{}, err
	}
	defer rows.Close()
	var result []RequestSummary
	for rows.Next() {
		item, err := scanRequestSummary(rows)
		if err != nil {
			return RequestPage{}, err
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return RequestPage{}, err
	}
	return RequestPage{Items: result, Total: total, Page: page, PageSize: pageSize}, nil
}

func (s *Store) RequestDetail(id string) (RequestDetail, error) {
	var detail RequestDetail
	var responseStatus, firstContent, total sql.NullInt64
	var inputTokens, cacheReadTokens, cacheWriteTokens, outputTokens, reasoningTokens, totalTokens sql.NullInt64
	var createdAt string
	var completedAt sql.NullString
	var requestBody, responseBody []byte
	var requestBodyEncoding, responseBodyEncoding string
	err := s.db.QueryRow(`SELECT r.id, r.status, COALESCE(r.access_key_name, ''), COALESCE(r.virtual_model, ''), r.inbound_protocol, COALESCE((SELECT a.upstream_protocol FROM attempts a WHERE a.request_id = r.id ORDER BY a.position DESC LIMIT 1), ''), r.stream, r.inbound_endpoint, COALESCE(r.reasoning_effort, ''), r.client_ip, COALESCE(r.user_agent, ''), r.response_status, r.first_content_ms, r.total_ms, r.request_headers, r.request_body, r.request_body_encoding, COALESCE(r.response_headers, ''), r.response_body, r.response_body_encoding, r.input_tokens, r.cache_read_tokens, r.cache_write_tokens, r.output_tokens, r.reasoning_tokens, r.total_tokens, COALESCE(r.error_message, ''), r.payload_pruned_at IS NOT NULL, r.created_at, r.completed_at FROM requests r WHERE r.id = ?`, id).Scan(
		&detail.ID, &detail.Status, &detail.AccessKeyName, &detail.VirtualModel, &detail.InboundProtocol, &detail.UpstreamProtocol, &detail.Stream, &detail.InboundEndpoint, &detail.ReasoningEffort, &detail.ClientIP, &detail.UserAgent, &responseStatus, &firstContent, &total, &detail.RequestHeaders, &requestBody, &requestBodyEncoding, &detail.ResponseHeaders, &responseBody, &responseBodyEncoding, &inputTokens, &cacheReadTokens, &cacheWriteTokens, &outputTokens, &reasoningTokens, &totalTokens, &detail.ErrorMessage, &detail.PayloadPruned, &createdAt, &completedAt,
	)
	if err != nil {
		return RequestDetail{}, err
	}
	decodedRequestBody, err := decodePayload(requestBody, requestBodyEncoding)
	if err != nil {
		return RequestDetail{}, err
	}
	decodedResponseBody, err := decodePayload(responseBody, responseBodyEncoding)
	if err != nil {
		return RequestDetail{}, err
	}
	detail.RequestBody = string(decodedRequestBody)
	detail.ResponseBody = string(decodedResponseBody)
	detail.InputTokens = nullableInt64Pointer(inputTokens)
	detail.CacheReadTokens = nullableInt64Pointer(cacheReadTokens)
	detail.CacheWriteTokens = nullableInt64Pointer(cacheWriteTokens)
	detail.OutputTokens = nullableInt64Pointer(outputTokens)
	detail.ReasoningTokens = nullableInt64Pointer(reasoningTokens)
	detail.TotalTokens = nullableInt64Pointer(totalTokens)
	if err := fillRequestTimes(&detail.RequestSummary, responseStatus, firstContent, total, createdAt, completedAt); err != nil {
		return RequestDetail{}, err
	}
	attempts, err := s.requestAttempts(id)
	if err != nil {
		return RequestDetail{}, err
	}
	detail.Attempts = attempts
	return detail, nil
}

func (s *Store) requestAttempts(requestID string) ([]AttemptDetail, error) {
	rows, err := s.db.Query(`SELECT id, position, COALESCE(provider_name, ''), COALESCE(upstream_key_name, ''), COALESCE(upstream_model, ''), COALESCE(upstream_protocol, ''), COALESCE(upstream_endpoint, ''), status, COALESCE(request_headers, ''), request_body, COALESCE(request_body_encoding, 'identity'), response_status, COALESCE(response_headers, ''), response_body, COALESCE(response_body_encoding, 'identity'), COALESCE(raw_usage_json, ''), first_byte_ms, first_content_ms, total_ms, COALESCE(error_message, ''), payload_pruned_at IS NOT NULL, created_at, completed_at FROM attempts WHERE request_id = ? ORDER BY position`, requestID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]AttemptDetail, 0)
	for rows.Next() {
		var item AttemptDetail
		var requestBody, responseBody []byte
		var requestBodyEncoding, responseBodyEncoding string
		var responseStatus, firstByte, firstContent, total sql.NullInt64
		var createdAt string
		var completedAt sql.NullString
		if err := rows.Scan(&item.ID, &item.Position, &item.ProviderName, &item.UpstreamKeyName, &item.UpstreamModel, &item.UpstreamProtocol, &item.UpstreamEndpoint, &item.Status, &item.RequestHeaders, &requestBody, &requestBodyEncoding, &responseStatus, &item.ResponseHeaders, &responseBody, &responseBodyEncoding, &item.RawUsageJSON, &firstByte, &firstContent, &total, &item.ErrorMessage, &item.PayloadPruned, &createdAt, &completedAt); err != nil {
			return nil, err
		}
		decodedRequestBody, err := decodePayload(requestBody, requestBodyEncoding)
		if err != nil {
			return nil, err
		}
		decodedResponseBody, err := decodePayload(responseBody, responseBodyEncoding)
		if err != nil {
			return nil, err
		}
		item.RequestBody = string(decodedRequestBody)
		item.ResponseBody = string(decodedResponseBody)
		item.ResponseStatus = nullableIntPointer(responseStatus)
		item.FirstByteMS = nullableInt64Pointer(firstByte)
		item.FirstContentMS = nullableInt64Pointer(firstContent)
		item.TotalMS = nullableInt64Pointer(total)
		item.CreatedAt, err = time.Parse(timestampLayout, createdAt)
		if err != nil {
			return nil, err
		}
		item.CompletedAt, err = parseNullableTime(completedAt)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func scanRequestSummary(row rowScanner) (RequestSummary, error) {
	var item RequestSummary
	var responseStatus, firstContent, total sql.NullInt64
	var createdAt string
	var completedAt sql.NullString
	if err := row.Scan(&item.ID, &item.Status, &item.AccessKeyName, &item.VirtualModel, &item.InboundProtocol, &item.UpstreamProtocol, &item.Stream, &item.ClientIP, &item.UserAgent, &responseStatus, &firstContent, &total, &item.PayloadPruned, &createdAt, &completedAt); err != nil {
		return RequestSummary{}, err
	}
	if err := fillRequestTimes(&item, responseStatus, firstContent, total, createdAt, completedAt); err != nil {
		return RequestSummary{}, err
	}
	return item, nil
}

func fillRequestTimes(item *RequestSummary, responseStatus, firstContent, total sql.NullInt64, createdAt string, completedAt sql.NullString) error {
	var err error
	item.CreatedAt, err = time.Parse(timestampLayout, createdAt)
	if err != nil {
		return err
	}
	item.ResponseStatus = nullableIntPointer(responseStatus)
	item.FirstContentMS = nullableInt64Pointer(firstContent)
	item.TotalMS = nullableInt64Pointer(total)
	item.CompletedAt, err = parseNullableTime(completedAt)
	return err
}

func nullableIntPointer(value sql.NullInt64) *int {
	if !value.Valid {
		return nil
	}
	result := int(value.Int64)
	return &result
}

func nullableInt64Pointer(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	result := value.Int64
	return &result
}

func nullableInt(value int) any {
	if value == 0 {
		return nil
	}
	return value
}

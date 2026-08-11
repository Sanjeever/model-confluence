package store

import (
	"database/sql"
	"time"
)

type RequestStart struct {
	ID              string
	AccessKeyID     int64
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
	_, err := s.db.Exec(`INSERT INTO requests (id, status, access_key_id, access_key_name, virtual_model, inbound_protocol, inbound_endpoint, stream, reasoning_effort, client_ip, user_agent, request_headers, request_body, created_at) VALUES (?, 'in_progress', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, input.ID, input.AccessKeyID, input.AccessKeyName, input.VirtualModel, input.InboundProtocol, input.InboundEndpoint, input.Stream, nullableString(input.ReasoningEffort), input.ClientIP, nullableString(input.UserAgent), input.Headers, input.Body, formatTime(input.CreatedAt))
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
	result, err := s.db.Exec(`INSERT INTO attempts (request_id, position, provider_id, provider_name, upstream_key_id, upstream_key_name, candidate_id, upstream_model, upstream_protocol, upstream_endpoint, status, request_headers, request_body, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'in_progress', ?, ?, ?)`, input.RequestID, input.Position, input.ProviderID, input.ProviderName, input.UpstreamKeyID, nullableString(input.UpstreamKeyName), input.CandidateID, input.UpstreamModel, input.Protocol, input.Endpoint, input.Headers, input.Body, formatTime(input.CreatedAt))
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
	_, err := s.db.Exec(`UPDATE attempts SET status = ?, response_status = ?, response_headers = ?, response_body = ?, raw_usage_json = ?, first_byte_ms = ?, first_content_ms = ?, total_ms = ?, error_message = ?, completed_at = ? WHERE id = ?`, result.Status, nullableInt(result.ResponseStatus), nullableString(result.ResponseHeaders), result.ResponseBody, nullableString(result.RawUsageJSON), result.FirstByteMS, result.FirstContentMS, result.TotalMS, nullableString(result.ErrorMessage), formatTime(result.CompletedAt), result.ID)
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
	_, err := s.db.Exec(`UPDATE requests SET status = ?, response_status = ?, response_headers = ?, response_body = ?, input_tokens = ?, cache_read_tokens = ?, cache_write_tokens = ?, output_tokens = ?, reasoning_tokens = ?, total_tokens = ?, first_content_ms = ?, total_ms = ?, error_message = ?, completed_at = ? WHERE id = ?`, result.Status, nullableInt(result.ResponseStatus), nullableString(result.ResponseHeaders), result.ResponseBody, result.InputTokens, result.CacheReadTokens, result.CacheWriteTokens, result.OutputTokens, result.ReasoningTokens, result.TotalTokens, result.FirstContentMS, result.TotalMS, nullableString(result.ErrorMessage), formatTime(result.CompletedAt), result.ID)
	return err
}

func (s *Store) CreateSecurityEvent(eventType, clientIP, userAgent, endpoint, reason string) error {
	_, err := s.db.Exec(`INSERT INTO security_events (event_type, client_ip, user_agent, endpoint, reason, created_at) VALUES (?, ?, ?, ?, ?, ?)`, eventType, clientIP, nullableString(userAgent), endpoint, reason, formatTime(time.Now()))
	return err
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
	InboundEndpoint  string          `json:"inbound_endpoint"`
	ReasoningEffort  string          `json:"reasoning_effort"`
	RequestHeaders   string          `json:"request_headers"`
	RequestBody      string          `json:"request_body"`
	ResponseHeaders  string          `json:"response_headers"`
	ResponseBody     string          `json:"response_body"`
	InputTokens      *int64          `json:"input_tokens"`
	CacheReadTokens  *int64          `json:"cache_read_tokens"`
	CacheWriteTokens *int64          `json:"cache_write_tokens"`
	OutputTokens     *int64          `json:"output_tokens"`
	ReasoningTokens  *int64          `json:"reasoning_tokens"`
	TotalTokens      *int64          `json:"total_tokens"`
	ErrorMessage     string          `json:"error_message"`
	Attempts         []AttemptDetail `json:"attempts"`
}

type AttemptDetail struct {
	ID               int64      `json:"id"`
	Position         int        `json:"position"`
	ProviderName     string     `json:"provider_name"`
	UpstreamKeyName  string     `json:"upstream_key_name"`
	UpstreamModel    string     `json:"upstream_model"`
	UpstreamProtocol string     `json:"upstream_protocol"`
	UpstreamEndpoint string     `json:"upstream_endpoint"`
	Status           string     `json:"status"`
	RequestHeaders   string     `json:"request_headers"`
	RequestBody      string     `json:"request_body"`
	ResponseStatus   *int       `json:"response_status"`
	ResponseHeaders  string     `json:"response_headers"`
	ResponseBody     string     `json:"response_body"`
	RawUsageJSON     string     `json:"raw_usage_json"`
	FirstByteMS      *int64     `json:"first_byte_ms"`
	FirstContentMS   *int64     `json:"first_content_ms"`
	TotalMS          *int64     `json:"total_ms"`
	ErrorMessage     string     `json:"error_message"`
	CreatedAt        time.Time  `json:"created_at"`
	CompletedAt      *time.Time `json:"completed_at"`
}

func (s *Store) ListRequests(page, pageSize int, requestID string) (RequestPage, error) {
	var total int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM requests WHERE ? = '' OR instr(id, ?) > 0`, requestID, requestID).Scan(&total); err != nil {
		return RequestPage{}, err
	}
	rows, err := s.db.Query(`SELECT r.id, r.status, COALESCE(r.access_key_name, ''), COALESCE(r.virtual_model, ''), r.inbound_protocol, COALESCE((SELECT a.upstream_protocol FROM attempts a WHERE a.request_id = r.id ORDER BY a.position DESC LIMIT 1), ''), r.stream, r.client_ip, COALESCE(r.user_agent, ''), r.response_status, r.first_content_ms, r.total_ms, r.created_at, r.completed_at FROM requests r WHERE ? = '' OR instr(r.id, ?) > 0 ORDER BY r.created_at DESC LIMIT ? OFFSET ?`, requestID, requestID, pageSize, (page-1)*pageSize)
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
	err := s.db.QueryRow(`SELECT r.id, r.status, COALESCE(r.access_key_name, ''), COALESCE(r.virtual_model, ''), r.inbound_protocol, COALESCE((SELECT a.upstream_protocol FROM attempts a WHERE a.request_id = r.id ORDER BY a.position DESC LIMIT 1), ''), r.stream, r.inbound_endpoint, COALESCE(r.reasoning_effort, ''), r.client_ip, COALESCE(r.user_agent, ''), r.response_status, r.first_content_ms, r.total_ms, r.request_headers, r.request_body, COALESCE(r.response_headers, ''), r.response_body, r.input_tokens, r.cache_read_tokens, r.cache_write_tokens, r.output_tokens, r.reasoning_tokens, r.total_tokens, COALESCE(r.error_message, ''), r.created_at, r.completed_at FROM requests r WHERE r.id = ?`, id).Scan(
		&detail.ID, &detail.Status, &detail.AccessKeyName, &detail.VirtualModel, &detail.InboundProtocol, &detail.UpstreamProtocol, &detail.Stream, &detail.InboundEndpoint, &detail.ReasoningEffort, &detail.ClientIP, &detail.UserAgent, &responseStatus, &firstContent, &total, &detail.RequestHeaders, &requestBody, &detail.ResponseHeaders, &responseBody, &inputTokens, &cacheReadTokens, &cacheWriteTokens, &outputTokens, &reasoningTokens, &totalTokens, &detail.ErrorMessage, &createdAt, &completedAt,
	)
	if err != nil {
		return RequestDetail{}, err
	}
	detail.RequestBody = string(requestBody)
	detail.ResponseBody = string(responseBody)
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
	rows, err := s.db.Query(`SELECT id, position, COALESCE(provider_name, ''), COALESCE(upstream_key_name, ''), COALESCE(upstream_model, ''), COALESCE(upstream_protocol, ''), COALESCE(upstream_endpoint, ''), status, COALESCE(request_headers, ''), request_body, response_status, COALESCE(response_headers, ''), response_body, COALESCE(raw_usage_json, ''), first_byte_ms, first_content_ms, total_ms, COALESCE(error_message, ''), created_at, completed_at FROM attempts WHERE request_id = ? ORDER BY position`, requestID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]AttemptDetail, 0)
	for rows.Next() {
		var item AttemptDetail
		var requestBody, responseBody []byte
		var responseStatus, firstByte, firstContent, total sql.NullInt64
		var createdAt string
		var completedAt sql.NullString
		if err := rows.Scan(&item.ID, &item.Position, &item.ProviderName, &item.UpstreamKeyName, &item.UpstreamModel, &item.UpstreamProtocol, &item.UpstreamEndpoint, &item.Status, &item.RequestHeaders, &requestBody, &responseStatus, &item.ResponseHeaders, &responseBody, &item.RawUsageJSON, &firstByte, &firstContent, &total, &item.ErrorMessage, &createdAt, &completedAt); err != nil {
			return nil, err
		}
		item.RequestBody = string(requestBody)
		item.ResponseBody = string(responseBody)
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
	if err := row.Scan(&item.ID, &item.Status, &item.AccessKeyName, &item.VirtualModel, &item.InboundProtocol, &item.UpstreamProtocol, &item.Stream, &item.ClientIP, &item.UserAgent, &responseStatus, &firstContent, &total, &createdAt, &completedAt); err != nil {
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

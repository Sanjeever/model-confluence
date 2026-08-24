package store

const schema = `
PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS schema_migrations (
    version INTEGER PRIMARY KEY,
    applied_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS admin (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    password_hash TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS admin_sessions (
    token TEXT PRIMARY KEY,
    created_at TEXT NOT NULL,
    last_seen_at TEXT NOT NULL,
    expires_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS access_keys (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL UNIQUE,
    secret TEXT NOT NULL UNIQUE,
    enabled INTEGER NOT NULL DEFAULT 1,
    expires_at TEXT,
    last_used_at TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    archived_at TEXT
);

CREATE TABLE IF NOT EXISTS providers (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL UNIQUE,
    enabled INTEGER NOT NULL DEFAULT 1,
    auth_type TEXT NOT NULL,
    auth_header TEXT,
    static_headers_json TEXT NOT NULL DEFAULT '{}',
    quota_codes_json TEXT NOT NULL DEFAULT '[]',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    archived_at TEXT
);

CREATE TABLE IF NOT EXISTS provider_endpoints (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    provider_id INTEGER NOT NULL REFERENCES providers(id),
    protocol TEXT NOT NULL,
    url TEXT NOT NULL,
    UNIQUE(provider_id, protocol)
);

CREATE TABLE IF NOT EXISTS upstream_keys (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    provider_id INTEGER NOT NULL REFERENCES providers(id),
    name TEXT,
    secret TEXT NOT NULL,
    position INTEGER NOT NULL,
    enabled INTEGER NOT NULL DEFAULT 1,
    expires_at TEXT,
    runtime_status TEXT NOT NULL DEFAULT 'available',
    runtime_reason TEXT,
    recover_at TEXT,
    last_used_at TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    archived_at TEXT,
    UNIQUE(provider_id, position)
);

CREATE TABLE IF NOT EXISTS virtual_models (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL UNIQUE,
    enabled INTEGER NOT NULL DEFAULT 1,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    archived_at TEXT
);

CREATE TABLE IF NOT EXISTS model_candidates (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    virtual_model_id INTEGER NOT NULL REFERENCES virtual_models(id),
    provider_id INTEGER NOT NULL REFERENCES providers(id),
    upstream_model TEXT NOT NULL,
    position INTEGER NOT NULL,
    enabled INTEGER NOT NULL DEFAULT 1,
    default_max_output_tokens INTEGER NOT NULL,
    max_output_tokens INTEGER NOT NULL,
    runtime_status TEXT NOT NULL DEFAULT 'available',
    runtime_reason TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    archived_at TEXT,
    UNIQUE(virtual_model_id, position)
);

CREATE TABLE IF NOT EXISTS candidate_protocols (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    candidate_id INTEGER NOT NULL REFERENCES model_candidates(id),
    protocol TEXT NOT NULL,
    position INTEGER NOT NULL,
    supports_stream INTEGER NOT NULL DEFAULT 1,
    supports_tools INTEGER NOT NULL DEFAULT 1,
    supports_parallel_tools INTEGER NOT NULL DEFAULT 1,
    effort_levels_json TEXT NOT NULL DEFAULT '[]',
    supports_stream_usage INTEGER NOT NULL DEFAULT 0,
    UNIQUE(candidate_id, protocol),
    UNIQUE(candidate_id, position)
);

CREATE TABLE IF NOT EXISTS requests (
    id TEXT PRIMARY KEY,
    status TEXT NOT NULL,
    access_key_id INTEGER REFERENCES access_keys(id),
    access_key_name TEXT,
    virtual_model TEXT,
    upstream_model TEXT,
    provider_name TEXT,
    inbound_protocol TEXT NOT NULL,
    inbound_endpoint TEXT NOT NULL,
	stream INTEGER NOT NULL DEFAULT 0,
    reasoning_effort TEXT,
    client_ip TEXT NOT NULL,
    user_agent TEXT,
    request_headers TEXT NOT NULL,
    request_body BLOB NOT NULL,
    request_body_encoding TEXT NOT NULL DEFAULT 'identity',
    response_status INTEGER,
    response_headers TEXT,
    response_body BLOB,
    response_body_encoding TEXT NOT NULL DEFAULT 'identity',
    input_tokens INTEGER,
    cache_read_tokens INTEGER,
    cache_write_tokens INTEGER,
    output_tokens INTEGER,
    reasoning_tokens INTEGER,
    total_tokens INTEGER,
    first_content_ms INTEGER,
    total_ms INTEGER,
    error_message TEXT,
    created_at TEXT NOT NULL,
    completed_at TEXT,
    payload_pruned_at TEXT
);

CREATE TABLE IF NOT EXISTS attempts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    request_id TEXT NOT NULL REFERENCES requests(id) ON DELETE CASCADE,
    position INTEGER NOT NULL,
    provider_id INTEGER REFERENCES providers(id),
    provider_name TEXT,
    upstream_key_id INTEGER REFERENCES upstream_keys(id),
    upstream_key_name TEXT,
    candidate_id INTEGER REFERENCES model_candidates(id),
    upstream_model TEXT,
    upstream_protocol TEXT,
    upstream_endpoint TEXT,
    status TEXT NOT NULL,
    request_headers TEXT,
    request_body BLOB,
    request_body_encoding TEXT NOT NULL DEFAULT 'identity',
    response_status INTEGER,
    response_headers TEXT,
    response_body BLOB,
    response_body_encoding TEXT NOT NULL DEFAULT 'identity',
    raw_usage_json TEXT,
    first_byte_ms INTEGER,
    first_content_ms INTEGER,
    total_ms INTEGER,
    error_message TEXT,
    created_at TEXT NOT NULL,
    completed_at TEXT,
    payload_pruned_at TEXT,
    UNIQUE(request_id, position)
);

CREATE TABLE IF NOT EXISTS security_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    event_type TEXT NOT NULL,
    client_ip TEXT NOT NULL,
    user_agent TEXT,
    endpoint TEXT NOT NULL,
    reason TEXT NOT NULL,
    created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_requests_created_at ON requests(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_requests_status ON requests(status);
CREATE INDEX IF NOT EXISTS idx_requests_access_key_id ON requests(access_key_id);
CREATE INDEX IF NOT EXISTS idx_requests_virtual_model ON requests(virtual_model);
CREATE INDEX IF NOT EXISTS idx_attempts_request_id ON attempts(request_id);
CREATE INDEX IF NOT EXISTS idx_attempts_status ON attempts(status);
CREATE INDEX IF NOT EXISTS idx_attempts_provider_id ON attempts(provider_id);
CREATE INDEX IF NOT EXISTS idx_attempts_upstream_key_id ON attempts(upstream_key_id);
CREATE INDEX IF NOT EXISTS idx_attempts_candidate_id ON attempts(candidate_id);
CREATE INDEX IF NOT EXISTS idx_security_events_created_at ON security_events(created_at DESC);
`

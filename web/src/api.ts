export class APIError extends Error {
  status: number

  constructor(status: number, message: string) {
    super(message)
    this.status = status
  }
}

function cookie(name: string): string {
  const prefix = `${name}=`
  return document.cookie
    .split(';')
    .map((item) => item.trim())
    .find((item) => item.startsWith(prefix))
    ?.slice(prefix.length) ?? ''
}

export async function api<T>(path: string, init: RequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers)
  if (init.body) headers.set('Content-Type', 'application/json')
  const method = init.method?.toUpperCase() ?? 'GET'
  if (!['GET', 'HEAD', 'OPTIONS'].includes(method)) {
    headers.set('X-CSRF-Token', decodeURIComponent(cookie('mc_csrf')))
  }
  const response = await fetch(path, { ...init, headers, credentials: 'same-origin' })
  if (!response.ok) {
    const payload = await response.json().catch(() => ({ error: response.statusText })) as { error?: string }
    throw new APIError(response.status, payload.error ?? response.statusText)
  }
  if (response.status === 204) return undefined as T
  return response.json() as Promise<T>
}

export type Overview = {
  access_keys: number
  providers: number
  virtual_models: number
  request_count: number
}

export type AccessKey = {
  id: number
  name: string
  secret?: string
  secret_hint: string
  enabled: boolean
  expires_at: string | null
  last_used_at: string | null
  created_at: string
}

export type UpstreamKey = {
  id: number
  name: string
  secret?: string
  secret_hint: string
  position: number
  enabled: boolean
  expires_at: string | null
  runtime_status: string
  runtime_reason?: string
  recover_at: string | null
  last_used_at: string | null
}

export type Provider = {
  id: number
  name: string
  enabled: boolean
  auth_type: 'bearer' | 'x-api-key' | 'custom'
  auth_header?: string
  static_headers: Record<string, string>
  quota_codes: string[]
  endpoints: Record<string, string>
  keys: UpstreamKey[]
  created_at: string
}

export type CandidateProtocol = {
  protocol: 'chat_completions' | 'responses' | 'messages'
  position: number
  supports_stream: boolean
  supports_tools: boolean
  supports_parallel_tools: boolean
  effort_levels: string[]
  supports_stream_usage: boolean
}

export type VirtualModel = {
  id: number
  name: string
  enabled: boolean
  candidates: Array<{
    id: number
    provider_id: number
    provider_name: string
    upstream_model: string
    position: number
    enabled: boolean
    default_max_output_tokens: number
    max_output_tokens: number
    runtime_status: string
    protocols: CandidateProtocol[]
  }>
  created_at: string
}

export type ModelTestResult = {
  request_id: string
  response: unknown
}

export type RequestSummary = {
  id: string
  status: string
  access_key_name: string
  virtual_model: string
  inbound_protocol: string
  upstream_protocol: string
  stream: boolean
  client_ip: string
  user_agent: string
  response_status: number | null
  first_content_ms: number | null
  total_ms: number | null
  created_at: string
  completed_at: string | null
}

export type StreamSummaryBlock = {
  type: 'text' | 'reasoning' | 'tool_call'
  index: number
  content?: string
  call_id?: string
  name?: string
  arguments?: string
  arguments_valid: boolean
  complete: boolean
}

export type StreamSummary = {
  parse_status: 'ok' | 'partial' | 'unavailable'
  completed: boolean
  stop_reason: string | null
  blocks: StreamSummaryBlock[]
  warnings: string[]
}

export type RequestPage = {
  items: RequestSummary[]
  total: number
  page: number
  page_size: number
}

export type AttemptDetail = {
  id: number
  position: number
  provider_name: string
  upstream_key_name: string
  upstream_model: string
  upstream_protocol: string
  upstream_endpoint: string
  status: string
  request_headers: string
  request_body: string
  response_status: number | null
  response_headers: string
  response_body: string
  response_summary?: StreamSummary
  raw_usage_json: string
  first_byte_ms: number | null
  first_content_ms: number | null
  total_ms: number | null
  error_message: string
  created_at: string
  completed_at: string | null
}

export type RequestDetail = RequestSummary & {
  inbound_endpoint: string
  reasoning_effort: string
  request_headers: string
  request_body: string
  response_headers: string
  response_body: string
  response_summary?: StreamSummary
  input_tokens: number | null
  cache_read_tokens: number | null
  cache_write_tokens: number | null
  output_tokens: number | null
  reasoning_tokens: number | null
  total_tokens: number | null
  error_message: string
  attempts: AttemptDetail[]
}

export type DeleteResult = {
  archived: boolean
}

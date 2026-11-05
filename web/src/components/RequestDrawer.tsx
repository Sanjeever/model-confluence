import { useEffect, useState } from 'react'
import {
  ChevronDown,
  ArrowDownLeft,
  ArrowUpRight,
  Eye,
  EyeOff,
} from 'lucide-react'
import type { RequestDetail, AttemptDetail, StreamSummary } from '../api'
import { formatCount, formatDuration } from '../format'
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
  SheetDescription,
} from './ui/sheet'
import { Button } from './ui/button'
import { Tabs, TabsContent, TabsList, TabsTrigger } from './ui/tabs'
import {
  CopyButton,
  EmptyState,
  ErrorNotice,
  Loading,
  MetaGrid,
  displayDate,
} from './console'
import { ProtocolFlow, Status, protocolName, statusName } from './Status'
import JsonPayload from './JsonPayload'
import StreamResponseViewer from './StreamResponseViewer'
import { RouteDecisions } from './RouteDiagnostics'
import { cn } from '@/lib/utils'

export { protocolName, statusName }
export default function RequestDrawer({
  detail,
  loading,
  open,
  onClose,
  error,
  onRetry,
}: {
  detail?: RequestDetail
  loading: boolean
  open: boolean
  onClose: () => void
  error?: Error | null
  onRetry?: () => void
}) {
  const [reveal, setReveal] = useState(false)
  const [selected, setSelected] = useState('inbound')
  useEffect(() => {
    setReveal(false)
    const failed =
      detail?.status === 'failed'
        ? [...detail.attempts]
            .reverse()
            .find((attempt) => attempt.status === 'failed')
        : undefined
    setSelected(failed ? `attempt:${failed.id}` : 'inbound')
  }, [detail?.id])
  const attempt = detail?.attempts.find(
    (item) => `attempt:${item.id}` === selected,
  )
  return (
    <Sheet
      open={open}
      onOpenChange={(next) => {
        if (!next) onClose()
      }}
    >
      <SheetContent className="gap-0 p-0 data-[side=right]:w-full data-[side=right]:sm:max-w-[min(1200px,100vw)]">
        <SheetHeader className="shrink-0 border-b px-5 py-4 pr-14">
          <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
            <SheetTitle>请求详情</SheetTitle>
            {detail && (
              <div className="flex min-w-0 items-center gap-1">
                <code className="truncate text-xs text-muted-foreground">
                  {detail.id}
                </code>
                <CopyButton value={detail.id} label="复制请求 ID" />
              </div>
            )}
          </div>
          <SheetDescription className="sr-only">
            请求摘要、上游尝试及协议载荷
          </SheetDescription>
        </SheetHeader>
        {error ? (
          <div className="p-5">
            <ErrorNotice error={error} retry={onRetry} />
          </div>
        ) : loading || !detail ? (
          <Loading />
        ) : (
          <>
            <div className="shrink-0 border-b px-5 py-4">
              <div className="flex flex-wrap items-center gap-x-5 gap-y-2">
                <Status value={detail.status} />
                <code className="text-sm font-medium">
                  {detail.virtual_model || '未指定模型'}
                </code>
                <ProtocolFlow
                  inbound={detail.inbound_protocol}
                  upstream={detail.upstream_protocol}
                />
                <span className="ml-auto text-xs text-muted-foreground">
                  首内容{' '}
                  <span className="ml-1 tabular-nums text-foreground">
                    {formatDuration(detail.first_content_ms)}
                  </span>
                </span>
                <span className="text-xs text-muted-foreground">
                  总耗时{' '}
                  <span className="ml-1 tabular-nums text-foreground">
                    {formatDuration(detail.total_ms)}
                  </span>
                </span>
              </div>
              {detail.error_message && (
                <div
                  role="alert"
                  className="mt-3 break-words text-xs leading-5 text-destructive"
                >
                  {detail.error_message}
                </div>
              )}
              {!!detail.route_decisions?.length && <details className="mt-3 text-xs"><summary className="w-fit cursor-pointer text-muted-foreground">路由决策</summary><div className="mt-3 space-y-2"><RouteDecisions decisions={detail.route_decisions} />{detail.routing_events?.map((event, index) => <p key={index} className="text-muted-foreground">候选 {event.candidate_id} · 密钥 {event.key_id} · {event.action}</p>)}</div></details>}
              <details className="group mt-4">
                <summary className="flex w-fit cursor-pointer list-none items-center gap-1 text-xs text-muted-foreground">
                  请求信息
                  <ChevronDown className="size-3 transition-transform group-open:rotate-180" />
                </summary>
                <div className="pt-4">
                  <MetaGrid
                    items={[
                      {
                        label: '请求时间',
                        value: displayDate(detail.created_at),
                      },
                      { label: '访问密钥', value: detail.access_key_name },
                      {
                        label: '请求方式',
                        value: detail.stream ? '流式' : '非流式',
                      },
                      {
                        label: '输入 Token',
                        value: formatCount(detail.input_tokens),
                      },
                      {
                        label: '缓存读 / 写',
                        value: `${formatCount(detail.cache_read_tokens)} / ${formatCount(detail.cache_write_tokens)}`,
                      },
                      {
                        label: '输出 / 推理 Token',
                        value: `${formatCount(detail.output_tokens)} / ${formatCount(detail.reasoning_tokens)}`,
                      },
                    ]}
                  />
                </div>
              </details>
            </div>
            <div className="grid min-h-0 flex-1 grid-rows-[auto_minmax(0,1fr)] md:grid-cols-[210px_minmax(0,1fr)] md:grid-rows-1">
              <nav
                aria-label="调用过程"
                className="flex gap-1 overflow-x-auto border-b bg-muted/20 p-3 md:flex-col md:overflow-y-auto md:border-r md:border-b-0"
              >
                <Node
                  label="入站请求"
                  selected={selected === 'inbound'}
                  onClick={() => setSelected('inbound')}
                  icon="in"
                />
                <div className="hidden px-3 pt-4 pb-2 text-[11px] text-muted-foreground md:block">
                  上游尝试 · {detail.attempts.length}
                </div>
                {detail.attempts.map((item) => (
                  <Node
                    key={item.id}
                    label={item.provider_name}
                    sublabel={item.upstream_model}
                    selected={selected === `attempt:${item.id}`}
                    onClick={() => setSelected(`attempt:${item.id}`)}
                    status={item.status}
                    position={item.position + 1}
                  />
                ))}
                {!detail.attempts.length && (
                  <span className="hidden px-3 py-2 text-xs text-muted-foreground md:block">
                    尚未调用上游
                  </span>
                )}
                <div className="hidden h-3 md:block" />
                <Node
                  label="客户端响应"
                  selected={selected === 'response'}
                  onClick={() => setSelected('response')}
                  icon="out"
                />
              </nav>
              <div className="min-w-0 overflow-y-auto p-4 sm:p-5">
                <div className="mb-4 flex items-center justify-between gap-3">
                  <h2 className="text-sm font-semibold">
                    {attempt
                      ? `上游尝试 ${attempt.position + 1}`
                      : selected === 'response'
                        ? '客户端响应'
                        : '入站请求'}
                  </h2>
                  <Button
                    size="sm"
                    variant="ghost"
                    onClick={() => setReveal(!reveal)}
                  >
                    {reveal ? <EyeOff /> : <Eye />}
                    {reveal ? '遮罩凭据' : '显示凭据'}
                  </Button>
                </div>
                {attempt ? (
                  <AttemptPayload
                    key={attempt.id}
                    attempt={attempt}
                    reveal={reveal}
                  />
                ) : (
                  <PayloadPair
                    headers={
                      selected === 'response'
                        ? detail.response_headers
                        : detail.request_headers
                    }
                    body={
                      selected === 'response'
                        ? detail.response_body
                        : detail.request_body
                    }
                    reveal={reveal}
                    headerLabel={selected === 'response' ? '响应头' : '请求头'}
                    stream={selected === 'response' && detail.stream}
                    summary={detail.response_summary}
                    pruned={detail.payload_pruned}
                    pending={
                      detail.status === 'in_progress' && selected === 'response'
                    }
                  />
                )}
              </div>
            </div>
          </>
        )}
      </SheetContent>
    </Sheet>
  )
}
function Node({
  label,
  sublabel,
  selected,
  onClick,
  status,
  position,
  icon,
}: {
  label: string
  sublabel?: string
  selected: boolean
  onClick: () => void
  status?: string
  position?: number
  icon?: string
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-current={selected ? 'step' : undefined}
      className={cn(
        'flex shrink-0 items-start gap-2 rounded-md px-3 py-2.5 text-left transition-colors hover:bg-accent md:w-full',
        selected ? 'bg-accent' : '',
      )}
    >
      <span className="mt-0.5 w-4 shrink-0 text-xs text-muted-foreground">
        {position ??
          (icon === 'in' ? (
            <ArrowDownLeft className="size-4" />
          ) : (
            <ArrowUpRight className="size-4" />
          ))}
      </span>
      <span className="min-w-0">
        <span className="block truncate text-xs font-medium">{label}</span>
        {sublabel && (
          <code
            className="mt-1 hidden truncate text-[11px] text-muted-foreground md:block"
            title={sublabel}
          >
            {sublabel}
          </code>
        )}
        {status && (
          <span className="mt-1 hidden md:block">
            <Status value={status} />
          </span>
        )}
      </span>
    </button>
  )
}
function AttemptPayload({
  attempt,
  reveal,
}: {
  attempt: AttemptDetail
  reveal: boolean
}) {
  return (
    <div className="space-y-5">
      <MetaGrid
        items={[
          { label: '状态', value: <Status value={attempt.status} /> },
          {
            label: '上游协议 / 传输',
            value: `${protocolName(attempt.upstream_protocol)} / ${attempt.upstream_stream ? 'SSE' : 'JSON'}`,
          },
          { label: '响应状态', value: attempt.response_status ?? '—' },
          {
            label: '真实模型',
            value: <code className="text-xs">{attempt.upstream_model}</code>,
          },
          { label: '上游凭据', value: attempt.upstream_key_name || '未命名' },
          { label: '总耗时', value: formatDuration(attempt.total_ms) },
          {
            label: '上游端点',
            value: (
              <code className="break-all text-xs">
                {attempt.upstream_endpoint}
              </code>
            ),
          },
          {
            label: '忽略参数',
            value: attempt.ignored_parameters.length
              ? attempt.ignored_parameters.join('、')
              : '—',
          },
        ]}
      />
      {attempt.error_message && (
        <p
          role="alert"
          className="break-words text-xs leading-5 text-destructive"
        >
          {attempt.error_message}
        </p>
      )}
      <Tabs defaultValue="response">
        <TabsList>
          <TabsTrigger value="request">发送到上游</TabsTrigger>
          <TabsTrigger value="response">上游响应</TabsTrigger>
          {attempt.raw_usage_json && (
            <TabsTrigger value="usage">原始用量</TabsTrigger>
          )}
        </TabsList>
        <TabsContent value="request" className="pt-3">
          <PayloadPair
            headers={attempt.request_headers}
            body={attempt.request_body}
            reveal={reveal}
            pruned={attempt.payload_pruned}
          />
        </TabsContent>
        <TabsContent value="response" className="pt-3">
          <PayloadPair
            headerLabel="响应头"
            headers={attempt.response_headers}
            body={attempt.response_body}
            reveal={reveal}
            stream={attempt.upstream_stream}
            summary={attempt.response_summary}
            pruned={attempt.payload_pruned}
            pending={attempt.status === 'in_progress'}
          />
        </TabsContent>
        <TabsContent value="usage" className="pt-3">
          <JsonPayload value={attempt.raw_usage_json} />
        </TabsContent>
      </Tabs>
    </div>
  )
}
function PayloadPair({
  headers,
  body,
  reveal,
  headerLabel = '请求头',
  stream,
  summary,
  pruned,
  pending,
}: {
  headers: string
  body: string
  reveal: boolean
  headerLabel?: string
  stream?: boolean
  summary?: StreamSummary
  pruned?: boolean
  pending?: boolean
}) {
  if (pruned) return <EmptyState>载荷已按日志保留策略清理</EmptyState>
  return (
    <div className="space-y-4">
      <details className="group">
        <summary className="mb-2 flex w-fit cursor-pointer list-none items-center gap-1 text-xs text-muted-foreground">
          {headerLabel}
          <ChevronDown className="size-3 transition-transform group-open:rotate-180" />
        </summary>
        <JsonPayload value={reveal ? headers : maskHeaders(headers)} />
      </details>
      {pending && !body ? (
        <EmptyState>请求进行中，载荷将在上游尝试结束后展示</EmptyState>
      ) : stream ? (
        <StreamResponseViewer body={body} summary={summary} />
      ) : (
        <JsonPayload value={body} />
      )}
    </div>
  )
}
function maskHeaders(value: string): string {
  if (!value) return ''
  try {
    const headers = JSON.parse(value) as Record<string, unknown>
    for (const name of Object.keys(headers)) {
      if (
        [
          'authorization',
          'x-api-key',
          'cookie',
          'set-cookie',
          'api-key',
        ].includes(name.toLowerCase())
      )
        headers[name] = ['••••••••']
    }
    return JSON.stringify(headers)
  } catch {
    return value
  }
}

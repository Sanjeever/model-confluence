import { useMemo, useRef } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link, useLocation, useSearchParams } from 'react-router-dom'
import { ArrowRight, X } from 'lucide-react'
import { api, type LiveRouteEvent, type LiveRoutingCatalogNode, type LiveRoutingRequest, type LiveRoutingSnapshot } from '@/api'
import LiveRoutingGraph from '@/components/LiveRoutingGraph'
import { ErrorNotice, PageHeader } from '@/components/console'
import { protocolName } from '@/components/Status'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { formatDuration } from '@/format'
import { accessKeyName, buildRoutingGraph, liveStagePresentation, requestMatchesNode, stageLabel, virtualModelName } from '@/lib/live-routing'
import { cn } from '@/lib/utils'

const noRequests: LiveRoutingRequest[] = []
const noNodes: LiveRoutingCatalogNode[] = []

function elapsed(request: LiveRoutingRequest, at: string) {
  return formatDuration(Math.max(0, Date.parse(at) - Date.parse(request.started_at)))
}

function RequestStage({ request }: { request: LiveRoutingRequest }) {
  return (
    <span className="inline-flex items-center gap-1.5 whitespace-nowrap text-xs" style={{ color: `var(--${liveStagePresentation[request.stage].tone})` }}>
      <span aria-hidden="true" className="size-1.5 shrink-0 rounded-full bg-current" />
      {stageLabel(request)}
    </span>
  )
}

function eventLabel(event: LiveRouteEvent) {
  const provider = event.upstream?.provider_name
  if (event.kind === 'attempt_started') return `调用 ${provider} / ${event.upstream?.model}`
  if (event.kind === 'attempt_failed') return event.response_status ? `${provider} 返回 ${event.response_status}` : `${provider} 调用失败`
  if (event.kind === 'streaming') return '向客户端写出首内容'
  const label = liveStagePresentation[event.kind].label
  return event.response_status && event.response_status >= 300 ? `${label} · HTTP ${event.response_status}` : label
}

function RoutingTrail({ request }: { request: LiveRoutingRequest }) {
  const { pathname, search } = useLocation()
  return (
    <section aria-labelledby="live-route-trail-heading" className="rounded-b-lg border-t bg-muted/60 px-4 py-3">
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <h3 id="live-route-trail-heading" className="text-xs font-semibold">
          路由记录 <code className="ml-1 break-all font-normal text-muted-foreground">{request.id}</code>
        </h3>
        <Link to={`/requests/${request.id}`} state={{ returnTo: pathname + search }} className="text-link text-xs">
          查看使用记录
        </Link>
      </div>
      <ol className="mt-2 flex flex-wrap items-center gap-x-2.5 gap-y-1 text-xs">
        {request.events.map((event, index) => (
          <li key={`${event.kind}-${event.at}-${index}`} className="inline-flex min-w-0 items-center gap-1.5">
            {index > 0 && <ArrowRight aria-hidden="true" className="size-3 shrink-0 text-muted-foreground" />}
            <time className="shrink-0 font-mono text-[11px] text-muted-foreground" dateTime={event.at}>+{elapsed(request, event.at)}</time>
            <span className={cn('wrap-anywhere', (event.kind === 'failed' || event.kind === 'attempt_failed') && 'text-destructive')}>
              {eventLabel(event)}
            </span>
          </li>
        ))}
      </ol>
    </section>
  )
}

export default function LiveRoutingPage() {
  const snapshot = useQuery({
    queryKey: ['live-routing'],
    queryFn: ({ signal }) => api<LiveRoutingSnapshot>('/api/admin/live-routing', { signal }),
    refetchInterval: 1000,
    refetchIntervalInBackground: false,
    refetchOnWindowFocus: 'always',
    retry: false,
  })
  const catalog = useQuery({
    queryKey: ['live-routing-catalog'],
    queryFn: ({ signal }) => api<LiveRoutingCatalogNode[]>('/api/admin/live-routing/catalog', { signal }),
    refetchInterval: 10000,
    refetchIntervalInBackground: false,
    refetchOnWindowFocus: 'always',
  })
  const [params, setParams] = useSearchParams()
  const tableScroll = useRef<HTMLDivElement>(null)
  const trail = useRef<HTMLDivElement>(null)
  const filter = params.get('node') ?? ''
  const selectedID = params.get('request') ?? ''
  const showIdle = params.get('idle') === '1'
  const active = snapshot.data?.active ?? noRequests
  const recent = snapshot.data?.recent ?? noRequests
  const catalogNodes = catalog.data ?? noNodes
  const graph = useMemo(() => buildRoutingGraph(active, catalogNodes, showIdle, filter), [active, catalogNodes, showIdle, filter])
  const shown = useMemo(() => filter ? active.filter((request) => requestMatchesNode(request, filter)) : active, [active, filter])
  const focus = useMemo(() => filter ? new Set(shown.map((request) => request.id)) : null, [filter, shown])
  const selected = active.find((request) => request.id === selectedID) ?? recent.find((request) => request.id === selectedID)
  const currentUpstreams = graph.columns.find((column) => column.kind === 'provider')?.live ?? 0

  function update(values: Record<string, string>) {
    setParams((previous) => {
      const next = new URLSearchParams(previous)
      for (const [key, value] of Object.entries(values)) {
        if (value) next.set(key, value)
        else next.delete(key)
      }
      return next
    }, { replace: true, preventScrollReset: true })
  }

  function selectNode(key: string) {
    if (tableScroll.current) tableScroll.current.scrollTop = 0
    update({ node: key === filter ? '' : key, request: '' })
  }

  function selectRequest(request: LiveRoutingRequest) {
    const id = selected?.id === request.id ? '' : request.id
    update({ request: id })
    if (id && request.completed_at) requestAnimationFrame(() => trail.current?.scrollIntoView({ block: 'nearest' }))
  }

  function requestButton(request: LiveRoutingRequest) {
    return (
      <button
        type="button"
        className="text-link block w-full min-w-0 max-w-36 truncate rounded-sm text-left font-mono text-xs font-normal"
        title={request.id}
        aria-expanded={selected?.id === request.id}
        aria-controls="live-route-trail"
        onClick={() => selectRequest(request)}
      >
        {request.id}
      </button>
    )
  }

  return (
    <>
      <PageHeader title="实时路由">
        {snapshot.data && (
          <span className="inline-flex items-center gap-1.5 text-xs text-muted-foreground">
            <span aria-hidden="true" className={cn('size-1.5 rounded-full', snapshot.isError ? 'bg-warning' : 'bg-success')} />
            {snapshot.isError ? '更新中断' : '实时'}
          </span>
        )}
      </PageHeader>
      <ErrorNotice error={snapshot.error} retry={() => void snapshot.refetch()} />
      <ErrorNotice error={catalog.error} retry={() => void catalog.refetch()} />
      {snapshot.isPending && <p role="status" className="py-8 text-center text-muted-foreground">正在读取实时路由</p>}
      {snapshot.data && (
        <>
          <section className="rounded-lg border bg-surface" aria-labelledby="live-flow-heading">
            <header className="flex min-h-14 flex-wrap items-center justify-between gap-3 border-b px-4 py-3">
              <h2 id="live-flow-heading" className="text-sm font-semibold">请求流向</h2>
              <div className="flex flex-wrap items-center gap-x-4 gap-y-2 text-xs">
                <p className="text-muted-foreground">
                  <strong className="tabular-nums text-foreground">{active.length}</strong> 进行中
                  <span className="mx-2" aria-hidden="true">·</span>
                  <strong className="tabular-nums text-foreground">{currentUpstreams}</strong> 个上游
                </p>
                <div className="flex items-center gap-2">
                  <Switch id="live-idle" size="sm" checked={showIdle} disabled={!catalog.data} onCheckedChange={(checked) => update({ idle: checked ? '1' : '' })} />
                  <Label htmlFor="live-idle" className="cursor-pointer text-xs">显示空闲节点</Label>
                </div>
              </div>
            </header>
            <LiveRoutingGraph
              graph={graph}
              catalogReady={!!catalog.data}
              filter={filter}
              focus={focus}
              selectedID={selected?.completed_at ? '' : (selected?.id ?? '')}
              onSelect={selectNode}
            />
          </section>

          <section className="section" aria-labelledby="live-requests-heading">
            <header className="mb-2 flex min-h-8 flex-wrap items-center justify-between gap-2">
              <h2 id="live-requests-heading" className="text-sm font-semibold">
                进行中的请求 <span className="ml-1 text-xs font-normal tabular-nums text-muted-foreground">{shown.length}</span>
              </h2>
              {filter && (
                <div className="flex items-center gap-2 text-xs text-muted-foreground">
                  <span className="tabular-nums">{shown.length} / {active.length} 条</span>
                  <Button type="button" variant="outline" size="sm" aria-label="清除节点筛选" onClick={() => update({ node: '' })}>
                    <span className="max-w-44 truncate">{graph.selectedNode?.name ?? '节点筛选'}</span>
                    <X aria-hidden="true" />
                  </Button>
                </div>
              )}
            </header>
            <div className="rounded-lg border bg-surface">
              <div className="max-h-80 overflow-auto rounded-t-lg" tabIndex={0} role="region" aria-labelledby="live-requests-heading" ref={tableScroll}>
                <table className="w-full min-w-168 border-separate border-spacing-0 text-[13px]">
                  <TableHeader className="[&_th]:border-b">
                    <TableRow>
                      <TableHead scope="col" className="sticky left-0 z-10 bg-muted">请求 ID</TableHead>
                      <TableHead scope="col">访问密钥</TableHead>
                      <TableHead scope="col">虚拟模型</TableHead>
                      <TableHead scope="col">实际上游</TableHead>
                      <TableHead scope="col">阶段</TableHead>
                      <TableHead scope="col" className="text-right">耗时</TableHead>
                      <TableHead scope="col" className="text-right">尝试</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody className="[&_tr:not(:last-child)>*]:border-b">
                    {shown.map((request) => (
                      <TableRow key={request.id} data-state={selected?.id === request.id ? 'selected' : undefined} className="group">
                        <th scope="row" className="sticky left-0 z-1 bg-surface px-3 group-data-[state=selected]:bg-muted">{requestButton(request)}</th>
                        <TableCell>
                          <span className="block max-w-36 truncate" title={accessKeyName(request)}>{accessKeyName(request)}</span>
                          <span className="block text-[11px] text-muted-foreground">{protocolName(request.inbound_protocol)}</span>
                        </TableCell>
                        <TableCell><code className="block max-w-40 truncate text-xs" title={virtualModelName(request)}>{virtualModelName(request)}</code></TableCell>
                        <TableCell>
                          {request.upstream ? (
                            <>
                              <span className="block max-w-52 truncate" title={request.upstream.provider_name}>{request.upstream.provider_name}</span>
                              <code className="block max-w-52 truncate text-[11px] text-muted-foreground" title={request.upstream.model}>{request.upstream.model}</code>
                            </>
                          ) : '—'}
                        </TableCell>
                        <TableCell><RequestStage request={request} /></TableCell>
                        <TableCell className="text-right font-mono text-xs tabular-nums">{elapsed(request, snapshot.data.now)}</TableCell>
                        <TableCell className="text-right font-mono text-xs tabular-nums">{request.attempts}</TableCell>
                      </TableRow>
                    ))}
                    {!shown.length && <TableRow><TableCell colSpan={7} className="text-muted-foreground">{filter ? '此节点暂无进行中的请求' : '暂无进行中的请求'}</TableCell></TableRow>}
                  </TableBody>
                </table>
              </div>
              <div id="live-route-trail" ref={trail}>
                {selected && <RoutingTrail request={selected} />}
              </div>
            </div>
          </section>

          <section className="section" aria-labelledby="live-recent-heading">
            <h2 id="live-recent-heading" className="section-heading">最近结束</h2>
            <ul className="divide-y rounded-lg border bg-surface">
              {recent.map((request) => (
                <li key={request.id} className="grid min-h-10 grid-cols-[minmax(3.5rem,0.6fr)_minmax(0,2fr)_auto_auto] items-center gap-2 px-3 py-2 text-xs sm:grid-cols-[9rem_minmax(0,1fr)_6rem_5rem]">
                  {requestButton(request)}
                  <span className="truncate" title={`${virtualModelName(request)} · ${request.upstream?.provider_name ?? '未调用'}`}>
                    <code>{virtualModelName(request)}</code>
                    {request.upstream && <><span aria-hidden="true" className="mx-2 text-muted-foreground">→</span>{request.upstream.provider_name}</>}
                  </span>
                  <RequestStage request={request} />
                  <span className="text-right font-mono tabular-nums text-muted-foreground">{elapsed(request, request.completed_at!)}</span>
                </li>
              ))}
              {!recent.length && <li className="px-3 py-3 text-muted-foreground">暂无最近结束的请求</li>}
            </ul>
          </section>
        </>
      )}
    </>
  )
}

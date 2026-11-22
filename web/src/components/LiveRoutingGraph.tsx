import { useLayoutEffect, useRef } from 'react'
import { KeyRound, Layers3, Server } from 'lucide-react'
import type { LiveRoutingRequest, LiveRoutingStage } from '@/api'
import { useTheme } from '@/hooks/use-theme'
import { buildRoutingGraph, liveStagePresentation, type GraphEdge } from '@/lib/live-routing'
import { cn } from '@/lib/utils'

const icons = { access_key: KeyRound, virtual_model: Layers3, provider: Server }

function sharedStage(requests: LiveRoutingRequest[]): LiveRoutingStage | 'mixed' {
  const stage = requests[0].stage
  return requests.every((request) => request.stage === stage) ? stage : 'mixed'
}

function drawRouting(
  element: HTMLDivElement,
  canvas: HTMLCanvasElement,
  edges: GraphEdge[],
  dense: boolean,
  focus: ReadonlySet<string> | null,
  selectedID: string,
) {
  const context = canvas.getContext('2d')
  if (!context) return
  const bounds = element.getBoundingClientRect()
  const scale = window.devicePixelRatio || 1
  canvas.width = Math.round(bounds.width * scale)
  canvas.height = Math.round(bounds.height * scale)
  context.setTransform(scale, 0, 0, scale, 0, 0)
  context.lineCap = 'round'

  const styles = getComputedStyle(element)
  const color = (tone: string) => styles.getPropertyValue(`--${tone}`).trim()
  const boxes = new Map([...element.querySelectorAll<HTMLElement>('[data-route-node]')]
    .map((node) => [node.dataset.routeNode!, node.getBoundingClientRect()]))
  const starts = new Map<string, GraphEdge[]>()
  const ends = new Map<string, GraphEdge[]>()
  for (const edge of edges) {
    if (!starts.has(edge.from)) starts.set(edge.from, [])
    if (!ends.has(edge.to)) ends.set(edge.to, [])
    starts.get(edge.from)!.push(edge)
    ends.get(edge.to)!.push(edge)
  }
  for (const group of starts.values()) group.sort((a, b) => boxes.get(a.to)!.top - boxes.get(b.to)!.top)
  for (const group of ends.values()) group.sort((a, b) => boxes.get(a.from)!.top - boxes.get(b.from)!.top)

  function point(edge: GraphEdge, side: 'from' | 'to') {
    const box = boxes.get(edge[side])!
    const group = (side === 'from' ? starts : ends).get(edge[side])!
    const step = Math.min(9, (box.height - 10) / group.length)
    return {
      x: (side === 'from' ? box.right : box.left) - bounds.left,
      y: box.top - bounds.top + box.height / 2 + (group.indexOf(edge) - (group.length - 1) / 2) * step,
    }
  }

  function draw(edge: GraphEdge, requests: LiveRoutingRequest[], opacity: number, toneOverride?: string, highlight = false) {
    if (!context) return
    const from = point(edge, 'from')
    const to = point(edge, 'to')
    const stage = sharedStage(requests)
    const presentation = stage === 'mixed' ? { tone: 'muted-foreground', dash: [] } : liveStagePresentation[stage]
    const center = (from.x + to.x) / 2
    const stroke = color(toneOverride ?? presentation.tone)
    context.globalAlpha = opacity
    context.strokeStyle = stroke
    context.fillStyle = stroke
    context.lineWidth = highlight ? 2.7 : Math.min(6, 1.5 + 1.5 * Math.log2(requests.length))
    context.setLineDash(toneOverride ? [] : presentation.dash)
    context.beginPath()
    context.moveTo(from.x, from.y)
    context.bezierCurveTo(center, from.y, center, to.y, to.x - 6, to.y)
    context.stroke()
    context.beginPath()
    context.moveTo(to.x - 1, to.y)
    context.lineTo(to.x - 7, to.y - 3)
    context.lineTo(to.x - 7, to.y + 3)
    context.fill()
  }

  for (const edge of edges) {
    const opacity = focus || (dense && selectedID) ? .18 : dense ? .48 : .78
    draw(edge, edge.requests, opacity, dense ? 'muted-foreground' : undefined)
  }
  if (focus) {
    for (const edge of edges) {
      const matching = edge.requests.filter((request) => focus.has(request.id))
      if (matching.length) draw(edge, matching, .95)
    }
  }
  if (selectedID) {
    for (const edge of edges) {
      const picked = edge.requests.find((request) => request.id === selectedID)
      if (picked) draw(edge, [picked], 1, undefined, true)
    }
  }
}

export default function LiveRoutingGraph({
  graph, catalogReady, filter, focus, selectedID, onSelect,
}: {
  graph: ReturnType<typeof buildRoutingGraph>
  catalogReady: boolean
  filter: string
  focus: ReadonlySet<string> | null
  selectedID: string
  onSelect: (key: string) => void
}) {
  const element = useRef<HTMLDivElement>(null)
  const canvas = useRef<HTMLCanvasElement>(null)
  const { dark } = useTheme()
  const compact = graph.columns.some((column) => column.nodes.length >= 8)
  const dense = graph.edges.length >= 23 || graph.columns.some((column) => column.live >= 8)
  const overview = dense && !focus && !selectedID
  const shownEdges = graph.edges.map((edge) => focus ? edge.requests.filter((request) => focus.has(request.id)) : edge.requests)
  const legendRequests = dense && !focus && selectedID
    ? shownEdges.flat().filter((request) => request.id === selectedID)
    : shownEdges.flat()
  const visibleStages = new Set(legendRequests.map((request) => request.stage))
  const selectedStage = graph.edges.flatMap((edge) => edge.requests).find((request) => request.id === selectedID)?.stage
  if (selectedStage) visibleStages.add(selectedStage)
  const mixed = !overview && !(dense && !focus && selectedID) &&
    shownEdges.some((requests) => requests.length > 1 && sharedStage(requests) === 'mixed')
  const weighted = graph.edges.some((edge) => edge.requests.length > 1)

  useLayoutEffect(() => {
    const diagram = element.current
    const surface = canvas.current
    if (!diagram || !surface) return
    let frame = 0
    const scheduleDraw = () => {
      cancelAnimationFrame(frame)
      frame = requestAnimationFrame(() => drawRouting(diagram, surface, graph.edges, dense, focus, selectedID))
    }
    const observer = new ResizeObserver(scheduleDraw)
    observer.observe(diagram)
    window.addEventListener('resize', scheduleDraw)
    scheduleDraw()
    return () => {
      cancelAnimationFrame(frame)
      observer.disconnect()
      window.removeEventListener('resize', scheduleDraw)
    }
  }, [graph.edges, dense, focus, selectedID, dark])

  return (
    <>
      <div className="overflow-x-auto bg-background" tabIndex={0} role="region" aria-label="请求流向图">
        <div ref={element} className={cn(
          'relative isolate grid min-w-[42rem] grid-cols-[minmax(9rem,1fr)_minmax(8rem,.92fr)_minmax(13rem,1.22fr)] gap-x-[clamp(2.25rem,7vw,7rem)] px-4 sm:px-6',
          compact ? 'py-3.5' : 'py-5',
        )}>
          <canvas ref={canvas} aria-hidden="true" className="pointer-events-none absolute inset-0 -z-10 size-full" />
          {graph.columns.map((column) => {
            const Icon = icons[column.kind]
            return (
              <section key={column.kind} className="grid min-w-0 grid-rows-[auto_1fr] gap-3">
                <h3 className="flex flex-wrap items-baseline justify-between gap-x-2 text-xs font-medium text-muted-foreground">
                  {column.title}
                  <span className="text-[11px] font-normal tabular-nums">
                    {column.live} 活跃{catalogReady && ` / ${column.total} 启用`}
                  </span>
                </h3>
                <div className={cn('flex flex-col', compact ? 'gap-1.5' : 'justify-center gap-3')}>
                  {column.nodes.map((node) => {
                    const count = filter ? node.focused : node.live
                    const labels = [...node.sublabels]
                    const detail = !node.live ? '空闲' : labels.length > 1 && node.kind === 'provider'
                      ? `${labels.length} 个真实模型`
                      : labels.join(' / ')
                    return (
                      <button
                        key={node.key}
                        type="button"
                        data-route-node={node.key}
                        title={[node.name, detail].filter(Boolean).join(' · ')}
                        aria-label={`${node.name}，${detail ? `${detail}，` : ''}${count} 个请求`}
                        aria-pressed={filter === node.key}
                        onClick={() => onSelect(node.key)}
                        className={cn(
                          'grid w-full min-w-0 grid-cols-[auto_minmax(0,1fr)_auto] items-center gap-2.5 rounded-md border bg-surface px-2.5 text-left hover:bg-muted',
                          compact ? 'min-h-9 py-1' : 'min-h-14 py-2',
                          !node.live && 'border-dashed bg-background',
                          !count && 'text-muted-foreground',
                          filter === node.key && 'border-brand bg-muted',
                        )}
                      >
                        <span className={cn('grid place-items-center rounded border bg-muted text-muted-foreground', compact ? 'size-5.5' : 'size-7')}>
                          <Icon className={compact ? 'size-3' : 'size-3.5'} aria-hidden="true" />
                        </span>
                        <span className="truncate">
                          <span className={cn('font-medium', compact ? 'inline' : 'block truncate')}>{node.name}</span>
                          {detail && <span className={cn('text-[11px] text-muted-foreground', compact ? 'ml-2' : 'block truncate')}>{detail}</span>}
                        </span>
                        <span className="min-w-5 rounded bg-muted px-1 text-center font-mono text-[11px] tabular-nums">{count}</span>
                      </button>
                    )
                  })}
                </div>
              </section>
            )
          })}
          {!graph.edges.length && !graph.columns.some((column) => column.nodes.length) && (
            <p className="col-span-3 py-8 text-center text-sm text-muted-foreground">暂无进行中的请求</p>
          )}
        </div>
      </div>
      {graph.edges.length > 0 && (
        <ul className="flex min-h-11 flex-wrap items-center gap-x-4 gap-y-2 border-t px-4 py-2 text-xs text-muted-foreground" aria-label="流向图图例">
          {!overview && [...visibleStages].map((stage) => {
            const { dash, tone, label } = liveStagePresentation[stage]
            return (
              <li key={stage} className="inline-flex items-center gap-1.5">
                <span aria-hidden="true" className="w-4.5 border-t-2" style={{ borderColor: `var(--${tone})`, borderTopStyle: dash.length ? (dash[0] > 2 ? 'dashed' : 'dotted') : 'solid' }} />
                {stage === 'waiting' ? '等待上游' : label}
              </li>
            )
          })}
          {mixed && <li className="inline-flex items-center gap-1.5"><span aria-hidden="true" className="w-4.5 border-t-2 border-muted-foreground" />混合阶段</li>}
          {weighted && <li>线宽对应请求数</li>}
          {overview && <li>点选节点查看路径状态</li>}
        </ul>
      )}
    </>
  )
}

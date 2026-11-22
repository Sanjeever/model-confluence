import type { LiveRoutingCatalogNode, LiveRoutingNodeKind, LiveRoutingRequest, LiveRoutingStage } from '@/api'
import { protocolName } from '@/components/Status'

export const routingColumns = [
  { kind: 'access_key', title: '访问密钥' },
  { kind: 'virtual_model', title: '虚拟模型' },
  { kind: 'provider', title: '实际上游' },
] as const

export const liveStagePresentation = {
  routing: { label: '选择路由', tone: 'muted-foreground', dash: [2, 5] },
  waiting: { label: '等待首内容', tone: 'warning', dash: [6, 5] },
  receiving: { label: '接收上游响应', tone: 'info', dash: [6, 3, 1, 3] },
  aggregating: { label: '聚合上游响应', tone: 'info', dash: [1, 5] },
  streaming: { label: '输出中', tone: 'success', dash: [] },
  retrying: { label: '准备重试', tone: 'warning', dash: [2, 5] },
  completed: { label: '已完成', tone: 'success', dash: [] },
  failed: { label: '失败', tone: 'destructive', dash: [] },
  cancelled: { label: '已取消', tone: 'muted-foreground', dash: [] },
} satisfies Record<LiveRoutingStage, { label: string; tone: string; dash: number[] }>

type NodeIdentity = { key: string; kind: LiveRoutingNodeKind; name: string }
export type GraphNode = NodeIdentity & {
  live: number
  focused: number
  sublabels: Set<string>
}

export type GraphEdge = {
  from: string
  to: string
  requests: LiveRoutingRequest[]
}

export function virtualModelName(request: LiveRoutingRequest) {
  return request.virtual_model || '未识别模型'
}

export function accessKeyName(request: LiveRoutingRequest) {
  return request.access_key_name || '管理后台'
}

export function stageLabel(request: LiveRoutingRequest) {
  if (request.stage === 'waiting' && !request.stream) return '等待响应'
  return liveStagePresentation[request.stage].label
}

function requestNodes(request: LiveRoutingRequest): NodeIdentity[] {
  const source = request.access_key_id == null ? `admin:${accessKeyName(request)}` : request.access_key_id
  const path: NodeIdentity[] = [
    { key: `access_key:${source}`, kind: 'access_key', name: accessKeyName(request) },
    { key: `virtual_model:${request.virtual_model}`, kind: 'virtual_model', name: virtualModelName(request) },
  ]
  if (request.upstream) {
    path.push({
      key: `provider:${request.upstream.provider_id}`,
      kind: 'provider',
      name: request.upstream.provider_name,
    })
  }
  return path
}

export function requestMatchesNode(request: LiveRoutingRequest, key: string) {
  return requestNodes(request).some((node) => node.key === key)
}

export function buildRoutingGraph(
  requests: LiveRoutingRequest[],
  catalog: LiveRoutingCatalogNode[],
  showIdle: boolean,
  filter: string,
) {
  const nodes = new Map<string, GraphNode>()
  const edges = new Map<string, GraphEdge>()
  const totals = { access_key: 0, virtual_model: 0, provider: 0 }

  for (const { id, kind, name } of catalog) {
    const key = `${kind}:${kind === 'virtual_model' ? name : id}`
    totals[kind]++
    nodes.set(key, { key, kind, name, live: 0, focused: 0, sublabels: new Set() })
  }
  for (const request of requests) {
    const path = requestNodes(request)
    const matches = !filter || path.some((node) => node.key === filter)
    for (const identity of path) {
      let node = nodes.get(identity.key)
      if (!node) {
        node = { ...identity, live: 0, focused: 0, sublabels: new Set() }
        nodes.set(identity.key, node)
      }
      node.live++
      if (matches) node.focused++
      if (identity.kind === 'access_key' && request.inbound_protocol) node.sublabels.add(protocolName(request.inbound_protocol))
      if (identity.kind === 'provider' && request.upstream?.model && matches) node.sublabels.add(request.upstream.model)
    }
    for (let index = 1; index < path.length; index++) {
      const from = path[index - 1].key
      const to = path[index].key
      const key = JSON.stringify([from, to])
      const edge = edges.get(key)
      if (edge) edge.requests.push(request)
      else edges.set(key, { from, to, requests: [request] })
    }
  }

  const sorted = [...nodes.values()].sort((a, b) => a.name.localeCompare(b.name, 'zh-CN') || a.key.localeCompare(b.key))
  return {
    columns: routingColumns.map(({ kind, title }) => {
      const group = sorted.filter((node) => node.kind === kind)
      return {
        kind, title,
        live: group.filter((node) => node.live > 0).length,
        total: totals[kind],
        nodes: group.filter((node) => node.live > 0 || showIdle || node.key === filter),
      }
    }),
    selectedNode: nodes.get(filter),
    edges: [...edges.values()],
  }
}

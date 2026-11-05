import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { api } from '../api'
import { ErrorNotice, displayDate } from './console'

type Observation = { model: string; request_id: string; observed_at: string; headers: Record<string, string[]> }
export default function UpstreamLimits({ keyID }: { keyID: number }) {
  const limits = useQuery({ queryKey: ['upstream-limits', keyID], queryFn: () => api<Observation[]>(`/api/admin/upstream-keys/${keyID}/limits`), refetchInterval: 15000 })
  return <div className="space-y-3 border-t pt-3"><h3 className="text-xs font-medium">上游额度与限流信号</h3><ErrorNotice error={limits.error} />{limits.isPending ? <p className="text-xs text-muted-foreground">读取中…</p> : limits.data?.length ? limits.data.map((o) => <div key={o.model} className="space-y-2 text-xs"><div className="flex flex-wrap gap-3"><code>{o.model}</code><span className="text-muted-foreground">{displayDate(o.observed_at)}</span><Link className="text-link" to={`/requests/${o.request_id}`}>来源请求</Link></div><dl className="grid gap-1">{Object.entries(o.headers).map(([name, value]) => <div key={name} className="flex flex-wrap gap-x-3"><dt className="font-mono text-muted-foreground">{name}</dt><dd className="break-all font-mono">{value.join(', ')}</dd></div>)}</dl></div>) : <p className="text-xs text-muted-foreground">未知，上游尚未返回额度或限流信息。</p>}</div>
}

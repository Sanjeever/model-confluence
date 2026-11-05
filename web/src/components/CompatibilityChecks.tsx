import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { api, type VirtualModel } from '../api'
import { Button } from './ui/button'
import { NativeSelect } from './ui/native-select'
import { Label } from './ui/label'
import { Switch } from './ui/switch'
import { ErrorNotice, displayDate } from './console'

type Check = { passed: boolean; message: string; request_ids: string[]; checked_at: string; model: string; candidate_id: number; protocol: string; scenario: string; stream: boolean; provider: string; upstream_model: string; actual_protocol: string }

export default function CompatibilityChecks({ model }: { model: VirtualModel }) {
  const client = useQueryClient()
  const [candidate, setCandidate] = useState('0')
  const [protocol, setProtocol] = useState('chat_completions')
  const [upstream, setUpstream] = useState('')
  const [scenario, setScenario] = useState('text')
  const [stream, setStream] = useState(false)
  const checks = useQuery({ queryKey: ['compatibility', model.id], queryFn: () => api<Check[]>(`/api/admin/models/${model.id}/checks`) })
  const run = useMutation({ mutationFn: () => api<Check>(`/api/admin/models/${model.id}/checks`, { method: 'POST', body: JSON.stringify({ candidate_id: Number(candidate), protocol, upstream_protocol: upstream, scenario, stream }) }), onSuccess: () => { client.invalidateQueries({ queryKey: ['compatibility', model.id] }); client.invalidateQueries({ queryKey: ['requests'] }) } })
  return <details className="border-t pt-3"><summary className="w-fit cursor-pointer text-sm font-medium">兼容性验证</summary><div className="mt-4 space-y-3">
    <fieldset disabled={run.isPending} className="flex flex-wrap gap-3">
      <NativeSelect aria-label="测试候选" value={candidate} onChange={(e) => setCandidate(e.target.value)}><option value="0">按虚拟模型路由</option>{model.candidates.map((c) => <option key={c.id} value={c.id}>{c.position + 1}. {c.provider_name} / {c.upstream_model}</option>)}</NativeSelect>
      <NativeSelect aria-label="测试入站协议" value={protocol} onChange={(e) => setProtocol(e.target.value)}>{['chat_completions', 'responses', 'messages'].map((p) => <option key={p}>{p}</option>)}</NativeSelect>
      <NativeSelect aria-label="测试上游协议" value={upstream} onChange={(e) => setUpstream(e.target.value)}><option value="">按协议优先级</option>{['chat_completions', 'responses', 'messages', 'antigravity'].map((p) => <option key={p}>{p}</option>)}</NativeSelect>
      <NativeSelect aria-label="测试场景" value={scenario} onChange={(e) => setScenario(e.target.value)}><option value="text">文本</option><option value="tool">工具连续两轮</option><option value="parallel">并行工具连续两轮</option></NativeSelect>
      <Label className="flex items-center gap-2"><Switch checked={stream} onCheckedChange={setStream} />SSE</Label>
    </fieldset>
    <div className="flex items-center gap-3"><Button size="sm" disabled={run.isPending} onClick={() => run.mutate()}>{run.isPending ? '验证中…' : '发送验证请求'}</Button><span className="text-xs text-muted-foreground">真实调用上游，工具场景发送三次请求，最长两分钟。</span></div>
    <ErrorNotice error={run.error ?? checks.error} />
    {checks.data?.map((c, index) => <div key={`${c.checked_at}:${index}`} className="space-y-1 border-t pt-3 text-xs"><div className="flex flex-wrap justify-between gap-2"><span className={c.passed ? 'text-foreground' : 'text-destructive'}>{c.passed ? '通过' : c.message}</span><span className="text-muted-foreground">{displayDate(c.checked_at)}</span></div><div className="break-all text-muted-foreground">{c.protocol} · {c.stream ? 'SSE' : 'JSON'} · {c.scenario} · {c.provider || '—'} / {c.upstream_model || '—'} · {c.actual_protocol || '—'}{c.candidate_id ? ` · 指定候选 ${c.candidate_id}` : ' · 虚拟模型路由'}</div><div className="flex flex-wrap gap-3">{c.request_ids.map((id) => <Link key={id} className="text-link font-mono" to={`/requests/${id}`}>{id}</Link>)}</div></div>)}
  </div></details>
}

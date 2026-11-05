import { useState } from 'react'
import { useMutation } from '@tanstack/react-query'
import { api, type RouteDecision } from '../api'
import { Button } from './ui/button'
import { Switch } from './ui/switch'
import { Label } from './ui/label'
import { NativeSelect } from './ui/native-select'
import { ErrorNotice, displayDate } from './console'

export function RouteDecisions({ decisions }: { decisions: RouteDecision[] }) {
  return <div className="divide-y rounded-md border">{decisions.length ? decisions.map((d, index) => <div key={index} className="grid gap-1 p-3 text-xs sm:grid-cols-2">
    <span className="font-medium">{d.provider || '模型'} · {d.model || '—'}</span><span className={d.reason === '可用' ? 'text-muted-foreground' : 'text-warning'}>{d.reason}</span>
    <span className="font-mono text-muted-foreground">{d.protocol || '—'}{d.key_id ? ` · ${d.key_name || `密钥 ${d.key_id}`}` : ''}</span><span className="text-muted-foreground">{d.recover_at ? displayDate(d.recover_at) : ''}</span>
  </div>) : <p className="p-3 text-xs text-muted-foreground">没有配置候选</p>}</div>
}

export default function RouteDiagnostics({ model }: { model: string }) {
  const [protocol, setProtocol] = useState('chat_completions')
  const [effort, setEffort] = useState('')
  const [flags, setFlags] = useState({ stream: false, tools: false, parallel_tools: false })
  const explain = useMutation({ mutationFn: () => api<RouteDecision[]>('/api/admin/routes/explain', { method: 'POST', body: JSON.stringify({ virtual_model: model, protocol, effort, ...flags, tools: flags.tools || flags.parallel_tools }) }) })
  return <div className="space-y-3">
    <div className="flex flex-wrap items-center gap-3">
      <NativeSelect aria-label="入站协议" value={protocol} onChange={(e) => { setProtocol(e.target.value); explain.reset() }}>{['chat_completions', 'responses', 'messages'].map((p) => <option key={p}>{p}</option>)}</NativeSelect>
      <NativeSelect aria-label="推理强度" value={effort} onChange={(e) => { setEffort(e.target.value); explain.reset() }}><option value="">无推理强度要求</option>{['minimal', 'low', 'medium', 'high', 'xhigh', 'max'].map((p) => <option key={p}>{p}</option>)}</NativeSelect>
      {([['stream', '流式'], ['tools', '工具'], ['parallel_tools', '并行工具']] as const).map(([key, text]) => <Label key={key} className="flex items-center gap-2"><Switch checked={flags[key]} onCheckedChange={(checked) => { setFlags({ ...flags, [key]: checked }); explain.reset() }} />{text}</Label>)}
      <Button size="sm" variant="outline" disabled={explain.isPending} onClick={() => explain.mutate()}>预演路由</Button>
    </div>
    <ErrorNotice error={explain.error} />
    {explain.data && <RouteDecisions decisions={explain.data} />}
  </div>
}

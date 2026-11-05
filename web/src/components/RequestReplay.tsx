import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { api, type RequestDetail } from '../api'
import { Button } from './ui/button'
import { Textarea } from './ui/textarea'
import { Label } from './ui/label'
import { ErrorNotice } from './console'

export default function RequestReplay({ detail }: { detail: RequestDetail }) {
  const client = useQueryClient()
  const [body, setBody] = useState(() => { try { return JSON.stringify(JSON.parse(detail.request_body), null, 2) } catch { return detail.request_body } })
  const run = useMutation({ mutationFn: () => api<{ request_id: string; status: number }>(`/api/admin/requests/${detail.id}/replay`, { method: 'POST', body: JSON.stringify({ body: JSON.parse(body) }) }), onSuccess: () => client.invalidateQueries({ queryKey: ['requests'] }) })
  return <div className="space-y-4">
    <p className="text-xs text-muted-foreground">通过当前配置和凭据重新发送，沿用原访问密钥身份。将产生新的上游调用和请求记录，最长两分钟。</p>
    <div className="field"><Label htmlFor="replay-body">{detail.inbound_protocol} 请求正文</Label><Textarea id="replay-body" rows={20} className="font-mono text-xs" value={body} disabled={run.isPending} onChange={(e) => { setBody(e.target.value); run.reset() }} /></div>
    <ErrorNotice error={run.error} />
    <Button disabled={detail.payload_pruned || detail.status === 'in_progress' || run.isPending || !body.trim()} onClick={() => run.mutate()}>{run.isPending ? '发送中…' : '发送调试请求'}</Button>
    {run.data && <div className="text-xs">HTTP {run.data.status} · <Link className="text-link font-mono" to={`/requests/${run.data.request_id}`}>{run.data.request_id}</Link></div>}
  </div>
}

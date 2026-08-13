import { useState } from 'react'
import { Button, Collapse, Descriptions, Drawer, Empty, Space, Tabs, Tag } from 'antd'
import type { RequestDetail } from '../api'
import JsonPayload from './JsonPayload'
import StreamResponseViewer from './StreamResponseViewer'

export default function RequestDrawer({ detail, loading, open, onClose }: { detail?: RequestDetail; loading: boolean; open: boolean; onClose: () => void }) {
  const [reveal, setReveal] = useState(false)
  return (
    <Drawer rootClassName="mc-request-drawer" title="请求轨迹" size="min(920px, 100vw)" open={open} onClose={() => { setReveal(false); onClose() }} loading={loading} extra={<Button size="small" onClick={() => setReveal((value) => !value)}>{reveal ? '遮罩凭据' : '显示原文'}</Button>}>
      {detail && <>
        <Descriptions size="small" column={{ xs: 1, sm: 2, lg: 3 }} className="mb-6" items={[
          { key: 'id', label: '请求 ID', children: <code className="break-all">{detail.id}</code> },
          { key: 'status', label: '状态', children: <Tag color={detail.status === 'completed' ? 'success' : 'error'}>{statusName(detail.status)}</Tag> },
          { key: 'model', label: '虚拟模型', children: <code>{detail.virtual_model}</code> },
          { key: 'protocol', label: '入站协议', children: protocolName(detail.inbound_protocol) },
          { key: 'upstream_protocol', label: '上游协议', children: protocolName(detail.upstream_protocol) },
          { key: 'stream', label: '请求方式', children: detail.stream ? <Tag color="processing">流式</Tag> : <Tag>非流式</Tag> },
          { key: 'key', label: '访问密钥', children: detail.access_key_name },
          { key: 'payload', label: '日志载荷', children: detail.payload_pruned ? <Tag>已按保留策略清理</Tag> : <Tag color="success">完整</Tag> },
          { key: 'latency', label: '总耗时', children: detail.total_ms == null ? '—' : `${detail.total_ms} ms` },
          { key: 'time', label: '请求时间', children: new Date(detail.created_at).toLocaleString() },
          { key: 'input', label: '输入 Token', children: detail.input_tokens ?? '—' },
          { key: 'cache', label: '缓存读取', children: detail.cache_read_tokens ?? '—' },
          { key: 'output', label: '输出 Token', children: detail.output_tokens ?? '—' },
        ]} />
        <Tabs items={[
          { key: 'inbound', label: detail.payload_pruned ? '入站请求（载荷已清理）' : '入站请求', children: <PayloadPair headers={detail.request_headers} body={detail.request_body} reveal={reveal} pruned={detail.payload_pruned} /> },
          { key: 'response', label: detail.payload_pruned ? '客户端响应（载荷已清理）' : '客户端响应', children: <PayloadPair headerLabel="响应头" headers={detail.response_headers} body={detail.response_body} reveal={reveal} stream={detail.stream} summary={detail.response_summary} pruned={detail.payload_pruned} /> },
          { key: 'attempts', label: `上游尝试 ${detail.attempts.length}`, children: detail.attempts.length ? <Collapse items={detail.attempts.map((attempt) => ({
            key: attempt.id,
            label: <Space wrap><span className="font-mono text-[#d7783d]">#{attempt.position + 1}</span><strong>{attempt.provider_name}</strong><code className="break-all">{attempt.upstream_model}</code><Tag>{protocolName(attempt.upstream_protocol)}</Tag><Tag color={attempt.status === 'completed' ? 'success' : 'error'}>{statusName(attempt.status)}</Tag>{attempt.payload_pruned && <Tag>载荷已清理</Tag>}</Space>,
            children: <div className="space-y-6"><Descriptions size="small" column={{ xs: 1, sm: 2, lg: 3 }} items={[
              { key: 'endpoint', label: '上游端点', span: 3, children: <code className="break-all">{attempt.upstream_endpoint}</code> },
              { key: 'key', label: '上游密钥', children: attempt.upstream_key_name || '未命名' },
              { key: 'status', label: '响应状态', children: attempt.response_status ?? '—' },
              { key: 'latency', label: '总耗时', children: attempt.total_ms == null ? '—' : `${attempt.total_ms} ms` },
            ]} /><PayloadPair title="发送到上游" headers={attempt.request_headers} body={attempt.request_body} reveal={reveal} pruned={attempt.payload_pruned} /><PayloadPair title="上游响应" headerLabel="响应头" headers={attempt.response_headers} body={attempt.response_body} reveal={reveal} stream={detail.stream} summary={attempt.response_summary} pruned={attempt.payload_pruned} />{attempt.raw_usage_json && <div><div className="mb-3 text-sm font-medium text-[#7c8d86]">原始用量</div><JsonPayload value={attempt.raw_usage_json} /></div>}</div>,
          }))} /> : <Empty className="py-16" description="该请求在调用上游前失败，没有产生上游尝试" /> },
        ]} />
      </>}
    </Drawer>
  )
}

function PayloadPair({ title, headerLabel = '请求头', headers, body, reveal, stream = false, summary, pruned = false }: { title?: string; headerLabel?: string; headers: string; body: string; reveal: boolean; stream?: boolean; summary?: RequestDetail['response_summary']; pruned?: boolean }) {
  if (pruned) return <div>{title && <div className="mc-eyebrow mb-3 text-[#7c8d86]">{title}</div>}<Empty className="py-8" description="载荷已按日志保留策略清理，仅保留结构化元数据" /></div>
  return <div>
    {title && <div className="mc-eyebrow mb-3 text-[#7c8d86]">{title}</div>}
    <Collapse size="small" className="mb-5" items={[{ key: 'headers', label: headerLabel, children: <JsonPayload value={reveal ? headers : maskHeaders(headers)} /> }]} />
    <div className="mb-2 text-xs font-medium text-[#7c8d86]">正文</div>
    {stream ? <StreamResponseViewer body={body} summary={summary} /> : <JsonPayload value={body} />}
  </div>
}

function maskHeaders(value: string): string {
  if (!value) return ''
  try {
    const headers = JSON.parse(value) as Record<string, unknown>
    for (const name of Object.keys(headers)) {
      if (['authorization', 'x-api-key', 'cookie', 'set-cookie', 'api-key'].includes(name.toLowerCase())) headers[name] = ['••••••••']
    }
    return JSON.stringify(headers)
  } catch {
    return value
  }
}

export function protocolName(value: string): string {
  if (!value) return '未调用'
  return ({ chat_completions: 'Chat Completions', responses: 'Responses', messages: 'Messages' } as Record<string, string>)[value] ?? value
}

export function statusName(value: string): string {
  return ({ completed: '已完成', in_progress: '进行中', failed: '失败', cancelled: '已取消' } as Record<string, string>)[value] ?? value
}

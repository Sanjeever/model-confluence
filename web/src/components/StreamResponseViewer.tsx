import { useEffect, useState } from 'react'
import { Alert, App, Button, Collapse, Empty, Space, Tag, Tabs, Tooltip } from 'antd'
import { CopyOutlined } from '@ant-design/icons'
import { type StreamSummary, type StreamSummaryBlock } from '../api'
import JsonPayload from './JsonPayload'

export default function StreamResponseViewer({ body, summary }: { body: string; summary?: StreamSummary }) {
  const [mode, setMode] = useState<'aggregate' | 'raw'>(() => !summary || summary.parse_status === 'unavailable' ? 'raw' : 'aggregate')
  const { message } = App.useApp()
  const status = summaryStatus(summary)

  useEffect(() => {
    setMode(!summary || summary.parse_status === 'unavailable' ? 'raw' : 'aggregate')
  }, [body, summary?.parse_status])

  async function copy() {
    try {
      await navigator.clipboard.writeText(body)
      message.success('原文已复制')
    } catch {
      message.error('复制失败')
    }
  }

  return <div className="overflow-hidden rounded border border-black/10 bg-black/[.025] dark:border-white/10 dark:bg-white/[.03]">
    <div className="flex flex-wrap items-center justify-between gap-2 px-2 py-1.5 sm:px-3">
      <Space size={6} wrap>
        <Tag color="processing">流式 SSE</Tag>
        <Tag color={status.color}>{status.label}</Tag>
        {summary?.stop_reason && <Tag>结束：{summary.stop_reason}</Tag>}
      </Space>
      <Tooltip title="复制原文"><Button type="text" size="small" icon={<CopyOutlined />} aria-label="复制流式原文" disabled={!body} onClick={copy} /></Tooltip>
    </div>
    {summary?.warnings.length ? <Alert className="mx-2 mb-2 sm:mx-3" type="warning" showIcon message="聚合内容可能不完整" description={summary.warnings.join('；')} /> : null}
    <Tabs
      className="mc-stream-tabs"
      activeKey={mode}
      onChange={(value) => setMode(value as 'aggregate' | 'raw')}
      items={[
        { key: 'aggregate', label: '聚合内容', children: <AggregateContent summary={summary} /> },
        { key: 'raw', label: '原文', children: <RawContent body={body} /> },
      ]}
    />
  </div>
}

function AggregateContent({ summary }: { summary?: StreamSummary }) {
  if (!summary || summary.parse_status === 'unavailable') return <Empty className="py-12" description="无法聚合该流式响应，请查看原文" />
  if (!summary.blocks.length) return <Empty className="py-12" description="没有可展示的聚合内容" />
  return <div className="space-y-4 px-3 pb-4 sm:px-4">{summary.blocks.map((block, index) => <SummaryBlock key={`${block.type}-${block.index}-${index}`} block={block} />)}</div>
}

function SummaryBlock({ block }: { block: StreamSummaryBlock }) {
  if (block.type === 'tool_call') {
    return <section className="overflow-hidden rounded border border-[#d7783d]/30 bg-[#d7783d]/[.05]">
      <div className="flex flex-wrap items-center gap-2 border-b border-[#d7783d]/20 px-3 py-2 text-sm">
        <Tag color="orange" style={{ marginInlineEnd: 0 }}>工具调用</Tag>
        <code className="break-all font-medium">{block.name || '未命名工具'}</code>
        {block.call_id && <code className="break-all text-xs text-[#7c8d86]">{block.call_id}</code>}
        <Tag className="ml-auto" color={block.complete ? 'success' : 'warning'}>{block.complete ? '已完成' : '未完成'}</Tag>
      </div>
      <div className="p-3">
        {block.arguments ? block.arguments_valid ? <JsonPayload value={block.arguments} /> : <pre className="m-0 max-h-[360px] overflow-auto whitespace-pre-wrap break-all font-mono text-xs leading-5">{block.arguments}</pre> : <span className="text-sm text-[#7c8d86]">未返回参数</span>}
        {!block.arguments_valid && block.arguments && <div className="mt-2 text-xs text-[#b45d2f]">参数尚未组成完整 JSON，已保留原始增量内容</div>}
      </div>
    </section>
  }
  if (block.type === 'reasoning') {
    return <Collapse size="small" items={[{ key: `reasoning-${block.index}`, label: <Tag style={{ marginInlineEnd: 0 }}>推理 / 思考</Tag>, children: <pre className="m-0 max-h-[420px] overflow-auto whitespace-pre-wrap break-words font-sans text-sm leading-6">{block.content || '—'}</pre> }]} />
  }
  return <section className="rounded border border-[#4f9d7e]/25 bg-[#4f9d7e]/[.04] px-3 py-3">
    <div className="mb-2 flex items-center justify-between gap-2">
      <Tag color="success" style={{ marginInlineEnd: 0 }}>输出</Tag>
      {!block.content && <span className="text-xs text-[#7c8d86]">暂无内容</span>}
    </div>
    <pre className="m-0 max-h-[420px] overflow-auto whitespace-pre-wrap break-words font-sans text-sm leading-6">{block.content || '—'}</pre>
  </section>
}

function RawContent({ body }: { body: string }) {
  return <pre className="m-0 max-h-[520px] max-w-full overflow-x-hidden overflow-y-auto whitespace-pre-wrap break-all px-3 pb-4 font-mono text-xs leading-5 sm:px-4">{body || '—'}</pre>
}

function summaryStatus(summary?: StreamSummary): { color: 'success' | 'warning' | undefined; label: string } {
  if (!summary || summary.parse_status === 'unavailable') return { color: undefined, label: '无法聚合' }
  if (summary.parse_status === 'partial') return { color: 'warning', label: summary.completed ? '部分聚合' : '未完成' }
  return { color: 'success', label: '已完成' }
}

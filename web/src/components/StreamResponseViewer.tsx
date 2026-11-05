import { useEffect, useState } from 'react'
import { ChevronDown, Wrench } from 'lucide-react'
import type { StreamSummary, StreamSummaryBlock } from '../api'
import JsonPayload from './JsonPayload'
import { CopyButton, EmptyState } from './console'
import { Tabs, TabsContent, TabsList, TabsTrigger } from './ui/tabs'
import { Status } from './Status'

export default function StreamResponseViewer({
  body,
  summary,
}: {
  body: string
  summary?: StreamSummary
}) {
  const [mode, setMode] = useState(() =>
    !summary || summary.parse_status === 'unavailable' ? 'raw' : 'aggregate',
  )
  useEffect(() => {
    setMode(
      !summary || summary.parse_status === 'unavailable' ? 'raw' : 'aggregate',
    )
  }, [body, summary?.parse_status])
  const partial = summary?.parse_status === 'partial'
  return (
    <div className="overflow-hidden rounded-md border bg-background">
      <div className="flex flex-wrap items-center justify-between gap-2 border-b px-3 py-2">
        <div className="flex items-center gap-3 text-xs">
          <span className="text-muted-foreground">SSE</span>
          <Status
            value={
              !summary || summary.parse_status === 'unavailable'
                ? 'disabled'
                : partial
                  ? 'rate_limited'
                  : 'completed'
            }
            label={
              !summary || summary.parse_status === 'unavailable'
                ? '无法聚合'
                : partial
                  ? summary.completed
                    ? '部分聚合'
                    : '未完成'
                  : '已完成'
            }
          />
          {summary?.stop_reason && (
            <span className="text-muted-foreground">{summary.stop_reason}</span>
          )}
        </div>
        <CopyButton value={body} label="复制流式原文" />
      </div>
      {!!summary?.warnings.length && (
        <div
          role="status"
          className="border-b bg-warning/5 px-3 py-2 text-xs text-warning"
        >
          {summary.warnings.join('；')}
        </div>
      )}
      <Tabs value={mode} onValueChange={setMode}>
        <TabsList className="m-3 h-7">
          <TabsTrigger value="aggregate" className="text-xs">
            聚合内容
          </TabsTrigger>
          <TabsTrigger value="raw" className="text-xs">
            原文
          </TabsTrigger>
        </TabsList>
        <TabsContent value="aggregate" className="m-0 px-4 pb-4">
          {!summary || summary.parse_status === 'unavailable' ? (
            <EmptyState>无法聚合该流式响应，请查看原文</EmptyState>
          ) : !summary.blocks.length ? (
            <EmptyState>没有可展示的聚合内容</EmptyState>
          ) : (
            <div className="space-y-4">
              {summary.blocks.map((block, index) => (
                <SummaryBlock
                  key={`${block.type}-${block.index}-${index}`}
                  block={block}
                />
              ))}
            </div>
          )}
        </TabsContent>
        <TabsContent value="raw" className="m-0 px-4 pb-4">
          <pre className="payload-pre break-all">{body || '—'}</pre>
        </TabsContent>
      </Tabs>
    </div>
  )
}
function SummaryBlock({ block }: { block: StreamSummaryBlock }) {
  if (block.type === 'tool_call')
    return (
      <section className="rounded-md border">
        <header className="flex flex-wrap items-center gap-2 border-b px-3 py-2 text-xs">
          <Wrench className="size-3.5 text-muted-foreground" />
          <code className="font-medium">{block.name || '未命名工具'}</code>
          <span className="ml-auto">
            <Status
              value={block.complete ? 'completed' : 'rate_limited'}
              label={block.complete ? '已完成' : '未完成'}
            />
          </span>
        </header>
        <div className="space-y-2 p-3">
          {block.call_id && (
            <code className="block break-all text-xs text-muted-foreground">
              {block.call_id}
            </code>
          )}
          {block.arguments ? (
            block.arguments_valid ? (
              <JsonPayload value={block.arguments} />
            ) : (
              <pre className="payload-pre">{block.arguments}</pre>
            )
          ) : (
            <span className="text-xs text-muted-foreground">未返回参数</span>
          )}
          {!block.arguments_valid && block.arguments && (
            <p className="text-xs text-warning">
              参数尚未组成完整 JSON，已保留原始增量内容
            </p>
          )}
        </div>
      </section>
    )
  if (block.type === 'reasoning')
    return (
      <details className="group rounded-md border">
        <summary className="flex cursor-pointer list-none items-center justify-between px-3 py-2 text-xs text-muted-foreground">
          推理 / 思考
          <ChevronDown className="size-3.5 transition-transform group-open:rotate-180" />
        </summary>
        <pre className="payload-pre border-t p-3 font-sans text-sm leading-6">
          {block.content || '—'}
        </pre>
      </details>
    )
  return (
    <section>
      <pre className="payload-pre font-sans text-sm leading-6">
        {block.content || '—'}
      </pre>
    </section>
  )
}

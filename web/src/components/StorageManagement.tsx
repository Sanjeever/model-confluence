import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '../api'
import { Button } from './ui/button'
import { NativeSelect } from './ui/native-select'
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription } from './ui/dialog'
import { ConfirmButton, DateRangePicker, ErrorNotice, MetaGrid, displayDate, rangeBounds, todayRange, type DateRangeValue } from './console'

type Cleanup = { mode: string; count: number; from: string; to: string; finished_at: string; error?: string }
type Stats = { database_bytes: number; wal_bytes: number; reusable_bytes: number; request_count: number; retention_days: number; last_cleanup: Cleanup | null }
const bytes = (value: number) => `${(value / 1024 / 1024).toLocaleString('zh-CN', { maximumFractionDigits: 2 })} MiB`

export default function StorageManagement() {
  const [open, setOpen] = useState(false)
  return <><Button variant="outline" onClick={() => setOpen(true)}>存储管理</Button><Dialog open={open} onOpenChange={setOpen}><DialogContent className="max-h-[85dvh] overflow-y-auto sm:max-w-2xl"><DialogHeader><DialogTitle>存储管理</DialogTitle><DialogDescription className="sr-only">查看空间和日志保留策略，按时间范围清理已结束请求。</DialogDescription></DialogHeader>{open && <StorageContent />}</DialogContent></Dialog></>
}

function StorageContent() {
  const client = useQueryClient()
  const [range, setRange] = useState<DateRangeValue>(todayRange)
  const [mode, setMode] = useState('prune')
  const [from, to] = rangeBounds(range)
  const query = new URLSearchParams({ created_from: from, created_to: to, mode })
  const stats = useQuery({ queryKey: ['storage'], queryFn: () => api<Stats>('/api/admin/storage') })
  const preview = useQuery({ queryKey: ['storage-preview', from, to, mode], queryFn: () => api<{ count: number }>(`/api/admin/storage/cleanup?${query}`), enabled: false })
  const cleanup = useMutation({ mutationFn: () => api<Cleanup>(`/api/admin/storage/cleanup?${query}`, { method: 'POST' }), onSuccess: () => {
    client.invalidateQueries({ queryKey: ['storage'] }); client.invalidateQueries({ queryKey: ['requests'] }); client.invalidateQueries({ queryKey: ['request-detail'] }); client.invalidateQueries({ queryKey: ['performance'] }); client.invalidateQueries({ queryKey: ['usage'] }); client.removeQueries({ queryKey: ['storage-preview'] })
  } })
  return <div className="space-y-5">
    <ErrorNotice error={stats.error ?? preview.error ?? cleanup.error} />
    {stats.data && <><MetaGrid items={[{ label: '数据库文件', value: bytes(stats.data.database_bytes) }, { label: 'WAL 文件', value: bytes(stats.data.wal_bytes) }, { label: '可复用空间', value: bytes(stats.data.reusable_bytes) }, { label: '请求记录', value: stats.data.request_count.toLocaleString() }, { label: '载荷保留期', value: stats.data.retention_days ? `${stats.data.retention_days} 天` : '永久保留' }]} /><p className="text-xs text-muted-foreground">清理后空间供 SQLite 复用，文件通常不会立即缩小。Antigravity 工具历史独立保留。</p></>}
    <fieldset className="space-y-3 border-t pt-4" disabled={cleanup.isPending}>
      <div className="flex flex-wrap gap-3"><DateRangePicker value={range} onChange={(value) => { setRange(value); cleanup.reset() }} /><NativeSelect aria-label="清理方式" value={mode} onChange={(e) => { setMode(e.target.value); cleanup.reset() }}><option value="prune">清空载荷，保留统计</option><option value="delete">删除记录与上游尝试</option></NativeSelect></div>
      <div className="flex flex-wrap items-center gap-3"><Button variant="outline" disabled={preview.isFetching || cleanup.isPending} onClick={() => preview.refetch()}>预览范围</Button>{preview.data && <span className="text-xs text-muted-foreground">当前匹配 {preview.data.count.toLocaleString()} 条已结束请求</span>}{preview.data && preview.data.count > 0 && <ConfirmButton title={mode === 'delete' ? '永久删除此时间范围内的记录？' : '清空此时间范围内的载荷？'} description={`${range[0]} 至 ${range[1]}。进行中请求保留，此操作无法撤销。`} pending={cleanup.isPending} onConfirm={() => cleanup.mutate()}><Button variant="destructive" disabled={cleanup.isPending}>执行清理</Button></ConfirmButton>}</div>
    </fieldset>
    {stats.data?.last_cleanup && <div className="space-y-1 border-t pt-3 text-xs text-muted-foreground"><p>最近清理 · {displayDate(stats.data.last_cleanup.finished_at)}</p><p>{stats.data.last_cleanup.mode === 'delete' ? '删除记录' : stats.data.last_cleanup.mode === 'retention' ? '保留期清理' : '清空载荷'} · {stats.data.last_cleanup.count.toLocaleString()} 条</p>{stats.data.last_cleanup.error && <p role="alert" className="text-destructive">{stats.data.last_cleanup.error}</p>}</div>}
  </div>
}

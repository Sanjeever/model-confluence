import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Alert, Button, Card, Col, DatePicker, Empty, Row, Skeleton, Select, Space, Table, Tag, Typography } from 'antd'
import { ReloadOutlined } from '@ant-design/icons'
import dayjs, { type Dayjs } from 'dayjs'
import { api, type RequestDetail, type UsageGroup, type UsagePage } from '../api'
import RequestDrawer from '../components/RequestDrawer'

export default function CostsPage() {
  const [dateRange, setDateRange] = useState<[Dayjs, Dayjs]>(() => {
    const today = dayjs().startOf('day')
    return [today.subtract(6, 'day'), today]
  })
  const [virtualModel, setVirtualModel] = useState('')
  const [detailID, setDetailID] = useState<string | null>(null)
  const createdFrom = dateRange[0].startOf('day').toISOString()
  const createdTo = dateRange[1].startOf('day').add(1, 'day').toISOString()
  const today = dayjs().startOf('day')
  const shouldPoll = dateRange[0].startOf('day').isBefore(today.add(1, 'day')) && dateRange[1].startOf('day').add(1, 'day').isAfter(today)
  const usage = useQuery({
    queryKey: ['usage', createdFrom, createdTo, virtualModel],
    queryFn: () => {
      const params = new URLSearchParams({ created_from: createdFrom, created_to: createdTo })
      if (virtualModel) params.set('virtual_model', virtualModel)
      return api<UsagePage>(`/api/admin/costs?${params}`)
    },
    refetchInterval: shouldPoll ? 10000 : false,
  })
  const modelNames = useQuery({
    queryKey: ['model-names'],
    queryFn: () => api<string[]>('/api/admin/model-names'),
  })
  const detail = useQuery({
    queryKey: ['request-detail', detailID],
    queryFn: () => api<RequestDetail>(`/api/admin/requests/${detailID}`),
    enabled: !!detailID,
  })

  const summary = usage.data?.summary
  return (
    <div className="mc-enter mx-auto max-w-[1500px]">
      <div className="mb-6 flex flex-col gap-4 sm:mb-10 lg:flex-row lg:items-end lg:justify-between">
        <Typography.Title level={2} className="!mb-0 !tracking-[-.04em]">用量统计</Typography.Title>
        <div className="flex w-full flex-col gap-2 sm:flex-row lg:w-auto">
          <DatePicker.RangePicker aria-label="用量统计时间范围" allowClear={false} format="YYYY-MM-DD" value={dateRange} onChange={(value) => { if (!value?.[0] || !value?.[1]) return; setDateRange([value[0], value[1]]) }} className="w-full sm:w-[260px]" />
          <Select allowClear placeholder="虚拟模型" className="w-full sm:w-[200px]" value={virtualModel || undefined} onChange={(value) => setVirtualModel(value ?? '')} options={modelNames.data?.map((name) => ({ value: name, label: name })) ?? []} />
          <Button icon={<ReloadOutlined />} loading={usage.isFetching} onClick={() => usage.refetch()}><span className="hidden sm:inline">刷新</span></Button>
        </div>
      </div>

      {usage.isError && <Alert className="mb-6" type="error" showIcon message="用量数据加载失败" description={(usage.error as Error).message} />}

      <Row gutter={[12, 12]}>
        <Col xs={12} xl={6}><MetricCard label="请求数" value={formatCount(summary?.request_count)} accent="#d7783d" opacity={.25} loading={usage.isPending} /></Col>
        <Col xs={12} xl={6}><MetricCard label="总 Token" value={formatCount(summary?.usage.total_tokens)} accent="#d7783d" opacity={.4} loading={usage.isPending} /></Col>
        <Col xs={12} xl={6}><MetricCard label="输入 Token" value={formatCount(summary?.usage.input_tokens)} accent="#d7783d" opacity={.55} loading={usage.isPending} /></Col>
        <Col xs={12} xl={6}><MetricCard label="输出 Token" value={formatCount(summary?.usage.output_tokens)} accent="#d7783d" opacity={.7} loading={usage.isPending} /></Col>
      </Row>

      <Card className="!mt-6" styles={{ body: { padding: 20 } }}>
        <div className="mb-6 text-sm text-[#7c8d86]">缓存命中</div>
        <div className="flex h-2 overflow-hidden rounded-full bg-black/[.06] dark:bg-white/[.08]">
          {(() => {
            const cached = summary?.usage.input_cached_tokens ?? 0
            const total = summary?.usage.input_tokens ?? 0
            const width = total ? `${(cached / total) * 100}%` : '0%'
            return <div title={`缓存 ${cached}`} style={{ width, backgroundColor: '#4d82b5' }} />
          })()}
        </div>
        <div className="mt-5 grid grid-cols-2 gap-4 sm:grid-cols-4">
          <CacheItem label="缓存读" value={summary?.usage.cache_read_tokens ?? 0} color="#4d82b5" />
          <CacheItem label="缓存写" value={summary?.usage.cache_write_tokens ?? 0} color="#4d9b72" />
          <CacheItem label="推理输出" value={summary?.usage.reasoning_tokens ?? 0} color="#d28a4a" />
          <CacheItem label="非缓存输入" value={Math.max(0, (summary?.usage.input_tokens ?? 0) - (summary?.usage.input_cached_tokens ?? 0))} color="#7c8d86" />
        </div>
      </Card>

      <div className="hidden lg:block">
        <Card className="mt-6 overflow-hidden" styles={{ body: { padding: 0 } }}>
          <Table
            rowKey={(record) => `${record.date}-${record.virtual_model}-${record.upstream_model}`}
            loading={usage.isPending}
            dataSource={usage.data?.groups ?? []}
            scroll={{ x: 1180 }}
            pagination={{ pageSize: 20, showSizeChanger: true, pageSizeOptions: [10, 20, 50] }}
            locale={{ emptyText: <Empty className="py-14" description="所选范围内暂无已完成的用量记录" /> }}
            columns={[
              { title: '日期', dataIndex: 'date', width: 120, render: (value) => <span className="font-mono">{value}</span> },
              { title: '虚拟模型', dataIndex: 'virtual_model', width: 160, render: (value) => <code>{value || '—'}</code> },
              { title: '真实模型', width: 200, render: (_, record) => <Space direction="vertical" size={0}><strong>{record.provider_name || '—'}</strong><code className="break-all text-xs">{record.upstream_model || '—'}</code></Space> },
              { title: '请求数', dataIndex: 'request_count', width: 90, align: 'right' },
              { title: '输入', dataIndex: 'usage', width: 110, align: 'right', render: (usage) => formatCount(usage.input_tokens) },
              { title: '缓存读', dataIndex: 'usage', width: 100, align: 'right', render: (usage) => formatCount(usage.cache_read_tokens) },
              { title: '缓存写', dataIndex: 'usage', width: 100, align: 'right', render: (usage) => formatCount(usage.cache_write_tokens) },
              { title: '输出', dataIndex: 'usage', width: 100, align: 'right', render: (usage) => formatCount(usage.output_tokens) },
              { title: '推理', dataIndex: 'usage', width: 100, align: 'right', render: (usage) => formatCount(usage.reasoning_tokens) },
              { title: '总计', dataIndex: 'usage', width: 110, align: 'right', render: (usage) => <strong>{formatCount(usage.total_tokens)}</strong> },
              { title: '缓存占比', dataIndex: 'usage', width: 90, align: 'right', render: (usage) => formatPercent(usage.input_tokens ? usage.input_cached_tokens / usage.input_tokens : 0) },
            ]}
          />
        </Card>
      </div>

      <div className="mt-6 space-y-3 lg:hidden">
        {usage.isPending ? <Card><Skeleton active paragraph={{ rows: 3 }} /></Card> : usage.data?.groups.length ? usage.data.groups.map((record) => <UsageCard key={`${record.date}-${record.virtual_model}-${record.upstream_model}`} record={record} />) : <Card><Empty className="py-8" description="所选范围内暂无已完成的用量记录" /></Card>}
      </div>

      <RequestDrawer detail={detail.data} loading={detail.isPending} open={!!detailID} onClose={() => setDetailID(null)} />
    </div>
  )
}

function MetricCard({ label, value, accent, opacity, loading }: { label: string; value: string; accent: string; opacity: number; loading: boolean }) {
  return <Card className="relative overflow-hidden" styles={{ body: { minHeight: 132 } }}>
    <div className="absolute right-0 top-0 h-full w-1" style={{ backgroundColor: accent, opacity }} />
    <div className="mb-6 text-sm text-[#7c8d86] sm:mb-8">{label}</div>
    {loading ? <Skeleton.Input active size="large" /> : <div className="font-mono text-3xl font-medium sm:text-4xl">{value}</div>}
  </Card>
}

function CacheItem({ label, value, color }: { label: string; value: number; color: string }) {
  return <div className="flex items-center gap-2 text-sm"><span className="h-2 w-2 rounded-full" style={{ backgroundColor: color }} /><span className="text-[#7c8d86]">{label}</span><strong className="ml-auto font-mono">{formatCount(value)}</strong></div>
}

function UsageCard({ record }: { record: UsageGroup }) {
  return <Card size="small">
    <div className="mb-3 flex items-start justify-between gap-3">
      <div className="min-w-0"><span className="font-mono text-xs text-[#7c8d86]">{record.date}</span><code className="mt-1 block break-all text-sm font-medium">{record.virtual_model || '未指定'}</code></div>
      <Tag>{formatCount(record.request_count)} 请求</Tag>
    </div>
    <div className="mb-2 flex items-start justify-between gap-3"><strong className="min-w-0 break-all text-sm">{record.provider_name || '—'}</strong><code className="shrink-0 text-xs">{record.upstream_model || '—'}</code></div>
    <Space wrap size={[4, 4]}>
      <TokenTag label="输入" value={record.usage.input_tokens} />
      <TokenTag label="缓存读" value={record.usage.cache_read_tokens} />
      <TokenTag label="缓存写" value={record.usage.cache_write_tokens} />
      <TokenTag label="输出" value={record.usage.output_tokens} />
      <TokenTag label="推理" value={record.usage.reasoning_tokens} />
    </Space>
    <div className="mt-3 border-t border-black/5 pt-2 text-right text-sm dark:border-white/5">总计 <strong className="font-mono">{formatCount(record.usage.total_tokens)}</strong> Token</div>
  </Card>
}

function TokenTag({ label, value }: { label: string; value: number }) {
  return <Tag>{label} {formatCount(value)}</Tag>
}

function formatCount(value: number | undefined): string {
  return value == null ? '—' : value.toLocaleString()
}

function formatPercent(value: number): string {
  return `${(value * 100).toFixed(1)}%`
}

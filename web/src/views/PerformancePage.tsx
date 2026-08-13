import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Alert, Button, Card, Col, DatePicker, Empty, Row, Skeleton, Space, Table, Tag, Typography } from 'antd'
import { ReloadOutlined, SwapRightOutlined } from '@ant-design/icons'
import dayjs, { type Dayjs } from 'dayjs'
import { api, type PerformanceOverview, type PerformanceRequest, type RequestDetail } from '../api'
import RequestDrawer, { protocolName, statusName } from '../components/RequestDrawer'

const statusItems = [
  { key: 'completed', label: '已完成', color: '#4d9b72' },
  { key: 'failed', label: '失败', color: '#c65d4b' },
  { key: 'cancelled', label: '已取消', color: '#d28a4a' },
  { key: 'in_progress', label: '进行中', color: '#4d82b5' },
] as const

export default function PerformancePage() {
  const [dateRange, setDateRange] = useState<[Dayjs, Dayjs]>(() => {
    const today = dayjs().startOf('day')
    return [today, today]
  })
  const [detailID, setDetailID] = useState<string | null>(null)
  const createdFrom = dateRange[0].startOf('day').toISOString()
  const createdTo = dateRange[1].startOf('day').add(1, 'day').toISOString()
  const today = dayjs().startOf('day')
  const shouldPoll = dateRange[0].startOf('day').isBefore(today.add(1, 'day')) && dateRange[1].startOf('day').add(1, 'day').isAfter(today)
  const performance = useQuery({
    queryKey: ['performance', createdFrom, createdTo],
    queryFn: () => api<PerformanceOverview>(`/api/admin/performance?created_from=${encodeURIComponent(createdFrom)}&created_to=${encodeURIComponent(createdTo)}`),
    refetchInterval: shouldPoll ? 10000 : false,
  })
  const detail = useQuery({
    queryKey: ['request-detail', detailID],
    queryFn: () => api<RequestDetail>(`/api/admin/requests/${detailID}`),
    enabled: !!detailID,
  })

  function refresh() {
    performance.refetch()
    if (detailID) detail.refetch()
  }

  const overview = performance.data
  const statusCounts = overview?.status_counts ?? { completed: 0, failed: 0, cancelled: 0, in_progress: 0 }
  return (
    <div className="mc-enter mx-auto max-w-[1500px]">
      <div className="mb-6 flex flex-col gap-4 sm:mb-10 lg:flex-row lg:items-end lg:justify-between">
        <Typography.Title level={2} className="!mb-0 !tracking-[-.04em]">性能监控</Typography.Title>
        <div className="flex w-full flex-col gap-2 sm:flex-row lg:w-auto">
          <DatePicker.RangePicker aria-label="性能统计时间范围" allowClear={false} format="YYYY-MM-DD" value={dateRange} onChange={(value) => { if (!value?.[0] || !value?.[1]) return; setDateRange([value[0], value[1]]) }} className="w-full sm:w-[260px]" />
          <Button icon={<ReloadOutlined />} loading={performance.isFetching} onClick={refresh}><span className="hidden sm:inline">刷新</span></Button>
        </div>
      </div>

      {performance.isError && <Alert className="mb-6" type="error" showIcon message="性能数据加载失败" description={(performance.error as Error).message} />}

      <Row gutter={[12, 12]}>
        <Col xs={12} xl={6}><MetricCard label="请求数" value={formatCount(overview?.request_count)} accent="#d7783d" opacity={.25} loading={performance.isPending} /></Col>
        <Col xs={12} xl={6}><MetricCard label="成功率" value={formatPercent(overview?.success_rate)} accent="#d7783d" opacity={.4} loading={performance.isPending} /></Col>
        <Col xs={12} xl={6}><MetricCard label="首内容 P95" value={formatDuration(overview?.first_content_latency.p95)} accent="#d7783d" opacity={.55} loading={performance.isPending} /></Col>
        <Col xs={12} xl={6}><MetricCard label="总耗时 P95" value={formatDuration(overview?.total_latency.p95)} accent="#d7783d" opacity={.7} loading={performance.isPending} /></Col>
      </Row>

      <Card className="!mt-6" styles={{ body: { padding: 20 } }}>
        <div className="mb-6 text-sm text-[#7c8d86]">状态分布</div>
        <div className="flex h-2 overflow-hidden rounded-full bg-black/[.06] dark:bg-white/[.08]">
          {statusItems.map((item) => {
            const count = statusCounts[item.key]
            const width = overview?.request_count ? `${(count / overview.request_count) * 100}%` : '0%'
            return <div key={item.key} title={`${item.label} ${count}`} style={{ width, backgroundColor: item.color }} />
          })}
        </div>
        <div className="mt-5 grid grid-cols-2 gap-4 sm:grid-cols-4">
          {statusItems.map((item) => <div key={item.key} className="flex items-center gap-2 text-sm"><span className="h-2 w-2 rounded-full" style={{ backgroundColor: item.color }} /><span className="text-[#7c8d86]">{item.label}</span><strong className="ml-auto font-mono">{formatCount(statusCounts[item.key])}</strong></div>)}
        </div>
      </Card>

      <Typography.Title level={4} className="!mb-0 !mt-8 !tracking-[-.03em]">异常与慢请求</Typography.Title>

      <div className="mt-4 hidden lg:block">
        <Card className="overflow-hidden" styles={{ body: { padding: 0 } }}>
          <Table
            rowKey="id"
            loading={performance.isPending}
            dataSource={overview?.attention_requests ?? []}
            scroll={{ x: 1180 }}
            onRow={(record) => ({ onClick: () => setDetailID(record.id), className: 'cursor-pointer' })}
            pagination={false}
            locale={{ emptyText: <Empty className="py-14" description={overview?.request_count ? '所选范围内没有可关注的请求' : '所选时间范围内暂无请求记录'} /> }}
            columns={[
              { title: '请求时间', dataIndex: 'created_at', width: 180, render: (value) => new Date(value).toLocaleString() },
              { title: '模型', dataIndex: 'virtual_model', width: 150, render: (value) => <code>{value || '未指定'}</code> },
              { title: '上游', width: 220, render: (_, record) => record.provider_name ? <Space direction="vertical" size={0}><strong>{record.provider_name}</strong><code className="break-all text-xs">{record.upstream_model}</code></Space> : '未调用' },
              { title: '协议', width: 230, render: (_, record) => <Space size={6} className="whitespace-nowrap"><Tag style={{ marginInlineEnd: 0 }}>{protocolName(record.inbound_protocol)}</Tag><SwapRightOutlined className="text-[#7c8d86]" /><Tag color="orange" style={{ marginInlineEnd: 0 }}>{protocolName(record.upstream_protocol)}</Tag></Space> },
              { title: '状态', width: 110, render: (_, record) => <Tag color={statusColor(record.status)}>{statusName(record.status)}</Tag> },
              { title: '首内容', dataIndex: 'first_content_ms', width: 100, align: 'right', render: (value) => formatDuration(value) },
              { title: '总耗时', dataIndex: 'total_ms', width: 100, align: 'right', render: (value) => formatDuration(value) },
              { title: '响应', width: 230, render: (_, record) => <span className="block max-w-[210px] truncate" title={record.error_message || undefined}>{record.error_message || (record.response_status ? `HTTP ${record.response_status}` : '—')}</span> },
            ]}
          />
        </Card>
      </div>

      <div className="mt-4 space-y-3 lg:hidden">
        {performance.isPending ? <Card><Skeleton active paragraph={{ rows: 3 }} /></Card> : (overview?.attention_requests ?? []).length ? (overview?.attention_requests ?? []).map((record) => <AttentionCard key={record.id} record={record} onClick={() => setDetailID(record.id)} />) : <Card><Empty className="py-8" description={overview?.request_count ? '所选范围内没有可关注的请求' : '所选时间范围内暂无请求记录'} /></Card>}
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

function AttentionCard({ record, onClick }: { record: PerformanceRequest; onClick: () => void }) {
  return <Card size="small" className="cursor-pointer" onClick={onClick}>
    <div className="mb-3 flex items-start justify-between gap-3">
      <div className="min-w-0"><div className="text-xs text-[#7c8d86]">{new Date(record.created_at).toLocaleString()}</div><code className="mt-1 block break-all text-xs">{record.id}</code></div>
      <Tag className="shrink-0" color={statusColor(record.status)}>{statusName(record.status)}</Tag>
    </div>
    <div className="mb-3 flex items-start justify-between gap-3"><code className="min-w-0 break-all text-sm font-medium">{record.virtual_model || '未指定'}</code><span className="shrink-0 text-xs text-[#7c8d86]">{formatDuration(record.total_ms)}</span></div>
    <Space size={6} wrap>
      <Tag style={{ marginInlineEnd: 0 }}>{protocolName(record.inbound_protocol)}</Tag>
      <SwapRightOutlined className="text-[#7c8d86]" />
      <Tag color="orange" style={{ marginInlineEnd: 0 }}>{protocolName(record.upstream_protocol)}</Tag>
      {record.provider_name && <Tag>{record.provider_name}</Tag>}
    </Space>
    {record.error_message && <div className="mt-3 truncate text-xs text-[#c65d4b]" title={record.error_message}>{record.error_message}</div>}
  </Card>
}

function formatCount(value: number | undefined): string {
  return value == null ? '—' : value.toLocaleString()
}

function formatPercent(value: number | null | undefined): string {
  return value == null ? '—' : `${(value * 100).toFixed(1)}%`
}

function formatDuration(value: number | null | undefined): string {
  if (value == null) return '—'
  if (value < 1000) return `${value} ms`
  const seconds = value / 1000
  return `${seconds.toFixed(seconds >= 10 ? 1 : 2)} s`
}

function statusColor(value: string): string | undefined {
  return ({ completed: 'success', failed: 'error', cancelled: 'warning', in_progress: 'processing' } as Record<string, string>)[value]
}

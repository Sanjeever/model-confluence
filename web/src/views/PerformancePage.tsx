import { useMemo, useState } from 'react'
import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { Link, useLocation } from 'react-router-dom'
import type { ColumnDef } from '@tanstack/react-table'
import { RefreshCw } from 'lucide-react'
import { api, type PerformanceOverview, type PerformanceRequest } from '../api'
import { formatCount, formatDuration, formatPercent } from '../format'
import MetricCard from '../components/MetricCard'
import DataTable from '../components/DataTable'
import { ProtocolFlow, Status } from '../components/Status'
import {
  DateRangePicker,
  ErrorNotice,
  PageHeader,
  displayDate,
  includesToday,
  rangeBounds,
  todayRange,
} from '../components/console'
import { Button } from '@/components/ui/button'

const statuses = [
  { key: 'completed', label: '已完成', color: 'var(--success)' },
  { key: 'failed', label: '失败', color: 'var(--destructive)' },
  { key: 'cancelled', label: '已取消', color: 'var(--muted-foreground)' },
  { key: 'in_progress', label: '进行中', color: 'var(--info)' },
] as const
export default function PerformancePage() {
  const [range, setRange] = useState(() => todayRange())
  const location = useLocation()
  const [from, to] = rangeBounds(range)
  const performance = useQuery({
    queryKey: ['performance', from, to],
    queryFn: () =>
      api<PerformanceOverview>(
        `/api/admin/performance?${new URLSearchParams({ created_from: from, created_to: to })}`,
      ),
    placeholderData: keepPreviousData,
    refetchInterval: includesToday(range) ? 10000 : false,
  })
  const overview = performance.data
  const columns = useMemo<ColumnDef<PerformanceRequest>[]>(
    () => [
      {
        accessorKey: 'created_at',
        header: '请求时间',
        size: 160,
        cell: ({ row }) => (
          <Link
            className="text-link text-xs tabular-nums"
            to={`/requests/${row.original.id}`}
            state={{ returnTo: location.pathname + location.search }}
          >
            {displayDate(row.original.created_at)}
          </Link>
        ),
      },
      {
        accessorKey: 'virtual_model',
        header: '模型',
        cell: ({ row }) => (
          <code
            className="block max-w-40 truncate text-xs"
            title={row.original.virtual_model}
          >
            {row.original.virtual_model || '未指定'}
          </code>
        ),
      },
      {
        id: 'upstream',
        header: '上游',
        cell: ({ row }) => (
          <div className="max-w-44">
            <span className="block truncate text-xs">
              {row.original.provider_name || '未调用'}
            </span>
            {row.original.upstream_model && (
              <code
                className="mt-1 block truncate text-[11px] text-muted-foreground"
                title={row.original.upstream_model}
              >
                {row.original.upstream_model}
              </code>
            )}
          </div>
        ),
      },
      {
        id: 'protocol',
        header: '协议',
        size: 210,
        cell: ({ row }) => (
          <ProtocolFlow
            inbound={row.original.inbound_protocol}
            upstream={row.original.upstream_protocol}
          />
        ),
      },
      {
        accessorKey: 'status',
        header: '状态',
        size: 100,
        cell: ({ row }) => <Status value={row.original.status} />,
      },
      {
        accessorKey: 'first_content_ms',
        header: '首内容',
        size: 85,
        meta: { align: 'right' },
        cell: ({ row }) => formatDuration(row.original.first_content_ms),
      },
      {
        accessorKey: 'total_ms',
        header: '总耗时',
        size: 85,
        meta: { align: 'right' },
        cell: ({ row }) => formatDuration(row.original.total_ms),
      },
      {
        id: 'response',
        header: '响应',
        size: 210,
        cell: ({ row }) => (
          <span
            title={row.original.error_message || undefined}
            className={`block max-w-52 truncate text-xs ${row.original.error_message ? 'text-destructive' : 'text-muted-foreground'}`}
          >
            {row.original.error_message ||
              (row.original.response_status
                ? `HTTP ${row.original.response_status}`
                : '—')}
          </span>
        ),
      },
    ],
    [location.pathname, location.search],
  )
  return (
    <>
      <PageHeader title="性能监控">
        <Button
          variant="outline"
          size="icon"
          aria-label="刷新性能数据"
          disabled={performance.isFetching}
          onClick={() => performance.refetch()}
        >
          <RefreshCw className={performance.isFetching ? 'animate-spin' : ''} />
        </Button>
      </PageHeader>
      <div className="filter-bar">
        <DateRangePicker value={range} onChange={setRange} />
      </div>
      <ErrorNotice
        error={performance.error}
        retry={() => performance.refetch()}
      />
      <div className="metric-strip">
        <MetricCard
          label="请求数"
          value={formatCount(overview?.request_count)}
          loading={performance.isPending}
        />
        <MetricCard
          label="成功率"
          value={formatPercent(overview?.success_rate)}
          loading={performance.isPending}
        />
        <MetricCard
          label="首内容 P95"
          value={formatDuration(overview?.first_content_latency.p95)}
          loading={performance.isPending}
        />
        <MetricCard
          label="总耗时 P95"
          value={formatDuration(overview?.total_latency.p95)}
          loading={performance.isPending}
        />
      </div>
      <section>
        <h2 className="section-heading">异常与慢请求</h2>
        <DataTable
          data={overview?.attention_requests ?? []}
          columns={columns}
          rowID={(row) => row.id}
          loading={performance.isPending}
          fetching={performance.isFetching}
          empty={
            overview?.request_count
              ? '所选日期没有异常与慢请求'
              : '所选日期暂无请求'
          }
          minWidth={1080}
        />
      </section>
      <section className="section">
        <h2 className="section-heading">状态分布</h2>
        <div className="flex h-1.5 overflow-hidden rounded-full bg-muted">
          {statuses.map((item) => (
            <div
              key={item.key}
              title={`${item.label} ${overview?.status_counts[item.key] ?? 0}`}
              style={{
                width: overview?.request_count
                  ? `${(overview.status_counts[item.key] / overview.request_count) * 100}%`
                  : '0%',
                backgroundColor: item.color,
              }}
            />
          ))}
        </div>
        <div className="mt-3 flex flex-wrap gap-x-6 gap-y-2">
          {statuses.map((item) => (
            <div key={item.key} className="flex items-center gap-2">
              <Status value={item.key} />
              <span className="text-xs tabular-nums">
                {formatCount(overview?.status_counts[item.key])}
              </span>
            </div>
          ))}
        </div>
      </section>
    </>
  )
}

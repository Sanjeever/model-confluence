import { useMemo, useState } from 'react'
import { keepPreviousData, useQuery } from '@tanstack/react-query'
import type { ColumnDef } from '@tanstack/react-table'
import { RefreshCw } from 'lucide-react'
import { api, type UsagePage, type UsageGroup } from '../api'
import { formatCount, formatPercent } from '../format'
import MetricCard from '../components/MetricCard'
import DataTable from '../components/DataTable'
import {
  DateRangePicker,
  ErrorNotice,
  FilterSelect,
  PageHeader,
  includesToday,
  rangeBounds,
  todayRange,
} from '../components/console'
import { Button } from '@/components/ui/button'

export default function CostsPage() {
  const [range, setRange] = useState(() => todayRange(7))
  const [model, setModel] = useState('')
  const [from, to] = rangeBounds(range)
  const usage = useQuery({
    queryKey: ['usage', from, to, model],
    queryFn: () => {
      const query = new URLSearchParams({ created_from: from, created_to: to })
      if (model) query.set('virtual_model', model)
      return api<UsagePage>(`/api/admin/costs?${query}`)
    },
    placeholderData: keepPreviousData,
    refetchInterval: includesToday(range) ? 10000 : false,
  })
  const models = useQuery({
    queryKey: ['model-names'],
    queryFn: () => api<string[]>('/api/admin/model-names'),
  })
  const summary = usage.data?.summary
  const columns = useMemo<ColumnDef<UsageGroup>[]>(
    () => [
      {
        accessorKey: 'date',
        header: '日期',
        size: 110,
        cell: ({ row }) => (
          <span className="text-xs tabular-nums">{row.original.date}</span>
        ),
      },
      {
        accessorKey: 'virtual_model',
        header: '虚拟模型',
        cell: ({ row }) => (
          <code
            className="block max-w-40 truncate text-xs"
            title={row.original.virtual_model}
          >
            {row.original.virtual_model || '—'}
          </code>
        ),
      },
      {
        id: 'upstream',
        header: '真实模型 / 供应商',
        size: 180,
        cell: ({ row }) => (
          <div className="max-w-44">
            <code
              className="block truncate text-xs"
              title={row.original.upstream_model}
            >
              {row.original.upstream_model || '—'}
            </code>
            <span className="mt-1 block truncate text-[11px] text-muted-foreground">
              {row.original.provider_name || '—'}
            </span>
          </div>
        ),
      },
      {
        accessorKey: 'request_count',
        header: '请求数',
        size: 75,
        meta: { align: 'right' },
        cell: ({ row }) => formatCount(row.original.request_count),
      },
      ...(
        [
          { key: 'input_tokens', label: '输入' },
          { key: 'cache_read_tokens', label: '缓存读' },
          { key: 'cache_write_tokens', label: '缓存写' },
          { key: 'output_tokens', label: '输出' },
          { key: 'reasoning_tokens', label: '推理' },
          { key: 'total_tokens', label: '总计' },
        ] as const
      ).map((item): ColumnDef<UsageGroup> => ({
        id: item.key,
        header: item.label,
        size: 90,
        meta: { align: 'right' },
        cell: ({ row }) => (
          <span className={item.key === 'total_tokens' ? 'font-medium' : ''}>
            {formatCount(row.original.usage[item.key])}
          </span>
        ),
      })),
      {
        id: 'cache_share',
        header: '缓存占比',
        size: 85,
        meta: { align: 'right' },
        cell: ({ row }) =>
          formatPercent(
            row.original.usage.input_tokens
              ? row.original.usage.input_cached_tokens /
                  row.original.usage.input_tokens
              : 0,
          ),
      },
    ],
    [],
  )
  return (
    <>
      <PageHeader title="用量统计">
        <Button
          variant="outline"
          size="icon"
          aria-label="刷新用量数据"
          disabled={usage.isFetching}
          onClick={() => usage.refetch()}
        >
          <RefreshCw className={usage.isFetching ? 'animate-spin' : ''} />
        </Button>
      </PageHeader>
      <div className="filter-bar">
        <DateRangePicker value={range} onChange={setRange} />
        <FilterSelect
          value={model}
          placeholder="虚拟模型"
          onChange={setModel}
          searchable
          options={
            models.data?.map((name) => ({ value: name, label: name })) ?? []
          }
        />
      </div>
      <ErrorNotice error={usage.error} retry={() => usage.refetch()} />
      <ErrorNotice error={models.error} retry={() => models.refetch()} />
      <div className="metric-strip">
        <MetricCard
          label="请求数"
          value={formatCount(summary?.request_count)}
          loading={usage.isPending}
        />
        <MetricCard
          label="总 Token"
          value={formatCount(summary?.usage.total_tokens)}
          loading={usage.isPending}
        />
        <MetricCard
          label="输入 Token"
          value={formatCount(summary?.usage.input_tokens)}
          loading={usage.isPending}
        />
        <MetricCard
          label="输出 Token"
          value={formatCount(summary?.usage.output_tokens)}
          loading={usage.isPending}
        />
      </div>
      <div className="mb-6 flex flex-wrap items-center gap-x-6 gap-y-3 border-b pb-5 text-xs">
        <div className="flex items-center gap-3">
          <span className="text-muted-foreground">输入缓存占比</span>
          <span className="font-medium tabular-nums">
            {formatPercent(
              summary
                ? summary.usage.input_tokens
                  ? summary.usage.input_cached_tokens /
                    summary.usage.input_tokens
                  : 0
                : undefined,
            )}
          </span>
        </div>
        <CacheItem label="缓存读" value={summary?.usage.cache_read_tokens} />
        <CacheItem label="缓存写" value={summary?.usage.cache_write_tokens} />
        <CacheItem label="推理输出" value={summary?.usage.reasoning_tokens} />
        <CacheItem
          label="非缓存输入"
          value={
            summary
              ? Math.max(
                  0,
                  summary.usage.input_tokens -
                    summary.usage.input_cached_tokens,
                )
              : undefined
          }
        />
      </div>
      <DataTable
        data={usage.data?.groups ?? []}
        columns={columns}
        rowID={(row) =>
          `${row.date}-${row.virtual_model}-${row.upstream_model}`
        }
        loading={usage.isPending}
        fetching={usage.isFetching}
        empty="所选条件暂无已完成的用量记录"
        minWidth={1120}
      />
    </>
  )
}
function CacheItem({ label, value }: { label: string; value?: number }) {
  return (
    <div className="flex items-center gap-2">
      <span className="text-muted-foreground">{label}</span>
      <span className="tabular-nums">{formatCount(value)}</span>
    </div>
  )
}

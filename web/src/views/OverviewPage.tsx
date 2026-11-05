import { useMemo } from 'react'
import { keepPreviousData, useQuery } from '@tanstack/react-query'
import {
  Link,
  useLocation,
  useNavigate,
  useParams,
  useSearchParams,
} from 'react-router-dom'
import { RefreshCw } from 'lucide-react'
import dayjs from 'dayjs'
import type { ColumnDef } from '@tanstack/react-table'
import {
  api,
  type AccessKeyOption,
  type RequestDetail,
  type RequestPage,
  type RequestSummary,
} from '../api'
import { formatDuration } from '../format'
import RequestDrawer from '../components/RequestDrawer'
import StorageManagement from '../components/StorageManagement'
import DataTable from '../components/DataTable'
import { Status, ProtocolFlow } from '../components/Status'
import {
  DateRangePicker,
  ErrorNotice,
  FilterSelect,
  PageHeader,
  SearchField,
  displayDate,
  includesToday,
  rangeBounds,
  todayRange,
  type DateRangeValue,
} from '../components/console'
import { Button } from '@/components/ui/button'

export default function OverviewPage() {
  const [params, setParams] = useSearchParams()
  const today = todayRange()
  const dateRange: DateRangeValue = [
    dayjs(params.get('from')).isValid() ? params.get('from')! : today[0],
    dayjs(params.get('to')).isValid() ? params.get('to')! : today[1],
  ]
  const page = positiveInteger(params.get('page'), 1)
  const pageSize = positiveInteger(params.get('page_size'), 10)
  const requestID = params.get('request_id') ?? ''
  const keyID = params.get('access_key_id') ?? ''
  const extraFilters = Object.fromEntries(['status', 'provider', 'virtual_model', 'upstream_model', 'inbound_protocol', 'upstream_protocol'].map((key) => [key, params.get(key) ?? '']))
  const { detailID } = useParams()
  const location = useLocation()
  const navigate = useNavigate()
  const [from, to] = rangeBounds(dateRange)
  function update(values: Record<string, string>) {
    setParams(
      (previous) => {
        const next = new URLSearchParams(previous)
        for (const [key, value] of Object.entries(values)) {
          if (value) next.set(key, value)
          else next.delete(key)
        }
        return next
      },
      { replace: true },
    )
  }
  const keys = useQuery({
    queryKey: ['access-key-options'],
    queryFn: () => api<AccessKeyOption[]>('/api/admin/access-key-options'),
  })
  const requests = useQuery({
    queryKey: ['requests', page, pageSize, requestID, keyID, from, to, extraFilters],
    queryFn: () => {
      const query = new URLSearchParams({
        page: String(page),
        page_size: String(pageSize),
        request_id: requestID,
        created_from: from,
        created_to: to,
      })
      if (keyID) query.set('access_key_id', keyID)
      for (const [key, value] of Object.entries(extraFilters)) if (value) query.set(key, value)
      return api<RequestPage>(`/api/admin/requests?${query}`)
    },
    placeholderData: keepPreviousData,
    refetchInterval: includesToday(dateRange) ? 5000 : false,
  })
  const detail = useQuery({
    queryKey: ['request-detail', detailID],
    queryFn: () => api<RequestDetail>(`/api/admin/requests/${detailID}`),
    enabled: !!detailID,
    refetchInterval: (query) =>
      query.state.data?.status === 'in_progress' ? 3000 : false,
  })
  const columns = useMemo<ColumnDef<RequestSummary>[]>(
    () => [
      {
        accessorKey: 'created_at',
        header: '请求时间',
        size: 155,
        cell: ({ row }) => (
          <span className="text-xs tabular-nums text-muted-foreground">
            {displayDate(row.original.created_at)}
          </span>
        ),
      },
      {
        accessorKey: 'id',
        header: '请求 ID',
        size: 135,
        cell: ({ row }) => (
          <Link
            className="text-link block max-w-32 truncate font-mono text-xs"
            title={row.original.id}
            to={{
              pathname: `/requests/${row.original.id}`,
              search: location.search,
            }}
            state={{ returnTo: location.pathname + location.search }}
          >
            {row.original.id}
          </Link>
        ),
      },
      {
        accessorKey: 'virtual_model',
        header: '模型',
        size: 150,
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
        id: 'protocol',
        header: '协议',
        size: 190,
        cell: ({ row }) => (
          <ProtocolFlow
            inbound={row.original.inbound_protocol}
            upstream={row.original.upstream_protocol}
          />
        ),
      },
      {
        accessorKey: 'stream',
        header: '方式',
        size: 65,
        cell: ({ row }) => (
          <span className="text-xs text-muted-foreground">
            {row.original.stream ? '流式' : '非流式'}
          </span>
        ),
      },
      {
        accessorKey: 'access_key_name',
        header: '访问密钥',
        size: 110,
        cell: ({ row }) => (
          <span
            className="block max-w-28 truncate text-xs"
            title={row.original.access_key_name}
          >
            {row.original.access_key_name || '—'}
          </span>
        ),
      },
      {
        accessorKey: 'first_content_ms',
        header: '首内容',
        size: 85,
        meta: { align: 'right' },
        cell: ({ row }) => (
          <span className="text-xs">
            {formatDuration(row.original.first_content_ms)}
          </span>
        ),
      },
      {
        accessorKey: 'total_ms',
        header: '总耗时',
        size: 85,
        meta: { align: 'right' },
        cell: ({ row }) => (
          <span className="text-xs">
            {formatDuration(row.original.total_ms)}
          </span>
        ),
      },
      {
        accessorKey: 'status',
        header: '状态',
        size: 95,
        cell: ({ row }) => (
          <div>
            <Status value={row.original.status} />
            {row.original.payload_pruned && (
              <span className="mt-1 block text-[11px] text-muted-foreground">
                载荷已清理
              </span>
            )}
          </div>
        ),
      },
    ],
    [location.pathname, location.search],
  )
  function closeDetail() {
    const returnTo = (location.state as { returnTo?: string } | null)?.returnTo
    navigate(returnTo ?? { pathname: '/requests', search: location.search }, {
      replace: true,
    })
  }
  return (
    <>
      <PageHeader title="使用记录">
        <StorageManagement />
        <Button
          variant="outline"
          size="icon"
          aria-label="刷新请求"
          onClick={() => {
            requests.refetch()
            if (detailID) detail.refetch()
          }}
          disabled={requests.isFetching}
        >
          <RefreshCw className={requests.isFetching ? 'animate-spin' : ''} />
        </Button>
      </PageHeader>
      <div className="filter-bar">
        <DateRangePicker
          value={dateRange}
          onChange={(value) =>
            update({ from: value[0], to: value[1], page: '1' })
          }
        />
        <FilterSelect
          placeholder="访问密钥"
          value={keyID}
          onChange={(value) => update({ access_key_id: value, page: '1' })}
          searchable
          options={
            keys.data?.map((key) => ({
              value: String(key.id),
              label: key.archived ? `${key.name}（已归档）` : key.name,
            })) ?? []
          }
        />
        <SearchField
          key={requestID}
          placeholder="搜索请求 ID"
          value={requestID}
          onChange={(value) => update({ request_id: value, page: '1' })}
          className="sm:ml-auto"
        />
      </div>
      <details className="mb-4 text-xs">
        <summary className="w-fit cursor-pointer text-muted-foreground">更多筛选{Object.values(extraFilters).some(Boolean) ? ' · 已应用' : ''}</summary>
        <div className="filter-bar mt-3">
          <FilterSelect placeholder="状态" value={extraFilters.status} onChange={(value) => update({ status: value, page: '1' })} options={[{ value: 'completed', label: '已完成' }, { value: 'failed', label: '失败' }, { value: 'cancelled', label: '已取消' }, { value: 'in_progress', label: '进行中' }]} />
          {(['inbound_protocol', 'upstream_protocol'] as const).map((key) => <FilterSelect key={key} placeholder={key === 'inbound_protocol' ? '入站协议' : '上游协议'} value={extraFilters[key]} onChange={(value) => update({ [key]: value, page: '1' })} options={['chat_completions', 'responses', 'messages', ...(key === 'upstream_protocol' ? ['antigravity'] : [])].map((value) => ({ value, label: value }))} />)}
          {([['provider', '供应商全名'], ['virtual_model', '虚拟模型全名'], ['upstream_model', '真实模型全名']] as const).map(([key, label]) => <SearchField key={`${key}:${extraFilters[key]}`} placeholder={label} value={extraFilters[key]} onChange={(value) => update({ [key]: value, page: '1' })} />)}
          <Button variant="ghost" size="sm" onClick={() => update({ ...Object.fromEntries(Object.keys(extraFilters).map((key) => [key, ''])), page: '1' })}>清除</Button>
        </div>
      </details>
      <ErrorNotice error={requests.error} retry={() => requests.refetch()} />
      <ErrorNotice error={keys.error} retry={() => keys.refetch()} />
      <DataTable
        data={requests.data?.items ?? []}
        columns={columns}
        rowID={(row) => row.id}
        loading={requests.isPending}
        fetching={requests.isFetching}
        empty={requestID || keyID ? '没有匹配条件的请求' : '所选日期暂无请求'}
        total={requests.data?.total ?? 0}
        pagination={{ pageIndex: page - 1, pageSize }}
        onPaginationChange={(next) =>
          update({
            page: String(next.pageIndex + 1),
            page_size: String(next.pageSize),
          })
        }
        minWidth={1080}
      />
      <RequestDrawer
        detail={detail.data}
        loading={detail.isPending}
        error={detail.error}
        onRetry={() => detail.refetch()}
        open={!!detailID}
        onClose={closeDetail}
      />
    </>
  )
}
function positiveInteger(value: string | null, fallback: number) {
  const parsed = Number(value)
  return Number.isInteger(parsed) && parsed > 0 ? parsed : fallback
}

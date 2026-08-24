import { useEffect, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useLocation, useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { Button, Card, Col, DatePicker, Empty, Input, Row, Space, Table, Tag, Typography } from 'antd'
import { EyeOutlined, ReloadOutlined, SwapRightOutlined } from '@ant-design/icons'
import dayjs, { type Dayjs } from 'dayjs'
import { api, type Overview, type RequestDetail, type RequestPage } from '../api'
import RequestDrawer, { protocolName, statusName } from '../components/RequestDrawer'
import MetricCard from '../components/MetricCard'
import { formatCount } from '../format'

const metrics: Array<{ key: keyof Overview; label: string }> = [
  { key: 'access_keys', label: '访问密钥' },
  { key: 'providers', label: '供应商' },
  { key: 'virtual_models', label: '虚拟模型' },
]

export default function OverviewPage() {
  const [searchParams, setSearchParams] = useSearchParams()
  const [page, setPage] = useState(() => positiveInteger(searchParams.get('page'), 1))
  const [pageSize, setPageSize] = useState(() => positiveInteger(searchParams.get('page_size'), 10))
  const [dateRange, setDateRange] = useState<[Dayjs, Dayjs]>(() => {
    const today = dayjs().startOf('day')
    const from = dayjs(searchParams.get('from'))
    const to = dayjs(searchParams.get('to'))
    return [from.isValid() ? from : today, to.isValid() ? to : today]
  })
  const [requestIDInput, setRequestIDInput] = useState(() => searchParams.get('request_id') ?? '')
  const [requestID, setRequestID] = useState(() => searchParams.get('request_id') ?? '')
  const { detailID } = useParams<{ detailID: string }>()
  const location = useLocation()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const createdFrom = dateRange[0].startOf('day').toISOString()
  const createdTo = dateRange[1].startOf('day').add(1, 'day').toISOString()
  const shouldPoll = dateRange[0].startOf('day').isBefore(dayjs().startOf('day').add(1, 'day')) && dateRange[1].startOf('day').add(1, 'day').isAfter(dayjs().startOf('day'))

  useEffect(() => {
    const params = new URLSearchParams({
      from: dateRange[0].format('YYYY-MM-DD'),
      to: dateRange[1].format('YYYY-MM-DD'),
      page: String(page),
      page_size: String(pageSize),
    })
    if (requestID) params.set('request_id', requestID)
    setSearchParams(params, { replace: true })
  }, [dateRange, page, pageSize, requestID, setSearchParams])
  const overview = useQuery({
    queryKey: ['overview'],
    queryFn: () => api<Overview>('/api/admin/overview'),
  })
  const requests = useQuery({
    queryKey: ['requests', page, pageSize, requestID, createdFrom, createdTo],
    queryFn: () => {
      const params = new URLSearchParams({
        page: String(page),
        page_size: String(pageSize),
        request_id: requestID,
        created_from: createdFrom,
        created_to: createdTo,
      })
      return api<RequestPage>(`/api/admin/requests?${params}`)
    },
    refetchInterval: shouldPoll ? 5000 : false,
  })
  const detail = useQuery({
    queryKey: ['request-detail', detailID],
    queryFn: () => api<RequestDetail>(`/api/admin/requests/${detailID}`),
    enabled: !!detailID,
  })

  function refresh() {
    requests.refetch()
    queryClient.invalidateQueries({ queryKey: ['overview'] })
    if (detailID) detail.refetch()
  }

  function openDetail(id: string) {
    navigate({ pathname: `/requests/${id}`, search: location.search }, { state: { returnTo: location.pathname + location.search } })
  }

  function closeDetail() {
    const returnTo = (location.state as { returnTo?: string } | null)?.returnTo
    navigate(returnTo ?? { pathname: '/requests', search: location.search }, { replace: true })
  }

  return (
    <div className="mc-enter mx-auto max-w-[1500px]">
      <div className="mb-6 flex flex-col gap-4 sm:mb-10 lg:flex-row lg:items-end lg:justify-between">
        <Typography.Title level={2} className="!mb-0 !tracking-[-.04em]">使用记录</Typography.Title>
        <div className="flex w-full flex-col gap-2 sm:flex-row lg:w-auto">
          <DatePicker.RangePicker aria-label="请求时间范围" allowClear={false} format="YYYY-MM-DD" value={dateRange} onChange={(value) => { if (!value?.[0] || !value?.[1]) return; setDateRange([value[0], value[1]]); setPage(1) }} className="w-full sm:w-[260px]" />
          <Input.Search allowClear enterButton="搜索" placeholder="输入请求 ID" className="min-w-0 flex-1 lg:w-[360px]" value={requestIDInput} onChange={(event) => { const value = event.target.value; setRequestIDInput(value); if (!value) { setRequestID(''); setPage(1) } }} onSearch={(value) => { setRequestID(value.trim()); setPage(1) }} />
          <Button icon={<ReloadOutlined />} loading={requests.isFetching} onClick={refresh}><span className="hidden sm:inline">刷新</span></Button>
        </div>
      </div>
      <Row gutter={[12, 12]}>
        {metrics.map((metric, index) => (
          <Col xs={12} xl={8} key={metric.key}>
            <MetricCard label={metric.label} value={formatCount(overview.data?.[metric.key])} opacity={.25 + index * .15} loading={overview.isPending} />
          </Col>
        ))}
      </Row>
      <div className="mt-6 lg:mt-8">
        <Card className="overflow-hidden" styles={{ body: { padding: 0 } }}>
          <Table
          rowKey="id"
          loading={requests.isPending}
          dataSource={requests.data?.items ?? []}
          scroll={{ x: 1380, y: 'calc(100vh - 430px)' }}
          onRow={(record) => ({ onClick: () => openDetail(record.id), className: 'cursor-pointer' })}
          pagination={{ current: page, pageSize, total: requests.data?.total ?? 0, showSizeChanger: true, pageSizeOptions: [10, 20, 50], onChange: (nextPage, nextPageSize) => { setPage(nextPageSize === pageSize ? nextPage : 1); setPageSize(nextPageSize) }, showTotal: (total) => `共 ${total} 条` }}
          locale={{ emptyText: <Empty className="py-14" description={requestID ? '没有匹配该请求 ID 的记录' : '所选时间范围内暂无请求记录'} /> }}
          columns={[
            { title: '请求时间', dataIndex: 'created_at', width: 190, render: (value) => new Date(value).toLocaleString() },
            { title: '请求 ID', dataIndex: 'id', width: 180, render: (value) => <code>{value}</code> },
            { title: '模型', dataIndex: 'virtual_model', width: 160, render: (value) => <code>{value}</code> },
            { title: '协议', width: 240, render: (_, record) => <Space size={6} className="whitespace-nowrap"><Tag style={{ marginInlineEnd: 0 }}>{protocolName(record.inbound_protocol)}</Tag><SwapRightOutlined className="text-[#7c8d86]" /><Tag color="orange" style={{ marginInlineEnd: 0 }}>{protocolName(record.upstream_protocol)}</Tag></Space> },
            { title: '请求方式', dataIndex: 'stream', width: 100, render: (value) => value ? <Tag color="processing">流式</Tag> : <Tag>非流式</Tag> },
            { title: '访问密钥', dataIndex: 'access_key_name', width: 140 },
            { title: '首内容', dataIndex: 'first_content_ms', width: 100, align: 'right', render: (value) => value == null ? '—' : `${value} ms` },
            { title: '总耗时', dataIndex: 'total_ms', width: 100, align: 'right', render: (value) => value == null ? '—' : `${value} ms` },
            { title: '状态', dataIndex: 'status', width: 160, align: 'right', render: (value, record) => <Space size={4} wrap><Tag color={value === 'completed' ? 'success' : value === 'in_progress' ? 'processing' : 'error'}>{statusName(value)}</Tag>{record.payload_pruned && <Tag>载荷已清理</Tag>}</Space> },
            { title: '', width: 48, render: (_, record) => <Button type="text" icon={<EyeOutlined />} aria-label={`查看 ${record.id}`} onClick={(event) => { event.stopPropagation(); openDetail(record.id) }} /> },
          ]}
          />
        </Card>
      </div>
      <RequestDrawer detail={detail.data} loading={detail.isPending} open={!!detailID} onClose={closeDetail} />
    </div>
  )
}

function positiveInteger(value: string | null, fallback: number): number {
  const parsed = Number(value)
  return Number.isInteger(parsed) && parsed > 0 ? parsed : fallback
}

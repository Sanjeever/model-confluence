import { useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useLocation, useNavigate, useParams } from 'react-router-dom'
import { Button, Card, Col, DatePicker, Empty, Input, Pagination, Row, Skeleton, Space, Table, Tag, Typography } from 'antd'
import { EyeOutlined, ReloadOutlined, SwapRightOutlined } from '@ant-design/icons'
import dayjs, { type Dayjs } from 'dayjs'
import { api, type Overview, type RequestDetail, type RequestPage } from '../api'
import RequestDrawer, { protocolName, statusName } from '../components/RequestDrawer'

const metrics: Array<{ key: keyof Overview; label: string }> = [
  { key: 'access_keys', label: '访问密钥' },
  { key: 'providers', label: '供应商' },
  { key: 'virtual_models', label: '虚拟模型' },
]

export default function OverviewPage() {
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(10)
  const [dateRange, setDateRange] = useState<[Dayjs, Dayjs]>(() => {
    const today = dayjs().startOf('day')
    return [today, today]
  })
  const [requestIDInput, setRequestIDInput] = useState('')
  const [requestID, setRequestID] = useState('')
  const { detailID } = useParams<{ detailID: string }>()
  const location = useLocation()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const createdFrom = dateRange[0].startOf('day').toISOString()
  const createdTo = dateRange[1].startOf('day').add(1, 'day').toISOString()
  const shouldPoll = dateRange[0].startOf('day').isBefore(dayjs().startOf('day').add(1, 'day')) && dateRange[1].startOf('day').add(1, 'day').isAfter(dayjs().startOf('day'))
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
    navigate(`/requests/${id}`, { state: { returnTo: location.pathname + location.search } })
  }

  function closeDetail() {
    const returnTo = (location.state as { returnTo?: string } | null)?.returnTo
    navigate(returnTo ?? '/requests')
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
            <Card className="relative overflow-hidden" styles={{ body: { minHeight: 132 } }}>
              <div className="absolute right-0 top-0 h-full w-1 bg-[#d7783d]" style={{ opacity: .25 + index * .15 }} />
              <div className="mb-6 text-sm text-[#7c8d86] sm:mb-8">{metric.label}</div>
              {overview.isPending ? <Skeleton.Input active size="large" /> : <div className="font-mono text-3xl font-medium sm:text-4xl">{overview.data?.[metric.key] ?? 0}</div>}
            </Card>
          </Col>
        ))}
      </Row>
      <div className="mt-6 hidden lg:mt-8 lg:block">
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
      <div className="mt-6 space-y-3 lg:hidden">
        {requests.isPending ? <Card><Skeleton active paragraph={{ rows: 3 }} /></Card> : (requests.data?.items ?? []).length ? (requests.data?.items ?? []).map((record) => (
          <Card key={record.id} size="small" className="cursor-pointer" onClick={() => openDetail(record.id)}>
            <div className="mb-3 flex items-start justify-between gap-3">
              <div className="min-w-0">
                <div className="text-xs text-[#7c8d86]">{new Date(record.created_at).toLocaleString()}</div>
                <code className="mt-1 block break-all text-xs">{record.id}</code>
              </div>
              <Space className="shrink-0" size={4} wrap><Tag color={record.status === 'completed' ? 'success' : record.status === 'in_progress' ? 'processing' : 'error'}>{statusName(record.status)}</Tag>{record.payload_pruned && <Tag>载荷已清理</Tag>}</Space>
            </div>
            <div className="mb-3 flex items-center justify-between gap-3">
              <code className="min-w-0 break-all text-sm font-medium">{record.virtual_model}</code>
              <span className="shrink-0 text-xs text-[#7c8d86]">{record.total_ms == null ? '—' : `${record.total_ms} ms`}</span>
            </div>
            <Space size={6} wrap>
              <Tag style={{ marginInlineEnd: 0 }}>{protocolName(record.inbound_protocol)}</Tag>
              <SwapRightOutlined className="text-[#7c8d86]" />
              <Tag color="orange" style={{ marginInlineEnd: 0 }}>{protocolName(record.upstream_protocol)}</Tag>
              <Tag color={record.stream ? 'processing' : undefined}>{record.stream ? '流式' : '非流式'}</Tag>
            </Space>
          </Card>
        )) : <Card><Empty className="py-8" description={requestID ? '没有匹配该请求 ID 的记录' : '所选时间范围内暂无请求记录'} /></Card>}
        {!!requests.data?.total && <div className="flex justify-center pt-2"><Pagination simple current={page} pageSize={pageSize} total={requests.data.total} onChange={(nextPage) => setPage(nextPage)} /></div>}
      </div>
      <RequestDrawer detail={detail.data} loading={detail.isPending} open={!!detailID} onClose={closeDetail} />
    </div>
  )
}

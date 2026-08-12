import { useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Button, Card, Col, Collapse, DatePicker, Descriptions, Drawer, Empty, Input, Pagination, Row, Skeleton, Space, Table, Tabs, Tag, Typography } from 'antd'
import { EyeOutlined, ReloadOutlined, SwapRightOutlined } from '@ant-design/icons'
import dayjs, { type Dayjs } from 'dayjs'
import { api, type Overview, type RequestDetail, type RequestPage } from '../api'
import JsonPayload from '../components/JsonPayload'

const metrics: Array<{ key: keyof Overview; label: string }> = [
  { key: 'request_count', label: '请求数' },
  { key: 'access_keys', label: '访问密钥' },
  { key: 'providers', label: '供应商' },
  { key: 'virtual_models', label: '虚拟模型' },
]

export default function OverviewPage({ data, loading }: { data?: Overview; loading: boolean }) {
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(10)
  const [dateRange, setDateRange] = useState<[Dayjs, Dayjs]>(() => {
    const today = dayjs().startOf('day')
    return [today, today]
  })
  const [requestIDInput, setRequestIDInput] = useState('')
  const [requestID, setRequestID] = useState('')
  const [detailID, setDetailID] = useState<string | null>(null)
  const queryClient = useQueryClient()
  const createdFrom = dateRange[0].startOf('day').toISOString()
  const createdTo = dateRange[1].startOf('day').add(1, 'day').toISOString()
  const shouldPoll = dateRange[0].startOf('day').isBefore(dayjs().startOf('day').add(1, 'day')) && dateRange[1].startOf('day').add(1, 'day').isAfter(dayjs().startOf('day'))
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
          <Col xs={12} xl={6} key={metric.key}>
            <Card className="relative overflow-hidden" styles={{ body: { minHeight: 132 } }}>
              <div className="absolute right-0 top-0 h-full w-1 bg-[#d7783d]" style={{ opacity: .25 + index * .15 }} />
              <div className="mb-6 text-sm text-[#7c8d86] sm:mb-8">{metric.label}</div>
              {loading ? <Skeleton.Input active size="large" /> : <div className="font-mono text-3xl font-medium sm:text-4xl">{data?.[metric.key] ?? 0}</div>}
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
          onRow={(record) => ({ onClick: () => setDetailID(record.id), className: 'cursor-pointer' })}
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
            { title: '状态', dataIndex: 'status', width: 90, align: 'right', render: (value) => <Tag color={value === 'completed' ? 'success' : value === 'in_progress' ? 'processing' : 'error'}>{statusName(value)}</Tag> },
            { title: '', width: 48, render: (_, record) => <Button type="text" icon={<EyeOutlined />} aria-label={`查看 ${record.id}`} onClick={(event) => { event.stopPropagation(); setDetailID(record.id) }} /> },
          ]}
          />
        </Card>
      </div>
      <div className="mt-6 space-y-3 lg:hidden">
        {requests.isPending ? <Card><Skeleton active paragraph={{ rows: 3 }} /></Card> : (requests.data?.items ?? []).length ? (requests.data?.items ?? []).map((record) => (
          <Card key={record.id} size="small" className="cursor-pointer" onClick={() => setDetailID(record.id)}>
            <div className="mb-3 flex items-start justify-between gap-3">
              <div className="min-w-0">
                <div className="text-xs text-[#7c8d86]">{new Date(record.created_at).toLocaleString()}</div>
                <code className="mt-1 block break-all text-xs">{record.id}</code>
              </div>
              <Tag className="shrink-0" color={record.status === 'completed' ? 'success' : record.status === 'in_progress' ? 'processing' : 'error'}>{statusName(record.status)}</Tag>
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
      <RequestDrawer detail={detail.data} loading={detail.isPending} open={!!detailID} onClose={() => setDetailID(null)} />
    </div>
  )
}

function RequestDrawer({ detail, loading, open, onClose }: { detail?: RequestDetail; loading: boolean; open: boolean; onClose: () => void }) {
  const [reveal, setReveal] = useState(false)
  return (
    <Drawer rootClassName="mc-request-drawer" title="请求轨迹" size="min(920px, 100vw)" open={open} onClose={() => { setReveal(false); onClose() }} loading={loading} extra={<Button size="small" onClick={() => setReveal((value) => !value)}>{reveal ? '遮罩凭据' : '显示原文'}</Button>}>
      {detail && <>
        <Descriptions size="small" column={{ xs: 1, sm: 2, lg: 3 }} className="mb-6" items={[
          { key: 'id', label: '请求 ID', children: <code className="break-all">{detail.id}</code> },
          { key: 'status', label: '状态', children: <Tag color={detail.status === 'completed' ? 'success' : 'error'}>{statusName(detail.status)}</Tag> },
          { key: 'model', label: '虚拟模型', children: <code>{detail.virtual_model}</code> },
          { key: 'protocol', label: '入站协议', children: protocolName(detail.inbound_protocol) },
          { key: 'upstream_protocol', label: '上游协议', children: protocolName(detail.upstream_protocol) },
          { key: 'stream', label: '请求方式', children: detail.stream ? <Tag color="processing">流式</Tag> : <Tag>非流式</Tag> },
          { key: 'key', label: '访问密钥', children: detail.access_key_name },
          { key: 'latency', label: '总耗时', children: detail.total_ms == null ? '—' : `${detail.total_ms} ms` },
          { key: 'time', label: '请求时间', children: new Date(detail.created_at).toLocaleString() },
          { key: 'input', label: '输入 Token', children: detail.input_tokens ?? '—' },
          { key: 'cache', label: '缓存读取', children: detail.cache_read_tokens ?? '—' },
          { key: 'output', label: '输出 Token', children: detail.output_tokens ?? '—' },
        ]} />
        <Tabs items={[
          { key: 'inbound', label: '入站请求', children: <PayloadPair headers={detail.request_headers} body={detail.request_body} reveal={reveal} /> },
          { key: 'response', label: '客户端响应', children: <PayloadPair headerLabel="响应头" headers={detail.response_headers} body={detail.response_body} reveal={reveal} /> },
          { key: 'attempts', label: `上游尝试 ${detail.attempts.length}`, children: detail.attempts.length ? <Collapse items={detail.attempts.map((attempt) => ({
            key: attempt.id,
            label: <Space wrap><span className="font-mono text-[#d7783d]">#{attempt.position + 1}</span><strong>{attempt.provider_name}</strong><code className="break-all">{attempt.upstream_model}</code><Tag>{protocolName(attempt.upstream_protocol)}</Tag><Tag color={attempt.status === 'completed' ? 'success' : 'error'}>{statusName(attempt.status)}</Tag></Space>,
            children: <div className="space-y-6"><Descriptions size="small" column={{ xs: 1, sm: 2, lg: 3 }} items={[
              { key: 'endpoint', label: '上游端点', span: 3, children: <code className="break-all">{attempt.upstream_endpoint}</code> },
              { key: 'key', label: '上游密钥', children: attempt.upstream_key_name || '未命名' },
              { key: 'status', label: '响应状态', children: attempt.response_status ?? '—' },
              { key: 'latency', label: '总耗时', children: attempt.total_ms == null ? '—' : `${attempt.total_ms} ms` },
            ]} /><PayloadPair title="发送到上游" headers={attempt.request_headers} body={attempt.request_body} reveal={reveal} /><PayloadPair title="上游响应" headerLabel="响应头" headers={attempt.response_headers} body={attempt.response_body} reveal={reveal} />{attempt.raw_usage_json && <div><div className="mb-3 text-sm font-medium text-[#7c8d86]">原始用量</div><JsonPayload value={attempt.raw_usage_json} /></div>}</div>,
          }))} /> : <Empty className="py-16" description="该请求在调用上游前失败，没有产生上游尝试" /> },
        ]} />
      </>}
    </Drawer>
  )
}

function PayloadPair({ title, headerLabel = '请求头', headers, body, reveal }: { title?: string; headerLabel?: string; headers: string; body: string; reveal: boolean }) {
  return <div>
    {title && <div className="mc-eyebrow mb-3 text-[#7c8d86]">{title}</div>}
    <Collapse size="small" className="mb-5" items={[{ key: 'headers', label: headerLabel, children: <JsonPayload value={reveal ? headers : maskHeaders(headers)} /> }]} />
    <div className="mb-2 text-xs font-medium text-[#7c8d86]">正文</div>
    <JsonPayload value={body} />
  </div>
}

function maskHeaders(value: string): string {
  if (!value) return ''
  try {
    const headers = JSON.parse(value) as Record<string, unknown>
    for (const name of Object.keys(headers)) {
      if (['authorization', 'x-api-key', 'cookie', 'set-cookie', 'api-key'].includes(name.toLowerCase())) headers[name] = ['••••••••']
    }
    return JSON.stringify(headers)
  } catch {
    return value
  }
}

function protocolName(value: string): string {
  if (!value) return '未调用'
  return ({ chat_completions: 'Chat Completions', responses: 'Responses', messages: 'Messages' } as Record<string, string>)[value] ?? value
}

function statusName(value: string): string {
  return ({ completed: '已完成', in_progress: '进行中', failed: '失败', cancelled: '已取消' } as Record<string, string>)[value] ?? value
}

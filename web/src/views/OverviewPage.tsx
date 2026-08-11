import { useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Button, Card, Col, Collapse, Descriptions, Drawer, Empty, Input, Row, Skeleton, Space, Table, Tabs, Tag, Typography } from 'antd'
import { EyeOutlined, ReloadOutlined, SwapRightOutlined } from '@ant-design/icons'
import { api, type Overview, type RequestDetail, type RequestPage } from '../api'

const metrics: Array<{ key: keyof Overview; label: string }> = [
  { key: 'requests_today', label: '今日请求' },
  { key: 'access_keys', label: '访问密钥' },
  { key: 'providers', label: '供应商' },
  { key: 'virtual_models', label: '虚拟模型' },
]

export default function OverviewPage({ data, loading }: { data?: Overview; loading: boolean }) {
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(10)
  const [requestIDInput, setRequestIDInput] = useState('')
  const [requestID, setRequestID] = useState('')
  const [detailID, setDetailID] = useState<string | null>(null)
  const queryClient = useQueryClient()
  const requests = useQuery({
    queryKey: ['requests', page, pageSize, requestID],
    queryFn: () => api<RequestPage>(`/api/admin/requests?page=${page}&page_size=${pageSize}&request_id=${encodeURIComponent(requestID)}`),
    refetchInterval: 5000,
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
      <div className="mb-10 flex items-end justify-between">
        <Typography.Title level={2} className="!mb-0 !tracking-[-.04em]">使用记录</Typography.Title>
        <Space><Input.Search allowClear enterButton="搜索" placeholder="输入请求 ID" className="w-[360px]" value={requestIDInput} onChange={(event) => { const value = event.target.value; setRequestIDInput(value); if (!value) { setRequestID(''); setPage(1) } }} onSearch={(value) => { setRequestID(value.trim()); setPage(1) }} /><Button icon={<ReloadOutlined />} loading={requests.isFetching} onClick={refresh}>刷新</Button></Space>
      </div>
      <Row gutter={16}>
        {metrics.map((metric, index) => (
          <Col span={6} key={metric.key}>
            <Card className="relative overflow-hidden" styles={{ body: { minHeight: 156 } }}>
              <div className="absolute right-0 top-0 h-full w-1 bg-[#d7783d]" style={{ opacity: .25 + index * .15 }} />
              <div className="mb-8 text-sm text-[#7c8d86]">{metric.label}</div>
              {loading ? <Skeleton.Input active size="large" /> : <div className="font-mono text-4xl font-medium tracking-[-.05em]">{data?.[metric.key] ?? 0}</div>}
            </Card>
          </Col>
        ))}
      </Row>
      <div style={{ marginTop: 32 }}>
        <Card className="overflow-hidden" styles={{ body: { padding: 0 } }}>
          <Table
          rowKey="id"
          loading={requests.isPending}
          dataSource={requests.data?.items ?? []}
          scroll={{ x: 1380, y: 'calc(100vh - 430px)' }}
          onRow={(record) => ({ onClick: () => setDetailID(record.id), className: 'cursor-pointer' })}
          pagination={{ current: page, pageSize, total: requests.data?.total ?? 0, showSizeChanger: true, pageSizeOptions: [10, 20, 50], onChange: (nextPage, nextPageSize) => { setPage(nextPageSize === pageSize ? nextPage : 1); setPageSize(nextPageSize) }, showTotal: (total) => `共 ${total} 条` }}
          locale={{ emptyText: <Empty className="py-14" description={requestID ? '没有匹配该请求 ID 的记录' : '创建访问密钥、供应商和模型路由后，请求记录会出现在这里'} /> }}
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
      <RequestDrawer detail={detail.data} loading={detail.isPending} open={!!detailID} onClose={() => setDetailID(null)} />
    </div>
  )
}

function RequestDrawer({ detail, loading, open, onClose }: { detail?: RequestDetail; loading: boolean; open: boolean; onClose: () => void }) {
  const [reveal, setReveal] = useState(false)
  return (
    <Drawer title={<span>请求轨迹 <code className="ml-2 text-xs">{detail?.id}</code></span>} width={920} open={open} onClose={() => { setReveal(false); onClose() }} loading={loading} extra={<Button onClick={() => setReveal((value) => !value)}>{reveal ? '遮罩凭据' : '显示原文'}</Button>}>
      {detail && <>
        <Descriptions size="small" column={3} className="mb-6" items={[
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
            label: <Space><span className="font-mono text-[#d7783d]">#{attempt.position + 1}</span><strong>{attempt.provider_name}</strong><code>{attempt.upstream_model}</code><Tag>{protocolName(attempt.upstream_protocol)}</Tag><Tag color={attempt.status === 'completed' ? 'success' : 'error'}>{statusName(attempt.status)}</Tag></Space>,
            children: <div className="space-y-6"><Descriptions size="small" column={3} items={[
              { key: 'endpoint', label: '上游端点', span: 3, children: <code className="break-all">{attempt.upstream_endpoint}</code> },
              { key: 'key', label: '上游密钥', children: attempt.upstream_key_name || '未命名' },
              { key: 'status', label: '响应状态', children: attempt.response_status ?? '—' },
              { key: 'latency', label: '总耗时', children: attempt.total_ms == null ? '—' : `${attempt.total_ms} ms` },
            ]} /><PayloadPair title="发送到上游" headers={attempt.request_headers} body={attempt.request_body} reveal={reveal} /><PayloadPair title="上游响应" headerLabel="响应头" headers={attempt.response_headers} body={attempt.response_body} reveal={reveal} />{attempt.raw_usage_json && <div><div className="mb-3 text-sm font-medium text-[#7c8d86]">原始用量</div><Payload value={attempt.raw_usage_json} /></div>}</div>,
          }))} /> : <Empty className="py-16" description="该请求在调用上游前失败，没有产生上游尝试" /> },
        ]} />
      </>}
    </Drawer>
  )
}

function PayloadPair({ title, headerLabel = '请求头', headers, body, reveal }: { title?: string; headerLabel?: string; headers: string; body: string; reveal: boolean }) {
  return <div>
    {title && <div className="mc-eyebrow mb-3 text-[#7c8d86]">{title}</div>}
    <Collapse size="small" className="mb-5" items={[{ key: 'headers', label: headerLabel, children: <Payload value={reveal ? headers : maskHeaders(headers)} /> }]} />
    <div className="mb-2 text-xs font-medium text-[#7c8d86]">正文</div>
    <Payload value={body} />
  </div>
}

function Payload({ value }: { value: string }) {
  return <pre className="max-h-[420px] overflow-auto whitespace-pre-wrap break-all rounded border border-black/10 bg-black/[.025] p-4 font-mono text-xs leading-5 dark:border-white/10 dark:bg-white/[.03]">{formatJSON(value) || '—'}</pre>
}

function formatJSON(value: string): string {
  if (!value) return ''
  try { return JSON.stringify(JSON.parse(value), null, 2) } catch { return value }
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

import { useQuery } from '@tanstack/react-query'
import { Alert, Button, Card, Col, Empty, Row, Skeleton, Space, Table, Tag, Typography } from 'antd'
import { ReloadOutlined } from '@ant-design/icons'
import { api, type CandidateHealth, type HealthOverview, type UpstreamKeyHealth } from '../api'
import MetricCard from '../components/MetricCard'
import { formatCount } from '../format'

const keyStatusMeta: Record<string, { label: string; color: string }> = {
  available: { label: '可用', color: 'success' },
  auth_invalid: { label: '鉴权失效', color: 'error' },
  quota_exhausted: { label: '额度耗尽', color: 'error' },
  rate_limited: { label: '限流冷却', color: 'warning' },
}

export default function HealthPage() {
  const health = useQuery({
    queryKey: ['health'],
    queryFn: () => api<HealthOverview>('/api/admin/health'),
    refetchInterval: 10000,
  })

  return (
    <div className="mc-enter mx-auto max-w-[1500px]">
      <div className="mb-6 flex items-center justify-between gap-3 sm:mb-8">
        <Typography.Title level={2} className="!mb-0 !tracking-[-.04em]">上游健康</Typography.Title>
        <Button icon={<ReloadOutlined />} loading={health.isFetching} onClick={() => health.refetch()}>刷新</Button>
      </div>

      {health.isError && <Alert className="mb-6" type="error" showIcon message="健康数据加载失败" description={(health.error as Error).message} />}

      <Row gutter={[12, 12]}>
        <Col xs={12} xl={8}><MetricCard label="异常密钥" value={formatCount(health.data?.abnormal_key_count)} opacity={.25} loading={health.isPending} /></Col>
        <Col xs={12} xl={8}><MetricCard label="候选近期失败" value={formatCount(health.data?.failed_candidates)} opacity={.55} loading={health.isPending} /></Col>
        <Col xs={12} xl={8}><MetricCard label="无可用路由模型" value={formatCount(health.data?.unrouted_models.length)} opacity={.85} loading={health.isPending} /></Col>
      </Row>

      <Typography.Title level={4} className="!mb-0 !mt-8 !tracking-[-.03em]">密钥池状态</Typography.Title>
      <Card className="mt-4 overflow-hidden" styles={{ body: { padding: 0 } }}>
        <Table
          rowKey="id"
          loading={health.isPending}
          dataSource={health.data?.keys ?? []}
          scroll={{ x: 1000 }}
          pagination={{ pageSize: 10, showSizeChanger: true, pageSizeOptions: [10, 20, 50] }}
          locale={{ emptyText: <Empty className="py-14" description="暂无上游密钥" /> }}
          columns={[
            { title: '供应商', dataIndex: 'provider_name', render: (value) => <strong>{value}</strong> },
            { title: '密钥', width: 200, render: (_, record) => <span className="min-w-0 break-all">{record.name || `密钥 ${record.position + 1}`}</span> },
            { title: '状态', width: 120, render: (_, record) => <KeyStatusTag record={record} /> },
            { title: '恢复时间', dataIndex: 'recover_at', width: 170, render: (value) => value ? <span className="font-mono text-xs">{new Date(value).toLocaleString()}</span> : '—' },
            { title: '过期时间', dataIndex: 'expires_at', width: 170, render: (value) => value ? <span className="font-mono text-xs">{new Date(value).toLocaleString()}</span> : '—' },
            { title: '最后使用', dataIndex: 'last_used_at', width: 170, render: (value) => value ? <span className="font-mono text-xs">{new Date(value).toLocaleString()}</span> : '—' },
            { title: '备注', dataIndex: 'runtime_reason', ellipsis: true, render: (value) => value ? <code className="text-xs">{value}</code> : '—' },
          ]}
        />
      </Card>

      <Typography.Title level={4} className="!mb-0 !mt-8 !tracking-[-.03em]">候选近期失败</Typography.Title>
      <Card className="mt-4 overflow-hidden" styles={{ body: { padding: 0 } }}>
        <Table
          rowKey="candidate_id"
          loading={health.isPending}
          dataSource={health.data?.candidates ?? []}
          scroll={{ x: 900 }}
          pagination={false}
          locale={{ emptyText: <Empty className="py-14" description="近期没有失败的候选" /> }}
          columns={[
            { title: '虚拟模型', dataIndex: 'virtual_model', render: (value) => <code>{value || '—'}</code> },
            { title: '真实模型', dataIndex: 'upstream_model', render: (value) => <code className="break-all">{value || '—'}</code> },
            { title: '供应商', dataIndex: 'provider_name', width: 160, render: (value) => value || '—' },
            { title: '失败次数', dataIndex: 'failed_count', width: 100, align: 'right' },
            { title: '最近失败时间', dataIndex: 'last_failed_at', width: 170, render: (value) => <span className="font-mono text-xs">{new Date(value).toLocaleString()}</span> },
            { title: '最近失败原因', dataIndex: 'last_failure', ellipsis: true, render: (value) => <code className="text-xs">{value}</code> },
          ]}
        />
      </Card>

      <Typography.Title level={4} className="!mb-0 !mt-8 !tracking-[-.03em]">无可用路由</Typography.Title>
      <Card className="mt-4" styles={{ body: { padding: 16 } }}>
        {health.isPending ? <Skeleton active paragraph={{ rows: 1 }} /> : health.data?.unrouted_models.length ? <Space wrap>{health.data.unrouted_models.map((name) => <Tag color="error" key={name}><code>{name}</code></Tag>)}</Space> : <Empty className="py-6" description="所有启用模型当前均有可用路由" />}
      </Card>
    </div>
  )
}

function KeyStatusTag({ record }: { record: UpstreamKeyHealth }) {
  if (!record.enabled) return <Tag>已停用</Tag>
  const status = keyStatusMeta[record.runtime_status] ?? { label: record.runtime_status, color: 'default' as const }
  return <Tag color={status.color}>{status.label}</Tag>
}

import { useMemo } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import type { ColumnDef } from '@tanstack/react-table'
import { RefreshCw } from 'lucide-react'
import { Link } from 'react-router-dom'
import {
  api,
  type CandidateHealth,
  type HealthOverview,
  type UpstreamKeyHealth,
} from '../api'
import { formatCount } from '../format'
import DataTable from '../components/DataTable'
import MetricCard from '../components/MetricCard'
import { Status } from '../components/Status'
import {
  ConfirmButton,
  ErrorNotice,
  PageHeader,
  displayDate,
} from '../components/console'
import { Button } from '@/components/ui/button'

type ModelCooldown = HealthOverview['model_cooldowns'][number]
export default function HealthPage() {
  const client = useQueryClient()
  const health = useQuery({
    queryKey: ['health'],
    queryFn: () => api<HealthOverview>('/api/admin/health'),
    refetchInterval: 10000,
  })
  const reset = useMutation({
    mutationFn: (id: number) =>
      api(`/api/admin/upstream-keys/${id}/reset-runtime`, { method: 'POST' }),
    onSuccess: () => {
      client.invalidateQueries({ queryKey: ['health'] })
      client.invalidateQueries({ queryKey: ['providers'] })
    },
  })
  const keys = useMemo<ColumnDef<UpstreamKeyHealth>[]>(
    () => [
      {
        accessorKey: 'provider_name',
        header: '供应商',
        cell: ({ row }) => (
          <span className="font-medium">{row.original.provider_name}</span>
        ),
      },
      {
        accessorKey: 'name',
        header: '密钥 / 账号',
        cell: ({ row }) => (
          <span className="block max-w-44 truncate" title={row.original.name}>
            {row.original.name || `密钥 ${row.original.position + 1}`}
          </span>
        ),
      },
      {
        accessorKey: 'enabled',
        header: '管理员状态',
        size: 100,
        cell: ({ row }) => (
          <span className="text-xs text-muted-foreground">
            {row.original.enabled ? '已启用' : '已停用'}
          </span>
        ),
      },
      {
        accessorKey: 'runtime_status',
        header: '运行状态',
        size: 110,
        cell: ({ row }) => <Status value={row.original.runtime_status} />,
      },
      {
        accessorKey: 'recover_at',
        header: '恢复时间',
        size: 160,
        cell: ({ row }) => (
          <span className="text-xs tabular-nums">
            {displayDate(row.original.recover_at)}
          </span>
        ),
      },
      {
        id: 'expires',
        header: '到期时间',
        size: 165,
        cell: ({ row }) => (
          <div className="text-xs tabular-nums">
            {displayDate(
              row.original.provider_kind === 'generic'
                ? row.original.expires_at
                : row.original.access_token_expires_at,
            )}
            {row.original.provider_kind !== 'generic' && (
              <span className="mt-1 block text-[11px] text-muted-foreground">
                访问令牌
              </span>
            )}
          </div>
        ),
      },
      {
        accessorKey: 'last_used_at',
        header: '最后使用',
        size: 160,
        cell: ({ row }) => (
          <span className="text-xs tabular-nums">
            {displayDate(row.original.last_used_at)}
          </span>
        ),
      },
      {
        accessorKey: 'runtime_reason',
        header: '原因',
        cell: ({ row }) => (
          <code
            title={row.original.runtime_reason}
            className="block max-w-44 truncate text-xs text-muted-foreground"
          >
            {row.original.runtime_reason || '—'}
          </code>
        ),
      },
      {
        id: 'action',
        header: '',
        size: 100,
        cell: ({ row }) =>
          row.original.provider_kind !== 'generic' &&
          row.original.runtime_status === 'auth_invalid' ? (
            <span className="text-xs text-muted-foreground">需重新导入</span>
          ) : row.original.enabled &&
            row.original.runtime_status !== 'available' ? (
            <ConfirmButton
              title="重置密钥运行状态？"
              description="请确认供应商侧已恢复额度或服务。管理员启停状态保持不变。"
              destructive={false}
              pending={reset.isPending}
              onConfirm={() => reset.mutate(row.original.id)}
            >
              <Button
                variant="ghost"
                size="sm"
                disabled={
                  reset.isPending && reset.variables === row.original.id
                }
              >
                重置状态
              </Button>
            </ConfirmButton>
          ) : null,
      },
    ],
    [reset.isPending, reset.variables],
  )
  const candidates = useMemo<ColumnDef<CandidateHealth>[]>(
    () => [
      {
        accessorKey: 'virtual_model',
        header: '虚拟模型',
        cell: ({ row }) => (
          <code className="text-xs">{row.original.virtual_model || '—'}</code>
        ),
      },
      {
        accessorKey: 'upstream_model',
        header: '真实模型',
        cell: ({ row }) => (
          <code
            className="block max-w-48 truncate text-xs"
            title={row.original.upstream_model}
          >
            {row.original.upstream_model || '—'}
          </code>
        ),
      },
      { accessorKey: 'provider_name', header: '供应商' },
      {
        accessorKey: 'failed_count',
        header: '失败次数',
        size: 90,
        meta: { align: 'right' },
      },
      {
        accessorKey: 'last_failed_at',
        header: '最近失败',
        size: 160,
        cell: ({ row }) => (
          <span className="text-xs tabular-nums">
            {displayDate(row.original.last_failed_at)}
          </span>
        ),
      },
      {
        accessorKey: 'last_failure',
        header: '最近失败原因',
        size: 240,
        cell: ({ row }) => (
          <code
            title={row.original.last_failure}
            className="block max-w-64 truncate text-xs text-muted-foreground"
          >
            {row.original.last_failure}
          </code>
        ),
      },
    ],
    [],
  )
  const cooldowns = useMemo<ColumnDef<ModelCooldown>[]>(
    () => [
      { accessorKey: 'provider_name', header: '供应商' },
      { accessorKey: 'account_name', header: '账号' },
      {
        accessorKey: 'upstream_model',
        header: '真实模型',
        cell: ({ row }) => (
          <code className="text-xs">{row.original.upstream_model}</code>
        ),
      },
      { accessorKey: 'reason', header: '原因' },
      {
        accessorKey: 'recover_at',
        header: '恢复时间',
        cell: ({ row }) => (
          <span className="text-xs tabular-nums">
            {displayDate(row.original.recover_at)}
          </span>
        ),
      },
      {
        id: 'actions',
        header: '',
        size: 100,
        cell: ({ row }) => (
          <ConfirmButton
            title="清除此账号的模型冷却？"
            description="将重置该账号的运行状态并清除其模型冷却。"
            destructive={false}
            pending={reset.isPending}
            onConfirm={() => reset.mutate(row.original.upstream_key_id)}
          >
            <Button size="sm" variant="ghost" disabled={reset.isPending}>
              重置状态
            </Button>
          </ConfirmButton>
        ),
      },
    ],
    [reset.isPending],
  )
  return (
    <>
      <PageHeader title="上游健康">
        <Button
          variant="outline"
          size="icon"
          aria-label="刷新健康数据"
          disabled={health.isFetching}
          onClick={() => health.refetch()}
        >
          <RefreshCw className={health.isFetching ? 'animate-spin' : ''} />
        </Button>
      </PageHeader>
      <ErrorNotice error={health.error} retry={() => health.refetch()} />
      <ErrorNotice error={reset.error} />
      {!!health.data?.candidate_cooldowns?.length && <section className="mb-5 rounded-md border p-4"><h2 className="mb-3 text-sm font-medium">候选实时冷却</h2><div className="divide-y">{health.data.candidate_cooldowns.map((item) => <div key={item.candidate_id} className="flex flex-wrap gap-4 py-2 text-xs"><span>候选 {item.candidate_id}</span><span>连续失败 {item.failures} 次</span><span>{item.probing ? '正在探测' : new Date(item.recover_at) > new Date() ? `冷却至 ${displayDate(item.recover_at)}` : '等待下一请求探测'}</span></div>)}</div></section>}
      <div className="metric-strip lg:grid-cols-3">
        <MetricCard
          label="异常密钥"
          value={formatCount(health.data?.abnormal_key_count)}
          loading={health.isPending}
        />
        <MetricCard
          label="候选近期失败"
          value={formatCount(health.data?.failed_candidates)}
          loading={health.isPending}
        />
        <MetricCard
          label="无可用路由模型"
          value={formatCount(health.data?.unrouted_models.length)}
          loading={health.isPending}
        />
      </div>
      {!!health.data?.unrouted_models.length && (
        <section className="mb-6 rounded-md border border-destructive/25 bg-destructive/5 px-4 py-3">
          <h2 className="mb-2 text-xs font-medium text-destructive">
            无可用路由
          </h2>
          <div className="flex flex-wrap gap-x-4 gap-y-2">
            {health.data.unrouted_models.map((name) => (
              <Link
                key={name}
                to={`/models?name=${encodeURIComponent(name)}`}
                className="text-link font-mono text-xs"
              >
                {name}
              </Link>
            ))}
          </div>
        </section>
      )}
      <section>
        <h2 className="section-heading">密钥池状态</h2>
        <DataTable
          data={health.data?.keys ?? []}
          columns={keys}
          rowID={(row) => String(row.id)}
          loading={health.isPending}
          fetching={health.isFetching}
          empty="暂无上游密钥"
          minWidth={1140}
        />
      </section>
      {!!health.data?.model_cooldowns.length && (
        <section className="section">
          <h2 className="section-heading">模型冷却</h2>
          <DataTable
            data={health.data.model_cooldowns}
            columns={cooldowns}
            rowID={(row) => `${row.upstream_key_id}:${row.upstream_model}`}
          />
        </section>
      )}
      <section className="section">
        <h2 className="section-heading">候选近期失败</h2>
        <DataTable
          data={health.data?.candidates ?? []}
          columns={candidates}
          rowID={(row) => String(row.candidate_id)}
          loading={health.isPending}
          empty="近期没有失败的候选"
          minWidth={960}
        />
      </section>
    </>
  )
}

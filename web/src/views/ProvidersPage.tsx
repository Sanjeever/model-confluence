import {
  keepPreviousData,
  useMutation,
  useQuery,
  useQueryClient,
} from '@tanstack/react-query'
import { Link, useLocation } from 'react-router-dom'
import type { ColumnDef } from '@tanstack/react-table'
import { Pencil, Plus, Trash2 } from 'lucide-react'
import { api, type DeleteResult, type Page, type Provider } from '../api'
import DataTable from '../components/DataTable'
import OAuthAccounts from '../components/OAuthAccounts'
import { Status, protocolName } from '../components/Status'
import {
  ConfirmButton,
  CopyButton,
  ErrorNotice,
  FilterSelect,
  PageHeader,
  SearchField,
} from '../components/console'
import { Button } from '@/components/ui/button'
import { Switch } from '@/components/ui/switch'
import { useCatalogFilters } from '@/hooks/use-catalog-filters'

export default function ProvidersPage() {
  const filters = useCatalogFilters()
  const client = useQueryClient()
  const location = useLocation()
  const providers = useQuery({
    queryKey: [
      'providers',
      filters.page,
      filters.pageSize,
      filters.name,
      filters.enabled,
      filters.authType,
    ],
    queryFn: () => {
      const query = new URLSearchParams({
        page: String(filters.page),
        page_size: String(filters.pageSize),
      })
      if (filters.name) query.set('name', filters.name)
      if (filters.enabled) query.set('enabled', filters.enabled)
      if (filters.authType) query.set('auth_type', filters.authType)
      return api<Page<Provider>>(`/api/admin/providers?${query}`)
    },
    placeholderData: keepPreviousData,
  })
  function refresh() {
    client.invalidateQueries({ queryKey: ['providers'] })
    client.invalidateQueries({ queryKey: ['provider-options'] })
    client.invalidateQueries({ queryKey: ['health'] })
  }
  const toggle = useMutation({
    mutationFn: ({ id, enabled }: { id: number; enabled: boolean }) =>
      api(`/api/admin/providers/${id}`, {
        method: 'PATCH',
        body: JSON.stringify({ enabled }),
      }),
    onSuccess: refresh,
  })
  const remove = useMutation({
    mutationFn: (id: number) =>
      api<DeleteResult>(`/api/admin/providers/${id}`, { method: 'DELETE' }),
    onSuccess: refresh,
  })
  const editor = (id: number) => `/providers/${id}/edit${location.search}`
  const columns: ColumnDef<Provider>[] = [
    {
      accessorKey: 'name',
      header: '供应商',
      cell: ({ row }) => (
        <Link className="text-link font-medium" to={editor(row.original.id)}>
          {row.original.name}
        </Link>
      ),
    },
    {
      accessorKey: 'auth_type',
      header: '鉴权',
      size: 170,
      cell: ({ row }) => (
        <span className="text-xs text-muted-foreground">
          {row.original.kind === 'codex_oauth'
            ? 'Codex OAuth'
            : row.original.kind === 'antigravity_oauth'
              ? 'Antigravity OAuth · 实验性'
              : row.original.auth_type === 'bearer'
                ? 'Bearer'
                : row.original.auth_type === 'custom'
                  ? row.original.auth_header
                  : 'x-api-key'}
        </span>
      ),
    },
    {
      accessorKey: 'endpoints',
      header: '协议',
      size: 300,
      cell: ({ row }) => (
        <span className="text-xs text-muted-foreground">
          {Object.keys(row.original.endpoints).map(protocolName).join(' · ') ||
            '—'}
        </span>
      ),
    },
    {
      accessorKey: 'keys',
      header: '凭据池',
      size: 110,
      cell: ({ row }) => (
        <span className="text-xs tabular-nums">
          {row.original.keys.length}{' '}
          {row.original.kind === 'generic' ? '把密钥' : '个账号'}
        </span>
      ),
    },
    {
      accessorKey: 'enabled',
      header: '启用',
      size: 70,
      cell: ({ row }) => (
        <Switch
          aria-label={`启用 ${row.original.name}`}
          checked={row.original.enabled}
          disabled={
            toggle.isPending && toggle.variables?.id === row.original.id
          }
          onCheckedChange={(enabled) =>
            toggle.mutate({ id: row.original.id, enabled })
          }
        />
      ),
    },
    {
      id: 'actions',
      header: '',
      size: 85,
      cell: ({ row }) => (
        <div className="flex justify-end gap-1">
          <Button variant="ghost" size="icon-sm" asChild>
            <Link
              to={editor(row.original.id)}
              aria-label={`编辑 ${row.original.name}`}
            >
              <Pencil />
            </Link>
          </Button>
          <ConfirmButton
            title="删除供应商？"
            description="仍被模型路由使用时将拒绝删除。"
            pending={remove.isPending}
            onConfirm={() => remove.mutate(row.original.id)}
          >
            <Button
              variant="ghost"
              size="icon-sm"
              className="text-muted-foreground hover:text-destructive"
              aria-label={`删除 ${row.original.name}`}
            >
              <Trash2 />
            </Button>
          </ConfirmButton>
        </div>
      ),
    },
  ]
  return (
    <>
      <PageHeader title="供应商">
        <Button asChild>
          <Link to={`/providers/new${location.search}`}>
            <Plus />
            新增供应商
          </Link>
        </Button>
      </PageHeader>
      <div className="filter-bar">
        <SearchField
          key={filters.name}
          value={filters.name}
          placeholder="搜索名称"
          onChange={(name) => filters.update({ name })}
        />
        <FilterSelect
          value={filters.enabled}
          placeholder="状态"
          options={[
            { value: 'true', label: '已启用' },
            { value: 'false', label: '已停用' },
          ]}
          onChange={(enabled) => filters.update({ enabled })}
        />
        <FilterSelect
          value={filters.authType}
          placeholder="鉴权方式"
          options={[
            { value: 'bearer', label: 'Bearer' },
            { value: 'x-api-key', label: 'x-api-key' },
            { value: 'custom', label: '自定义请求头' },
            { value: 'oauth', label: 'OAuth' },
          ]}
          onChange={(auth_type) => filters.update({ auth_type })}
        />
      </div>
      <ErrorNotice error={providers.error} retry={() => providers.refetch()} />
      <ErrorNotice error={toggle.error ?? remove.error} />
      <DataTable
        data={providers.data?.items ?? []}
        columns={columns}
        rowID={(row) => String(row.id)}
        loading={providers.isPending}
        fetching={providers.isFetching}
        total={providers.data?.total ?? 0}
        pagination={filters.pagination}
        onPaginationChange={filters.onPaginationChange}
        empty={
          filters.name || filters.enabled || filters.authType
            ? '没有匹配的供应商'
            : '暂无供应商'
        }
        minWidth={920}
        expanded={(record) =>
          record.kind !== 'generic' ? (
            <OAuthAccounts provider={record} />
          ) : (
            <ProviderDetails provider={record} />
          )
        }
      />
    </>
  )
}
function ProviderDetails({ provider }: { provider: Provider }) {
  return (
    <div className="grid gap-6 lg:grid-cols-2">
      <section className="min-w-0">
        <h2 className="section-heading">协议端点</h2>
        <div className="space-y-3">
          {Object.entries(provider.endpoints).map(([key, value]) => (
            <div key={key}>
              <span className="block text-xs text-muted-foreground">
                {protocolName(key)}
              </span>
              <code className="mt-1 block break-all text-xs">{value}</code>
            </div>
          ))}
        </div>
      </section>
      <section className="min-w-0">
        <h2 className="section-heading">上游密钥池</h2>
        <div className="divide-y">
          {provider.keys.map((key) => (
            <div key={key.id} className="space-y-2 py-2 first:pt-0">
              <div className="flex items-center justify-between gap-2">
                <span className="text-xs font-medium">
                  {key.name || `密钥 ${key.position + 1}`}
                </span>
                <div className="flex items-center gap-3">
                  <span className="text-xs text-muted-foreground">
                    {key.enabled ? '已启用' : '已停用'}
                  </span>
                  <Status value={key.runtime_status} />
                </div>
              </div>
              <div className="flex items-center gap-1">
                <code
                  className="min-w-0 flex-1 truncate text-xs"
                  title={key.secret}
                >
                  {key.secret || '—'}
                </code>
                <CopyButton value={key.secret ?? ''} label="复制上游密钥" />
              </div>
            </div>
          ))}
        </div>
      </section>
    </div>
  )
}

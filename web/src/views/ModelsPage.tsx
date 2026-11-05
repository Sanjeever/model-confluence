import { useState } from 'react'
import {
  keepPreviousData,
  useMutation,
  useQuery,
  useQueryClient,
} from '@tanstack/react-query'
import { Link, useLocation } from 'react-router-dom'
import type { ColumnDef } from '@tanstack/react-table'
import { FlaskConical, Loader2, Pencil, Plus, Trash2 } from 'lucide-react'
import {
  api,
  type DeleteResult,
  type ModelTestResult,
  type Page,
  type ProviderOption,
  type VirtualModel,
} from '../api'
import DataTable from '../components/DataTable'
import JsonPayload from '../components/JsonPayload'
import { protocolName } from '../components/Status'
import {
  ConfirmButton,
  ErrorNotice,
  FilterSelect,
  PageHeader,
  SearchField,
} from '../components/console'
import { Button } from '@/components/ui/button'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import { Label } from '@/components/ui/label'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { useCatalogFilters } from '@/hooks/use-catalog-filters'

export default function ModelsPage() {
  const filters = useCatalogFilters()
  const location = useLocation()
  const client = useQueryClient()
  const [testing, setTesting] = useState<VirtualModel | null>(null)
  const [prompt, setPrompt] = useState('请简短回复：模型连接正常')
  const providers = useQuery({
    queryKey: ['provider-options'],
    queryFn: () => api<ProviderOption[]>('/api/admin/provider-options'),
  })
  const models = useQuery({
    queryKey: [
      'models',
      filters.page,
      filters.pageSize,
      filters.name,
      filters.enabled,
    ],
    queryFn: () => {
      const query = new URLSearchParams({
        page: String(filters.page),
        page_size: String(filters.pageSize),
      })
      if (filters.name) query.set('name', filters.name)
      if (filters.enabled) query.set('enabled', filters.enabled)
      return api<Page<VirtualModel>>(`/api/admin/models?${query}`)
    },
    placeholderData: keepPreviousData,
  })
  function refresh() {
    client.invalidateQueries({ queryKey: ['models'] })
    client.invalidateQueries({ queryKey: ['model-names'] })
    client.invalidateQueries({ queryKey: ['health'] })
  }
  const toggle = useMutation({
    mutationFn: ({ id, enabled }: { id: number; enabled: boolean }) =>
      api(`/api/admin/models/${id}`, {
        method: 'PATCH',
        body: JSON.stringify({ enabled }),
      }),
    onSuccess: refresh,
  })
  const remove = useMutation({
    mutationFn: (id: number) =>
      api<DeleteResult>(`/api/admin/models/${id}`, { method: 'DELETE' }),
    onSuccess: refresh,
  })
  const test = useMutation({
    mutationFn: ({ id, prompt }: { id: number; prompt: string }) =>
      api<ModelTestResult>(`/api/admin/models/${id}/test`, {
        method: 'POST',
        body: JSON.stringify({ prompt }),
      }),
    onSuccess: () => {
      client.invalidateQueries({ queryKey: ['requests'] })
      client.invalidateQueries({ queryKey: ['performance'] })
    },
  })
  const editor = (id: number) => `/models/${id}/edit${location.search}`
  const columns: ColumnDef<VirtualModel>[] = [
    {
      accessorKey: 'name',
      header: '虚拟模型',
      cell: ({ row }) => (
        <Link
          className="text-link font-mono text-xs font-medium"
          to={editor(row.original.id)}
        >
          {row.original.name}
        </Link>
      ),
    },
    {
      accessorKey: 'candidates',
      header: '候选',
      size: 130,
      cell: ({ row }) => (
        <span className="text-xs text-muted-foreground">
          {row.original.candidates.length} 条有序路由
        </span>
      ),
    },
    {
      id: 'preferred',
      header: '首选供应商',
      cell: ({ row }) => row.original.candidates[0]?.provider_name ?? '—',
    },
    {
      accessorKey: 'enabled',
      header: '启用',
      size: 80,
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
      size: 125,
      cell: ({ row }) => (
        <div className="flex justify-end gap-1">
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label={`测试 ${row.original.name}`}
            title={row.original.enabled ? '测试模型' : '启用后可测试'}
            disabled={!row.original.enabled}
            onClick={() => {
              test.reset()
              setTesting(row.original)
            }}
          >
            <FlaskConical />
          </Button>
          <Button variant="ghost" size="icon-sm" asChild>
            <Link
              to={editor(row.original.id)}
              aria-label={`编辑 ${row.original.name}`}
            >
              <Pencil />
            </Link>
          </Button>
          <ConfirmButton
            title="删除模型路由？"
            description="已有历史请求时将转为归档。"
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
      <PageHeader title="模型路由">
        <Button asChild disabled={!providers.data?.length}>
          <Link
            to={`/models/new${location.search}`}
            aria-disabled={!providers.data?.length}
            onClick={(event) => {
              if (!providers.data?.length) event.preventDefault()
            }}
          >
            <Plus />
            新增虚拟模型
          </Link>
        </Button>
      </PageHeader>
      <div className="filter-bar">
        <SearchField
          key={filters.name}
          value={filters.name}
          placeholder="搜索虚拟模型"
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
      </div>
      <ErrorNotice error={models.error} retry={() => models.refetch()} />
      <ErrorNotice error={providers.error} retry={() => providers.refetch()} />
      <ErrorNotice error={toggle.error ?? remove.error} />
      <DataTable
        data={models.data?.items ?? []}
        columns={columns}
        rowID={(row) => String(row.id)}
        loading={models.isPending}
        fetching={models.isFetching}
        total={models.data?.total ?? 0}
        pagination={filters.pagination}
        onPaginationChange={filters.onPaginationChange}
        empty={
          filters.name || filters.enabled
            ? '没有匹配的模型路由'
            : '暂无模型路由'
        }
        minWidth={760}
        expanded={(model) => (
          <div className="divide-y">
            {model.candidates.map((candidate) => (
              <div
                key={candidate.id}
                className="grid grid-cols-[24px_minmax(0,1fr)_minmax(0,1fr)] gap-4 py-3 first:pt-0 lg:grid-cols-[24px_minmax(0,1fr)_minmax(0,1fr)_minmax(0,2fr)]"
              >
                <span className="text-xs tabular-nums text-muted-foreground">
                  {candidate.position + 1}
                </span>
                <span className="text-xs font-medium">
                  {candidate.provider_name}
                </span>
                <code className="break-all text-xs">
                  {candidate.upstream_model}
                </code>
                <span className="col-span-2 text-xs text-muted-foreground lg:col-span-1">
                  {candidate.protocols
                    .map((item) => protocolName(item.protocol))
                    .join(' → ')}
                </span>
              </div>
            ))}
          </div>
        )}
      />
      <Dialog
        open={!!testing}
        onOpenChange={(open) => {
          if (!open && !test.isPending) {
            setTesting(null)
            test.reset()
          }
        }}
      >
        <DialogContent className="max-h-[85dvh] overflow-y-auto sm:max-w-2xl">
          <DialogHeader>
            <DialogTitle>测试模型</DialogTitle>
            <DialogDescription className="font-mono text-xs">
              {testing?.name}
            </DialogDescription>
          </DialogHeader>
          <div className="field">
            <Label htmlFor="model-test-prompt">测试内容</Label>
            <Textarea
              id="model-test-prompt"
              value={prompt}
              disabled={test.isPending}
              onChange={(event) => {
                setPrompt(event.target.value)
                test.reset()
              }}
              rows={3}
            />
          </div>
          <ErrorNotice error={test.error} />
          {test.data && (
            <section>
              <div className="mb-3 flex flex-wrap items-center justify-between gap-2 text-xs text-muted-foreground">
                <span>模型响应</span>
                <Link
                  className="text-link font-mono"
                  to={`/requests/${test.data.request_id}`}
                >
                  {test.data.request_id}
                </Link>
              </div>
              <JsonPayload value={JSON.stringify(test.data.response)} />
            </section>
          )}
          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => setTesting(null)}
              disabled={test.isPending}
            >
              关闭
            </Button>
            <Button
              disabled={!prompt.trim() || test.isPending}
              onClick={() =>
                testing &&
                test.mutate({ id: testing.id, prompt: prompt.trim() })
              }
            >
              {test.isPending && <Loader2 className="animate-spin" />}发送测试
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  )
}

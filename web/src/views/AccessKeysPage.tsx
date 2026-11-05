import { useState } from 'react'
import {
  useMutation,
  useQuery,
  useQueryClient,
  keepPreviousData,
} from '@tanstack/react-query'
import { useForm } from 'react-hook-form'
import type { ColumnDef } from '@tanstack/react-table'
import { Pencil, Plus, Trash2, Loader2 } from 'lucide-react'
import dayjs from 'dayjs'
import { api, type AccessKey, type DeleteResult, type Page } from '../api'
import DataTable from '../components/DataTable'
import {
  ConfirmButton,
  CopyButton,
  ErrorNotice,
  FilterSelect,
  PageHeader,
  SearchField,
  displayDate,
  localDateTime,
} from '../components/console'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { useCatalogFilters } from '@/hooks/use-catalog-filters'

type KeyForm = { name: string; expires_at: string }
export default function AccessKeysPage() {
  const filters = useCatalogFilters()
  const [open, setOpen] = useState(false)
  const [editing, setEditing] = useState<AccessKey | null>(null)
  const form = useForm<KeyForm>({ defaultValues: { name: '', expires_at: '' } })
  const client = useQueryClient()
  const keys = useQuery({
    queryKey: [
      'access-keys',
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
      return api<Page<AccessKey>>(`/api/admin/access-keys?${query}`)
    },
    placeholderData: keepPreviousData,
  })
  function refresh() {
    client.invalidateQueries({ queryKey: ['access-keys'] })
    client.invalidateQueries({ queryKey: ['access-key-options'] })
  }
  const save = useMutation({
    mutationFn: (values: KeyForm) =>
      api(
        editing
          ? `/api/admin/access-keys/${editing.id}`
          : '/api/admin/access-keys',
        {
          method: editing ? 'PUT' : 'POST',
          body: JSON.stringify({
            name: values.name,
            expires_at: values.expires_at
              ? dayjs(values.expires_at).toISOString()
              : null,
            ...(editing ? { enabled: editing.enabled } : {}),
          }),
        },
      ),
    onSuccess: () => {
      if (!editing) filters.update({ name: '', enabled: '', page: '1' })
      setOpen(false)
      setEditing(null)
      form.reset()
      refresh()
    },
  })
  const toggle = useMutation({
    mutationFn: ({ id, enabled }: { id: number; enabled: boolean }) =>
      api(`/api/admin/access-keys/${id}`, {
        method: 'PATCH',
        body: JSON.stringify({ enabled }),
      }),
    onSuccess: refresh,
  })
  const remove = useMutation({
    mutationFn: (id: number) =>
      api<DeleteResult>(`/api/admin/access-keys/${id}`, { method: 'DELETE' }),
    onSuccess: refresh,
  })
  function edit(key: AccessKey | null) {
    setEditing(key)
    form.reset({
      name: key?.name ?? '',
      expires_at: localDateTime(key?.expires_at),
    })
    save.reset()
    setOpen(true)
  }
  const columns: ColumnDef<AccessKey>[] = [
    {
      accessorKey: 'name',
      header: '名称',
      cell: ({ row }) => (
        <span className="font-medium">{row.original.name}</span>
      ),
    },
    {
      accessorKey: 'secret',
      header: '密钥',
      size: 320,
      cell: ({ row }) => (
        <div className="flex max-w-80 items-center gap-1">
          <code className="truncate text-xs" title={row.original.secret}>
            {row.original.secret || '—'}
          </code>
          <CopyButton
            value={row.original.secret ?? ''}
            label={`复制 ${row.original.name} 密钥`}
          />
        </div>
      ),
    },
    {
      accessorKey: 'last_used_at',
      header: '最后使用',
      size: 165,
      cell: ({ row }) => (
        <span className="text-xs tabular-nums text-muted-foreground">
          {row.original.last_used_at
            ? displayDate(row.original.last_used_at)
            : '从未使用'}
        </span>
      ),
    },
    {
      accessorKey: 'expires_at',
      header: '过期时间',
      size: 165,
      cell: ({ row }) => (
        <span className="text-xs tabular-nums">
          {row.original.expires_at
            ? displayDate(row.original.expires_at)
            : '永不过期'}
        </span>
      ),
    },
    {
      accessorKey: 'enabled',
      header: '启用',
      size: 75,
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
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label={`编辑 ${row.original.name}`}
            onClick={() => edit(row.original)}
          >
            <Pencil />
          </Button>
          <ConfirmButton
            title="删除访问密钥？"
            description="已产生历史记录的密钥将转为归档。"
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
      <PageHeader title="访问密钥">
        <Button onClick={() => edit(null)}>
          <Plus />
          创建密钥
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
      </div>
      <ErrorNotice error={keys.error} retry={() => keys.refetch()} />
      <ErrorNotice error={toggle.error ?? remove.error} />
      <DataTable
        data={keys.data?.items ?? []}
        columns={columns}
        rowID={(row) => String(row.id)}
        loading={keys.isPending}
        fetching={keys.isFetching}
        total={keys.data?.total ?? 0}
        pagination={filters.pagination}
        onPaginationChange={filters.onPaginationChange}
        empty={
          filters.name || filters.enabled
            ? '没有匹配的访问密钥'
            : '暂无访问密钥'
        }
        minWidth={1000}
      />
      <Dialog
        open={open}
        onOpenChange={(next) => {
          if (!save.isPending) setOpen(next)
        }}
      >
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>
              {editing ? '编辑访问密钥' : '创建访问密钥'}
            </DialogTitle>
            <DialogDescription className="sr-only">
              设置密钥名称及可选过期时间
            </DialogDescription>
          </DialogHeader>
          <form
            id="access-key-form"
            className="space-y-4"
            onSubmit={form.handleSubmit((values) => save.mutate(values))}
          >
            <div className="field">
              <Label htmlFor="key-name">名称</Label>
              <Input
                id="key-name"
                autoFocus
                {...form.register('name', { required: '请输入名称' })}
                aria-invalid={!!form.formState.errors.name}
              />
              <p className="field-error">
                {form.formState.errors.name?.message}
              </p>
            </div>
            <div className="field">
              <Label htmlFor="key-expiry">过期时间（可选）</Label>
              <Input
                id="key-expiry"
                type="datetime-local"
                step="1"
                {...form.register('expires_at')}
              />
            </div>
            <ErrorNotice error={save.error} />
          </form>
          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => setOpen(false)}
              disabled={save.isPending}
            >
              取消
            </Button>
            <Button
              type="submit"
              form="access-key-form"
              disabled={save.isPending}
            >
              {save.isPending && <Loader2 className="animate-spin" />}
              {editing ? '保存' : '创建密钥'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  )
}

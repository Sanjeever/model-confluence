import { useRef, useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import type { ColumnDef } from '@tanstack/react-table'
import { Loader2, Trash2, Upload } from 'lucide-react'
import { api, type Provider, type UpstreamKey } from '../api'
import DataTable from './DataTable'
import OAuthLoginButton from './OAuthLoginButton'
import { Status } from './Status'
import { ConfirmButton, ErrorNotice, MetaGrid, displayDate } from './console'
import { Button } from './ui/button'
import { Switch } from './ui/switch'

export default function OAuthAccounts({ provider }: { provider: Provider }) {
  const antigravity = provider.kind === 'antigravity_oauth'
  const input = useRef<HTMLInputElement>(null)
  const [uploading, setUploading] = useState(false)
  const [uploadErrors, setUploadErrors] = useState<
    Array<{ name: string; error: string }>
  >([])
  const client = useQueryClient()
  async function refresh() {
    await Promise.all([
      client.invalidateQueries({ queryKey: ['providers'] }),
      client.invalidateQueries({ queryKey: ['health'] }),
    ])
  }
  async function upload(files: FileList | null) {
    if (!files) return
    setUploading(true)
    setUploadErrors([])
    const errors: Array<{ name: string; error: string }> = []
    for (const file of Array.from(files)) {
      const body = new FormData()
      body.append('file', file)
      try {
        if (antigravity) {
          const result = await api<{
            accounts: UpstreamKey[]
            errors: Array<{ email: string; error: string }>
          }>(`/api/admin/providers/${provider.id}/auth-files`, {
            method: 'POST',
            body,
          })
          errors.push(
            ...result.errors.map((item) => ({
              name: item.email || file.name,
              error: item.error,
            })),
          )
        } else
          await api<UpstreamKey>(
            `/api/admin/providers/${provider.id}/auth-files`,
            { method: 'POST', body },
          )
      } catch (error) {
        errors.push({ name: file.name, error: (error as Error).message })
      }
    }
    setUploadErrors(errors)
    await refresh()
    setUploading(false)
    if (input.current) input.current.value = ''
  }
  const toggle = useMutation({
    mutationFn: ({ id, enabled }: { id: number; enabled: boolean }) =>
      api(`/api/admin/upstream-keys/${id}`, {
        method: 'PATCH',
        body: JSON.stringify({ enabled }),
      }),
    onSuccess: refresh,
  })
  const remove = useMutation({
    mutationFn: (id: number) =>
      api(`/api/admin/providers/${provider.id}/auth-files/${id}`, {
        method: 'DELETE',
      }),
    onSuccess: refresh,
  })
  const columns: ColumnDef<UpstreamKey>[] = [
    {
      accessorKey: 'file_name',
      header: '来源',
      cell: ({ row }) => (
        <span
          className="block max-w-40 truncate text-xs"
          title={row.original.file_name}
        >
          {row.original.file_name}
        </span>
      ),
    },
    {
      id: 'email',
      header: '邮箱',
      cell: ({ row }) => (
        <span
          className="block max-w-48 truncate text-xs"
          title={
            row.original.antigravity_identity?.email ??
            row.original.oauth_identity?.email
          }
        >
          {row.original.antigravity_identity?.email ??
            row.original.oauth_identity?.email ??
            '—'}
        </span>
      ),
    },
    {
      id: 'account',
      header: antigravity ? '项目' : '工作区',
      cell: ({ row }) => (
        <code
          className="block max-w-40 truncate text-xs"
          title={
            row.original.antigravity_identity?.project_id ??
            row.original.oauth_identity?.account_id
          }
        >
          {row.original.antigravity_identity?.project_id ??
            row.original.oauth_identity?.account_id ??
            '—'}
        </code>
      ),
    },
    {
      accessorKey: 'runtime_status',
      header: '运行状态',
      cell: ({ row }) => (
        <Status
          value={row.original.runtime_status}
          label={
            row.original.runtime_status === 'auth_invalid'
              ? '需重新导入'
              : undefined
          }
        />
      ),
    },
    {
      accessorKey: 'last_used_at',
      header: '最近使用',
      cell: ({ row }) => (
        <span className="text-xs tabular-nums text-muted-foreground">
          {displayDate(row.original.last_used_at)}
        </span>
      ),
    },
    {
      accessorKey: 'enabled',
      header: '启用',
      size: 60,
      cell: ({ row }) => (
        <Switch
          aria-label={`启用 ${row.original.file_name}`}
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
      size: 48,
      cell: ({ row }) => (
        <ConfirmButton
          title="删除账号凭据？"
          pending={remove.isPending}
          onConfirm={() => remove.mutate(row.original.id)}
        >
          <Button
            size="icon-sm"
            variant="ghost"
            className="text-muted-foreground hover:text-destructive"
            aria-label={`删除 ${row.original.file_name}`}
          >
            <Trash2 />
          </Button>
        </ConfirmButton>
      ),
    },
  ]
  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h2 className="text-sm font-semibold">OAuth 账号</h2>
        <div className="flex gap-2">
          <OAuthLoginButton provider={provider} />
          <Button
            variant="outline"
            disabled={uploading}
            onClick={() => input.current?.click()}
          >
            {uploading ? <Loader2 className="animate-spin" /> : <Upload />}
            {antigravity ? '导入账号 JSON' : '上传 auth.json'}
          </Button>
        </div>
        <input
          ref={input}
          type="file"
          multiple
          accept=".json,application/json"
          className="hidden"
          aria-label="上传 OAuth 账号文件"
          onChange={(event) => upload(event.target.files)}
        />
      </div>
      {antigravity && (
        <p className="text-xs leading-5 text-muted-foreground">
          实验性接入，尚无订阅账号实测。个人订阅的第三方访问受{' '}
          <a
            className="text-link underline"
            href="https://antigravity.google/terms"
            target="_blank"
            rel="noreferrer"
          >
            Google 条款
          </a>
          限制。导入前需配置签发令牌的 OAuth 客户端，见
          <a
            className="text-link underline"
            href="https://github.com/Sanjeever/model-confluence/blob/main/docs/operations.md#antigravity-oauth"
            target="_blank"
            rel="noreferrer"
          >
            接入说明
          </a>
          。
        </p>
      )}
      <ErrorNotice error={toggle.error ?? remove.error} />
      {uploadErrors.map((item, index) => (
        <p key={index} role="alert" className="text-xs text-destructive">
          {item.name} · {item.error}
        </p>
      ))}
      <DataTable
        data={provider.keys}
        columns={columns}
        rowID={(row) => String(row.id)}
        empty="暂无账号"
        minWidth={850}
        expanded={(key) => (
          <MetaGrid
            items={[
              {
                label: antigravity ? 'Google 账号 ID' : '工作区 ID',
                value: antigravity
                  ? key.antigravity_identity?.subject
                  : key.oauth_identity?.account_id,
              },
              {
                label: antigravity ? '项目 ID' : '套餐',
                value: antigravity
                  ? key.antigravity_identity?.project_id
                  : key.oauth_identity?.plan,
              },
              {
                label: '访问令牌到期',
                value: displayDate(key.access_token_expires_at),
              },
              { label: '最近刷新', value: displayDate(key.last_refresh) },
              { label: '恢复时间', value: displayDate(key.recover_at) },
              { label: '状态原因', value: key.runtime_reason },
            ]}
          />
        )}
      />
    </div>
  )
}

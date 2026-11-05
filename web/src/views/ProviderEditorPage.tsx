import { Controller, useFieldArray, useForm } from 'react-hook-form'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useLocation, useNavigate, useParams } from 'react-router-dom'
import { ArrowLeft, Loader2, Plus, Trash2 } from 'lucide-react'
import dayjs from 'dayjs'
import { api, type Provider } from '../api'
import { providerTemplates } from '../providerTemplates'
import OAuthAccounts from '../components/OAuthAccounts'
import {
  CopyButton,
  ErrorNotice,
  Loading,
  PageHeader,
  localDateTime,
} from '../components/console'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { Label } from '@/components/ui/label'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Switch } from '@/components/ui/switch'
import { useUnsavedChanges } from '@/hooks/use-unsaved-changes'

type KeyForm = {
  id?: number
  name: string
  secret: string
  expires_at: string
  enabled: boolean
}
type ProviderForm = {
  kind: Provider['kind']
  name: string
  auth_type: Provider['auth_type']
  auth_header: string
  chat_completions: string
  responses: string
  messages: string
  static_headers: string
  quota_codes: string
  keys: KeyForm[]
}

export default function ProviderEditorPage() {
  const { providerID } = useParams()
  const query = useQuery({
    queryKey: ['providers', 'detail', providerID],
    queryFn: () => api<Provider>(`/api/admin/providers/${providerID}`),
    enabled: !!providerID,
  })
  if (providerID && query.isPending) return <Loading />
  if (query.error)
    return <ErrorNotice error={query.error} retry={() => query.refetch()} />
  return <ProviderEditor key={providerID ?? 'new'} provider={query.data} />
}

function ProviderEditor({ provider }: { provider?: Provider }) {
  const location = useLocation()
  const navigate = useNavigate()
  const client = useQueryClient()
  const form = useForm<ProviderForm>({
    defaultValues: {
      kind: provider?.kind ?? 'generic',
      name: provider?.name ?? '',
      auth_type: provider?.auth_type ?? 'bearer',
      auth_header: provider?.auth_header ?? '',
      chat_completions: provider?.endpoints.chat_completions ?? '',
      responses: provider?.endpoints.responses ?? '',
      messages: provider?.endpoints.messages ?? '',
      static_headers:
        provider && Object.keys(provider.static_headers).length
          ? JSON.stringify(provider.static_headers, null, 2)
          : '',
      quota_codes: provider?.quota_codes.join('\n') ?? '',
      keys: provider?.keys.map((key) => ({
        id: key.id,
        name: key.name,
        secret: key.secret ?? '',
        expires_at: localDateTime(key.expires_at),
        enabled: key.enabled,
      })) ?? [{ name: '', secret: '', expires_at: '', enabled: true }],
    },
  })
  const keys = useFieldArray({
    control: form.control,
    name: 'keys',
    keyName: 'formKey',
  })
  const kind = form.watch('kind')
  const authType = form.watch('auth_type')
  const oauth = kind !== 'generic'
  const guard = useUnsavedChanges(form.formState.isDirty)
  const returnTo = `/providers${location.search}`
  const save = useMutation({
    mutationFn: (values: ProviderForm) => {
      const body =
        values.kind !== 'generic'
          ? { name: values.name, kind: values.kind }
          : {
              kind: values.kind,
              name: values.name,
              auth_type: values.auth_type,
              auth_header: values.auth_header,
              endpoints: Object.fromEntries(
                (
                  [
                    ['chat_completions', values.chat_completions],
                    ['responses', values.responses],
                    ['messages', values.messages],
                  ] as const
                ).filter((entry) => !!entry[1]),
              ),
              static_headers: values.static_headers.trim()
                ? JSON.parse(values.static_headers)
                : {},
              quota_codes: values.quota_codes
                .split('\n')
                .map((value) => value.trim())
                .filter(Boolean),
              keys: values.keys.map((key) => ({
                id: key.id ?? 0,
                name: key.name,
                secret:
                  key.id &&
                  provider?.keys.find((item) => item.id === key.id)?.secret ===
                    key.secret
                    ? ''
                    : key.secret,
                enabled: key.enabled,
                expires_at: key.expires_at
                  ? dayjs(key.expires_at).toISOString()
                  : null,
              })),
            }
      return api<Provider | undefined>(
        provider
          ? `/api/admin/providers/${provider.id}`
          : '/api/admin/providers',
        { method: provider ? 'PUT' : 'POST', body: JSON.stringify(body) },
      )
    },
    onSuccess: (result) => {
      guard.allowNavigation()
      client.invalidateQueries({ queryKey: ['providers'] })
      client.invalidateQueries({ queryKey: ['provider-options'] })
      client.invalidateQueries({ queryKey: ['health'] })
      if (!provider && oauth && result) {
        client.setQueryData(['providers', 'detail', String(result.id)], result)
        navigate(`/providers/${result.id}/edit${location.search}`, {
          replace: true,
        })
      } else navigate(returnTo)
    },
  })
  function template(index: string) {
    if (!index) return
    const values = providerTemplates[Number(index) - 1]
    form.setValue('name', values.name, { shouldDirty: true })
    form.setValue('chat_completions', values.chat_completions, {
      shouldDirty: true,
    })
    form.setValue('responses', values.responses, { shouldDirty: true })
    form.setValue('messages', values.messages, { shouldDirty: true })
  }
  return (
    <div className="mx-auto max-w-5xl">
      {guard.dialog}
      <div className="sticky -top-6 z-20 -mx-1 bg-background px-1 pt-1 pb-2">
        <PageHeader
          title={provider ? provider.name : '新增供应商'}
          back={
            <Button variant="ghost" size="icon-sm" asChild>
              <Link to={returnTo} aria-label="返回供应商">
                <ArrowLeft />
              </Link>
            </Button>
          }
        >
          <Button variant="outline" asChild>
            <Link to={returnTo}>取消</Link>
          </Button>
          <Button
            type="submit"
            form="provider-editor"
            disabled={save.isPending}
          >
            {save.isPending && <Loader2 className="animate-spin" />}
            {!provider && oauth ? '创建并添加账号' : '保存'}
          </Button>
        </PageHeader>
      </div>
      <ErrorNotice error={save.error} />
      <form
        id="provider-editor"
        onSubmit={form.handleSubmit((values) => save.mutate(values))}
        noValidate
      >
        <fieldset disabled={save.isPending}>
          <section className="editor-section">
            <h2 className="section-heading">基础信息</h2>
            <div className="grid gap-5 sm:max-w-2xl sm:grid-cols-2">
              <div className="field">
                <Label htmlFor="provider-kind">供应商类型</Label>
                <NativeSelect
                  id="provider-kind"
                  className="w-full"
                  disabled={!!provider}
                  {...form.register('kind')}
                >
                  <NativeSelectOption value="generic">
                    API Key
                  </NativeSelectOption>
                  <NativeSelectOption value="codex_oauth">
                    Codex OAuth
                  </NativeSelectOption>
                  <NativeSelectOption value="antigravity_oauth">
                    Antigravity OAuth（实验性）
                  </NativeSelectOption>
                </NativeSelect>
              </div>
              <div className="field">
                <Label htmlFor="provider-name">供应商名称</Label>
                <Input
                  id="provider-name"
                  {...form.register('name', { required: '请输入供应商名称' })}
                  aria-invalid={!!form.formState.errors.name}
                />
                <p className="field-error">
                  {form.formState.errors.name?.message}
                </p>
              </div>
              {!oauth && (
                <>
                  <div className="field">
                    <Label htmlFor="provider-auth">上游鉴权</Label>
                    <NativeSelect
                      id="provider-auth"
                      className="w-full"
                      {...form.register('auth_type')}
                    >
                      <NativeSelectOption value="bearer">
                        Authorization: Bearer
                      </NativeSelectOption>
                      <NativeSelectOption value="x-api-key">
                        x-api-key
                      </NativeSelectOption>
                      <NativeSelectOption value="custom">
                        自定义请求头
                      </NativeSelectOption>
                    </NativeSelect>
                  </div>
                  {authType === 'custom' && (
                    <div className="field">
                      <Label htmlFor="provider-auth-header">自定义鉴权头</Label>
                      <Input
                        id="provider-auth-header"
                        {...form.register('auth_header', {
                          required: '请输入鉴权头名称',
                        })}
                        aria-invalid={!!form.formState.errors.auth_header}
                      />
                      <p className="field-error">
                        {form.formState.errors.auth_header?.message}
                      </p>
                    </div>
                  )}
                </>
              )}
            </div>
          </section>
          {!oauth && (
            <>
              <section className="editor-section">
                <div className="mb-4 flex flex-wrap items-center justify-between gap-3">
                  <h2 className="text-sm font-semibold">协议端点</h2>
                  <NativeSelect
                    aria-label="应用供应商模板"
                    value=""
                    onChange={(event) => template(event.target.value)}
                  >
                    <NativeSelectOption value="">应用模板</NativeSelectOption>
                    {providerTemplates.map((item, index) => (
                      <NativeSelectOption
                        key={item.name}
                        value={String(index + 1)}
                      >
                        {item.name}
                      </NativeSelectOption>
                    ))}
                  </NativeSelect>
                </div>
                <div className="max-w-3xl space-y-4">
                  {(
                    [
                      { name: 'chat_completions', label: 'Chat Completions' },
                      { name: 'responses', label: 'Responses' },
                      { name: 'messages', label: 'Messages' },
                    ] as const
                  ).map((item) => (
                    <div key={item.name} className="field">
                      <Label htmlFor={`endpoint-${item.name}`}>
                        {item.label}
                      </Label>
                      <Input
                        id={`endpoint-${item.name}`}
                        placeholder="完整 URL"
                        className="font-mono text-xs"
                        {...form.register(item.name)}
                      />
                    </div>
                  ))}
                </div>
              </section>
              <section className="editor-section">
                <div className="mb-4 flex items-center justify-between">
                  <h2 className="text-sm font-semibold">上游密钥池</h2>
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() =>
                      keys.append({
                        name: '',
                        secret: '',
                        enabled: true,
                        expires_at: '',
                      })
                    }
                  >
                    <Plus />
                    添加密钥
                  </Button>
                </div>
                <div className="space-y-4">
                  {keys.fields.map((field, index) => (
                    <div
                      key={field.formKey}
                      className="grid items-start gap-3 rounded-md border bg-surface p-4 sm:grid-cols-[minmax(100px,1fr)_minmax(140px,2fr)_minmax(150px,1.4fr)_auto_auto]"
                    >
                      <div className="field">
                        <Label
                          htmlFor={`upstream-name-${index}`}
                          className="text-xs"
                        >
                          备注
                        </Label>
                        <Input
                          id={`upstream-name-${index}`}
                          {...form.register(`keys.${index}.name`)}
                        />
                      </div>
                      <div className="field">
                        <Label
                          htmlFor={`upstream-secret-${index}`}
                          className="text-xs"
                        >
                          密钥
                        </Label>
                        <div className="flex items-center gap-1">
                          <Input
                            id={`upstream-secret-${index}`}
                            autoComplete="off"
                            className="font-mono text-xs"
                            {...form.register(`keys.${index}.secret`, {
                              validate: (value) =>
                                !!field.id || !!value || '请输入密钥',
                            })}
                            aria-invalid={
                              !!form.formState.errors.keys?.[index]?.secret
                            }
                          />
                          <CopyButton
                            value={form.watch(`keys.${index}.secret`)}
                            label="复制上游密钥"
                          />
                        </div>
                        <p className="field-error">
                          {form.formState.errors.keys?.[index]?.secret?.message}
                        </p>
                      </div>
                      <div className="field">
                        <Label
                          htmlFor={`upstream-expiry-${index}`}
                          className="text-xs"
                        >
                          过期时间（可选）
                        </Label>
                        <Input
                          id={`upstream-expiry-${index}`}
                          type="datetime-local"
                          step="1"
                          {...form.register(`keys.${index}.expires_at`)}
                        />
                      </div>
                      <div className="field">
                        <Label
                          htmlFor={`upstream-enabled-${index}`}
                          className="text-xs"
                        >
                          启用
                        </Label>
                        <div className="flex h-8 items-center">
                          <Controller
                            control={form.control}
                            name={`keys.${index}.enabled`}
                            render={({ field: control }) => (
                              <Switch
                                id={`upstream-enabled-${index}`}
                                checked={control.value}
                                onCheckedChange={control.onChange}
                              />
                            )}
                          />
                        </div>
                      </div>
                      <Button
                        variant="ghost"
                        size="icon-sm"
                        className="mt-6 text-muted-foreground hover:text-destructive"
                        disabled={keys.fields.length === 1}
                        onClick={() => keys.remove(index)}
                        aria-label={`移除密钥 ${index + 1}`}
                      >
                        <Trash2 />
                      </Button>
                    </div>
                  ))}
                </div>
              </section>
              <section className="editor-section">
                <details open={!!form.formState.errors.static_headers}>
                  <summary className="w-fit cursor-pointer text-sm font-semibold">
                    高级设置
                  </summary>
                  <div className="mt-5 grid gap-5 sm:grid-cols-2">
                    <div className="field">
                      <Label htmlFor="provider-quota-codes">
                        额度耗尽错误码（一行一个）
                      </Label>
                      <Textarea
                        id="provider-quota-codes"
                        rows={4}
                        className="font-mono text-xs"
                        {...form.register('quota_codes')}
                      />
                    </div>
                    <div className="field">
                      <Label htmlFor="provider-static-headers">
                        静态请求头（JSON 对象）
                      </Label>
                      <Textarea
                        id="provider-static-headers"
                        rows={4}
                        className="font-mono text-xs"
                        {...form.register('static_headers', {
                          validate: validateStaticHeaders,
                        })}
                        aria-invalid={!!form.formState.errors.static_headers}
                      />
                      <p className="field-error">
                        {form.formState.errors.static_headers?.message}
                      </p>
                    </div>
                  </div>
                </details>
              </section>
            </>
          )}
        </fieldset>
      </form>
      {oauth && provider && (
        <section className="py-6">
          <OAuthAccounts provider={provider} />
        </section>
      )}
    </div>
  )
}
function validateStaticHeaders(value: string) {
  if (!value.trim()) return true
  try {
    const parsed: unknown = JSON.parse(value)
    return (
      (parsed !== null &&
        !Array.isArray(parsed) &&
        typeof parsed === 'object' &&
        Object.values(parsed).every((item) => typeof item === 'string')) ||
      '静态请求头必须是字符串键值的 JSON 对象'
    )
  } catch {
    return '静态请求头必须是合法 JSON 对象'
  }
}

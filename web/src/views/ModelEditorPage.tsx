import { useState } from 'react'
import {
  Controller,
  useFieldArray,
  useForm,
  useWatch,
  type UseFormReturn,
} from 'react-hook-form'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useLocation, useNavigate, useParams } from 'react-router-dom'
import {
  ArrowDown,
  ArrowLeft,
  ArrowUp,
  Loader2,
  Plus,
  RefreshCw,
  Trash2,
} from 'lucide-react'
import {
  api,
  type CandidateProtocol,
  type ProviderOption,
  type VirtualModel,
} from '../api'
import { ErrorNotice, Loading, PageHeader } from '../components/console'
import { protocolName } from '../components/Status'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { useUnsavedChanges } from '@/hooks/use-unsaved-changes'
import { cn } from '@/lib/utils'

type Protocol = CandidateProtocol['protocol']
type CandidateForm = {
  id?: number
  provider_id?: number
  upstream_model: string
  default_max_output_tokens: number
  max_output_tokens: number
  protocols: Protocol[]
}
type ModelForm = { name: string; candidates: CandidateForm[] }
const protocolNames: Protocol[] = [
  'chat_completions',
  'responses',
  'messages',
  'antigravity',
]
const newCandidate = (): CandidateForm => ({
  upstream_model: '',
  default_max_output_tokens: 16384,
  max_output_tokens: 65536,
  protocols: [],
})

export default function ModelEditorPage() {
  const { modelID } = useParams()
  const model = useQuery({
    queryKey: ['models', 'detail', modelID],
    queryFn: () => api<VirtualModel>(`/api/admin/models/${modelID}`),
    enabled: !!modelID,
  })
  const providers = useQuery({
    queryKey: ['provider-options'],
    queryFn: () => api<ProviderOption[]>('/api/admin/provider-options'),
  })
  if (providers.isPending || (modelID && model.isPending)) return <Loading />
  if (model.error || providers.error)
    return (
      <ErrorNotice
        error={model.error ?? providers.error}
        retry={() => {
          providers.refetch()
          if (modelID) model.refetch()
        }}
      />
    )
  return (
    <ModelEditor
      key={modelID ?? 'new'}
      model={model.data}
      providers={providers.data ?? []}
    />
  )
}

function ModelEditor({
  model,
  providers,
}: {
  model?: VirtualModel
  providers: ProviderOption[]
}) {
  const location = useLocation()
  const navigate = useNavigate()
  const client = useQueryClient()
  const [active, setActive] = useState(0)
  const form = useForm<ModelForm>({
    defaultValues: {
      name: model?.name ?? '',
      candidates: model?.candidates.map((candidate) => ({
        id: candidate.id,
        provider_id: candidate.provider_id,
        upstream_model: candidate.upstream_model,
        default_max_output_tokens: candidate.default_max_output_tokens,
        max_output_tokens: candidate.max_output_tokens,
        protocols: candidate.protocols
          .map((item) => item.protocol)
          .filter((protocol) =>
            configuredProtocols(candidate.provider_id, providers).includes(
              protocol,
            ),
          ),
      })) ?? [newCandidate()],
    },
  })
  const candidates = useFieldArray({
    control: form.control,
    name: 'candidates',
    keyName: 'formKey',
  })
  const watched = useWatch({ control: form.control, name: 'candidates' })
  const guard = useUnsavedChanges(form.formState.isDirty)
  const returnTo = `/models${location.search}`
  const save = useMutation({
    mutationFn: (values: ModelForm) =>
      api(model ? `/api/admin/models/${model.id}` : '/api/admin/models', {
        method: model ? 'PUT' : 'POST',
        body: JSON.stringify({
          name: values.name,
          candidates: values.candidates.map((candidate) => {
            const codex =
              providers.find(
                (provider) => provider.id === candidate.provider_id,
              )?.kind === 'codex_oauth'
            return {
              id: candidate.id ?? 0,
              provider_id: candidate.provider_id,
              upstream_model: candidate.upstream_model,
              default_max_output_tokens: codex
                ? 0
                : candidate.default_max_output_tokens,
              max_output_tokens: codex ? 0 : candidate.max_output_tokens,
              protocols: candidate.protocols.map((protocol, position) => {
                const existing = model?.candidates
                  .find((item) => item.id === candidate.id)
                  ?.protocols.find((item) => item.protocol === protocol)
                return existing
                  ? { ...existing, position }
                  : defaultProtocol(protocol, position)
              }),
            }
          }),
        }),
      }),
    onSuccess: () => {
      guard.allowNavigation()
      client.invalidateQueries({ queryKey: ['models'] })
      client.invalidateQueries({ queryKey: ['model-names'] })
      client.invalidateQueries({ queryKey: ['health'] })
      navigate(returnTo)
    },
  })
  function move(index: number, offset: number) {
    const destination = index + offset
    candidates.move(index, destination)
    if (active === index) setActive(destination)
    else if (active === destination) setActive(index)
  }
  return (
    <div className="mx-auto max-w-6xl">
      {guard.dialog}
      <div className="sticky -top-6 z-20 -mx-1 bg-background px-1 pt-1 pb-2">
        <PageHeader
          title={model ? model.name : '新增虚拟模型'}
          back={
            <Button variant="ghost" size="icon-sm" asChild>
              <Link to={returnTo} aria-label="返回模型路由">
                <ArrowLeft />
              </Link>
            </Button>
          }
        >
          <Button variant="outline" asChild>
            <Link to={returnTo}>取消</Link>
          </Button>
          <Button type="submit" form="model-editor" disabled={save.isPending}>
            {save.isPending && <Loader2 className="animate-spin" />}保存路由
          </Button>
        </PageHeader>
      </div>
      <ErrorNotice error={save.error} />
      <form
        id="model-editor"
        noValidate
        onSubmit={form.handleSubmit(
          (values) => save.mutate(values),
          (errors) => {
            const index =
              errors.candidates?.findIndex?.((error) => !!error) ?? -1
            if (index >= 0) setActive(index)
          },
        )}
      >
        <fieldset disabled={save.isPending}>
          <div className="field mb-6 max-w-md">
            <Label htmlFor="virtual-model-name">虚拟模型名</Label>
            <Input
              id="virtual-model-name"
              className="font-mono"
              {...form.register('name', { required: '请输入虚拟模型名' })}
              aria-invalid={!!form.formState.errors.name}
            />
            <p className="field-error">{form.formState.errors.name?.message}</p>
          </div>
          <div className="grid overflow-hidden rounded-lg border bg-surface lg:grid-cols-[260px_minmax(0,1fr)]">
            <aside className="border-b bg-muted/20 p-3 lg:border-r lg:border-b-0">
              <h2 className="px-2 py-2 text-xs font-medium text-muted-foreground">
                候选顺序
              </h2>
              <div className="space-y-1">
                {candidates.fields.map((field, index) => (
                  <div
                    key={field.formKey}
                    className={cn(
                      'rounded-md border border-transparent',
                      active === index && 'border-border bg-accent/70',
                    )}
                  >
                    <button
                      type="button"
                      className="flex w-full gap-2 px-3 py-3 text-left"
                      onClick={() => setActive(index)}
                      aria-current={active === index ? 'step' : undefined}
                    >
                      <span className="mt-0.5 w-4 text-xs tabular-nums text-muted-foreground">
                        {index + 1}
                      </span>
                      <span className="min-w-0">
                        <span className="block truncate text-xs font-medium">
                          {providers.find(
                            (provider) =>
                              provider.id === watched[index]?.provider_id,
                          )?.name ?? '选择供应商'}
                        </span>
                        <code className="mt-1 block truncate text-[11px] text-muted-foreground">
                          {watched[index]?.upstream_model || '真实模型名'}
                        </code>
                        {form.formState.errors.candidates?.[index] && (
                          <span className="mt-1 block text-xs text-destructive">
                            配置未完成
                          </span>
                        )}
                      </span>
                    </button>
                    <div className="flex justify-end gap-1 px-2 pb-2">
                      <Button
                        variant="ghost"
                        size="icon-xs"
                        aria-label={`上移候选 ${index + 1}`}
                        disabled={index === 0}
                        onClick={() => move(index, -1)}
                      >
                        <ArrowUp />
                      </Button>
                      <Button
                        variant="ghost"
                        size="icon-xs"
                        aria-label={`下移候选 ${index + 1}`}
                        disabled={index === candidates.fields.length - 1}
                        onClick={() => move(index, 1)}
                      >
                        <ArrowDown />
                      </Button>
                      <Button
                        variant="ghost"
                        size="icon-xs"
                        className="text-muted-foreground hover:text-destructive"
                        aria-label={`移除候选 ${index + 1}`}
                        disabled={candidates.fields.length === 1}
                        onClick={() => {
                          candidates.remove(index)
                          setActive(
                            Math.max(0, active >= index ? active - 1 : active),
                          )
                        }}
                      >
                        <Trash2 />
                      </Button>
                    </div>
                  </div>
                ))}
              </div>
              <Button
                variant="outline"
                className="mt-3 w-full"
                onClick={() => {
                  candidates.append(newCandidate(), { shouldFocus: false })
                  setActive(candidates.fields.length)
                }}
              >
                <Plus />
                添加后备候选
              </Button>
            </aside>
            <div className="min-w-0 p-5 sm:p-6">
              {candidates.fields.map((field, index) => (
                <section key={field.formKey} hidden={active !== index}>
                  <h2 className="mb-5 text-sm font-semibold">
                    候选 {index + 1}
                  </h2>
                  <CandidateEditor
                    form={form}
                    index={index}
                    providers={providers}
                  />
                </section>
              ))}
            </div>
          </div>
        </fieldset>
      </form>
    </div>
  )
}

function CandidateEditor({
  form,
  index,
  providers,
}: {
  form: UseFormReturn<ModelForm>
  index: number
  providers: ProviderOption[]
}) {
  const candidate = useWatch({
    control: form.control,
    name: `candidates.${index}`,
  })
  const provider = providers.find((item) => item.id === candidate.provider_id)
  const codex = provider?.kind === 'codex_oauth'
  const errors = form.formState.errors.candidates?.[index]
  return (
    <div className="space-y-5">
      <div className="field">
        <Label htmlFor={`candidate-provider-${index}`}>供应商</Label>
        <Controller
          control={form.control}
          name={`candidates.${index}.provider_id`}
          rules={{ required: '请选择供应商' }}
          render={({ field }) => (
            <NativeSelect
              id={`candidate-provider-${index}`}
              className="w-full"
              value={field.value ?? ''}
              onBlur={field.onBlur}
              ref={field.ref}
              aria-invalid={!!errors?.provider_id}
              onChange={(event) => {
                const id = Number(event.target.value)
                field.onChange(id)
                const available = configuredProtocols(id, providers)
                const codex =
                  providers.find((item) => item.id === id)?.kind ===
                  'codex_oauth'
                form.setValue(
                  `candidates.${index}.protocols`,
                  available.length ? [available[0]] : [],
                  { shouldDirty: true },
                )
                form.setValue(
                  `candidates.${index}.default_max_output_tokens`,
                  codex ? 0 : 16384,
                  { shouldDirty: true },
                )
                form.setValue(
                  `candidates.${index}.max_output_tokens`,
                  codex ? 0 : 65536,
                  { shouldDirty: true },
                )
              }}
            >
              <NativeSelectOption value="" disabled>
                选择供应商
              </NativeSelectOption>
              {providers.map((item) => (
                <NativeSelectOption key={item.id} value={item.id}>
                  {item.name}
                  {item.enabled ? '' : '（已停用）'}
                </NativeSelectOption>
              ))}
            </NativeSelect>
          )}
        />
        <p className="field-error">{errors?.provider_id?.message}</p>
      </div>
      <div className="field">
        <Label htmlFor={`candidate-model-${index}`}>真实模型名</Label>
        <UpstreamModelField form={form} index={index} provider={provider} />
        <p className="field-error">{errors?.upstream_model?.message}</p>
      </div>
      <div className="field">
        <Label>协议入口及后备顺序</Label>
        <Controller
          control={form.control}
          name={`candidates.${index}.protocols`}
          rules={{
            validate: (value) =>
              value.length > 0 || '请选择至少一个供应商已配置的协议',
          }}
          render={({ field }) => (
            <ProtocolOrder
              value={field.value}
              onChange={field.onChange}
              available={configuredProtocols(candidate.provider_id, providers)}
            />
          )}
        />
        <p className="field-error">{errors?.protocols?.message}</p>
      </div>
      {!codex && (
        <details
          open={
            !!errors?.default_max_output_tokens || !!errors?.max_output_tokens
          }
          className="border-t pt-5"
        >
          <summary className="w-fit cursor-pointer text-sm font-medium">
            高级设置
          </summary>
          <div className="mt-4 grid gap-4 sm:grid-cols-2">
            <div className="field">
              <Label
                htmlFor={`candidate-default-${index}`}
                title="客户端未指定输出 Token 数时使用"
              >
                默认最大输出 Token
              </Label>
              <Input
                id={`candidate-default-${index}`}
                type="number"
                min={1}
                {...form.register(
                  `candidates.${index}.default_max_output_tokens`,
                  {
                    valueAsNumber: true,
                    required: '请输入默认最大输出 Token',
                    min: { value: 1, message: '至少为 1' },
                  },
                )}
                aria-invalid={!!errors?.default_max_output_tokens}
              />
              <p className="field-error">
                {errors?.default_max_output_tokens?.message}
              </p>
            </div>
            <div className="field">
              <Label
                htmlFor={`candidate-max-${index}`}
                title="该候选允许的最大输出 Token 数"
              >
                输出 Token 上限
              </Label>
              <Input
                id={`candidate-max-${index}`}
                type="number"
                min={1}
                {...form.register(`candidates.${index}.max_output_tokens`, {
                  valueAsNumber: true,
                  required: '请输入输出 Token 上限',
                  validate: (value) =>
                    value >=
                      form.getValues(
                        `candidates.${index}.default_max_output_tokens`,
                      ) || '输出上限不能小于默认最大输出',
                })}
                aria-invalid={!!errors?.max_output_tokens}
              />
              <p className="field-error">
                {errors?.max_output_tokens?.message}
              </p>
            </div>
          </div>
        </details>
      )}
    </div>
  )
}

function UpstreamModelField({
  form,
  index,
  provider,
}: {
  form: UseFormReturn<ModelForm>
  index: number
  provider?: ProviderOption
}) {
  const models = useQuery({
    queryKey: ['provider-models', provider?.id],
    queryFn: () =>
      api<{ models: string[] }>(`/api/admin/providers/${provider?.id}/models`),
    enabled: false,
    staleTime: Infinity,
    retry: false,
  })
  return (
    <>
      <div className="flex gap-2">
        <Input
          id={`candidate-model-${index}`}
          className="min-w-0 font-mono text-xs"
          list={
            provider?.kind === 'codex_oauth'
              ? undefined
              : `model-options-${index}`
          }
          {...form.register(`candidates.${index}.upstream_model`, {
            required: '请输入真实模型名',
          })}
          aria-invalid={
            !!form.formState.errors.candidates?.[index]?.upstream_model
          }
        />
        {provider?.kind !== 'codex_oauth' && (
          <Button
            variant="outline"
            disabled={!provider || models.isFetching}
            onClick={() => models.refetch()}
          >
            <RefreshCw className={models.isFetching ? 'animate-spin' : ''} />
            拉取模型
          </Button>
        )}
      </div>
      {provider?.kind !== 'codex_oauth' && (
        <datalist id={`model-options-${index}`}>
          {models.data?.models.map((name) => (
            <option key={name} value={name} />
          ))}
        </datalist>
      )}
      <ErrorNotice error={models.error} />
    </>
  )
}

function ProtocolOrder({
  value,
  onChange,
  available,
}: {
  value: Protocol[]
  onChange: (value: Protocol[]) => void
  available: Protocol[]
}) {
  const remaining = available.filter((protocol) => !value.includes(protocol))
  function move(index: number, offset: number) {
    const next = [...value]
    ;[next[index], next[index + offset]] = [next[index + offset], next[index]]
    onChange(next)
  }
  return (
    <div className="space-y-2 rounded-md border p-3">
      {value.map((protocol, index) => (
        <div key={protocol} className="flex items-center gap-2">
          <span className="w-4 text-xs tabular-nums text-muted-foreground">
            {index + 1}
          </span>
          <NativeSelect
            aria-label={`协议入口 ${index + 1}`}
            className="min-w-0 flex-1"
            value={protocol}
            onChange={(event) =>
              onChange(
                value.map((item, position) =>
                  position === index ? (event.target.value as Protocol) : item,
                ),
              )
            }
          >
            {available
              .filter((item) => item === protocol || !value.includes(item))
              .map((item) => (
                <NativeSelectOption key={item} value={item}>
                  {protocolName(item)}
                </NativeSelectOption>
              ))}
          </NativeSelect>
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label={`上移协议 ${index + 1}`}
            disabled={!index}
            onClick={() => move(index, -1)}
          >
            <ArrowUp />
          </Button>
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label={`下移协议 ${index + 1}`}
            disabled={index === value.length - 1}
            onClick={() => move(index, 1)}
          >
            <ArrowDown />
          </Button>
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label={`移除协议 ${index + 1}`}
            className="text-muted-foreground hover:text-destructive"
            onClick={() =>
              onChange(value.filter((_, position) => position !== index))
            }
          >
            <Trash2 />
          </Button>
        </div>
      ))}
      {!available.length && (
        <p className="text-xs text-muted-foreground">
          {value.length ? '协议不可用' : '所选供应商尚未配置协议端点'}
        </p>
      )}
      {!!remaining.length && (
        <Button
          variant="ghost"
          size="sm"
          onClick={() => onChange([...value, remaining[0]])}
        >
          <Plus />
          添加协议入口
        </Button>
      )}
    </div>
  )
}
function configuredProtocols(
  id: number | undefined,
  providers: ProviderOption[],
) {
  const endpoints =
    providers.find((provider) => provider.id === id)?.endpoints ?? {}
  return protocolNames.filter((protocol) => !!endpoints[protocol])
}
function defaultProtocol(
  protocol: Protocol,
  position: number,
): CandidateProtocol {
  return {
    protocol,
    position,
    supports_stream: true,
    supports_tools: true,
    supports_parallel_tools: true,
    effort_levels:
      protocol === 'antigravity' ? [] : ['low', 'medium', 'high', 'xhigh'],
    supports_stream_usage: protocol === 'chat_completions',
  }
}

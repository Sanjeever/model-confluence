import { useState } from 'react'
import dayjs, { type Dayjs } from 'dayjs'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { App, Button, Card, Collapse, DatePicker, Empty, Form, Input, Modal, Pagination, Popconfirm, Select, Skeleton, Space, Switch, Table, Tag, Typography } from 'antd'
import { CopyOutlined, DeleteOutlined, EditOutlined, PlusOutlined } from '@ant-design/icons'
import { api, type DeleteResult, type Page, type Provider } from '../api'

type ProviderKeyForm = { id?: number; name?: string; secret?: string; expires_at?: Dayjs | null; enabled?: boolean }
type ProviderForm = {
  name: string
  auth_type: 'bearer' | 'x-api-key' | 'custom'
  auth_header?: string
  chat_completions?: string
  responses?: string
  messages?: string
  static_headers?: string
  quota_codes?: string
  keys: ProviderKeyForm[]
}

const providerTemplates: Array<{ name: string; chat_completions: string; responses: string; messages: string }> = [
  {
    name: '火山方舟 Agent Plan',
    chat_completions: 'https://ark.cn-beijing.volces.com/api/plan/v3/chat/completions',
    responses: 'https://ark.cn-beijing.volces.com/api/plan/v3/responses',
    messages: 'https://ark.cn-beijing.volces.com/api/plan/v1/messages',
  },
  {
    name: '火山方舟 Coding Plan',
    chat_completions: 'https://ark.cn-beijing.volces.com/api/coding/v3/chat/completions',
    responses: 'https://ark.cn-beijing.volces.com/api/coding/v3/responses',
    messages: 'https://ark.cn-beijing.volces.com/api/coding/v1/messages',
  },
  {
    name: 'DeepSeek',
    chat_completions: 'https://api.deepseek.com/chat/completions',
    responses: 'https://api.deepseek.com/v1/responses',
    messages: 'https://api.deepseek.com/anthropic/v1/messages',
  },
  {
    name: '百炼按量计费',
    chat_completions: 'https://{WorkspaceId}.cn-beijing.maas.aliyuncs.com/compatible-mode/v1/chat/completions',
    responses: 'https://{WorkspaceId}.cn-beijing.maas.aliyuncs.com/compatible-mode/v1/responses',
    messages: 'https://{WorkspaceId}.cn-beijing.maas.aliyuncs.com/apps/anthropic/v1/messages',
  },
  {
    name: 'OpenRouter',
    chat_completions: 'https://openrouter.ai/api/v1/chat/completions',
    responses: 'https://openrouter.ai/api/v1/responses',
    messages: 'https://openrouter.ai/api/v1/messages',
  },
  {
    name: 'Groq',
    chat_completions: 'https://api.groq.com/openai/v1/chat/completions',
    responses: 'https://api.groq.com/openai/v1/responses',
    messages: '',
  },
  {
    name: 'SiliconFlow',
    chat_completions: 'https://api.siliconflow.cn/v1/chat/completions',
    responses: '',
    messages: 'https://api.siliconflow.cn/v1/messages',
  },
  {
    name: 'dots studio',
    chat_completions: 'https://note3-prev-api.askdiandian.com/v1/chat/completions',
    responses: '',
    messages: 'https://note3-prev-api.askdiandian.com/v1/messages',
  },
  {
    name: 'TeamoRouter',
    chat_completions: 'https://api.teamorouter.com/v1/chat/completions',
    responses: 'https://api.teamorouter.com/v1/responses',
    messages: 'https://api.teamorouter.com/v1/messages',
  },
]

export default function ProvidersPage() {
  const { message } = App.useApp()
  const [open, setOpen] = useState(false)
  const [editing, setEditing] = useState<Provider | null>(null)
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(10)
  const [nameInput, setNameInput] = useState('')
  const [name, setName] = useState('')
  const [enabled, setEnabled] = useState('')
  const [authType, setAuthType] = useState('')
  const [form] = Form.useForm<ProviderForm>()
  const queryClient = useQueryClient()
  const providers = useQuery({
    queryKey: ['providers', page, pageSize, name, enabled, authType],
    queryFn: () => {
      const params = new URLSearchParams({ page: String(page), page_size: String(pageSize) })
      if (name) params.set('name', name)
      if (enabled) params.set('enabled', enabled)
      if (authType) params.set('auth_type', authType)
      return api<Page<Provider>>(`/api/admin/providers?${params}`)
    },
  })
  const save = useMutation({
    mutationFn: (values: ProviderForm) => {
      const endpoints = Object.fromEntries([
        ['chat_completions', values.chat_completions], ['responses', values.responses], ['messages', values.messages],
      ].filter((entry): entry is [string, string] => !!entry[1]))
      let staticHeaders: Record<string, string> = {}
      if (values.static_headers?.trim()) staticHeaders = JSON.parse(values.static_headers)
      const body = JSON.stringify({
        name: values.name,
        auth_type: values.auth_type,
        auth_header: values.auth_header ?? '',
        static_headers: staticHeaders,
        quota_codes: values.quota_codes?.split('\n').map((value) => value.trim()).filter(Boolean) ?? [],
        endpoints,
        keys: values.keys.map((key) => ({ id: key.id ?? 0, name: key.name ?? '', secret: key.id && editing?.keys.find((item) => item.id === key.id)?.secret === key.secret ? '' : key.secret ?? '', enabled: key.enabled ?? true, expires_at: key.expires_at?.toISOString() ?? null })),
      })
      return api(editing ? `/api/admin/providers/${editing.id}` : '/api/admin/providers', { method: editing ? 'PUT' : 'POST', body })
    },
    onSuccess: () => {
      message.success(editing ? '供应商已更新' : '供应商已创建')
      setOpen(false)
      setEditing(null)
      form.resetFields()
      queryClient.invalidateQueries({ queryKey: ['providers'] })
      queryClient.invalidateQueries({ queryKey: ['provider-options'] })
      queryClient.invalidateQueries({ queryKey: ['overview'] })
    },
    onError: (error) => message.error(error.message),
  })
  const toggle = useMutation({
    mutationFn: ({ id, enabled }: { id: number; enabled: boolean }) => api(`/api/admin/providers/${id}`, { method: 'PATCH', body: JSON.stringify({ enabled }) }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['providers'] })
      queryClient.invalidateQueries({ queryKey: ['provider-options'] })
    },
    onError: (error) => message.error(error.message),
  })
  const remove = useMutation({
    mutationFn: (id: number) => api<DeleteResult>(`/api/admin/providers/${id}`, { method: 'DELETE' }),
    onSuccess: (result) => {
      message.success(result.archived ? '供应商已归档，历史记录保持不变' : '供应商已删除')
      queryClient.invalidateQueries({ queryKey: ['providers'] })
      queryClient.invalidateQueries({ queryKey: ['provider-options'] })
      queryClient.invalidateQueries({ queryKey: ['overview'] })
    },
    onError: (error) => message.error(error.message),
  })

  function createProvider() {
    setEditing(null)
    form.resetFields()
    form.setFieldsValue({ auth_type: 'bearer', keys: [{ enabled: true }] })
    setOpen(true)
  }

  function editProvider(provider: Provider) {
    setEditing(provider)
    form.setFieldsValue({
      name: provider.name,
      auth_type: provider.auth_type,
      auth_header: provider.auth_header,
      chat_completions: provider.endpoints.chat_completions,
      responses: provider.endpoints.responses,
      messages: provider.endpoints.messages,
      static_headers: Object.keys(provider.static_headers).length ? JSON.stringify(provider.static_headers, null, 2) : '',
      quota_codes: provider.quota_codes.join('\n'),
      keys: provider.keys.map((key) => ({ id: key.id, name: key.name, secret: key.secret, enabled: key.enabled, expires_at: key.expires_at ? dayjs(key.expires_at) : null })),
    })
    setOpen(true)
  }

  function applyTemplate(template: typeof providerTemplates[number]) {
    form.setFieldsValue(template)
  }

  return (
    <div className="mc-enter mx-auto max-w-[1500px]">
      <PageHeader title="供应商"><Button type="primary" icon={<PlusOutlined />} onClick={createProvider}>新增供应商</Button></PageHeader>
      <div className="mb-4 flex flex-col gap-2 sm:flex-row">
        <Input.Search allowClear enterButton="搜索" placeholder="搜索名称" className="min-w-0 sm:max-w-[360px]" value={nameInput} onChange={(event) => { const value = event.target.value; setNameInput(value); if (!value) { setName(''); setPage(1) } }} onSearch={(value) => { setName(value.trim()); setPage(1) }} />
        <Select allowClear placeholder="状态" className="w-full sm:w-[140px]" value={enabled || undefined} onChange={(value) => { setEnabled(value ?? ''); setPage(1) }} options={[{ value: 'true', label: '启用' }, { value: 'false', label: '停用' }]} />
        <Select allowClear placeholder="鉴权方式" className="w-full sm:w-[180px]" value={authType || undefined} onChange={(value) => { setAuthType(value ?? ''); setPage(1) }} options={[{ value: 'bearer', label: 'Authorization: Bearer' }, { value: 'x-api-key', label: 'x-api-key' }, { value: 'custom', label: '自定义请求头' }]} />
      </div>
      <div className="hidden lg:block">
        <Table rowKey="id" loading={providers.isPending} dataSource={providers.data?.items ?? []} scroll={{ x: 900, y: 'calc(100vh - 330px)' }} pagination={{ current: page, pageSize, total: providers.data?.total ?? 0, showSizeChanger: true, pageSizeOptions: [10, 20, 50], onChange: (nextPage, nextPageSize) => { setPage(nextPageSize === pageSize ? nextPage : 1); setPageSize(nextPageSize) }, showTotal: (total) => `共 ${total} 条` }} locale={{ emptyText: <Empty className="py-14" description={name || enabled || authType ? '没有匹配的供应商' : '暂无供应商'} /> }} expandable={{ expandedRowRender: (record) => <ProviderDetails record={record} /> }} columns={[
          { title: '供应商', dataIndex: 'name', render: (value) => <strong>{value}</strong> },
          { title: '鉴权', dataIndex: 'auth_type', render: (value) => <Tag>{value}</Tag> },
          { title: '协议', dataIndex: 'endpoints', render: (value) => <Space wrap>{Object.keys(value).map((item) => <Tag color="orange" key={item}>{item}</Tag>)}</Space> },
          { title: '密钥池', dataIndex: 'keys', render: (value) => `${value.length} 把` },
          { title: '启用', dataIndex: 'enabled', align: 'right', render: (enabled, record) => <Switch checked={enabled} onChange={(value) => toggle.mutate({ id: record.id, enabled: value })} /> },
          { title: '操作', width: 108, align: 'right', render: (_, record) => <Space size={4}><Button type="text" icon={<EditOutlined />} onClick={() => editProvider(record)} /><Popconfirm title="删除供应商？" description="仍被模型路由使用时将拒绝删除。" okText="删除" cancelText="取消" okButtonProps={{ danger: true }} onConfirm={() => remove.mutate(record.id)}><Button type="text" danger icon={<DeleteOutlined />} /></Popconfirm></Space> },
        ]} />
      </div>
      <div className="space-y-3 lg:hidden">
        {providers.isPending ? <Card><Skeleton active paragraph={{ rows: 3 }} /></Card> : providers.data?.items.length ? providers.data.items.map((provider) => (
          <Card key={provider.id} size="small">
            <div className="mb-3 flex items-start justify-between gap-3">
              <div className="min-w-0"><strong className="break-all">{provider.name}</strong><div className="mt-1 text-xs text-[#7c8d86]">{provider.auth_type} · {provider.keys.length} 把密钥</div></div>
            </div>
            <Space wrap size={[4, 4]} className="mb-3">{Object.keys(provider.endpoints).map((item) => <Tag color="orange" key={item}>{item}</Tag>)}</Space>
            <Collapse ghost size="small" items={[{ key: 'details', label: '端点与密钥池', children: <ProviderDetails record={provider} compact /> }]} />
            <div className="mt-2 flex items-center justify-end gap-1">
              <Switch size="small" checked={provider.enabled} onChange={(value) => toggle.mutate({ id: provider.id, enabled: value })} />
              <Button type="text" icon={<EditOutlined />} aria-label={`编辑 ${provider.name}`} onClick={() => editProvider(provider)} />
              <Popconfirm title="删除供应商？" description="仍被模型路由使用时将拒绝删除。" okText="删除" cancelText="取消" okButtonProps={{ danger: true }} onConfirm={() => remove.mutate(provider.id)}><Button type="text" danger icon={<DeleteOutlined />} aria-label={`删除 ${provider.name}`} /></Popconfirm>
            </div>
          </Card>
        )) : <Card><Empty className="py-8" description={name || enabled || authType ? '没有匹配的供应商' : '暂无供应商'} /></Card>}
        {!!providers.data?.total && <div className="flex justify-center pt-2"><Pagination simple current={page} pageSize={pageSize} total={providers.data.total} onChange={(nextPage) => setPage(nextPage)} /></div>}
      </div>
      <Modal wrapClassName="mc-responsive-modal" title={editing ? '编辑供应商' : '新增供应商'} width={760} open={open} onCancel={() => { setOpen(false); setEditing(null) }} onOk={() => form.submit()} confirmLoading={save.isPending} okText="保存">
        <Form form={form} layout="vertical" onFinish={(values) => save.mutate(values)} className="pt-4">
          <div className="mb-5 flex flex-col gap-3 rounded border border-black/10 bg-black/[.015] px-4 py-3 dark:border-white/10 dark:bg-white/[.02] sm:flex-row sm:items-center sm:justify-between"><span className="text-sm text-[#7c8d86]">供应商模板</span><Space wrap>{providerTemplates.map((template) => <Button key={template.name} size="small" onClick={() => applyTemplate(template)}>{template.name}</Button>)}</Space></div>
          <div className="grid grid-cols-1 gap-x-4 sm:grid-cols-2"><Form.Item name="name" label="供应商名称" rules={[{ required: true }]}><Input placeholder="例如：OpenCode Go" /></Form.Item><Form.Item name="auth_type" label="上游鉴权" rules={[{ required: true }]}><Select options={[{ value: 'bearer', label: 'Authorization: Bearer' }, { value: 'x-api-key', label: 'x-api-key' }, { value: 'custom', label: '自定义请求头' }]} /></Form.Item></div>
          <Form.Item noStyle shouldUpdate={(before, after) => before.auth_type !== after.auth_type}>{({ getFieldValue }) => getFieldValue('auth_type') === 'custom' ? <Form.Item name="auth_header" label="自定义鉴权头" rules={[{ required: true }]}><Input placeholder="例如：api-key" /></Form.Item> : null}</Form.Item>
          <Collapse ghost items={[{ key: 'endpoints', label: '协议端点', children: <div className="grid gap-1"><Form.Item name="chat_completions" label="Chat Completions 完整 URL"><Input placeholder="https://…/v1/chat/completions" /></Form.Item><Form.Item name="responses" label="Responses 完整 URL"><Input placeholder="https://…/v1/responses" /></Form.Item><Form.Item name="messages" label="Messages 完整 URL"><Input placeholder="https://…/v1/messages" /></Form.Item></div> }, { key: 'advanced', label: '高级设置', children: <><Form.Item name="quota_codes" label="额度耗尽错误码（一行一个）"><Input.TextArea rows={3} placeholder="AccountQuotaExceeded" /></Form.Item><Form.Item name="static_headers" label="静态请求头（JSON 对象）" rules={[{ validator: validateStaticHeaders }]}><Input.TextArea rows={3} placeholder={'{"X-Custom-Header":"value"}'} className="font-mono" /></Form.Item></> }]} />
          <div className="mb-3 mt-5 flex items-center justify-between"><span className="font-medium">上游密钥池</span></div>
          <Form.List name="keys">{(fields, { add, remove: removeField }) => <div className="space-y-3">{fields.map(({ key, ...field }, index) => <div key={key} className="grid grid-cols-[minmax(0,1fr)_36px] gap-3 rounded border border-black/10 p-3 dark:border-white/10 sm:grid-cols-[1fr_2fr_1.2fr_52px_36px]"><Form.Item {...field} name={[field.name, 'id']} hidden><Input /></Form.Item><Form.Item {...field} name={[field.name, 'name']} className="!mb-0 col-span-2 sm:col-span-1"><Input placeholder={`备注 ${index + 1}`} /></Form.Item><Form.Item {...field} name={[field.name, 'secret']} className="!mb-0 col-span-2 sm:col-span-1" rules={[{ validator: (_, value) => form.getFieldValue(['keys', field.name, 'id']) || value ? Promise.resolve() : Promise.reject(new Error('请输入密钥')) }]}><Input placeholder="上游密钥" suffix={<CopyOutlined className="cursor-pointer text-[#7c8d86]" onClick={() => { navigator.clipboard.writeText(form.getFieldValue(['keys', field.name, 'secret']) ?? ''); message.success('密钥已复制') }} />} /></Form.Item><Form.Item {...field} name={[field.name, 'expires_at']} className="!mb-0 col-span-2 sm:col-span-1"><DatePicker showTime placeholder="永不过期" className="w-full" /></Form.Item><div className="flex items-center justify-start sm:justify-center"><Form.Item {...field} name={[field.name, 'enabled']} noStyle valuePropName="checked"><Switch /></Form.Item></div><Button type="text" danger icon={<DeleteOutlined />} disabled={fields.length === 1} onClick={() => removeField(field.name)} /></div>)}<Button type="dashed" icon={<PlusOutlined />} onClick={() => add({ enabled: true })} block>添加密钥</Button></div>}</Form.List>
        </Form>
      </Modal>
    </div>
  )
}

function ProviderDetails({ record, compact = false }: { record: Provider; compact?: boolean }) {
  return <div className={`grid grid-cols-1 gap-5 ${compact ? '' : 'p-4 lg:grid-cols-2 lg:gap-6'}`}>
    <div><div className="mb-3 text-sm font-medium text-[#7c8d86]">协议端点</div>{Object.entries(record.endpoints).map(([key, value]) => <div key={key} className="mb-2 flex flex-col gap-1 sm:flex-row sm:gap-3"><Tag className="w-fit">{key}</Tag><code className="break-all text-xs">{value}</code></div>)}</div>
    <div><div className="mb-3 text-sm font-medium text-[#7c8d86]">上游密钥池</div>{record.keys.map((key) => <div key={key.id} className="mb-2 grid grid-cols-[minmax(0,1fr)_auto] items-center gap-2 border-b border-black/5 pb-2 dark:border-white/5"><span className="min-w-0 break-all">{key.name || `密钥 ${key.position + 1}`} · <code className="text-xs">{key.secret || '—'}</code></span><Tag color={key.runtime_status === 'available' ? 'success' : key.runtime_status === 'auth_invalid' ? 'error' : 'warning'}>{runtimeStatusName(key.runtime_status)}</Tag></div>)}</div>
  </div>
}

function PageHeader({ title, children }: { title: string; children: React.ReactNode }) {
  return <div className="mb-6 flex items-center justify-between gap-4 sm:mb-8"><Typography.Title level={2} className="!mb-0 !tracking-[-.04em]">{title}</Typography.Title>{children}</div>
}

function runtimeStatusName(value: string): string {
  return ({ available: '可用', auth_invalid: '鉴权失效', quota_exhausted: '额度耗尽', rate_limited: '限流冷却' } as Record<string, string>)[value] ?? value
}

function validateStaticHeaders(_: unknown, value?: string): Promise<void> {
  if (!value?.trim()) return Promise.resolve()
  try {
    const parsed: unknown = JSON.parse(value)
    if (parsed == null || Array.isArray(parsed) || typeof parsed !== 'object' || Object.values(parsed).some((item) => typeof item !== 'string')) {
      return Promise.reject(new Error('静态请求头必须是字符串键值的 JSON 对象'))
    }
    return Promise.resolve()
  } catch {
    return Promise.reject(new Error('静态请求头必须是合法 JSON 对象'))
  }
}

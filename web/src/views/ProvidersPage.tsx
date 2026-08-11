import { useState } from 'react'
import dayjs, { type Dayjs } from 'dayjs'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Button, Collapse, DatePicker, Form, Input, Modal, Popconfirm, Select, Space, Switch, Table, Tag, Typography, message } from 'antd'
import { CopyOutlined, DeleteOutlined, EditOutlined, PlusOutlined } from '@ant-design/icons'
import { api, type DeleteResult, type Provider } from '../api'

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
]

export default function ProvidersPage() {
  const [open, setOpen] = useState(false)
  const [editing, setEditing] = useState<Provider | null>(null)
  const [form] = Form.useForm<ProviderForm>()
  const queryClient = useQueryClient()
  const providers = useQuery({ queryKey: ['providers'], queryFn: () => api<Provider[]>('/api/admin/providers') })
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
      queryClient.invalidateQueries({ queryKey: ['overview'] })
    },
    onError: (error) => message.error(error instanceof SyntaxError ? '静态请求头必须是合法 JSON 对象' : error.message),
  })
  const toggle = useMutation({
    mutationFn: ({ id, enabled }: { id: number; enabled: boolean }) => api(`/api/admin/providers/${id}`, { method: 'PATCH', body: JSON.stringify({ enabled }) }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['providers'] }),
    onError: (error) => message.error(error.message),
  })
  const remove = useMutation({
    mutationFn: (id: number) => api<DeleteResult>(`/api/admin/providers/${id}`, { method: 'DELETE' }),
    onSuccess: (result) => {
      message.success(result.archived ? '供应商已归档，历史记录保持不变' : '供应商已删除')
      queryClient.invalidateQueries({ queryKey: ['providers'] })
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
      <Table rowKey="id" loading={providers.isPending} dataSource={providers.data ?? []} pagination={false} expandable={{
        expandedRowRender: (record) => (
          <div className="grid grid-cols-2 gap-6 p-4">
            <div><div className="mb-3 text-sm font-medium text-[#7c8d86]">协议端点</div>{Object.entries(record.endpoints).map(([key, value]) => <div key={key} className="mb-2 flex gap-3"><Tag>{key}</Tag><code className="break-all text-xs">{value}</code></div>)}</div>
            <div><div className="mb-3 text-sm font-medium text-[#7c8d86]">上游密钥池</div>{record.keys.map((key) => <div key={key.id} className="mb-2 grid grid-cols-[minmax(0,1fr)_auto] items-center gap-3 border-b border-black/5 pb-2 dark:border-white/5"><span className="min-w-0">{key.name || `密钥 ${key.position + 1}`} · <code className="break-all text-xs">{key.secret || '—'}</code></span><Tag color={key.runtime_status === 'available' ? 'success' : key.runtime_status === 'auth_invalid' ? 'error' : 'warning'}>{runtimeStatusName(key.runtime_status)}</Tag></div>)}</div>
          </div>
        ),
      }} columns={[
        { title: '供应商', dataIndex: 'name', render: (value) => <strong>{value}</strong> },
        { title: '鉴权', dataIndex: 'auth_type', render: (value) => <Tag>{value}</Tag> },
        { title: '协议', dataIndex: 'endpoints', render: (value) => <Space wrap>{Object.keys(value).map((item) => <Tag color="orange" key={item}>{item}</Tag>)}</Space> },
        { title: '密钥池', dataIndex: 'keys', render: (value) => `${value.length} 把` },
        { title: '状态', dataIndex: 'enabled', render: (value) => value ? <Tag color="success">启用</Tag> : <Tag>停用</Tag> },
        { title: '启用', dataIndex: 'enabled', align: 'right', render: (enabled, record) => <Switch checked={enabled} onChange={(value) => toggle.mutate({ id: record.id, enabled: value })} /> },
        { title: '操作', width: 108, align: 'right', render: (_, record) => <Space size={4}><Button type="text" icon={<EditOutlined />} onClick={() => editProvider(record)} /><Popconfirm title="删除供应商？" description="仍被模型路由使用时将拒绝删除。" okText="删除" cancelText="取消" okButtonProps={{ danger: true }} onConfirm={() => remove.mutate(record.id)}><Button type="text" danger icon={<DeleteOutlined />} /></Popconfirm></Space> },
      ]} />
      <Modal title={editing ? '编辑供应商' : '新增供应商'} width={760} open={open} onCancel={() => { setOpen(false); setEditing(null) }} onOk={() => form.submit()} confirmLoading={save.isPending} okText="保存">
        <Form form={form} layout="vertical" onFinish={(values) => save.mutate(values)} className="pt-4">
          <div className="mb-5 flex items-center justify-between rounded border border-black/10 bg-black/[.015] px-4 py-3 dark:border-white/10 dark:bg-white/[.02]"><span className="text-sm text-[#7c8d86]">供应商模板</span><Space>{providerTemplates.map((template) => <Button key={template.name} size="small" onClick={() => applyTemplate(template)}>{template.name}</Button>)}</Space></div>
          <div className="grid grid-cols-2 gap-4"><Form.Item name="name" label="供应商名称" rules={[{ required: true }]}><Input placeholder="例如：OpenCode Go" /></Form.Item><Form.Item name="auth_type" label="上游鉴权" rules={[{ required: true }]}><Select options={[{ value: 'bearer', label: 'Authorization: Bearer' }, { value: 'x-api-key', label: 'x-api-key' }, { value: 'custom', label: '自定义请求头' }]} /></Form.Item></div>
          <Form.Item noStyle shouldUpdate={(before, after) => before.auth_type !== after.auth_type}>{({ getFieldValue }) => getFieldValue('auth_type') === 'custom' ? <Form.Item name="auth_header" label="自定义鉴权头" rules={[{ required: true }]}><Input placeholder="例如：api-key" /></Form.Item> : null}</Form.Item>
          <Collapse ghost items={[{ key: 'endpoints', label: '协议端点', children: <div className="grid gap-1"><Form.Item name="chat_completions" label="Chat Completions 完整 URL"><Input placeholder="https://…/v1/chat/completions" /></Form.Item><Form.Item name="responses" label="Responses 完整 URL"><Input placeholder="https://…/v1/responses" /></Form.Item><Form.Item name="messages" label="Messages 完整 URL"><Input placeholder="https://…/v1/messages" /></Form.Item></div> }, { key: 'advanced', label: '高级设置', children: <><Form.Item name="quota_codes" label="额度耗尽错误码（一行一个）"><Input.TextArea rows={3} placeholder="AccountQuotaExceeded" /></Form.Item><Form.Item name="static_headers" label="静态请求头（JSON 对象）"><Input.TextArea rows={3} placeholder={'{"X-Custom-Header":"value"}'} className="font-mono" /></Form.Item></> }]} />
          <div className="mb-3 mt-5 flex items-center justify-between"><span className="font-medium">上游密钥池</span></div>
          <Form.List name="keys">{(fields, { add, remove: removeField }) => <div className="space-y-3">{fields.map((field, index) => <div key={field.key} className="grid grid-cols-[1fr_2fr_1.2fr_52px_36px] gap-3 rounded border border-black/10 p-3 dark:border-white/10"><Form.Item {...field} name={[field.name, 'id']} hidden><Input /></Form.Item><Form.Item {...field} name={[field.name, 'name']} noStyle><Input placeholder={`备注 ${index + 1}`} /></Form.Item><Form.Item {...field} name={[field.name, 'secret']} noStyle rules={[{ validator: (_, value) => form.getFieldValue(['keys', field.name, 'id']) || value ? Promise.resolve() : Promise.reject(new Error('请输入密钥')) }]}><Input placeholder="上游密钥" suffix={<CopyOutlined className="cursor-pointer text-[#7c8d86]" onClick={() => { navigator.clipboard.writeText(form.getFieldValue(['keys', field.name, 'secret']) ?? ''); message.success('密钥已复制') }} />} /></Form.Item><Form.Item {...field} name={[field.name, 'expires_at']} noStyle><DatePicker showTime placeholder="永不过期" /></Form.Item><Form.Item {...field} name={[field.name, 'enabled']} noStyle valuePropName="checked"><Switch /></Form.Item><Button type="text" danger icon={<DeleteOutlined />} disabled={fields.length === 1} onClick={() => removeField(field.name)} /></div>)}<Button type="dashed" icon={<PlusOutlined />} onClick={() => add({ enabled: true })} block>添加密钥</Button></div>}</Form.List>
        </Form>
      </Modal>
    </div>
  )
}

function PageHeader({ title, children }: { title: string; children: React.ReactNode }) {
  return <div className="mb-8 flex items-end justify-between"><Typography.Title level={2} className="!mb-0 !tracking-[-.04em]">{title}</Typography.Title>{children}</div>
}

function runtimeStatusName(value: string): string {
  return ({ available: '可用', auth_invalid: '鉴权失效', quota_exhausted: '额度耗尽', rate_limited: '限流冷却' } as Record<string, string>)[value] ?? value
}

import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { App, Button, Collapse, Empty, Form, Input, InputNumber, Modal, Popconfirm, Select, Space, Switch, Table, Tag, Tooltip, Typography } from 'antd'
import { ArrowDownOutlined, ArrowUpOutlined, DeleteOutlined, EditOutlined, ExperimentOutlined, PlusOutlined } from '@ant-design/icons'
import { api, type CandidateProtocol, type DeleteResult, type ModelTestResult, type Page, type ProviderOption, type VirtualModel } from '../api'
import JsonPayload from '../components/JsonPayload'

const protocolLabels: Record<CandidateProtocol['protocol'], string> = {
  chat_completions: 'Chat Completions',
  responses: 'Responses',
  messages: 'Messages',
}

type CandidateForm = { id?: number; provider_id?: number; upstream_model?: string; default_max_output_tokens: number; max_output_tokens: number; protocols: CandidateProtocol['protocol'][] }
type ModelForm = { name: string; candidates: CandidateForm[] }

export default function ModelsPage() {
  const [open, setOpen] = useState(false)
  const [editing, setEditing] = useState<VirtualModel | null>(null)
  const [testing, setTesting] = useState<VirtualModel | null>(null)
  const [testPrompt, setTestPrompt] = useState('请简短回复：模型连接正常')
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(10)
  const [nameInput, setNameInput] = useState('')
  const [name, setName] = useState('')
  const [enabled, setEnabled] = useState('')
  const [testResult, setTestResult] = useState<ModelTestResult | null>(null)
  const [form] = Form.useForm<ModelForm>()
  const { message } = App.useApp()
  const queryClient = useQueryClient()
  const providers = useQuery({ queryKey: ['provider-options'], queryFn: () => api<ProviderOption[]>('/api/admin/provider-options') })
  const models = useQuery({
    queryKey: ['models', page, pageSize, name, enabled],
    queryFn: () => {
      const params = new URLSearchParams({ page: String(page), page_size: String(pageSize) })
      if (name) params.set('name', name)
      if (enabled) params.set('enabled', enabled)
      return api<Page<VirtualModel>>(`/api/admin/models?${params}`)
    },
  })
  const save = useMutation({
    mutationFn: (values: ModelForm) => api(editing ? `/api/admin/models/${editing.id}` : '/api/admin/models', {
      method: editing ? 'PUT' : 'POST',
      body: JSON.stringify({
        name: values.name,
        candidates: values.candidates.map((candidate) => ({
          ...candidate,
          id: candidate.id ?? 0,
          protocols: candidate.protocols.map((protocol, position) => {
            const existing = editing?.candidates.find((item) => item.id === candidate.id)?.protocols.find((item) => item.protocol === protocol)
            return existing ? { ...existing, position } : defaultProtocol(protocol, position)
          }),
        })),
      }),
    }),
    onSuccess: () => {
      message.success(editing ? '模型路由已更新' : '模型路由已创建')
      setOpen(false)
      setEditing(null)
      form.resetFields()
      queryClient.invalidateQueries({ queryKey: ['models'] })
      queryClient.invalidateQueries({ queryKey: ['overview'] })
    },
    onError: (error) => message.error(error.message),
  })
  const toggle = useMutation({
    mutationFn: ({ id, enabled }: { id: number; enabled: boolean }) => api(`/api/admin/models/${id}`, { method: 'PATCH', body: JSON.stringify({ enabled }) }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['models'] }),
    onError: (error) => message.error(error.message),
  })
  const remove = useMutation({
    mutationFn: (id: number) => api<DeleteResult>(`/api/admin/models/${id}`, { method: 'DELETE' }),
    onSuccess: (result) => {
      message.success(result.archived ? '模型路由已归档，历史记录保持不变' : '模型路由已删除')
      queryClient.invalidateQueries({ queryKey: ['models'] })
      queryClient.invalidateQueries({ queryKey: ['overview'] })
    },
    onError: (error) => message.error(error.message),
  })
  const test = useMutation({
    mutationFn: ({ id, prompt }: { id: number; prompt: string }) => api<ModelTestResult>(`/api/admin/models/${id}/test`, { method: 'POST', body: JSON.stringify({ prompt }) }),
    onSuccess: (result) => {
      setTestResult(result)
      message.success('模型测试完成')
      queryClient.invalidateQueries({ queryKey: ['requests'] })
      queryClient.invalidateQueries({ queryKey: ['overview'] })
    },
    onError: (error) => message.error(error.message),
  })

  function createModel() {
    setEditing(null)
    form.resetFields()
    form.setFieldsValue({ candidates: [{ default_max_output_tokens: 16384, max_output_tokens: 65536, protocols: [] }] })
    setOpen(true)
  }

  function editModel(model: VirtualModel) {
    setEditing(model)
    form.setFieldsValue({
      name: model.name,
      candidates: model.candidates.map((candidate) => {
        const available = configuredProtocols(candidate.provider_id, providers.data)
        return { id: candidate.id, provider_id: candidate.provider_id, upstream_model: candidate.upstream_model, default_max_output_tokens: candidate.default_max_output_tokens, max_output_tokens: candidate.max_output_tokens, protocols: candidate.protocols.map((item) => item.protocol).filter((protocol) => available.includes(protocol)) }
      }),
    })
    setOpen(true)
  }

  function testModel(model: VirtualModel) {
    setTesting(model)
    setTestResult(null)
  }

  return (
    <div className="mc-enter mx-auto max-w-[1500px]">
      <div className="mb-6 flex items-center justify-between gap-3 sm:mb-8"><Typography.Title level={2} className="!mb-0 !tracking-[-.04em]">模型路由</Typography.Title><Button type="primary" icon={<PlusOutlined />} onClick={createModel} disabled={!providers.data?.length}>新增虚拟模型</Button></div>
      <div className="mb-4 flex flex-col gap-2 sm:flex-row">
        <Input.Search allowClear enterButton="搜索" placeholder="搜索虚拟模型" className="min-w-0 sm:max-w-[360px]" value={nameInput} onChange={(event) => { const value = event.target.value; setNameInput(value); if (!value) { setName(''); setPage(1) } }} onSearch={(value) => { setName(value.trim()); setPage(1) }} />
        <Select allowClear placeholder="状态" className="w-full sm:w-[140px]" value={enabled || undefined} onChange={(value) => { setEnabled(value ?? ''); setPage(1) }} options={[{ value: 'true', label: '启用' }, { value: 'false', label: '停用' }]} />
      </div>
      <div>
        <Table rowKey="id" loading={models.isPending} dataSource={models.data?.items ?? []} scroll={{ x: 900, y: 'calc(100vh - 330px)' }} pagination={{ current: page, pageSize, total: models.data?.total ?? 0, showSizeChanger: true, pageSizeOptions: [10, 20, 50], onChange: (nextPage, nextPageSize) => { setPage(nextPageSize === pageSize ? nextPage : 1); setPageSize(nextPageSize) }, showTotal: (total) => `共 ${total} 条` }} locale={{ emptyText: <Empty className="py-14" description={name || enabled ? '没有匹配的模型路由' : '暂无模型路由'} /> }} expandable={{ expandedRowRender: (model) => <ModelCandidates model={model} /> }} columns={[
          { title: '虚拟模型', dataIndex: 'name', render: (value) => <code className="text-sm font-medium">{value}</code> },
          { title: '候选', dataIndex: 'candidates', render: (value) => `${value.length} 条有序路由` },
          { title: '首选供应商', dataIndex: 'candidates', render: (value) => value[0]?.provider_name ?? '—' },
          { title: '启用', dataIndex: 'enabled', align: 'right', render: (enabled, record) => <Switch checked={enabled} onChange={(value) => toggle.mutate({ id: record.id, enabled: value })} /> },
          { title: '操作', width: 148, align: 'right', render: (_, record) => <Space size={4}><Tooltip title={record.enabled ? '测试模型' : '启用后可测试'}><span><Button type="text" icon={<ExperimentOutlined />} aria-label={`测试 ${record.name}`} disabled={!record.enabled} onClick={() => testModel(record)} /></span></Tooltip><Button type="text" icon={<EditOutlined />} aria-label={`编辑 ${record.name}`} onClick={() => editModel(record)} /><Popconfirm title="删除模型路由？" description="已有历史请求时将转为归档。" okText="删除" cancelText="取消" okButtonProps={{ danger: true }} onConfirm={() => remove.mutate(record.id)}><Button type="text" danger icon={<DeleteOutlined />} aria-label={`删除 ${record.name}`} /></Popconfirm></Space> },
        ]} />
      </div>
      <Modal wrapClassName="mc-responsive-modal" title={editing ? '编辑模型路由' : '新增虚拟模型'} width={820} open={open} onCancel={() => { setOpen(false); setEditing(null) }} onOk={() => form.submit()} confirmLoading={save.isPending} okText="保存路由">
        <Form form={form} layout="vertical" onFinish={(values) => save.mutate(values)} className="pt-4">
          <Form.Item name="name" label="虚拟模型名" rules={[{ required: true }]}><Input placeholder="例如：coding-primary" className="font-mono" /></Form.Item>
          <Form.List name="candidates">{(fields, { add, remove: removeField, move }) => <div className="space-y-4">{fields.map(({ key, ...field }, index) => <div key={key} className="rounded border border-black/10 p-3 dark:border-white/10 sm:p-4"><Form.Item {...field} name={[field.name, 'id']} hidden><Input /></Form.Item><div className="mb-3 flex items-center justify-between"><span className="text-sm font-medium text-[#d7783d]">候选 {String(index + 1).padStart(2, '0')}</span><Space size={2}><Tooltip title="上移候选"><span><Button type="text" icon={<ArrowUpOutlined />} aria-label={`上移候选 ${index + 1}`} disabled={index === 0} onClick={() => move(field.name, field.name - 1)} /></span></Tooltip><Tooltip title="下移候选"><span><Button type="text" icon={<ArrowDownOutlined />} aria-label={`下移候选 ${index + 1}`} disabled={index === fields.length - 1} onClick={() => move(field.name, field.name + 1)} /></span></Tooltip><Button type="text" danger icon={<DeleteOutlined />} aria-label={`删除候选 ${index + 1}`} disabled={fields.length === 1} onClick={() => removeField(field.name)} /></Space></div><div className="grid grid-cols-1 gap-x-4 sm:grid-cols-2"><Form.Item {...field} name={[field.name, 'provider_id']} label="供应商" rules={[{ required: true }]}><Select options={providers.data?.map((item) => ({ value: item.id, label: item.enabled ? item.name : `${item.name}（停用）` }))} onChange={(providerID) => { const available = configuredProtocols(providerID, providers.data); form.setFieldValue(['candidates', field.name, 'protocols'], available.length ? [available[0]] : []) }} /></Form.Item><Form.Item {...field} name={[field.name, 'upstream_model']} label="真实模型名" rules={[{ required: true }]}><Input className="font-mono" /></Form.Item></div><Form.Item noStyle shouldUpdate>{({ getFieldValue }) => <Form.Item name={[field.name, 'protocols']} label="协议入口及后备顺序" rules={[{ required: true, message: '请选择至少一个供应商已配置的协议' }]}><ProtocolOrderField available={configuredProtocols(getFieldValue(['candidates', field.name, 'provider_id']), providers.data)} /></Form.Item>}</Form.Item><Collapse ghost size="small" className="mt-2" items={[{ key: 'advanced', forceRender: true, label: '高级设置', children: <div className="grid grid-cols-1 gap-x-4 sm:grid-cols-2"><Form.Item {...field} name={[field.name, 'default_max_output_tokens']} label="默认最大输出 Token" tooltip="客户端未指定 max_tokens 时使用该默认值" rules={[{ required: true }]}><InputNumber min={1} className="!w-full" /></Form.Item><Form.Item {...field} name={[field.name, 'max_output_tokens']} label="输出 Token 上限" tooltip="该候选允许的最大输出，客户端请求超过上限时在调用上游前拒绝" rules={[{ required: true }, ({ getFieldValue }) => ({ validator: (_, value) => { const fallback = getFieldValue(['candidates', field.name, 'default_max_output_tokens']); if (value != null && fallback != null && value < fallback) return Promise.reject(new Error('输出上限不能小于默认最大输出')); return Promise.resolve() } })]}><InputNumber min={1} className="!w-full" /></Form.Item></div> }]} /></div>)}<Button type="dashed" icon={<PlusOutlined />} onClick={() => add({ default_max_output_tokens: 16384, max_output_tokens: 65536, protocols: [] })} block>添加后备候选</Button></div>}</Form.List>
        </Form>
      </Modal>
      <Modal wrapClassName="mc-responsive-modal" title="测试模型" width={720} open={!!testing} onCancel={() => { setTesting(null); setTestResult(null); test.reset() }} onOk={() => testing && test.mutate({ id: testing.id, prompt: testPrompt.trim() })} okButtonProps={{ disabled: !testPrompt.trim() }} confirmLoading={test.isPending} okText="发送测试" cancelText="关闭">
        {testing && <div className="space-y-5 pt-4">
          <div className="text-sm text-[#7c8d86]">虚拟模型 <code className="ml-2 text-inherit">{testing.name}</code></div>
          <Input.TextArea value={testPrompt} onChange={(event) => { setTestPrompt(event.target.value); setTestResult(null) }} autoSize={{ minRows: 3, maxRows: 8 }} placeholder="输入测试内容" />
          {testResult && <div>
            <div className="mb-2 flex flex-wrap items-center justify-between gap-2 text-xs text-[#7c8d86]"><span>模型响应</span><code className="break-all">{testResult.request_id}</code></div>
            <JsonPayload value={JSON.stringify(testResult.response)} />
          </div>}
        </div>}
      </Modal>
    </div>
  )
}

function ModelCandidates({ model }: { model: VirtualModel }) {
  return <div className="p-4">{model.candidates.map((candidate) => <div key={candidate.id} className="mb-3 grid grid-cols-[44px_1fr_1fr_2fr] items-start gap-4 border-b border-black/5 pb-3 dark:border-white/5"><span className="font-mono text-[#d7783d]">{String(candidate.position + 1).padStart(2, '0')}</span><strong>{candidate.provider_name}</strong><code className="break-all">{candidate.upstream_model}</code><Space wrap>{candidate.protocols.map((item) => <Tag key={item.protocol}>{protocolLabels[item.protocol]}</Tag>)}</Space></div>)}</div>
}

function ProtocolOrderField({ value = [], onChange, available }: { value?: CandidateProtocol['protocol'][]; onChange?: (value: CandidateProtocol['protocol'][]) => void; available: CandidateProtocol['protocol'][] }) {
  function replace(index: number, protocol: CandidateProtocol['protocol']) {
    const next = [...value]
    next[index] = protocol
    onChange?.(next)
  }

  function move(index: number, offset: number) {
    const next = [...value]
    const current = next[index]
    next[index] = next[index + offset]
    next[index + offset] = current
    onChange?.(next)
  }

  const remaining = available.filter((protocol) => !value.includes(protocol))
  return <div className="space-y-2 rounded border border-black/10 bg-black/[.015] p-2 dark:border-white/10 dark:bg-white/[.02] sm:p-3">
    {value.map((protocol, index) => <div key={`${protocol}-${index}`} className="grid grid-cols-[32px_minmax(0,1fr)_32px_32px_32px] items-center gap-1 sm:grid-cols-[34px_minmax(0,1fr)_32px_32px_32px] sm:gap-2">
      <span className="flex h-8 w-8 items-center justify-center rounded bg-[#d7783d]/10 font-mono text-xs text-[#d7783d]">{index + 1}</span>
      <Select value={protocol} onChange={(next) => replace(index, next)} options={available.filter((item) => item === protocol || !value.includes(item)).map((item) => ({ value: item, label: protocolLabels[item] }))} />
      <Button type="text" aria-label="上移" icon={<ArrowUpOutlined />} disabled={index === 0} onClick={() => move(index, -1)} />
      <Button type="text" aria-label="下移" icon={<ArrowDownOutlined />} disabled={index === value.length - 1} onClick={() => move(index, 1)} />
      <Button type="text" danger aria-label="移除协议" icon={<DeleteOutlined />} onClick={() => onChange?.(value.filter((_, itemIndex) => itemIndex !== index))} />
    </div>)}
    {!available.length && <Typography.Text type="secondary">所选供应商尚未配置协议端点</Typography.Text>}
    {!!remaining.length && <Button type="dashed" size="small" icon={<PlusOutlined />} onClick={() => onChange?.([...value, remaining[0]])}>添加协议入口</Button>}
  </div>
}

function configuredProtocols(providerID: number | undefined, providers?: ProviderOption[]): CandidateProtocol['protocol'][] {
  const endpoints = providers?.find((provider) => provider.id === providerID)?.endpoints ?? {}
  return (Object.keys(protocolLabels) as CandidateProtocol['protocol'][]).filter((protocol) => !!endpoints[protocol])
}

function defaultProtocol(protocol: CandidateProtocol['protocol'], position: number): CandidateProtocol {
  return { protocol, position, supports_stream: true, supports_tools: true, supports_parallel_tools: true, effort_levels: ['low', 'medium', 'high', 'xhigh'], supports_stream_usage: protocol === 'chat_completions' }
}

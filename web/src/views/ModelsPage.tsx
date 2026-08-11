import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Button, Card, Collapse, Empty, Form, Input, InputNumber, Modal, Popconfirm, Select, Skeleton, Space, Switch, Table, Tag, Typography, message } from 'antd'
import { ArrowDownOutlined, ArrowUpOutlined, DeleteOutlined, EditOutlined, PlusOutlined } from '@ant-design/icons'
import { api, type CandidateProtocol, type DeleteResult, type Provider, type VirtualModel } from '../api'

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
  const [form] = Form.useForm<ModelForm>()
  const queryClient = useQueryClient()
  const providers = useQuery({ queryKey: ['providers'], queryFn: () => api<Provider[]>('/api/admin/providers') })
  const models = useQuery({ queryKey: ['models'], queryFn: () => api<VirtualModel[]>('/api/admin/models') })
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

  return (
    <div className="mc-enter mx-auto max-w-[1500px]">
      <div className="mb-6 flex items-center justify-between gap-3 sm:mb-8"><Typography.Title level={2} className="!mb-0 !tracking-[-.04em]">模型路由</Typography.Title><Button type="primary" icon={<PlusOutlined />} onClick={createModel} disabled={!providers.data?.length}>新增虚拟模型</Button></div>
      <div className="hidden lg:block">
        <Table rowKey="id" loading={models.isPending} dataSource={models.data ?? []} pagination={false} scroll={{ x: 900 }} expandable={{ expandedRowRender: (model) => <ModelCandidates model={model} /> }} columns={[
          { title: '虚拟模型', dataIndex: 'name', render: (value) => <code className="text-sm font-medium">{value}</code> },
          { title: '候选', dataIndex: 'candidates', render: (value) => `${value.length} 条有序路由` },
          { title: '首选供应商', dataIndex: 'candidates', render: (value) => value[0]?.provider_name ?? '—' },
          { title: '状态', dataIndex: 'enabled', render: (value) => value ? <Tag color="success">启用</Tag> : <Tag>停用</Tag> },
          { title: '启用', dataIndex: 'enabled', align: 'right', render: (enabled, record) => <Switch checked={enabled} onChange={(value) => toggle.mutate({ id: record.id, enabled: value })} /> },
          { title: '操作', width: 108, align: 'right', render: (_, record) => <Space size={4}><Button type="text" icon={<EditOutlined />} onClick={() => editModel(record)} /><Popconfirm title="删除模型路由？" description="已有历史请求时将转为归档。" okText="删除" cancelText="取消" okButtonProps={{ danger: true }} onConfirm={() => remove.mutate(record.id)}><Button type="text" danger icon={<DeleteOutlined />} /></Popconfirm></Space> },
        ]} />
      </div>
      <div className="space-y-3 lg:hidden">
        {models.isPending ? <Card><Skeleton active paragraph={{ rows: 3 }} /></Card> : models.data?.length ? models.data.map((model) => (
          <Card key={model.id} size="small">
            <div className="mb-3 flex items-start justify-between gap-3">
              <div className="min-w-0"><code className="break-all text-sm font-medium">{model.name}</code><div className="mt-1 text-xs text-[#7c8d86]">{model.candidates.length} 条有序路由 · {model.candidates[0]?.provider_name ?? '无候选'}</div></div>
              <Tag className="shrink-0" color={model.enabled ? 'success' : undefined}>{model.enabled ? '启用' : '停用'}</Tag>
            </div>
            <Collapse ghost size="small" items={[{ key: 'candidates', label: '查看候选顺序', children: <ModelCandidates model={model} compact /> }]} />
            <div className="mt-2 flex items-center justify-end gap-1">
              <Switch size="small" checked={model.enabled} onChange={(value) => toggle.mutate({ id: model.id, enabled: value })} />
              <Button type="text" icon={<EditOutlined />} aria-label={`编辑 ${model.name}`} onClick={() => editModel(model)} />
              <Popconfirm title="删除模型路由？" description="已有历史请求时将转为归档。" okText="删除" cancelText="取消" okButtonProps={{ danger: true }} onConfirm={() => remove.mutate(model.id)}><Button type="text" danger icon={<DeleteOutlined />} aria-label={`删除 ${model.name}`} /></Popconfirm>
            </div>
          </Card>
        )) : <Card><Empty className="py-8" description="暂无模型路由" /></Card>}
      </div>
      <Modal wrapClassName="mc-responsive-modal" title={editing ? '编辑模型路由' : '新增虚拟模型'} width={820} open={open} onCancel={() => { setOpen(false); setEditing(null) }} onOk={() => form.submit()} confirmLoading={save.isPending} okText="保存路由">
        <Form form={form} layout="vertical" onFinish={(values) => save.mutate(values)} className="pt-4">
          <Form.Item name="name" label="虚拟模型名" rules={[{ required: true }]}><Input placeholder="例如：coding-primary" className="font-mono" /></Form.Item>
          <Form.List name="candidates">{(fields, { add, remove: removeField }) => <div className="space-y-4">{fields.map(({ key, ...field }, index) => <div key={key} className="rounded border border-black/10 p-3 dark:border-white/10 sm:p-4"><Form.Item {...field} name={[field.name, 'id']} hidden><Input /></Form.Item><div className="mb-4 flex justify-between"><span className="text-sm font-medium text-[#d7783d]">候选 {String(index + 1).padStart(2, '0')}</span><Button type="text" danger icon={<DeleteOutlined />} disabled={fields.length === 1} onClick={() => removeField(field.name)} /></div><div className="grid grid-cols-1 gap-x-4 sm:grid-cols-2"><Form.Item {...field} name={[field.name, 'provider_id']} label="供应商" rules={[{ required: true }]}><Select options={providers.data?.map((item) => ({ value: item.id, label: item.enabled ? item.name : `${item.name}（停用）` }))} onChange={(providerID) => { const available = configuredProtocols(providerID, providers.data); form.setFieldValue(['candidates', field.name, 'protocols'], available.length ? [available[0]] : []) }} /></Form.Item><Form.Item {...field} name={[field.name, 'upstream_model']} label="真实模型名" rules={[{ required: true }]}><Input className="font-mono" /></Form.Item><Form.Item {...field} name={[field.name, 'default_max_output_tokens']} label="默认最大输出" rules={[{ required: true }]}><InputNumber min={1} className="!w-full" /></Form.Item><Form.Item {...field} name={[field.name, 'max_output_tokens']} label="输出上限" rules={[{ required: true }]}><InputNumber min={1} className="!w-full" /></Form.Item></div><Form.Item noStyle shouldUpdate>{({ getFieldValue }) => <Form.Item name={[field.name, 'protocols']} label="协议入口及后备顺序" rules={[{ required: true, message: '请选择至少一个供应商已配置的协议' }]}><ProtocolOrderField available={configuredProtocols(getFieldValue(['candidates', field.name, 'provider_id']), providers.data)} /></Form.Item>}</Form.Item></div>)}<Button type="dashed" icon={<PlusOutlined />} onClick={() => add({ default_max_output_tokens: 16384, max_output_tokens: 65536, protocols: [] })} block>添加后备候选</Button></div>}</Form.List>
        </Form>
      </Modal>
    </div>
  )
}

function ModelCandidates({ model, compact = false }: { model: VirtualModel; compact?: boolean }) {
  return <div className={compact ? '' : 'p-4'}>{model.candidates.map((candidate) => <div key={candidate.id} className={`mb-3 grid items-start gap-2 border-b border-black/5 pb-3 dark:border-white/5 ${compact ? 'grid-cols-[32px_minmax(0,1fr)]' : 'grid-cols-[44px_1fr_1fr_2fr] gap-4'}`}><span className="font-mono text-[#d7783d]">{String(candidate.position + 1).padStart(2, '0')}</span>{compact ? <div className="min-w-0"><strong className="block break-all">{candidate.provider_name}</strong><code className="mt-1 block break-all text-xs">{candidate.upstream_model}</code><Space wrap size={[4, 4]} className="mt-2">{candidate.protocols.map((item) => <Tag key={item.protocol}>{protocolLabels[item.protocol]}</Tag>)}</Space></div> : <><strong>{candidate.provider_name}</strong><code className="break-all">{candidate.upstream_model}</code><Space wrap>{candidate.protocols.map((item) => <Tag key={item.protocol}>{protocolLabels[item.protocol]}</Tag>)}</Space></>}</div>)}</div>
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

function configuredProtocols(providerID: number | undefined, providers?: Provider[]): CandidateProtocol['protocol'][] {
  const endpoints = providers?.find((provider) => provider.id === providerID)?.endpoints ?? {}
  return (Object.keys(protocolLabels) as CandidateProtocol['protocol'][]).filter((protocol) => !!endpoints[protocol])
}

function defaultProtocol(protocol: CandidateProtocol['protocol'], position: number): CandidateProtocol {
  return { protocol, position, supports_stream: true, supports_tools: true, supports_parallel_tools: true, effort_levels: ['low', 'medium', 'high', 'xhigh'], supports_stream_usage: protocol === 'chat_completions' }
}

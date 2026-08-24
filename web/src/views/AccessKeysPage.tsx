import { useState } from 'react'
import dayjs, { type Dayjs } from 'dayjs'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { App, Button, DatePicker, Empty, Form, Input, Modal, Popconfirm, Select, Space, Switch, Table, Typography } from 'antd'
import { CopyOutlined, DeleteOutlined, EditOutlined, PlusOutlined } from '@ant-design/icons'
import { api, type AccessKey, type DeleteResult, type Page } from '../api'

type AccessKeyForm = { name: string; expires_at?: Dayjs | null }

export default function AccessKeysPage() {
  const { message } = App.useApp()
  const [open, setOpen] = useState(false)
  const [editing, setEditing] = useState<AccessKey | null>(null)
  const [created, setCreated] = useState<AccessKey | null>(null)
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(10)
  const [nameInput, setNameInput] = useState('')
  const [name, setName] = useState('')
  const [enabled, setEnabled] = useState('')
  const [form] = Form.useForm<AccessKeyForm>()
  const queryClient = useQueryClient()
  const keys = useQuery({
    queryKey: ['access-keys', page, pageSize, name, enabled],
    queryFn: () => {
      const params = new URLSearchParams({ page: String(page), page_size: String(pageSize) })
      if (name) params.set('name', name)
      if (enabled) params.set('enabled', enabled)
      return api<Page<AccessKey>>(`/api/admin/access-keys?${params}`)
    },
  })
  const save = useMutation({
    mutationFn: (values: AccessKeyForm) => editing
      ? api(`/api/admin/access-keys/${editing.id}`, { method: 'PUT', body: JSON.stringify({ name: values.name, expires_at: values.expires_at?.toISOString() ?? null, enabled: editing.enabled }) })
      : api<AccessKey>('/api/admin/access-keys', { method: 'POST', body: JSON.stringify({ name: values.name, expires_at: values.expires_at?.toISOString() ?? null }) }),
    onSuccess: (key) => {
      setOpen(false)
      if (!editing) setCreated(key as AccessKey)
      setEditing(null)
      form.resetFields()
      queryClient.invalidateQueries({ queryKey: ['access-keys'] })
      queryClient.invalidateQueries({ queryKey: ['overview'] })
    },
    onError: (error) => message.error(error.message),
  })
  const toggle = useMutation({
    mutationFn: ({ id, enabled }: { id: number; enabled: boolean }) => api(`/api/admin/access-keys/${id}`, { method: 'PATCH', body: JSON.stringify({ enabled }) }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['access-keys'] }),
    onError: (error) => message.error(error.message),
  })
  const remove = useMutation({
    mutationFn: (id: number) => api<DeleteResult>(`/api/admin/access-keys/${id}`, { method: 'DELETE' }),
    onSuccess: (result) => {
      message.success(result.archived ? '访问密钥已归档，历史记录保持不变' : '访问密钥已删除')
      queryClient.invalidateQueries({ queryKey: ['access-keys'] })
      queryClient.invalidateQueries({ queryKey: ['overview'] })
    },
    onError: (error) => message.error(error.message),
  })

  function createKey() {
    setEditing(null)
    form.resetFields()
    setOpen(true)
  }

  function editKey(key: AccessKey) {
    setEditing(key)
    form.setFieldsValue({ name: key.name, expires_at: key.expires_at ? dayjs(key.expires_at) : null })
    setOpen(true)
  }

  return (
    <div className="mc-enter mx-auto max-w-[1500px]">
      <div className="mb-6 flex items-center justify-between gap-4 sm:mb-8">
        <Typography.Title level={2} className="!mb-0 !tracking-[-.04em]">访问密钥</Typography.Title>
        <Button type="primary" icon={<PlusOutlined />} onClick={createKey}>创建密钥</Button>
      </div>
      <div className="mb-4 flex flex-col gap-2 sm:flex-row">
        <Input.Search allowClear enterButton="搜索" placeholder="搜索名称" className="min-w-0 sm:max-w-[360px]" value={nameInput} onChange={(event) => { const value = event.target.value; setNameInput(value); if (!value) { setName(''); setPage(1) } }} onSearch={(value) => { setName(value.trim()); setPage(1) }} />
        <Select allowClear placeholder="状态" className="w-full sm:w-[140px]" value={enabled || undefined} onChange={(value) => { setEnabled(value ?? ''); setPage(1) }} options={[{ value: 'true', label: '启用' }, { value: 'false', label: '停用' }]} />
      </div>
      <div>
        <Table
          rowKey="id"
          loading={keys.isPending}
          dataSource={keys.data?.items ?? []}
          scroll={{ x: 1100, y: 'calc(100vh - 330px)' }}
          pagination={{ current: page, pageSize, total: keys.data?.total ?? 0, showSizeChanger: true, pageSizeOptions: [10, 20, 50], onChange: (nextPage, nextPageSize) => { setPage(nextPageSize === pageSize ? nextPage : 1); setPageSize(nextPageSize) }, showTotal: (total) => `共 ${total} 条` }}
          columns={[
            { title: '名称', dataIndex: 'name', render: (value) => <strong>{value}</strong> },
            { title: '密钥', dataIndex: 'secret', render: (value) => <Space.Compact className="max-w-[460px]"><Input readOnly value={value} className="font-mono" /><Button aria-label="复制密钥" icon={<CopyOutlined />} onClick={() => { navigator.clipboard.writeText(value); message.success('密钥已复制') }} /></Space.Compact> },
            { title: '最后使用', dataIndex: 'last_used_at', render: (value) => value ? new Date(value).toLocaleString() : '从未使用' },
            { title: '过期时间', dataIndex: 'expires_at', render: (value) => value ? new Date(value).toLocaleString() : '永不过期' },
            { title: '启用', dataIndex: 'enabled', align: 'right', render: (enabled, record) => <Switch checked={enabled} loading={toggle.isPending} onChange={(value) => toggle.mutate({ id: record.id, enabled: value })} /> },
            { title: '操作', width: 108, align: 'right', render: (_, record) => <Space size={4}><Button type="text" icon={<EditOutlined />} onClick={() => editKey(record)} /><Popconfirm title="删除访问密钥？" description="已产生历史记录的密钥将转为归档。" okText="删除" cancelText="取消" okButtonProps={{ danger: true }} onConfirm={() => remove.mutate(record.id)}><Button type="text" danger icon={<DeleteOutlined />} /></Popconfirm></Space> },
          ]}
        />
      </div>
      <Modal wrapClassName="mc-responsive-modal" title={editing ? '编辑访问密钥' : '创建访问密钥'} open={open} onCancel={() => { setOpen(false); setEditing(null) }} onOk={() => form.submit()} confirmLoading={save.isPending} okText="保存">
        <Form form={form} layout="vertical" onFinish={(values) => save.mutate(values)} className="pt-4">
          <Form.Item name="name" label="名称" rules={[{ required: true, message: '请输入名称' }]}><Input placeholder="例如：codex-workstation" /></Form.Item>
          <Form.Item name="expires_at" label="过期时间（可选）"><DatePicker showTime className="w-full" placeholder="不填表示永不过期" /></Form.Item>
        </Form>
      </Modal>
      <Modal wrapClassName="mc-responsive-modal" title="密钥已创建" open={!!created} onCancel={() => setCreated(null)} footer={<Button type="primary" onClick={() => setCreated(null)}>关闭</Button>}>
        <Typography.Paragraph type="secondary">密钥已创建，之后仍可在访问密钥列表中查看和复制。</Typography.Paragraph>
        <Space.Compact className="w-full"><Input readOnly value={created?.secret} className="font-mono" /><Button icon={<CopyOutlined />} onClick={() => { navigator.clipboard.writeText(created?.secret ?? ''); message.success('已复制') }} /></Space.Compact>
      </Modal>
    </div>
  )
}

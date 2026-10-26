import { useEffect, useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Button, Form, Input, Modal, Space, Typography } from 'antd'
import { LoginOutlined } from '@ant-design/icons'
import { api, type Provider } from '../api'

type LoginStatus = {
  id: string
  provider_id: number
  url?: string
  redirect_uri: string
  expires_at: string
  status: 'pending' | 'exchanging' | 'succeeded' | 'failed' | 'cancelled' | 'expired'
  error?: string
  account_id?: number
}

const isActive = (status: LoginStatus['status']) => status === 'pending' || status === 'exchanging'
const loginPath = (providerID: number, loginID?: string) => `/api/admin/providers/${providerID}/logins${loginID ? `/${loginID}` : ''}`

export default function OAuthLoginButton({ provider }: { provider: Provider }) {
  const [open, setOpen] = useState(false)
  const [login, setLogin] = useState<LoginStatus | null>(null)
  const [callbackURL, setCallbackURL] = useState('')
  const active = useRef<LoginStatus | null>(null)
  const mounted = useRef(true)
  const client = useQueryClient()
  const cancelDetached = (value: LoginStatus) => {
    void api(loginPath(value.provider_id, value.id), { method: 'DELETE' }).catch((error: Error) => console.error('取消 OAuth 登录失败', error.message))
  }

  useEffect(() => {
    mounted.current = true
    return () => {
      mounted.current = false
      if (active.current) cancelDetached(active.current)
    }
  }, [])

  const start = useMutation({
    mutationFn: () => api<LoginStatus>(loginPath(provider.id), { method: 'POST' }),
    onSuccess: (result) => {
      if (!mounted.current) { cancelDetached(result); return }
      active.current = result
      setLogin(result)
    },
  })
  const status = useQuery({
    queryKey: ['provider-login', provider.id, login?.id],
    queryFn: () => api<LoginStatus>(loginPath(provider.id, login!.id)),
    initialData: login ?? undefined,
    enabled: !!login,
    retry: false,
    refetchInterval: (query) => query.state.error ? false : isActive(query.state.data?.status ?? 'pending') ? 1000 : false,
  })
  const current = status.data ?? login
  const submit = useMutation({
    mutationFn: ({ id, url }: { id: string; url: string }) => api<LoginStatus>(`${loginPath(provider.id, id)}/callback`, { method: 'POST', body: JSON.stringify({ url }) }),
    onSuccess: (result, variables) => {
      if (active.current?.id !== variables.id) return
      setCallbackURL('')
      client.setQueryData(['provider-login', provider.id, variables.id], result)
    },
  })
  const close = useMutation({
    mutationFn: async () => {
      if (active.current) await api(loginPath(active.current.provider_id, active.current.id), { method: 'DELETE' })
    },
    onSuccess: () => {
      active.current = null
      setOpen(false)
      setLogin(null)
      setCallbackURL('')
      start.reset()
      submit.reset()
      void client.invalidateQueries({ queryKey: ['providers'] })
      void client.invalidateQueries({ queryKey: ['health'] })
    },
  })

  useEffect(() => {
    if (!current || isActive(current.status)) return
    active.current = null
    setCallbackURL('')
    if (current.status === 'succeeded') {
      setOpen(false)
      setLogin(null)
      void client.invalidateQueries({ queryKey: ['providers'] })
      void client.invalidateQueries({ queryKey: ['health'] })
      void client.invalidateQueries({ queryKey: ['overview'] })
    }
  }, [current, client])

  function begin() {
    setOpen(true)
    setLogin(null)
    setCallbackURL('')
    close.reset()
    submit.reset()
    start.mutate()
  }

  const error = close.error?.message ?? submit.error?.message ?? status.error?.message ?? current?.error ?? start.error?.message
  return <>
    <Button icon={<LoginOutlined />} onClick={begin}>登录添加账号</Button>
    <Modal title={provider.kind === 'codex_oauth' ? '登录 Codex' : '登录 Antigravity'} open={open} width={520} maskClosable={false} closable={!start.isPending && !close.isPending} onCancel={() => close.mutate()} footer={<Button disabled={start.isPending} loading={close.isPending} onClick={() => close.mutate()}>{current && isActive(current.status) ? '取消登录' : '关闭'}</Button>}>
      <div className="space-y-4 py-3">
        {start.isPending && <Typography.Text type="secondary">正在创建登录…</Typography.Text>}
        {current && isActive(current.status) && <>
          <Space><Button type="primary" href={current.url ?? login?.url} target="_blank" rel="noopener noreferrer" disabled={current.status !== 'pending'}>打开授权页</Button><Typography.Text type="secondary" aria-live="polite">{current.status === 'exchanging' ? '正在保存账号…' : '等待授权'}</Typography.Text></Space>
          <div className="text-xs text-[#7c8d86]">{new Date(current.expires_at).toLocaleTimeString()} 前完成授权</div>
          {current.status === 'pending' && <div>
            <Typography.Paragraph type="secondary" className="!mb-3 text-xs">授权后若浏览器无法连接，将地址栏中的完整回调 URL 粘贴到这里。</Typography.Paragraph>
            <Form.Item label="完整回调 URL" htmlFor={`oauth-callback-${current.id}`} className="!mb-3">
              <Input.TextArea id={`oauth-callback-${current.id}`} value={callbackURL} onChange={(event) => { setCallbackURL(event.target.value); submit.reset() }} autoSize={{ minRows: 2, maxRows: 4 }} autoComplete="off" spellCheck={false} disabled={submit.isPending} placeholder={`${current.redirect_uri}?state=…&code=…`} />
            </Form.Item>
            <Button onClick={() => submit.mutate({ id: current.id, url: callbackURL.trim() })} loading={submit.isPending} disabled={!callbackURL.trim()}>提交回调</Button>
          </div>}
        </>}
        {error && <Typography.Text type="danger" className="block" role="alert">{error}</Typography.Text>}
        {status.isError && <Button size="small" onClick={() => status.refetch()}>重新查询</Button>}
      </div>
    </Modal>
  </>
}

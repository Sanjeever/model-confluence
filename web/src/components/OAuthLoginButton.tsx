import { useEffect, useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { LogIn, Loader2, ExternalLink } from 'lucide-react'
import { Button } from './ui/button'
import { Textarea } from './ui/textarea'
import { Label } from './ui/label'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from './ui/dialog'
import { ErrorNotice } from './console'
import { api, type Provider } from '../api'

type LoginStatus = {
  id: string
  provider_id: number
  url?: string
  redirect_uri: string
  expires_at: string
  status:
    'pending' | 'exchanging' | 'succeeded' | 'failed' | 'cancelled' | 'expired'
  error?: string
  account_id?: number
}

const isActive = (status: LoginStatus['status']) =>
  status === 'pending' || status === 'exchanging'
const loginPath = (providerID: number, loginID?: string) =>
  `/api/admin/providers/${providerID}/logins${loginID ? `/${loginID}` : ''}`

export default function OAuthLoginButton({ provider, keyID }: { provider: Provider; keyID?: number }) {
  const [open, setOpen] = useState(false)
  const [login, setLogin] = useState<LoginStatus | null>(null)
  const [callbackURL, setCallbackURL] = useState('')
  const active = useRef<LoginStatus | null>(null)
  const mounted = useRef(true)
  const client = useQueryClient()
  const cancelDetached = (value: LoginStatus) => {
    void api(loginPath(value.provider_id, value.id), {
      method: 'DELETE',
    }).catch((error: Error) =>
      console.error('取消 OAuth 登录失败', error.message),
    )
  }

  useEffect(() => {
    mounted.current = true
    return () => {
      mounted.current = false
      if (active.current) cancelDetached(active.current)
    }
  }, [])

  const start = useMutation({
    mutationFn: () =>
      api<LoginStatus>(loginPath(provider.id), { method: 'POST', ...(keyID ? { body: JSON.stringify({ key_id: keyID }) } : {}) }),
    onSuccess: (result) => {
      if (!mounted.current) {
        cancelDetached(result)
        return
      }
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
    refetchInterval: (query) =>
      query.state.error
        ? false
        : isActive(query.state.data?.status ?? 'pending')
          ? 1000
          : false,
  })
  const current = status.data ?? login
  const submit = useMutation({
    mutationFn: ({ id, url }: { id: string; url: string }) =>
      api<LoginStatus>(`${loginPath(provider.id, id)}/callback`, {
        method: 'POST',
        body: JSON.stringify({ url }),
      }),
    onSuccess: (result, variables) => {
      if (active.current?.id !== variables.id) return
      setCallbackURL('')
      client.setQueryData(['provider-login', provider.id, variables.id], result)
    },
  })
  const close = useMutation({
    mutationFn: async () => {
      if (active.current)
        await api(loginPath(active.current.provider_id, active.current.id), {
          method: 'DELETE',
        })
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

  const error =
    close.error?.message ??
    submit.error?.message ??
    status.error?.message ??
    current?.error ??
    start.error?.message
  return (
    <>
      <Button variant="outline" onClick={begin}>
        <LogIn />
        {keyID ? '重新登录' : '登录添加账号'}
      </Button>
      <Dialog
        open={open}
        onOpenChange={(next) => {
          if (!next && !start.isPending && !close.isPending) close.mutate()
        }}
      >
        <DialogContent
          className="sm:max-w-lg"
          showCloseButton={!start.isPending && !close.isPending}
          onInteractOutside={(event) => event.preventDefault()}
        >
          <DialogHeader>
            <DialogTitle>
              {provider.kind === 'codex_oauth'
                ? '登录 Codex'
                : '登录 Antigravity'}
            </DialogTitle>
            <DialogDescription className="sr-only">
              打开授权页并完成账号授权
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-4">
            {start.isPending && (
              <div className="flex items-center gap-2 text-sm text-muted-foreground">
                <Loader2 className="size-4 animate-spin" />
                正在创建登录…
              </div>
            )}
            {current && isActive(current.status) && (
              <>
                <div className="flex items-center gap-3">
                  {current.status === 'pending' ? (
                    <Button asChild>
                      <a
                        href={current.url ?? login?.url}
                        target="_blank"
                        rel="noopener noreferrer"
                      >
                        打开授权页
                        <ExternalLink />
                      </a>
                    </Button>
                  ) : (
                    <Loader2 className="size-4 animate-spin" />
                  )}
                  <span
                    className="text-xs text-muted-foreground"
                    aria-live="polite"
                  >
                    {current.status === 'exchanging'
                      ? '正在保存账号…'
                      : '等待授权'}
                  </span>
                </div>
                <div className="text-xs text-muted-foreground">
                  {new Date(current.expires_at).toLocaleTimeString()} 前完成授权
                </div>
                {current.status === 'pending' && (
                  <div>
                    <p className="mb-3 text-xs leading-5 text-muted-foreground">
                      授权后若浏览器无法连接，将地址栏中的完整回调 URL
                      粘贴到这里。
                    </p>
                    <div className="field mb-3">
                      <Label htmlFor={`oauth-callback-${current.id}`}>
                        完整回调 URL
                      </Label>
                      <Textarea
                        id={`oauth-callback-${current.id}`}
                        value={callbackURL}
                        onChange={(event) => {
                          setCallbackURL(event.target.value)
                          submit.reset()
                        }}
                        rows={3}
                        autoComplete="off"
                        spellCheck={false}
                        disabled={submit.isPending}
                        placeholder={`${current.redirect_uri}?state=…&code=…`}
                      />
                    </div>
                    <Button
                      variant="outline"
                      onClick={() =>
                        submit.mutate({
                          id: current.id,
                          url: callbackURL.trim(),
                        })
                      }
                      disabled={!callbackURL.trim() || submit.isPending}
                    >
                      {submit.isPending && <Loader2 className="animate-spin" />}
                      提交回调
                    </Button>
                  </div>
                )}
              </>
            )}
            <ErrorNotice error={error ? new Error(error) : null} />
            {status.isError && (
              <Button
                size="sm"
                variant="outline"
                onClick={() => status.refetch()}
              >
                重新查询
              </Button>
            )}
          </div>
          <DialogFooter>
            <Button
              variant="outline"
              disabled={start.isPending || close.isPending}
              onClick={() => close.mutate()}
            >
              {close.isPending && <Loader2 className="animate-spin" />}
              {current && isActive(current.status) ? '取消登录' : '关闭'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  )
}

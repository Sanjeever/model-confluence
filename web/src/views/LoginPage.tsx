import { useMutation } from '@tanstack/react-query'
import { useForm } from 'react-hook-form'
import { ArrowRight, Loader2 } from 'lucide-react'
import { api } from '../api'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { ErrorNotice } from '@/components/console'

export default function LoginPage({
  onAuthenticated,
}: {
  onAuthenticated: () => Promise<void>
}) {
  const form = useForm<{ password: string }>()
  const login = useMutation({
    mutationFn: async (values: { password: string }) => {
      await api('/api/admin/login', {
        method: 'POST',
        body: JSON.stringify(values),
      })
      await onAuthenticated()
    },
  })
  return (
    <main className="flex min-h-dvh items-center justify-center px-6 py-12">
      <div className="w-full max-w-[340px]">
        <div className="mb-8 flex items-center gap-2.5">
          <img src="/model-confluence.svg" alt="" className="size-8" />
          <span className="text-xl font-semibold tracking-tight">模汇</span>
        </div>
        <h1 className="mb-6 text-base font-medium">管理员登录</h1>
        <form
          onSubmit={form.handleSubmit((values) => login.mutate(values))}
          className="space-y-4"
        >
          <div className="field">
            <Label htmlFor="admin-password">管理员密码</Label>
            <Input
              id="admin-password"
              type="password"
              autoComplete="current-password"
              autoFocus
              disabled={login.isPending}
              {...form.register('password', { required: '请输入管理员密码' })}
              aria-invalid={!!form.formState.errors.password}
              className="h-9"
            />
            <p className="field-error">
              {form.formState.errors.password?.message}
            </p>
          </div>
          <ErrorNotice error={login.error} />
          <Button
            type="submit"
            className="h-9 w-full justify-between"
            disabled={login.isPending}
          >
            登录
            {login.isPending ? (
              <Loader2 className="size-4 animate-spin" />
            ) : (
              <ArrowRight className="size-4" />
            )}
          </Button>
        </form>
      </div>
    </main>
  )
}

import { useState } from 'react'
import { useMutation } from '@tanstack/react-query'
import { useForm } from 'react-hook-form'
import { NavLink, Outlet } from 'react-router-dom'
import {
  Activity,
  ArrowRightLeft,
  ChevronDown,
  Database,
  HeartPulse,
  KeyRound,
  LockKeyhole,
  LogOut,
  Menu,
  Moon,
  Monitor,
  Server,
  Settings2,
  Sun,
  Loader2,
} from 'lucide-react'
import { api } from '../api'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
  SheetDescription,
} from '@/components/ui/sheet'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
  DialogDescription,
} from '@/components/ui/dialog'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuLabel,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { ErrorNotice } from '@/components/console'
import { useTheme, type ThemeMode } from '@/hooks/use-theme'
import { cn } from '@/lib/utils'

type PasswordForm = {
  current_password: string
  new_password: string
  confirm_password: string
}
const navigation = [
  [
    { path: '/performance', label: '性能监控', icon: Activity },
    { path: '/requests', label: '使用记录', icon: Database },
    { path: '/usage', label: '用量统计', icon: ArrowRightLeft },
    { path: '/health', label: '上游健康', icon: HeartPulse },
  ],
  [
    { path: '/providers', label: '供应商', icon: Server },
    { path: '/models', label: '模型路由', icon: Settings2 },
    { path: '/access-keys', label: '访问密钥', icon: KeyRound },
  ],
]

export default function ConsoleLayout({
  onSignedOut,
}: {
  onSignedOut: () => void
}) {
  const [mobileOpen, setMobileOpen] = useState(false)
  const [passwordOpen, setPasswordOpen] = useState(false)
  const { mode, setMode } = useTheme()
  const form = useForm<PasswordForm>()
  const logout = useMutation({
    mutationFn: () => api('/api/admin/logout', { method: 'POST' }),
    onSuccess: onSignedOut,
  })
  const changePassword = useMutation({
    mutationFn: (values: PasswordForm) =>
      api('/api/admin/change-password', {
        method: 'POST',
        body: JSON.stringify({
          current_password: values.current_password,
          new_password: values.new_password,
        }),
      }),
    onSuccess: () => {
      form.reset()
      setPasswordOpen(false)
      onSignedOut()
    },
  })
  const links = (
    <nav aria-label="主导航" className="flex-1 space-y-6 px-3 py-5">
      {navigation.map((group, index) => (
        <div key={index} className="space-y-1">
          {group.map(({ path, label, icon: Icon }) => (
            <NavLink
              key={path}
              to={path}
              onClick={() => setMobileOpen(false)}
              className={({ isActive }) =>
                cn(
                  'flex h-9 items-center gap-2.5 rounded-md px-3 text-[13px] transition-colors hover:bg-sidebar-accent hover:text-sidebar-accent-foreground',
                  isActive
                    ? 'bg-sidebar-accent font-medium text-sidebar-accent-foreground'
                    : 'text-sidebar-foreground',
                )
              }
            >
              <Icon className="size-4" strokeWidth={1.7} />
              {label}
            </NavLink>
          ))}
        </div>
      ))}
    </nav>
  )
  const account = (
    <div className="border-t border-sidebar-border p-3">
      <ErrorNotice error={logout.error} />
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button
            variant="ghost"
            className="w-full justify-between px-3 font-normal"
          >
            <span className="flex items-center gap-2.5">
              <span className="flex size-6 items-center justify-center rounded-full border bg-surface text-[11px] font-medium">
                管
              </span>
              管理员
            </span>
            <ChevronDown className="size-3.5 text-muted-foreground" />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="start" side="top" className="w-48">
          <DropdownMenuLabel className="text-xs font-normal text-muted-foreground">
            外观
          </DropdownMenuLabel>
          <DropdownMenuRadioGroup
            value={mode}
            onValueChange={(value) => setMode(value as ThemeMode)}
          >
            <DropdownMenuRadioItem value="system">
              <Monitor className="size-4" />
              跟随系统
            </DropdownMenuRadioItem>
            <DropdownMenuRadioItem value="light">
              <Sun className="size-4" />
              浅色
            </DropdownMenuRadioItem>
            <DropdownMenuRadioItem value="dark">
              <Moon className="size-4" />
              深色
            </DropdownMenuRadioItem>
          </DropdownMenuRadioGroup>
          <DropdownMenuSeparator />
          <DropdownMenuItem
            onSelect={() => {
              setMobileOpen(false)
              setPasswordOpen(true)
              changePassword.reset()
            }}
          >
            <LockKeyhole />
            修改密码
          </DropdownMenuItem>
          <DropdownMenuItem
            disabled={logout.isPending}
            onSelect={() => logout.mutate()}
          >
            <LogOut />
            退出登录
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  )
  return (
    <div className="min-h-dvh lg:flex lg:h-dvh lg:overflow-hidden">
      <aside className="hidden w-52 shrink-0 flex-col border-r border-sidebar-border bg-sidebar lg:flex">
        <div className="flex h-16 shrink-0 items-center gap-2.5 px-6">
          <img src="/model-confluence.svg" alt="" className="size-7" />
          <span className="text-base font-semibold tracking-tight">模汇</span>
        </div>
        {links}
        {account}
      </aside>
      <div className="min-w-0 flex-1 lg:flex lg:flex-col">
        <header className="sticky top-0 z-30 flex h-14 items-center justify-between border-b bg-background px-4 lg:hidden">
          <div className="flex items-center gap-2">
            <img src="/model-confluence.svg" alt="" className="size-6" />
            <span className="font-semibold">模汇</span>
          </div>
          <Button
            variant="ghost"
            size="icon"
            aria-label="打开导航"
            onClick={() => setMobileOpen(true)}
          >
            <Menu />
          </Button>
        </header>
        <main
          id="main-content"
          className="min-w-0 flex-1 p-4 sm:p-6 lg:overflow-y-auto"
        >
          <Outlet />
        </main>
      </div>
      <Sheet open={mobileOpen} onOpenChange={setMobileOpen}>
        <SheetContent
          side="left"
          className="gap-0 bg-sidebar p-0 data-[side=left]:w-64 data-[side=left]:sm:max-w-64"
        >
          <SheetHeader className="h-16 justify-center border-b px-6">
            <SheetTitle>模汇</SheetTitle>
            <SheetDescription className="sr-only">
              主导航与管理员设置
            </SheetDescription>
          </SheetHeader>
          {links}
          {account}
        </SheetContent>
      </Sheet>
      <Dialog
        open={passwordOpen}
        onOpenChange={(open) => {
          if (!changePassword.isPending) {
            setPasswordOpen(open)
            form.reset()
          }
        }}
      >
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>修改管理员密码</DialogTitle>
            <DialogDescription className="sr-only">
              修改后需要重新登录
            </DialogDescription>
          </DialogHeader>
          <form
            id="password-form"
            onSubmit={form.handleSubmit((values) =>
              changePassword.mutate(values),
            )}
            className="space-y-4"
          >
            <div className="field">
              <Label htmlFor="current-password">当前密码</Label>
              <Input
                id="current-password"
                type="password"
                autoComplete="current-password"
                {...form.register('current_password', {
                  required: '请输入当前密码',
                })}
                aria-invalid={!!form.formState.errors.current_password}
              />
              <p className="field-error">
                {form.formState.errors.current_password?.message}
              </p>
            </div>
            <div className="field">
              <Label htmlFor="new-password">新密码</Label>
              <Input
                id="new-password"
                type="password"
                autoComplete="new-password"
                {...form.register('new_password', {
                  required: '请输入新密码',
                  minLength: { value: 12, message: '新密码至少需要 12 个字符' },
                })}
                aria-invalid={!!form.formState.errors.new_password}
              />
              <p className="field-error">
                {form.formState.errors.new_password?.message}
              </p>
            </div>
            <div className="field">
              <Label htmlFor="confirm-password">确认新密码</Label>
              <Input
                id="confirm-password"
                type="password"
                autoComplete="new-password"
                {...form.register('confirm_password', {
                  validate: (value) =>
                    value === form.getValues('new_password') ||
                    '两次输入的新密码不一致',
                })}
                aria-invalid={!!form.formState.errors.confirm_password}
              />
              <p className="field-error">
                {form.formState.errors.confirm_password?.message}
              </p>
            </div>
            <ErrorNotice error={changePassword.error} />
          </form>
          <DialogFooter>
            <Button
              variant="outline"
              disabled={changePassword.isPending}
              onClick={() => setPasswordOpen(false)}
            >
              取消
            </Button>
            <Button
              type="submit"
              form="password-form"
              disabled={changePassword.isPending}
            >
              {changePassword.isPending && <Loader2 className="animate-spin" />}
              修改密码
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}

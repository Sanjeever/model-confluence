import { useState, type ReactNode } from 'react'
import {
  AlertCircle,
  Check,
  CheckCheck,
  ChevronDown,
  Copy,
  Loader2,
  Search,
} from 'lucide-react'
import dayjs from 'dayjs'
import { zhCN } from 'date-fns/locale'
import type { DateRange as CalendarRange } from 'react-day-picker'
import { Button } from './ui/button'
import { Input } from './ui/input'
import { Calendar } from './ui/calendar'
import { Popover, PopoverContent, PopoverTrigger } from './ui/popover'
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from './ui/command'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from './ui/alert-dialog'
import { cn } from '@/lib/utils'

export function PageHeader({
  title,
  children,
  back,
}: {
  title: string
  children?: ReactNode
  back?: ReactNode
}) {
  return (
    <header className="page-header">
      <div className="flex min-w-0 items-center gap-3">
        {back}
        <h1 className="truncate text-xl font-semibold tracking-tight">
          {title}
        </h1>
      </div>
      <div className="flex shrink-0 items-center gap-2">{children}</div>
    </header>
  )
}

export function ErrorNotice({
  error,
  retry,
}: {
  error?: Error | null
  retry?: () => void
}) {
  if (!error) return null
  return (
    <div
      role="alert"
      className="my-3 flex items-start gap-2 rounded-md border border-destructive/25 bg-destructive/5 px-3 py-2.5 text-sm text-destructive"
    >
      <AlertCircle className="mt-0.5 size-4 shrink-0" />
      <span className="min-w-0 flex-1 break-words">{error.message}</span>
      {retry && (
        <button
          type="button"
          className="shrink-0 underline underline-offset-4"
          onClick={retry}
        >
          重试
        </button>
      )}
    </div>
  )
}

export function EmptyState({
  children,
  action,
}: {
  children: ReactNode
  action?: ReactNode
}) {
  return (
    <div className="flex min-h-36 flex-col items-center justify-center gap-3 px-4 py-8 text-sm text-muted-foreground">
      <span>{children}</span>
      {action}
    </div>
  )
}

export function Loading({ label = '正在加载' }: { label?: string }) {
  return (
    <div
      role="status"
      className="flex min-h-40 items-center justify-center gap-2 text-sm text-muted-foreground"
    >
      <Loader2 className="size-4 animate-spin" />
      {label}
    </div>
  )
}

export function SearchField({
  value,
  onChange,
  placeholder,
  className,
}: {
  value: string
  onChange: (value: string) => void
  placeholder: string
  className?: string
}) {
  const [input, setInput] = useState(value)
  return (
    <form
      className={cn('relative w-full sm:w-64', className)}
      onSubmit={(event) => {
        event.preventDefault()
        onChange(input.trim())
      }}
      role="search"
    >
      <Search className="pointer-events-none absolute left-2.5 top-2 size-4 text-muted-foreground" />
      <Input
        aria-label={placeholder}
        placeholder={placeholder}
        className="pl-8 pr-12"
        value={input}
        onChange={(event) => {
          setInput(event.target.value)
          if (!event.target.value) onChange('')
        }}
      />
      <button
        type="submit"
        className="absolute right-2 top-1.5 rounded px-1 text-xs leading-5 text-muted-foreground hover:text-foreground"
      >
        搜索
      </button>
    </form>
  )
}

export function FilterSelect({
  value,
  onChange,
  options,
  placeholder,
  searchable = false,
  className,
}: {
  value: string
  onChange: (value: string) => void
  options: Array<{ value: string; label: string }>
  placeholder: string
  searchable?: boolean
  className?: string
}) {
  const [open, setOpen] = useState(false)
  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button
          variant="outline"
          role="combobox"
          aria-label={placeholder}
          aria-expanded={open}
          className={cn('justify-between font-normal sm:min-w-32', className)}
        >
          <span className="truncate">
            {options.find((option) => option.value === value)?.label ??
              placeholder}
          </span>
          <ChevronDown className="size-3.5 text-muted-foreground" />
        </Button>
      </PopoverTrigger>
      <PopoverContent align="start" className="w-64 p-0">
        <Command>
          {searchable && <CommandInput placeholder="搜索…" />}
          <CommandList>
            <CommandEmpty>没有匹配项</CommandEmpty>
            <CommandGroup>
              <CommandItem
                value={`全部 ${placeholder}`}
                onSelect={() => {
                  onChange('')
                  setOpen(false)
                }}
              >
                <Check className={cn('size-4', value ? 'opacity-0' : '')} />
                全部{placeholder}
              </CommandItem>
              {options.map((option) => (
                <CommandItem
                  key={option.value}
                  value={`${option.label} ${option.value}`}
                  onSelect={() => {
                    onChange(option.value)
                    setOpen(false)
                  }}
                >
                  <Check
                    className={cn(
                      'size-4',
                      value === option.value ? '' : 'opacity-0',
                    )}
                  />
                  <span className="truncate">{option.label}</span>
                </CommandItem>
              ))}
            </CommandGroup>
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  )
}

export type DateRangeValue = [string, string]
export function todayRange(days = 1): DateRangeValue {
  return [
    dayjs()
      .subtract(days - 1, 'day')
      .format('YYYY-MM-DD'),
    dayjs().format('YYYY-MM-DD'),
  ]
}
export function rangeBounds(value: DateRangeValue) {
  return [
    dayjs(value[0]).startOf('day').toISOString(),
    dayjs(value[1]).startOf('day').add(1, 'day').toISOString(),
  ] as const
}
export function includesToday(value: DateRangeValue) {
  const today = dayjs().format('YYYY-MM-DD')
  return value[0] <= today && value[1] >= today
}
export function DateRangePicker({
  value,
  onChange,
}: {
  value: DateRangeValue
  onChange: (value: DateRangeValue) => void
}) {
  const [open, setOpen] = useState(false)
  const [draft, setDraft] = useState<CalendarRange | undefined>()
  function apply(next: DateRangeValue) {
    onChange(next)
    setOpen(false)
  }
  return (
    <Popover
      open={open}
      onOpenChange={(next) => {
        if (next)
          setDraft({
            from: new Date(`${value[0]}T00:00:00`),
            to: new Date(`${value[1]}T00:00:00`),
          })
        setOpen(next)
      }}
    >
      <PopoverTrigger asChild>
        <Button
          variant="outline"
          className="justify-between font-normal tabular-nums"
          aria-label="日期范围"
        >
          {value[0] === value[1] ? value[0] : `${value[0]} — ${value[1]}`}
          <ChevronDown className="size-3.5 text-muted-foreground" />
        </Button>
      </PopoverTrigger>
      <PopoverContent align="start" className="w-auto p-0">
        <div className="flex gap-1 border-b p-2">
          {[1, 7, 30].map((days) => (
            <Button
              key={days}
              size="sm"
              variant="ghost"
              onClick={() => apply(todayRange(days))}
            >
              {days === 1 ? '今天' : `近 ${days} 天`}
            </Button>
          ))}
        </div>
        <Calendar
          mode="range"
          selected={draft}
          onSelect={setDraft}
          locale={zhCN}
          numberOfMonths={1}
          defaultMonth={draft?.from}
        />
        <div className="flex justify-end border-t p-2">
          <Button
            size="sm"
            disabled={!draft?.from || !draft.to}
            onClick={() =>
              draft?.from &&
              draft.to &&
              apply([
                dayjs(draft.from).format('YYYY-MM-DD'),
                dayjs(draft.to).format('YYYY-MM-DD'),
              ])
            }
          >
            应用
          </Button>
        </div>
      </PopoverContent>
    </Popover>
  )
}

export function CopyButton({
  value,
  label = '复制',
  className,
}: {
  value: string
  label?: string
  className?: string
}) {
  const [copied, setCopied] = useState(false)
  const [error, setError] = useState(false)
  async function copy() {
    try {
      await navigator.clipboard.writeText(value)
      setCopied(true)
      setError(false)
    } catch {
      setError(true)
    }
  }
  return (
    <Button
      type="button"
      size="icon-sm"
      variant="ghost"
      aria-label={error ? '复制失败，点击重试' : copied ? '已复制' : label}
      title={error ? '复制失败，点击重试' : copied ? '已复制' : label}
      className={cn(error && 'text-destructive', className)}
      disabled={!value}
      onClick={copy}
      onBlur={() => setCopied(false)}
    >
      {copied ? <CheckCheck /> : <Copy />}
    </Button>
  )
}

export function ConfirmButton({
  title,
  description,
  onConfirm,
  pending,
  children,
  destructive = true,
}: {
  title: string
  description?: string
  onConfirm: () => void
  pending?: boolean
  children: ReactNode
  destructive?: boolean
}) {
  return (
    <AlertDialog>
      <AlertDialogTrigger asChild>{children}</AlertDialogTrigger>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{title}</AlertDialogTitle>
          <AlertDialogDescription>
            {description ?? '此操作无法撤销。'}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>取消</AlertDialogCancel>
          <AlertDialogAction
            className={cn(
              destructive &&
                'bg-destructive text-white hover:bg-destructive/90',
            )}
            disabled={pending}
            onClick={onConfirm}
          >
            {pending && <Loader2 className="animate-spin" />}确认
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}

export function MetaGrid({
  items,
}: {
  items: Array<{ label: string; value: ReactNode }>
}) {
  return (
    <dl className="grid grid-cols-1 gap-x-6 gap-y-4 text-sm sm:grid-cols-2 xl:grid-cols-3">
      {items.map((item) => (
        <div key={item.label} className="min-w-0">
          <dt className="mb-1 text-xs text-muted-foreground">{item.label}</dt>
          <dd className="break-words">{item.value ?? '—'}</dd>
        </div>
      ))}
    </dl>
  )
}

export function localDateTime(value?: string | null) {
  return value ? dayjs(value).format('YYYY-MM-DDTHH:mm:ss') : ''
}
export function displayDate(value?: string | null) {
  return value ? dayjs(value).format('YYYY-MM-DD HH:mm:ss') : '—'
}

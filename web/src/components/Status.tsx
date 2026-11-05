import { cn } from '@/lib/utils'

const labels: Record<string, string> = {
  completed: '已完成',
  in_progress: '进行中',
  failed: '失败',
  cancelled: '已取消',
  available: '可用',
  auth_invalid: '鉴权失效',
  quota_exhausted: '额度耗尽',
  rate_limited: '限流冷却',
  config_error: '配置错误',
  disabled: '已停用',
}
const tones: Record<string, string> = {
  completed: 'text-success',
  available: 'text-success',
  in_progress: 'text-info',
  failed: 'text-destructive',
  auth_invalid: 'text-destructive',
  quota_exhausted: 'text-destructive',
  config_error: 'text-destructive',
  rate_limited: 'text-warning',
}
export function Status({ value, label }: { value: string; label?: string }) {
  return (
    <span
      className={cn(
        'inline-flex items-center gap-1.5 whitespace-nowrap text-xs',
        tones[value] ?? 'text-muted-foreground',
      )}
    >
      <span
        aria-hidden="true"
        className="size-1.5 shrink-0 rounded-full bg-current"
      />
      {label ?? labels[value] ?? value}
    </span>
  )
}
export function statusName(value: string) {
  return labels[value] ?? value
}
export function protocolName(value: string) {
  return (
    (
      {
        chat_completions: 'Chat Completions',
        responses: 'Responses',
        messages: 'Messages',
        antigravity: 'Antigravity',
      } as Record<string, string>
    )[value] ??
    (value || '未调用')
  )
}
export function ProtocolFlow({
  inbound,
  upstream,
}: {
  inbound: string
  upstream: string
}) {
  return (
    <span className="whitespace-nowrap text-xs">
      <span>{protocolName(inbound)}</span>
      <span className="mx-1.5 text-muted-foreground">→</span>
      <span className="text-muted-foreground">{protocolName(upstream)}</span>
    </span>
  )
}

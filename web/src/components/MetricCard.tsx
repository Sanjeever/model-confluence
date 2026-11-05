import { Skeleton } from './ui/skeleton'
export default function MetricCard({
  label,
  value,
  loading,
}: {
  label: string
  value: string
  loading: boolean
}) {
  return (
    <div className="min-w-0 px-5 py-4">
      <div className="mb-2 text-xs text-muted-foreground">{label}</div>
      {loading ? (
        <Skeleton className="h-7 w-24" />
      ) : (
        <div className="truncate text-2xl leading-8 font-medium tracking-tight tabular-nums">
          {value}
        </div>
      )}
    </div>
  )
}

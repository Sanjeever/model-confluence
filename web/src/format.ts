export function formatCount(value: number | null | undefined): string {
  return value == null ? '—' : value.toLocaleString()
}

export function formatPercent(value: number | null | undefined): string {
  return value == null ? '—' : `${(value * 100).toFixed(1)}%`
}

export function formatDuration(value: number | null | undefined): string {
  if (value == null) return '—'
  if (value < 1000) return `${value} ms`
  const seconds = value / 1000
  return `${seconds.toFixed(seconds >= 10 ? 1 : 2)} s`
}

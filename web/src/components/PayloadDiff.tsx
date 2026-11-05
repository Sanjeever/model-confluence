import { useMemo } from 'react'

type Difference = { path: string; before: string; after: string }
const show = (value: unknown) => value === undefined ? '（字段不存在）' : JSON.stringify(value)

export default function PayloadDiff({ before, after }: { before: string; after: string }) {
  const result = useMemo(() => {
    try {
      const differences: Difference[] = []
      function visit(a: unknown, b: unknown, path: string) {
        if (Object.is(a, b)) return
        if (a !== null && b !== null && typeof a === 'object' && typeof b === 'object' && Array.isArray(a) === Array.isArray(b)) {
          const left = a as Record<string, unknown>, right = b as Record<string, unknown>
          for (const key of new Set([...Object.keys(left), ...Object.keys(right)])) visit(left[key], right[key], Array.isArray(a) ? `${path}[${key}]` : `${path}[${JSON.stringify(key)}]`)
        } else differences.push({ path, before: show(a), after: show(b) })
      }
      visit(JSON.parse(before), JSON.parse(after), '$')
      return { differences }
    } catch { return { error: '载荷为空、已清理或不是 JSON，无法比较字段差异。' } }
  }, [before, after])
  if (result.error) return <p className="text-xs text-muted-foreground">{result.error}</p>
  return <div className="space-y-3">{result.differences?.length ? result.differences.map((d) => <div key={d.path} className="overflow-hidden rounded-md border text-xs"><code className="block break-all border-b bg-muted/30 px-3 py-2">{d.path}</code><div className="grid divide-y sm:grid-cols-2 sm:divide-x sm:divide-y-0"><div className="min-w-0 p-3"><span className="text-muted-foreground">客户端请求</span><pre className="mt-2 max-h-48 overflow-auto whitespace-pre-wrap break-all font-mono">{d.before}</pre></div><div className="min-w-0 p-3"><span className="text-muted-foreground">上游请求</span><pre className="mt-2 max-h-48 overflow-auto whitespace-pre-wrap break-all font-mono">{d.after}</pre></div></div></div>) : <p className="text-xs text-muted-foreground">JSON 字段一致</p>}</div>
}

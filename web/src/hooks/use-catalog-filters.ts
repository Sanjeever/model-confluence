import { useSearchParams } from 'react-router-dom'

export function useCatalogFilters() {
  const [params, setParams] = useSearchParams()
  const positive = (value: string | null, fallback: number) => {
    const number = Number(value)
    return Number.isInteger(number) && number > 0 ? number : fallback
  }
  const page = positive(params.get('page'), 1)
  const pageSize = positive(params.get('page_size'), 10)
  const update = (changes: Record<string, string>, resetPage = true) =>
    setParams(
      (previous) => {
        const next = new URLSearchParams(previous)
        if (resetPage) next.set('page', '1')
        for (const [key, value] of Object.entries(changes)) {
          if (value) next.set(key, value)
          else next.delete(key)
        }
        return next
      },
      { replace: true },
    )
  return {
    name: params.get('name') ?? '',
    enabled: params.get('enabled') ?? '',
    authType: params.get('auth_type') ?? '',
    page,
    pageSize,
    update,
    pagination: { pageIndex: page - 1, pageSize },
    onPaginationChange: (next: { pageIndex: number; pageSize: number }) =>
      update(
        { page: String(next.pageIndex + 1), page_size: String(next.pageSize) },
        false,
      ),
  }
}

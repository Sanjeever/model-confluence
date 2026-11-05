import { Fragment, useEffect, useState, type ReactNode } from 'react'
import {
  flexRender,
  getCoreRowModel,
  useReactTable,
  type ColumnDef,
  type PaginationState,
} from '@tanstack/react-table'
import { ChevronDown, ChevronLeft, ChevronRight, Loader2 } from 'lucide-react'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from './ui/table'
import { Button } from './ui/button'
import { NativeSelect, NativeSelectOption } from './ui/native-select'
import { EmptyState } from './console'
import { cn } from '@/lib/utils'

export default function DataTable<T>({
  data,
  columns,
  loading,
  fetching,
  empty = '暂无数据',
  pagination,
  onPaginationChange,
  total,
  rowID,
  expanded,
  minWidth = 720,
}: {
  data: T[]
  columns: ColumnDef<T>[]
  loading?: boolean
  fetching?: boolean
  empty?: ReactNode
  pagination?: PaginationState
  onPaginationChange?: (next: PaginationState) => void
  total?: number
  rowID: (row: T) => string
  expanded?: (row: T) => ReactNode
  minWidth?: number
}) {
  const [expandedID, setExpandedID] = useState<string | null>(null)
  const [localPage, setLocalPage] = useState<PaginationState>({
    pageIndex: 0,
    pageSize: 10,
  })
  const paged = pagination ?? localPage
  const paginate = !!pagination || (total == null && data.length > 10)
  const count = total ?? data.length
  const pageCount = Math.max(1, Math.ceil(count / paged.pageSize))
  useEffect(() => {
    if (!pagination)
      setLocalPage((previous) => ({
        ...previous,
        pageIndex: Math.min(previous.pageIndex, pageCount - 1),
      }))
  }, [pageCount, !!pagination])
  const visible =
    !pagination && paginate
      ? data.slice(
          paged.pageIndex * paged.pageSize,
          (paged.pageIndex + 1) * paged.pageSize,
        )
      : data
  const table = useReactTable({
    data: visible,
    columns,
    manualPagination: true,
    getCoreRowModel: getCoreRowModel(),
    getRowId: rowID,
  })
  const setPage = onPaginationChange ?? setLocalPage
  return (
    <div
      className="relative overflow-hidden rounded-lg border bg-surface"
      aria-busy={loading || fetching}
    >
      {fetching && !loading && (
        <span
          role="status"
          className="absolute right-2 top-3 z-10 text-muted-foreground"
        >
          <Loader2 className="size-3 animate-spin" />
          <span className="sr-only">正在刷新</span>
        </span>
      )}
      <Table style={{ minWidth }}>
        <TableHeader>
          {table.getHeaderGroups().map((group) => (
            <TableRow key={group.id}>
              {expanded && <TableHead className="w-10" aria-label="展开详情" />}
              {group.headers.map((header) => (
                <TableHead
                  key={header.id}
                  style={{ width: header.column.columnDef.size }}
                  className={cn(
                    header.column.columnDef.meta &&
                      (header.column.columnDef.meta as { align?: string })
                        .align === 'right' &&
                      'text-right',
                  )}
                >
                  {header.isPlaceholder
                    ? null
                    : flexRender(
                        header.column.columnDef.header,
                        header.getContext(),
                      )}
                </TableHead>
              ))}
            </TableRow>
          ))}
        </TableHeader>
        <TableBody>
          {loading ? (
            <TableRow>
              <TableCell
                colSpan={columns.length + (expanded ? 1 : 0)}
                className="h-40 text-center text-muted-foreground"
              >
                <Loader2 className="mx-auto size-4 animate-spin" />
                <span className="sr-only">正在加载</span>
              </TableCell>
            </TableRow>
          ) : table.getRowModel().rows.length ? (
            table.getRowModel().rows.map((row) => (
              <Fragment key={row.id}>
                <TableRow>
                  {expanded && (
                    <TableCell>
                      <Button
                        size="icon-sm"
                        variant="ghost"
                        aria-label={
                          expandedID === row.id ? '收起详情' : '展开详情'
                        }
                        aria-expanded={expandedID === row.id}
                        onClick={() =>
                          setExpandedID(expandedID === row.id ? null : row.id)
                        }
                      >
                        <ChevronDown
                          className={cn(
                            'transition-transform',
                            expandedID === row.id && 'rotate-180',
                          )}
                        />
                      </Button>
                    </TableCell>
                  )}
                  {row.getVisibleCells().map((cell) => (
                    <TableCell
                      key={cell.id}
                      className={cn(
                        (
                          cell.column.columnDef.meta as
                            { align?: string } | undefined
                        )?.align === 'right' && 'text-right tabular-nums',
                      )}
                    >
                      {flexRender(
                        cell.column.columnDef.cell,
                        cell.getContext(),
                      )}
                    </TableCell>
                  ))}
                </TableRow>
                {expanded && expandedID === row.id && (
                  <TableRow className="hover:bg-transparent">
                    <TableCell
                      colSpan={columns.length + 1}
                      className="bg-muted/30 p-4 whitespace-normal"
                    >
                      {expanded(row.original)}
                    </TableCell>
                  </TableRow>
                )}
              </Fragment>
            ))
          ) : (
            <TableRow>
              <TableCell colSpan={columns.length + (expanded ? 1 : 0)}>
                <EmptyState>{empty}</EmptyState>
              </TableCell>
            </TableRow>
          )}
        </TableBody>
      </Table>
      {paginate && (
        <footer className="flex flex-wrap items-center justify-between gap-3 border-t px-4 py-2.5 text-xs text-muted-foreground">
          <span>共 {count.toLocaleString()} 条</span>
          <div className="flex items-center gap-3">
            <NativeSelect
              aria-label="每页条数"
              value={paged.pageSize}
              className="h-7 text-xs"
              onChange={(event) =>
                setPage({ pageIndex: 0, pageSize: Number(event.target.value) })
              }
            >
              {[10, 20, 50].map((size) => (
                <NativeSelectOption key={size} value={size}>
                  {size} 条 / 页
                </NativeSelectOption>
              ))}
            </NativeSelect>
            <span className="tabular-nums">
              {paged.pageIndex + 1} / {pageCount}
            </span>
            <div className="flex gap-1">
              <Button
                variant="ghost"
                size="icon-sm"
                aria-label="上一页"
                disabled={!paged.pageIndex || loading}
                onClick={() =>
                  setPage({ ...paged, pageIndex: paged.pageIndex - 1 })
                }
              >
                <ChevronLeft />
              </Button>
              <Button
                variant="ghost"
                size="icon-sm"
                aria-label="下一页"
                disabled={paged.pageIndex + 1 >= pageCount || loading}
                onClick={() =>
                  setPage({ ...paged, pageIndex: paged.pageIndex + 1 })
                }
              >
                <ChevronRight />
              </Button>
            </div>
          </div>
        </footer>
      )}
    </div>
  )
}

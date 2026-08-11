import { useMemo, useState } from 'react'
import { App, Button, Segmented, Tooltip } from 'antd'
import { CopyOutlined } from '@ant-design/icons'
import { JsonView, collapseAllNested } from 'react-json-view-lite'

const jsonStyles = {
  container: 'mc-json-tree',
  basicChildStyle: 'mc-json-child',
  childFieldsContainer: 'mc-json-children',
  label: 'mc-json-label',
  clickableLabel: 'mc-json-label mc-json-clickable',
  nullValue: 'mc-json-null',
  undefinedValue: 'mc-json-null',
  stringValue: 'mc-json-string',
  booleanValue: 'mc-json-boolean',
  numberValue: 'mc-json-number',
  otherValue: 'mc-json-value',
  punctuation: 'mc-json-punctuation',
  collapseIcon: 'mc-json-expander mc-json-expanded',
  expandIcon: 'mc-json-expander mc-json-collapsed',
  collapsedContent: 'mc-json-placeholder',
  ariaLables: { collapseJson: '折叠 JSON 节点', expandJson: '展开 JSON 节点' },
  stringifyStringValues: true,
}

export default function JsonPayload({ value }: { value: string }) {
  const [mode, setMode] = useState<'tree' | 'raw'>('tree')
  const parsed = useMemo(() => parseJSON(value), [value])
  const { message } = App.useApp()

  async function copy() {
    try {
      await navigator.clipboard.writeText(value)
      message.success('已复制')
    } catch {
      message.error('复制失败')
    }
  }

  return <div className="overflow-hidden rounded border border-black/10 bg-black/[.025] dark:border-white/10 dark:bg-white/[.03]">
    <div className="flex min-h-10 items-center justify-between gap-2 px-2 py-1.5 sm:px-3">
      {parsed ? <Segmented size="small" value={mode} onChange={(value) => setMode(value as 'tree' | 'raw')} options={[{ label: '树形', value: 'tree' }, { label: '原文', value: 'raw' }]} /> : <span />}
      <Tooltip title="复制"><Button type="text" size="small" icon={<CopyOutlined />} aria-label="复制内容" disabled={!value} onClick={copy} /></Tooltip>
    </div>
    <div className="max-h-[420px] overflow-auto px-3 pb-3 font-mono text-xs leading-5 sm:px-4 sm:pb-4">
      {parsed && mode === 'tree'
        ? <JsonView data={parsed} style={jsonStyles} shouldExpandNode={collapseAllNested} clickToExpandNode />
        : <pre className="m-0 whitespace-pre-wrap break-all font-inherit">{formatJSON(value) || '—'}</pre>}
    </div>
  </div>
}

function parseJSON(value: string): object | null {
  if (!value) return null
  try {
    const parsed: unknown = JSON.parse(value)
    return typeof parsed === 'object' && parsed !== null ? parsed : null
  } catch {
    return null
  }
}

function formatJSON(value: string): string {
  if (!value) return ''
  try { return JSON.stringify(JSON.parse(value), null, 2) } catch { return value }
}

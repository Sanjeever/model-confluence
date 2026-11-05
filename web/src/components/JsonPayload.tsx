import { useMemo, useState } from 'react'
import { JsonView, collapseAllNested } from 'react-json-view-lite'
import { CopyButton } from './console'
import { Tabs, TabsList, TabsTrigger } from './ui/tabs'

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
  const [mode, setMode] = useState('tree')
  const parsed = useMemo(() => parseJSON(value), [value])
  const formatted = useMemo(() => {
    try {
      return JSON.stringify(JSON.parse(value), null, 2)
    } catch {
      return value
    }
  }, [value])
  return (
    <div className="overflow-hidden rounded-md border bg-background">
      <div className="flex items-center justify-between gap-2 border-b px-3 py-1.5">
        {parsed ? (
          <Tabs value={mode} onValueChange={setMode}>
            <TabsList className="h-7">
              <TabsTrigger value="tree" className="text-xs">
                树形
              </TabsTrigger>
              <TabsTrigger value="formatted" className="text-xs">
                格式化
              </TabsTrigger>
              <TabsTrigger value="raw" className="text-xs">
                原文
              </TabsTrigger>
            </TabsList>
          </Tabs>
        ) : (
          <span className="text-xs text-muted-foreground">原文</span>
        )}
        <CopyButton value={value} label="复制内容" />
      </div>
      <div className="max-h-[60dvh] overflow-auto p-4 font-mono text-xs leading-5">
        {parsed && mode === 'tree' ? (
          <JsonView
            data={parsed}
            style={jsonStyles}
            shouldExpandNode={collapseAllNested}
            clickToExpandNode
          />
        ) : (
          <pre className="m-0 whitespace-pre-wrap break-all font-inherit">
            {(mode === 'formatted' ? formatted : value) || '—'}
          </pre>
        )}
      </div>
    </div>
  )
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

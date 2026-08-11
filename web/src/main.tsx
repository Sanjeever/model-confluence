import '@fontsource/ibm-plex-mono/latin-400.css'
import '@fontsource/ibm-plex-mono/latin-500.css'
import '@fontsource/ibm-plex-sans/latin-400.css'
import '@fontsource/ibm-plex-sans/latin-500.css'
import '@fontsource/ibm-plex-sans/latin-600.css'
import './styles.css'

import { StrictMode, useEffect, useMemo, useState } from 'react'
import { createRoot } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { App as AntApp, ConfigProvider, theme } from 'antd'
import zhCN from 'antd/locale/zh_CN'
import Router from './router'

const queryClient = new QueryClient({
  defaultOptions: {
    queries: { retry: false, refetchOnWindowFocus: false },
  },
})

function Root() {
  const [dark, setDark] = useState(() => {
    const saved = localStorage.getItem('mc-theme')
    if (saved) return saved === 'dark'
    return matchMedia('(prefers-color-scheme: dark)').matches
  })

  useEffect(() => {
    document.documentElement.dataset.theme = dark ? 'dark' : 'light'
    localStorage.setItem('mc-theme', dark ? 'dark' : 'light')
  }, [dark])

  const antTheme = useMemo(() => ({
    algorithm: dark ? theme.darkAlgorithm : theme.defaultAlgorithm,
    token: {
      colorPrimary: '#d7783d',
      colorInfo: '#d7783d',
      colorSuccess: '#4f9d7e',
      borderRadius: 4,
      fontFamily: '"IBM Plex Sans", sans-serif',
      fontFamilyCode: '"IBM Plex Mono", monospace',
    },
    components: {
      Layout: { siderBg: dark ? '#09100f' : '#edf0eb', bodyBg: dark ? '#0d1413' : '#f5f6f2' },
      Menu: { darkItemBg: '#09100f', darkItemSelectedBg: '#1d2b27', itemBorderRadius: 2 },
      Table: { headerBorderRadius: 0 },
    },
  }), [dark])

  return (
    <ConfigProvider locale={zhCN} theme={antTheme}>
      <AntApp>
        <QueryClientProvider client={queryClient}>
          <Router dark={dark} onThemeChange={setDark} />
        </QueryClientProvider>
      </AntApp>
    </ConfigProvider>
  )
}

createRoot(document.getElementById('root')!).render(
  <StrictMode><Root /></StrictMode>,
)

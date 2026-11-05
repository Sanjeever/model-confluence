import '@fontsource/ibm-plex-mono/latin-400.css'
import '@fontsource/ibm-plex-mono/latin-500.css'
import './styles.css'

import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { App as AntApp, ConfigProvider, theme } from 'antd'
import zhCN from 'antd/locale/zh_CN'
import { TooltipProvider } from '@/components/ui/tooltip'
import { ThemeProvider, useTheme } from '@/hooks/use-theme'
import Router from './router'

const queryClient = new QueryClient({
  defaultOptions: { queries: { retry: false, refetchOnWindowFocus: false } },
})
function Root() {
  const { dark } = useTheme()
  return (
    <ConfigProvider
      locale={zhCN}
      theme={{
        algorithm: dark ? theme.darkAlgorithm : theme.defaultAlgorithm,
        token: {
          colorPrimary: dark ? '#f4f4f5' : '#18181b',
          borderRadius: 6,
          fontFamily: '"Geist Variable", "Microsoft YaHei", sans-serif',
        },
      }}
    >
      <AntApp>
        <TooltipProvider>
          <QueryClientProvider client={queryClient}>
            <Router />
          </QueryClientProvider>
        </TooltipProvider>
      </AntApp>
    </ConfigProvider>
  )
}
createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <ThemeProvider>
      <Root />
    </ThemeProvider>
  </StrictMode>,
)

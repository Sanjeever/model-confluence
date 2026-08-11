import { useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Button, Layout, Menu, Switch } from 'antd'
import { ApiOutlined, BranchesOutlined, DatabaseOutlined, KeyOutlined, LogoutOutlined, MoonOutlined, SunOutlined } from '@ant-design/icons'
import { api, type Overview } from '../api'
import OverviewPage from './OverviewPage'
import AccessKeysPage from './AccessKeysPage'
import ProvidersPage from './ProvidersPage'
import ModelsPage from './ModelsPage'

const { Sider, Content } = Layout

type Page = 'overview' | 'keys' | 'providers' | 'models'

export default function ConsoleLayout({ dark, onThemeChange }: { dark: boolean; onThemeChange: (value: boolean) => void }) {
  const [page, setPage] = useState<Page>('overview')
  const queryClient = useQueryClient()
  const overview = useQuery({ queryKey: ['overview'], queryFn: () => api<Overview>('/api/admin/overview') })

  async function logout() {
    await api('/api/admin/logout', { method: 'POST' })
    queryClient.clear()
    location.reload()
  }

  return (
    <Layout className="mc-noise h-screen overflow-hidden">
      <Sider width={252} className="h-screen shrink-0 border-r border-black/10 dark:border-white/10">
        <div className="flex h-full flex-col">
          <div className="flex h-24 shrink-0 items-center border-b border-black/10 px-7 dark:border-white/10">
            <div>
              <div className={`text-2xl font-semibold tracking-[-.04em] ${dark ? 'text-[#e5ebe7]' : 'text-[#18211f]'}`}>模汇</div>
              <div className="mt-1 text-xs text-[#6f817a]">大模型网关</div>
            </div>
          </div>
          <div className="flex-1 overflow-y-auto px-3 py-5">
            <Menu
              theme={dark ? 'dark' : 'light'}
              mode="inline"
              style={{ background: 'transparent' }}
              selectedKeys={[page]}
              onSelect={({ key }) => setPage(key as Page)}
              items={[
                { key: 'overview', icon: <DatabaseOutlined />, label: '使用记录' },
                { key: 'keys', icon: <KeyOutlined />, label: '访问密钥' },
                { key: 'providers', icon: <ApiOutlined />, label: '供应商' },
                { key: 'models', icon: <BranchesOutlined />, label: '模型路由' },
              ]}
            />
          </div>
          <div className="shrink-0 border-t border-black/10 p-5 dark:border-white/10">
            <div className="mb-5 flex items-center justify-between text-xs text-[#84968f]">
              <span>外观</span>
              <Switch checked={dark} onChange={onThemeChange} checkedChildren={<MoonOutlined />} unCheckedChildren={<SunOutlined />} />
            </div>
            <Button type="text" danger icon={<LogoutOutlined />} onClick={logout} block>退出登录</Button>
          </div>
        </div>
      </Sider>
      <Layout className="min-w-0 overflow-hidden">
        <Content className="mc-grid overflow-y-auto p-8">
          {page === 'overview' && <OverviewPage data={overview.data} loading={overview.isPending} />}
          {page === 'keys' && <AccessKeysPage />}
          {page === 'providers' && <ProvidersPage />}
          {page === 'models' && <ModelsPage />}
        </Content>
      </Layout>
    </Layout>
  )
}

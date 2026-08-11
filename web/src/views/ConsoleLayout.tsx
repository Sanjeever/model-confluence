import { useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Button, Drawer, Layout, Menu, Switch } from 'antd'
import { ApiOutlined, BranchesOutlined, DatabaseOutlined, KeyOutlined, LogoutOutlined, MenuFoldOutlined, MenuOutlined, MenuUnfoldOutlined, MoonOutlined, SunOutlined } from '@ant-design/icons'
import { api, type Overview } from '../api'
import OverviewPage from './OverviewPage'
import AccessKeysPage from './AccessKeysPage'
import ProvidersPage from './ProvidersPage'
import ModelsPage from './ModelsPage'

const { Sider, Content } = Layout

type Page = 'overview' | 'keys' | 'providers' | 'models'

const menuItems = [
  { key: 'overview', icon: <DatabaseOutlined />, label: '使用记录' },
  { key: 'keys', icon: <KeyOutlined />, label: '访问密钥' },
  { key: 'providers', icon: <ApiOutlined />, label: '供应商' },
  { key: 'models', icon: <BranchesOutlined />, label: '模型路由' },
]

export default function ConsoleLayout({ dark, onThemeChange }: { dark: boolean; onThemeChange: (value: boolean) => void }) {
  const [page, setPage] = useState<Page>('overview')
  const [collapsed, setCollapsed] = useState(false)
  const [mobileMenuOpen, setMobileMenuOpen] = useState(false)
  const queryClient = useQueryClient()
  const overview = useQuery({ queryKey: ['overview'], queryFn: () => api<Overview>('/api/admin/overview') })

  async function logout() {
    await api('/api/admin/logout', { method: 'POST' })
    queryClient.clear()
    location.reload()
  }

  return (
    <Layout className="mc-noise lg:h-screen lg:overflow-hidden" style={{ minHeight: '100dvh' }}>
      <Sider width={252} collapsedWidth={80} collapsed={collapsed} trigger={null} className="!hidden h-screen shrink-0 border-r border-black/10 dark:border-white/10 lg:!block">
        <div className="flex h-full flex-col">
          <div className={`flex h-24 shrink-0 items-center border-b border-black/10 dark:border-white/10 ${collapsed ? 'justify-center px-2' : 'justify-between px-7'}`}>
            {!collapsed && <div>
              <div className={`text-2xl font-semibold tracking-[-.04em] ${dark ? 'text-[#e5ebe7]' : 'text-[#18211f]'}`}>模汇</div>
              <div className="mt-1 text-xs text-[#6f817a]">大模型网关</div>
            </div>}
            <Button type="text" icon={collapsed ? <MenuUnfoldOutlined /> : <MenuFoldOutlined />} aria-label={collapsed ? '展开侧栏' : '收起侧栏'} onClick={() => setCollapsed((value) => !value)} />
          </div>
          <div className="flex-1 overflow-y-auto px-3 py-5">
            <Menu
              theme={dark ? 'dark' : 'light'}
              mode="inline"
              inlineCollapsed={collapsed}
              style={{ background: 'transparent' }}
              selectedKeys={[page]}
              onSelect={({ key }) => setPage(key as Page)}
              items={menuItems}
            />
          </div>
          <div className={`shrink-0 border-t border-black/10 dark:border-white/10 ${collapsed ? 'px-2 py-5' : 'p-5'}`}>
            <div className={`mb-4 flex items-center text-xs text-[#84968f] ${collapsed ? 'justify-center' : 'justify-between'}`}>
              {!collapsed && <span>外观</span>}
              <Switch checked={dark} onChange={onThemeChange} checkedChildren={<MoonOutlined />} unCheckedChildren={<SunOutlined />} />
            </div>
            <Button type="text" danger icon={<LogoutOutlined />} onClick={logout} block>{collapsed ? null : '退出登录'}</Button>
          </div>
        </div>
      </Sider>
      <Layout className="min-w-0 lg:overflow-hidden">
        <header className="sticky top-0 z-20 flex h-16 shrink-0 items-center justify-between border-b border-black/10 bg-[#f5f6f2]/95 px-4 backdrop-blur dark:border-white/10 dark:bg-[#0d1413]/95 lg:hidden">
          <div>
            <div className="text-lg font-semibold">模汇</div>
            <div className="text-[11px] text-[#6f817a]">大模型网关</div>
          </div>
          <Button type="text" icon={<MenuOutlined />} aria-label="打开导航" onClick={() => setMobileMenuOpen(true)} />
        </header>
        <Content className="mc-grid min-h-[calc(100dvh-64px)] overflow-visible p-4 sm:p-6 lg:min-h-0 lg:overflow-y-auto lg:p-8">
          {page === 'overview' && <OverviewPage data={overview.data} loading={overview.isPending} />}
          {page === 'keys' && <AccessKeysPage />}
          {page === 'providers' && <ProvidersPage />}
          {page === 'models' && <ModelsPage />}
        </Content>
      </Layout>
      <Drawer title="模汇" placement="left" size={280} open={mobileMenuOpen} onClose={() => setMobileMenuOpen(false)} rootClassName="lg:hidden" styles={{ body: { padding: 0 } }}>
        <div className="flex h-full flex-col">
          <Menu
            theme={dark ? 'dark' : 'light'}
            mode="inline"
            style={{ background: 'transparent', borderInlineEnd: 0 }}
            selectedKeys={[page]}
            items={menuItems}
            onSelect={({ key }) => {
              setPage(key as Page)
              setMobileMenuOpen(false)
            }}
          />
          <div className="mt-auto border-t border-black/10 p-4 dark:border-white/10">
            <div className="mb-4 flex items-center justify-between text-sm text-[#84968f]">
              <span>外观</span>
              <Switch checked={dark} onChange={onThemeChange} checkedChildren={<MoonOutlined />} unCheckedChildren={<SunOutlined />} />
            </div>
            <Button type="text" danger icon={<LogoutOutlined />} onClick={logout} block>退出登录</Button>
          </div>
        </div>
      </Drawer>
    </Layout>
  )
}

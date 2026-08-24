import { useState } from 'react'
import { App, Button, Drawer, Form, Input, Layout, Menu, Modal, Switch } from 'antd'
import { ApiOutlined, BranchesOutlined, DashboardOutlined, DatabaseOutlined, FundOutlined, KeyOutlined, LockOutlined, LogoutOutlined, MenuFoldOutlined, MenuOutlined, MenuUnfoldOutlined, MoonOutlined, SunOutlined, HeartOutlined } from '@ant-design/icons'
import { Outlet, useLocation, useNavigate } from 'react-router-dom'
import { api } from '../api'

const { Sider, Content } = Layout

type PasswordForm = { current_password: string; new_password: string; confirm_password: string }

const menuItems = [
  { key: '/performance', icon: <DashboardOutlined />, label: '性能监控' },
  { key: '/requests', icon: <DatabaseOutlined />, label: '使用记录' },
  { key: '/usage', icon: <FundOutlined />, label: '用量统计' },
  { key: '/health', icon: <HeartOutlined />, label: '上游健康' },
  { key: '/access-keys', icon: <KeyOutlined />, label: '访问密钥' },
  { key: '/providers', icon: <ApiOutlined />, label: '供应商' },
  { key: '/models', icon: <BranchesOutlined />, label: '模型路由' },
]

export default function ConsoleLayout({ dark, onThemeChange, onSignedOut }: { dark: boolean; onThemeChange: (value: boolean) => void; onSignedOut: () => void }) {
  const [collapsed, setCollapsed] = useState(false)
  const [mobileMenuOpen, setMobileMenuOpen] = useState(false)
  const [passwordOpen, setPasswordOpen] = useState(false)
  const [passwordSaving, setPasswordSaving] = useState(false)
  const [passwordForm] = Form.useForm<PasswordForm>()
  const navigate = useNavigate()
  const location = useLocation()
  const { message } = App.useApp()
  const selectedPath = location.pathname.startsWith('/requests/') ? '/requests' : location.pathname

  async function logout() {
    await api('/api/admin/logout', { method: 'POST' })
    onSignedOut()
  }

  async function changePassword(values: PasswordForm) {
    setPasswordSaving(true)
    try {
      await api('/api/admin/change-password', { method: 'POST', body: JSON.stringify({ current_password: values.current_password, new_password: values.new_password }) })
      message.success('密码已修改，请重新登录')
      setPasswordOpen(false)
      passwordForm.resetFields()
      onSignedOut()
    } catch (error) {
      message.error(error instanceof Error ? error.message : '无法修改密码')
    } finally {
      setPasswordSaving(false)
    }
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
              selectedKeys={[selectedPath]}
              onSelect={({ key }) => navigate(key)}
              items={menuItems}
            />
          </div>
          <div className={`shrink-0 border-t border-black/10 dark:border-white/10 ${collapsed ? 'px-2 py-5' : 'p-5'}`}>
            <div className={`mb-4 flex items-center text-xs text-[#84968f] ${collapsed ? 'justify-center' : 'justify-between'}`}>
              {!collapsed && <span>外观</span>}
              <Switch checked={dark} onChange={onThemeChange} checkedChildren={<MoonOutlined />} unCheckedChildren={<SunOutlined />} />
            </div>
            <Button type="text" icon={<LockOutlined />} onClick={() => setPasswordOpen(true)} block>{collapsed ? null : '修改密码'}</Button>
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
          <Outlet />
        </Content>
      </Layout>
      <Drawer title="模汇" placement="left" size={280} open={mobileMenuOpen} onClose={() => setMobileMenuOpen(false)} rootClassName="lg:hidden" styles={{ body: { padding: 0 } }}>
        <div className="flex h-full flex-col">
          <Menu
            theme={dark ? 'dark' : 'light'}
            mode="inline"
            style={{ background: 'transparent', borderInlineEnd: 0 }}
            selectedKeys={[selectedPath]}
            items={menuItems}
            onSelect={({ key }) => {
              navigate(key)
              setMobileMenuOpen(false)
            }}
          />
          <div className="mt-auto border-t border-black/10 p-4 dark:border-white/10">
            <div className="mb-4 flex items-center justify-between text-sm text-[#84968f]">
              <span>外观</span>
              <Switch checked={dark} onChange={onThemeChange} checkedChildren={<MoonOutlined />} unCheckedChildren={<SunOutlined />} />
            </div>
            <Button type="text" icon={<LockOutlined />} onClick={() => { setMobileMenuOpen(false); setPasswordOpen(true) }} block>修改密码</Button>
            <Button type="text" danger icon={<LogoutOutlined />} onClick={logout} block>退出登录</Button>
          </div>
        </div>
      </Drawer>
      <Modal title="修改管理员密码" open={passwordOpen} onCancel={() => { setPasswordOpen(false); passwordForm.resetFields() }} onOk={() => passwordForm.submit()} confirmLoading={passwordSaving} okText="修改密码" cancelText="取消" destroyOnHidden>
        <Form form={passwordForm} layout="vertical" onFinish={changePassword} className="pt-4" requiredMark={false}>
          <Form.Item name="current_password" label="当前密码" rules={[{ required: true, message: '请输入当前密码' }]}><Input.Password autoComplete="current-password" /></Form.Item>
          <Form.Item name="new_password" label="新密码" rules={[{ required: true, message: '请输入新密码' }, { min: 12, message: '新密码至少需要 12 个字符' }]}><Input.Password autoComplete="new-password" /></Form.Item>
          <Form.Item name="confirm_password" label="确认新密码" dependencies={['new_password']} rules={[{ required: true, message: '请再次输入新密码' }, ({ getFieldValue }) => ({ validator: (_, value) => !value || value === getFieldValue('new_password') ? Promise.resolve() : Promise.reject(new Error('两次输入的新密码不一致')) })]}><Input.Password autoComplete="new-password" /></Form.Item>
        </Form>
      </Modal>
    </Layout>
  )
}

import { useState } from 'react'
import { Button, Form, Input, message } from 'antd'
import { ArrowRightOutlined, LockOutlined } from '@ant-design/icons'
import { api } from '../api'

export default function LoginPage({ onAuthenticated }: { onAuthenticated: () => void }) {
  const [loading, setLoading] = useState(false)

  async function submit(values: { password: string }) {
    setLoading(true)
    try {
      await api('/api/admin/login', { method: 'POST', body: JSON.stringify(values) })
      onAuthenticated()
    } catch (error) {
      message.error(error instanceof Error ? error.message : '登录失败')
    } finally {
      setLoading(false)
    }
  }

  return (
    <main className="mc-grid mc-noise min-h-dvh bg-[#0d1413] text-[#d9e1dc]">
      <div className="grid min-h-dvh grid-cols-1 lg:grid-cols-[minmax(520px,1fr)_520px]">
        <section className="relative flex min-h-[220px] flex-col justify-between overflow-hidden p-8 sm:min-h-[280px] sm:p-10 lg:min-h-screen lg:p-16">
          <div className="absolute left-[18%] top-[24%] h-48 w-48 rounded-full border border-[#31433d] sm:h-64 sm:w-64 lg:h-72 lg:w-72" />
          <div className="absolute left-[calc(18%+48px)] top-[calc(24%+48px)] h-48 w-48 rounded-full border border-[#d7783d]/50 sm:left-[calc(18%+64px)] sm:top-[calc(24%+64px)] sm:h-64 sm:w-64 lg:left-[calc(18%+72px)] lg:top-[calc(24%+72px)] lg:h-72 lg:w-72" />
          <div className="relative z-10">
            <div className="text-sm tracking-[.12em] text-[#8fa39a]">大模型协议网关</div>
            <h1 className="mt-4 text-5xl font-semibold text-[#edf2ee] sm:mt-5 sm:text-6xl">模汇</h1>
          </div>
          <div className="relative z-10 hidden max-w-xl sm:block">
            <p className="mb-7 text-sm leading-7 text-[#7f928a]">支持 OpenAI Responses、Chat Completions 与 Anthropic Messages</p>
            <h2 className="text-3xl font-medium leading-tight text-[#dce5df] lg:text-4xl">让协议在边界处汇合，<br />让每一次请求都有迹可循。</h2>
          </div>
        </section>
        <section className="flex items-center border-t border-[#26332f] bg-[#101817]/90 px-6 py-10 sm:px-10 lg:border-l lg:border-t-0 lg:px-16 lg:py-0">
          <div className="mc-enter mx-auto w-full max-w-md">
            <h3 className="mb-2 text-2xl font-semibold tracking-tight">管理员登录</h3>
            <p className="mb-9 text-sm text-[#81938c]">进入请求追踪与路由控制台</p>
            <Form layout="vertical" onFinish={submit} requiredMark={false}>
              <Form.Item name="password" label={<span className="text-sm text-[#82948d]">管理员密码</span>} rules={[{ required: true, message: '请输入管理员密码' }]}>
                <Input.Password prefix={<LockOutlined />} size="large" autoFocus placeholder="输入管理员密码" />
              </Form.Item>
              <Button htmlType="submit" type="primary" size="large" loading={loading} block iconPlacement="end" icon={<ArrowRightOutlined />}>
                进入控制台
              </Button>
            </Form>
            <p className="mt-8 border-t border-[#293632] pt-6 text-xs leading-5 text-[#63756e]">登录会话闲置 24 小时后失效，最长保留 7 天。</p>
          </div>
        </section>
      </div>
    </main>
  )
}

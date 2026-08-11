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
    <main className="mc-grid mc-noise min-h-screen bg-[#0d1413] text-[#d9e1dc]">
      <div className="grid min-h-screen grid-cols-[minmax(520px,1fr)_520px]">
        <section className="relative flex flex-col justify-between overflow-hidden p-16">
          <div className="absolute left-[18%] top-[24%] h-72 w-72 rounded-full border border-[#31433d]" />
          <div className="absolute left-[calc(18%+72px)] top-[calc(24%+72px)] h-72 w-72 rounded-full border border-[#d7783d]/50" />
          <div className="relative z-10">
            <div className="text-sm tracking-[.12em] text-[#8fa39a]">大模型协议网关</div>
            <h1 className="mt-5 text-6xl font-semibold tracking-[-.055em] text-[#edf2ee]">模汇</h1>
          </div>
          <div className="relative z-10 max-w-xl">
            <p className="mb-7 text-sm leading-7 text-[#7f928a]">支持 OpenAI Responses、Chat Completions 与 Anthropic Messages</p>
            <h2 className="text-4xl font-medium leading-tight tracking-[-.035em] text-[#dce5df]">让协议在边界处汇合，<br />让每一次请求都有迹可循。</h2>
          </div>
        </section>
        <section className="flex items-center border-l border-[#26332f] bg-[#101817]/90 px-16">
          <div className="mc-enter w-full">
            <h3 className="mb-2 text-2xl font-semibold tracking-tight">管理员登录</h3>
            <p className="mb-9 text-sm text-[#81938c]">进入请求追踪与路由控制台</p>
            <Form layout="vertical" onFinish={submit} requiredMark={false}>
              <Form.Item name="password" label={<span className="text-sm text-[#82948d]">管理员密码</span>} rules={[{ required: true, message: '请输入管理员密码' }]}>
                <Input.Password prefix={<LockOutlined />} size="large" autoFocus placeholder="输入管理员密码" />
              </Form.Item>
              <Button htmlType="submit" type="primary" size="large" loading={loading} block iconPosition="end" icon={<ArrowRightOutlined />}>
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

import { Loading } from '@/components/console'
export default function LoadingScreen() {
  return (
    <main className="flex min-h-dvh items-center justify-center">
      <Loading label="正在连接管理服务" />
    </main>
  )
}

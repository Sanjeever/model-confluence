export default function LoadingScreen() {
  return (
    <main className="mc-grid flex min-h-screen items-center justify-center bg-[#0d1413] text-[#d9e1dc]">
      <div className="text-center">
        <div className="mx-auto mb-5 h-10 w-px animate-pulse bg-[#d7783d]" />
        <p className="text-sm tracking-[.12em]">正在连接管理服务</p>
      </div>
    </main>
  )
}

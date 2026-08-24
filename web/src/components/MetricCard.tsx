import { Card, Skeleton } from 'antd'

export default function MetricCard({ label, value, opacity, loading }: { label: string; value: string; opacity: number; loading: boolean }) {
  return <Card className="relative overflow-hidden" styles={{ body: { minHeight: 132 } }}>
    <div className="absolute right-0 top-0 h-full w-1 bg-[#d7783d]" style={{ opacity }} />
    <div className="mb-6 text-sm text-[#7c8d86] sm:mb-8">{label}</div>
    {loading ? <Skeleton.Input active size="large" /> : <div className="font-mono text-3xl font-medium sm:text-4xl">{value}</div>}
  </Card>
}

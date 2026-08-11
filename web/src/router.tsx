import { useQuery } from '@tanstack/react-query'
import { api } from './api'
import LoginPage from './views/LoginPage'
import ConsoleLayout from './views/ConsoleLayout'
import LoadingScreen from './views/LoadingScreen'

type Props = {
  dark: boolean
  onThemeChange: (value: boolean) => void
}

export default function Router({ dark, onThemeChange }: Props) {
  const session = useQuery({
    queryKey: ['session'],
    queryFn: () => api<{ authenticated: boolean }>('/api/admin/session'),
  })

  if (session.isPending) return <LoadingScreen />
  if (session.isError) return <LoginPage onAuthenticated={() => session.refetch()} />
  return <ConsoleLayout dark={dark} onThemeChange={onThemeChange} />
}

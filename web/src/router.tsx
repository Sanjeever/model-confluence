import { useEffect, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { BrowserRouter, Navigate, Outlet, Route, Routes, useLocation, useNavigate } from 'react-router-dom'
import { api, onUnauthorized } from './api'
import LoginPage from './views/LoginPage'
import ConsoleLayout from './views/ConsoleLayout'
import LoadingScreen from './views/LoadingScreen'
import OverviewPage from './views/OverviewPage'
import PerformancePage from './views/PerformancePage'
import CostsPage from './views/CostsPage'
import HealthPage from './views/HealthPage'
import AccessKeysPage from './views/AccessKeysPage'
import ProvidersPage from './views/ProvidersPage'
import ModelsPage from './views/ModelsPage'

type Props = {
  dark: boolean
  onThemeChange: (value: boolean) => void
}

export default function Router({ dark, onThemeChange }: Props) {
  return <BrowserRouter><AppRoutes dark={dark} onThemeChange={onThemeChange} /></BrowserRouter>
}

function AppRoutes({ dark, onThemeChange }: Props) {
  const [signedOut, setSignedOut] = useState(false)
  const queryClient = useQueryClient()
  const session = useQuery({
    queryKey: ['session'],
    queryFn: () => api<{ authenticated: boolean }>('/api/admin/session'),
    refetchOnWindowFocus: true,
  })
  const location = useLocation()
  const navigate = useNavigate()
  const refetchSession = session.refetch

  useEffect(() => onUnauthorized(() => {
    void refetchSession().then((result) => {
      if (result.isError) setSignedOut(true)
    })
  }), [refetchSession])

  async function authenticated() {
    setSignedOut(false)
    const result = await session.refetch()
    if (!result.isError) {
      const destination = (location.state as { returnTo?: string } | null)?.returnTo ?? '/performance'
      navigate(destination, { replace: true })
    }
  }

  function signOut() {
    setSignedOut(true)
    queryClient.clear()
    navigate('/login', { replace: true })
  }

  const unauthenticated = signedOut || session.isError

  return <Routes>
    <Route path="/login" element={session.isPending && !signedOut ? <LoadingScreen /> : unauthenticated ? <LoginPage onAuthenticated={authenticated} /> : <Navigate to="/performance" replace />} />
    <Route element={<RequireSession pending={session.isPending} unauthenticated={unauthenticated} />}>
      <Route element={<ConsoleLayout dark={dark} onThemeChange={onThemeChange} onSignedOut={signOut} />}>
        <Route index element={<Navigate to="/performance" replace />} />
        <Route path="performance" element={<PerformancePage />} />
        <Route path="requests" element={<OverviewPage />} />
        <Route path="requests/:detailID" element={<OverviewPage />} />
        <Route path="usage" element={<CostsPage />} />
        <Route path="health" element={<HealthPage />} />
        <Route path="access-keys" element={<AccessKeysPage />} />
        <Route path="providers" element={<ProvidersPage />} />
        <Route path="models" element={<ModelsPage />} />
        <Route path="*" element={<Navigate to="/performance" replace />} />
      </Route>
    </Route>
  </Routes>
}

function RequireSession({ pending, unauthenticated }: { pending: boolean; unauthenticated: boolean }) {
  const location = useLocation()
  if (pending && !unauthenticated) return <LoadingScreen />
  if (unauthenticated) return <Navigate to="/login" replace state={{ returnTo: location.pathname + location.search }} />
  return <Outlet />
}

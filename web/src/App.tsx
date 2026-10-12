import { useEffect, useState } from 'react'
import { Navigate, Route, Routes, useLocation } from 'react-router'
import { api } from './lib/api'
import { useApp } from './store'
import { connectGateway, disconnectGateway } from './gateway'
import Setup from './routes/Setup'
import Login from './routes/Login'
import Register from './routes/Register'
import InvitePage from './routes/InvitePage'
import AppShell from './routes/AppShell'
import { Spinner } from './components/ui'

export default function App() {
  const me = useApp((s) => s.me)
  const authChecked = useApp((s) => s.authChecked)
  const setMe = useApp((s) => s.setMe)
  const [needsSetup, setNeedsSetup] = useState<boolean | null>(null)
  const location = useLocation()

  useEffect(() => {
    api
      .get<{ needs_setup: boolean }>('/api/v1/setup/status')
      .then((r) => setNeedsSetup(r.needs_setup))
      .catch(() => setNeedsSetup(false))
    api
      .get<{ user: import('./lib/types').User }>('/api/v1/users/me')
      .then((r) => setMe(r.user))
      .catch(() => setMe(null))
  }, [setMe])

  // Gateway lives while signed in.
  useEffect(() => {
    if (me) {
      connectGateway()
      return () => disconnectGateway()
    }
  }, [me?.id])

  if (needsSetup === null || !authChecked) {
    return (
      <div className="flex h-full items-center justify-center">
        <Spinner />
      </div>
    )
  }

  if (needsSetup && location.pathname !== '/setup') {
    return <Navigate to="/setup" replace />
  }

  return (
    <Routes>
      <Route path="/setup" element={needsSetup ? <Setup onDone={() => setNeedsSetup(false)} /> : <Navigate to="/app" replace />} />
      <Route path="/login" element={me ? <Navigate to="/app" replace /> : <Login />} />
      <Route path="/register" element={me ? <Navigate to="/app" replace /> : <Register />} />
      <Route path="/invite/:code" element={<InvitePage />} />
      <Route path="/app/*" element={me ? <AppShell /> : <Navigate to="/login" replace />} />
      <Route path="*" element={<Navigate to={me ? '/app' : '/login'} replace />} />
    </Routes>
  )
}

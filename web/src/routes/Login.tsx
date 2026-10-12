import { useState } from 'react'
import { Link, useNavigate } from 'react-router'
import { api, ApiError } from '../lib/api'
import { useApp } from '../store'
import { AuthCard, Button, ErrorText, Input, Label } from '../components/ui'

export default function Login() {
  const navigate = useNavigate()
  const setMe = useApp((s) => s.setMe)
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    setBusy(true)
    setError('')
    try {
      const res = await api.post<{ user: import('../lib/types').User }>('/api/v1/auth/login', {
        username,
        password,
      })
      setMe(res.user)
      navigate('/app', { replace: true })
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Login failed')
    } finally {
      setBusy(false)
    }
  }

  return (
    <AuthCard title="Sign in to Lantern">
      <form className="space-y-4" onSubmit={submit}>
        <div>
          <Label>Username</Label>
          <Input value={username} onChange={(e) => setUsername(e.target.value)} autoComplete="username" autoFocus required />
        </div>
        <div>
          <Label>Password</Label>
          <Input type="password" value={password} onChange={(e) => setPassword(e.target.value)} autoComplete="current-password" required />
        </div>
        <ErrorText>{error}</ErrorText>
        <Button type="submit" className="w-full" disabled={busy}>
          {busy ? 'Signing in…' : 'Sign in'}
        </Button>
        <p className="text-center text-sm text-ink-1">
          Have an invite? <Link to="/register" className="text-accent hover:underline">Create an account</Link>
        </p>
      </form>
    </AuthCard>
  )
}

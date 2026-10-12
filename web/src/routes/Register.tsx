import { useState } from 'react'
import { Link, useNavigate, useSearchParams } from 'react-router'
import { api, ApiError } from '../lib/api'
import { useApp } from '../store'
import { AuthCard, Button, ErrorText, Input, Label } from '../components/ui'

export default function Register() {
  const navigate = useNavigate()
  const [params] = useSearchParams()
  const setMe = useApp((s) => s.setMe)
  const [invite, setInvite] = useState(params.get('invite') ?? '')
  const [username, setUsername] = useState('')
  const [displayName, setDisplayName] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    setBusy(true)
    setError('')
    try {
      const res = await api.post<{ user: import('../lib/types').User }>('/api/v1/auth/register', {
        invite_code: invite,
        username,
        display_name: displayName || username,
        password,
      })
      setMe(res.user)
      navigate('/app', { replace: true })
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Registration failed')
    } finally {
      setBusy(false)
    }
  }

  return (
    <AuthCard title="Join this Lantern server">
      <form className="space-y-4" onSubmit={submit}>
        <div>
          <Label>Invite code</Label>
          <Input value={invite} onChange={(e) => setInvite(e.target.value.toUpperCase())} placeholder="From your friend" required />
        </div>
        <div>
          <Label>Username</Label>
          <Input value={username} onChange={(e) => setUsername(e.target.value.toLowerCase())} placeholder="a-z, 0-9, _ ." autoComplete="username" required />
        </div>
        <div>
          <Label>Display name</Label>
          <Input value={displayName} onChange={(e) => setDisplayName(e.target.value)} placeholder="How friends see you (optional)" />
        </div>
        <div>
          <Label>Password</Label>
          <Input type="password" value={password} onChange={(e) => setPassword(e.target.value)} autoComplete="new-password" required minLength={8} />
        </div>
        <ErrorText>{error}</ErrorText>
        <Button type="submit" className="w-full" disabled={busy}>
          {busy ? 'Creating account…' : 'Create account'}
        </Button>
        <p className="text-center text-sm text-ink-1">
          Already a member? <Link to="/login" className="text-accent hover:underline">Sign in</Link>
        </p>
      </form>
    </AuthCard>
  )
}

import { useState } from 'react'
import { useNavigate } from 'react-router'
import { api, ApiError } from '../lib/api'
import { useApp } from '../store'
import { AuthCard, Button, ErrorText, Input, Label } from '../components/ui'
import type { NameClaim } from '../lib/types'

// The public Talaria server that hands out free names (see Hosted DDNS in
// the admin settings to run your own).
const NAME_HOST = 'https://talaria.gg'

export default function Setup({ onDone }: { onDone: () => void }) {
  const navigate = useNavigate()
  const setMe = useApp((s) => s.setMe)
  const [step, setStep] = useState(0)
  const [serverName, setServerName] = useState('')
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [confirm, setConfirm] = useState('')
  const [domain, setDomain] = useState('')
  const [wanted, setWanted] = useState('')
  const [claimed, setClaimed] = useState<NameClaim | null>(null)
  const [claiming, setClaiming] = useState(false)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  async function claim() {
    setClaiming(true)
    setError('')
    try {
      const c = await api.post<NameClaim>('/api/v1/setup/claim-name', { url: NAME_HOST, name: wanted })
      setClaimed(c)
      setDomain(c.domain)
    } catch (e) {
      setError(e instanceof ApiError ? e.message : 'Could not reach talaria.gg')
    } finally {
      setClaiming(false)
    }
  }

  async function finish() {
    setError('')
    if (password !== confirm) {
      setError('Passwords do not match')
      return
    }
    setBusy(true)
    try {
      const res = await api.post<{ user: import('../lib/types').User; restart_needed_for_tls: boolean }>(
        '/api/v1/setup',
        {
          server_name: serverName,
          username,
          password,
          domain,
          ...(claimed && domain === claimed.domain ? { ddns_url: NAME_HOST, ddns_token: claimed.token } : {}),
        },
      )
      setMe(res.user)
      onDone()
      if (res.restart_needed_for_tls) {
        alert(
          'Domain saved. HTTPS via Let\'s Encrypt activates the next time Talaria starts.\n\n' +
            'Make sure your router forwards TCP ports 80 and 443 to this machine first.',
        )
      }
      navigate('/app', { replace: true })
    } catch (e) {
      setError(e instanceof ApiError ? e.message : 'Setup failed')
    } finally {
      setBusy(false)
    }
  }

  return (
    <AuthCard title="Welcome to Talaria">
      {step === 0 && (
        <form
          className="space-y-4"
          onSubmit={(e) => {
            e.preventDefault()
            if (!username || password.length < 8) {
              setError('Pick a username and a password of at least 8 characters.')
              return
            }
            setError('')
            setStep(1)
          }}
        >
          <p className="text-sm text-ink-1">
            Let's set up your server. First, create the admin account — that's you.
          </p>
          <div>
            <Label>Server name</Label>
            <Input value={serverName} onChange={(e) => setServerName(e.target.value)} placeholder="Skyle's Talaria" />
          </div>
          <div>
            <Label>Username</Label>
            <Input
              value={username}
              onChange={(e) => setUsername(e.target.value.toLowerCase())}
              placeholder="a-z, 0-9, _ ."
              autoComplete="username"
              required
            />
          </div>
          <div>
            <Label>Password</Label>
            <Input type="password" value={password} onChange={(e) => setPassword(e.target.value)} autoComplete="new-password" required minLength={8} />
          </div>
          <div>
            <Label>Confirm password</Label>
            <Input type="password" value={confirm} onChange={(e) => setConfirm(e.target.value)} autoComplete="new-password" required />
          </div>
          <ErrorText>{error}</ErrorText>
          <Button type="submit" className="w-full">Continue</Button>
        </form>
      )}
      {step === 1 && (
        <form
          className="space-y-4"
          onSubmit={(e) => {
            e.preventDefault()
            finish()
          }}
        >
          <p className="text-sm text-ink-1">
            <strong className="text-ink">Remote access (optional).</strong> Give your server an address
            on the internet and Talaria fetches a free HTTPS certificate on next start. You'll need to
            forward TCP ports 80 and 443 on your router. Skip this to use Talaria on your local network
            only — everything here can be changed later in Settings.
          </p>
          {claimed ? (
            <p className="rounded-lg bg-success/10 px-3 py-2 text-sm text-success">
              <strong>{claimed.domain}</strong> is yours. It already points at this connection, and Talaria
              will keep it updated.{' '}
              <button type="button" className="underline" onClick={() => { setClaimed(null); setDomain('') }}>
                Use something else
              </button>
            </p>
          ) : (
            <div>
              <Label>Get a free name</Label>
              <div className="flex items-center gap-2">
                <Input
                  value={wanted}
                  onChange={(e) => setWanted(e.target.value.toLowerCase().trim())}
                  placeholder="yourname"
                  className="flex-1"
                />
                <span className="whitespace-nowrap text-sm text-ink-2">.talaria.gg</span>
                <Button type="button" variant="ghost" onClick={claim} disabled={claiming || !wanted}>
                  {claiming ? 'Claiming…' : 'Claim'}
                </Button>
              </div>
              <p className="mt-1 text-xs text-ink-2">
                No account needed; 3-32 lowercase letters, digits and hyphens. Or enter a domain you already
                have below.
              </p>
            </div>
          )}
          {!claimed && (
            <div>
              <Label>Your own DDNS domain</Label>
              <Input value={domain} onChange={(e) => setDomain(e.target.value.trim())} placeholder="yourname.duckdns.org" />
            </div>
          )}
          <ErrorText>{error}</ErrorText>
          <div className="flex gap-2">
            <Button type="button" variant="ghost" onClick={() => setStep(0)}>Back</Button>
            <Button type="submit" className="flex-1" disabled={busy}>
              {busy ? 'Setting up…' : domain ? 'Finish with remote access' : 'Finish (local only)'}
            </Button>
          </div>
        </form>
      )}
    </AuthCard>
  )
}

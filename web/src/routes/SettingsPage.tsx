import { useEffect, useState } from 'react'
import { Bell, BellOff, Download, Link2, Moon, Monitor, Sun, X } from 'lucide-react'
import { AvatarCropper } from '../components/AvatarCropper'
import { api, uploadFile } from '../lib/api'
import { useApp } from '../store'
import { PALETTES } from '../lib/palettes'
import {
  canInstall, disablePush, enablePush, isStandalone, onInstallChange, promptInstall, pushState,
  type PushState,
} from '../lib/notifications'
import { Avatar } from '../components/Avatar'
import { InviteLinkShare } from '../components/InviteLink'
import { Button, cx, ErrorText, Input, Label, Segmented, Select } from '../components/ui'
import { PageHeader } from './AppShell'
import type { Invite, LanAccessStatus, ServerConfig, SessionInfo, User } from '../lib/types'
import { formatDateTime, formatBytes } from '../lib/time'

export default function SettingsPage({ onMenu, onClose }: { onMenu?: () => void; onClose?: () => void }) {
  const me = useApp((s) => s.me)
  return (
    <div className="flex min-h-0 w-full flex-1 flex-col">
      <PageHeader
        onMenu={onMenu}
        title="Settings"
        right={
          onClose && (
            <button onClick={onClose} title="Close settings (Esc)" className="rounded-lg p-1.5 text-ink-1 hover:bg-bg-3 hover:text-ink">
              <X size={20} />
            </button>
          )
        }
      />
      {onClose && <EscToClose onClose={onClose} />}
      <div className="flex-1 space-y-8 overflow-y-auto p-4 pb-16">
        <ProfileSection />
        <NotificationsSection />
        <AppearanceSection />
        <PasswordSection />
        <SessionsSection />
        {me?.is_admin && <AdminSection />}
      </div>
    </div>
  )
}

function EscToClose({ onClose }: { onClose: () => void }) {
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])
  return null
}

function Card({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section className="max-w-lg rounded-xl border border-edge bg-bg-1 p-4">
      <h2 className="mb-3 font-semibold text-ink">{title}</h2>
      {children}
    </section>
  )
}

function ProfileSection() {
  const me = useApp((s) => s.me)
  const setMe = useApp((s) => s.setMe)
  const [displayName, setDisplayName] = useState(me?.display_name ?? '')
  const [status, setStatus] = useState(me?.status_text ?? '')
  const [msg, setMsg] = useState('')
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)
  const [cropFile, setCropFile] = useState<File | null>(null)

  async function save(extra?: Record<string, unknown>) {
    setBusy(true)
    setErr('')
    setMsg('')
    try {
      const res = await api.patch<{ user: User }>('/api/v1/users/me', {
        display_name: displayName,
        status_text: status,
        ...extra,
      })
      setMe(res.user)
      setMsg('Saved.')
    } catch (e: any) {
      setErr(e?.message ?? 'Could not save')
    } finally {
      setBusy(false)
    }
  }

  async function saveAvatar(blob: Blob) {
    const f = new File([blob], blob.type === 'image/png' ? 'avatar.png' : 'avatar.jpg', { type: blob.type })
    const res = await uploadFile(f)
    await save({ avatar_file_id: res.attachment.file_id })
    setCropFile(null)
  }

  if (!me) return null
  return (
    <Card title="Profile">
      <div className="mb-4 flex items-center gap-4">
        <Avatar user={me} size={64} />
        <div className="space-x-2">
          <label className="inline-block cursor-pointer rounded-lg bg-bg-2 px-3 py-1.5 text-sm text-ink hover:bg-bg-3">
            Change avatar
            <input
              type="file"
              accept="image/png,image/jpeg,image/gif,image/webp"
              hidden
              onChange={(e) => {
                if (e.target.files?.[0]) setCropFile(e.target.files[0])
                e.target.value = ''
              }}
            />
          </label>
          <AvatarCropper file={cropFile} onCancel={() => setCropFile(null)} onDone={saveAvatar} />
          {me.avatar_file_id && (
            <Button variant="ghost" className="px-3 py-1.5 text-sm" onClick={() => save({ avatar_file_id: '' })}>
              Remove
            </Button>
          )}
        </div>
      </div>
      <div className="space-y-3">
        <div>
          <Label>Display name</Label>
          <Input value={displayName} onChange={(e) => setDisplayName(e.target.value)} maxLength={64} />
        </div>
        <div>
          <Label>Status</Label>
          <Input value={status} onChange={(e) => setStatus(e.target.value)} maxLength={128} placeholder="What's happening?" />
        </div>
        <div className="flex items-center gap-3">
          <Button onClick={() => save()} disabled={busy}>Save profile</Button>
          {msg && <span className="text-sm text-success">{msg}</span>}
        </div>
        <ErrorText>{err}</ErrorText>
      </div>
    </Card>
  )
}

function AppearanceSection() {
  const theme = useApp((s) => s.theme)
  const setTheme = useApp((s) => s.setTheme)
  const palette = useApp((s) => s.palette)
  const setPalette = useApp((s) => s.setPalette)
  const dark =
    theme === 'dark' || (theme === 'system' && matchMedia('(prefers-color-scheme: dark)').matches)
  return (
    <Card title="Appearance">
      <div className="space-y-4">
        <div>
          <Label>Light or dark</Label>
          <Segmented
            value={theme}
            onChange={setTheme}
            options={[
              { value: 'light', label: <><Sun size={15} /> Light</> },
              { value: 'dark', label: <><Moon size={15} /> Dark</> },
              { value: 'system', label: <><Monitor size={15} /> Match system</> },
            ]}
          />
        </div>
        <div>
          <Label>Color theme</Label>
          <p className="mb-2 text-xs text-ink-2">
            Every theme works in light and dark mode. Pick whichever feels like you.
          </p>
          <div className="grid grid-cols-2 gap-2 sm:grid-cols-3">
            {PALETTES.map((p) => {
              const t = dark ? p.dark : p.light
              const selected = palette === p.id
              return (
                <button
                  key={p.id}
                  onClick={() => setPalette(p.id)}
                  className={cx(
                    'rounded-xl border p-2 text-left transition-all',
                    selected
                      ? 'border-accent ring-2 ring-accent/40'
                      : 'border-edge hover:border-ink-2',
                  )}
                  style={{ background: t['bg-1'] }}
                >
                  <span className="mb-1.5 flex gap-1">
                    <span className="h-4 w-4 rounded-full border border-black/10" style={{ background: t.accent }} />
                    <span className="h-4 w-4 rounded-full border border-black/10" style={{ background: t['bg-3'] }} />
                    <span className="h-4 w-4 rounded-full border border-black/10" style={{ background: t['bg-0'] }} />
                  </span>
                  <span className="block truncate text-xs font-semibold" style={{ color: t['text-0'] }}>
                    {p.label}
                  </span>
                </button>
              )
            })}
          </div>
        </div>
      </div>
    </Card>
  )
}

function NotificationsSection() {
  const [state, setState] = useState<PushState | null>(null)
  const [devices, setDevices] = useState(0)
  const [installable, setInstallable] = useState(canInstall())
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)

  const refresh = () => {
    pushState().then(setState)
    api.get<{ devices: number }>('/api/v1/push/status').then((r) => setDevices(r.devices)).catch(() => {})
  }
  useEffect(() => {
    refresh()
    return onInstallChange(() => setInstallable(canInstall()))
  }, [])

  async function turnOn() {
    setBusy(true)
    setErr('')
    const res = await enablePush().catch((e: any) => ({ ok: false, reason: e?.message ?? 'Something went wrong' }))
    if (!res.ok) setErr(res.reason ?? 'Could not turn on notifications')
    setBusy(false)
    refresh()
  }

  async function turnOff() {
    setBusy(true)
    await disablePush()
    setBusy(false)
    refresh()
  }

  return (
    <Card title="Notifications">
      <p className="mb-3 text-sm text-ink-1">
        Get a heads-up for direct messages, @mentions, and incoming calls, even when Talaria is closed.
      </p>
      {state === 'enabled' && (
        <div className="flex items-center gap-3">
          <span className="flex items-center gap-1.5 text-sm text-success">
            <Bell size={15} /> On for this device
          </span>
          <Button variant="ghost" className="px-3 py-1.5 text-xs" onClick={turnOff} disabled={busy}>
            <BellOff size={14} /> Turn off
          </Button>
        </div>
      )}
      {state === 'off' && (
        <Button onClick={turnOn} disabled={busy}>
          <Bell size={16} /> Turn on notifications
        </Button>
      )}
      {state === 'denied' && (
        <p className="text-sm text-warning">
          Notifications are blocked for this site. Allow them in your browser's site settings, then come back here.
        </p>
      )}
      {state === 'ios-needs-install' && (
        <p className="text-sm text-ink-1">
          On iPhone and iPad, notifications only work once Talaria is on your Home Screen: tap <strong>Share</strong>, then{' '}
          <strong>Add to Home Screen</strong>, open Talaria from there, and turn notifications on.
        </p>
      )}
      {state === 'unsupported' && (
        <p className="text-sm text-ink-2">This browser doesn't support notifications.</p>
      )}
      {devices > 0 && (
        <p className="mt-2 text-xs text-ink-2">
          On for {devices} {devices === 1 ? 'device' : 'devices'} on your account.
        </p>
      )}
      <ErrorText>{err}</ErrorText>
      {installable && !isStandalone() && (
        <div className="mt-4 border-t border-edge pt-3">
          <Button variant="ghost" className="px-3 py-1.5 text-sm" onClick={promptInstall}>
            <Download size={15} /> Install Talaria on this device
          </Button>
          <p className="mt-1 text-xs text-ink-2">Gets its own icon and window, like a regular app.</p>
        </div>
      )}
    </Card>
  )
}

function PasswordSection() {
  const [current, setCurrent] = useState('')
  const [next, setNext] = useState('')
  const [msg, setMsg] = useState('')
  const [err, setErr] = useState('')
  return (
    <Card title="Change password">
      <form
        className="space-y-3"
        onSubmit={async (e) => {
          e.preventDefault()
          setErr('')
          setMsg('')
          try {
            await api.patch('/api/v1/users/me', { current_password: current, new_password: next })
            setMsg('Password changed.')
            setCurrent('')
            setNext('')
          } catch (e2: any) {
            setErr(e2?.message ?? 'Could not change password')
          }
        }}
      >
        <div>
          <Label>Current password</Label>
          <Input type="password" value={current} onChange={(e) => setCurrent(e.target.value)} autoComplete="current-password" required />
        </div>
        <div>
          <Label>New password</Label>
          <Input type="password" value={next} onChange={(e) => setNext(e.target.value)} autoComplete="new-password" required minLength={8} />
        </div>
        <div className="flex items-center gap-3">
          <Button type="submit">Change password</Button>
          {msg && <span className="text-sm text-success">{msg}</span>}
        </div>
        <ErrorText>{err}</ErrorText>
      </form>
    </Card>
  )
}

function SessionsSection() {
  const [sessions, setSessions] = useState<SessionInfo[]>([])
  const load = () =>
    api.get<{ sessions: SessionInfo[] }>('/api/v1/auth/sessions').then((r) => setSessions(r.sessions)).catch(() => {})
  useEffect(() => {
    load()
  }, [])
  return (
    <Card title="Devices & sessions">
      <div className="space-y-2">
        {sessions.map((s) => (
          <div key={s.id} className="flex items-center gap-3 rounded-lg bg-bg-2 px-3 py-2 text-sm">
            <div className="min-w-0 flex-1">
              <div className="truncate text-ink">
                {s.user_agent.split(/[()]/)[1] ?? s.user_agent.slice(0, 40) ?? 'Unknown device'}
                {s.current && <span className="ml-2 rounded bg-accent/15 px-1.5 text-xs text-accent">this device</span>}
              </div>
              <div className="text-xs text-ink-2">Last active {formatDateTime(s.last_seen_at)} · {s.ip}</div>
            </div>
            {!s.current && (
              <Button
                variant="ghost"
                className="px-2 py-1 text-xs text-danger"
                onClick={() => api.del(`/api/v1/auth/sessions/${s.id}`).then(load)}
              >
                Revoke
              </Button>
            )}
          </div>
        ))}
      </div>
    </Card>
  )
}

function AdminSection() {
  const me = useApp((s) => s.me)
  const [invites, setInvites] = useState<Invite[]>([])
  const [adminUsers, setAdminUsers] = useState<User[]>([])
  const [stats, setStats] = useState<any>(null)
  const [newInviteCode, setNewInviteCode] = useState('')

  const load = () => {
    api.get<{ invites: Invite[] }>('/api/v1/admin/invites').then((r) => setInvites(r.invites ?? [])).catch(() => {})
    api.get<{ users: User[] }>('/api/v1/admin/users').then((r) => setAdminUsers(r.users ?? [])).catch(() => {})
    api.get('/api/v1/admin/stats').then(setStats).catch(() => {})
  }
  useEffect(load, [])

  return (
    <>
      <Card title="Admin — registration invites">
        <p className="mb-3 text-sm text-ink-1">
          New people need an invite code to create an account on this server.
        </p>
        <Button
          onClick={async () => {
            const r = await api.post<{ invite: Invite }>('/api/v1/admin/invites', {})
            setNewInviteCode(r.invite.code)
            load()
          }}
        >
          Create invite
        </Button>
        {newInviteCode && (
          <div className="mt-3">
            <InviteLinkShare path={`/register?invite=${newInviteCode}`} />
          </div>
        )}
        <div className="mt-3 space-y-1">
          {invites.filter((i) => !i.revoked_at).map((i) => (
            <div
              key={i.code}
              className={cx(
                'flex items-center gap-2 rounded-lg bg-bg-2 px-3 py-1.5 text-sm',
                newInviteCode === i.code && 'ring-1 ring-accent/50',
              )}
            >
              <code className="text-ink">{i.code}</code>
              <span className="text-xs text-ink-2">used {i.use_count}{i.max_uses ? `/${i.max_uses}` : ''}</span>
              <button
                className="ml-auto flex items-center gap-1 text-xs text-accent hover:underline"
                title="Show the shareable link for this code"
                onClick={() => setNewInviteCode(i.code)}
              >
                <Link2 size={13} /> Get link
              </button>
              <button
                className="text-xs text-danger hover:underline"
                onClick={() => api.del(`/api/v1/admin/invites/${i.code}`).then(load)}
              >
                Revoke
              </button>
            </div>
          ))}
        </div>
      </Card>
      <Card title="Admin — members">
        <div className="space-y-1">
          {adminUsers.map((u) => (
            <div key={u.id} className="flex items-center gap-3 rounded bg-bg-2 px-3 py-2 text-sm">
              <Avatar user={u} size={28} />
              <div className="min-w-0 flex-1">
                <span className="text-ink">{u.display_name}</span>
                <span className="ml-1 text-xs text-ink-2">@{u.username}</span>
                {u.is_admin && <span className="ml-2 rounded bg-accent/15 px-1.5 text-xs text-accent">admin</span>}
                {u.disabled_at ? <span className="ml-2 rounded bg-danger/15 px-1.5 text-xs text-danger">disabled</span> : null}
              </div>
              {u.id !== me?.id && (
                <button
                  className="text-xs text-ink-1 hover:text-danger"
                  onClick={() =>
                    api.patch(`/api/v1/admin/users/${u.id}`, { disabled: !u.disabled_at }).then(load)
                  }
                >
                  {u.disabled_at ? 'Enable' : 'Disable'}
                </button>
              )}
            </div>
          ))}
        </div>
      </Card>
      {stats && (
        <Card title="Admin — server stats">
          <div className="grid grid-cols-2 gap-3 text-sm">
            <Stat label="Members" value={stats.users} />
            <Stat label="Messages" value={stats.messages} />
            <Stat label="Files" value={stats.files} />
            <Stat label="Storage" value={formatBytes(stats.blob_bytes ?? 0)} />
          </div>
          <p className="mt-3 text-xs text-ink-2">Talaria {stats.version}</p>
        </Card>
      )}
      <ServerConfigSection />
    </>
  )
}

function SubHead({ children }: { children: React.ReactNode }) {
  return <h3 className="border-b border-edge pb-1 text-sm font-semibold text-ink">{children}</h3>
}

function Toggle({
  checked,
  onChange,
  label,
  hint,
}: {
  checked: boolean
  onChange: (v: boolean) => void
  label: string
  hint?: string
}) {
  return (
    <label className="flex cursor-pointer items-start gap-2">
      <input
        type="checkbox"
        className="mt-0.5 h-4 w-4 accent-[var(--color-accent)]"
        checked={checked}
        onChange={(e) => onChange(e.target.checked)}
      />
      <span className="text-sm text-ink">
        {label}
        {hint && <span className="block text-xs text-ink-2">{hint}</span>}
      </span>
    </label>
  )
}

// ServerConfigSection lets the admin manage talaria.toml from the browser —
// no hand-editing required.
function ServerConfigSection() {
  const [cfg, setCfg] = useState<ServerConfig | null>(null)
  const [configPath, setConfigPath] = useState('')
  const [lanIP, setLanIP] = useState('')
  const [lanStatus, setLanStatus] = useState<LanAccessStatus | null>(null)
  const [restartItems, setRestartItems] = useState<string[]>([])
  const [warnings, setWarnings] = useState<string[]>([])
  const [msg, setMsg] = useState('')
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    api
      .get<{ config: ServerConfig; config_path: string; lan_ip: string; lanaccess_status: LanAccessStatus }>(
        '/api/v1/admin/config',
      )
      .then((r) => {
        setCfg(r.config)
        setConfigPath(r.config_path)
        setLanIP(r.lan_ip)
        setLanStatus(r.lanaccess_status)
      })
      .catch((e: any) => setErr(e?.message ?? 'Could not load server configuration'))
  }, [])

  if (!cfg) return err ? <Card title="Admin — server configuration"><ErrorText>{err}</ErrorText></Card> : null

  // Immutable nested update helper: patch one section of the config.
  const patch = <K extends keyof ServerConfig>(section: K, values: Partial<ServerConfig[K]>) =>
    setCfg({ ...cfg, [section]: { ...cfg[section], ...values } })

  const num = (v: string) => (v === '' ? 0 : Number(v))

  async function save() {
    if (!cfg) return
    setBusy(true)
    setErr('')
    setMsg('')
    setRestartItems([])
    setWarnings([])
    try {
      const r = await api.put<{
        config: ServerConfig
        restart_required: string[]
        warnings: string[]
        lanaccess_status: LanAccessStatus
      }>('/api/v1/admin/config', cfg)
      setCfg(r.config)
      setRestartItems(r.restart_required ?? [])
      setWarnings(r.warnings ?? [])
      setLanStatus(r.lanaccess_status)
      setMsg('Saved.')
    } catch (e: any) {
      setErr(e?.message ?? 'Could not save')
    } finally {
      setBusy(false)
    }
  }

  return (
    <Card title="Admin — server configuration">
      <div className="space-y-5">
        <div className="space-y-3">
          <SubHead>General</SubHead>
          <div>
            <Label>Server name</Label>
            <Input value={cfg.server.name} onChange={(e) => patch('server', { name: e.target.value })} maxLength={64} />
          </div>
          <div className="grid grid-cols-2 gap-3">
            <div>
              <Label>HTTP port</Label>
              <Input type="number" value={cfg.server.http_port || ''} onChange={(e) => patch('server', { http_port: num(e.target.value) })} />
            </div>
            <div>
              <Label>Max upload (MB)</Label>
              <Input type="number" value={cfg.server.max_upload_mb || ''} onChange={(e) => patch('server', { max_upload_mb: num(e.target.value) })} />
            </div>
          </div>
          <Toggle
            checked={cfg.server.open_browser}
            onChange={(v) => patch('server', { open_browser: v })}
            label="Open browser on first run"
          />
        </div>

        <div className="space-y-3">
          <SubHead>Remote access — HTTPS</SubHead>
          <div>
            <Label>Mode</Label>
            <Select value={cfg.tls.mode} onChange={(e) => patch('tls', { mode: e.target.value as ServerConfig['tls']['mode'] })}>
              <option value="auto">Auto — HTTPS when a domain is set, else HTTP only</option>
              <option value="acme">Let's Encrypt (requires domain + ports 80/443)</option>
              <option value="selfsigned">Self-signed (LAN HTTPS, browser warning)</option>
              <option value="off">Off — plain HTTP only</option>
            </Select>
          </div>
          <div>
            <Label>Domain</Label>
            <Input
              value={cfg.tls.domain}
              onChange={(e) => patch('tls', { domain: e.target.value })}
              placeholder="yourname.duckdns.org"
            />
          </div>
          <div className="grid grid-cols-2 gap-3">
            <div>
              <Label>HTTPS port</Label>
              <Input type="number" value={cfg.tls.https_port || ''} onChange={(e) => patch('tls', { https_port: num(e.target.value) })} />
            </div>
            <div>
              <Label>ACME email (optional)</Label>
              <Input type="email" value={cfg.tls.acme_email} onChange={(e) => patch('tls', { acme_email: e.target.value })} placeholder="you@example.com" />
            </div>
          </div>
        </div>

        <div className="space-y-3">
          <SubHead>Dynamic DNS</SubHead>
          <Toggle
            checked={cfg.ddns.enabled}
            onChange={(v) => patch('ddns', { enabled: v })}
            label="Keep my domain pointed at this connection"
            hint="Talaria updates the DNS record itself — no separate updater needed. Applies immediately on save."
          />
          {cfg.ddns.enabled && (
            <>
              <div>
                <Label>Provider</Label>
                <Select value={cfg.ddns.provider} onChange={(e) => patch('ddns', { provider: e.target.value as ServerConfig['ddns']['provider'] })}>
                  <option value="duckdns">DuckDNS (free — duckdns.org)</option>
                  <option value="custom">Custom update URL</option>
                </Select>
              </div>
              {cfg.ddns.provider === 'duckdns' ? (
                <div>
                  <Label>DuckDNS token</Label>
                  <Input
                    value={cfg.ddns.token}
                    onChange={(e) => patch('ddns', { token: e.target.value })}
                    placeholder="shown at the top of duckdns.org"
                  />
                </div>
              ) : (
                <div>
                  <Label>Update URL</Label>
                  <Input
                    value={cfg.ddns.url}
                    onChange={(e) => patch('ddns', { url: e.target.value })}
                    placeholder="https://provider/update?host={DOMAIN}&pass={TOKEN}"
                  />
                </div>
              )}
              <div className="grid grid-cols-2 gap-3">
                <div>
                  <Label>DDNS domain (optional)</Label>
                  <Input
                    value={cfg.ddns.domain}
                    onChange={(e) => patch('ddns', { domain: e.target.value })}
                    placeholder={cfg.tls.domain || 'defaults to HTTPS domain'}
                  />
                </div>
                <div>
                  <Label>Update every (min)</Label>
                  <Input type="number" value={cfg.ddns.interval_minutes || ''} onChange={(e) => patch('ddns', { interval_minutes: num(e.target.value) })} />
                </div>
              </div>
            </>
          )}
        </div>

        <div className="space-y-3">
          <SubHead>Home network access</SubHead>
          <Toggle
            checked={cfg.lan_access.enabled}
            onChange={(v) => patch('lan_access', { enabled: v })}
            label="Same URL at home — route WiFi devices around the router"
            hint="Fixes routers that break the main domain inside your own network. No router or device changes needed; applies immediately on save."
          />
          {cfg.lan_access.enabled && (
            <>
              <div>
                <Label>Home domain</Label>
                <Input
                  value={cfg.lan_access.domain}
                  onChange={(e) => patch('lan_access', { domain: e.target.value })}
                  placeholder="yourname-lan.duckdns.org"
                />
                <p className="mt-1 text-xs text-ink-2">
                  Create this as a second free subdomain at duckdns.org (any IP — Talaria takes it
                  over, pointing it at this machine{lanIP ? ` (${lanIP})` : ''} and fetching its
                  certificate automatically). Uses your DuckDNS token from above.
                </p>
              </div>
              {lanStatus?.active && lanStatus.cert_ok ? (
                <p className="rounded-lg bg-success/10 px-3 py-2 text-sm text-success">
                  Active — devices on your WiFi are transparently redirected to{' '}
                  <strong>{lanStatus.domain}</strong> ({lanStatus.lan_ip}) with a valid certificate
                  {lanStatus.cert_expires ? `, renews before ${new Date(lanStatus.cert_expires).toLocaleDateString()}` : ''}.
                </p>
              ) : lanStatus?.active ? (
                <p className="rounded-lg bg-warning/10 px-3 py-2 text-sm text-warning">
                  Setting up — fetching the Let's Encrypt certificate (takes about a minute;
                  reload this page to refresh). {lanStatus.error && `Last error: ${lanStatus.error}`}
                </p>
              ) : lanStatus?.error ? (
                <p className="rounded-lg bg-danger/10 px-3 py-2 text-sm text-danger">
                  Not running: {lanStatus.error}
                </p>
              ) : null}
            </>
          )}
        </div>

        <div className="space-y-3">
          <SubHead>Listen together</SubHead>
          <div>
            <Label>YouTube API key (optional)</Label>
            <Input
              value={cfg.youtube.api_key}
              onChange={(e) => patch('youtube', { api_key: e.target.value })}
              placeholder="Search works without one"
            />
            <p className="mt-1 text-xs text-ink-2">
              Search reads YouTube's public results page by default. A free Data API key from Google Cloud
              switches to the official API if that ever stops working.
            </p>
          </div>
        </div>

        <div className="space-y-3">
          <SubHead>Calls (WebRTC)</SubHead>
          <div className="grid grid-cols-2 gap-3">
            <div>
              <Label>Media UDP port</Label>
              <Input type="number" value={cfg.webrtc.udp_port || ''} onChange={(e) => patch('webrtc', { udp_port: num(e.target.value) })} />
            </div>
            <div>
              <Label>Public IP override</Label>
              <Input value={cfg.webrtc.public_ip} onChange={(e) => patch('webrtc', { public_ip: e.target.value })} placeholder="auto-detected" />
            </div>
          </div>
          <p className="text-xs text-ink-2">
            Forward this UDP port on your router so remote friends can join calls.
          </p>
        </div>

        <div className="space-y-2">
          <div className="flex items-center gap-3">
            <Button onClick={save} disabled={busy}>Save configuration</Button>
            {msg && <span className="text-sm text-success">{msg}</span>}
          </div>
          {restartItems.length > 0 && (
            <p className="rounded-lg bg-warning/10 px-3 py-2 text-sm text-warning">
              Restart Talaria to apply: {restartItems.join(', ')}.
            </p>
          )}
          {warnings.map((w) => (
            <p key={w} className="rounded-lg bg-warning/10 px-3 py-2 text-sm text-warning">
              {w}
            </p>
          ))}
          <ErrorText>{err}</ErrorText>
          {configPath && <p className="text-xs text-ink-2">Stored in {configPath}</p>}
        </div>
      </div>
    </Card>
  )
}

function Stat({ label, value }: { label: string; value: React.ReactNode }) {
  return (
    <div className="rounded-lg bg-bg-2 p-3">
      <div className="text-xs text-ink-2">{label}</div>
      <div className="text-lg font-semibold text-ink">{value}</div>
    </div>
  )
}

import { useEffect, useState } from 'react'
import { Moon, Monitor, Sun } from 'lucide-react'
import { api, uploadFile } from '../lib/api'
import { useApp, type Theme } from '../store'
import { Avatar } from '../components/Avatar'
import { InviteLinkShare } from '../components/InviteLink'
import { Button, cx, ErrorText, Input, Label } from '../components/ui'
import { PageHeader } from './AppShell'
import type { Invite, SessionInfo, User } from '../lib/types'
import { formatDateTime, formatBytes } from '../lib/time'

export default function SettingsPage({ onMenu }: { onMenu: () => void }) {
  const me = useApp((s) => s.me)
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <PageHeader onMenu={onMenu} title="Settings" />
      <div className="flex-1 space-y-8 overflow-y-auto p-4 pb-16">
        <ProfileSection />
        <AppearanceSection />
        <PasswordSection />
        <SessionsSection />
        {me?.is_admin && <AdminSection />}
      </div>
    </div>
  )
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

  async function changeAvatar(file: File) {
    setBusy(true)
    setErr('')
    try {
      const res = await uploadFile(file)
      await save({ avatar_file_id: res.attachment.file_id })
    } catch (e: any) {
      setErr(e?.message ?? 'Avatar upload failed')
      setBusy(false)
    }
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
              onChange={(e) => e.target.files?.[0] && changeAvatar(e.target.files[0])}
            />
          </label>
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
  const options: { value: Theme; label: string; icon: React.ReactNode }[] = [
    { value: 'light', label: 'Light', icon: <Sun size={16} /> },
    { value: 'dark', label: 'Dark', icon: <Moon size={16} /> },
    { value: 'system', label: 'System', icon: <Monitor size={16} /> },
  ]
  return (
    <Card title="Appearance">
      <div className="flex gap-2">
        {options.map((o) => (
          <button
            key={o.value}
            onClick={() => setTheme(o.value)}
            className={cx(
              'flex items-center gap-2 rounded-lg border px-3 py-2 text-sm',
              theme === o.value
                ? 'border-accent bg-accent/10 text-ink'
                : 'border-edge text-ink-1 hover:bg-bg-3',
            )}
          >
            {o.icon} {o.label}
          </button>
        ))}
      </div>
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
            <div key={i.code} className="flex items-center gap-2 rounded bg-bg-2 px-3 py-1.5 text-sm">
              <code className="text-ink">{i.code}</code>
              <span className="text-xs text-ink-2">used {i.use_count}{i.max_uses ? `/${i.max_uses}` : ''}</span>
              <button
                className="ml-auto text-xs text-danger hover:underline"
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
          <p className="mt-3 text-xs text-ink-2">Lantern {stats.version}</p>
        </Card>
      )}
    </>
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

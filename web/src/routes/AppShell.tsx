import { useEffect, useMemo, useState } from 'react'
import { Link, Navigate, Route, Routes, useLocation, useNavigate, useParams } from 'react-router'
import { Hash, Menu, Phone, PhoneOff, Plus, Settings, Users, UserPlus, Volume2, X, LogOut } from 'lucide-react'
import { api } from '../lib/api'
import { dmOther, dmTitle, displayName, useApp } from '../store'
import { useCall } from '../call'
import { Avatar } from '../components/Avatar'
import { InviteLinkShare } from '../components/InviteLink'
import { Button, cx, ErrorText, Input, Label, Modal } from '../components/ui'
import FriendsPage from './FriendsPage'
import ChatPage from './ChatPage'
import SettingsPage from './SettingsPage'
import type { Group } from '../lib/types'

export default function AppShell() {
  const ready = useApp((s) => s.ready)
  const gatewayStatus = useApp((s) => s.gatewayStatus)
  const [leftOpen, setLeftOpen] = useState(false)
  const location = useLocation()

  // Close the drawer on navigation (mobile).
  useEffect(() => setLeftOpen(false), [location.pathname])

  return (
    <div className="flex h-dvh overflow-hidden bg-bg-0">
      {/* Left drawer (mobile) / fixed columns (md+) */}
      <div
        className={cx(
          'fixed inset-y-0 left-0 z-40 flex -translate-x-full transition-transform md:static md:translate-x-0',
          leftOpen && 'translate-x-0',
        )}
      >
        <GroupRail />
        <ChannelSidebar />
      </div>
      {leftOpen && (
        <div className="fixed inset-0 z-30 bg-black/40 md:hidden" onClick={() => setLeftOpen(false)} />
      )}

      <main className="flex min-w-0 flex-1 flex-col">
        {gatewayStatus !== 'online' && ready && (
          <div className="bg-danger/90 px-3 py-1 text-center text-xs font-medium text-white">
            {gatewayStatus === 'connecting' ? 'Reconnecting…' : 'Offline — trying to reconnect'}
          </div>
        )}
        <RingToast />
        <Routes>
          <Route index element={<Navigate to="friends" replace />} />
          <Route path="friends" element={<FriendsPage onMenu={() => setLeftOpen(true)} />} />
          <Route path="dm/:channelId" element={<ChatPage onMenu={() => setLeftOpen(true)} />} />
          <Route path="g/:groupId/:channelId" element={<ChatPage onMenu={() => setLeftOpen(true)} />} />
          <Route path="g/:groupId" element={<GroupRedirect />} />
          <Route path="settings/*" element={<SettingsPage onMenu={() => setLeftOpen(true)} />} />
          <Route path="*" element={<Navigate to="friends" replace />} />
        </Routes>
      </main>
    </div>
  )
}

function GroupRedirect() {
  const { groupId = '' } = useParams()
  const chIds = useApp((s) => s.groupChannelIds[groupId])
  if (chIds?.length) return <Navigate to={`/app/g/${groupId}/${chIds[0]}`} replace />
  return <Navigate to="/app/friends" replace />
}

// RingToast: an incoming DM call invitation.
function RingToast() {
  const ring = useCall((s) => s.ring)
  const join = useCall((s) => s.join)
  const dismissRing = useCall((s) => s.dismissRing)
  const users = useApp((s) => s.users)
  const navigate = useNavigate()
  if (!ring) return null
  return (
    <div className="flex items-center gap-3 border-b border-edge bg-accent/10 px-4 py-2">
      <Phone size={18} className="animate-pulse text-accent" />
      <span className="min-w-0 flex-1 truncate text-sm text-ink">
        <strong>{displayName(users, ring.fromUserId)}</strong> is calling you
      </span>
      <Button
        className="px-3 py-1.5 text-xs"
        onClick={() => {
          const chId = ring.channelId
          dismissRing()
          navigate(`/app/dm/${chId}`)
          join(chId)
        }}
      >
        <Phone size={14} /> Join
      </Button>
      <Button variant="ghost" className="px-2 py-1.5 text-xs" onClick={dismissRing}>
        <PhoneOff size={14} />
      </Button>
    </div>
  )
}

function GroupRail() {
  const groups = useApp((s) => s.groups)
  const groupChannelIds = useApp((s) => s.groupChannelIds)
  const latestIds = useApp((s) => s.latestIds)
  const readStates = useApp((s) => s.readStates)
  const { groupId } = useParams()
  const location = useLocation()
  const navigate = useNavigate()
  const [createOpen, setCreateOpen] = useState(false)

  const ordered = useMemo(
    () => Object.values(groups).sort((a, b) => a.created_at - b.created_at),
    [groups],
  )

  const activeGroupId = location.pathname.match(/\/app\/g\/([^/]+)/)?.[1] ?? groupId

  function groupUnread(g: Group): boolean {
    return (groupChannelIds[g.id] ?? []).some((cid) => {
      const latest = latestIds[cid]
      return latest && (!readStates[cid] || readStates[cid] < latest)
    })
  }

  return (
    <nav className="flex h-full w-[72px] flex-col items-center gap-2 overflow-y-auto bg-bg-2 py-3">
      <RailButton
        title="Friends & DMs"
        active={!activeGroupId}
        onClick={() => navigate('/app/friends')}
      >
        <Users size={22} />
      </RailButton>
      <div className="h-px w-8 bg-edge" />
      {ordered.map((g) => (
        <RailButton
          key={g.id}
          title={g.name}
          active={activeGroupId === g.id}
          onClick={() => navigate(`/app/g/${g.id}`)}
          dot={groupUnread(g)}
        >
          {g.icon_file_id ? (
            <img src={`/avatars/${g.icon_file_id}?s=64`} alt="" className="h-full w-full rounded-[inherit] object-cover" />
          ) : (
            <span className="text-sm font-bold">{g.name.slice(0, 2).toUpperCase()}</span>
          )}
        </RailButton>
      ))}
      <RailButton title="Create a group" onClick={() => setCreateOpen(true)}>
        <Plus size={22} />
      </RailButton>
      <CreateGroupModal open={createOpen} onClose={() => setCreateOpen(false)} />
    </nav>
  )
}

function RailButton({
  children,
  title,
  active,
  dot,
  onClick,
}: {
  children: React.ReactNode
  title: string
  active?: boolean
  dot?: boolean
  onClick: () => void
}) {
  return (
    <div className="relative">
      {dot && !active && (
        <span className="absolute -left-2 top-1/2 h-2 w-2 -translate-y-1/2 rounded-full bg-ink" />
      )}
      <button
        title={title}
        onClick={onClick}
        className={cx(
          'flex h-12 w-12 items-center justify-center overflow-hidden transition-all',
          active
            ? 'rounded-xl bg-accent text-accent-text'
            : 'rounded-3xl bg-bg-1 text-ink-1 hover:rounded-xl hover:bg-accent hover:text-accent-text',
        )}
      >
        {children}
      </button>
    </div>
  )
}

function CreateGroupModal({ open, onClose }: { open: boolean; onClose: () => void }) {
  const [name, setName] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const navigate = useNavigate()

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    setBusy(true)
    setError('')
    try {
      const res = await api.post<{ group: Group; channels: { id: string }[] }>('/api/v1/groups', { name })
      onClose()
      setName('')
      navigate(`/app/g/${res.group.id}/${res.channels[0].id}`)
    } catch (err: any) {
      setError(err?.message ?? 'Could not create group')
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal open={open} onClose={onClose} title="Create a group">
      <form className="space-y-4" onSubmit={submit}>
        <div>
          <Label>Group name</Label>
          <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="The Lounge" autoFocus required />
        </div>
        <ErrorText>{error}</ErrorText>
        <Button type="submit" className="w-full" disabled={busy}>
          {busy ? 'Creating…' : 'Create group'}
        </Button>
      </form>
    </Modal>
  )
}

function ChannelSidebar() {
  const location = useLocation()
  const activeGroupId = location.pathname.match(/\/app\/g\/([^/]+)/)?.[1]
  return (
    <div className="flex h-full w-60 flex-col bg-bg-1 border-r border-edge">
      {activeGroupId ? <GroupChannelList groupId={activeGroupId} /> : <DMList />}
      <SelfBar />
    </div>
  )
}

function GroupChannelList({ groupId }: { groupId: string }) {
  const group = useApp((s) => s.groups[groupId])
  const channelIds = useApp((s) => s.groupChannelIds[groupId])
  const channels = useApp((s) => s.channels)
  const me = useApp((s) => s.me)
  const members = useApp((s) => s.groupMembers[groupId])
  const latestIds = useApp((s) => s.latestIds)
  const readStates = useApp((s) => s.readStates)
  const calls = useCall((s) => s.calls)
  const { channelId } = useParams()
  const navigate = useNavigate()
  const [inviteOpen, setInviteOpen] = useState(false)
  const [channelOpen, setChannelOpen] = useState(false)

  if (!group) return <div className="flex-1" />
  const myRole = members?.find((m) => m.user_id === me?.id)?.role
  const canManage = myRole === 'owner' || myRole === 'admin'

  return (
    <>
      <div className="flex items-center justify-between border-b border-edge px-4 py-3">
        <h2 className="truncate font-semibold text-ink">{group.name}</h2>
        <button
          title="Invite people"
          className="text-ink-1 hover:text-ink"
          onClick={() => setInviteOpen(true)}
        >
          <UserPlus size={18} />
        </button>
      </div>
      <div className="flex-1 space-y-0.5 overflow-y-auto p-2">
        {(channelIds ?? []).map((cid) => {
          const c = channels[cid]
          if (!c) return null
          const latest = latestIds[cid]
          const unread = latest && (!readStates[cid] || readStates[cid] < latest)
          const inCall = (calls[cid]?.length ?? 0) > 0
          return (
            <button
              key={cid}
              onClick={() => navigate(`/app/g/${groupId}/${cid}`)}
              className={cx(
                'flex w-full items-center gap-2 rounded-lg px-2 py-1.5 text-left text-sm',
                channelId === cid ? 'bg-bg-3 text-ink' : 'text-ink-1 hover:bg-bg-3 hover:text-ink',
                unread && channelId !== cid && 'font-semibold text-ink',
              )}
            >
              <Hash size={16} className="shrink-0 opacity-60" />
              <span className="truncate">{c.name}</span>
              <span className="ml-auto flex items-center gap-1.5">
                {inCall && <Volume2 size={14} className="text-success" />}
                {unread && channelId !== cid && <span className="h-2 w-2 rounded-full bg-accent" />}
              </span>
            </button>
          )
        })}
        {canManage && (
          <button
            onClick={() => setChannelOpen(true)}
            className="flex w-full items-center gap-2 rounded-lg px-2 py-1.5 text-sm text-ink-2 hover:bg-bg-3 hover:text-ink"
          >
            <Plus size={16} /> Add channel
          </button>
        )}
      </div>
      <InviteModal open={inviteOpen} onClose={() => setInviteOpen(false)} groupId={groupId} />
      <CreateChannelModal open={channelOpen} onClose={() => setChannelOpen(false)} groupId={groupId} />
    </>
  )
}

function InviteModal({ open, onClose, groupId }: { open: boolean; onClose: () => void; groupId: string }) {
  const [code, setCode] = useState('')
  const [error, setError] = useState('')

  useEffect(() => {
    if (!open) return
    setCode('')
    setError('')
    api
      .post<{ invite: { code: string } }>(`/api/v1/groups/${groupId}/invites`, { expires_in_s: 7 * 24 * 3600 })
      .then((r) => setCode(r.invite.code))
      .catch((e) => setError(e?.message ?? 'Could not create invite'))
  }, [open, groupId])

  return (
    <Modal open={open} onClose={onClose} title="Invite people">
      <p className="mb-3 text-sm text-ink-1">
        Share this link — it works for existing members and brand-new users alike, and expires in 7 days.
        Pick the address your friend can reach.
      </p>
      <ErrorText>{error}</ErrorText>
      {code && <InviteLinkShare path={`/invite/${code}`} />}
    </Modal>
  )
}

function CreateChannelModal({ open, onClose, groupId }: { open: boolean; onClose: () => void; groupId: string }) {
  const [name, setName] = useState('')
  const [error, setError] = useState('')
  const navigate = useNavigate()

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    try {
      const res = await api.post<{ channel: { id: string } }>(`/api/v1/groups/${groupId}/channels`, {
        name,
        topic: '',
      })
      setName('')
      onClose()
      navigate(`/app/g/${groupId}/${res.channel.id}`)
    } catch (err: any) {
      setError(err?.message ?? 'Could not create channel')
    }
  }

  return (
    <Modal open={open} onClose={onClose} title="Add a channel">
      <form className="space-y-4" onSubmit={submit}>
        <div>
          <Label>Channel name</Label>
          <Input value={name} onChange={(e) => setName(e.target.value.toLowerCase().replace(/\s+/g, '-'))} placeholder="music" autoFocus required />
        </div>
        <ErrorText>{error}</ErrorText>
        <Button type="submit" className="w-full">Create channel</Button>
      </form>
    </Modal>
  )
}

function DMList() {
  const dmChannelIds = useApp((s) => s.dmChannelIds)
  const channels = useApp((s) => s.channels)
  const users = useApp((s) => s.users)
  const online = useApp((s) => s.online)
  const me = useApp((s) => s.me)
  const latestIds = useApp((s) => s.latestIds)
  const readStates = useApp((s) => s.readStates)
  const calls = useCall((s) => s.calls)
  const { channelId } = useParams()
  const navigate = useNavigate()
  const location = useLocation()

  const sorted = useMemo(
    () =>
      [...dmChannelIds].sort((a, b) => {
        const la = latestIds[a] ?? ''
        const lb = latestIds[b] ?? ''
        return la > lb ? -1 : 1
      }),
    [dmChannelIds, latestIds],
  )

  return (
    <>
      <div className="border-b border-edge px-4 py-3">
        <h2 className="font-semibold text-ink">Lantern</h2>
      </div>
      <div className="flex-1 space-y-0.5 overflow-y-auto p-2">
        <button
          onClick={() => navigate('/app/friends')}
          className={cx(
            'flex w-full items-center gap-2 rounded-lg px-2 py-2 text-left text-sm',
            location.pathname === '/app/friends' ? 'bg-bg-3 text-ink' : 'text-ink-1 hover:bg-bg-3 hover:text-ink',
          )}
        >
          <Users size={18} /> Friends
        </button>
        <div className="px-2 pb-1 pt-3 text-xs font-semibold uppercase tracking-wide text-ink-2">
          Direct messages
        </div>
        {sorted.map((cid) => {
          const c = channels[cid]
          if (!c || !me) return null
          const other = dmOther(c, me.id)
          const otherUser = other ? users[other.id] ?? other : undefined
          const latest = latestIds[cid]
          const unread = latest && (!readStates[cid] || readStates[cid] < latest)
          return (
            <button
              key={cid}
              onClick={() => navigate(`/app/dm/${cid}`)}
              className={cx(
                'flex w-full items-center gap-2 rounded-lg px-2 py-1.5 text-left text-sm',
                channelId === cid ? 'bg-bg-3 text-ink' : 'text-ink-1 hover:bg-bg-3 hover:text-ink',
                unread && channelId !== cid && 'font-semibold text-ink',
              )}
            >
              <Avatar user={otherUser} size={28} online={otherUser ? !!online[otherUser.id] : undefined} />
              <span className="truncate">{dmTitle(c, me.id, users)}</span>
              <span className="ml-auto flex items-center gap-1.5">
                {(calls[cid]?.length ?? 0) > 0 && <Volume2 size={14} className="text-success" />}
                {unread && channelId !== cid && <span className="h-2 w-2 rounded-full bg-accent" />}
              </span>
            </button>
          )
        })}
        {sorted.length === 0 && (
          <p className="px-2 py-1 text-xs text-ink-2">Message a friend to start a DM.</p>
        )}
      </div>
    </>
  )
}

function SelfBar() {
  const me = useApp((s) => s.me)
  const setMe = useApp((s) => s.setMe)
  const navigate = useNavigate()
  if (!me) return null
  return (
    <div className="flex items-center gap-2 border-t border-edge bg-bg-2 p-2">
      <Avatar user={me} size={32} online />
      <div className="min-w-0 flex-1">
        <div className="truncate text-sm font-medium text-ink">{me.display_name}</div>
        <div className="truncate text-xs text-ink-2">{me.status_text || `@${me.username}`}</div>
      </div>
      <Link to="/app/settings" title="Settings" className="rounded p-1.5 text-ink-1 hover:bg-bg-3 hover:text-ink">
        <Settings size={18} />
      </Link>
      <button
        title="Sign out"
        className="rounded p-1.5 text-ink-1 hover:bg-bg-3 hover:text-danger"
        onClick={async () => {
          await api.post('/api/v1/auth/logout').catch(() => {})
          setMe(null)
          navigate('/login', { replace: true })
        }}
      >
        <LogOut size={18} />
      </button>
    </div>
  )
}

// Shared page header with the mobile hamburger.
export function PageHeader({
  onMenu,
  icon,
  title,
  subtitle,
  right,
}: {
  onMenu: () => void
  icon?: React.ReactNode
  title: string
  subtitle?: string
  right?: React.ReactNode
}) {
  return (
    <header className="flex h-12 shrink-0 items-center gap-2 border-b border-edge bg-bg-1 px-3">
      <button className="rounded p-1.5 text-ink-1 hover:bg-bg-3 md:hidden" onClick={onMenu}>
        <Menu size={20} />
      </button>
      {icon && <span className="text-ink-2">{icon}</span>}
      <h1 className="truncate font-semibold text-ink">{title}</h1>
      {subtitle && <span className="hidden truncate text-sm text-ink-2 sm:block">— {subtitle}</span>}
      <div className="ml-auto flex items-center gap-1">{right}</div>
    </header>
  )
}

export { X }

import { useEffect, useMemo, useRef, useState } from 'react'
import { useNavigate, useParams, useSearchParams } from 'react-router'
import { Bell, BellOff, Hash, Headphones, LogOut, Pencil, Phone, Search, UserPlus, Users } from 'lucide-react'
import { api } from '../lib/api'
import { dmTitle, displayName, useApp, isNotesChat } from '../store'
import { useCall } from '../call'
import { PageHeader } from './AppShell'
import { MessageList } from '../components/chat/MessageList'
import { Composer } from '../components/chat/Composer'
import { CallPanel } from '../components/call/CallPanel'
import { ListenPanel, StartListenModal } from '../components/listen/ListenPanel'
import { useListen } from '../listen'
import { Avatar } from '../components/Avatar'
import { Button, cx, ErrorText, Input, Label, Modal } from '../components/ui'
import type { Message } from '../lib/types'

export default function ChatPage({ onMenu }: { onMenu: () => void }) {
  const { channelId = '', groupId } = useParams()
  const channel = useApp((s) => s.channels[channelId])
  const users = useApp((s) => s.users)
  const me = useApp((s) => s.me)
  const loadMessages = useApp((s) => s.loadMessages)
  const jumpToMessage = useApp((s) => s.jumpToMessage)
  const openSearch = useApp((s) => s.openSearch)
  const markRead = useApp((s) => s.markRead)
  const [searchParams, setSearchParams] = useSearchParams()
  const jumpTo = searchParams.get('m')
  const latest = useApp((s) => s.latestIds[channelId])
  const [membersOpen, setMembersOpen] = useState(false)
  const [replyTo, setReplyTo] = useState<Message | null>(null)
  const [addOpen, setAddOpen] = useState(false)
  const [chatSettingsOpen, setChatSettingsOpen] = useState(false)

  useEffect(() => {
    setReplyTo(null)
    if (!channelId) return
    if (jumpTo) {
      // Arriving from search: land on the message, then drop the param.
      jumpToMessage(channelId, jumpTo).catch(() => loadMessages(channelId).catch(() => {}))
      setSearchParams({}, { replace: true })
    } else {
      loadMessages(channelId).catch(() => {})
    }
  }, [channelId, jumpTo, loadMessages, jumpToMessage, setSearchParams])

  // Mark read when viewing and new messages arrive while visible.
  useEffect(() => {
    if (!channelId || !latest) return
    const mark = () => document.visibilityState === 'visible' && markRead(channelId)
    mark()
    document.addEventListener('visibilitychange', mark)
    return () => document.removeEventListener('visibilitychange', mark)
  }, [channelId, latest, markRead])

  if (!channel || !me) {
    return (
      <div className="flex flex-1 items-center justify-center text-ink-2">
        <PageHeaderPlaceholder onMenu={onMenu} />
        Channel not found
      </div>
    )
  }

  const isDM = channel.kind === 'dm'
  const title = isDM ? dmTitle(channel, me.id, users) : `${channel.name}`
  const isGroupChat = isDM && ((channel.participants?.length ?? 0) > 2 || !!channel.name)
  const isNotes = isDM && isNotesChat(channel, me.id)

  return (
    <div className="flex min-h-0 flex-1">
      <div className="flex min-h-0 min-w-0 flex-1 flex-col">
        <PageHeader
          onMenu={onMenu}
          icon={isDM ? undefined : <Hash size={18} />}
          title={title}
          subtitle={isNotes ? 'Only you can see this. Handy for notes and files you want to keep.' : channel.topic || undefined}
          right={
            <>
              <button
                className="rounded p-1.5 text-ink-1 hover:bg-bg-3 hover:text-ink"
                title="Search this chat"
                onClick={() => openSearch({ scope: 'channel', id: channelId })}
              >
                <Search size={18} />
              </button>
              {!isNotes && <StartCallButton channelId={channelId} />}
              <ListenButton channelId={channelId} />
              {!isNotes && <MuteButton channelId={channelId} />}
              {isDM && !isNotes && (
                <button
                  className="rounded p-1.5 text-ink-1 hover:bg-bg-3 hover:text-ink"
                  title="Add people to this chat"
                  onClick={() => setAddOpen(true)}
                >
                  <UserPlus size={18} />
                </button>
              )}
              {isGroupChat && (
                <button
                  className="rounded p-1.5 text-ink-1 hover:bg-bg-3 hover:text-ink"
                  title="Chat settings"
                  onClick={() => setChatSettingsOpen(true)}
                >
                  <Pencil size={18} />
                </button>
              )}
              {!isDM && (
                <button
                  className="rounded p-1.5 text-ink-1 hover:bg-bg-3 hover:text-ink"
                  title="Members"
                  onClick={() => setMembersOpen((v) => !v)}
                >
                  <Users size={18} />
                </button>
              )}
            </>
          }
        />
        <CallPanel channelId={channelId} />
        <ListenPanel channelId={channelId} />
        <MessageList channelId={channelId} onReply={setReplyTo} />
        <TypingLine channelId={channelId} />
        <Composer channelId={channelId} replyTo={replyTo} onClearReply={() => setReplyTo(null)} />
      </div>
      {!isDM && groupId && <MemberPanel groupId={groupId} open={membersOpen} onClose={() => setMembersOpen(false)} />}
      {isDM && !isNotes && <AddPeopleModal open={addOpen} onClose={() => setAddOpen(false)} channelId={channelId} />}
      {isGroupChat && (
        <ChatSettingsModal open={chatSettingsOpen} onClose={() => setChatSettingsOpen(false)} channelId={channelId} />
      )}
    </div>
  )
}

// AddPeopleModal: pick friends to bring into this chat. Adding to a 1:1
// starts a new chat (the private history stays private), so we navigate.
function AddPeopleModal({ open, onClose, channelId }: { open: boolean; onClose: () => void; channelId: string }) {
  const channel = useApp((s) => s.channels[channelId])
  const friends = useApp((s) => s.friends)
  const users = useApp((s) => s.users)
  const navigate = useNavigate()
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  const participantIds = new Set((channel?.participants ?? []).map((p) => p.id))
  const candidates = friends
    .filter((f) => f.friendship.status === 'accepted' && !participantIds.has(f.user_id))
    .sort((a, b) => displayName(users, a.user_id).localeCompare(displayName(users, b.user_id)))

  async function add(userId: string) {
    setBusy(true)
    setError('')
    try {
      const res = await api.post<{ channel: { id: string } }>(
        `/api/v1/channels/${channelId}/participants`,
        { user_id: userId },
      )
      onClose()
      if (res.channel.id !== channelId) navigate(`/app/dm/${res.channel.id}`)
    } catch (e: any) {
      setError(e?.message ?? 'Could not add them')
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal open={open} onClose={onClose} title="Add people">
      {participantIds.size === 2 && (
        <p className="mb-3 text-sm text-ink-1">
          This starts a new chat with everyone — your direct messages stay just between you two.
        </p>
      )}
      <div className="max-h-72 space-y-1 overflow-y-auto">
        {candidates.length === 0 && (
          <p className="text-sm text-ink-2">All of your friends are already here.</p>
        )}
        {candidates.map((f) => {
          const u = users[f.user_id]
          return (
            <button
              key={f.user_id}
              disabled={busy}
              onClick={() => add(f.user_id)}
              className="flex w-full items-center gap-3 rounded-xl px-2 py-2 text-left hover:bg-bg-3/60 disabled:opacity-50"
            >
              <Avatar user={u} size={32} />
              <span className="min-w-0 flex-1 truncate text-sm font-medium text-ink">
                {displayName(users, f.user_id)}
              </span>
              <UserPlus size={16} className="text-ink-2" />
            </button>
          )
        })}
      </div>
      <ErrorText>{error}</ErrorText>
    </Modal>
  )
}

// ChatSettingsModal: rename or leave a multi-person chat.
function ChatSettingsModal({ open, onClose, channelId }: { open: boolean; onClose: () => void; channelId: string }) {
  const channel = useApp((s) => s.channels[channelId])
  const navigate = useNavigate()
  const [name, setName] = useState('')
  const [msg, setMsg] = useState('')
  const [error, setError] = useState('')

  useEffect(() => {
    if (open) {
      setName(channel?.name ?? '')
      setMsg('')
      setError('')
    }
  }, [open, channel?.name])

  async function save(e: React.FormEvent) {
    e.preventDefault()
    setError('')
    setMsg('')
    try {
      await api.patch(`/api/v1/channels/${channelId}`, { name })
      setMsg('Saved.')
    } catch (e2: any) {
      setError(e2?.message ?? 'Could not rename')
    }
  }

  async function leave() {
    if (!confirm('Leave this chat? You can be added back by anyone in it.')) return
    try {
      await api.del(`/api/v1/channels/${channelId}/participants/me`)
      onClose()
      navigate('/app/friends')
    } catch (e: any) {
      setError(e?.message ?? 'Could not leave')
    }
  }

  return (
    <Modal open={open} onClose={onClose} title="Chat settings">
      <form className="space-y-3" onSubmit={save}>
        <div>
          <Label>Chat name</Label>
          <Input
            value={name}
            onChange={(e) => setName(e.target.value)}
            maxLength={64}
            placeholder="Leave blank to show everyone's names"
          />
        </div>
        <div className="flex items-center gap-3">
          <Button type="submit">Save</Button>
          {msg && <span className="text-sm text-success">{msg}</span>}
        </div>
      </form>
      <ErrorText>{error}</ErrorText>
      <div className="mt-5 border-t border-edge pt-4">
        <Button variant="danger" className="w-full" onClick={leave}>
          <LogOut size={16} /> Leave chat
        </Button>
      </div>
    </Modal>
  )
}

// ListenButton starts a listening session (hidden while one is running:
// the panel takes over).
function ListenButton({ channelId }: { channelId: string }) {
  const active = useListen((s) => !!s.sessions[channelId]?.session)
  const [open, setOpen] = useState(false)
  if (active) return null
  return (
    <>
      <button
        className="rounded p-1.5 text-ink-1 hover:bg-bg-3 hover:text-accent"
        title="Listen together"
        onClick={() => setOpen(true)}
      >
        <Headphones size={18} />
      </button>
      <StartListenModal open={open} onClose={() => setOpen(false)} channelId={channelId} />
    </>
  )
}

// MuteButton silences notifications and unread badges for one channel.
function MuteButton({ channelId }: { channelId: string }) {
  const muted = useApp((s) => !!s.mutedChannelIds[channelId])
  const setMuted = useApp((s) => s.setMuted)
  async function toggle() {
    const next = !muted
    setMuted(channelId, next)
    try {
      if (next) await api.put(`/api/v1/channels/${channelId}/mute`, {})
      else await api.del(`/api/v1/channels/${channelId}/mute`)
    } catch {
      setMuted(channelId, !next)
    }
  }
  return (
    <button
      className={cx('rounded p-1.5 hover:bg-bg-3', muted ? 'text-ink-2 hover:text-ink' : 'text-ink-1 hover:text-ink')}
      title={muted ? 'Notifications are off for this chat. Click to turn on' : 'Mute this chat'}
      onClick={toggle}
    >
      {muted ? <BellOff size={18} /> : <Bell size={18} />}
    </button>
  )
}

function StartCallButton({ channelId }: { channelId: string }) {
  const joinedChannelId = useCall((s) => s.joinedChannelId)
  const participants = useCall((s) => s.calls[channelId]) ?? []
  const join = useCall((s) => s.join)
  // Hidden while a call already shows a banner/panel in this channel.
  if (joinedChannelId === channelId || participants.length > 0) return null
  return (
    <button
      className="rounded p-1.5 text-ink-1 hover:bg-bg-3 hover:text-success"
      title="Start a call"
      onClick={() => join(channelId)}
    >
      <Phone size={18} />
    </button>
  )
}

function PageHeaderPlaceholder({ onMenu }: { onMenu: () => void }) {
  return (
    <div className="absolute left-0 right-0 top-0">
      <PageHeader onMenu={onMenu} title="" />
    </div>
  )
}

function TypingLine({ channelId }: { channelId: string }) {
  const typing = useApp((s) => s.typing[channelId])
  const users = useApp((s) => s.users)
  const names = useMemo(() => {
    const now = Date.now()
    return Object.entries(typing ?? {})
      .filter(([, exp]) => exp > now)
      .map(([uid]) => displayName(users, uid))
  }, [typing, users])
  return (
    <div className="h-5 px-4 text-xs text-ink-2">
      {names.length > 0 && (
        <span className="animate-pulse">
          {names.join(', ')} {names.length === 1 ? 'is' : 'are'} typing…
        </span>
      )}
    </div>
  )
}

function MemberPanel({ groupId, open, onClose }: { groupId: string; open: boolean; onClose: () => void }) {
  const members = useApp((s) => s.groupMembers[groupId]) ?? []
  const users = useApp((s) => s.users)
  const online = useApp((s) => s.online)
  const openProfile = useApp((s) => s.openProfile)
  const listenSessions = useListen((s) => s.sessions)
  const listening = useMemo(() => {
    const set = new Set<string>()
    for (const st of Object.values(listenSessions)) for (const id of st.members) set.add(id)
    return set
  }, [listenSessions])

  const sorted = useMemo(() => {
    return [...members].sort((a, b) => {
      const ao = online[a.user_id] ? 0 : 1
      const bo = online[b.user_id] ? 0 : 1
      if (ao !== bo) return ao - bo
      return displayName(users, a.user_id).localeCompare(displayName(users, b.user_id))
    })
  }, [members, users, online])

  return (
    <>
      {open && <div className="fixed inset-0 z-30 bg-black/40 lg:hidden" onClick={onClose} />}
      <aside
        className={cx(
          'fixed inset-y-0 right-0 z-40 flex w-60 flex-col bg-bg-1 border-l border-edge transition-transform',
          // Conditional, never stacked: conflicting translate utilities
          // resolve by stylesheet order, not class order (hidden would win).
          open ? 'translate-x-0' : 'translate-x-full',
          'lg:static lg:z-auto', open ? 'lg:flex' : 'lg:hidden',
        )}
      >
        <div className="shrink-0 border-b border-edge px-4 py-3 text-sm font-semibold text-ink">
          Members — {members.length}
        </div>
        <div className="min-h-0 flex-1 space-y-0.5 overflow-y-auto p-2">
          {sorted.length === 0 && (
            <p className="px-2 py-1 text-xs text-ink-2">
              No members to show — try reloading the page.
            </p>
          )}
          {sorted.map((m) => {
            const u = users[m.user_id]
            return (
              <button
                key={m.user_id}
                onClick={() => openProfile(m.user_id)}
                title="View profile"
                className="flex w-full items-center gap-2 rounded-lg px-2 py-1.5 text-left hover:bg-bg-3/60"
              >
                <Avatar user={u} size={30} online={!!online[m.user_id]} />
                <div className="min-w-0">
                  <div className={cx('truncate text-sm', online[m.user_id] ? 'text-ink' : 'text-ink-2')}>
                    {displayName(users, m.user_id)}
                    {m.role !== 'member' && (
                      <span className="ml-1 rounded bg-bg-3 px-1 text-[10px] uppercase text-ink-1">{m.role}</span>
                    )}
                    {listening.has(m.user_id) && (
                      <Headphones size={12} className="ml-1 inline text-accent" aria-label="Listening" />
                    )}
                  </div>
                  {u?.status_text && <div className="truncate text-xs text-ink-2">{u.status_text}</div>}
                </div>
              </button>
            )
          })}
        </div>
      </aside>
    </>
  )
}

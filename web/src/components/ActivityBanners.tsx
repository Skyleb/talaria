// ActivityBanners: one slim bar per live session you are part of (a call, a
// listening session) whenever that session's own screen is not in view. It
// sits above everything at the top of the app, so nothing floats over the
// page and you are always one tap from getting back.
import { useEffect, useMemo, useState, type ReactNode } from 'react'
import { useLocation, useNavigate } from 'react-router'
import { ArrowRight, Headphones, LogOut, Mic, MicOff, Pause, PhoneOff, Play } from 'lucide-react'
import { displayName, dmTitle, useApp } from '../store'
import { formatClock, positionMs, useListen } from '../listen'
import { useCall } from '../call'
import { cx } from './ui'
import type { Channel, ListenState } from '../lib/types'

export function ActivityBanners() {
  const me = useApp((s) => s.me)
  const sessions = useListen((s) => s.sessions)
  const joinedCall = useCall((s) => s.joinedChannelId)
  const callMinimized = useCall((s) => s.minimized)
  const listening = useMemo(
    () => (me ? Object.values(sessions).find((st) => st.session && st.members.includes(me.id)) ?? null : null),
    [sessions, me],
  )
  if (!me) return null
  return (
    <>
      {joinedCall && callMinimized && <CallBanner channelId={joinedCall} />}
      {listening && <ListenBanner key={listening.channel_id} st={listening} meId={me.id} />}
    </>
  )
}

function pathFor(ch: Channel | undefined): string {
  if (!ch) return '/app'
  return ch.kind === 'dm' ? `/app/dm/${ch.id}` : `/app/g/${ch.group_id}/${ch.id}`
}

function useChannelLabel(channelId: string): string {
  const channel = useApp((s) => s.channels[channelId])
  const users = useApp((s) => s.users)
  const me = useApp((s) => s.me)
  if (!channel) return ''
  return channel.kind === 'dm' ? dmTitle(channel, me?.id ?? '', users) : `#${channel.name}`
}

// CallBanner: shown while the call view is minimized and you are not looking
// at that channel (the channel itself has its own "Open call" strip).
function CallBanner({ channelId }: { channelId: string }) {
  const location = useLocation()
  const muted = useCall((s) => s.muted)
  const label = useChannelLabel(channelId)
  const { toggleMute, leave, setMinimized } = useCall.getState()
  if (location.pathname.endsWith(`/${channelId}`)) return null
  return (
    <Bar>
      <span className="h-2.5 w-2.5 shrink-0 animate-pulse rounded-full bg-success" />
      <button onClick={() => setMinimized(false)} className="min-w-0 flex-1 text-left" title="Back to the call">
        <span className="block truncate text-sm text-ink">
          <span className="font-medium">In a call</span>
          {label && <span className="text-ink-1"> · {label}</span>}
        </span>
      </button>
      <IconButton title={muted ? 'Unmute' : 'Mute'} onClick={toggleMute} className={muted ? 'text-danger' : undefined}>
        {muted ? <MicOff size={16} /> : <Mic size={16} />}
      </IconButton>
      <ReturnButton onClick={() => setMinimized(false)} />
      <IconButton title="Leave call" onClick={leave} danger>
        <PhoneOff size={16} />
      </IconButton>
    </Bar>
  )
}

// ListenBanner: shown while the player is not visible in its channel (you
// are elsewhere in the app, or an overlay such as Settings is open).
function ListenBanner({ st, meId }: { st: ListenState; meId: string }) {
  const session = st.session!
  const channelId = st.channel_id
  const location = useLocation()
  const slot = useListen((s) => s.slot)
  const overlayOpen = useApp((s) => s.overlayOpen)
  const users = useApp((s) => s.users)
  const channel = useApp((s) => s.channels[channelId])
  const navigate = useNavigate()
  const { play, pause, leave } = useListen.getState()
  const isHost = session.host_user_id === meId
  const current = st.items.find((i) => i.id === session.current_item_id) ?? null
  // In view when the player slot is on screen, or when you are already on
  // the channel page (an empty queue has no slot yet) with no overlay open.
  const onChannelPage = location.pathname.endsWith(`/${channelId}`)
  const inView = !overlayOpen && ((!!slot && slot.channelId === channelId) || onChannelPage)

  // Keep the clock moving while the bar is up.
  const [, tick] = useState(0)
  useEffect(() => {
    if (inView || session.state !== 'playing') return
    const t = setInterval(() => tick((n) => n + 1), 1000)
    return () => clearInterval(t)
  }, [inView, session.state])

  if (inView) return null
  const where = channel ? (channel.kind === 'dm' ? dmTitle(channel, meId, users) : `#${channel.name}`) : ''
  const back = () => navigate(pathFor(channel))
  function toggle() {
    if (!isHost || !current) return
    ;(session.state === 'playing' ? pause(channelId) : play(channelId)).catch(() => {})
  }
  return (
    <Bar>
      <Headphones size={16} className="shrink-0 text-accent" />
      <button onClick={back} className="min-w-0 flex-1 text-left" title="Back to the listening session">
        <span className="block truncate text-sm text-ink">
          <span className="font-medium">{current?.title ?? 'Listening together'}</span>
          <span className="text-ink-1">
            {where && ` · ${where}`}
            {current && ` · ${formatClock(positionMs(session))}`}
            {session.state === 'paused' && ' · paused'}
            {!isHost && ` · ${displayName(users, session.host_user_id)} is driving`}
          </span>
        </span>
      </button>
      {isHost && current && (
        <IconButton title={session.state === 'playing' ? 'Pause for everyone' : 'Play for everyone'} onClick={toggle}>
          {session.state === 'playing' ? <Pause size={16} /> : <Play size={16} className="ml-0.5" />}
        </IconButton>
      )}
      <ReturnButton onClick={back} />
      <IconButton title="Leave the listening session" onClick={() => leave(channelId).catch(() => {})} danger>
        <LogOut size={16} />
      </IconButton>
    </Bar>
  )
}

function Bar({ children }: { children: ReactNode }) {
  return <div className="flex h-11 shrink-0 items-center gap-2 border-b border-edge bg-bg-1 px-3">{children}</div>
}

function ReturnButton({ onClick }: { onClick: () => void }) {
  return (
    <button
      onClick={onClick}
      className="flex shrink-0 items-center gap-1 rounded-full bg-accent px-3 py-1 text-xs font-medium text-accent-text hover:bg-accent-hover"
    >
      Return <ArrowRight size={13} />
    </button>
  )
}

function IconButton({
  title,
  onClick,
  danger,
  className,
  children,
}: {
  title: string
  onClick: () => void
  danger?: boolean
  className?: string
  children: ReactNode
}) {
  return (
    <button
      title={title}
      onClick={onClick}
      className={cx(
        'shrink-0 rounded-full p-1.5 text-ink-1 hover:bg-bg-3',
        danger ? 'hover:text-danger' : 'hover:text-ink',
        className,
      )}
    >
      {children}
    </button>
  )
}

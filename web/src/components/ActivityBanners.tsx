// ActivityBanners: a slim bar for the call you are in whenever the call view
// is not on screen. It sits above everything at the top of the app, so
// nothing floats over the page and you are always one tap from getting back.
import { useEffect, useState, type ReactNode } from 'react'
import { useLocation } from 'react-router'
import { ArrowRight, Headphones, Mic, MicOff, Pause, PhoneOff, Play } from 'lucide-react'
import { displayName, dmTitle, useApp } from '../store'
import { formatClock, positionMs, useListen } from '../listen'
import { useCall } from '../call'
import { cx } from './ui'

export function ActivityBanners() {
  const me = useApp((s) => s.me)
  const joinedCall = useCall((s) => s.joinedChannelId)
  const callMinimized = useCall((s) => s.minimized)
  if (!me) return null
  return <>{joinedCall && callMinimized && <CallBanner channelId={joinedCall} meId={me.id} />}</>
}

function useChannelLabel(channelId: string): string {
  const channel = useApp((s) => s.channels[channelId])
  const users = useApp((s) => s.users)
  const me = useApp((s) => s.me)
  if (!channel) return ''
  return channel.kind === 'dm' ? dmTitle(channel, me?.id ?? '', users) : `#${channel.name}`
}

// CallBanner: shown while the call view is minimized and you are not looking
// at that channel (the channel itself has its own "Open call" strip). When
// the call is listening together it doubles as the now-playing bar.
function CallBanner({ channelId, meId }: { channelId: string; meId: string }) {
  const location = useLocation()
  const muted = useCall((s) => s.muted)
  const label = useChannelLabel(channelId)
  const users = useApp((s) => s.users)
  const st = useListen((s) => s.sessions[channelId])
  const { toggleMute, leave, setMinimized } = useCall.getState()
  const { play, pause } = useListen.getState()
  const session = st?.session ?? null
  const current = session ? (st!.items.find((i) => i.id === session.current_item_id) ?? null) : null
  const isHost = !!session && session.host_user_id === meId
  const onChannelPage = location.pathname.endsWith(`/${channelId}`)

  // Keep the clock moving while the bar is up.
  const [, tick] = useState(0)
  useEffect(() => {
    if (onChannelPage || session?.state !== 'playing') return
    const t = setInterval(() => tick((n) => n + 1), 1000)
    return () => clearInterval(t)
  }, [onChannelPage, session?.state])

  if (onChannelPage) return null
  function toggle() {
    if (!isHost || !current || !session) return
    ;(session.state === 'playing' ? pause(channelId) : play(channelId)).catch(() => {})
  }
  return (
    <Bar>
      {current ? <Headphones size={16} className="shrink-0 text-accent" /> : <span className="h-2.5 w-2.5 shrink-0 animate-pulse rounded-full bg-success" />}
      <button onClick={() => setMinimized(false)} className="min-w-0 flex-1 text-left" title="Back to the call">
        <span className="block truncate text-sm text-ink">
          <span className="font-medium">{current ? current.title : 'In a call'}</span>
          <span className="text-ink-1">
            {label && ` · ${label}`}
            {current && session && ` · ${formatClock(positionMs(session))}`}
            {session?.state === 'paused' && ' · paused'}
            {current && !isHost && session && ` · ${displayName(users, session.host_user_id)} is driving`}
          </span>
        </span>
      </button>
      {isHost && current && (
        <IconButton title={session!.state === 'playing' ? 'Pause for everyone' : 'Play for everyone'} onClick={toggle}>
          {session!.state === 'playing' ? <Pause size={16} /> : <Play size={16} className="ml-0.5" />}
        </IconButton>
      )}
      <IconButton title={muted ? 'Unmute' : 'Mute'} onClick={toggleMute} className={muted ? 'text-danger' : undefined}>
        {muted ? <MicOff size={16} /> : <Mic size={16} />}
      </IconButton>
      <button
        onClick={() => setMinimized(false)}
        className="flex shrink-0 items-center gap-1 rounded-full bg-accent px-3 py-1 text-xs font-medium text-accent-text hover:bg-accent-hover"
      >
        Return <ArrowRight size={13} />
      </button>
      <IconButton title="Leave call" onClick={leave} danger>
        <PhoneOff size={16} />
      </IconButton>
    </Bar>
  )
}

function Bar({ children }: { children: ReactNode }) {
  return <div className="flex h-11 shrink-0 items-center gap-2 border-b border-edge bg-bg-1 px-3">{children}</div>
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

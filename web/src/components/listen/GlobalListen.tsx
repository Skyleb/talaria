// GlobalListen: the one persistent player for the session you are in. It is
// mounted at the app root so navigating (other channels, settings, friends)
// never unmounts the YouTube iframe or media element. In the channel it
// follows the PlayerSlot rectangle that ListenPanel renders; anywhere else
// it docks as a mini player in the corner.
import { useEffect, useMemo, useRef, useState, type CSSProperties } from 'react'
import { useNavigate } from 'react-router'
import { Headphones, LogOut, Maximize2, Minimize2, Pause, Play } from 'lucide-react'
import { displayName, dmTitle, useApp } from '../../store'
import { formatClock, positionMs, useListen } from '../../listen'
import { cx } from '../ui'
import { MediaPlayer, YouTubePlayer } from './players'
import type { ListenState } from '../../lib/types'

export function GlobalListen() {
  const sessions = useListen((s) => s.sessions)
  const me = useApp((s) => s.me)
  const joined = useMemo(
    () => (me ? Object.values(sessions).find((st) => st.session && st.members.includes(me.id)) ?? null : null),
    [sessions, me],
  )
  if (!joined || !me) return null
  return <FloatingPlayer key={joined.channel_id} st={joined} meId={me.id} />
}

function FloatingPlayer({ st, meId }: { st: ListenState; meId: string }) {
  const session = st.session!
  const channelId = st.channel_id
  const slot = useListen((s) => s.slot)
  const audioOnly = useListen((s) => s.audioOnly)
  const fsNonce = useListen((s) => s.fsNonce)
  const volume = useListen((s) => s.volume)
  const muted = useListen((s) => s.muted)
  const overlayOpen = useApp((s) => s.overlayOpen)
  const users = useApp((s) => s.users)
  const channel = useApp((s) => s.channels[channelId])
  const navigate = useNavigate()
  const boxRef = useRef<HTMLDivElement>(null)
  const [fs, setFs] = useState(false)
  const { play, pause, next, leave, reportDuration } = useListen.getState()

  const isHost = session.host_user_id === meId
  const current = st.items.find((i) => i.id === session.current_item_id) ?? null
  const inSlot = !!slot && slot.channelId === channelId && !overlayOpen && !fs

  // Fullscreen is requested from the panel's button via a counter.
  useEffect(() => {
    if (!fsNonce || !boxRef.current) return
    if (document.fullscreenElement) {
      document.exitFullscreen().catch(() => {})
      return
    }
    boxRef.current.requestFullscreen?.().catch(() => {})
  }, [fsNonce])
  useEffect(() => {
    const onChange = () => setFs(document.fullscreenElement === boxRef.current)
    document.addEventListener('fullscreenchange', onChange)
    return () => document.removeEventListener('fullscreenchange', onChange)
  }, [])

  // Only the host advances the queue when a track ends.
  const endedFor = useRef<string | null>(null)
  useEffect(() => {
    endedFor.current = null
  }, [session.current_item_id])
  function onEnded(itemId: string) {
    if (!isHost || endedFor.current === itemId) return
    endedFor.current = itemId
    next(channelId, itemId).catch(() => {})
  }

  function toggle() {
    if (!isHost || !current) return
    ;(session.state === 'playing' ? pause(channelId) : play(channelId)).catch(() => {})
  }

  // Re-render the mini player clock while playing.
  const [, tick] = useState(0)
  useEffect(() => {
    if (inSlot || session.state !== 'playing') return
    const t = setInterval(() => tick((n) => n + 1), 1000)
    return () => clearInterval(t)
  }, [inSlot, session.state])

  const style: CSSProperties = fs
    ? { position: 'fixed', inset: 0, width: '100%', height: '100%', zIndex: 60 }
    : inSlot && slot
      ? { position: 'fixed', left: slot.rect.left, top: slot.rect.top, width: slot.rect.width, height: slot.rect.height, zIndex: 20 }
      : { position: 'fixed', left: 16, bottom: 16, width: 'min(320px, calc(100vw - 32px))', zIndex: 45 }

  const channelPath = channel
    ? channel.kind === 'dm' ? `/app/dm/${channel.id}` : `/app/g/${channel.group_id}/${channel.id}`
    : '/app'
  const where = channel ? (channel.kind === 'dm' ? dmTitle(channel, meId, users) : `#${channel.name}`) : ''

  const player = current ? (
    current.kind === 'video' && current.refs?.youtube ? (
      <YouTubePlayer
        key="yt"
        item={current}
        session={session}
        volume={volume}
        muted={muted}
        onEnded={onEnded}
        onDuration={(id, ms) => reportDuration(channelId, id, ms)}
        onSurfaceClick={isHost ? toggle : undefined}
      />
    ) : (
      <MediaPlayer
        key="media"
        item={current}
        session={session}
        volume={volume}
        muted={muted}
        onEnded={onEnded}
        onDuration={(id, ms) => reportDuration(channelId, id, ms)}
        onSurfaceClick={isHost ? toggle : undefined}
      />
    )
  ) : null

  const showCover = audioOnly && !fs

  return (
    <div
      ref={boxRef}
      style={style}
      className={cx(
        'transition-[left,top,width,height] duration-150 ease-out',
        inSlot || fs
          ? 'overflow-hidden rounded-xl bg-black shadow-lg'
          : 'flex items-center gap-2 rounded-2xl border border-edge bg-bg-1 p-2 shadow-xl',
        fs && 'rounded-none',
      )}
    >
      {/* Video box: fills the slot/fullscreen, or a small thumbnail in the mini player. */}
      <div className={cx('relative shrink-0 overflow-hidden bg-black', inSlot || fs ? 'h-full w-full' : 'h-[54px] w-24 rounded-lg')}>
        <div
          className={cx(
            'absolute inset-0',
            // Audio-only: keep playing but at a 2px size, which makes YouTube serve
            // its lowest quality stream, then paint a cover over it.
            showCover && 'pointer-events-none opacity-0 [&_iframe]:!h-[2px] [&_iframe]:!w-[2px] [&_video]:!h-[2px] [&_video]:!w-[2px]',
          )}
        >
          {player}
        </div>
        {showCover && current && (
          <button
            onClick={isHost ? toggle : undefined}
            className="absolute inset-0 flex items-center justify-center bg-bg-2"
            title={isHost ? (session.state === 'playing' ? 'Pause' : 'Play') : undefined}
          >
            {current.refs?.thumbnail ? (
              <img src={current.refs.thumbnail} alt="" className="h-full w-full object-cover opacity-40 blur-sm" />
            ) : null}
            <span className="absolute flex flex-col items-center gap-1 text-ink">
              <Headphones size={inSlot ? 36 : 18} className="text-accent" />
              {inSlot && <span className="text-xs text-ink-1">Audio only</span>}
            </span>
          </button>
        )}
        {fs && (
          <div className="absolute bottom-0 left-0 right-0 flex items-center gap-2 bg-gradient-to-t from-black/70 to-transparent px-4 pb-3 pt-8 text-white">
            {isHost && (
              <button onClick={toggle} className="rounded-full bg-white/20 p-2 hover:bg-white/30">
                {session.state === 'playing' ? <Pause size={18} /> : <Play size={18} className="ml-0.5" />}
              </button>
            )}
            <span className="min-w-0 flex-1 truncate text-sm">{current?.title}</span>
            <button
              onClick={() => document.exitFullscreen().catch(() => {})}
              title="Exit fullscreen"
              className="rounded-full bg-white/20 p-2 hover:bg-white/30"
            >
              <Minimize2 size={18} />
            </button>
          </div>
        )}
      </div>

      {/* Mini player chrome */}
      {!inSlot && !fs && (
        <>
          <button
            onClick={() => navigate(channelPath)}
            className="min-w-0 flex-1 text-left"
            title="Back to the listening session"
          >
            <div className="truncate text-sm font-medium text-ink">{current?.title ?? 'Listening together'}</div>
            <div className="truncate text-xs text-ink-2">
              {where}
              {current ? ` · ${formatClock(positionMs(session))}` : ''}
              {session.state === 'paused' && ' · paused'}
              {!isHost && ` · ${displayName(users, session.host_user_id)} is driving`}
            </div>
          </button>
          {isHost && current && (
            <button
              onClick={toggle}
              title={session.state === 'playing' ? 'Pause for everyone' : 'Play for everyone'}
              className="rounded-full bg-accent p-2 text-accent-text hover:bg-accent-hover"
            >
              {session.state === 'playing' ? <Pause size={15} /> : <Play size={15} className="ml-0.5" />}
            </button>
          )}
          <button
            onClick={() => navigate(channelPath)}
            title="Open"
            className="rounded-full p-2 text-ink-1 hover:bg-bg-3 hover:text-ink"
          >
            <Maximize2 size={15} />
          </button>
          <button
            onClick={() => leave(channelId).catch(() => {})}
            title="Leave the listening session"
            className="rounded-full p-2 text-ink-1 hover:bg-bg-3 hover:text-danger"
          >
            <LogOut size={15} />
          </button>
        </>
      )}
    </div>
  )
}

// GlobalListen: the one persistent player for the session you are in. It is
// mounted at the app root so navigating (other channels, settings, friends)
// never unmounts the YouTube iframe or media element. In the channel it
// follows the PlayerSlot rectangle that ListenPanel renders; anywhere else
// it shrinks to an invisible 2px box so the audio keeps going while the
// ActivityBanners strip at the top offers the way back.
import { useEffect, useMemo, useRef, useState, type CSSProperties } from 'react'
import { Headphones, Minimize2, Pause, Play } from 'lucide-react'
import { useApp } from '../../store'
import { useListen } from '../../listen'
import { mixer } from '../../audio/mixer'
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
  const boxRef = useRef<HTMLDivElement>(null)
  const [fs, setFs] = useState(false)
  // duck lowers the music while someone in a call is talking.
  const [duck, setDuck] = useState(1)
  useEffect(() => mixer.registerDuckTarget('listen', setDuck), [])
  const { play, pause, next, reportDuration } = useListen.getState()

  const isHost = session.host_user_id === meId
  const current = st.items.find((i) => i.id === session.current_item_id) ?? null
  const inSlot = !!slot && slot.channelId === channelId && !overlayOpen && !fs
  const hidden = !inSlot && !fs

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

  const style: CSSProperties = fs
    ? { position: 'fixed', inset: 0, width: '100%', height: '100%', zIndex: 60 }
    : inSlot && slot
      ? { position: 'fixed', left: slot.rect.left, top: slot.rect.top, width: slot.rect.width, height: slot.rect.height, zIndex: 20 }
      : { position: 'fixed', left: 0, top: 0, width: 2, height: 2, opacity: 0, pointerEvents: 'none', zIndex: 0 }

  const player = current ? (
    current.kind === 'video' && current.refs?.youtube ? (
      <YouTubePlayer
        key="yt"
        item={current}
        session={session}
        volume={volume * duck}
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
        volume={volume * duck}
        muted={muted}
        onEnded={onEnded}
        onDuration={(id, ms) => reportDuration(channelId, id, ms)}
        onSurfaceClick={isHost ? toggle : undefined}
      />
    )
  ) : null

  const showCover = audioOnly && inSlot
  // Audio-only (and hidden): keep playing but at a 2px size, which makes
  // YouTube serve its lowest quality stream; the cover is painted over it.
  const tiny = showCover || hidden

  return (
    <div
      ref={boxRef}
      style={style}
      className={cx(
        'overflow-hidden bg-black',
        inSlot && 'rounded-xl shadow-lg transition-[left,top,width,height] duration-150 ease-out',
      )}
    >
      <div className="relative h-full w-full overflow-hidden bg-black">
        <div
          className={cx(
            'absolute inset-0',
            tiny && 'pointer-events-none opacity-0 [&_iframe]:!h-[2px] [&_iframe]:!w-[2px] [&_video]:!h-[2px] [&_video]:!w-[2px]',
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
              <Headphones size={36} className="text-accent" />
              <span className="text-xs text-ink-1">Audio only</span>
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
    </div>
  )
}

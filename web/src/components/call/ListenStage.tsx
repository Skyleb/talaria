// ListenStage: the shared player as a tile inside the call view. Everyone in
// the call sees and hears the same moment (server-clocked); the host drives
// transport. Music ducks while someone talks (audio mixer duck bus).
import { useEffect, useRef, useState } from 'react'
import { Crown, Headphones, ListMusic, Pause, Play, SkipBack, SkipForward, Video, VideoOff, Volume2, VolumeX } from 'lucide-react'
import { mixer } from '../../audio/mixer'
import { formatClock, positionMs, useListen } from '../../listen'
import { displayName, useApp } from '../../store'
import { cx } from '../ui'
import { MediaPlayer, YouTubePlayer } from '../listen/players'

export function ListenStage({ channelId, fill }: { channelId: string; fill?: boolean }) {
  const st = useListen((s) => s.sessions[channelId])
  const me = useApp((s) => s.me)!
  const users = useApp((s) => s.users)
  const volume = useListen((s) => s.volume)
  const muted = useListen((s) => s.muted)
  const audioOnly = useListen((s) => s.audioOnly)
  const queueOpen = useListen((s) => s.queueOpen)
  const { play, pause, seek, next, previous, reportDuration, setVolume, setMuted, setAudioOnly, setQueueOpen } = useListen.getState()
  const [error, setError] = useState('')

  // Music follows the duck bus: quieter while people talk.
  const [duck, setDuck] = useState(1)
  useEffect(() => mixer.registerDuckTarget('listen', setDuck), [])

  const session = st?.session ?? null
  const current = session ? (st!.items.find((i) => i.id === session.current_item_id) ?? null) : null
  const isHost = !!session && session.host_user_id === me.id
  const durationMs = current?.duration_ms ?? 0

  // Keep the clock moving while playing.
  const [, tick] = useState(0)
  useEffect(() => {
    if (session?.state !== 'playing') return
    const t = setInterval(() => tick((n) => n + 1), 500)
    return () => clearInterval(t)
  }, [session?.state])

  // Only the host advances the queue when a track ends.
  const endedFor = useRef<string | null>(null)
  useEffect(() => {
    endedFor.current = null
  }, [session?.current_item_id])

  if (!session) return null
  const pos = Math.min(positionMs(session), durationMs > 0 ? durationMs : Infinity)

  function guard(p: Promise<unknown>) {
    setError('')
    p.catch((e: any) => setError(e?.message ?? 'That did not work'))
  }
  function onEnded(itemId: string) {
    if (!isHost || endedFor.current === itemId) return
    endedFor.current = itemId
    guard(next(channelId, itemId))
  }
  function toggle() {
    if (!isHost || !current) return
    guard(session!.state === 'playing' ? pause(channelId) : play(channelId))
  }

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

  return (
    <div className={cx('relative flex h-full w-full flex-col bg-black', !fill && 'aspect-video')}>
      <div className="relative min-h-0 flex-1">
        {/* Audio-only keeps playing at a 2px size (YouTube then serves its
            lowest quality) behind a cover. */}
        <div
          className={cx(
            'absolute inset-0',
            audioOnly && 'pointer-events-none opacity-0 [&_iframe]:!h-[2px] [&_iframe]:!w-[2px] [&_video]:!h-[2px] [&_video]:!w-[2px]',
          )}
        >
          {player}
        </div>
        {(audioOnly || !current) && (
          <button
            onClick={isHost ? toggle : undefined}
            className="absolute inset-0 flex items-center justify-center bg-bg-2"
            title={isHost && current ? (session.state === 'playing' ? 'Pause' : 'Play') : undefined}
          >
            {current?.refs?.thumbnail && (
              <img src={current.refs.thumbnail} alt="" className="h-full w-full object-cover opacity-40 blur-sm" />
            )}
            <span className="absolute flex flex-col items-center gap-1 px-4 text-center text-ink">
              <Headphones size={36} className="text-accent" />
              <span className="line-clamp-2 text-sm font-medium">{current ? current.title : 'Nothing queued yet'}</span>
              <span className="text-xs text-ink-1">{current ? 'Audio only' : 'Add a song or video from the queue'}</span>
            </span>
          </button>
        )}
      </div>

      {/* Transport bar */}
      <div
        className="shrink-0 bg-black/80 px-2 pb-1.5 pt-1 text-white"
        onClick={(e) => e.stopPropagation()}
        onPointerDown={(e) => e.stopPropagation()}
      >
        <div className="flex items-center gap-2 text-[11px] tabular-nums text-white/70">
          <span>{formatClock(pos)}</span>
          <input
            type="range"
            min={0}
            max={durationMs > 0 ? durationMs : Math.max(pos, 1)}
            value={Math.min(pos, durationMs > 0 ? durationMs : pos)}
            disabled={!isHost || !current}
            onChange={(e) => guard(seek(channelId, Number(e.target.value)))}
            className="h-1 flex-1 accent-[var(--color-accent)] disabled:opacity-60"
            aria-label="Playback position"
          />
          <span>{durationMs > 0 ? formatClock(durationMs) : '--:--'}</span>
        </div>
        <div className="flex items-center gap-1.5">
          {isHost ? (
            <>
              <Ctl title="Previous" onClick={() => guard(previous(channelId))} disabled={!current}>
                <SkipBack size={14} />
              </Ctl>
              <button
                title={session.state === 'playing' ? 'Pause for everyone' : 'Play for everyone'}
                disabled={!current}
                onClick={toggle}
                className="rounded-full bg-accent p-1.5 text-accent-text hover:bg-accent-hover disabled:opacity-50"
              >
                {session.state === 'playing' ? <Pause size={14} /> : <Play size={14} className="ml-0.5" />}
              </button>
              <Ctl title="Next" onClick={() => guard(next(channelId))} disabled={!current}>
                <SkipForward size={14} />
              </Ctl>
            </>
          ) : (
            <span className="flex items-center gap-1 text-[11px] text-white/70">
              <Crown size={11} className="text-warning" />
              {displayName(users, session.host_user_id)} is driving{session.state === 'paused' ? ' · paused' : ''}
            </span>
          )}
          <span className="min-w-0 flex-1 truncate px-1 text-xs">{current?.title}</span>
          <Ctl title={audioOnly ? 'Show video' : 'Audio only (saves data)'} onClick={() => setAudioOnly(!audioOnly)} active={audioOnly}>
            {audioOnly ? <VideoOff size={14} /> : <Video size={14} />}
          </Ctl>
          <Ctl title={muted ? 'Unmute music' : 'Mute music (just for you)'} onClick={() => setMuted(!muted)}>
            {muted || volume === 0 ? <VolumeX size={14} /> : <Volume2 size={14} />}
          </Ctl>
          <input
            type="range"
            min={0}
            max={1}
            step={0.02}
            value={muted ? 0 : volume}
            onChange={(e) => setVolume(Number(e.target.value))}
            className="hidden w-16 accent-[var(--color-accent)] sm:block"
            aria-label="Music volume"
          />
          <Ctl title={queueOpen ? 'Hide queue' : 'Show queue'} onClick={() => setQueueOpen(!queueOpen)} active={queueOpen}>
            <ListMusic size={14} />
          </Ctl>
        </div>
        {error && <p className="px-1 pt-0.5 text-[11px] text-danger">{error}</p>}
      </div>
    </div>
  )
}

function Ctl({
  children, title, disabled, active, onClick,
}: {
  children: React.ReactNode
  title: string
  disabled?: boolean
  active?: boolean
  onClick: () => void
}) {
  return (
    <button
      title={title}
      disabled={disabled}
      onClick={onClick}
      className={cx('rounded-full p-1.5 disabled:opacity-40', active ? 'bg-accent/30 text-accent' : 'bg-white/10 hover:bg-white/20')}
    >
      {children}
    </button>
  )
}

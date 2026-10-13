import { useEffect, useMemo, useRef, useState } from 'react'
import {
  ChevronDown, Expand, Grid2x2, Maximize2, Mic, MicOff, Monitor, MonitorOff,
  Phone, PhoneOff, Video, VideoOff,
} from 'lucide-react'
import { useCall, type RemoteFeed } from '../../call'
import { displayName, dmTitle, useApp } from '../../store'
import { Avatar } from '../Avatar'
import { Button, cx } from '../ui'

// CallPanel renders at the top of a channel: a join banner when a call is in
// progress, or a slim "you're in this call" banner while the call UI is
// minimized. The active call itself renders globally (GlobalCall) so audio
// and the floating pill survive navigating anywhere in the app.
export function CallPanel({ channelId }: { channelId: string }) {
  const participants = useCall((s) => s.calls[channelId]) ?? []
  const joinedChannelId = useCall((s) => s.joinedChannelId)
  const minimized = useCall((s) => s.minimized)
  const setMinimized = useCall((s) => s.setMinimized)

  if (joinedChannelId === channelId && minimized) {
    return (
      <div className="flex items-center gap-3 border-b border-edge bg-success/10 px-4 py-2">
        <LiveDot />
        <span className="min-w-0 flex-1 truncate text-sm font-medium text-ink">
          You're in this call
        </span>
        <Button className="px-3 py-1.5 text-xs" onClick={() => setMinimized(false)}>
          <Expand size={14} /> Open call
        </Button>
      </div>
    )
  }
  if (joinedChannelId !== channelId && participants.length > 0) {
    return <JoinBanner channelId={channelId} />
  }
  return null
}

function LiveDot() {
  return (
    <span className="relative flex h-2.5 w-2.5">
      <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-success opacity-60" />
      <span className="relative inline-flex h-2.5 w-2.5 rounded-full bg-success" />
    </span>
  )
}

function JoinBanner({ channelId }: { channelId: string }) {
  const participants = useCall((s) => s.calls[channelId]) ?? []
  const join = useCall((s) => s.join)
  const users = useApp((s) => s.users)
  const names = participants.map((p) => displayName(users, p.user_id)).join(', ')
  return (
    <div className="flex items-center gap-3 border-b border-edge bg-bg-2 px-4 py-2">
      <LiveDot />
      <div className="min-w-0 flex-1 text-sm">
        <span className="font-medium text-ink">Voice call in progress</span>
        <span className="ml-2 truncate text-ink-1">{names}</span>
      </div>
      <Button className="px-3 py-1.5 text-xs" onClick={() => join(channelId)}>
        <Phone size={14} /> Join
      </Button>
    </div>
  )
}

// GlobalCall mounts once in AppShell. It owns the remote audio elements (so
// sound keeps playing wherever you browse) and shows the full-screen call
// view unless minimized (then the ActivityBanners strip offers the way back).
export function GlobalCall() {
  const joinedChannelId = useCall((s) => s.joinedChannelId)
  const minimized = useCall((s) => s.minimized)
  const remoteFeeds = useCall((s) => s.remoteFeeds)
  if (!joinedChannelId) return null
  const audioFeeds = remoteFeeds.filter((f) => f.kind === 'mic')
  return (
    <>
      {audioFeeds.map((f) => (
        <AudioSink key={f.key} feed={f} />
      ))}
      {!minimized && <CallOverlay channelId={joinedChannelId} />}
    </>
  )
}

function useCallTitle(channelId: string): string {
  const channel = useApp((s) => s.channels[channelId])
  const users = useApp((s) => s.users)
  const me = useApp((s) => s.me)
  if (!channel) return 'Call'
  return channel.kind === 'dm' ? dmTitle(channel, me?.id ?? '', users) : `#${channel.name}`
}

// One entry per visible tile, so grid, spotlight, and filmstrip all render
// from the same list.
interface Tile {
  key: string
  stream?: MediaStream
  mirror?: boolean
  userId?: string // avatar tiles
  label: string
  muted?: boolean
}

function useTiles(channelId: string): Tile[] {
  const participants = useCall((s) => s.calls[channelId]) ?? []
  const remoteFeeds = useCall((s) => s.remoteFeeds)
  const localCam = useCall((s) => s.localCamStream)
  const localScreen = useCall((s) => s.localScreenStream)
  const selfMuted = useCall((s) => s.muted)
  const users = useApp((s) => s.users)
  const me = useApp((s) => s.me)

  return useMemo(() => {
    const tiles: Tile[] = []
    const videoFeeds = remoteFeeds.filter((f) => f.kind !== 'mic')
    for (const f of videoFeeds) {
      tiles.push({
        key: f.key,
        stream: f.stream,
        label: displayName(users, f.userId) + (f.kind === 'screen' ? ' (screen)' : ''),
      })
    }
    for (const p of participants) {
      if (p.user_id === me?.id) continue
      if (videoFeeds.some((f) => f.userId === p.user_id)) continue
      tiles.push({
        key: `avatar-${p.user_id}`,
        userId: p.user_id,
        label: displayName(users, p.user_id),
        muted: p.muted,
      })
    }
    if (localScreen) tiles.push({ key: 'local-screen', stream: localScreen, label: 'Your screen' })
    if (localCam) {
      tiles.push({ key: 'local-cam', stream: localCam, mirror: true, label: 'You' })
    } else if (me) {
      tiles.push({ key: `avatar-${me.id}`, userId: me.id, label: 'You', muted: selfMuted })
    }
    return tiles
  }, [participants, remoteFeeds, localCam, localScreen, selfMuted, users, me])
}

function CallOverlay({ channelId }: { channelId: string }) {
  const setMinimized = useCall((s) => s.setMinimized)
  const connecting = useCall((s) => s.connecting)
  const mediaError = useCall((s) => s.mediaError)
  const title = useCallTitle(channelId)
  const tiles = useTiles(channelId)
  const [focusKey, setFocusKey] = useState<string | null>(null)

  const focused = tiles.find((t) => t.key === focusKey) ?? null
  // If the focused feed disappears (participant left), fall back to the grid.
  useEffect(() => {
    if (focusKey && !tiles.some((t) => t.key === focusKey)) setFocusKey(null)
  }, [focusKey, tiles])

  return (
    <div className="fixed inset-0 z-50 flex flex-col bg-bg-0 pt-[env(safe-area-inset-top)]">
      <header className="flex h-12 shrink-0 items-center gap-2.5 border-b border-edge px-4">
        <LiveDot />
        <h1 className="truncate text-sm font-semibold text-ink">{title}</h1>
        <button
          title="Minimize — keep browsing while you talk"
          onClick={() => setMinimized(true)}
          className="ml-auto flex items-center gap-1.5 rounded-lg px-2.5 py-1.5 text-sm text-ink-1 hover:bg-bg-3 hover:text-ink"
        >
          <ChevronDown size={18} />
          <span className="hidden sm:inline">Minimize</span>
        </button>
      </header>

      <div className="flex min-h-0 flex-1 flex-col gap-2 p-3">
        {focused ? (
          <>
            <FocusedTile tile={focused} onBack={() => setFocusKey(null)} />
            {tiles.length > 1 && (
              <div className="flex shrink-0 gap-2 overflow-x-auto pb-1">
                {tiles
                  .filter((t) => t.key !== focused.key)
                  .map((t) => (
                    <div key={t.key} className="w-36 shrink-0">
                      <TileView tile={t} onClick={() => setFocusKey(t.key)} />
                    </div>
                  ))}
              </div>
            )}
          </>
        ) : (
          <div
            className={cx(
              'grid min-h-0 flex-1 content-center gap-2 overflow-y-auto',
              tiles.length <= 1
                ? 'grid-cols-1'
                : tiles.length <= 4
                  ? 'grid-cols-1 sm:grid-cols-2'
                  : 'grid-cols-2 sm:grid-cols-3',
            )}
          >
            {tiles.map((t) => (
              <TileView key={t.key} tile={t} onClick={() => setFocusKey(t.key)} hint="Tap to enlarge" />
            ))}
          </div>
        )}
      </div>

      {connecting && <p className="text-center text-xs text-ink-2">Connecting…</p>}
      {mediaError && <p className="px-4 text-center text-xs text-danger">{mediaError}</p>}
      <div className="shrink-0 pb-[max(0.75rem,env(safe-area-inset-bottom))] pt-1">
        <Controls />
      </div>
    </div>
  )
}

// FocusedTile fills the stage; the expand button goes native fullscreen.
function FocusedTile({ tile, onBack }: { tile: Tile; onBack: () => void }) {
  const wrapRef = useRef<HTMLDivElement>(null)
  function goFullscreen() {
    const el = wrapRef.current
    if (!el) return
    if (document.fullscreenElement) {
      document.exitFullscreen().catch(() => {})
      return
    }
    if (el.requestFullscreen) {
      el.requestFullscreen().catch(() => {})
    } else {
      // iOS Safari only allows fullscreen on the <video> element itself.
      const v = el.querySelector('video') as any
      v?.webkitEnterFullscreen?.()
    }
  }
  return (
    <div ref={wrapRef} className="relative min-h-0 flex-1">
      <TileView tile={tile} fill />
      <div className="absolute right-2 top-2 flex gap-1.5">
        <button
          title="Fullscreen"
          onClick={goFullscreen}
          className="rounded-lg bg-black/55 p-2 text-white hover:bg-black/75"
        >
          <Maximize2 size={16} />
        </button>
        <button
          title="Back to grid"
          onClick={onBack}
          className="rounded-lg bg-black/55 p-2 text-white hover:bg-black/75"
        >
          <Grid2x2 size={16} />
        </button>
      </div>
    </div>
  )
}

function TileView({
  tile,
  fill,
  hint,
  onClick,
}: {
  tile: Tile
  fill?: boolean
  hint?: string
  onClick?: () => void
}) {
  const users = useApp((s) => s.users)
  const body = tile.stream ? (
    <StreamVideo stream={tile.stream} mirror={tile.mirror} />
  ) : (
    <div className="flex h-full w-full flex-col items-center justify-center gap-1.5 bg-bg-1">
      <Avatar user={tile.userId ? users[tile.userId] : undefined} size={fill ? 96 : 52} />
    </div>
  )
  return (
    <div
      onClick={onClick}
      title={hint}
      className={cx(
        'group relative overflow-hidden rounded-xl bg-black',
        fill ? 'h-full w-full' : 'aspect-video',
        onClick && 'cursor-pointer transition-shadow hover:ring-2 hover:ring-accent/60',
      )}
    >
      {body}
      <span className="absolute bottom-1 left-1.5 flex items-center gap-1 rounded bg-black/60 px-1.5 py-0.5 text-[11px] text-white">
        {tile.label}
        {tile.muted && <MicOff size={11} className="text-danger" />}
      </span>
    </div>
  )
}

function StreamVideo({ stream, mirror }: { stream: MediaStream; mirror?: boolean }) {
  const ref = useRef<HTMLVideoElement>(null)
  useEffect(() => {
    if (ref.current) ref.current.srcObject = stream
  }, [stream])
  return (
    <video
      ref={ref}
      autoPlay
      playsInline
      muted
      className={cx('h-full w-full object-contain', mirror && '-scale-x-100')}
    />
  )
}


function Controls() {
  const muted = useCall((s) => s.muted)
  const cameraOn = useCall((s) => s.cameraOn)
  const sharing = useCall((s) => s.sharing)
  const { toggleMute, toggleCamera, toggleScreen, leave } = useCall.getState()
  const canCapture = !!navigator.mediaDevices?.getUserMedia
  return (
    <div className="flex items-center justify-center gap-2">
      <CtlBtn title={muted ? 'Unmute' : 'Mute'} active={!muted} onClick={toggleMute} disabled={!canCapture}>
        {muted ? <MicOff size={18} /> : <Mic size={18} />}
      </CtlBtn>
      <CtlBtn title={cameraOn ? 'Turn camera off' : 'Turn camera on'} active={cameraOn} onClick={toggleCamera} disabled={!canCapture}>
        {cameraOn ? <Video size={18} /> : <VideoOff size={18} />}
      </CtlBtn>
      <CtlBtn
        title={sharing ? 'Stop sharing' : 'Share your screen'}
        active={sharing}
        onClick={toggleScreen}
        disabled={!navigator.mediaDevices?.getDisplayMedia}
      >
        {sharing ? <MonitorOff size={18} /> : <Monitor size={18} />}
      </CtlBtn>
      <button
        title="Leave call"
        onClick={leave}
        className="rounded-full bg-danger p-2.5 text-white hover:opacity-90"
      >
        <PhoneOff size={18} />
      </button>
    </div>
  )
}

function CtlBtn({
  children, title, active, disabled, onClick,
}: {
  children: React.ReactNode
  title: string
  active?: boolean
  disabled?: boolean
  onClick: () => void
}) {
  return (
    <button
      title={title}
      disabled={disabled}
      onClick={onClick}
      className={cx(
        'rounded-full p-2.5 transition-colors disabled:opacity-40',
        active ? 'bg-bg-1 text-ink hover:bg-bg-3' : 'bg-bg-3 text-ink-1 hover:text-ink',
      )}
    >
      {children}
    </button>
  )
}

function AudioSink({ feed }: { feed: RemoteFeed }) {
  const ref = useRef<HTMLAudioElement>(null)
  useEffect(() => {
    if (ref.current) ref.current.srcObject = feed.stream
  }, [feed.stream])
  return <audio ref={ref} autoPlay />
}

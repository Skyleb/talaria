import { useEffect, useMemo, useRef, useState } from 'react'
import {
  ChevronDown, Ear, EarOff, Expand, Grid2x2, Headphones, ListMusic, Maximize2, Mic, MicOff, Monitor, MonitorOff,
  Phone, PhoneOff, SlidersHorizontal, SwitchCamera, Video, VideoOff, Volume2, VolumeX,
} from 'lucide-react'
import { useCall, type RemoteFeed, type StreamQuality } from '../../call'
import { useListen } from '../../listen'
import { mixer } from '../../audio/mixer'
import { isMobile, usePrefs, type AVPrefs } from '../../prefs'
import { MIC_TIERS, CAM_PRESETS, SCREEN_PRESETS } from '../../media/encodings'
import { displayName, dmTitle, useApp } from '../../store'
import { Avatar } from '../Avatar'
import { Button, cx, Select } from '../ui'
import { ListenStage } from './ListenStage'
import { QueuePanel } from './QueuePanel'
import { StartListenModal } from '../listen/queue'

// CallPanel renders at the top of a channel: a join banner when a call is in
// progress, or a slim "you're in this call" banner while the call UI is
// minimized. The active call itself renders globally (GlobalCall) so audio
// and the floating pill survive navigating anywhere in the app.
export function CallPanel({ channelId }: { channelId: string }) {
  const participants = useCall((s) => s.calls[channelId]) ?? []
  const joinedChannelId = useCall((s) => s.joinedChannelId)
  const minimized = useCall((s) => s.minimized)
  const setMinimized = useCall((s) => s.setMinimized)
  const nowPlaying = useListen((s) => {
    const st = s.sessions[channelId]
    return st?.session ? (st.items.find((i) => i.id === st.session!.current_item_id)?.title ?? null) : null
  })

  if (joinedChannelId === channelId && minimized) {
    return (
      <div className="flex items-center gap-3 border-b border-edge bg-success/10 px-4 py-2">
        <LiveDot />
        <span className="min-w-0 flex-1 truncate text-sm text-ink">
          <span className="font-medium">You're in this call</span>
          {nowPlaying && <span className="text-ink-1"> · 🎧 {nowPlaying}</span>}
        </span>
        <Button className="px-3 py-1.5 text-xs" onClick={() => setMinimized(false)}>
          <Expand size={14} /> Open call
        </Button>
      </div>
    )
  }
  if (joinedChannelId !== channelId && participants.length > 0) {
    return <JoinBanner channelId={channelId} nowPlaying={nowPlaying} />
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

function JoinBanner({ channelId, nowPlaying }: { channelId: string; nowPlaying: string | null }) {
  const participants = useCall((s) => s.calls[channelId]) ?? []
  const join = useCall((s) => s.join)
  const users = useApp((s) => s.users)
  const names = participants.map((p) => displayName(users, p.user_id)).join(', ')
  return (
    <div className="flex items-center gap-3 border-b border-edge bg-bg-2 px-4 py-2">
      <LiveDot />
      <div className="min-w-0 flex-1 truncate text-sm">
        <span className="font-medium text-ink">{nowPlaying ? 'Call · listening together' : 'Voice call in progress'}</span>
        <span className="ml-2 text-ink-1">{names}</span>
        {nowPlaying && <span className="ml-2 text-ink-2">🎧 {nowPlaying}</span>}
      </div>
      <Button className="px-3 py-1.5 text-xs" onClick={() => join(channelId)}>
        <Phone size={14} /> Join
      </Button>
    </div>
  )
}

// GlobalCall mounts once in AppShell. It owns the remote audio (so sound
// keeps playing wherever you browse) and the full-screen call view. The view
// stays mounted while minimized (hidden) so a shared player inside it keeps
// playing; the ActivityBanners strip offers the way back.
export function GlobalCall() {
  const joinedChannelId = useCall((s) => s.joinedChannelId)
  const remoteFeeds = useCall((s) => s.remoteFeeds)
  if (!joinedChannelId) return null
  // Everything with sound: microphones, and shared screens that carry tab
  // or system audio.
  const audioFeeds = remoteFeeds.filter((f) => f.kind === 'mic' || f.stream.getAudioTracks().length > 0)
  return (
    <>
      <MixerBridge feeds={audioFeeds} />
      {audioFeeds.map((f) => (
        <AudioSink key={f.key} feed={f} />
      ))}
      <CallOverlay key={joinedChannelId} channelId={joinedChannelId} />
    </>
  )
}

// MixerBridge keeps the Web Audio graph in step with the remote feeds that
// carry audio. Screen audio is wired as non-voice (no leveler, no speaking
// detection, no ducking trigger).
function MixerBridge({ feeds }: { feeds: RemoteFeed[] }) {
  const attached = useRef(new Map<string, string>())
  useEffect(() => {
    const seen = new Set<string>()
    for (const f of feeds) {
      seen.add(f.key)
      const sig = f.stream.id + ':' + f.stream.getAudioTracks().map((t) => t.id).join(',')
      if (attached.current.get(f.key) !== sig) {
        mixer.attach(f.key, f.userId, f.stream, { voice: f.kind === 'mic' })
        attached.current.set(f.key, sig)
      }
    }
    for (const key of [...attached.current.keys()]) {
      if (!seen.has(key)) {
        mixer.detach(key)
        attached.current.delete(key)
      }
    }
  }, [feeds])
  useEffect(
    () => () => {
      for (const key of attached.current.keys()) mixer.detach(key)
      attached.current.clear()
    },
    [],
  )
  return null
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
  userId?: string
  self?: boolean
  // listen marks the shared-player tile.
  listen?: boolean
  label: string
  muted?: boolean
  speaking?: boolean
}

const LISTEN_KEY = 'listen'

function useTiles(channelId: string): Tile[] {
  const participants = useCall((s) => s.calls[channelId]) ?? []
  const remoteFeeds = useCall((s) => s.remoteFeeds)
  const localCam = useCall((s) => s.localCamStream)
  const localScreen = useCall((s) => s.localScreenStream)
  const selfMuted = useCall((s) => s.muted)
  const speaking = useCall((s) => s.speaking)
  // The selfie camera is mirrored like a mirror; the rear camera shows the
  // world the right way round.
  const mirrorSelf = usePrefs((s) => s.camFacing) !== 'environment'
  const listening = useListen((s) => !!s.sessions[channelId]?.session)
  const users = useApp((s) => s.users)
  const me = useApp((s) => s.me)

  return useMemo(() => {
    const tiles: Tile[] = []
    if (listening) tiles.push({ key: LISTEN_KEY, listen: true, label: 'Listening together' })
    const videoFeeds = remoteFeeds.filter((f) => f.kind !== 'mic')
    const mutedOf = (uid: string) => participants.find((p) => p.user_id === uid)?.muted
    for (const f of videoFeeds) {
      tiles.push({
        key: f.key,
        stream: f.stream,
        userId: f.userId,
        label: displayName(users, f.userId) + (f.kind === 'screen' ? ' (screen)' : ''),
        muted: f.kind === 'cam' ? mutedOf(f.userId) : undefined,
        speaking: f.kind === 'cam' && !!speaking[f.userId],
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
        speaking: !!speaking[p.user_id],
      })
    }
    if (localScreen) tiles.push({ key: 'local-screen', stream: localScreen, self: true, label: 'Your screen' })
    if (localCam) {
      tiles.push({ key: 'local-cam', stream: localCam, mirror: mirrorSelf, self: true, label: 'You', muted: selfMuted, speaking: !!speaking.me })
    } else if (me) {
      tiles.push({ key: `avatar-${me.id}`, userId: me.id, self: true, label: 'You', muted: selfMuted, speaking: !!speaking.me })
    }
    return tiles
  }, [participants, remoteFeeds, localCam, localScreen, selfMuted, speaking, listening, users, me, mirrorSelf])
}

// useMixerState polls the audio context so the "tap to hear" prompt can
// appear when a browser refused to start sound without a gesture.
function useMixerState() {
  const [state, setState] = useState(mixer.state())
  useEffect(() => {
    const t = setInterval(() => setState(mixer.state()), 1000)
    return () => clearInterval(t)
  }, [])
  return state
}

function CallOverlay({ channelId }: { channelId: string }) {
  const minimized = useCall((s) => s.minimized)
  const setMinimized = useCall((s) => s.setMinimized)
  const connecting = useCall((s) => s.connecting)
  const mediaError = useCall((s) => s.mediaError)
  const remoteFeeds = useCall((s) => s.remoteFeeds)
  const listening = useListen((s) => !!s.sessions[channelId]?.session)
  const queueOpen = useListen((s) => s.queueOpen)
  const setQueueOpen = useListen((s) => s.setQueueOpen)
  const title = useCallTitle(channelId)
  const tiles = useTiles(channelId)
  const mixerState = useMixerState()
  const [focusKey, setFocusKey] = useState<string | null>(null)
  const [startOpen, setStartOpen] = useState(false)

  const focused = tiles.find((t) => t.key === focusKey) ?? null
  // If the focused feed disappears (participant left), fall back to the grid.
  useEffect(() => {
    if (focusKey && !tiles.some((t) => t.key === focusKey)) setFocusKey(null)
  }, [focusKey, tiles])
  // The server favours the focused stream when bandwidth is tight.
  const setFocus = useCall((s) => s.setFocus)
  useEffect(() => {
    setFocus(focusKey)
  }, [focusKey, setFocus])
  // A shared player takes the stage when it appears; the queue closes when
  // the session ends.
  useEffect(() => {
    if (listening) setFocusKey(LISTEN_KEY)
    else setQueueOpen(false)
  }, [listening, setQueueOpen])

  const needsTap = mixerState === 'suspended' && remoteFeeds.some((f) => f.kind === 'mic')

  return (
    <div
      className={cx(
        'fixed inset-0 z-50 flex flex-col bg-bg-0 pt-[env(safe-area-inset-top)]',
        minimized && 'invisible pointer-events-none',
      )}
      aria-hidden={minimized}
    >
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

      {needsTap && (
        <button
          onClick={() => mixer.ensureStarted()}
          className="mx-3 mt-3 flex items-center justify-center gap-2 rounded-xl bg-accent px-4 py-3 text-sm font-medium text-accent-text"
        >
          <Volume2 size={18} /> Tap to hear the call
        </button>
      )}

      <div className={cx('flex min-h-0 flex-1 flex-col gap-2 p-3', queueOpen && 'sm:mr-80')}>
        {focused ? (
          <>
            <FocusedTile tile={focused} channelId={channelId} onBack={() => setFocusKey(null)} />
            {tiles.length > 1 && (
              <div className="flex shrink-0 gap-2 overflow-x-auto pb-1">
                {tiles
                  .filter((t) => t.key !== focused.key)
                  .map((t) => (
                    <div key={t.key} className="w-36 shrink-0">
                      <TileView tile={t} channelId={channelId} onClick={() => setFocusKey(t.key)} />
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
              <TileView key={t.key} tile={t} channelId={channelId} onClick={() => setFocusKey(t.key)} hint="Tap to enlarge" />
            ))}
          </div>
        )}
      </div>

      {connecting && <p className="text-center text-xs text-ink-2">Connecting…</p>}
      {mediaError && <p className="px-4 text-center text-xs text-danger">{mediaError}</p>}
      <div className="shrink-0 pb-[max(0.75rem,env(safe-area-inset-bottom))] pt-1">
        <Controls onListen={() => (listening ? setQueueOpen(!queueOpen) : setStartOpen(true))} listening={listening} queueOpen={queueOpen} />
      </div>

      {queueOpen && listening && !minimized && <QueuePanel channelId={channelId} onClose={() => setQueueOpen(false)} />}
      <StartListenModal open={startOpen} onClose={() => setStartOpen(false)} channelId={channelId} />
    </div>
  )
}

// FocusedTile fills the stage; the expand button goes native fullscreen.
function FocusedTile({ tile, channelId, onBack }: { tile: Tile; channelId: string; onBack: () => void }) {
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
      <TileView tile={tile} channelId={channelId} fill />
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
  channelId,
  fill,
  hint,
  onClick,
}: {
  tile: Tile
  channelId: string
  fill?: boolean
  hint?: string
  onClick?: () => void
}) {
  const users = useApp((s) => s.users)
  const [menu, setMenu] = useState(false)
  const canAdjust = !!tile.userId && !tile.self
  // Remote video tiles are keyed "userId|kind", which is also the stream key
  // for quality choices and layer reports.
  const streamKey = tile.stream && !tile.self && !tile.listen ? tile.key : undefined
  const layer = useCall((s) => (streamKey ? s.layers[streamKey] : undefined))
  const quality = useCall((s) => (streamKey ? (s.quality[streamKey] ?? 'auto') : 'auto'))
  if (tile.listen) {
    return (
      <div
        onClick={fill ? undefined : onClick}
        title={fill ? undefined : hint}
        className={cx('relative overflow-hidden rounded-xl bg-black', fill ? 'h-full w-full' : 'aspect-video cursor-pointer hover:ring-2 hover:ring-accent/60')}
      >
        {fill ? (
          <ListenStage channelId={channelId} fill />
        ) : (
          <div className="pointer-events-none flex h-full w-full flex-col items-center justify-center gap-1 bg-bg-1 text-ink-1">
            <Headphones size={22} className="text-accent" />
            <span className="text-[11px]">Listening together</span>
          </div>
        )}
      </div>
    )
  }
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
        'group relative rounded-xl bg-black transition-shadow',
        fill ? 'h-full w-full' : 'aspect-video',
        tile.speaking && 'ring-2 ring-success',
        !tile.speaking && onClick && 'cursor-pointer hover:ring-2 hover:ring-accent/60',
      )}
    >
      {/* The picture clips to the rounded corners; the label and its popover
          sit outside that clip so a small tile cannot cut the popover off. */}
      <div className="absolute inset-0 overflow-hidden rounded-xl">{body}</div>
      <button
        onClick={(e) => {
          if (!canAdjust) return
          e.stopPropagation()
          setMenu((v) => !v)
        }}
        title={canAdjust ? 'Volume for this person' : undefined}
        className={cx(
          'absolute bottom-1 left-1.5 flex items-center gap-1 rounded bg-black/60 px-1.5 py-0.5 text-[11px] text-white',
          canAdjust && 'hover:bg-black/80',
        )}
      >
        {tile.label}
        {tile.muted && <MicOff size={11} className="text-danger" />}
        {streamKey && layer?.rid && (
          <span className="rounded bg-white/15 px-1 text-[10px] uppercase tracking-wide">
            {quality === 'auto' ? 'auto · ' : ''}
            {LAYER_LABEL[layer.rid] ?? layer.rid}
          </span>
        )}
        {canAdjust && <Volume2 size={11} className="opacity-70" />}
      </button>
      {menu && tile.userId && (
        <VolumePopover userId={tile.userId} name={tile.label} streamKey={streamKey} onClose={() => setMenu(false)} />
      )}
    </div>
  )
}

const LAYER_LABEL: Record<string, string> = { f: 'high', h: 'medium', q: 'low', '': '' }

// VolumePopover: how loud this person is for you (0–200 %), remembered per
// person on this device. For a video stream it also offers the quality you
// receive (Auto lets the server pick for your connection).
function VolumePopover({
  userId,
  name,
  streamKey,
  onClose,
}: {
  userId: string
  name: string
  streamKey?: string
  onClose: () => void
}) {
  const [vol, setVol] = useState(() => mixer.getUserVolume(userId))
  const quality = useCall((s) => (streamKey ? (s.quality[streamKey] ?? 'auto') : 'auto'))
  const layer = useCall((s) => (streamKey ? s.layers[streamKey] : undefined))
  const setStreamQuality = useCall((s) => s.setStreamQuality)
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && onClose()
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])
  function update(v: number) {
    setVol(v)
    mixer.setUserVolume(userId, v)
  }
  return (
    <>
      {/* On phones the popover is a sheet above the controls; tapping
          anywhere else closes it. */}
      <div className="fixed inset-0 z-10 sm:hidden" onClick={(e) => { e.stopPropagation(); onClose() }} />
      <div
        onClick={(e) => e.stopPropagation()}
        className={cx(
          'z-20 rounded-xl border border-edge bg-bg-1 p-3 text-ink shadow-xl',
          'fixed inset-x-3 bottom-[calc(env(safe-area-inset-bottom)+5rem)]',
          'sm:absolute sm:inset-x-auto sm:bottom-8 sm:left-1.5 sm:z-10 sm:w-60',
        )}
      >
      <div className="mb-2 flex items-center justify-between text-xs">
        <span className="truncate font-medium">{name}</span>
        <span className="tabular-nums text-ink-2">{Math.round(vol * 100)}%</span>
      </div>
      <div className="flex items-center gap-2">
        <button title="Mute for me" onClick={() => update(vol === 0 ? 1 : 0)} className="text-ink-1 hover:text-ink">
          {vol === 0 ? <VolumeX size={16} /> : <Volume2 size={16} />}
        </button>
        <input
          type="range"
          min={0}
          max={200}
          step={5}
          value={Math.round(vol * 100)}
          onChange={(e) => update(Number(e.target.value) / 100)}
          className="w-full accent-[var(--color-accent)]"
        />
      </div>
      {streamKey && (
        <div className="mt-3">
          <label className="mb-1 block text-[11px] font-semibold uppercase tracking-wide text-ink-1">Video quality</label>
          <Select value={quality} onChange={(e) => setStreamQuality(streamKey, e.target.value as StreamQuality)}>
            <option value="auto">Auto{layer?.rid ? ` (${LAYER_LABEL[layer.rid] ?? layer.rid} now)` : ''}</option>
            <option value="f">High</option>
            <option value="h">Medium</option>
            <option value="q">Low</option>
            <option value="off">Pause video</option>
          </Select>
          {layer && layer.bps > 0 && (
            <p className="mt-1 text-[11px] text-ink-2">Receiving about {Math.round(layer.bps / 1000)} kbps</p>
          )}
        </div>
      )}
      <div className="mt-2 flex justify-between text-[11px] text-ink-2">
        <button onClick={() => update(1)} className="hover:text-ink">
          Reset
        </button>
        <button onClick={onClose} className="hover:text-ink">
          Done
        </button>
      </div>
      </div>
    </>
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

function Controls({ onListen, listening, queueOpen }: { onListen: () => void; listening: boolean; queueOpen: boolean }) {
  const muted = useCall((s) => s.muted)
  const deafened = useCall((s) => s.deafened)
  const cameraOn = useCall((s) => s.cameraOn)
  const sharing = useCall((s) => s.sharing)
  const facing = usePrefs((s) => s.camFacing)
  const { toggleMute, toggleDeafen, toggleCamera, flipCamera, toggleScreen, leave } = useCall.getState()
  const canCapture = !!navigator.mediaDevices?.getUserMedia
  const canFlip = useHasSeveralCameras(cameraOn)
  return (
    <div className="flex flex-wrap items-center justify-center gap-1.5 px-2 sm:gap-2">
      <CtlBtn title={muted ? 'Unmute' : 'Mute'} active={!muted} onClick={toggleMute} disabled={!canCapture}>
        {muted ? <MicOff size={18} /> : <Mic size={18} />}
      </CtlBtn>
      <CtlBtn title={deafened ? 'Undeafen' : 'Deafen — silence everyone'} active={!deafened} onClick={toggleDeafen}>
        {deafened ? <EarOff size={18} /> : <Ear size={18} />}
      </CtlBtn>
      <CtlBtn title={cameraOn ? 'Turn camera off' : 'Turn camera on'} active={cameraOn} onClick={toggleCamera} disabled={!canCapture}>
        {cameraOn ? <Video size={18} /> : <VideoOff size={18} />}
      </CtlBtn>
      {cameraOn && canFlip && (
        <CtlBtn title={facing === 'environment' ? 'Switch to the front camera' : 'Switch to the rear camera'} active onClick={flipCamera}>
          <SwitchCamera size={18} />
        </CtlBtn>
      )}
      <CtlBtn
        title={sharing ? 'Stop sharing' : 'Share your screen'}
        active={sharing}
        onClick={toggleScreen}
        disabled={!navigator.mediaDevices?.getDisplayMedia}
      >
        {sharing ? <MonitorOff size={18} /> : <Monitor size={18} />}
      </CtlBtn>
      <CtlBtn title={listening ? (queueOpen ? 'Hide the queue' : 'Show the queue') : 'Listen together'} active={listening && queueOpen} onClick={onListen}>
        {listening ? <ListMusic size={18} /> : <Headphones size={18} />}
      </CtlBtn>
      <QualityButton />
      <button
        title="Leave call"
        onClick={leave}
        className="rounded-full bg-danger p-2 text-white hover:opacity-90 sm:p-2.5"
      >
        <PhoneOff size={18} />
      </button>
    </div>
  )
}

// useHasSeveralCameras: whether a flip button makes sense. Phones always get
// one (they have a front and a rear camera even when enumeration hides
// labels before permission); elsewhere only when more than one camera is
// attached. Re-checked when the camera turns on, since permission is what
// makes device labels and counts reliable.
function useHasSeveralCameras(cameraOn: boolean): boolean {
  const [several, setSeveral] = useState(isMobile())
  useEffect(() => {
    if (isMobile() || !cameraOn || !navigator.mediaDevices?.enumerateDevices) return
    let live = true
    navigator.mediaDevices
      .enumerateDevices()
      .then((devs) => live && setSeveral(devs.filter((d) => d.kind === 'videoinput').length > 1))
      .catch(() => {})
    return () => {
      live = false
    }
  }, [cameraOn])
  return several
}

// QualityButton: in-call access to the microphone tier and (Phase 4) the
// camera / screen presets. The same preferences live in Settings.
function QualityButton() {
  const [open, setOpen] = useState(false)
  const prefs = usePrefs()
  const set = usePrefs((s) => s.set)
  useEffect(() => {
    if (!open) return
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && setOpen(false)
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [open])
  return (
    <div className="relative">
      <CtlBtn title="Quality settings" active={open} onClick={() => setOpen((v) => !v)}>
        <SlidersHorizontal size={18} />
      </CtlBtn>
      {open && (
        <>
          <div className="fixed inset-0 z-10 sm:hidden" onClick={() => setOpen(false)} />
          <div
            className={cx(
              'z-20 rounded-xl border border-edge bg-bg-1 p-3 text-left text-sm text-ink shadow-xl',
              // Phones: a sheet above the controls that scrolls if it must.
              'fixed inset-x-3 bottom-[calc(env(safe-area-inset-bottom)+5rem)] max-h-[70dvh] overflow-y-auto',
              // Wider screens: anchored under the button.
              'sm:absolute sm:inset-x-auto sm:bottom-12 sm:left-1/2 sm:z-10 sm:max-h-none sm:w-64 sm:-translate-x-1/2 sm:overflow-visible',
            )}
          >
          <label className="mb-1 block text-xs font-semibold uppercase tracking-wide text-ink-1">Microphone</label>
          <Select value={prefs.micTier} onChange={(e) => set({ micTier: e.target.value as AVPrefs['micTier'] })}>
            {(Object.keys(MIC_TIERS) as AVPrefs['micTier'][]).map((t) => (
              <option key={t} value={t}>
                {MIC_TIERS[t].label}
              </option>
            ))}
          </Select>
          <p className="mt-1 text-xs text-ink-2">{MIC_TIERS[prefs.micTier].hint}</p>
          <label className="mb-1 mt-3 block text-xs font-semibold uppercase tracking-wide text-ink-1">Camera</label>
          <Select value={prefs.camPreset} onChange={(e) => set({ camPreset: e.target.value as AVPrefs['camPreset'] })}>
            {(Object.keys(CAM_PRESETS) as AVPrefs['camPreset'][]).map((p) => (
              <option key={p} value={p}>
                {CAM_PRESETS[p].label} · up to {CAM_PRESETS[p].kbps} kbps
              </option>
            ))}
          </Select>
          <label className="mb-1 mt-3 block text-xs font-semibold uppercase tracking-wide text-ink-1">Screen share</label>
          <Select value={prefs.screenPreset} onChange={(e) => set({ screenPreset: e.target.value as AVPrefs['screenPreset'] })}>
            {(Object.keys(SCREEN_PRESETS) as AVPrefs['screenPreset'][]).map((p) => (
              <option key={p} value={p}>
                {SCREEN_PRESETS[p].label} · up to {SCREEN_PRESETS[p].kbps} kbps
              </option>
            ))}
          </Select>
          {(CAM_PRESETS[prefs.camPreset].heavy || SCREEN_PRESETS[prefs.screenPreset].heavy) && (
            <p className="mt-1 text-xs text-warning">High tiers need a strong machine and upload; the server caps may lower them.</p>
          )}
          <p className="mt-1 text-xs text-ink-2">Camera and screen settings apply the next time you turn them on.</p>
          <label className="mt-3 flex cursor-pointer items-center gap-2 text-xs text-ink">
            <input type="checkbox" className="accent-[var(--color-accent)]" checked={prefs.voiceIsolation} onChange={(e) => set({ voiceIsolation: e.target.checked })} />
            Voice isolation (neural noise removal: wind, fans, keyboards)
          </label>
          <label className="mt-1.5 flex cursor-pointer items-center gap-2 text-xs text-ink">
            <input type="checkbox" className="accent-[var(--color-accent)]" checked={prefs.ducking.enabled} onChange={(e) => set({ ducking: { enabled: e.target.checked } as AVPrefs['ducking'] })} />
            Duck music while people talk
          </label>
          <button onClick={() => setOpen(false)} className="mt-3 w-full rounded-lg bg-bg-2 py-1.5 text-xs text-ink-1 hover:bg-bg-3 hover:text-ink">
            Done
          </button>
          </div>
        </>
      )}
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
        'rounded-full p-2 transition-colors disabled:opacity-40 sm:p-2.5',
        active ? 'bg-bg-1 text-ink hover:bg-bg-3' : 'bg-bg-3 text-ink-1 hover:text-ink',
      )}
    >
      {children}
    </button>
  )
}

// AudioSink keeps each remote stream attached to a (muted) media element.
// Sound is produced by the Web Audio mixer, but WebKit and older Chrome only
// deliver remote WebRTC audio into Web Audio while an element holds the
// stream.
function AudioSink({ feed }: { feed: RemoteFeed }) {
  const ref = useRef<HTMLAudioElement>(null)
  useEffect(() => {
    if (ref.current) ref.current.srcObject = feed.stream
  }, [feed.stream])
  return <audio ref={ref} autoPlay muted />
}

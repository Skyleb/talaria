import { useEffect, useMemo, useRef } from 'react'
import {
  Mic, MicOff, Monitor, MonitorOff, Phone, PhoneOff, Video, VideoOff,
} from 'lucide-react'
import { useCall, type RemoteFeed } from '../../call'
import { displayName, useApp } from '../../store'
import { Avatar } from '../Avatar'
import { Button, cx } from '../ui'

// CallPanel renders at the top of a channel: a join banner when a call is in
// progress, or the full tile grid + controls when this client is in it.
export function CallPanel({ channelId }: { channelId: string }) {
  const participants = useCall((s) => s.calls[channelId]) ?? []
  const joinedChannelId = useCall((s) => s.joinedChannelId)
  const joined = joinedChannelId === channelId

  if (!joined && participants.length === 0) return null
  return joined ? <ActiveCall channelId={channelId} /> : <JoinBanner channelId={channelId} />
}

function JoinBanner({ channelId }: { channelId: string }) {
  const participants = useCall((s) => s.calls[channelId]) ?? []
  const join = useCall((s) => s.join)
  const users = useApp((s) => s.users)
  const names = participants.map((p) => displayName(users, p.user_id)).join(', ')
  return (
    <div className="flex items-center gap-3 border-b border-edge bg-bg-2 px-4 py-2">
      <span className="relative flex h-2.5 w-2.5">
        <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-success opacity-60" />
        <span className="relative inline-flex h-2.5 w-2.5 rounded-full bg-success" />
      </span>
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

function ActiveCall({ channelId }: { channelId: string }) {
  const participants = useCall((s) => s.calls[channelId]) ?? []
  const remoteFeeds = useCall((s) => s.remoteFeeds)
  const connecting = useCall((s) => s.connecting)
  const mediaError = useCall((s) => s.mediaError)
  const me = useApp((s) => s.me)

  const videoFeeds = remoteFeeds.filter((f) => f.kind !== 'mic')
  const audioFeeds = remoteFeeds.filter((f) => f.kind === 'mic')
  const localCam = useCall((s) => s.localCamStream)
  const localScreen = useCall((s) => s.localScreenStream)

  const others = useMemo(
    () => participants.filter((p) => p.user_id !== me?.id),
    [participants, me],
  )

  const tileCount =
    videoFeeds.length + (localCam ? 1 : 0) + (localScreen ? 1 : 0) +
    others.filter((p) => !videoFeeds.some((f) => f.userId === p.user_id)).length +
    (localCam ? 0 : 1) // self avatar tile when no local video

  return (
    <div className="border-b border-edge bg-bg-2/70 p-3">
      {/* Hidden audio sinks for every remote mic */}
      {audioFeeds.map((f) => (
        <AudioSink key={f.key} feed={f} />
      ))}
      <div
        className={cx(
          'grid gap-2',
          tileCount <= 1 ? 'grid-cols-1' : tileCount <= 4 ? 'grid-cols-2' : 'grid-cols-2 sm:grid-cols-3',
        )}
      >
        {/* Remote video tiles (camera + screens) */}
        {videoFeeds.map((f) => (
          <VideoTile key={f.key} feed={f} />
        ))}
        {/* Avatar tiles for voice-only remote participants */}
        {others
          .filter((p) => !videoFeeds.some((f) => f.userId === p.user_id))
          .map((p) => (
            <AvatarTile key={p.user_id} userId={p.user_id} muted={p.muted} />
          ))}
        {/* Local preview */}
        {localScreen && <LocalTile stream={localScreen} label="Your screen" />}
        {localCam ? (
          <LocalTile stream={localCam} label="You" mirror />
        ) : (
          me && <AvatarTile userId={me.id} muted={useCall.getState().muted} self />
        )}
      </div>
      {connecting && <p className="mt-2 text-center text-xs text-ink-2">Connecting…</p>}
      {mediaError && <p className="mt-2 text-center text-xs text-danger">{mediaError}</p>}
      <Controls />
    </div>
  )
}

function Controls() {
  const muted = useCall((s) => s.muted)
  const cameraOn = useCall((s) => s.cameraOn)
  const sharing = useCall((s) => s.sharing)
  const { toggleMute, toggleCamera, toggleScreen, leave } = useCall.getState()
  const canCapture = !!navigator.mediaDevices?.getUserMedia
  return (
    <div className="mt-3 flex items-center justify-center gap-2">
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

function VideoTile({ feed }: { feed: RemoteFeed }) {
  const ref = useRef<HTMLVideoElement>(null)
  const users = useApp((s) => s.users)
  useEffect(() => {
    if (ref.current) ref.current.srcObject = feed.stream
  }, [feed.stream])
  return (
    <div className="relative aspect-video overflow-hidden rounded-lg bg-black">
      <video ref={ref} autoPlay playsInline muted className="h-full w-full object-contain" />
      <span className="absolute bottom-1 left-1.5 rounded bg-black/60 px-1.5 py-0.5 text-[11px] text-white">
        {displayName(users, feed.userId)}
        {feed.kind === 'screen' && ' (screen)'}
      </span>
    </div>
  )
}

function LocalTile({ stream, label, mirror }: { stream: MediaStream; label: string; mirror?: boolean }) {
  const ref = useRef<HTMLVideoElement>(null)
  useEffect(() => {
    if (ref.current) ref.current.srcObject = stream
  }, [stream])
  return (
    <div className="relative aspect-video overflow-hidden rounded-lg bg-black">
      <video
        ref={ref}
        autoPlay
        playsInline
        muted
        className={cx('h-full w-full object-contain', mirror && '-scale-x-100')}
      />
      <span className="absolute bottom-1 left-1.5 rounded bg-black/60 px-1.5 py-0.5 text-[11px] text-white">
        {label}
      </span>
    </div>
  )
}

function AvatarTile({ userId, muted, self }: { userId: string; muted?: boolean; self?: boolean }) {
  const users = useApp((s) => s.users)
  const u = users[userId]
  return (
    <div className="relative flex aspect-video flex-col items-center justify-center gap-1.5 rounded-lg bg-bg-1">
      <Avatar user={u} size={52} />
      <span className="text-xs text-ink-1">
        {self ? 'You' : displayName(users, userId)}
      </span>
      {muted && (
        <span className="absolute right-1.5 top-1.5 rounded-full bg-bg-3 p-1 text-danger">
          <MicOff size={12} />
        </span>
      )}
    </div>
  )
}

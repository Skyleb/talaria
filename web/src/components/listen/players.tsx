// Media backends for synchronized listening. Each player follows the
// server's transport state: it seeks when drift exceeds a threshold and
// matches play/pause. The host's own player is driven the same way, so what
// the host sees is what everyone sees.
//
// Both players are long-lived: switching tracks swaps the video inside the
// existing player instead of creating a new one. That matters in the
// background: a brand-new iframe or media element asks the browser for
// autoplay permission and gets refused in an unfocused tab, while a player
// that is already playing may keep going.
import { useEffect, useRef, useState } from 'react'
import { Play } from 'lucide-react'
import { positionMs } from '../../listen'
import type { PlaybackSession, PlaylistItem } from '../../lib/types'

declare global {
  interface Window {
    YT: any
    onYouTubeIframeAPIReady?: () => void
  }
}

// Seek when more than this far from the server position.
const DRIFT_SEC = 1.25
// Re-check drift this often while playing (browsers throttle this in
// background tabs; sync.state events still arrive promptly).
const TICK_MS = 4000

export interface PlayerProps {
  item: PlaylistItem
  session: PlaybackSession
  volume: number
  muted: boolean
  onEnded: (itemId: string) => void
  onDuration: (itemId: string, ms: number) => void
  // onSurfaceClick handles taps on the video. The surface is always covered
  // so the embedded player's own controls can't fight the shared state.
  onSurfaceClick?: () => void
}

// useLatest keeps a ref to the newest value so long-lived player event
// handlers never read stale closures.
function useLatest<T>(v: T) {
  const r = useRef(v)
  r.current = v
  return r
}

let ytLoading: Promise<any> | null = null
function loadYouTubeAPI(): Promise<any> {
  if (window.YT?.Player) return Promise.resolve(window.YT)
  if (!ytLoading) {
    ytLoading = new Promise((resolve) => {
      const prev = window.onYouTubeIframeAPIReady
      window.onYouTubeIframeAPIReady = () => {
        prev?.()
        resolve(window.YT)
      }
      const s = document.createElement('script')
      s.src = 'https://www.youtube.com/iframe_api'
      document.head.appendChild(s)
    })
  }
  return ytLoading
}

// BlockedOverlay appears when the browser refused to start playback without
// a tap (autoplay policy). The tap itself is the gesture that unblocks it.
function BlockedOverlay({ onTap }: { onTap: () => void }) {
  return (
    <button
      onClick={onTap}
      className="absolute inset-0 z-10 flex flex-col items-center justify-center gap-2 bg-black/70 text-white"
    >
      <span className="flex h-14 w-14 items-center justify-center rounded-full bg-accent text-accent-text">
        <Play size={26} className="ml-1" />
      </span>
      <span className="text-sm font-medium">Tap to join the playback</span>
    </button>
  )
}

function Surface({ onClick, title }: { onClick?: () => void; title?: string }) {
  return <div className="absolute inset-0 z-[5]" onClick={onClick} title={title} style={{ cursor: onClick ? 'pointer' : 'default' }} />
}

export function YouTubePlayer({ item, session, volume, muted, onEnded, onDuration, onSurfaceClick }: PlayerProps) {
  const hostRef = useRef<HTMLDivElement>(null)
  const playerRef = useRef<any>(null)
  const loadedIdRef = useRef<string>('')
  const [ready, setReady] = useState(false)
  const [blocked, setBlocked] = useState(false)
  const videoId = item.refs?.youtube ?? ''
  const onEndedRef = useLatest(onEnded)
  const onDurationRef = useLatest(onDuration)
  const itemRef = useLatest(item)
  const sessionRef = useLatest(session)

  // Create the player once for the life of the component.
  useEffect(() => {
    let cancelled = false
    loadYouTubeAPI().then((YT) => {
      if (cancelled || !hostRef.current) return
      const target = document.createElement('div')
      hostRef.current.innerHTML = ''
      hostRef.current.appendChild(target)
      loadedIdRef.current = itemRef.current.refs?.youtube ?? ''
      playerRef.current = new YT.Player(target, {
        videoId: loadedIdRef.current,
        width: '100%',
        height: '100%',
        playerVars: { autoplay: 0, controls: 0, disablekb: 1, rel: 0, playsinline: 1, origin: location.origin },
        events: {
          onReady: () => setReady(true),
          onStateChange: (e: any) => {
            if (e.data === YT.PlayerState.ENDED) onEndedRef.current(itemRef.current.id)
            if (e.data === YT.PlayerState.PLAYING) {
              setBlocked(false)
              const d = playerRef.current?.getDuration?.() ?? 0
              if (d > 0) onDurationRef.current(itemRef.current.id, d * 1000)
            }
          },
        },
      })
    })
    return () => {
      cancelled = true
      try {
        playerRef.current?.destroy?.()
      } catch {
        /* already gone */
      }
      playerRef.current = null
      setReady(false)
    }
  }, [])

  // Track change: swap the video inside the existing player.
  useEffect(() => {
    const p = playerRef.current
    if (!ready || !p || !videoId || loadedIdRef.current === videoId) return
    loadedIdRef.current = videoId
    const start = positionMs(sessionRef.current) / 1000
    if (sessionRef.current.state === 'playing') p.loadVideoById({ videoId, startSeconds: start })
    else p.cueVideoById({ videoId, startSeconds: start })
  }, [ready, videoId])

  // Follow the server transport state.
  useEffect(() => {
    if (!ready) return
    const p = playerRef.current
    if (!p) return
    const YT = window.YT
    const sync = () => {
      const target = positionMs(session) / 1000
      const cur = p.getCurrentTime?.() ?? 0
      const state = p.getPlayerState?.()
      if (Math.abs(cur - target) > DRIFT_SEC) p.seekTo(target, true)
      if (session.state === 'playing') {
        if (state !== YT.PlayerState.PLAYING && state !== YT.PlayerState.BUFFERING) p.playVideo()
      } else if (state === YT.PlayerState.PLAYING || state === YT.PlayerState.BUFFERING) {
        p.pauseVideo()
      }
    }
    sync()
    // If play didn't take within a moment, the browser needs a tap.
    const check = setTimeout(() => {
      const state = p.getPlayerState?.()
      if (session.state === 'playing' && state !== YT.PlayerState.PLAYING && state !== YT.PlayerState.BUFFERING) {
        setBlocked(true)
      }
    }, 2500)
    const t = setInterval(sync, TICK_MS)
    return () => {
      clearTimeout(check)
      clearInterval(t)
    }
  }, [ready, videoId, session.state, session.position_ms, session.anchor_server_ms, session.current_item_id])

  useEffect(() => {
    const p = playerRef.current
    if (!ready || !p) return
    p.setVolume?.(Math.round(volume * 100))
    if (muted) p.mute?.()
    else p.unMute?.()
  }, [ready, volume, muted])

  return (
    <div className="relative h-full w-full overflow-hidden bg-black">
      <div ref={hostRef} className="h-full w-full [&>iframe]:h-full [&>iframe]:w-full" />
      <Surface onClick={onSurfaceClick} title={onSurfaceClick ? undefined : 'The host controls playback'} />
      {blocked && (
        <BlockedOverlay
          onTap={() => {
            const p = playerRef.current
            p?.seekTo?.(positionMs(session) / 1000, true)
            p?.playVideo?.()
            setBlocked(false)
          }}
        />
      )}
    </div>
  )
}

// MediaPlayer plays uploaded audio/video through one long-lived media element.
export function MediaPlayer({ item, session, volume, muted, onEnded, onDuration, onSurfaceClick }: PlayerProps) {
  const ref = useRef<HTMLVideoElement>(null)
  const [blocked, setBlocked] = useState(false)
  const isAudio = (item.refs?.mime ?? '').startsWith('audio/')
  const src = item.attachment_id ? `/files/${item.attachment_id}/${encodeURIComponent(item.title || 'media')}` : ''
  const onEndedRef = useLatest(onEnded)
  const onDurationRef = useLatest(onDuration)
  const itemRef = useLatest(item)

  useEffect(() => {
    const el = ref.current
    if (!el) return
    const sync = () => {
      const target = positionMs(session) / 1000
      if (isFinite(target) && Math.abs(el.currentTime - target) > DRIFT_SEC) el.currentTime = target
      if (session.state === 'playing') {
        if (el.paused) {
          el.play().then(() => setBlocked(false)).catch(() => setBlocked(true))
        }
      } else if (!el.paused) {
        el.pause()
      }
    }
    sync()
    const t = setInterval(sync, TICK_MS)
    return () => clearInterval(t)
  }, [session.state, session.position_ms, session.anchor_server_ms, session.current_item_id, src])

  useEffect(() => {
    const el = ref.current
    if (!el) return
    el.volume = volume
    el.muted = muted
  }, [volume, muted])

  return (
    <div className="relative h-full w-full overflow-hidden bg-black">
      <video
        ref={ref}
        src={src}
        playsInline
        preload="auto"
        className={isAudio ? 'hidden' : 'h-full w-full object-contain'}
        onEnded={() => onEndedRef.current(itemRef.current.id)}
        onLoadedMetadata={(e) => {
          const d = e.currentTarget.duration
          if (isFinite(d) && d > 0) onDurationRef.current(itemRef.current.id, d * 1000)
        }}
      />
      {isAudio && (
        <div className="flex h-full items-center justify-center gap-3 px-4 text-white">
          <span className="text-3xl">🎵</span>
          <div className="min-w-0">
            <div className="truncate font-semibold">{item.title}</div>
            <div className="text-xs opacity-70">Uploaded audio</div>
          </div>
        </div>
      )}
      <Surface onClick={onSurfaceClick} title={onSurfaceClick ? undefined : 'The host controls playback'} />
      {blocked && (
        <BlockedOverlay
          onTap={() => {
            const el = ref.current
            if (!el) return
            el.currentTime = positionMs(session) / 1000
            el.play().then(() => setBlocked(false)).catch(() => {})
          }}
        />
      )}
    </div>
  )
}

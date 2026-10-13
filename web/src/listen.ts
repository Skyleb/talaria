// Phase 4 client: synchronized listening sessions. Server state arrives as
// sync.state events; local player code follows it (see components/listen).
import { create } from 'zustand'
import { api } from './lib/api'
import { useApp } from './store'
import type { ListenState, PlaybackSession, Playlist } from './lib/types'

interface ListenStore {
  // channelId -> live session state (entries exist only while a session runs)
  sessions: Record<string, ListenState>
  // Local playback preferences (never synced).
  volume: number
  muted: boolean
  // audioOnly shrinks the video to save bandwidth.
  audioOnly: boolean
  // queueOpen shows the queue drawer inside the call view.
  queueOpen: boolean

  setVolume: (v: number) => void
  setMuted: (m: boolean) => void
  setAudioOnly: (v: boolean) => void
  setQueueOpen: (v: boolean) => void

  start: (channelId: string, playlistId?: string) => Promise<void>
  join: (channelId: string) => Promise<void>
  leave: (channelId: string) => Promise<void>
  end: (channelId: string) => Promise<void>
  play: (channelId: string, positionMs?: number) => Promise<void>
  pause: (channelId: string, positionMs?: number) => Promise<void>
  seek: (channelId: string, positionMs: number) => Promise<void>
  select: (channelId: string, itemId: string) => Promise<void>
  next: (channelId: string, fromItemId?: string) => Promise<void>
  previous: (channelId: string) => Promise<void>
  addUrl: (channelId: string, url: string) => Promise<AddResult>
  addUpload: (channelId: string, attachmentId: string) => Promise<void>
  removeItem: (channelId: string, itemId: string) => Promise<void>
  transferHost: (channelId: string, userId: string) => Promise<void>
  save: (channelId: string, name: string) => Promise<Playlist>
  reportDuration: (channelId: string, itemId: string, durationMs: number) => void
}

function applyState(st: ListenState) {
  useListen.setState((s) => {
    const sessions = { ...s.sessions }
    if (st.session) sessions[st.channel_id] = st
    else delete sessions[st.channel_id]
    return { sessions }
  })
}

export interface AddResult {
  added?: number
  playlist_title?: string
}

async function postCmd(channelId: string, path: string, body: unknown = {}): Promise<AddResult> {
  const res = await api.post<{ state: ListenState } & AddResult>(`/api/v1/channels/${channelId}/listen${path}`, body)
  if (res?.state) applyState(res.state)
  return { added: res?.added, playlist_title: res?.playlist_title }
}

async function cmd(channelId: string, path: string, body: unknown = {}): Promise<void> {
  await postCmd(channelId, path, body)
}

export const useListen = create<ListenStore>((set) => ({
  sessions: {},
  volume: Number(localStorage.getItem('talaria-volume') ?? '0.8'),
  muted: false,
  audioOnly: localStorage.getItem('talaria-audio-only') === '1',
  queueOpen: false,

  setVolume: (v) => {
    localStorage.setItem('talaria-volume', String(v))
    set({ volume: v, muted: false })
  },
  setMuted: (m) => set({ muted: m }),
  setAudioOnly: (v) => {
    localStorage.setItem('talaria-audio-only', v ? '1' : '0')
    set({ audioOnly: v })
  },
  setQueueOpen: (v) => set({ queueOpen: v }),

  start: (c, playlistId) => cmd(c, '', playlistId ? { playlist_id: playlistId } : {}),
  join: (c) => cmd(c, '/join'),
  leave: (c) => cmd(c, '/leave'),
  end: (c) => cmd(c, '/end'),
  play: (c, positionMs) => cmd(c, '/play', positionMs === undefined ? {} : { position_ms: Math.round(positionMs) }),
  pause: (c, positionMs) => cmd(c, '/pause', positionMs === undefined ? {} : { position_ms: Math.round(positionMs) }),
  seek: (c, positionMs) => cmd(c, '/seek', { position_ms: Math.round(positionMs) }),
  select: (c, itemId) => cmd(c, '/select', { item_id: itemId }),
  next: (c, fromItemId) => cmd(c, '/next', fromItemId ? { from_item_id: fromItemId } : {}),
  previous: (c) => cmd(c, '/previous'),
  addUrl: (c, url) => postCmd(c, '/items', { url }),
  addUpload: (c, attachmentId) => cmd(c, '/items', { attachment_id: attachmentId }),
  removeItem: async (c, itemId) => {
    const res = await api.del<{ state: ListenState }>(`/api/v1/channels/${c}/listen/items/${itemId}`)
    if (res?.state) applyState(res.state)
  },
  transferHost: (c, userId) => cmd(c, '/host', { user_id: userId }),
  save: async (c, name) => {
    const res = await api.post<{ playlist: Playlist }>(`/api/v1/channels/${c}/listen/save`, { name })
    return res.playlist
  },
  reportDuration: (c, itemId, durationMs) => {
    api.post(`/api/v1/channels/${c}/listen/items/${itemId}/duration`, { duration_ms: Math.round(durationMs) }).catch(() => {})
  },
}))

// --- gateway hooks ---

export function handleListenEvent(t: string, d: any) {
  if (t === 'sync.state' && d?.channel_id) applyState(d as ListenState)
}

export function setActiveListening(states: any[]) {
  const sessions: Record<string, ListenState> = {}
  for (const st of states ?? []) if (st?.session) sessions[st.channel_id] = st
  useListen.setState({ sessions })
}

// --- clock helpers ---

// serverNow estimates the server clock using the gateway's ping offset.
export function serverNow(): number {
  return Date.now() + useApp.getState().clockOffsetMs
}

// positionMs is where the session is right now, per the server clock.
export function positionMs(session: PlaybackSession): number {
  if (session.state === 'playing') return session.position_ms + (serverNow() - session.anchor_server_ms)
  return session.position_ms
}

// listeningFor finds the session a user is currently in (for presence).
export function listeningFor(sessions: Record<string, ListenState>, userId: string): ListenState | null {
  for (const st of Object.values(sessions)) {
    if (st.session && st.members.includes(userId)) return st
  }
  return null
}

export function formatClock(ms: number): string {
  const total = Math.max(0, Math.floor(ms / 1000))
  const h = Math.floor(total / 3600)
  const m = Math.floor((total % 3600) / 60)
  const s = total % 60
  const mm = h > 0 ? String(m).padStart(2, '0') : String(m)
  return `${h > 0 ? h + ':' : ''}${mm}:${String(s).padStart(2, '0')}`
}

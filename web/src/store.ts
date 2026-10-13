import { create } from 'zustand'
import { api } from './lib/api'
import { applyTheme, DEFAULT_PALETTE } from './lib/palettes'
import type {
  Channel, FriendEntry, Group, GroupMember, GroupSummary, Message,
  ReadyPayload, User,
} from './lib/types'

export type Theme = 'light' | 'dark' | 'system'
export type GatewayStatus = 'connecting' | 'online' | 'offline'

interface AppState {
  me: User | null
  authChecked: boolean
  ready: boolean
  gatewayStatus: GatewayStatus
  clockOffsetMs: number

  users: Record<string, User>
  groups: Record<string, Group>
  channels: Record<string, Channel>
  groupChannelIds: Record<string, string[]>
  groupMembers: Record<string, GroupMember[]>
  dmChannelIds: string[]
  friends: FriendEntry[]
  readStates: Record<string, string>
  latestIds: Record<string, string>
  online: Record<string, true>
  typing: Record<string, Record<string, number>>

  messages: Record<string, Message[]>
  hasMore: Record<string, boolean>
  loaded: Record<string, boolean>

  theme: Theme
  palette: string
  // profileUserId drives the global user profile card (open from anywhere).
  profileUserId: string | null

  // actions
  setTheme: (t: Theme) => void
  setPalette: (p: string) => void
  openProfile: (userId: string) => void
  closeProfile: () => void
  setMe: (u: User | null) => void
  setGatewayStatus: (s: GatewayStatus) => void
  applyReady: (p: ReadyPayload) => void
  applyGroupCreate: (p: GroupSummary) => void
  loadMessages: (channelId: string, before?: string) => Promise<void>
  sendMessage: (channelId: string, content: string, attachmentIds: string[], replyToId?: string) => Promise<void>
  markRead: (channelId: string) => void
  pruneTyping: () => void
}

let nonceCounter = 0
const newNonce = () => `n${Date.now()}-${++nonceCounter}`

// sendRead is injected by the gateway module to push read updates over WS.
export let sendReadOverGateway: ((channelId: string, messageId: string) => boolean) | null = null
export function registerReadSender(fn: typeof sendReadOverGateway) {
  sendReadOverGateway = fn
}

export const useApp = create<AppState>((set, get) => ({
  me: null,
  authChecked: false,
  ready: false,
  gatewayStatus: 'connecting',
  clockOffsetMs: 0,

  users: {},
  groups: {},
  channels: {},
  groupChannelIds: {},
  groupMembers: {},
  dmChannelIds: [],
  friends: [],
  readStates: {},
  latestIds: {},
  online: {},
  typing: {},

  messages: {},
  hasMore: {},
  loaded: {},

  theme: (localStorage.getItem('talaria-theme') as Theme) || 'system',
  palette: localStorage.getItem('talaria-palette') || DEFAULT_PALETTE,
  profileUserId: null,

  openProfile: (userId) => set({ profileUserId: userId }),
  closeProfile: () => set({ profileUserId: null }),

  setTheme: (t) => {
    localStorage.setItem('talaria-theme', t)
    applyTheme(get().palette, t)
    set({ theme: t })
  },

  setPalette: (p) => {
    localStorage.setItem('talaria-palette', p)
    applyTheme(p, get().theme)
    set({ palette: p })
  },

  setMe: (u) => set({ me: u, authChecked: true }),
  setGatewayStatus: (s) => set({ gatewayStatus: s }),

  applyReady: (p) => {
    const users = { ...p.users }
    const groups: Record<string, Group> = {}
    const channels: Record<string, Channel> = {}
    const groupChannelIds: Record<string, string[]> = {}
    const groupMembers: Record<string, GroupMember[]> = {}
    for (const gs of p.groups ?? []) {
      groups[gs.group.id] = gs.group
      groupChannelIds[gs.group.id] = (gs.channels ?? []).map((c) => c.id)
      groupMembers[gs.group.id] = gs.members ?? []
      for (const c of gs.channels ?? []) channels[c.id] = c
    }
    const dmChannelIds: string[] = []
    for (const c of p.dm_channels ?? []) {
      channels[c.id] = c
      dmChannelIds.push(c.id)
    }
    const readStates: Record<string, string> = {}
    for (const r of p.read_states ?? []) {
      if (r.last_read_message_id) readStates[r.channel_id] = r.last_read_message_id
    }
    const online: Record<string, true> = {}
    for (const id of p.online_user_ids ?? []) online[id] = true
    set({
      me: p.user,
      authChecked: true,
      ready: true,
      users,
      groups,
      channels,
      groupChannelIds,
      groupMembers,
      dmChannelIds,
      friends: p.friends ?? [],
      readStates,
      latestIds: p.latest_message_ids ?? {},
      online,
      // Keep already-loaded messages across resumes; a 'reset' clears elsewhere.
    })
  },

  applyGroupCreate: (gs) => {
    set((st) => {
      const channels = { ...st.channels }
      for (const c of gs.channels ?? []) channels[c.id] = c
      return {
        groups: { ...st.groups, [gs.group.id]: gs.group },
        groupChannelIds: {
          ...st.groupChannelIds,
          [gs.group.id]: (gs.channels ?? []).map((c) => c.id),
        },
        groupMembers: { ...st.groupMembers, [gs.group.id]: gs.members ?? [] },
        channels,
      }
    })
  },

  loadMessages: async (channelId, before) => {
    const st = get()
    if (!before && st.loaded[channelId]) return
    const q = before ? `?before=${encodeURIComponent(before)}` : ''
    const res = await api.get<{ messages: Message[]; has_more: boolean }>(
      `/api/v1/channels/${channelId}/messages${q}`,
    )
    const incoming = [...(res.messages ?? [])].reverse() // server is desc; we keep asc
    set((s) => {
      const existing = s.messages[channelId] ?? []
      const seen = new Set(existing.map((m) => m.id))
      const merged = before
        ? [...incoming.filter((m) => !seen.has(m.id)), ...existing]
        : [...existing.filter((m) => !incoming.some((im) => im.id === m.id)), ...incoming]
      merged.sort((a, b) => (a.id < b.id ? -1 : 1))
      // Harvest author profiles we don't have yet.
      const users = { ...s.users }
      for (const m of res.messages ?? []) {
        if (m.author) users[m.author.id] = m.author
        if (m.reply_to?.author) users[m.reply_to.author.id] = m.reply_to.author
      }
      return {
        messages: { ...s.messages, [channelId]: merged },
        hasMore: { ...s.hasMore, [channelId]: res.has_more },
        loaded: { ...s.loaded, [channelId]: true },
        users,
      }
    })
  },

  sendMessage: async (channelId, content, attachmentIds, replyToId) => {
    const st = get()
    const me = st.me!
    const nonce = newNonce()
    const optimistic: Message = {
      id: `pending-${nonce}`,
      channel_id: channelId,
      author_id: me.id,
      kind: 'default',
      content,
      reply_to_id: replyToId,
      created_at: Date.now() + st.clockOffsetMs,
      author: me,
      nonce,
      pending: true,
    }
    set((s) => ({
      messages: {
        ...s.messages,
        [channelId]: [...(s.messages[channelId] ?? []), optimistic],
      },
    }))
    try {
      const res = await api.post<{ message: Message }>(
        `/api/v1/channels/${channelId}/messages`,
        { content, attachment_ids: attachmentIds, reply_to_id: replyToId || undefined, nonce },
      )
      // Replace the optimistic row (the WS echo may have already done this).
      set((s) => {
        const list = s.messages[channelId] ?? []
        const hasReal = list.some((m) => m.id === res.message.id)
        const next = list
          .filter((m) => m.nonce !== nonce || m.id === res.message.id)
          .map((m) => (m.id === res.message.id ? res.message : m))
        if (!hasReal && !next.some((m) => m.id === res.message.id)) next.push(res.message)
        next.sort((a, b) => (a.id < b.id ? -1 : 1))
        return { messages: { ...s.messages, [channelId]: next } }
      })
      get().markRead(channelId)
    } catch (err) {
      set((s) => ({
        messages: {
          ...s.messages,
          [channelId]: (s.messages[channelId] ?? []).map((m) =>
            m.nonce === nonce ? { ...m, pending: false, failed: true } : m,
          ),
        },
      }))
      throw err
    }
  },

  markRead: (channelId) => {
    const st = get()
    const latest = st.latestIds[channelId]
    if (!latest || st.readStates[channelId] === latest) return
    set((s) => ({ readStates: { ...s.readStates, [channelId]: latest } }))
    if (!sendReadOverGateway?.(channelId, latest)) {
      api.put(`/api/v1/channels/${channelId}/read`, { last_read_message_id: latest }).catch(() => {})
    }
  },

  pruneTyping: () => {
    const now = Date.now()
    set((s) => {
      const typing: typeof s.typing = {}
      let changed = false
      for (const [cid, users] of Object.entries(s.typing)) {
        const kept: Record<string, number> = {}
        for (const [uid, exp] of Object.entries(users)) {
          if (exp > now) kept[uid] = exp
          else changed = true
        }
        if (Object.keys(kept).length) typing[cid] = kept
      }
      return changed ? { typing } : {}
    })
  },
}))

// Apply the saved theme on load and keep it in sync with the OS setting.
applyTheme(useApp.getState().palette, useApp.getState().theme)
matchMedia('(prefers-color-scheme: dark)').addEventListener('change', () => {
  const { theme, palette } = useApp.getState()
  if (theme === 'system') applyTheme(palette, theme)
})

// Helpers used across components.
export function displayName(users: Record<string, User>, id: string): string {
  return users[id]?.display_name || users[id]?.username || 'Unknown'
}

export function dmTitle(ch: Channel, meId: string, users: Record<string, User>): string {
  const others = (ch.participants ?? []).filter((p) => p.id !== meId)
  if (others.length) return others.map((p) => p.display_name || p.username).join(', ')
  return 'Direct message'
}

export function dmOther(ch: Channel, meId: string): User | undefined {
  return (ch.participants ?? []).find((p) => p.id !== meId)
}

export function unreadCount(st: Pick<AppState, 'latestIds' | 'readStates' | 'me' | 'channels'>, channelId: string): boolean {
  const latest = st.latestIds[channelId]
  if (!latest) return false
  const read = st.readStates[channelId]
  return !read || read < latest
}

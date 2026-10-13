// Gateway: the single WebSocket connection with resume, heartbeat/clock-offset
// estimation (groundwork for Phase 4 synced playback), and store dispatch.
import { registerReadSender, useApp } from './store'
import { handleCallEvent, setActiveCalls, useCall } from './call'
import { handleListenEvent, setActiveListening } from './listen'
import type {
  Channel, FriendEntry, Friendship, Group, GroupMember, GroupSummary,
  Message, ReadyPayload, User,
} from './lib/types'

interface Frame {
  op: 'hello' | 'resume' | 'resumed' | 'reset' | 'evt' | 'cmd' | 'ping' | 'pong' | 'err'
  t?: string
  seq?: number
  n?: string
  d?: any
}

let ws: WebSocket | null = null
let resumeId = ''
let lastSeq = 0
let heartbeatMs = 25000
let heartbeatTimer: ReturnType<typeof setInterval> | null = null
let reconnectTimer: ReturnType<typeof setTimeout> | null = null
let backoff = 1000
let stopped = true
let offsetSamples: number[] = []

export function connectGateway() {
  stopped = false
  open()
  document.addEventListener('visibilitychange', onVisible)
  setInterval(() => useApp.getState().pruneTyping(), 2000)
}

export function disconnectGateway() {
  // Hang up first while the socket can still carry the leave command.
  useCall.getState().leave()
  stopped = true
  document.removeEventListener('visibilitychange', onVisible)
  cleanup()
  resumeId = ''
  lastSeq = 0
}

function onVisible() {
  // Mobile browsers kill background sockets; reconnect fast when foregrounded.
  if (document.visibilityState === 'visible' && stopped === false) {
    if (!ws || ws.readyState === WebSocket.CLOSED || ws.readyState === WebSocket.CLOSING) {
      if (reconnectTimer) clearTimeout(reconnectTimer)
      backoff = 1000
      open()
    }
  }
}

function cleanup() {
  if (heartbeatTimer) clearInterval(heartbeatTimer)
  heartbeatTimer = null
  if (reconnectTimer) clearTimeout(reconnectTimer)
  reconnectTimer = null
  if (ws) {
    ws.onclose = null
    ws.close()
    ws = null
  }
}

function scheduleReconnect() {
  if (stopped) return
  useApp.getState().setGatewayStatus('offline')
  if (reconnectTimer) return
  reconnectTimer = setTimeout(() => {
    reconnectTimer = null
    open()
  }, backoff + Math.random() * 500)
  backoff = Math.min(backoff * 2, 30000)
}

function open() {
  if (stopped) return
  cleanup()
  useApp.getState().setGatewayStatus('connecting')
  const proto = location.protocol === 'https:' ? 'wss:' : 'ws:'
  ws = new WebSocket(`${proto}//${location.host}/api/v1/gateway`)
  ws.onmessage = (ev) => {
    let f: Frame
    try {
      f = JSON.parse(ev.data)
    } catch {
      return
    }
    handleFrame(f)
  }
  ws.onclose = () => scheduleReconnect()
  ws.onerror = () => {
    /* onclose follows */
  }
}

function send(f: Frame) {
  if (ws && ws.readyState === WebSocket.OPEN) ws.send(JSON.stringify(f))
}

export function sendCommand(t: string, d: unknown): boolean {
  if (!ws || ws.readyState !== WebSocket.OPEN) return false
  send({ op: 'cmd', t, d })
  return true
}

registerReadSender((channelId, messageId) =>
  sendCommand('read.update', { channel_id: channelId, last_read_message_id: messageId }),
)

export function sendTyping(channelId: string) {
  sendCommand('typing.start', { channel_id: channelId })
}

function startHeartbeat() {
  if (heartbeatTimer) clearInterval(heartbeatTimer)
  const ping = () => send({ op: 'ping', d: { c: Date.now() } })
  heartbeatTimer = setInterval(ping, heartbeatMs)
  ping()
}

function handleFrame(f: Frame) {
  switch (f.op) {
    case 'hello': {
      const freshId = f.d?.session_resume_id ?? ''
      heartbeatMs = f.d?.heartbeat_ms ?? 25000
      if (resumeId) {
        send({ op: 'resume', d: { session_resume_id: resumeId, last_seq: lastSeq } })
      } else {
        resumeId = freshId
        lastSeq = 0
        send({ op: 'resume', d: { session_resume_id: '', last_seq: 0 } })
      }
      // Remember the fresh id in case our resume gets rejected (reset follows).
      pendingFreshId = freshId
      startHeartbeat()
      backoff = 1000
      break
    }
    case 'resumed':
      useApp.getState().setGatewayStatus('online')
      break
    case 'reset':
      // Our resume failed: adopt the new session; a `ready` event follows.
      resumeId = pendingFreshId
      lastSeq = 0
      break
    case 'pong': {
      const now = Date.now()
      const c = f.d?.c ?? now
      const s = f.d?.s ?? now
      const rtt = now - c
      const offset = s + rtt / 2 - now
      offsetSamples.push(offset)
      if (offsetSamples.length > 10) offsetSamples.shift()
      const sorted = [...offsetSamples].sort((a, b) => a - b)
      const median = sorted[Math.floor(sorted.length / 2)]
      useApp.setState({ clockOffsetMs: median })
      break
    }
    case 'evt':
      if (typeof f.seq === 'number') lastSeq = f.seq
      handleEvent(f.t!, f.d)
      break
  }
}

let pendingFreshId = ''

function handleEvent(t: string, d: any) {
  const st = useApp.getState()
  if (t.startsWith('call.') || t.startsWith('rtc.')) {
    handleCallEvent(t, d)
    return
  }
  if (t.startsWith('sync.')) {
    handleListenEvent(t, d)
    return
  }
  switch (t) {
    case 'ready':
      st.applyReady(d as ReadyPayload)
      st.setGatewayStatus('online')
      setActiveCalls(d.active_calls ?? [])
      setActiveListening(d.active_listening ?? [])
      break

    case 'message.create': {
      const m = d.message as Message
      useApp.setState((s) => {
        const users = m.author ? { ...s.users, [m.author.id]: m.author } : s.users
        const latestIds = { ...s.latestIds, [m.channel_id]: m.id }
        const list = s.messages[m.channel_id]
        if (!list) return { users, latestIds } // channel not loaded; unread badge only
        // Viewing an older window (after a jump): leave the gap until the
        // user scrolls down or jumps to latest, so nothing is skipped.
        if (s.hasMoreAfter[m.channel_id]) return { users, latestIds }
        if (list.some((x) => x.id === m.id)) return { users, latestIds }
        const next = m.nonce
          ? list.filter((x) => !(x.pending && x.nonce === m.nonce))
          : [...list]
        next.push(m)
        next.sort((a, b) => (a.id < b.id ? -1 : 1))
        // Message arrival clears that user's typing indicator.
        const typing = { ...s.typing }
        if (typing[m.channel_id]?.[m.author_id]) {
          const { [m.author_id]: _, ...rest } = typing[m.channel_id]
          typing[m.channel_id] = rest
        }
        return { users, latestIds, messages: { ...s.messages, [m.channel_id]: next }, typing }
      })
      break
    }

    case 'message.update': {
      const m = d.message as Message
      useApp.setState((s) => {
        const list = s.messages[m.channel_id]
        if (!list) return {}
        return {
          messages: {
            ...s.messages,
            [m.channel_id]: list.map((x) => (x.id === m.id ? { ...m, reply_to: x.reply_to } : x)),
          },
        }
      })
      break
    }

    case 'message.delete':
      useApp.setState((s) => {
        const list = s.messages[d.channel_id]
        if (!list) return {}
        return {
          messages: {
            ...s.messages,
            [d.channel_id]: list.map((x) =>
              x.id === d.message_id
                ? { ...x, deleted_at: Date.now(), content: '', attachments: [] }
                : x,
            ),
          },
        }
      })
      break

    case 'typing.start':
      if (d.user_id === st.me?.id) break
      useApp.setState((s) => ({
        typing: {
          ...s.typing,
          [d.channel_id]: {
            ...(s.typing[d.channel_id] ?? {}),
            [d.user_id]: Date.now() + (d.expires_ms ?? 10000),
          },
        },
      }))
      break

    case 'read.update':
      useApp.setState((s) => ({
        readStates: { ...s.readStates, [d.channel_id]: d.last_read_message_id },
      }))
      break

    case 'presence.update':
      useApp.setState((s) => {
        const online = { ...s.online }
        if (d.status === 'online') online[d.user_id] = true
        else delete online[d.user_id]
        return { online }
      })
      break

    case 'user.update': {
      const u = d.user as User
      useApp.setState((s) => ({ users: { ...s.users, [u.id]: u } }))
      break
    }

    case 'group.create':
      st.applyGroupCreate(d as GroupSummary)
      break

    case 'group.update': {
      const g = d.group as Group
      useApp.setState((s) => ({ groups: { ...s.groups, [g.id]: g } }))
      break
    }

    case 'group.delete':
      useApp.setState((s) => {
        const { [d.group_id]: _, ...groups } = s.groups
        const { [d.group_id]: chIds, ...groupChannelIds } = s.groupChannelIds
        const channels = { ...s.channels }
        for (const id of chIds ?? []) delete channels[id]
        const { [d.group_id]: __, ...groupMembers } = s.groupMembers
        return { groups, groupChannelIds, groupMembers, channels }
      })
      break

    case 'group.member_add': {
      const m = d.member as GroupMember
      useApp.setState((s) => ({
        groupMembers: {
          ...s.groupMembers,
          [d.group_id]: [...(s.groupMembers[d.group_id] ?? []).filter((x) => x.user_id !== m.user_id), m],
        },
        users: d.user ? { ...s.users, [d.user.id]: d.user } : s.users,
      }))
      break
    }

    case 'group.member_remove':
      if (d.user_id === st.me?.id) {
        // We were kicked or left: drop the whole group.
        handleEvent('group.delete', { group_id: d.group_id })
      } else {
        useApp.setState((s) => ({
          groupMembers: {
            ...s.groupMembers,
            [d.group_id]: (s.groupMembers[d.group_id] ?? []).filter((x) => x.user_id !== d.user_id),
          },
        }))
      }
      break

    case 'channel.create': {
      const c = d.channel as Channel
      useApp.setState((s) => ({
        channels: { ...s.channels, [c.id]: c },
        groupChannelIds: c.group_id
          ? {
              ...s.groupChannelIds,
              [c.group_id]: [...(s.groupChannelIds[c.group_id] ?? []).filter((id) => id !== c.id), c.id],
            }
          : s.groupChannelIds,
      }))
      break
    }

    case 'channel.update': {
      const c = d.channel as Channel
      useApp.setState((s) => ({ channels: { ...s.channels, [c.id]: c } }))
      break
    }

    case 'channel.delete':
      useApp.setState((s) => {
        const { [d.channel_id]: _, ...channels } = s.channels
        return {
          channels,
          dmChannelIds: s.dmChannelIds.filter((id) => id !== d.channel_id),
          groupChannelIds: d.group_id
            ? {
                ...s.groupChannelIds,
                [d.group_id]: (s.groupChannelIds[d.group_id] ?? []).filter((id) => id !== d.channel_id),
              }
            : s.groupChannelIds,
        }
      })
      break

    case 'dm.create': {
      const c = d.channel as Channel
      useApp.setState((s) => ({
        channels: { ...s.channels, [c.id]: c },
        dmChannelIds: s.dmChannelIds.includes(c.id) ? s.dmChannelIds : [c.id, ...s.dmChannelIds],
      }))
      break
    }

    case 'friend.request': {
      const f = d.friendship as Friendship
      const u = d.user as User | undefined
      useApp.setState((s) => ({
        friends: [
          ...s.friends.filter((x) => x.friendship.id !== f.id),
          { friendship: f, user_id: f.requester_id === s.me?.id ? f.addressee_id : f.requester_id },
        ],
        users: u ? { ...s.users, [u.id]: u } : s.users,
      }))
      break
    }

    case 'friend.update': {
      const f = d.friendship as Friendship
      const u = d.user as User | undefined
      useApp.setState((s) => {
        const meId = s.me?.id
        const otherId = f.requester_id === meId ? f.addressee_id : f.requester_id
        let friends: FriendEntry[]
        if (f.status === 'removed' || f.status === 'declined') {
          friends = s.friends.filter((x) => x.friendship.id !== f.id)
        } else {
          friends = [
            ...s.friends.filter((x) => x.friendship.id !== f.id && x.user_id !== otherId),
            { friendship: f, user_id: otherId },
          ]
        }
        return { friends, users: u ? { ...s.users, [u.id]: u } : s.users }
      })
      break
    }
  }
}

// Phase 2 client: call state + WebRTC against the embedded SFU.
//
// Two PeerConnections: `pub` (we offer; carries mic/cam/screen up) and `sub`
// (server offers; carries everyone else's tracks down). Downlink streams are
// named "<userId>|<kind>" by the server so tracks attribute to participants.
import { create } from 'zustand'
import { sendCommand } from './gateway'

export interface CallParticipant {
  user_id: string
  muted: boolean
  camera: boolean
  screen: boolean
}

export interface RemoteFeed {
  key: string // "<userId>|<kind>"
  userId: string
  kind: 'mic' | 'cam' | 'screen'
  stream: MediaStream
}

interface CallState {
  joinedChannelId: string | null
  connecting: boolean
  connected: boolean
  muted: boolean
  cameraOn: boolean
  sharing: boolean
  // minimized collapses the call UI to a floating pill so you can browse.
  minimized: boolean
  mediaError: string
  calls: Record<string, CallParticipant[]> // channelId -> live participants
  remoteFeeds: RemoteFeed[]
  localCamStream: MediaStream | null
  localScreenStream: MediaStream | null
  ring: { channelId: string; fromUserId: string } | null

  join: (channelId: string) => Promise<void>
  leave: () => void
  toggleMute: () => void
  toggleCamera: () => Promise<void>
  toggleScreen: () => Promise<void>
  setMinimized: (v: boolean) => void
  dismissRing: () => void
}

const ICE_CONFIG: RTCConfiguration = {
  iceServers: [{ urls: 'stun:stun.l.google.com:19302' }],
}

// --- connection internals (module scope, not reactive state) ---
let pubPC: RTCPeerConnection | null = null
let subPC: RTCPeerConnection | null = null
let pubPendingICE: RTCIceCandidateInit[] = []
let subPendingICE: RTCIceCandidateInit[] = []
let pubDirty = false
let streamMeta: Record<string, string> = {}
let micStream: MediaStream | null = null
let channelId = ''

function cleanupConnections() {
  pubPC?.close()
  subPC?.close()
  pubPC = subPC = null
  pubPendingICE = []
  subPendingICE = []
  pubDirty = false
  streamMeta = {}
  for (const s of [micStream, useCall.getState().localCamStream, useCall.getState().localScreenStream]) {
    s?.getTracks().forEach((t) => t.stop())
  }
  micStream = null
  channelId = ''
}

function ensurePubPC(): RTCPeerConnection {
  if (pubPC) return pubPC
  const pc = new RTCPeerConnection(ICE_CONFIG)
  pc.onicecandidate = (e) => {
    if (e.candidate) {
      sendCommand('rtc.ice', { channel_id: channelId, target: 'pub', candidate: e.candidate.toJSON() })
    }
  }
  pc.onnegotiationneeded = () => sendPubOffer()
  pubPC = pc
  return pc
}

async function sendPubOffer() {
  const pc = pubPC
  if (!pc || !channelId) return
  if (pc.signalingState !== 'stable') {
    pubDirty = true
    return
  }
  try {
    const offer = await pc.createOffer()
    await pc.setLocalDescription(offer)
    sendCommand('rtc.offer', { channel_id: channelId, sdp: pc.localDescription, meta: streamMeta })
  } catch (err) {
    console.error('pub offer failed', err)
  }
}

function watchConnection(pc: RTCPeerConnection) {
  pc.onconnectionstatechange = () => {
    const st = useCall.getState()
    if (!st.joinedChannelId) return
    if (pc.connectionState === 'connected') {
      useCall.setState({ connected: true, connecting: false })
    } else if (pc.connectionState === 'failed') {
      st.leave()
      useCall.setState({ mediaError: 'Call connection failed — check that UDP port 50000 is reachable.' })
    }
  }
}

// --- gateway event handlers (wired from gateway.ts) ---

export function handleCallEvent(t: string, d: any) {
  const st = useCall.getState()
  switch (t) {
    case 'call.state': {
      const participants: CallParticipant[] = d.participants ?? []
      useCall.setState((s) => ({
        calls: { ...s.calls, [d.channel_id]: participants },
        ring:
          s.ring?.channelId === d.channel_id && participants.length === 0 ? null : s.ring,
      }))
      // If the server dropped us (error or kick), reflect it locally.
      if (d.error && st.joinedChannelId === d.channel_id) {
        st.leave()
        useCall.setState({ mediaError: d.error })
      }
      break
    }
    case 'call.ring':
      if (st.joinedChannelId !== d.channel_id) {
        useCall.setState({ ring: { channelId: d.channel_id, fromUserId: d.from_user_id } })
      }
      break
    case 'rtc.answer': // answer to our pub offer
      if (pubPC && d.sdp) {
        pubPC
          .setRemoteDescription(d.sdp)
          .then(async () => {
            for (const c of pubPendingICE) await pubPC?.addIceCandidate(c).catch(() => {})
            pubPendingICE = []
            if (pubDirty) {
              pubDirty = false
              sendPubOffer()
            }
          })
          .catch((e) => console.error('pub answer', e))
      }
      break
    case 'rtc.offer': // subscriber offer from the server
      handleSubOffer(d)
      break
    case 'rtc.ice': {
      const cand: RTCIceCandidateInit = d.candidate
      if (d.target === 'pub') {
        if (pubPC?.remoteDescription) pubPC.addIceCandidate(cand).catch(() => {})
        else pubPendingICE.push(cand)
      } else {
        if (subPC?.remoteDescription) subPC.addIceCandidate(cand).catch(() => {})
        else subPendingICE.push(cand)
      }
      break
    }
  }
}

export function setActiveCalls(states: any[]) {
  const calls: Record<string, CallParticipant[]> = {}
  for (const s of states ?? []) calls[s.channel_id] = s.participants ?? []
  useCall.setState({ calls })
}

async function handleSubOffer(d: any) {
  if (!d.sdp || d.channel_id !== channelId) return
  if (!subPC) {
    const pc = new RTCPeerConnection(ICE_CONFIG)
    pc.onicecandidate = (e) => {
      if (e.candidate) {
        sendCommand('rtc.ice', { channel_id: channelId, target: 'sub', candidate: e.candidate.toJSON() })
      }
    }
    pc.ontrack = (e) => {
      const stream = e.streams[0]
      if (!stream) return
      const [userId, kind] = stream.id.split('|')
      if (!userId || !kind) return
      const key = stream.id
      useCall.setState((s) => ({
        remoteFeeds: [
          ...s.remoteFeeds.filter((f) => f.key !== key),
          { key, userId, kind: kind as RemoteFeed['kind'], stream },
        ],
      }))
      stream.onremovetrack = () => {
        if (stream.getTracks().length === 0) {
          useCall.setState((s) => ({ remoteFeeds: s.remoteFeeds.filter((f) => f.key !== key) }))
        }
      }
    }
    watchConnection(pc)
    subPC = pc
  }
  try {
    await subPC.setRemoteDescription(d.sdp)
    for (const c of subPendingICE) await subPC.addIceCandidate(c).catch(() => {})
    subPendingICE = []
    const answer = await subPC.createAnswer()
    await subPC.setLocalDescription(answer)
    sendCommand('rtc.answer', { channel_id: channelId, sdp: subPC.localDescription })
  } catch (err) {
    console.error('sub offer failed', err)
  }
}

function pushState() {
  const s = useCall.getState()
  sendCommand('call.update', { muted: s.muted, camera: s.cameraOn, screen: s.sharing })
}

export const useCall = create<CallState>((set, get) => ({
  joinedChannelId: null,
  connecting: false,
  connected: false,
  muted: false,
  cameraOn: false,
  sharing: false,
  minimized: false,
  mediaError: '',
  calls: {},
  remoteFeeds: [],
  localCamStream: null,
  localScreenStream: null,
  ring: null,

  join: async (chId) => {
    const st = get()
    if (st.joinedChannelId === chId) return
    if (st.joinedChannelId) st.leave()

    channelId = chId
    set({
      joinedChannelId: chId, connecting: true, connected: false,
      muted: false, cameraOn: false, sharing: false, minimized: false, mediaError: '',
      remoteFeeds: [], ring: st.ring?.channelId === chId ? null : st.ring,
    })
    sendCommand('call.join', { channel_id: chId })

    // Mic is best-effort: insecure origins (plain-HTTP LAN) and denied
    // permissions still get a listen-only seat in the call.
    try {
      if (!navigator.mediaDevices?.getUserMedia) {
        throw new Error('Microphone needs HTTPS (or localhost). You are connected listen-only.')
      }
      micStream = await navigator.mediaDevices.getUserMedia({ audio: true })
      const pc = ensurePubPC()
      watchConnection(pc)
      for (const track of micStream.getTracks()) {
        streamMeta[micStream.id] = 'mic'
        pc.addTrack(track, micStream)
      }
    } catch (err: any) {
      set({ mediaError: err?.message ?? 'Microphone unavailable — connected listen-only.', muted: true })
      pushState()
    }
  },

  leave: () => {
    if (!get().joinedChannelId) return
    sendCommand('call.leave', {})
    cleanupConnections()
    set({
      joinedChannelId: null, connecting: false, connected: false,
      muted: false, cameraOn: false, sharing: false, minimized: false,
      remoteFeeds: [], localCamStream: null, localScreenStream: null,
    })
  },

  setMinimized: (v) => set({ minimized: v }),

  toggleMute: () => {
    const next = !get().muted
    micStream?.getAudioTracks().forEach((t) => (t.enabled = !next))
    set({ muted: next })
    pushState()
  },

  toggleCamera: async () => {
    const st = get()
    if (st.cameraOn) {
      const stream = st.localCamStream
      if (stream && pubPC) {
        for (const sender of pubPC.getSenders()) {
          if (sender.track && stream.getTracks().includes(sender.track)) pubPC.removeTrack(sender)
        }
        delete streamMeta[stream.id]
      }
      stream?.getTracks().forEach((t) => t.stop())
      set({ cameraOn: false, localCamStream: null })
      pushState()
      return
    }
    try {
      const stream = await navigator.mediaDevices.getUserMedia({
        video: { width: { ideal: 1280 }, height: { ideal: 720 } },
      })
      const pc = ensurePubPC()
      watchConnection(pc)
      streamMeta[stream.id] = 'cam'
      for (const track of stream.getTracks()) pc.addTrack(track, stream)
      set({ cameraOn: true, localCamStream: stream })
      pushState()
    } catch (err: any) {
      set({ mediaError: err?.message ?? 'Camera unavailable' })
    }
  },

  toggleScreen: async () => {
    const st = get()
    if (st.sharing) {
      const stream = st.localScreenStream
      if (stream && pubPC) {
        for (const sender of pubPC.getSenders()) {
          if (sender.track && stream.getTracks().includes(sender.track)) pubPC.removeTrack(sender)
        }
        delete streamMeta[stream.id]
      }
      stream?.getTracks().forEach((t) => t.stop())
      set({ sharing: false, localScreenStream: null })
      pushState()
      return
    }
    try {
      const stream = await navigator.mediaDevices.getDisplayMedia({ video: true, audio: false })
      const pc = ensurePubPC()
      watchConnection(pc)
      streamMeta[stream.id] = 'screen'
      for (const track of stream.getTracks()) {
        pc.addTrack(track, stream)
        track.onended = () => {
          // Browser "stop sharing" button.
          if (get().sharing) get().toggleScreen()
        }
      }
      set({ sharing: true, localScreenStream: stream })
      pushState()
    } catch {
      /* user cancelled the picker */
    }
  },

  dismissRing: () => set({ ring: null }),
}))

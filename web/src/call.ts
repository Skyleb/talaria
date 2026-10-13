// Phase 2 client: call state + WebRTC against the embedded SFU.
//
// Two PeerConnections: `pub` (we offer; carries mic/cam/screen up) and `sub`
// (server offers; carries everyone else's tracks down). Downlink streams are
// named "<userId>|<kind>" by the server so tracks attribute to participants.
import { create } from 'zustand'
import { sendCommand } from './gateway'
import { mixer } from './audio/mixer'
import { createMicChain, type MicChain } from './audio/mic'
import { usePrefs, type MicTier } from './prefs'
import { MIC_TIERS, micConstraints, mungeOpusFmtp } from './media/encodings'

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
  // deafened silences everyone (and mutes you) without leaving.
  deafened: boolean
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
  // speaking: userId (or "me") -> currently talking, from the audio mixer.
  speaking: Record<string, boolean>

  join: (channelId: string) => Promise<void>
  leave: () => void
  toggleMute: () => void
  toggleDeafen: () => void
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
// rawMic is the device stream; micStream is what we send (after the
// high-pass / voice-isolation chain, when Web Audio is available).
let rawMic: MediaStream | null = null
let micChain: MicChain | null = null
let channelId = ''
// mutedByDeafen remembers that deafening is what muted the mic, so
// undeafening can give the mic back.
let mutedByDeafen = false

function setMicEnabled(on: boolean) {
  rawMic?.getAudioTracks().forEach((t) => (t.enabled = on))
  micStream?.getAudioTracks().forEach((t) => (t.enabled = on))
  mixer.setLocalMuted(!on)
}

let micTierInUse: MicTier = 'voice'

function buildMicChain(raw: MediaStream, tier: MicTier): MicChain | null {
  if (tier === 'music') return null
  try {
    const p = usePrefs.getState()
    return createMicChain(mixer.context(), raw, { highPass: p.highPass, voiceIsolation: p.voiceIsolation })
  } catch {
    return null
  }
}

function micSender(): RTCRtpSender | undefined {
  return pubPC?.getSenders().find((s) => s.track?.kind === 'audio')
}

// applyMicParams caps the sender's bitrate to the tier (the fmtp rewrite in
// the answer handles stereo and the encoder's own target).
function applyMicParams() {
  const sender = micSender()
  if (!sender) return
  const params = sender.getParameters()
  if (!params.encodings?.length) return
  const spec = MIC_TIERS[micTierInUse]
  params.encodings = params.encodings.map((e) => ({ ...e, maxBitrate: spec.maxBitrate }))
  sender.setParameters(params).catch(() => {})
}

// switchMicTier recaptures the microphone with the new tier's constraints
// mid-call, swaps the track without renegotiating, then re-offers so the
// answer's Opus parameters change too.
async function switchMicTier(tier: MicTier) {
  if (!rawMic || !pubPC || tier === micTierInUse) return
  const sender = micSender()
  if (!sender) return
  const wasEnabled = rawMic.getAudioTracks()[0]?.enabled ?? true
  let fresh: MediaStream
  try {
    fresh = await navigator.mediaDevices.getUserMedia({ audio: micConstraints(tier) })
  } catch {
    return
  }
  micChain?.close()
  rawMic.getTracks().forEach((t) => t.stop())
  rawMic = fresh
  micTierInUse = tier
  micChain = buildMicChain(rawMic, tier)
  micStream = micChain ? micChain.stream : rawMic
  const track = micStream.getAudioTracks()[0]
  if (track) {
    await sender.replaceTrack(track).catch(() => {})
    setMicEnabled(wasEnabled)
    mixer.attachLocal(micStream)
    applyMicParams()
  }
  sendPubOffer()
}

function cleanupConnections() {
  pubPC?.close()
  subPC?.close()
  pubPC = subPC = null
  pubPendingICE = []
  subPendingICE = []
  pubDirty = false
  streamMeta = {}
  micChain?.close()
  micChain = null
  for (const s of [rawMic, micStream, useCall.getState().localCamStream, useCall.getState().localScreenStream]) {
    s?.getTracks().forEach((t) => t.stop())
  }
  micStream = null
  rawMic = null
  channelId = ''
  mutedByDeafen = false
  mixer.attachLocal(null)
  mixer.setDeafened(false)
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
        // The encoder reads Opus parameters (stereo, bitrate) from the remote
        // description, so the tier is applied by rewriting the answer.
        const answer: RTCSessionDescriptionInit = { type: d.sdp.type, sdp: mungeOpusFmtp(d.sdp.sdp ?? '', usePrefs.getState().micTier) }
        pubPC
          .setRemoteDescription(answer)
          .then(async () => {
            for (const c of pubPendingICE) await pubPC?.addIceCandidate(c).catch(() => {})
            pubPendingICE = []
            applyMicParams()
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
  deafened: false,
  cameraOn: false,
  sharing: false,
  minimized: false,
  mediaError: '',
  calls: {},
  remoteFeeds: [],
  localCamStream: null,
  localScreenStream: null,
  ring: null,
  speaking: {},

  join: async (chId) => {
    const st = get()
    if (st.joinedChannelId === chId) return
    if (st.joinedChannelId) st.leave()

    channelId = chId
    set({
      joinedChannelId: chId, connecting: true, connected: false,
      muted: false, deafened: false, cameraOn: false, sharing: false, minimized: false, mediaError: '',
      remoteFeeds: [], speaking: {}, ring: st.ring?.channelId === chId ? null : st.ring,
    })
    sendCommand('call.join', { channel_id: chId })

    // Joining is always a tap/click, which is the gesture the audio graph
    // needs to be allowed to make sound.
    const prefs = usePrefs.getState()
    mixer.setLeveler(prefs.leveler)
    mixer.setDucking(prefs.ducking)
    mixer.setLocalMuted(false)
    mixer.ensureStarted().catch(() => {})

    // Mic is best-effort: insecure origins (plain-HTTP LAN) and denied
    // permissions still get a listen-only seat in the call.
    try {
      if (!navigator.mediaDevices?.getUserMedia) {
        throw new Error('Microphone needs HTTPS (or localhost). You are connected listen-only.')
      }
      micStream = rawMic = await navigator.mediaDevices.getUserMedia({ audio: micConstraints(prefs.micTier) })
      micTierInUse = prefs.micTier
      // Outgoing chain: high-pass + optional voice isolation (voice tiers
      // only; the music tier sends the untouched stereo capture). If Web
      // Audio is unavailable the raw device track is sent as-is.
      micChain = buildMicChain(rawMic, prefs.micTier)
      if (micChain) micStream = micChain.stream
      const pc = ensurePubPC()
      watchConnection(pc)
      for (const track of micStream.getTracks()) {
        streamMeta[micStream.id] = 'mic'
        pc.addTrack(track, micStream)
      }
      applyMicParams()
      mixer.attachLocal(micStream)
    } catch (err: any) {
      set({ mediaError: err?.message ?? 'Microphone unavailable — connected listen-only.', muted: true })
      mixer.setLocalMuted(true)
      pushState()
    }
  },

  leave: () => {
    if (!get().joinedChannelId) return
    sendCommand('call.leave', {})
    cleanupConnections()
    set({
      joinedChannelId: null, connecting: false, connected: false,
      muted: false, deafened: false, cameraOn: false, sharing: false, minimized: false,
      remoteFeeds: [], localCamStream: null, localScreenStream: null, speaking: {},
    })
  },

  setMinimized: (v) => set({ minimized: v }),

  toggleMute: () => {
    const next = !get().muted
    setMicEnabled(!next)
    mutedByDeafen = false
    set({ muted: next })
    pushState()
  },

  toggleDeafen: () => {
    const st = get()
    const next = !st.deafened
    mixer.setDeafened(next)
    if (next && !st.muted) {
      // Deafening also mutes you; undeafening gives the mic back.
      setMicEnabled(false)
      mutedByDeafen = true
      set({ deafened: true, muted: true })
      pushState()
      return
    }
    if (!next && mutedByDeafen) {
      setMicEnabled(true)
      mutedByDeafen = false
      set({ deafened: false, muted: false })
      pushState()
      return
    }
    set({ deafened: next })
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

// The mixer reports who is talking; tiles light up from this.
mixer.onSpeaking((speaking) => {
  if (useCall.getState().joinedChannelId) useCall.setState({ speaking })
})

// Preference changes apply live to the running graph.
usePrefs.subscribe((p) => {
  mixer.setLeveler(p.leveler)
  mixer.setDucking(p.ducking)
  micChain?.update({ highPass: p.highPass, voiceIsolation: p.voiceIsolation })
  if (useCall.getState().joinedChannelId && p.micTier !== micTierInUse) switchMicTier(p.micTier)
})

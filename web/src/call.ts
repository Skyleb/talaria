// Phase 2 client: call state + WebRTC against the embedded SFU.
//
// Two PeerConnections: `pub` (we offer; carries mic/cam/screen up) and `sub`
// (server offers; carries everyone else's tracks down). Downlink streams are
// named "<userId>|<kind>" by the server so tracks attribute to participants.
import { create } from 'zustand'
import { sendCommand } from './gateway'
import { mixer } from './audio/mixer'
import { createMicChain, type MicChain } from './audio/mic'
import { usePrefs, isLowPower, type CamFacing, type MicTier } from './prefs'
import { MIC_TIERS, SCREEN_PRESETS, micConstraints, mungeOpusFmtp, camConstraints, camLadder, screenConstraints, screenLadder, preferVP8 } from './media/encodings'

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
  // layers: "userId|kind" -> the simulcast layer the server is sending us.
  layers: Record<string, { rid: string; bps: number }>
  // quality: "userId|kind" -> what we asked for ('auto' when unset).
  quality: Record<string, StreamQuality>
  focusKey: string | null

  join: (channelId: string) => Promise<void>
  leave: () => void
  toggleMute: () => void
  toggleDeafen: () => void
  toggleCamera: () => Promise<void>
  // flipCamera switches between the front and rear camera mid-call.
  flipCamera: () => Promise<void>
  toggleScreen: () => Promise<void>
  setMinimized: (v: boolean) => void
  dismissRing: () => void
  setStreamQuality: (key: string, q: StreamQuality) => void
  setFocus: (key: string | null) => void
}

export type StreamQuality = 'auto' | 'q' | 'h' | 'f' | 'off'

// Server upload caps (kbps), replaced by rtc.caps after joining.
let caps = { cam: 2500, screen: 4000 }

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
      reconnectAttempts = 0
    } else if (pc.connectionState === 'failed') {
      // Never hang up on the user for a network hiccup: rebuild the media
      // connections and keep the seat.
      reconnect('media connection failed')
    }
  }
}

// --- reconnection ---------------------------------------------------------
// The only things that end a call are hanging up or joining another call.
// Network trouble (Wi-Fi handoff, phone backgrounded, media path failure)
// is handled by rebuilding the PeerConnections while the server keeps the
// seat for us (it waits 30 s for a gateway reconnect, and `call.join` for a
// call we are already in only replaces dead media connections).
let reconnectAttempts = 0
let reconnecting = false

async function reconnect(reason: string) {
  const st = useCall.getState()
  if (!st.joinedChannelId || reconnecting) return
  reconnectAttempts++
  if (reconnectAttempts > 4) {
    st.leave()
    useCall.setState({ mediaError: 'Lost the call connection and could not get it back — check that UDP port 50000 is reachable.' })
    return
  }
  reconnecting = true
  console.warn('call: reconnecting —', reason)
  useCall.setState({ connecting: true, connected: false, remoteFeeds: [], layers: {} })
  try {
    pubPC?.close()
    subPC?.close()
    pubPC = subPC = null
    pubPendingICE = []
    subPendingICE = []
    pubDirty = false
    sendCommand('call.join', { channel_id: st.joinedChannelId })
    // Re-publish whatever we were sending.
    const pc = ensurePubPC()
    watchConnection(pc)
    const prefs = usePrefs.getState()
    if (micStream) {
      for (const track of micStream.getTracks()) pc.addTrack(track, micStream)
    }
    if (st.localCamStream) {
      for (const track of st.localCamStream.getTracks()) {
        const tx = pc.addTransceiver(track, { direction: 'sendonly', streams: [st.localCamStream], sendEncodings: camLadder(prefs.camPreset, caps.cam, isLowPower()) })
        preferVP8(tx)
      }
    }
    if (st.localScreenStream) {
      for (const track of st.localScreenStream.getTracks()) {
        if (track.kind === 'video') {
          const tx = pc.addTransceiver(track, { direction: 'sendonly', streams: [st.localScreenStream], sendEncodings: screenLadder(prefs.screenPreset, caps.screen) })
          preferVP8(tx)
        } else {
          pc.addTrack(track, st.localScreenStream)
        }
      }
    }
    if (micStream) applyMicParams()
    pushState()
  } finally {
    reconnecting = false
  }
  // Decay the attempt counter so one bad minute does not count forever.
  setTimeout(() => {
    reconnectAttempts = Math.max(0, reconnectAttempts - 1)
  }, 60_000)
}

// resync runs after the gateway reconnects: if the media connections are
// still healthy, just remind the server we are here; otherwise rebuild.
export function resyncCall() {
  const st = useCall.getState()
  if (!st.joinedChannelId) return
  const healthy = (pc: RTCPeerConnection | null) => !!pc && pc.connectionState === 'connected'
  if (healthy(pubPC) && healthy(subPC)) {
    sendCommand('call.join', { channel_id: st.joinedChannelId })
    pushState()
    pushSubscribe()
    return
  }
  reconnect('gateway reconnected with dead media connections')
}

// Closing the tab is a hang-up: tell the server now rather than making
// everyone wait for the media timeout.
window.addEventListener('pagehide', () => {
  if (useCall.getState().joinedChannelId) sendCommand('call.leave', {})
})

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
    case 'rtc.caps':
      if (d.channel_id === channelId) caps = { cam: d.cam_kbps || caps.cam, screen: d.screen_kbps || caps.screen }
      break
    case 'rtc.layers':
      if (d.channel_id === channelId) useCall.setState({ layers: d.streams ?? {} })
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
  layers: {},
  quality: {},
  focusKey: null,

  join: async (chId) => {
    const st = get()
    if (st.joinedChannelId === chId) return
    if (st.joinedChannelId) {
      // Switching calls is the one way (besides hanging up) to leave one.
      if (!window.confirm('Leave your current call and join this one?')) return
      st.leave()
    }
    reconnectAttempts = 0

    channelId = chId
    set({
      joinedChannelId: chId, connecting: true, connected: false,
      muted: false, deafened: false, cameraOn: false, sharing: false, minimized: false, mediaError: '',
      remoteFeeds: [], speaking: {}, layers: {}, quality: {}, focusKey: null, ring: st.ring?.channelId === chId ? null : st.ring,
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
      remoteFeeds: [], localCamStream: null, localScreenStream: null, speaking: {}, layers: {}, quality: {}, focusKey: null,
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
      const prefs = usePrefs.getState()
      const stream = await navigator.mediaDevices.getUserMedia({ video: camConstraints(prefs.camPreset, prefs.camFacing) })
      const pc = ensurePubPC()
      watchConnection(pc)
      streamMeta[stream.id] = 'cam'
      for (const track of stream.getTracks()) {
        // Simulcast: several sizes of the same picture; the server picks a
        // layer per viewer.
        const tx = pc.addTransceiver(track, {
          direction: 'sendonly',
          streams: [stream],
          sendEncodings: camLadder(prefs.camPreset, caps.cam, isLowPower()),
        })
        preferVP8(tx)
      }
      set({ cameraOn: true, localCamStream: stream })
      pushState()
    } catch (err: any) {
      set({ mediaError: err?.message ?? 'Camera unavailable' })
    }
  },

  flipCamera: async () => {
    const stream = get().localCamStream
    if (!stream || !pubPC) return
    const old = stream.getVideoTracks()[0]
    const sender = pubPC.getSenders().find((s) => s.track === old)
    if (!old || !sender) return
    const prefs = usePrefs.getState()
    const facing: CamFacing = prefs.camFacing === 'user' ? 'environment' : 'user'
    let fresh: MediaStream
    try {
      // Ask for that camera specifically; if the device has no such camera
      // (a laptop), take whatever it offers rather than failing.
      fresh = await navigator.mediaDevices
        .getUserMedia({ video: camConstraints(prefs.camPreset, facing, true) })
        .catch(() => navigator.mediaDevices.getUserMedia({ video: camConstraints(prefs.camPreset, facing) }))
    } catch (err: any) {
      set({ mediaError: err?.message ?? 'Could not switch camera' })
      return
    }
    const track = fresh.getVideoTracks()[0]
    if (!track) return
    // Swap the track inside the same MediaStream so the stream id (which
    // the server maps to "cam") and the local tile's element stay put. The
    // sender keeps its simulcast encodings; no renegotiation.
    await sender.replaceTrack(track).catch(() => {})
    stream.removeTrack(old)
    stream.addTrack(track)
    old.stop()
    // What the camera actually gave us decides the mirror, not what we asked.
    const got = track.getSettings().facingMode
    prefs.set({ camFacing: got === 'environment' || got === 'user' ? got : facing })
    set({ localCamStream: stream })
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
      // Ask for tab/system audio too (Chrome and Edge offer it; others
      // quietly give video only). Keep it raw: it is music/game sound, not
      // speech.
      const screenPreset = usePrefs.getState().screenPreset
      const spec = SCREEN_PRESETS[screenPreset]
      const stream = await navigator.mediaDevices.getDisplayMedia({
        video: screenConstraints(screenPreset),
        audio: { echoCancellation: false, noiseSuppression: false, autoGainControl: false },
      })
      const pc = ensurePubPC()
      watchConnection(pc)
      streamMeta[stream.id] = 'screen'
      const hint = spec.hint
      for (const track of stream.getTracks()) {
        if (track.kind === 'video') {
          // 'detail' keeps text sharp at a low frame rate; 'motion' favours
          // smooth movement (games, video).
          try {
            track.contentHint = hint
          } catch {
            /* unsupported */
          }
          const tx = pc.addTransceiver(track, {
            direction: 'sendonly',
            streams: [stream],
            sendEncodings: screenLadder(screenPreset, caps.screen),
          })
          preferVP8(tx)
          const params = tx.sender.getParameters()
          ;(params as any).degradationPreference = hint === 'detail' ? 'maintain-resolution' : 'maintain-framerate'
          tx.sender.setParameters(params).catch(() => {})
          track.onended = () => {
            // Browser "stop sharing" button.
            if (get().sharing) get().toggleScreen()
          }
        } else {
          pc.addTrack(track, stream)
        }
      }
      set({ sharing: true, localScreenStream: stream })
      pushState()
    } catch {
      /* user cancelled the picker */
    }
  },

  dismissRing: () => set({ ring: null }),

  setStreamQuality: (key, q) => set((s) => ({ quality: { ...s.quality, [key]: q } })),
  setFocus: (key) => set({ focusKey: key }),
}))

// --- subscriber preferences -------------------------------------------------
// Whenever what we are looking at changes, tell the server which layer we
// want for each stream: the viewer's explicit pick, Auto otherwise; hidden
// cameras (minimized view) are paused, the focused tile gets priority.
let subscribeTimer: number | null = null
function pushSubscribe() {
  if (subscribeTimer !== null) return
  subscribeTimer = window.setTimeout(() => {
    subscribeTimer = null
    const s = useCall.getState()
    if (!s.joinedChannelId) return
    const streams: Record<string, { quality: StreamQuality; priority: number }> = {}
    for (const f of s.remoteFeeds) {
      if (f.kind === 'mic') continue
      let quality: StreamQuality = s.quality[f.key] ?? 'auto'
      if (s.minimized && f.kind === 'cam' && quality !== 'off') quality = 'off'
      const priority = s.focusKey === f.key ? 2 : s.minimized ? 0 : 1
      streams[f.key] = { quality, priority }
    }
    sendCommand('rtc.subscribe', { channel_id: s.joinedChannelId, streams })
  }, 300)
}
useCall.subscribe((s, prev) => {
  if (s.remoteFeeds !== prev.remoteFeeds || s.minimized !== prev.minimized || s.focusKey !== prev.focusKey || s.quality !== prev.quality) {
    pushSubscribe()
  }
})

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

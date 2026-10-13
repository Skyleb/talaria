// mixer: one Web Audio graph for everything you hear in a call.
//
// Each remote microphone goes
//   source → user volume → makeup (slow AGC) → leveler → analyser → master → limiter → out
// so you can turn individual people up or down, nobody can be painfully
// loud (leveler + limiter), tiles can show who is speaking (analyser), and
// shared music can duck while someone talks.
//
// Browser notes: an AudioContext only runs after a user gesture, so
// ensureStarted() is called from the join click. WebKit (and old Chrome)
// only feed remote WebRTC tracks into Web Audio while the stream is also
// attached to a media element, hence the muted <audio> sinks in CallPanel.

type Feed = {
  key: string
  userId: string
  stream: MediaStream
  trackId: string
  // voice feeds get the leveler, speaking detection and ducking; screen
  // audio (music, games) only gets the person's volume and the limiter.
  voice: boolean
  source: MediaStreamAudioSourceNode
  userGain: GainNode
  makeup: GainNode
  leveler: DynamicsCompressorNode
  analyser: AnalyserNode
  buf: Float32Array<ArrayBuffer>
  loudFrames: number
  lastLoudAt: number
  speaking: boolean
  makeupDb: number
}

type Local = {
  source: MediaStreamAudioSourceNode
  analyser: AnalyserNode
  buf: Float32Array<ArrayBuffer>
  loudFrames: number
  lastLoudAt: number
  speaking: boolean
}

const TICK_MS = 50
const SPEAK_ON_DB = -45
const SPEAK_OFF_DB = -50
const SPEAK_HOLD_MS = 300
const AGC_TARGET_DB = -20
const AGC_STEP_DB = 0.5
const AGC_MIN_DB = -6
const AGC_MAX_DB = 12
const DUCK_ATTACK_MS = 80
const DUCK_RELEASE_MS = 800

let ctx: AudioContext | null = null
let master: GainNode | null = null
let limiter: DynamicsCompressorNode | null = null
let timer: number | null = null
const feeds = new Map<string, Feed>()
let local: Local | null = null
let localMuted = false
let levelerOn = true
let deafened = false
let ducking = { enabled: true, amount: 0.6 }
let duckGain = 1
const duckTargets = new Map<string, (gain: number) => void>()
const speakingSubs = new Set<(s: Record<string, boolean>) => void>()
let lastSpeakingJSON = ''

function dbToGain(db: number): number {
  return Math.pow(10, db / 20)
}

function getCtx(): AudioContext {
  if (ctx) return ctx
  const AC = window.AudioContext || (window as any).webkitAudioContext
  ctx = new AC() as AudioContext
  master = ctx.createGain()
  master.gain.value = deafened ? 0 : 1
  limiter = ctx.createDynamicsCompressor()
  limiter.threshold.value = -3
  limiter.knee.value = 0
  limiter.ratio.value = 20
  limiter.attack.value = 0.001
  limiter.release.value = 0.05
  master.connect(limiter)
  limiter.connect(ctx.destination)
  ctx.onstatechange = () => {
    if (ctx?.state === 'suspended' && document.visibilityState === 'visible') resume()
  }
  document.addEventListener('visibilitychange', () => {
    if (document.visibilityState === 'visible') resume()
  })
  startTicker()
  return ctx
}

function resume() {
  ctx?.resume().catch(() => {})
}

function applyLeveler(c: DynamicsCompressorNode) {
  if (levelerOn) {
    c.threshold.value = -24
    c.knee.value = 12
    c.ratio.value = 4
    c.attack.value = 0.01
    c.release.value = 0.25
  } else {
    bypass(c)
  }
}

// A 1:1 compressor is a pass-through.
function bypass(c: DynamicsCompressorNode) {
  c.threshold.value = 0
  c.knee.value = 0
  c.ratio.value = 1
}

function rmsDb(analyser: AnalyserNode, buf: Float32Array<ArrayBuffer>): number {
  analyser.getFloatTimeDomainData(buf)
  let sum = 0
  for (let i = 0; i < buf.length; i++) sum += buf[i] * buf[i]
  const rms = Math.sqrt(sum / buf.length)
  return rms > 0 ? 20 * Math.log10(rms) : -120
}

function detect(s: { loudFrames: number; lastLoudAt: number; speaking: boolean }, db: number, now: number) {
  if (db > SPEAK_ON_DB) {
    s.loudFrames++
    s.lastLoudAt = now
    if (s.loudFrames >= 2) s.speaking = true
  } else {
    s.loudFrames = 0
    if (db < SPEAK_OFF_DB && now - s.lastLoudAt > SPEAK_HOLD_MS) s.speaking = false
  }
}

function tick() {
  if (!ctx || ctx.state !== 'running') return
  const now = performance.now()
  const t = ctx.currentTime
  let anySpeaking = false
  const speaking: Record<string, boolean> = {}
  for (const f of feeds.values()) {
    if (!f.voice) continue
    const db = rmsDb(f.analyser, f.buf)
    detect(f, db, now)
    if (f.speaking) {
      anySpeaking = true
      speaking[f.userId] = true
      // Slow automatic gain: nudge quiet talkers up and loud ones down.
      if (levelerOn) {
        const measured = db + f.makeupDb
        if (measured < AGC_TARGET_DB - 1) f.makeupDb = Math.min(AGC_MAX_DB, f.makeupDb + AGC_STEP_DB)
        else if (measured > AGC_TARGET_DB + 1) f.makeupDb = Math.max(AGC_MIN_DB, f.makeupDb - AGC_STEP_DB)
        f.makeup.gain.setTargetAtTime(dbToGain(f.makeupDb), t, 0.1)
      }
    }
  }
  if (local) {
    const db = rmsDb(local.analyser, local.buf)
    detect(local, db, now)
    if (local.speaking && !localMuted) {
      anySpeaking = true
      speaking.me = true
    }
  }
  // Ducking envelope.
  const target = ducking.enabled && anySpeaking ? 1 - ducking.amount : 1
  const tau = target < duckGain ? DUCK_ATTACK_MS : DUCK_RELEASE_MS
  const k = 1 - Math.exp(-TICK_MS / tau)
  const next = duckGain + (target - duckGain) * k
  if (Math.abs(next - duckGain) > 0.002 || (next !== duckGain && Math.abs(next - target) < 0.002)) {
    duckGain = Math.abs(next - target) < 0.002 ? target : next
    for (const apply of duckTargets.values()) apply(duckGain)
  }
  const json = JSON.stringify(speaking)
  if (json !== lastSpeakingJSON) {
    lastSpeakingJSON = json
    for (const cb of speakingSubs) cb(speaking)
  }
}

function startTicker() {
  if (timer !== null) return
  timer = window.setInterval(tick, TICK_MS)
}

function volumeKey(userId: string) {
  return `talaria-user-volume:${userId}`
}

export const mixer = {
  // Create/resume the context. Call from a user gesture (joining a call).
  async ensureStarted(): Promise<void> {
    const c = getCtx()
    if (c.state !== 'running') await c.resume().catch(() => {})
  },

  // The shared context, for the outgoing mic chain.
  context(): AudioContext {
    return getCtx()
  },

  state(): 'running' | 'suspended' | 'off' {
    if (!ctx) return 'off'
    return ctx.state === 'running' ? 'running' : 'suspended'
  },

  attach(key: string, userId: string, stream: MediaStream, opts: { voice?: boolean } = {}) {
    const voice = opts.voice ?? true
    const track = stream.getAudioTracks()[0]
    if (!track) return
    const prev = feeds.get(key)
    if (prev) {
      // Same track already wired: nothing to do. A new audio track on the
      // same stream (screen audio arriving after the video) needs a fresh
      // source node, since a source only follows the track it was made with.
      if (prev.stream === stream && prev.trackId === track.id) return
      mixer.detach(key)
    }
    const c = getCtx()
    if (!master) return
    const source = c.createMediaStreamSource(stream)
    const userGain = c.createGain()
    userGain.gain.value = mixer.getUserVolume(userId)
    const makeup = c.createGain()
    const leveler = c.createDynamicsCompressor()
    if (voice) applyLeveler(leveler)
    else bypass(leveler) // shared screen sound is music/game audio: leave its dynamics alone
    const analyser = c.createAnalyser()
    analyser.fftSize = 512
    source.connect(userGain)
    userGain.connect(makeup)
    makeup.connect(leveler)
    leveler.connect(analyser)
    analyser.connect(master)
    feeds.set(key, {
      key, userId, stream, trackId: track.id, voice, source, userGain, makeup, leveler, analyser,
      buf: new Float32Array(analyser.fftSize) as Float32Array<ArrayBuffer>,
      loudFrames: 0, lastLoudAt: 0, speaking: false, makeupDb: 0,
    })
  },

  detach(key: string) {
    const f = feeds.get(key)
    if (!f) return
    feeds.delete(key)
    for (const n of [f.source, f.userGain, f.makeup, f.leveler, f.analyser]) {
      try {
        n.disconnect()
      } catch {
        /* already gone */
      }
    }
  },

  // Your own microphone, analysed only (never played back).
  attachLocal(stream: MediaStream | null) {
    if (local) {
      try {
        local.source.disconnect()
        local.analyser.disconnect()
      } catch {
        /* ignore */
      }
      local = null
    }
    if (!stream || stream.getAudioTracks().length === 0) return
    const c = getCtx()
    const source = c.createMediaStreamSource(stream)
    const analyser = c.createAnalyser()
    analyser.fftSize = 512
    source.connect(analyser)
    local = { source, analyser, buf: new Float32Array(analyser.fftSize) as Float32Array<ArrayBuffer>, loudFrames: 0, lastLoudAt: 0, speaking: false }
  },

  setLocalMuted(b: boolean) {
    localMuted = b
  },

  setUserVolume(userId: string, v: number) {
    const vol = Math.min(2, Math.max(0, v))
    localStorage.setItem(volumeKey(userId), String(vol))
    const t = ctx?.currentTime ?? 0
    for (const f of feeds.values()) {
      if (f.userId === userId) f.userGain.gain.setTargetAtTime(vol, t, 0.02)
    }
  },

  getUserVolume(userId: string): number {
    const raw = localStorage.getItem(volumeKey(userId))
    const v = raw === null ? 1 : Number(raw)
    return Number.isFinite(v) ? Math.min(2, Math.max(0, v)) : 1
  },

  setDeafened(b: boolean) {
    deafened = b
    if (master && ctx) master.gain.setTargetAtTime(b ? 0 : 1, ctx.currentTime, 0.02)
  },

  setLeveler(on: boolean) {
    levelerOn = on
    for (const f of feeds.values()) {
      if (!f.voice) continue
      applyLeveler(f.leveler)
      if (!on) {
        f.makeupDb = 0
        f.makeup.gain.setTargetAtTime(1, ctx?.currentTime ?? 0, 0.1)
      }
    }
  },

  setDucking(o: { enabled: boolean; amount: number }) {
    ducking = { enabled: o.enabled, amount: Math.min(0.95, Math.max(0, o.amount)) }
  },

  // Something that plays shared music registers to receive the duck gain (0..1).
  registerDuckTarget(id: string, apply: (gain: number) => void): () => void {
    duckTargets.set(id, apply)
    apply(duckGain)
    startTicker()
    return () => {
      duckTargets.delete(id)
    }
  },

  onSpeaking(cb: (s: Record<string, boolean>) => void): () => void {
    speakingSubs.add(cb)
    return () => {
      speakingSubs.delete(cb)
    }
  },
}

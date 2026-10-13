// mic: the outgoing microphone chain. The browser's echo cancellation,
// noise suppression and gain control run at capture; this adds a high-pass
// (rumble, handling noise, the low thump that feeds back through phone
// speakers) and an optional "voice isolation" mode that band-limits the mic
// to speech and gates silence, so a phone in the same room as a laptop
// stops howling and keyboard clatter mostly disappears.
//
// The processed track is what gets sent; the raw track is kept so mute and
// leave can act on the real device.

export interface MicOptions {
  highPass: boolean
  voiceIsolation: boolean
}

export interface MicChain {
  stream: MediaStream
  update(o: MicOptions): void
  close(): void
}

const HP_FREQ = 100 // Hz, always-on high-pass when enabled
const ISO_HP_FREQ = 180 // Hz, speech band lower edge
const ISO_LP_FREQ = 4200 // Hz, speech band upper edge; squeal sits above
const GATE_OPEN_DB = -48
const GATE_CLOSE_DB = -54
const GATE_HOLD_MS = 250
const GATE_TICK_MS = 20

// Constraints to request from getUserMedia for a voice call. Unknown keys
// such as voiceIsolation are ignored by browsers that lack them.
export function voiceConstraints(): MediaTrackConstraints {
  return {
    echoCancellation: true,
    noiseSuppression: true,
    autoGainControl: true,
    channelCount: 1,
    ...({ voiceIsolation: true } as object),
  }
}

export function createMicChain(ctx: AudioContext, raw: MediaStream, opts: MicOptions): MicChain {
  const source = ctx.createMediaStreamSource(raw)
  const hp = ctx.createBiquadFilter()
  hp.type = 'highpass'
  hp.Q.value = 0.707
  const lp = ctx.createBiquadFilter()
  lp.type = 'lowpass'
  lp.Q.value = 0.707
  const gate = ctx.createGain()
  const analyser = ctx.createAnalyser()
  analyser.fftSize = 512
  const buf = new Float32Array(analyser.fftSize) as Float32Array<ArrayBuffer>
  const dest = ctx.createMediaStreamDestination()

  source.connect(hp)
  hp.connect(lp)
  lp.connect(analyser)
  lp.connect(gate)
  gate.connect(dest)

  let current = opts
  let timer: number | null = null
  let lastOpenAt = 0
  let open = true

  function apply(o: MicOptions) {
    current = o
    const t = ctx.currentTime
    if (o.voiceIsolation) {
      hp.frequency.setTargetAtTime(ISO_HP_FREQ, t, 0.05)
      lp.frequency.setTargetAtTime(ISO_LP_FREQ, t, 0.05)
      if (timer === null) timer = window.setInterval(tickGate, GATE_TICK_MS)
    } else {
      hp.frequency.setTargetAtTime(o.highPass ? HP_FREQ : 10, t, 0.05)
      lp.frequency.setTargetAtTime(20000, t, 0.05)
      if (timer !== null) {
        clearInterval(timer)
        timer = null
      }
      open = true
      gate.gain.setTargetAtTime(1, t, 0.02)
    }
  }

  function tickGate() {
    analyser.getFloatTimeDomainData(buf)
    let sum = 0
    for (let i = 0; i < buf.length; i++) sum += buf[i] * buf[i]
    const rms = Math.sqrt(sum / buf.length)
    const db = rms > 0 ? 20 * Math.log10(rms) : -120
    const now = performance.now()
    const t = ctx.currentTime
    if (db > GATE_OPEN_DB) {
      lastOpenAt = now
      if (!open) {
        open = true
        gate.gain.setTargetAtTime(1, t, 0.005)
      }
    } else if (open && db < GATE_CLOSE_DB && now - lastOpenAt > GATE_HOLD_MS) {
      open = false
      gate.gain.setTargetAtTime(0, t, 0.15)
    }
  }

  apply(opts)

  return {
    stream: dest.stream,
    update: apply,
    close() {
      if (timer !== null) clearInterval(timer)
      for (const n of [source, hp, lp, gate, analyser]) {
        try {
          n.disconnect()
        } catch {
          /* ignore */
        }
      }
      dest.stream.getTracks().forEach((t) => t.stop())
      void current
    },
  }
}

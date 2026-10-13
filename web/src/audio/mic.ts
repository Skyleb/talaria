// mic: the outgoing microphone chain. The browser's echo cancellation,
// noise suppression and gain control run at capture; this adds a high-pass
// (rumble, handling noise, the low thump that feeds back through phone
// speakers) and "voice isolation": RNNoise, a neural noise suppressor that
// removes wind, traffic, fans and keyboard clatter far better than the
// browser's built-in filter. The processed track is what gets sent; the raw
// track is kept so mute and leave can act on the real device.
import { RnnoiseWorkletNode, loadRnnoise } from '@sapphi-red/web-noise-suppressor'
import rnnoiseWorkletPath from '@sapphi-red/web-noise-suppressor/rnnoiseWorklet.js?url'
import rnnoiseWasmPath from '@sapphi-red/web-noise-suppressor/rnnoise.wasm?url'
import rnnoiseSimdWasmPath from '@sapphi-red/web-noise-suppressor/rnnoise_simd.wasm?url'

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
const ISO_HP_FREQ = 150 // Hz, a little higher when isolating: wind lives down there

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

// RNNoise runs at 48 kHz; the mixer creates its context at that rate.
let wasmPromise: Promise<ArrayBuffer> | null = null
const moduleLoaded = new WeakSet<AudioContext>()

async function ensureRnnoise(ctx: AudioContext): Promise<ArrayBuffer | null> {
  if (ctx.sampleRate !== 48000 || !ctx.audioWorklet) return null
  try {
    wasmPromise ??= loadRnnoise({ url: rnnoiseWasmPath, simdUrl: rnnoiseSimdWasmPath })
    const binary = await wasmPromise
    if (!moduleLoaded.has(ctx)) {
      await ctx.audioWorklet.addModule(rnnoiseWorkletPath)
      moduleLoaded.add(ctx)
    }
    return binary
  } catch (err) {
    console.warn('voice isolation unavailable:', err)
    wasmPromise = null
    return null
  }
}

export function createMicChain(ctx: AudioContext, raw: MediaStream, opts: MicOptions): MicChain {
  const source = ctx.createMediaStreamSource(raw)
  const hp = ctx.createBiquadFilter()
  hp.type = 'highpass'
  hp.Q.value = 0.707
  // nsIn → (RNNoise, when on and loaded) → nsOut. Before the worklet loads,
  // or when isolation is off, nsIn feeds nsOut directly.
  const nsIn = ctx.createGain()
  const nsOut = ctx.createGain()
  const dest = ctx.createMediaStreamDestination()
  source.connect(hp)
  hp.connect(nsIn)
  nsIn.connect(nsOut)
  nsOut.connect(dest)

  let current = opts
  let rnnoise: RnnoiseWorkletNode | null = null
  let rnnoiseActive = false
  let closed = false

  function setRnnoise(on: boolean) {
    if (!rnnoise || on === rnnoiseActive) return
    rnnoiseActive = on
    try {
      if (on) {
        nsIn.disconnect(nsOut)
        nsIn.connect(rnnoise)
        rnnoise.connect(nsOut)
      } else {
        nsIn.disconnect(rnnoise)
        rnnoise.disconnect()
        nsIn.connect(nsOut)
      }
    } catch {
      /* graph already in the wanted shape */
    }
  }

  function apply(o: MicOptions) {
    current = o
    const t = ctx.currentTime
    const hpFreq = o.voiceIsolation ? ISO_HP_FREQ : o.highPass ? HP_FREQ : 10
    hp.frequency.setTargetAtTime(hpFreq, t, 0.05)
    if (o.voiceIsolation && !rnnoise) {
      ensureRnnoise(ctx).then((binary) => {
        if (!binary || closed || rnnoise) return
        try {
          rnnoise = new RnnoiseWorkletNode(ctx, { wasmBinary: binary, maxChannels: 1 })
        } catch (err) {
          console.warn('voice isolation node failed:', err)
          return
        }
        setRnnoise(current.voiceIsolation)
      })
    }
    setRnnoise(o.voiceIsolation)
  }

  apply(opts)

  return {
    stream: dest.stream,
    update: apply,
    close() {
      closed = true
      for (const n of [source, hp, nsIn, nsOut]) {
        try {
          n.disconnect()
        } catch {
          /* ignore */
        }
      }
      try {
        rnnoise?.disconnect()
        rnnoise?.destroy()
      } catch {
        /* ignore */
      }
      rnnoise = null
      dest.stream.getTracks().forEach((t) => t.stop())
    },
  }
}

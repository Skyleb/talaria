// encodings: what we ask the browser to capture and send.
//
// Microphone tiers pick capture constraints, the Opus parameters the encoder
// should use (set by rewriting the fmtp line in the server's answer, which is
// where Chrome's encoder reads them), and a sender bitrate cap. Camera and
// screen ladders describe the simulcast layers (Phase 4).
import type { CamPreset, MicTier, ScreenHint } from '../prefs'
import { voiceConstraints } from '../audio/mic'

export interface MicTierSpec {
  label: string
  hint: string
  maxBitrate: number // bps, sender cap
  stereo: boolean
  fmtp: Record<string, string>
}

export const MIC_TIERS: Record<MicTier, MicTierSpec> = {
  voice: {
    label: 'Voice (32 kbps)',
    hint: 'Clear speech, lowest data. Echo cancellation and noise suppression on.',
    maxBitrate: 32_000,
    stereo: false,
    fmtp: { stereo: '0', 'sprop-stereo': '0', maxaveragebitrate: '32000', useinbandfec: '1', usedtx: '1' },
  },
  balanced: {
    label: 'Balanced (64 kbps)',
    hint: 'Fuller voice, still processed for calls.',
    maxBitrate: 64_000,
    stereo: false,
    fmtp: { stereo: '0', 'sprop-stereo': '0', maxaveragebitrate: '64000', useinbandfec: '1', usedtx: '0' },
  },
  music: {
    label: 'Music (128 kbps stereo)',
    hint: 'Stereo, no echo cancellation or noise suppression — for instruments and singing. Use headphones or you will echo.',
    maxBitrate: 128_000,
    stereo: true,
    fmtp: { stereo: '1', 'sprop-stereo': '1', maxaveragebitrate: '128000', maxplaybackrate: '48000', useinbandfec: '1', usedtx: '0' },
  },
}

export function micConstraints(tier: MicTier): MediaTrackConstraints {
  if (tier === 'music') {
    return { echoCancellation: false, noiseSuppression: false, autoGainControl: false, channelCount: 2, sampleRate: 48000 }
  }
  return voiceConstraints()
}

// mungeOpusFmtp rewrites the Opus fmtp line(s) in an SDP so the encoder uses
// the tier's parameters. Existing parameters not in the tier are kept.
export function mungeOpusFmtp(sdp: string, tier: MicTier): string {
  const spec = MIC_TIERS[tier]
  const pts = [...sdp.matchAll(/^a=rtpmap:(\d+) opus\/48000(?:\/2)?\s*$/gim)].map((m) => m[1])
  if (pts.length === 0) return sdp
  const lines = sdp.split(/\r\n|\n/)
  const eol = sdp.includes('\r\n') ? '\r\n' : '\n'
  const seen = new Set<string>()
  const out: string[] = []
  for (const line of lines) {
    const m = /^a=fmtp:(\d+) (.*)$/.exec(line)
    if (m && pts.includes(m[1])) {
      seen.add(m[1])
      out.push(`a=fmtp:${m[1]} ${mergeParams(m[2], spec.fmtp)}`)
      continue
    }
    out.push(line)
  }
  // An answer without any fmtp for Opus: add one right after its rtpmap.
  for (const pt of pts) {
    if (seen.has(pt)) continue
    const idx = out.findIndex((l) => new RegExp(`^a=rtpmap:${pt} opus`, 'i').test(l))
    if (idx >= 0) out.splice(idx + 1, 0, `a=fmtp:${pt} ${mergeParams('', spec.fmtp)}`)
  }
  return out.join(eol)
}

function mergeParams(existing: string, override: Record<string, string>): string {
  const params = new Map<string, string>()
  for (const kv of existing.split(';')) {
    const [k, v] = kv.split('=')
    if (k?.trim()) params.set(k.trim(), (v ?? '').trim())
  }
  for (const [k, v] of Object.entries(override)) params.set(k, v)
  return [...params.entries()].map(([k, v]) => (v === '' ? k : `${k}=${v}`)).join(';')
}

// --- video (Phase 4) ---

export const CAM_PRESETS: Record<CamPreset, { label: string; width: number; height: number; fps: number; kbps: number }> = {
  '360p': { label: '360p', width: 640, height: 360, fps: 30, kbps: 600 },
  '720p': { label: '720p', width: 1280, height: 720, fps: 30, kbps: 1500 },
  '1080p': { label: '1080p', width: 1920, height: 1080, fps: 30, kbps: 2500 },
}

export function camConstraints(p: CamPreset): MediaTrackConstraints {
  const s = CAM_PRESETS[p]
  return { width: { ideal: s.width }, height: { ideal: s.height }, frameRate: { ideal: s.fps, max: s.fps } }
}

// camLadder: simulcast encodings, lowest first (rids q, h, f). Low-power
// devices get two layers.
export function camLadder(p: CamPreset, capKbps: number, lowPower: boolean): RTCRtpEncodingParameters[] {
  const top = Math.min(CAM_PRESETS[p].kbps, capKbps) * 1000
  if (p === '360p' || lowPower) {
    return [
      { rid: 'q', scaleResolutionDownBy: 2, maxBitrate: Math.min(150_000, top) },
      { rid: 'f', maxBitrate: top },
    ]
  }
  return [
    { rid: 'q', scaleResolutionDownBy: 4, maxBitrate: Math.min(top / 6, 250_000) },
    { rid: 'h', scaleResolutionDownBy: 2, maxBitrate: Math.min(top / 2.5, 900_000) },
    { rid: 'f', maxBitrate: top },
  ]
}

export function screenLadder(h: ScreenHint, capKbps: number): RTCRtpEncodingParameters[] {
  const top = Math.min(h === 'detail' ? 2500 : 4000, capKbps) * 1000
  const fps = h === 'detail' ? 10 : 30
  return [
    { rid: 'h', scaleResolutionDownBy: 2, maxBitrate: Math.min(top / 4, 800_000), maxFramerate: fps },
    { rid: 'f', maxBitrate: top, maxFramerate: fps },
  ]
}

// preferVP8 puts VP8 first for simulcast; VP9/AV1 would negotiate SVC, which
// the server's forwarder does not understand. Feature-detected.
export function preferVP8(tx: RTCRtpTransceiver): void {
  const caps = RTCRtpSender.getCapabilities?.('video')
  if (!caps || typeof tx.setCodecPreferences !== 'function') return
  const vp8 = caps.codecs.filter((c) => /vp8/i.test(c.mimeType))
  const rtx = caps.codecs.filter((c) => /rtx/i.test(c.mimeType))
  const rest = caps.codecs.filter((c) => !/vp8|rtx/i.test(c.mimeType))
  if (vp8.length === 0) return
  try {
    tx.setCodecPreferences([...vp8, ...rtx, ...rest])
  } catch {
    /* unsupported combination; browser default */
  }
}

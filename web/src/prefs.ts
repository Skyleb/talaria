// prefs: local audio/video preferences (never synced to the server).
import { create } from 'zustand'

export type MicTier = 'voice' | 'balanced' | 'music'
export type CamPreset = '360p' | '720p' | '1080p' | '1080p60' | '1440p' | '1440p60' | '2160p'
export type ScreenPreset = 'text' | 'motion' | 'motion60' | 'hires' | 'ultra'

export interface AVPrefs {
  micTier: MicTier
  camPreset: CamPreset
  screenPreset: ScreenPreset
  // highPass cuts rumble and handling noise from your mic.
  highPass: boolean
  // voiceIsolation band-limits your mic to speech and gates silence; it
  // stops phones howling in the same room as another device.
  voiceIsolation: boolean
  // leveler evens out how loud each person is; the limiter always runs.
  leveler: boolean
  // ducking lowers shared music while someone talks.
  ducking: { enabled: boolean; amount: number }
}

// v2: defaults changed (balanced mic, leveler and isolation off); older
// saved prefs are left behind on purpose so everyone gets the new defaults.
const KEY = 'talaria-av-prefs-v2'

export function isMobile(): boolean {
  return /Android|iPhone|iPad|iPod/i.test(navigator.userAgent)
}

// Phones and small laptops get the lighter camera default.
export function isLowPower(): boolean {
  if (isMobile()) return true
  return (navigator.hardwareConcurrency ?? 8) <= 4
}

const DEFAULTS: AVPrefs = {
  micTier: 'balanced',
  camPreset: isLowPower() ? '360p' : '720p',
  screenPreset: 'text',
  highPass: true,
  voiceIsolation: false,
  leveler: false,
  ducking: { enabled: true, amount: 0.6 },
}

function load(): AVPrefs {
  try {
    const saved = JSON.parse(localStorage.getItem(KEY) || 'null')
    if (saved && typeof saved === 'object') {
      return { ...DEFAULTS, ...saved, ducking: { ...DEFAULTS.ducking, ...(saved.ducking ?? {}) } }
    }
  } catch {
    /* defaults */
  }
  return DEFAULTS
}

export const usePrefs = create<AVPrefs & { set: (p: Partial<AVPrefs>) => void }>((set, get) => ({
  ...load(),
  set: (p) => {
    const next: AVPrefs = {
      micTier: p.micTier ?? get().micTier,
      camPreset: p.camPreset ?? get().camPreset,
      screenPreset: p.screenPreset ?? get().screenPreset,
      highPass: p.highPass ?? get().highPass,
      voiceIsolation: p.voiceIsolation ?? get().voiceIsolation,
      leveler: p.leveler ?? get().leveler,
      ducking: { ...get().ducking, ...(p.ducking ?? {}) },
    }
    localStorage.setItem(KEY, JSON.stringify(next))
    set(next)
  },
}))

// prefs: local audio/video preferences (never synced to the server).
import { create } from 'zustand'

export type MicTier = 'voice' | 'balanced' | 'music'
export type CamPreset = '360p' | '720p' | '1080p'
export type ScreenHint = 'detail' | 'motion'

export interface AVPrefs {
  micTier: MicTier
  camPreset: CamPreset
  screenHint: ScreenHint
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

const KEY = 'talaria-av-prefs'

export function isMobile(): boolean {
  return /Android|iPhone|iPad|iPod/i.test(navigator.userAgent)
}

// Phones and small laptops get the lighter camera default.
export function isLowPower(): boolean {
  if (isMobile()) return true
  return (navigator.hardwareConcurrency ?? 8) <= 4
}

const DEFAULTS: AVPrefs = {
  micTier: 'voice',
  camPreset: isLowPower() ? '360p' : '720p',
  screenHint: 'detail',
  highPass: true,
  voiceIsolation: isMobile(),
  leveler: true,
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
      screenHint: p.screenHint ?? get().screenHint,
      highPass: p.highPass ?? get().highPass,
      voiceIsolation: p.voiceIsolation ?? get().voiceIsolation,
      leveler: p.leveler ?? get().leveler,
      ducking: { ...get().ducking, ...(p.ducking ?? {}) },
    }
    localStorage.setItem(KEY, JSON.stringify(next))
    set(next)
  },
}))

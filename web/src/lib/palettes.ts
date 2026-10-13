// Color themes ("palettes"), inspired by the flat, bold look of N++: quiet
// one-hue surfaces with a single vivid accent. Each palette is generated from
// a few seed values so every theme stays consistent in both light and dark
// mode — and adding a new one is three lines.

export interface Tokens {
  'bg-0': string
  'bg-1': string
  'bg-2': string
  'bg-3': string
  'text-0': string
  'text-1': string
  'text-2': string
  accent: string
  'accent-hover': string
  'accent-text': string
  danger: string
  success: string
  warning: string
  border: string
}

interface PaletteDef {
  id: string
  label: string
  hue: number // surface hue
  sat: number // surface saturation (%): 0 = neutral grey, higher = tinted
  accent: string // light-mode accent (hex)
  accentDark: string // dark-mode accent (hex)
  light?: Partial<Tokens>
  dark?: Partial<Tokens>
}

const DEFS: PaletteDef[] = [
  { id: 'classic', label: 'Classic', hue: 230, sat: 10, accent: '#6457f5', accentDark: '#7b6ff6' },
  { id: 'nightgold', label: 'Night Gold', hue: 228, sat: 26, accent: '#a87b10', accentDark: '#ffd75e' },
  { id: 'midnight', label: 'Midnight Run', hue: 225, sat: 24, accent: '#d6455d', accentDark: '#ef5d75' },
  { id: 'jade', label: 'Jade', hue: 160, sat: 13, accent: '#13865d', accentDark: '#2fbd87' },
  { id: 'paper', label: 'Paper Trail', hue: 36, sat: 24, accent: '#b4652a', accentDark: '#d08748' },
  { id: 'blueprint', label: 'Blueprint', hue: 212, sat: 20, accent: '#1f6fd6', accentDark: '#4a90e8' },
  { id: 'synth', label: 'Synthwave', hue: 285, sat: 18, accent: '#b43fd6', accentDark: '#e06cff' },
  { id: 'ember', label: 'Ember', hue: 20, sat: 10, accent: '#d6500f', accentDark: '#f07a45' },
  { id: 'minty', label: 'Minty', hue: 170, sat: 16, accent: '#0e8f8f', accentDark: '#2cc5c5' },
  { id: 'rosebud', label: 'Rosebud', hue: 345, sat: 14, accent: '#d6457d', accentDark: '#ef6b9d' },
  { id: 'mono', label: 'Ink & Paper', hue: 0, sat: 0, accent: '#1a1a1a', accentDark: '#ededed' },
  { id: 'deepdive', label: 'Deep Dive', hue: 205, sat: 30, accent: '#0f7fbf', accentDark: '#38a8e8' },
  { id: 'citrus', label: 'Citrus', hue: 80, sat: 15, accent: '#5d8f0e', accentDark: '#96c93d' },
  { id: 'lavender', label: 'Lavender', hue: 265, sat: 14, accent: '#7a5fd6', accentDark: '#957ff0' },
  {
    id: 'terminal', label: 'Terminal', hue: 130, sat: 14, accent: '#0e8f3d', accentDark: '#2fd66d',
    dark: { 'bg-0': '#060a07', 'bg-1': '#0b130d', 'bg-2': '#081009', 'bg-3': '#13241a', 'text-0': '#b8f0c8', 'text-1': '#75b98a', 'text-2': '#4d7a5c', border: '#1d3a28' },
  },
  { id: 'dusk', label: 'Dusk', hue: 255, sat: 22, accent: '#8a5fd6', accentDark: '#f0a05e' },
]

const hsl = (h: number, s: number, l: number) => `hsl(${h} ${s}% ${l}%)`

// Perceived brightness (0..255) of a hex color, for picking readable text.
function brightness(hex: string): number {
  const n = parseInt(hex.slice(1), 16)
  const r = (n >> 16) & 255, g = (n >> 8) & 255, b = n & 255
  return (r * 299 + g * 587 + b * 114) / 1000
}

// Shift a hex color's lightness for hover states.
function shade(hex: string, delta: number): string {
  const n = parseInt(hex.slice(1), 16)
  const adj = (v: number) => Math.max(0, Math.min(255, Math.round(v + delta)))
  const r = adj((n >> 16) & 255), g = adj((n >> 8) & 255), b = adj(n & 255)
  return `#${((r << 16) | (g << 8) | b).toString(16).padStart(6, '0')}`
}

function variant(def: PaletteDef, dark: boolean): Tokens {
  const { hue: h, sat: s } = def
  const accent = dark ? def.accentDark : def.accent
  const base: Tokens = dark
    ? {
        'bg-0': hsl(h, s, 7),
        'bg-1': hsl(h, s, 11),
        'bg-2': hsl(h, s, 9),
        'bg-3': hsl(h, s, 17),
        'text-0': hsl(h, Math.min(s, 16), 92),
        'text-1': hsl(h, Math.min(s, 12), 68),
        'text-2': hsl(h, Math.min(s, 10), 49),
        accent,
        'accent-hover': shade(accent, 22),
        'accent-text': brightness(accent) > 150 ? '#101010' : '#ffffff',
        danger: '#ef5d75',
        success: '#3fbf7f',
        warning: '#e0b050',
        border: hsl(h, s, 20),
      }
    : {
        'bg-0': hsl(h, s, 95),
        'bg-1': hsl(h, Math.min(s, 30), 99),
        'bg-2': hsl(h, s, 91),
        'bg-3': hsl(h, s, 86),
        'text-0': hsl(h, Math.min(s, 25), 13),
        'text-1': hsl(h, Math.min(s, 12), 40),
        'text-2': hsl(h, Math.min(s, 10), 55),
        accent,
        'accent-hover': shade(accent, -22),
        'accent-text': brightness(accent) > 150 ? '#101010' : '#ffffff',
        danger: '#d6455d',
        success: '#2d9d68',
        warning: '#9a6a10',
        border: hsl(h, s, 84),
      }
  return { ...base, ...(dark ? def.dark : def.light) }
}

export interface Palette {
  id: string
  label: string
  light: Tokens
  dark: Tokens
}

export const PALETTES: Palette[] = DEFS.map((d) => ({
  id: d.id,
  label: d.label,
  light: variant(d, false),
  dark: variant(d, true),
}))

export const DEFAULT_PALETTE = 'classic'

export function getPalette(id: string): Palette {
  return PALETTES.find((p) => p.id === id) ?? PALETTES[0]
}

// applyTheme writes the active variant's tokens as CSS variables and keeps
// the .dark class (used by Tailwind's dark: variant) in sync. It also
// snapshots the variables to localStorage so index.html's inline script can
// restore them before first paint — no flash of the default theme.
export function applyTheme(paletteId: string, mode: 'light' | 'dark' | 'system') {
  const dark =
    mode === 'dark' || (mode === 'system' && matchMedia('(prefers-color-scheme: dark)').matches)
  const tokens = getPalette(paletteId)[dark ? 'dark' : 'light']
  const root = document.documentElement
  root.classList.toggle('dark', dark)
  for (const [k, v] of Object.entries(tokens)) root.style.setProperty(`--${k}`, v)
  localStorage.setItem('talaria-theme-vars', JSON.stringify({ dark, tokens }))
  document
    .querySelector(`meta[name="theme-color"]:not([media])`)
    ?.setAttribute('content', tokens['bg-1'])
}

// GifPicker: search Tenor and send a GIF with one tap. Shown as a popover
// above the composer; an empty search shows what is trending.
import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router'
import { Search, X } from 'lucide-react'
import { api } from '../../lib/api'
import { useApp } from '../../store'
import { Input } from '../ui'

export interface Gif {
  id: string
  title: string
  url: string
  preview: string
  width: number
  height: number
}

export function GifPicker({ open, onClose, onPick }: { open: boolean; onClose: () => void; onPick: (gif: Gif) => void }) {
  const me = useApp((s) => s.me)
  const navigate = useNavigate()
  const [enabled, setEnabled] = useState<boolean | null>(null)
  const [query, setQuery] = useState('')
  const [results, setResults] = useState<Gif[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')

  useEffect(() => {
    if (!open) return
    setQuery('')
    setError('')
    api.get<{ enabled: boolean }>('/api/v1/gifs/status').then((r) => setEnabled(r.enabled)).catch(() => setEnabled(false))
  }, [open])

  useEffect(() => {
    if (!open || enabled !== true) return
    const t = setTimeout(async () => {
      setLoading(true)
      setError('')
      try {
        const r = await api.get<{ results: Gif[] }>(`/api/v1/gifs/search?q=${encodeURIComponent(query.trim())}`)
        setResults(r.results ?? [])
      } catch (e: any) {
        setError(e?.message ?? 'Search failed')
        setResults([])
      } finally {
        setLoading(false)
      }
    }, 300)
    return () => clearTimeout(t)
  }, [open, enabled, query])

  useEffect(() => {
    if (!open) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [open, onClose])

  if (!open) return null
  return (
    <div className="absolute bottom-full left-2 right-2 z-20 mb-1 flex max-h-[26rem] flex-col rounded-xl border border-edge bg-bg-1 p-2 shadow-lg sm:left-4 sm:right-auto sm:w-96">
      <div className="flex items-center gap-2">
        <div className="relative flex-1">
          <Search size={15} className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-ink-2" />
          <Input
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Search GIFs"
            className="pl-9"
            autoFocus
            disabled={enabled !== true}
          />
        </div>
        <button onClick={onClose} title="Close" className="rounded-lg p-1.5 text-ink-2 hover:bg-bg-3 hover:text-ink">
          <X size={16} />
        </button>
      </div>

      <div className="mt-2 min-h-0 flex-1 overflow-y-auto">
        {enabled === false && (
          <div className="px-2 py-6 text-center text-sm text-ink-1">
            <p>GIF search isn't set up on this server yet.</p>
            {me?.is_admin ? (
              <p className="mt-2">
                It needs a free Tenor key.{' '}
                <button className="text-accent hover:underline" onClick={() => navigate('/app/settings')}>
                  Add one in Settings → Admin → Server configuration
                </button>
                .
              </p>
            ) : (
              <p className="mt-2 text-xs text-ink-2">Ask the server admin to turn it on.</p>
            )}
          </div>
        )}
        {enabled && error && <p className="px-2 py-4 text-center text-sm text-danger">{error}</p>}
        {enabled && !error && loading && results.length === 0 && (
          <p className="px-2 py-6 text-center text-sm text-ink-2">Loading…</p>
        )}
        {enabled && !error && !loading && results.length === 0 && (
          <p className="px-2 py-6 text-center text-sm text-ink-2">No GIFs found.</p>
        )}
        {enabled && results.length > 0 && (
          <div className="grid grid-cols-3 gap-1">
            {results.map((g) => (
              <button
                key={g.id}
                onClick={() => onPick(g)}
                title={g.title || 'Send this GIF'}
                className="overflow-hidden rounded-lg bg-bg-2 focus:outline-none focus:ring-2 focus:ring-accent"
              >
                <img src={g.preview} alt={g.title} loading="lazy" className="h-24 w-full object-cover" />
              </button>
            ))}
          </div>
        )}
      </div>
      {enabled && <p className="mt-1 text-right text-[10px] text-ink-2">Powered by Tenor</p>}
    </div>
  )
}

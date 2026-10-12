import { useEffect, useState } from 'react'
import { Check, Copy } from 'lucide-react'
import { api } from '../lib/api'
import { Button, cx, Input } from './ui'

interface BaseOption {
  label: string
  url: string
}

let cached: BaseOption[] | null = null

// useBaseUrls returns shareable server addresses, most widely reachable first:
// DDNS domain, LAN IP (from the server), then the current browser origin as a
// fallback when it isn't already one of those (e.g. localhost).
export function useBaseUrls(): BaseOption[] {
  const [opts, setOpts] = useState<BaseOption[]>(cached ?? [])
  useEffect(() => {
    if (cached) {
      setOpts(cached)
      return
    }
    api
      .get<{ urls: BaseOption[] | null }>('/api/v1/server/info')
      .then((r) => {
        const all = [...(r.urls ?? [])]
        if (!all.some((o) => o.url === location.origin)) {
          all.push({ label: 'This address', url: location.origin })
        }
        cached = all
        setOpts(all)
      })
      .catch(() => {
        cached = [{ label: 'This address', url: location.origin }]
        setOpts(cached)
      })
  }, [])
  return opts
}

// InviteLinkShare renders an invite link with a choice of which server address
// it points at (DDNS / LAN / current origin) and a copy button.
export function InviteLinkShare({ path }: { path: string }) {
  const opts = useBaseUrls()
  const [selected, setSelected] = useState<string | null>(null)
  const [copied, setCopied] = useState(false)
  const base = selected ?? opts[0]?.url ?? location.origin
  const url = base + path

  useEffect(() => setCopied(false), [url])

  return (
    <div className="space-y-2">
      {opts.length > 1 && (
        <div className="flex flex-wrap gap-1.5">
          {opts.map((o) => (
            <button
              key={o.url}
              type="button"
              title={o.url}
              onClick={() => setSelected(o.url)}
              className={cx(
                'rounded-full border px-3 py-1 text-xs font-medium',
                base === o.url
                  ? 'border-accent bg-accent/10 text-ink'
                  : 'border-edge text-ink-1 hover:bg-bg-3',
              )}
            >
              {o.label}
            </button>
          ))}
        </div>
      )}
      <div className="flex gap-2">
        <Input readOnly value={url} onFocus={(e) => e.target.select()} />
        <Button
          type="button"
          variant="ghost"
          onClick={() => {
            navigator.clipboard.writeText(url)
            setCopied(true)
          }}
        >
          {copied ? <Check size={16} /> : <Copy size={16} />} {copied ? 'Copied!' : 'Copy'}
        </Button>
      </div>
      {base.startsWith('http://') && opts.some((o) => o.url.startsWith('https://')) && (
        <p className="text-xs text-ink-2">This link only works for people on your local network.</p>
      )}
    </div>
  )
}

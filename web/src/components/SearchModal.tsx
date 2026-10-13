// SearchModal: find messages in this chat, this group, or everything you
// can see. Results jump straight to the message in its conversation.
import { useEffect, useMemo, useRef, useState } from 'react'
import { useNavigate } from 'react-router'
import { Hash, Search, X } from 'lucide-react'
import { api } from '../lib/api'
import { displayName, dmTitle, useApp } from '../store'
import { formatDateTime } from '../lib/time'
import { Avatar } from './Avatar'
import { Button, cx, Input, Spinner } from './ui'
import type { SearchHit } from '../lib/types'

type Scope = 'channel' | 'group' | 'all'

export function SearchModal() {
  const open = useApp((s) => s.searchOpen)
  const closeSearch = useApp((s) => s.closeSearch)
  if (!open) return null
  return <SearchDialog key={`${open.scope}:${open.id ?? ''}`} initialScope={open.scope} id={open.id} onClose={closeSearch} />
}

function SearchDialog({ initialScope, id, onClose }: { initialScope: Scope; id?: string; onClose: () => void }) {
  const channels = useApp((s) => s.channels)
  const groups = useApp((s) => s.groups)
  const users = useApp((s) => s.users)
  const me = useApp((s) => s.me)!
  const navigate = useNavigate()
  const [query, setQuery] = useState('')
  const [scope, setScope] = useState<Scope>(initialScope)
  const [results, setResults] = useState<SearchHit[]>([])
  const [hasMore, setHasMore] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const inputRef = useRef<HTMLInputElement>(null)

  // Work out which scopes make sense from where search was opened.
  const channel = initialScope === 'channel' && id ? channels[id] : undefined
  const groupId = initialScope === 'group' ? id : channel?.group_id
  const scopeId = (s: Scope) => (s === 'channel' ? channel?.id : s === 'group' ? groupId : undefined)
  const chips: { value: Scope; label: string }[] = []
  if (channel) chips.push({ value: 'channel', label: channel.kind === 'dm' ? 'This chat' : `#${channel.name}` })
  if (groupId && groups[groupId]) chips.push({ value: 'group', label: groups[groupId].name })
  chips.push({ value: 'all', label: 'Everything' })

  useEffect(() => {
    inputRef.current?.focus()
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && onClose()
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])

  const trimmed = query.trim()
  useEffect(() => {
    if (!trimmed) {
      setResults([])
      setHasMore(false)
      return
    }
    const t = setTimeout(() => run(false), 300)
    return () => clearTimeout(t)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [trimmed, scope])

  async function run(more: boolean) {
    setBusy(true)
    setError('')
    try {
      const params = new URLSearchParams({ q: trimmed, scope })
      const sid = scopeId(scope)
      if (sid) params.set('id', sid)
      if (more && results.length) params.set('before', results[results.length - 1].message.id)
      const r = await api.get<{ results: SearchHit[]; has_more: boolean }>(`/api/v1/search?${params}`)
      setResults(more ? [...results, ...r.results] : r.results)
      setHasMore(r.has_more)
    } catch (e: any) {
      setError(e?.message ?? 'Search failed')
    } finally {
      setBusy(false)
    }
  }

  function channelLabel(cid: string): { icon: React.ReactNode; text: string } {
    const c = channels[cid]
    if (!c) return { icon: <Hash size={12} />, text: 'a chat' }
    if (c.kind === 'dm') return { icon: null, text: dmTitle(c, me.id, users) }
    const g = c.group_id ? groups[c.group_id] : undefined
    return { icon: <Hash size={12} />, text: `${c.name}${g && scope !== 'group' && scope !== 'channel' ? ` · ${g.name}` : ''}` }
  }

  function go(hit: SearchHit) {
    const c = channels[hit.message.channel_id]
    const path = c ? (c.kind === 'dm' ? `/app/dm/${c.id}` : `/app/g/${c.group_id}/${c.id}`) : null
    if (!path) return
    onClose()
    navigate(`${path}?m=${encodeURIComponent(hit.message.id)}`)
  }

  const highlighted = useMemo(() => results.map((h) => renderSnippet(h.snippet)), [results])

  return (
    <div className="fixed inset-0 z-50 flex items-start justify-center bg-black/50 p-3 pt-[10vh]" onClick={onClose}>
      <div
        className="flex max-h-[80vh] w-full max-w-xl flex-col overflow-hidden rounded-2xl border border-edge bg-bg-1 shadow-xl"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center gap-2 border-b border-edge p-3">
          <div className="relative flex-1">
            <Search size={16} className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-ink-2" />
            <Input
              ref={inputRef}
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder="Search messages…"
              className="pl-9"
            />
          </div>
          <button onClick={onClose} title="Close" className="rounded-lg p-1.5 text-ink-1 hover:bg-bg-3 hover:text-ink">
            <X size={18} />
          </button>
        </div>
        {chips.length > 1 && (
          <div className="flex gap-1.5 border-b border-edge px-3 py-2">
            {chips.map((c) => (
              <button
                key={c.value}
                onClick={() => setScope(c.value)}
                className={cx(
                  'rounded-full px-3 py-1 text-xs font-medium',
                  scope === c.value ? 'bg-accent text-accent-text' : 'bg-bg-2 text-ink-1 hover:bg-bg-3',
                )}
              >
                {c.label}
              </button>
            ))}
          </div>
        )}
        <div className="min-h-0 flex-1 overflow-y-auto p-2">
          {!trimmed && (
            <p className="px-2 py-8 text-center text-sm text-ink-2">Type to search. Results show the message and where it was said.</p>
          )}
          {trimmed && !busy && results.length === 0 && !error && (
            <p className="px-2 py-8 text-center text-sm text-ink-2">Nothing matched.</p>
          )}
          {error && <p className="px-2 py-3 text-center text-sm text-danger">{error}</p>}
          {results.map((h, i) => {
            const m = h.message
            const author = m.author ?? users[m.author_id]
            const where = channelLabel(m.channel_id)
            return (
              <button
                key={m.id}
                onClick={() => go(h)}
                className="flex w-full items-start gap-3 rounded-xl px-2 py-2 text-left hover:bg-bg-3/60"
              >
                <Avatar user={author} size={32} />
                <div className="min-w-0 flex-1">
                  <div className="flex items-baseline gap-2 text-xs text-ink-2">
                    <span className="font-semibold text-ink">{author ? author.display_name || author.username : displayName(users, m.author_id)}</span>
                    <span className="flex items-center gap-0.5 truncate">
                      {where.icon}
                      {where.text}
                    </span>
                    <span className="ml-auto shrink-0">{formatDateTime(m.created_at)}</span>
                  </div>
                  <div className="mt-0.5 text-sm leading-snug text-ink-1 [&_mark]:rounded [&_mark]:bg-accent/25 [&_mark]:px-0.5 [&_mark]:text-ink">
                    {highlighted[i]}
                  </div>
                </div>
              </button>
            )
          })}
          {busy && (
            <div className="flex justify-center py-3">
              <Spinner className="h-5 w-5" />
            </div>
          )}
          {hasMore && !busy && (
            <div className="flex justify-center py-2">
              <Button variant="ghost" className="px-3 py-1.5 text-xs" onClick={() => run(true)}>
                Show more
              </Button>
            </div>
          )}
        </div>
      </div>
    </div>
  )
}

// renderSnippet converts the server's control-character match markers into
// <mark> elements without ever treating message text as HTML.
function renderSnippet(snippet: string): React.ReactNode[] {
  const out: React.ReactNode[] = []
  const parts = snippet.split('\x02')
  parts.forEach((part, i) => {
    if (i === 0) {
      if (part) out.push(part)
      return
    }
    const [hit, rest] = part.split('\x03')
    out.push(<mark key={i}>{hit}</mark>)
    if (rest) out.push(rest)
  })
  return out
}

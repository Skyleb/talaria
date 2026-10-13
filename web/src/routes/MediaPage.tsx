// MediaPage: every photo, video, audio clip and file shared in one chat, one
// group, or everywhere you can see — in a grid with filters and sorting.
// Rendered as a full-screen overlay (like Settings) at /app/media?scope=&id=.
import { Fragment, useCallback, useEffect, useMemo, useRef, useState, type PointerEvent as RPointerEvent } from 'react'
import { useNavigate, useSearchParams } from 'react-router'
import { Download, ExternalLink, FileText, Images, Info, MessageSquare, Music, Play, X } from 'lucide-react'
import { api, fileUrl, thumbUrl } from '../lib/api'
import { displayName, dmTitle, useApp } from '../store'
import { formatBytes, formatDateTime, formatDay, sameDay } from '../lib/time'
import { Avatar } from '../components/Avatar'
import { Button, cx, Input, Modal, Select, Spinner } from '../components/ui'
import { Lightbox, type LightboxItem } from '../components/chat/Lightbox'
import { PageHeader } from './AppShell'
import type { MediaItem, MediaResponse, MediaSort, MediaType, User } from '../lib/types'

type Scope = 'channel' | 'group' | 'all'
type Kind = 'image' | 'video' | 'audio' | 'file'

const TYPE_CHIPS: { value: MediaType; label: string }[] = [
  { value: '', label: 'All' },
  { value: 'image', label: 'Images' },
  { value: 'video', label: 'Videos' },
  { value: 'audio', label: 'Audio' },
  { value: 'file', label: 'Files' },
]

const SORTS: { value: MediaSort; label: string }[] = [
  { value: 'newest', label: 'Newest first' },
  { value: 'oldest', label: 'Oldest first' },
  { value: 'largest', label: 'Largest first' },
  { value: 'name', label: 'Name A–Z' },
]

function kindOf(mime: string): Kind {
  if (mime.startsWith('image/')) return 'image'
  if (mime.startsWith('video/')) return 'video'
  if (mime.startsWith('audio/')) return 'audio'
  return 'file'
}

function formatDuration(ms?: number): string {
  if (!ms) return ''
  const s = Math.round(ms / 1000)
  const m = Math.floor(s / 60)
  const h = Math.floor(m / 60)
  const mm = h ? String(m % 60).padStart(2, '0') : String(m)
  return `${h ? `${h}:` : ''}${mm}:${String(s % 60).padStart(2, '0')}`
}

export default function MediaPage({ onClose }: { onClose: () => void }) {
  const [params] = useSearchParams()
  const scope = (params.get('scope') as Scope) || 'all'
  const id = params.get('id') ?? undefined
  const navigate = useNavigate()
  const channels = useApp((s) => s.channels)
  const groups = useApp((s) => s.groups)
  const users = useApp((s) => s.users)
  const me = useApp((s) => s.me)!

  const [type, setType] = useState<MediaType>('')
  const [sort, setSort] = useState<MediaSort>('newest')
  const [uploader, setUploader] = useState('')
  const [query, setQuery] = useState('')
  const [needle, setNeedle] = useState('')
  const [items, setItems] = useState<MediaItem[]>([])
  const [next, setNext] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [people, setPeople] = useState<Record<string, User>>({})
  const [uploaders, setUploaders] = useState<User[]>([])
  const [viewIndex, setViewIndex] = useState<number | null>(null)
  const [detail, setDetail] = useState<MediaItem | null>(null)
  const [playing, setPlaying] = useState<MediaItem | null>(null)
  const seq = useRef(0)
  const scrollRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && viewIndex === null && !detail && !playing) onClose()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose, viewIndex, detail, playing])

  // Debounce the file-name filter.
  useEffect(() => {
    const t = setTimeout(() => setNeedle(query.trim()), 300)
    return () => clearTimeout(t)
  }, [query])

  const load = useCallback(
    async (cursor?: string) => {
      const mine = ++seq.current
      setBusy(true)
      setError('')
      try {
        const p = new URLSearchParams({ scope })
        if (id) p.set('id', id)
        if (type) p.set('type', type)
        if (sort !== 'newest') p.set('sort', sort)
        if (uploader) p.set('uploader', uploader)
        if (needle) p.set('q', needle)
        if (cursor) p.set('cursor', cursor)
        const r = await api.get<MediaResponse>(`/api/v1/media?${p}`)
        if (mine !== seq.current) return
        setItems((prev) => (cursor ? [...prev, ...r.items.filter((it) => !prev.some((x) => x.id === it.id))] : r.items))
        setNext(r.next_cursor)
        setPeople((prev) => ({ ...prev, ...r.users }))
      } catch (e: any) {
        if (mine === seq.current) setError(e?.message ?? 'Could not load media')
      } finally {
        if (mine === seq.current) setBusy(false)
      }
    },
    [scope, id, type, sort, uploader, needle],
  )

  // Any filter change starts over from the first page.
  useEffect(() => {
    setItems([])
    setNext('')
    scrollRef.current?.scrollTo({ top: 0 })
    load()
  }, [load])

  useEffect(() => {
    const p = new URLSearchParams({ scope })
    if (id) p.set('id', id)
    api
      .get<{ users: User[] }>(`/api/v1/media/uploaders?${p}`)
      .then((r) => setUploaders(r.users ?? []))
      .catch(() => {})
  }, [scope, id])

  function onScroll() {
    const el = scrollRef.current
    if (!el || !next || busy) return
    if (el.scrollHeight - el.scrollTop - el.clientHeight < 400) load(next)
  }

  const nameOf = (uid: string) => people[uid]?.display_name || people[uid]?.username || displayName(users, uid)

  const labelFor = useCallback(
    (channelId: string): string => {
      const c = channels[channelId]
      if (!c) return 'a chat'
      if (c.kind === 'dm') return dmTitle(c, me.id, users)
      const g = c.group_id ? groups[c.group_id] : undefined
      return `#${c.name}${g ? ` · ${g.name}` : ''}`
    },
    [channels, groups, users, me.id],
  )

  const subtitle = useMemo(() => {
    if (scope === 'channel' && id) return labelFor(id)
    if (scope === 'group' && id) return groups[id]?.name ?? 'this group'
    return 'Everything you can see'
  }, [scope, id, groups, labelFor])

  // Images and videos open in the gallery; the index maps into this subset.
  const viewable = useMemo(() => items.filter((it) => kindOf(it.mime) !== 'audio' && kindOf(it.mime) !== 'file'), [items])
  const lightboxItems: LightboxItem[] = useMemo(
    () => viewable.map((it) => ({ url: fileUrl(it), alt: it.file_name, kind: kindOf(it.mime) === 'video' ? 'video' : 'image' })),
    [viewable],
  )

  function open(it: MediaItem) {
    switch (kindOf(it.mime)) {
      case 'image':
      case 'video':
        setViewIndex(viewable.findIndex((v) => v.id === it.id))
        break
      case 'audio':
        setPlaying(it)
        break
      default:
        window.open(fileUrl(it), '_blank', 'noopener')
    }
  }

  function jump(it: MediaItem) {
    const c = channels[it.channel_id]
    if (!c || !it.message_id) return
    const path = c.kind === 'dm' ? `/app/dm/${c.id}` : `/app/g/${c.group_id}/${c.id}`
    navigate(`${path}?m=${encodeURIComponent(it.message_id)}`)
  }

  // Day headers only make sense in time order.
  const sections = useMemo(() => {
    const byDay = sort === 'newest' || sort === 'oldest'
    const out: { day?: number; items: MediaItem[] }[] = []
    for (const it of items) {
      const last = out[out.length - 1]
      if (!byDay) {
        if (last) last.items.push(it)
        else out.push({ items: [it] })
      } else if (last && last.day !== undefined && sameDay(last.day, it.created_at)) {
        last.items.push(it)
      } else {
        out.push({ day: it.created_at, items: [it] })
      }
    }
    return out
  }, [items, sort])

  const filtered = !!(type || uploader || needle)

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <PageHeader
        icon={<Images size={18} />}
        title="Media"
        subtitle={subtitle}
        right={
          <button onClick={onClose} title="Close (Esc)" className="rounded p-1.5 text-ink-1 hover:bg-bg-3 hover:text-ink">
            <X size={18} />
          </button>
        }
      />
      <div className="border-b border-edge bg-bg-1 px-3 py-2">
        <p className="mb-2 truncate text-xs text-ink-2 sm:hidden">{subtitle}</p>
        <div className="flex flex-wrap items-center gap-2">
          <div className="flex gap-1.5 overflow-x-auto">
            {TYPE_CHIPS.map((c) => (
              <button
                key={c.value}
                onClick={() => setType(c.value)}
                className={cx(
                  'shrink-0 rounded-full px-3 py-1 text-xs font-medium',
                  type === c.value ? 'bg-accent text-accent-text' : 'bg-bg-2 text-ink-1 hover:bg-bg-3',
                )}
              >
                {c.label}
              </button>
            ))}
          </div>
          <div className="flex flex-1 flex-wrap items-center gap-2 sm:ml-auto sm:flex-none">
            <div className="min-w-0 flex-1 sm:w-48 sm:flex-none">
              <Input value={query} onChange={(e) => setQuery(e.target.value)} placeholder="Filter by file name…" />
            </div>
            <div className="w-36">
              <Select value={uploader} onChange={(e) => setUploader(e.target.value)} title="Shared by">
                <option value="">Anyone</option>
                {uploaders.map((u) => (
                  <option key={u.id} value={u.id}>
                    {u.display_name || u.username}
                  </option>
                ))}
              </Select>
            </div>
            <div className="w-36">
              <Select value={sort} onChange={(e) => setSort(e.target.value as MediaSort)} title="Sort">
                {SORTS.map((s) => (
                  <option key={s.value} value={s.value}>
                    {s.label}
                  </option>
                ))}
              </Select>
            </div>
          </div>
        </div>
      </div>

      <div ref={scrollRef} onScroll={onScroll} className="min-h-0 flex-1 overflow-y-auto p-2 sm:p-3">
        {error && <p className="px-2 py-3 text-center text-sm text-danger">{error}</p>}
        {!busy && !error && items.length === 0 && (
          <p className="px-2 py-12 text-center text-sm text-ink-2">
            {filtered ? 'Nothing matches these filters.' : 'Nothing has been shared here yet.'}
          </p>
        )}
        <div className="grid grid-cols-3 gap-1.5 sm:grid-cols-4 lg:grid-cols-6 xl:grid-cols-8">
          {sections.map((sec, i) => (
            <Fragment key={sec.day ?? i}>
              {sec.day !== undefined && (
                <div className={cx('col-span-full px-1 text-xs font-medium text-ink-2', i > 0 && 'pt-3')}>{formatDay(sec.day)}</div>
              )}
              {sec.items.map((it) => (
                <Tile key={it.id} it={it} uploaderName={nameOf(it.uploader_id)} onOpen={open} onInfo={setDetail} />
              ))}
            </Fragment>
          ))}
        </div>
        {busy && (
          <div className="flex justify-center py-4">
            <Spinner className="h-5 w-5" />
          </div>
        )}
        {next && !busy && (
          <div className="flex justify-center py-3">
            <Button variant="ghost" className="px-3 py-1.5 text-xs" onClick={() => load(next)}>
              Show more
            </Button>
          </div>
        )}
      </div>

      {viewIndex !== null && lightboxItems.length > 0 && (
        <Lightbox
          items={lightboxItems}
          index={viewIndex}
          onIndexChange={setViewIndex}
          onClose={() => setViewIndex(null)}
        />
      )}

      {detail && (
        <Modal open title={detail.file_name} onClose={() => setDetail(null)}>
          <div className="flex items-center gap-3 text-sm">
            <Avatar user={people[detail.uploader_id] ?? users[detail.uploader_id]} size={36} />
            <div className="min-w-0">
              <div className="truncate font-medium text-ink">{nameOf(detail.uploader_id)}</div>
              <div className="truncate text-xs text-ink-2">
                {formatDateTime(detail.created_at)} · {formatBytes(detail.size_bytes)}
                {detail.duration_ms ? ` · ${formatDuration(detail.duration_ms)}` : ''}
                {detail.width && detail.height ? ` · ${detail.width}×${detail.height}` : ''}
              </div>
              <div className="truncate text-xs text-ink-2">
                {detail.source === 'queue' ? 'Listening queue · ' : ''}
                {labelFor(detail.channel_id)}
              </div>
            </div>
          </div>
          <div className="mt-4 flex flex-wrap gap-2">
            {detail.message_id && channels[detail.channel_id] && (
              <Button
                variant="primary"
                className="flex items-center gap-1.5 text-sm"
                onClick={() => {
                  setDetail(null)
                  jump(detail)
                }}
              >
                <MessageSquare size={16} /> Jump to message
              </Button>
            )}
            <Button
              variant="ghost"
              className="flex items-center gap-1.5 text-sm"
              onClick={() => {
                setDetail(null)
                open(detail)
              }}
            >
              <ExternalLink size={16} /> Open
            </Button>
            <a
              href={fileUrl(detail)}
              download={detail.file_name}
              className="flex items-center gap-1.5 rounded-xl px-3 py-2 text-sm text-ink-1 hover:bg-bg-3 hover:text-ink"
            >
              <Download size={16} /> Download
            </a>
          </div>
        </Modal>
      )}

      {playing && (
        <Modal open title={playing.file_name} onClose={() => setPlaying(null)}>
          <audio controls autoPlay src={fileUrl(playing)} className="w-full" />
          <p className="mt-2 text-xs text-ink-2">
            Shared by {nameOf(playing.uploader_id)} · {formatDateTime(playing.created_at)}
          </p>
        </Modal>
      )}
    </div>
  )
}

function Tile({
  it,
  uploaderName,
  onOpen,
  onInfo,
}: {
  it: MediaItem
  uploaderName: string
  onOpen: (it: MediaItem) => void
  onInfo: (it: MediaItem) => void
}) {
  const kind = kindOf(it.mime)
  // Long-press (touch) opens the details sheet; a mouse uses hover + right-click.
  const timer = useRef<number | null>(null)
  const longPressed = useRef(false)
  const cancel = () => {
    if (timer.current) {
      clearTimeout(timer.current)
      timer.current = null
    }
  }
  const press = {
    onPointerDown: (e: RPointerEvent) => {
      if (e.pointerType === 'mouse') return
      cancel()
      longPressed.current = false
      timer.current = window.setTimeout(() => {
        longPressed.current = true
        onInfo(it)
      }, 500)
    },
    onPointerUp: cancel,
    onPointerMove: cancel,
    onPointerCancel: cancel,
    onPointerLeave: cancel,
  }
  function click() {
    if (longPressed.current) {
      longPressed.current = false
      return
    }
    onOpen(it)
  }

  const Icon = kind === 'video' ? Play : kind === 'audio' ? Music : FileText
  return (
    <div
      className="group relative aspect-square select-none overflow-hidden rounded-lg bg-bg-2 [-webkit-touch-callout:none]"
      onContextMenu={(e) => {
        e.preventDefault()
        onInfo(it)
      }}
      {...press}
    >
      <button onClick={click} className="block h-full w-full text-left" title={it.file_name}>
        {kind === 'image' ? (
          <img
            src={thumbUrl(it, 320)}
            alt={it.file_name}
            loading="lazy"
            className="h-full w-full object-cover"
            onError={(e) => {
              // No thumbnail (format the server can't decode): show the original.
              const img = e.currentTarget
              if (img.dataset.fallback) return
              img.dataset.fallback = '1'
              img.src = fileUrl(it)
            }}
          />
        ) : (
          <div className={cx('flex h-full w-full flex-col items-center justify-center gap-1 p-2', kind === 'video' ? 'bg-black text-white' : 'text-ink-1')}>
            <Icon size={28} className={kind === 'video' ? 'text-white' : 'text-accent'} />
            <span className="w-full truncate text-center text-[11px]">{it.file_name}</span>
            <span className={cx('text-[10px]', kind === 'video' ? 'text-white/70' : 'text-ink-2')}>
              {kind === 'file' || !it.duration_ms ? formatBytes(it.size_bytes) : formatDuration(it.duration_ms)}
            </span>
          </div>
        )}
      </button>
      {it.mime === 'image/gif' && (
        <span className="pointer-events-none absolute left-1.5 top-1.5 rounded bg-black/60 px-1 text-[10px] font-semibold text-white">GIF</span>
      )}
      <div className="pointer-events-none absolute inset-x-0 bottom-0 hidden truncate bg-gradient-to-t from-black/70 to-transparent px-1.5 pb-1 pt-4 text-[11px] text-white group-hover:block">
        {uploaderName} · {formatDay(it.created_at)}
      </div>
      <button
        onClick={() => onInfo(it)}
        title="Details"
        className="absolute right-1 top-1 hidden rounded-full bg-black/60 p-1 text-white hover:bg-black/80 group-hover:block"
      >
        <Info size={14} />
      </button>
    </div>
  )
}

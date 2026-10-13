import { useEffect, useMemo, useRef, useState } from 'react'
import {
  ChevronDown, ChevronUp, Crown, Headphones, Link as LinkIcon, ListMusic, LogOut, Pause, Play,
  Plus, Save, SkipBack, SkipForward, Trash2, Upload, Volume2, VolumeX, X,
} from 'lucide-react'
import { uploadFile } from '../../lib/api'
import { displayName, useApp } from '../../store'
import { formatClock, positionMs, useListen } from '../../listen'
import { Avatar } from '../Avatar'
import { Button, cx, ErrorText, Input, Modal } from '../ui'
import { MediaPlayer, YouTubePlayer } from './players'
import type { PlaylistItem } from '../../lib/types'

// ListenPanel sits under the channel header. It renders nothing when no one
// is listening, a join banner when a session is running without you, and
// the full player + queue once you are in.
export function ListenPanel({ channelId }: { channelId: string }) {
  const st = useListen((s) => s.sessions[channelId])
  const me = useApp((s) => s.me)
  if (!st?.session || !me) return null
  const joined = st.members.includes(me.id)
  return joined ? <ActivePanel channelId={channelId} /> : <JoinBanner channelId={channelId} />
}

function JoinBanner({ channelId }: { channelId: string }) {
  const st = useListen((s) => s.sessions[channelId])!
  const join = useListen((s) => s.join)
  const users = useApp((s) => s.users)
  const current = st.items.find((i) => i.id === st.session!.current_item_id)
  return (
    <div className="flex items-center gap-3 border-b border-edge bg-accent/10 px-4 py-2">
      <Headphones size={18} className="shrink-0 text-accent" />
      <div className="min-w-0 flex-1 text-sm">
        <span className="font-medium text-ink">{displayName(users, st.session!.host_user_id)}</span>
        <span className="text-ink-1"> is listening{current ? ` to ${current.title}` : ''}</span>
        <span className="ml-2 text-xs text-ink-2">
          {st.members.length} {st.members.length === 1 ? 'person' : 'people'}
        </span>
      </div>
      <Button className="px-3 py-1.5 text-xs" onClick={() => join(channelId)}>
        <Headphones size={14} /> Listen along
      </Button>
    </div>
  )
}

function ActivePanel({ channelId }: { channelId: string }) {
  const st = useListen((s) => s.sessions[channelId])!
  const session = st.session!
  const me = useApp((s) => s.me)!
  const users = useApp((s) => s.users)
  const { leave, end, play, pause, seek, next, previous, select, removeItem, transferHost, reportDuration } =
    useListen.getState()
  const volume = useListen((s) => s.volume)
  const muted = useListen((s) => s.muted)
  const setVolume = useListen((s) => s.setVolume)
  const setMuted = useListen((s) => s.setMuted)
  const [collapsed, setCollapsed] = useState(false)
  const [addOpen, setAddOpen] = useState(false)
  const [saveOpen, setSaveOpen] = useState(false)
  const [error, setError] = useState('')

  const isHost = session.host_user_id === me.id
  const current = st.items.find((i) => i.id === session.current_item_id) ?? null
  const durationMs = current?.duration_ms ?? 0

  // Re-render twice a second while playing so the clock moves.
  const [, tick] = useState(0)
  useEffect(() => {
    if (session.state !== 'playing') return
    const t = setInterval(() => tick((n) => n + 1), 500)
    return () => clearInterval(t)
  }, [session.state])
  const pos = Math.min(positionMs(session), durationMs > 0 ? durationMs : Infinity)

  // Only the host advances the queue when a track ends; everyone else just
  // waits for the resulting state.
  const endedFor = useRef<string | null>(null)
  function onEnded(itemId: string) {
    if (!isHost || endedFor.current === itemId) return
    endedFor.current = itemId
    next(channelId, itemId).catch(() => {})
  }
  useEffect(() => {
    endedFor.current = null
  }, [session.current_item_id])

  async function guard(p: Promise<unknown>) {
    setError('')
    try {
      await p
    } catch (e: any) {
      setError(e?.message ?? 'That did not work')
    }
  }

  return (
    <div className="border-b border-edge bg-bg-2/70">
      {/* Title row */}
      <div className="flex items-center gap-2 px-3 pt-2">
        <Headphones size={16} className="shrink-0 text-accent" />
        <div className="min-w-0 flex-1 text-sm">
          <span className="font-semibold text-ink">{current ? current.title : 'Nothing queued yet'}</span>
          {current?.artist && <span className="ml-1.5 text-ink-2">{current.artist}</span>}
        </div>
        <Members channelId={channelId} isHost={isHost} onMakeHost={(uid) => guard(transferHost(channelId, uid))} />
        <button
          title={collapsed ? 'Show player' : 'Hide player'}
          onClick={() => setCollapsed((v) => !v)}
          className="rounded-lg p-1.5 text-ink-1 hover:bg-bg-3 hover:text-ink"
        >
          {collapsed ? <ChevronDown size={16} /> : <ChevronUp size={16} />}
        </button>
        <button
          title="Leave the listening session"
          onClick={() => guard(leave(channelId))}
          className="rounded-lg p-1.5 text-ink-1 hover:bg-bg-3 hover:text-danger"
        >
          <LogOut size={16} />
        </button>
      </div>

      {!collapsed && (
        <div className="grid gap-3 p-3 lg:grid-cols-[minmax(0,3fr)_minmax(0,2fr)]">
          {/* Player + transport */}
          <div className="space-y-2">
            {current ? (
              current.kind === 'video' && current.refs?.youtube ? (
                <YouTubePlayer
                  key={current.id}
                  item={current}
                  session={session}
                  volume={volume}
                  muted={muted}
                  onEnded={onEnded}
                  onDuration={(id, ms) => reportDuration(channelId, id, ms)}
                />
              ) : (
                <MediaPlayer
                  key={current.id}
                  item={current}
                  session={session}
                  volume={volume}
                  muted={muted}
                  onEnded={onEnded}
                  onDuration={(id, ms) => reportDuration(channelId, id, ms)}
                />
              )
            ) : (
              <div className="flex aspect-video items-center justify-center rounded-xl bg-bg-1 text-sm text-ink-2">
                Add a YouTube link or upload a song to get started.
              </div>
            )}

            {/* Progress */}
            <div className="flex items-center gap-2 text-xs tabular-nums text-ink-2">
              <span>{formatClock(pos)}</span>
              <input
                type="range"
                min={0}
                max={durationMs > 0 ? durationMs : Math.max(pos, 1)}
                value={Math.min(pos, durationMs > 0 ? durationMs : pos)}
                disabled={!isHost || !current}
                onChange={(e) => guard(seek(channelId, Number(e.target.value)))}
                className="h-1.5 flex-1 accent-[var(--accent)] disabled:opacity-60"
                aria-label="Playback position"
              />
              <span>{durationMs > 0 ? formatClock(durationMs) : '--:--'}</span>
            </div>

            {/* Transport */}
            <div className="flex items-center gap-2">
              {isHost ? (
                <>
                  <Ctl title="Previous" onClick={() => guard(previous(channelId))} disabled={!current}>
                    <SkipBack size={16} />
                  </Ctl>
                  <button
                    title={session.state === 'playing' ? 'Pause for everyone' : 'Play for everyone'}
                    disabled={!current}
                    onClick={() => guard(session.state === 'playing' ? pause(channelId) : play(channelId))}
                    className="rounded-full bg-accent p-2.5 text-accent-text hover:bg-accent-hover disabled:opacity-50"
                  >
                    {session.state === 'playing' ? <Pause size={18} /> : <Play size={18} className="ml-0.5" />}
                  </button>
                  <Ctl title="Next" onClick={() => guard(next(channelId))} disabled={!current}>
                    <SkipForward size={16} />
                  </Ctl>
                </>
              ) : (
                <span className="flex items-center gap-1.5 text-xs text-ink-2">
                  <Crown size={13} className="text-warning" />
                  {displayName(users, session.host_user_id)} is driving
                  {session.state === 'paused' && ' · paused'}
                </span>
              )}
              <div className="ml-auto flex items-center gap-1.5">
                <button
                  title={muted ? 'Unmute' : 'Mute (just for you)'}
                  onClick={() => setMuted(!muted)}
                  className="rounded-lg p-1.5 text-ink-1 hover:bg-bg-3 hover:text-ink"
                >
                  {muted || volume === 0 ? <VolumeX size={16} /> : <Volume2 size={16} />}
                </button>
                <input
                  type="range"
                  min={0}
                  max={1}
                  step={0.02}
                  value={muted ? 0 : volume}
                  onChange={(e) => setVolume(Number(e.target.value))}
                  className="w-20 accent-[var(--accent)]"
                  aria-label="Volume"
                />
              </div>
            </div>
            <ErrorText>{error}</ErrorText>
          </div>

          {/* Queue */}
          <div className="flex min-h-0 flex-col rounded-xl border border-edge bg-bg-1">
            <div className="flex items-center gap-2 border-b border-edge px-3 py-2 text-xs font-semibold uppercase tracking-wide text-ink-2">
              <ListMusic size={14} /> Up next · {st.items.length}
              <div className="ml-auto flex gap-1">
                <button
                  title="Save this queue as a playlist"
                  onClick={() => setSaveOpen(true)}
                  disabled={st.items.length === 0}
                  className="rounded-lg p-1.5 text-ink-1 hover:bg-bg-3 hover:text-ink disabled:opacity-40"
                >
                  <Save size={14} />
                </button>
                <button
                  title="Add to the queue"
                  onClick={() => setAddOpen(true)}
                  className="rounded-lg bg-accent/15 p-1.5 text-accent hover:bg-accent/25"
                >
                  <Plus size={14} />
                </button>
              </div>
            </div>
            <div className="max-h-64 flex-1 overflow-y-auto p-1.5">
              {st.items.length === 0 && (
                <p className="px-2 py-3 text-center text-xs text-ink-2">The queue is empty.</p>
              )}
              {st.items.map((it, idx) => (
                <QueueRow
                  key={it.id}
                  item={it}
                  index={idx}
                  current={it.id === session.current_item_id}
                  playing={session.state === 'playing' && it.id === session.current_item_id}
                  canSelect={isHost}
                  canRemove={isHost || it.added_by === me.id}
                  onSelect={() => guard(select(channelId, it.id))}
                  onRemove={() => guard(removeItem(channelId, it.id))}
                />
              ))}
            </div>
            {isHost && (
              <button
                onClick={() => {
                  if (confirm('End the listening session for everyone?')) guard(end(channelId))
                }}
                className="border-t border-edge px-3 py-1.5 text-center text-xs text-ink-2 hover:text-danger"
              >
                End for everyone
              </button>
            )}
          </div>
        </div>
      )}

      <AddModal open={addOpen} onClose={() => setAddOpen(false)} channelId={channelId} />
      <SaveModal open={saveOpen} onClose={() => setSaveOpen(false)} channelId={channelId} />
    </div>
  )
}

function Ctl({ children, title, disabled, onClick }: { children: React.ReactNode; title: string; disabled?: boolean; onClick: () => void }) {
  return (
    <button
      title={title}
      disabled={disabled}
      onClick={onClick}
      className="rounded-full bg-bg-3 p-2 text-ink-1 hover:text-ink disabled:opacity-40"
    >
      {children}
    </button>
  )
}

function Members({ channelId, isHost, onMakeHost }: { channelId: string; isHost: boolean; onMakeHost: (uid: string) => void }) {
  const st = useListen((s) => s.sessions[channelId])!
  const users = useApp((s) => s.users)
  const me = useApp((s) => s.me)!
  const hostId = st.session!.host_user_id
  return (
    <div className="flex -space-x-1.5" title={`${st.members.length} listening`}>
      {st.members.slice(0, 6).map((uid) => (
        <button
          key={uid}
          title={
            uid === hostId
              ? `${displayName(users, uid)} (host)`
              : isHost && uid !== me.id
                ? `Make ${displayName(users, uid)} the host`
                : displayName(users, uid)
          }
          onClick={() => {
            if (isHost && uid !== me.id && confirm(`Make ${displayName(users, uid)} the host?`)) onMakeHost(uid)
          }}
          className={cx('relative rounded-full ring-2 ring-bg-2', uid === hostId && 'z-10')}
        >
          <Avatar user={users[uid]} size={22} />
          {uid === hostId && (
            <Crown size={10} className="absolute -right-1 -top-1 rounded-full bg-bg-2 p-px text-warning" />
          )}
        </button>
      ))}
      {st.members.length > 6 && (
        <span className="flex h-[22px] items-center rounded-full bg-bg-3 px-1.5 text-[10px] text-ink-1 ring-2 ring-bg-2">
          +{st.members.length - 6}
        </span>
      )}
    </div>
  )
}

function QueueRow({
  item, index, current, playing, canSelect, canRemove, onSelect, onRemove,
}: {
  item: PlaylistItem
  index: number
  current: boolean
  playing: boolean
  canSelect: boolean
  canRemove: boolean
  onSelect: () => void
  onRemove: () => void
}) {
  const users = useApp((s) => s.users)
  const thumb = item.refs?.thumbnail
  return (
    <div
      className={cx(
        'group flex items-center gap-2 rounded-lg px-2 py-1.5 text-sm',
        current ? 'bg-accent/15 text-ink' : 'text-ink-1 hover:bg-bg-3/60',
        canSelect && !current && 'cursor-pointer',
      )}
      onClick={canSelect && !current ? onSelect : undefined}
      title={canSelect && !current ? 'Play this now' : undefined}
    >
      <span className="w-5 shrink-0 text-center text-xs tabular-nums text-ink-2">
        {playing ? <span className="animate-pulse text-accent">▶</span> : index + 1}
      </span>
      {thumb ? (
        <img src={thumb} alt="" className="h-8 w-12 shrink-0 rounded object-cover" />
      ) : (
        <span className="flex h-8 w-12 shrink-0 items-center justify-center rounded bg-bg-3 text-base">🎵</span>
      )}
      <div className="min-w-0 flex-1">
        <div className="truncate font-medium">{item.title}</div>
        <div className="truncate text-xs text-ink-2">
          {item.artist ? `${item.artist} · ` : ''}
          {item.duration_ms ? formatClock(item.duration_ms) + ' · ' : ''}
          added by {displayName(users, item.added_by)}
        </div>
      </div>
      {canRemove && (
        <button
          title="Remove from queue"
          onClick={(e) => {
            e.stopPropagation()
            onRemove()
          }}
          className="rounded p-1 text-ink-2 opacity-0 hover:text-danger group-hover:opacity-100"
        >
          <Trash2 size={14} />
        </button>
      )}
    </div>
  )
}

function AddModal({ open, onClose, channelId }: { open: boolean; onClose: () => void; channelId: string }) {
  const addUrl = useListen((s) => s.addUrl)
  const addUpload = useListen((s) => s.addUpload)
  const [url, setUrl] = useState('')
  const [busy, setBusy] = useState(false)
  const [progress, setProgress] = useState<number | null>(null)
  const [error, setError] = useState('')
  const fileRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    if (open) {
      setUrl('')
      setError('')
      setProgress(null)
    }
  }, [open])

  async function submitUrl(e: React.FormEvent) {
    e.preventDefault()
    if (!url.trim()) return
    setBusy(true)
    setError('')
    try {
      await addUrl(channelId, url.trim())
      setUrl('')
      onClose()
    } catch (e2: any) {
      setError(e2?.message ?? 'Could not add that link')
    } finally {
      setBusy(false)
    }
  }

  async function upload(file: File) {
    setBusy(true)
    setError('')
    setProgress(0)
    try {
      const res = await uploadFile(file, setProgress)
      await addUpload(channelId, res.attachment.id)
      onClose()
    } catch (e2: any) {
      setError(e2?.message ?? 'Upload failed')
    } finally {
      setBusy(false)
      setProgress(null)
    }
  }

  return (
    <Modal open={open} onClose={onClose} title="Add to the queue">
      <form className="space-y-2" onSubmit={submitUrl}>
        <div className="flex gap-2">
          <Input
            value={url}
            onChange={(e) => setUrl(e.target.value)}
            placeholder="Paste a YouTube link"
            autoFocus
          />
          <Button type="submit" disabled={busy || !url.trim()}>
            <LinkIcon size={15} /> Add
          </Button>
        </div>
        <p className="text-xs text-ink-2">YouTube and YouTube Music links work today. Spotify and Apple Music are coming.</p>
      </form>
      <div className="my-4 flex items-center gap-3 text-xs text-ink-2">
        <span className="h-px flex-1 bg-edge" /> or <span className="h-px flex-1 bg-edge" />
      </div>
      <Button variant="ghost" className="w-full border border-edge" disabled={busy} onClick={() => fileRef.current?.click()}>
        <Upload size={15} /> {progress !== null ? `Uploading… ${Math.round(progress * 100)}%` : 'Upload a song or video'}
      </Button>
      <input
        ref={fileRef}
        type="file"
        accept="audio/*,video/*"
        hidden
        onChange={(e) => {
          const f = e.target.files?.[0]
          if (f) upload(f)
          e.target.value = ''
        }}
      />
      <div className="mt-2">
        <ErrorText>{error}</ErrorText>
      </div>
    </Modal>
  )
}

function SaveModal({ open, onClose, channelId }: { open: boolean; onClose: () => void; channelId: string }) {
  const save = useListen((s) => s.save)
  const [name, setName] = useState('')
  const [msg, setMsg] = useState('')
  const [error, setError] = useState('')
  useEffect(() => {
    if (open) {
      setName('')
      setMsg('')
      setError('')
    }
  }, [open])
  return (
    <Modal open={open} onClose={onClose} title="Save as a playlist">
      <form
        className="space-y-3"
        onSubmit={async (e) => {
          e.preventDefault()
          setError('')
          try {
            await save(channelId, name)
            setMsg('Saved! Start it any time from the Listen together button.')
          } catch (e2: any) {
            setError(e2?.message ?? 'Could not save')
          }
        }}
      >
        <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="Road trip mix" maxLength={80} autoFocus required />
        <div className="flex items-center gap-3">
          <Button type="submit">
            <Save size={15} /> Save
          </Button>
          {msg && <span className="text-sm text-success">{msg}</span>}
        </div>
        <ErrorText>{error}</ErrorText>
      </form>
    </Modal>
  )
}

// StartListenModal starts a session, empty or from a saved playlist.
export function StartListenModal({ open, onClose, channelId }: { open: boolean; onClose: () => void; channelId: string }) {
  const start = useListen((s) => s.start)
  const [lists, setLists] = useState<{ id: string; name: string; item_count: number; owner_id: string }[]>([])
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const me = useApp((s) => s.me)

  const load = useMemo(
    () => () =>
      fetch(`/api/v1/channels/${channelId}/playlists`, { credentials: 'same-origin' })
        .then((r) => r.json())
        .then((d) => setLists(d.playlists ?? []))
        .catch(() => {}),
    [channelId],
  )
  useEffect(() => {
    if (open) {
      setError('')
      load()
    }
  }, [open, load])

  async function go(playlistId?: string) {
    setBusy(true)
    setError('')
    try {
      await start(channelId, playlistId)
      onClose()
    } catch (e: any) {
      setError(e?.message ?? 'Could not start')
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal open={open} onClose={onClose} title="Listen together">
      <p className="mb-3 text-sm text-ink-1">
        Everyone who joins hears the same thing at the same moment. You drive the playback; anyone can add to the queue.
      </p>
      <Button className="w-full" disabled={busy} onClick={() => go()}>
        <Headphones size={16} /> Start with an empty queue
      </Button>
      {lists.length > 0 && (
        <div className="mt-4">
          <div className="mb-1.5 text-xs font-semibold uppercase tracking-wide text-ink-2">Saved playlists</div>
          <div className="max-h-56 space-y-1 overflow-y-auto">
            {lists.map((p) => (
              <div key={p.id} className="flex items-center gap-2 rounded-xl px-2 py-1.5 hover:bg-bg-3/60">
                <button className="flex min-w-0 flex-1 items-center gap-2 text-left" disabled={busy} onClick={() => go(p.id)}>
                  <ListMusic size={16} className="shrink-0 text-accent" />
                  <span className="truncate text-sm font-medium text-ink">{p.name}</span>
                  <span className="shrink-0 text-xs text-ink-2">{p.item_count} {p.item_count === 1 ? 'item' : 'items'}</span>
                </button>
                {(p.owner_id === me?.id || me?.is_admin) && (
                  <button
                    title="Delete playlist"
                    className="rounded p-1 text-ink-2 hover:text-danger"
                    onClick={() => {
                      if (confirm(`Delete "${p.name}"?`)) {
                        fetch(`/api/v1/playlists/${p.id}`, { method: 'DELETE', credentials: 'same-origin' }).then(load)
                      }
                    }}
                  >
                    <X size={14} />
                  </button>
                )}
              </div>
            ))}
          </div>
        </div>
      )}
      <div className="mt-2">
        <ErrorText>{error}</ErrorText>
      </div>
    </Modal>
  )
}

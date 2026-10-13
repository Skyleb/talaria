import { useMemo, useState } from 'react'
import { CornerUpLeft, Pencil, SmilePlus, Trash2 } from 'lucide-react'
import { api } from '../../lib/api'
import { renderMarkdown } from '../../lib/markdown'
import { formatTime, formatDateTime } from '../../lib/time'
import { displayName, useApp } from '../../store'
import { Avatar } from '../Avatar'
import { Button, cx } from '../ui'
import { AttachmentView } from './AttachmentView'
import { Lightbox } from './Lightbox'
import { ReactionPicker } from './ReactionPicker'
import type { Message } from '../../lib/types'

// A lone https link to an image file (what the GIF picker sends) renders as
// the picture itself rather than as a link.
const IMAGE_LINK = /^https:\/\/\S+\.(?:gif|png|jpe?g|webp)(?:\?\S*)?$/i

export function MessageItem({
  message: m,
  grouped,
  onReply,
  highlighted,
}: {
  message: Message
  grouped: boolean
  onReply: (m: Message) => void
  highlighted?: boolean
}) {
  const me = useApp((s) => s.me)
  const users = useApp((s) => s.users)
  const openProfile = useApp((s) => s.openProfile)
  const [editing, setEditing] = useState(false)
  const [editText, setEditText] = useState('')
  const [actionsOpen, setActionsOpen] = useState(false)
  const [lightbox, setLightbox] = useState(false)
  const [picker, setPicker] = useState<'hover' | 'sheet' | null>(null)
  const jumpToMessage = useApp((s) => s.jumpToMessage)

  const author = users[m.author_id] ?? m.author
  const mine = m.author_id === me?.id
  const mentionsMe = !!me && (m.mention_ids ?? []).includes(me.id)
  const mentionNames = useMemo(() => {
    const names: Record<string, string> = {}
    for (const u of Object.values(users)) names[u.username.toLowerCase()] = u.display_name || u.username
    return names
  }, [users])
  const imageURL = !m.deleted_at && IMAGE_LINK.test(m.content.trim()) ? m.content.trim() : null
  const html = useMemo(
    () => (m.deleted_at || imageURL ? '' : renderMarkdown(m.content, mentionNames)),
    [m.content, m.deleted_at, mentionNames, imageURL],
  )

  async function saveEdit() {
    const content = editText.trim()
    setEditing(false)
    if (!content || content === m.content) return
    await api.patch(`/api/v1/messages/${m.id}`, { content }).catch(() => {})
  }

  async function remove() {
    if (!confirm('Delete this message?')) return
    await api.del(`/api/v1/messages/${m.id}`).catch(() => {})
  }

  // toggleReaction adds my reaction, or withdraws it if I already reacted.
  async function toggleReaction(emoji: string) {
    setPicker(null)
    setActionsOpen(false)
    if (!me) return
    const mine = (m.reactions ?? []).some((r) => r.emoji === emoji && r.user_ids.includes(me.id))
    const path = `/api/v1/messages/${m.id}/reactions/${encodeURIComponent(emoji)}`
    await (mine ? api.del(path) : api.put(path, {})).catch(() => {})
  }

  function reactionTitle(userIds: string[]): string {
    const names = userIds.map((id) => (id === me?.id ? 'You' : displayName(users, id)))
    return names.join(', ')
  }

  // System lines ("X added Y") render as a quiet divider, not a bubble.
  if (m.kind === 'system') {
    return (
      <p className="mt-2 px-2 text-center text-xs italic text-ink-2" title={formatDateTime(m.created_at)} data-mid={m.id}>
        {m.content}
      </p>
    )
  }

  return (
    <div
      className={cx(
        'group relative rounded-lg px-2 py-0.5 hover:bg-bg-3/50',
        !grouped && 'mt-2.5',
        m.pending && 'opacity-60',
        m.failed && 'opacity-60 outline outline-1 outline-danger',
        mentionsMe && 'border-l-2 border-accent bg-accent/10 hover:bg-accent/15',
        highlighted && 'bg-accent/20 ring-2 ring-accent/60 transition-colors duration-700',
      )}
      data-mid={m.id}
      onTouchStart={() => {}}
      onContextMenu={(e) => {
        e.preventDefault()
        setActionsOpen(true)
      }}
    >
      {m.reply_to && (
        <button
          onClick={() => jumpToMessage(m.channel_id, m.reply_to!.id)}
          title="Jump to the original message"
          className="mb-0.5 ml-10 flex max-w-full items-center gap-1 truncate rounded text-left text-xs text-ink-2 hover:text-ink"
        >
          <CornerUpLeft size={12} className="shrink-0" />
          <span className="font-medium">{m.reply_to.author ? m.reply_to.author.display_name : displayName(users, m.reply_to.author_id)}</span>
          <span className="truncate">{m.reply_to.deleted_at ? 'deleted message' : m.reply_to.content}</span>
        </button>
      )}
      <div className="flex gap-2.5">
        <div className="w-9 shrink-0 pt-0.5">
          {!grouped && (
            <button title="View profile" onClick={() => openProfile(m.author_id)}>
              <Avatar user={author} size={36} />
            </button>
          )}
        </div>
        <div className="min-w-0 flex-1">
          {!grouped && (
            <div className="flex items-baseline gap-2">
              <button
                className="text-sm font-semibold text-ink hover:underline"
                title="View profile"
                onClick={() => openProfile(m.author_id)}
              >
                {author?.display_name ?? displayName(users, m.author_id)}
              </button>
              <span className="text-[11px] text-ink-2" title={formatDateTime(m.created_at)}>
                {formatTime(m.created_at)}
              </span>
            </div>
          )}
          {m.deleted_at ? (
            <p className="text-sm italic text-ink-2">message deleted</p>
          ) : editing ? (
            <div className="mt-1">
              <textarea
                className="w-full rounded-lg border border-edge bg-bg-0 p-2 text-sm text-ink focus:border-accent focus:outline-none"
                value={editText}
                onChange={(e) => setEditText(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === 'Enter' && !e.shiftKey) {
                    e.preventDefault()
                    saveEdit()
                  }
                  if (e.key === 'Escape') setEditing(false)
                }}
                rows={2}
                autoFocus
              />
              <div className="mt-1 flex gap-2">
                <Button className="px-2 py-1 text-xs" onClick={saveEdit}>Save</Button>
                <Button variant="ghost" className="px-2 py-1 text-xs" onClick={() => setEditing(false)}>Cancel</Button>
              </div>
            </div>
          ) : (
            <>
              {imageURL ? (
                <>
                  <button onClick={() => setLightbox(true)} className="mt-1 block min-h-32 w-fit" title="View full size">
                    <img src={imageURL} alt="GIF" loading="lazy" className="max-h-64 max-w-full rounded-lg bg-bg-2" />
                  </button>
                  {lightbox && <Lightbox url={imageURL} alt="GIF" onClose={() => setLightbox(false)} />}
                </>
              ) : (
                m.content && (
                  <div className="md break-words text-[0.925rem] leading-relaxed text-ink" dangerouslySetInnerHTML={{ __html: html }} />
                )
              )}
              {m.edited_at ? <span className="text-[10px] text-ink-2"> (edited)</span> : null}
              {(m.attachments ?? []).map((a) => (
                <AttachmentView key={a.id} attachment={a} />
              ))}
              {(m.reactions ?? []).length > 0 && (
                <div className="mt-1 flex flex-wrap items-center gap-1">
                  {m.reactions!.map((r) => {
                    const mine = !!me && r.user_ids.includes(me.id)
                    return (
                      <button
                        key={r.emoji}
                        onClick={() => toggleReaction(r.emoji)}
                        title={reactionTitle(r.user_ids)}
                        className={cx(
                          'flex items-center gap-1 rounded-full border px-2 py-0.5 text-xs leading-none',
                          mine ? 'border-accent bg-accent/15 text-ink' : 'border-edge bg-bg-2 text-ink-1 hover:bg-bg-3',
                        )}
                      >
                        <span className="text-sm leading-none">{r.emoji}</span>
                        <span className="tabular-nums">{r.count}</span>
                      </button>
                    )
                  })}
                  {!m.deleted_at && (
                    <div className="relative">
                      <button
                        onClick={() => setPicker(picker === 'hover' ? null : 'hover')}
                        title="Add a reaction"
                        className="flex items-center rounded-full border border-dashed border-edge px-1.5 py-0.5 text-ink-2 hover:text-ink"
                      >
                        <SmilePlus size={13} />
                      </button>
                      {picker === 'hover' && <ReactionPicker className="left-0 top-7" onPick={toggleReaction} onClose={() => setPicker(null)} />}
                    </div>
                  )}
                </div>
              )}
              {m.failed && <p className="text-xs text-danger">Failed to send.</p>}
            </>
          )}
        </div>
      </div>

      {/* Hover actions (desktop) */}
      {!m.deleted_at && !editing && !m.pending && (
        <div className={cx('absolute -top-3 right-2 gap-0.5 rounded-lg border border-edge bg-bg-1 p-0.5 shadow group-hover:flex', picker === 'hover' && (m.reactions ?? []).length === 0 ? 'flex' : 'hidden')}>
          <div className="relative">
            <IconBtn title="React" onClick={() => setPicker(picker === 'hover' ? null : 'hover')}>
              <SmilePlus size={14} />
            </IconBtn>
            {picker === 'hover' && (m.reactions ?? []).length === 0 && (
              <ReactionPicker className="right-0 top-8" onPick={toggleReaction} onClose={() => setPicker(null)} />
            )}
          </div>
          <IconBtn title="Reply" onClick={() => onReply(m)}>
            <CornerUpLeft size={14} />
          </IconBtn>
          {mine && (
            <IconBtn
              title="Edit"
              onClick={() => {
                setEditText(m.content)
                setEditing(true)
              }}
            >
              <Pencil size={14} />
            </IconBtn>
          )}
          {mine && (
            <IconBtn title="Delete" danger onClick={remove}>
              <Trash2 size={14} />
            </IconBtn>
          )}
        </div>
      )}

      {/* Long-press / right-click sheet (mobile) */}
      {actionsOpen && !m.deleted_at && (
        <div className="fixed inset-0 z-50 flex items-end bg-black/40 pb-[env(safe-area-inset-bottom)] sm:hidden" onClick={() => { setActionsOpen(false); setPicker(null) }}>
          <div className="w-full rounded-t-2xl bg-bg-1 p-3" onClick={(e) => e.stopPropagation()}>
            {picker === 'sheet' ? (
              <ReactionPicker inline onPick={toggleReaction} onClose={() => setPicker(null)} />
            ) : (
              <SheetBtn onClick={() => setPicker('sheet')}>
                <SmilePlus size={16} /> React
              </SheetBtn>
            )}
            <SheetBtn
              onClick={() => {
                setActionsOpen(false)
                onReply(m)
              }}
            >
              <CornerUpLeft size={16} /> Reply
            </SheetBtn>
            {mine && (
              <SheetBtn
                onClick={() => {
                  setActionsOpen(false)
                  setEditText(m.content)
                  setEditing(true)
                }}
              >
                <Pencil size={16} /> Edit
              </SheetBtn>
            )}
            {mine && (
              <SheetBtn
                danger
                onClick={() => {
                  setActionsOpen(false)
                  remove()
                }}
              >
                <Trash2 size={16} /> Delete
              </SheetBtn>
            )}
          </div>
        </div>
      )}
    </div>
  )
}

function IconBtn({
  children,
  title,
  danger,
  onClick,
}: {
  children: React.ReactNode
  title: string
  danger?: boolean
  onClick: () => void
}) {
  return (
    <button
      title={title}
      onClick={onClick}
      className={cx('rounded p-1.5 text-ink-1 hover:bg-bg-3', danger ? 'hover:text-danger' : 'hover:text-ink')}
    >
      {children}
    </button>
  )
}

function SheetBtn({
  children,
  danger,
  onClick,
}: {
  children: React.ReactNode
  danger?: boolean
  onClick: () => void
}) {
  return (
    <button
      onClick={onClick}
      className={cx(
        'flex w-full items-center gap-3 rounded-lg px-3 py-3 text-left text-sm',
        danger ? 'text-danger' : 'text-ink',
        'hover:bg-bg-3',
      )}
    >
      {children}
    </button>
  )
}

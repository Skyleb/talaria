import { useEffect, useMemo, useRef, useState } from 'react'
import { CornerUpLeft, ImagePlay, Paperclip, SendHorizontal, Type, X } from 'lucide-react'
import { uploadFile } from '../../lib/api'
import { sendTyping } from '../../gateway'
import { displayName, useApp } from '../../store'
import { Avatar } from '../Avatar'
import { cx } from '../ui'
import { RichEditor, type RichEditorHandle } from './RichEditor'
import { GifPicker, type Gif } from './GifPicker'
import type { Attachment, Message } from '../../lib/types'

interface PendingUpload {
  key: string
  name: string
  progress: number
  attachment?: Attachment
  error?: string
}

// Best-effort media duration probe for audio/video uploads.
function probeDuration(file: File): Promise<number | undefined> {
  if (!file.type.startsWith('audio/') && !file.type.startsWith('video/')) {
    return Promise.resolve(undefined)
  }
  return new Promise((resolve) => {
    const el = document.createElement(file.type.startsWith('audio/') ? 'audio' : 'video')
    const url = URL.createObjectURL(file)
    const done = (v?: number) => {
      URL.revokeObjectURL(url)
      resolve(v)
    }
    el.preload = 'metadata'
    el.onloadedmetadata = () => done(isFinite(el.duration) ? el.duration * 1000 : undefined)
    el.onerror = () => done(undefined)
    setTimeout(() => done(undefined), 3000)
    el.src = url
  })
}

const TOOLS_KEY = 'talaria-composer-tools'

export function Composer({
  channelId,
  replyTo,
  onClearReply,
}: {
  channelId: string
  replyTo: Message | null
  onClearReply: () => void
}) {
  const sendMessage = useApp((s) => s.sendMessage)
  const users = useApp((s) => s.users)
  const me = useApp((s) => s.me)
  const channel = useApp((s) => s.channels[channelId])
  const groupMembers = useApp((s) => (channel?.group_id ? s.groupMembers[channel.group_id] : undefined))
  // @mention autocomplete: the token being typed and the highlighted row.
  const [mention, setMention] = useState<{ query: string } | null>(null)
  const [mentionSel, setMentionSel] = useState(0)
  const [gifOpen, setGifOpen] = useState(false)
  const [tools, setTools] = useState(() => {
    const saved = localStorage.getItem(TOOLS_KEY)
    return saved ? saved === '1' : window.matchMedia('(min-width: 640px)').matches
  })

  const memberIds = useMemo(
    () =>
      channel?.kind === 'dm'
        ? (channel.participants ?? []).map((p) => p.id)
        : (groupMembers ?? []).map((m) => m.user_id),
    [channel, groupMembers],
  )
  const suggestions = useMemo(() => {
    if (!mention) return []
    const q = mention.query.toLowerCase()
    return memberIds
      .filter((id) => id !== me?.id)
      .map((id) => users[id])
      .filter((u): u is NonNullable<typeof u> => !!u)
      .filter((u) => u.username.toLowerCase().startsWith(q) || (u.display_name ?? '').toLowerCase().startsWith(q))
      .slice(0, 6)
  }, [mention, memberIds, users, me])
  const [uploads, setUploads] = useState<PendingUpload[]>([])
  const [sending, setSending] = useState(false)
  const editor = useRef<RichEditorHandle>(null)
  const fileRef = useRef<HTMLInputElement>(null)
  const lastTyping = useRef(0)

  // Switching channels starts a fresh box.
  useEffect(() => {
    editor.current?.clear()
    setMention(null)
    setGifOpen(false)
  }, [channelId])

  // Look back from the caret for an @token with no whitespace in it.
  function detectMention() {
    const before = editor.current?.textBeforeCaret() ?? ''
    const m = /(^|\s)@([a-z0-9_.]*)$/i.exec(before)
    if (m) {
      setMention({ query: m[2] })
      setMentionSel(0)
    } else {
      setMention(null)
    }
  }

  function pickMention(username: string) {
    if (!mention) return
    editor.current?.replaceBeforeCaret(mention.query.length + 1, '@' + username + ' ')
    setMention(null)
  }

  function onInput() {
    detectMention()
    const now = Date.now()
    if (!editor.current?.isEmpty() && now - lastTyping.current > 8000) {
      lastTyping.current = now
      sendTyping(channelId)
    }
  }

  async function addFiles(files: FileList | File[]) {
    for (const file of Array.from(files)) {
      const key = `${file.name}-${Date.now()}-${Math.random()}`
      setUploads((u) => [...u, { key, name: file.name, progress: 0 }])
      try {
        const duration = await probeDuration(file)
        const res = await uploadFile(
          file,
          (frac) => setUploads((u) => u.map((x) => (x.key === key ? { ...x, progress: frac } : x))),
          duration,
        )
        setUploads((u) => u.map((x) => (x.key === key ? { ...x, progress: 1, attachment: res.attachment } : x)))
      } catch (err: any) {
        setUploads((u) => u.map((x) => (x.key === key ? { ...x, error: err?.message ?? 'upload failed' } : x)))
      }
    }
  }

  async function submit() {
    const content = editor.current?.getMarkdown() ?? ''
    const ready = uploads.filter((u) => u.attachment)
    if (!content && ready.length === 0) return
    if (uploads.some((u) => !u.attachment && !u.error)) return // still uploading
    setSending(true)
    const attachmentIds = ready.map((u) => u.attachment!.id)
    editor.current?.clear()
    setUploads([])
    setMention(null)
    onClearReply()
    try {
      await sendMessage(channelId, content, attachmentIds, replyTo?.id)
    } catch {
      /* failure is reflected on the optimistic message */
    } finally {
      setSending(false)
      editor.current?.focus()
    }
  }

  async function sendGif(gif: Gif) {
    setGifOpen(false)
    onClearReply()
    try {
      await sendMessage(channelId, gif.url, [], replyTo?.id)
    } catch {
      /* reflected on the message */
    }
    editor.current?.focus()
  }

  function toggleTools() {
    setTools((v) => {
      localStorage.setItem(TOOLS_KEY, v ? '0' : '1')
      return !v
    })
  }

  return (
    <div className="relative shrink-0 px-2 pb-2 sm:px-4 sm:pb-4">
      {mention && suggestions.length > 0 && (
        <div className="absolute bottom-full left-2 right-2 z-20 mb-1 overflow-hidden rounded-xl border border-edge bg-bg-1 shadow-lg sm:left-4 sm:w-72 sm:right-auto">
          {suggestions.map((u, i) => (
            <button
              key={u.id}
              onMouseDown={(e) => e.preventDefault()}
              onClick={() => pickMention(u.username)}
              className={cx(
                'flex w-full items-center gap-2 px-3 py-2 text-left text-sm',
                i === mentionSel ? 'bg-accent/15 text-ink' : 'text-ink-1 hover:bg-bg-3',
              )}
            >
              <Avatar user={u} size={24} />
              <span className="font-medium">{u.display_name || u.username}</span>
              <span className="text-xs text-ink-2">@{u.username}</span>
            </button>
          ))}
        </div>
      )}
      <GifPicker open={gifOpen} onClose={() => setGifOpen(false)} onPick={sendGif} />
      {replyTo && (
        <div className="flex items-center gap-2 rounded-t-lg border border-b-0 border-edge bg-bg-2 px-3 py-1.5 text-xs text-ink-1">
          <CornerUpLeft size={13} />
          Replying to <span className="font-semibold">{replyTo.author?.display_name ?? displayName(users, replyTo.author_id)}</span>
          <button className="ml-auto text-ink-2 hover:text-ink" onClick={onClearReply}>
            <X size={14} />
          </button>
        </div>
      )}
      {uploads.length > 0 && (
        <div className={cx('flex flex-wrap gap-2 border border-b-0 border-edge bg-bg-2 px-3 py-2', !replyTo && 'rounded-t-lg')}>
          {uploads.map((u) => (
            <div
              key={u.key}
              className={cx(
                'flex items-center gap-2 rounded-lg border border-edge bg-bg-1 px-2 py-1 text-xs',
                u.error && 'border-danger text-danger',
              )}
            >
              <span className="max-w-40 truncate">{u.name}</span>
              {u.error ? (
                <span>failed</span>
              ) : u.attachment ? (
                <span className="text-success">✓</span>
              ) : (
                <span className="tabular-nums">{Math.round(u.progress * 100)}%</span>
              )}
              <button
                className="text-ink-2 hover:text-ink"
                onClick={() => setUploads((list) => list.filter((x) => x.key !== u.key))}
              >
                <X size={12} />
              </button>
            </div>
          ))}
        </div>
      )}
      <div
        className={cx(
          'flex items-end gap-1 border border-edge bg-bg-1 p-1.5',
          replyTo || uploads.length ? 'rounded-b-lg' : 'rounded-lg',
        )}
      >
        <button
          title="Attach files"
          className="rounded-lg p-2 text-ink-1 hover:bg-bg-3 hover:text-ink"
          onClick={() => fileRef.current?.click()}
        >
          <Paperclip size={19} />
        </button>
        <button
          title="Send a GIF"
          className={cx('rounded-lg p-2 hover:bg-bg-3 hover:text-ink', gifOpen ? 'text-accent' : 'text-ink-1')}
          onClick={() => setGifOpen((v) => !v)}
        >
          <ImagePlay size={19} />
        </button>
        <button
          title={tools ? 'Hide formatting tools' : 'Show formatting tools'}
          className={cx('hidden rounded-lg p-2 hover:bg-bg-3 hover:text-ink sm:block', tools ? 'text-accent' : 'text-ink-1')}
          onClick={toggleTools}
        >
          <Type size={19} />
        </button>
        <input
          ref={fileRef}
          type="file"
          multiple
          hidden
          onChange={(e) => {
            if (e.target.files?.length) addFiles(e.target.files)
            e.target.value = ''
          }}
        />
        <RichEditor
          ref={editor}
          placeholder="Message…"
          toolbar={tools}
          onInput={onInput}
          onSubmit={submit}
          onFiles={addFiles}
          onClick={detectMention}
          onBlur={() => setTimeout(() => setMention(null), 150)}
          onKeyDown={(e) => {
            if (!mention || !suggestions.length) return false
            if (e.key === 'ArrowDown') {
              e.preventDefault()
              setMentionSel((i) => (i + 1) % suggestions.length)
              return true
            }
            if (e.key === 'ArrowUp') {
              e.preventDefault()
              setMentionSel((i) => (i - 1 + suggestions.length) % suggestions.length)
              return true
            }
            if (e.key === 'Enter' || e.key === 'Tab') {
              e.preventDefault()
              pickMention(suggestions[mentionSel].username)
              return true
            }
            if (e.key === 'Escape') {
              setMention(null)
              return true
            }
            return false
          }}
        />
        <button
          title={tools ? 'Hide formatting tools' : 'Show formatting tools'}
          className={cx('rounded-lg p-2 hover:bg-bg-3 hover:text-ink sm:hidden', tools ? 'text-accent' : 'text-ink-1')}
          onClick={toggleTools}
        >
          <Type size={19} />
        </button>
        <button
          title="Send"
          disabled={sending}
          className="rounded-lg bg-accent p-2 text-accent-text hover:bg-accent-hover disabled:opacity-50"
          onClick={submit}
        >
          <SendHorizontal size={19} />
        </button>
      </div>
    </div>
  )
}

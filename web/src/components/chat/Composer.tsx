import { useRef, useState } from 'react'
import { CornerUpLeft, Paperclip, SendHorizontal, X } from 'lucide-react'
import { uploadFile } from '../../lib/api'
import { sendTyping } from '../../gateway'
import { displayName, useApp } from '../../store'
import { cx } from '../ui'
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
  const [text, setText] = useState('')
  const [uploads, setUploads] = useState<PendingUpload[]>([])
  const [sending, setSending] = useState(false)
  const textareaRef = useRef<HTMLTextAreaElement>(null)
  const fileRef = useRef<HTMLInputElement>(null)
  const lastTyping = useRef(0)

  function autogrow() {
    const el = textareaRef.current
    if (!el) return
    el.style.height = 'auto'
    el.style.height = Math.min(el.scrollHeight, 160) + 'px'
  }

  function onType(v: string) {
    setText(v)
    autogrow()
    const now = Date.now()
    if (v.trim() && now - lastTyping.current > 8000) {
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
    const content = text.trim()
    const ready = uploads.filter((u) => u.attachment)
    if (!content && ready.length === 0) return
    if (uploads.some((u) => !u.attachment && !u.error)) return // still uploading
    setSending(true)
    const attachmentIds = ready.map((u) => u.attachment!.id)
    setText('')
    setUploads([])
    onClearReply()
    if (textareaRef.current) textareaRef.current.style.height = 'auto'
    try {
      await sendMessage(channelId, content, attachmentIds, replyTo?.id)
    } catch {
      /* failure is reflected on the optimistic message */
    } finally {
      setSending(false)
      textareaRef.current?.focus()
    }
  }

  return (
    <div className="shrink-0 px-2 pb-2 sm:px-4 sm:pb-4">
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
        <textarea
          ref={textareaRef}
          rows={1}
          value={text}
          placeholder="Message…"
          className="max-h-40 flex-1 resize-none bg-transparent px-1 py-2 text-[0.925rem] text-ink placeholder:text-ink-2 focus:outline-none"
          onChange={(e) => onType(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter' && !e.shiftKey) {
              e.preventDefault()
              submit()
            }
          }}
          onPaste={(e) => {
            const files = Array.from(e.clipboardData.files)
            if (files.length) {
              e.preventDefault()
              addFiles(files)
            }
          }}
        />
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

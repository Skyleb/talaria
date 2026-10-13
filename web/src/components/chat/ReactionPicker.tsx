// ReactionPicker: a small grid of common emoji plus a box to type any other.
import { useEffect, useRef, useState } from 'react'
import { cx, Input } from '../ui'

const QUICK = ['👍', '❤️', '😂', '😮', '😢', '😡', '🔥', '🎉', '👀', '🙏', '💯', '✅', '👎', '🤔', '😍', '🥳', '😎', '🤣', '🫡', '💀', '🤝', '🙌', '👏', '🎵', '🍕', '☕', '🐉', '⭐', '🚀', '🤯', '😴', '🫠']

const RECENT_KEY = 'talaria-recent-emoji'

function recent(): string[] {
  try {
    const v = JSON.parse(localStorage.getItem(RECENT_KEY) || '[]')
    return Array.isArray(v) ? v.filter((s) => typeof s === 'string').slice(0, 8) : []
  } catch {
    return []
  }
}

export function rememberEmoji(e: string) {
  const list = [e, ...recent().filter((x) => x !== e)].slice(0, 8)
  localStorage.setItem(RECENT_KEY, JSON.stringify(list))
}

export function ReactionPicker({
  onPick,
  onClose,
  className,
  inline,
}: {
  onPick: (emoji: string) => void
  onClose: () => void
  className?: string
  // inline renders the grid without the floating card chrome (mobile sheet).
  inline?: boolean
}) {
  const [typed, setTyped] = useState('')
  const inputRef = useRef<HTMLInputElement>(null)
  const recents = recent()
  useEffect(() => {
    if (inline) return
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && onClose()
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose, inline])

  function pick(e: string) {
    const emoji = e.trim()
    if (!emoji) return
    rememberEmoji(emoji)
    onPick(emoji)
  }

  const grid = (list: string[]) => (
    <div className="grid grid-cols-8 gap-0.5">
      {list.map((e) => (
        <button key={e} onClick={() => pick(e)} className="rounded-lg p-1 text-xl leading-none hover:bg-bg-3" title={e}>
          {e}
        </button>
      ))}
    </div>
  )

  return (
    <div
      onClick={(e) => e.stopPropagation()}
      className={cx(!inline && 'absolute z-20 w-72 rounded-xl border border-edge bg-bg-1 p-2 shadow-xl', className)}
    >
      {recents.length > 0 && (
        <>
          <div className="px-1 pb-0.5 text-[10px] font-semibold uppercase tracking-wide text-ink-2">Recent</div>
          {grid(recents)}
        </>
      )}
      <div className="px-1 pb-0.5 pt-1 text-[10px] font-semibold uppercase tracking-wide text-ink-2">Quick</div>
      {grid(QUICK)}
      <form
        className="mt-2 flex gap-1"
        onSubmit={(e) => {
          e.preventDefault()
          pick(typed)
        }}
      >
        <Input
          ref={inputRef}
          value={typed}
          onChange={(e) => setTyped(e.target.value)}
          placeholder="Or type any emoji…"
          maxLength={16}
          className="py-1 text-sm"
        />
        <button type="submit" disabled={!typed.trim()} className="rounded-xl bg-accent px-3 text-sm text-accent-text disabled:opacity-40">
          Add
        </button>
      </form>
    </div>
  )
}

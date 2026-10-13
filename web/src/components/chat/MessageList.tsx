import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import { useApp } from '../../store'
import { MessageItem } from './MessageItem'
import { Spinner } from '../ui'
import { formatDay, sameDay } from '../../lib/time'
import type { Message } from '../../lib/types'

// Groups consecutive messages from one author within 5 minutes.
function isGrouped(prev: Message | undefined, m: Message): boolean {
  return (
    !!prev &&
    prev.author_id === m.author_id &&
    prev.kind === 'default' &&
    !m.reply_to_id &&
    m.created_at - prev.created_at < 5 * 60 * 1000 &&
    sameDay(prev.created_at, m.created_at)
  )
}

export function MessageList({
  channelId,
  onReply,
}: {
  channelId: string
  onReply: (m: Message) => void
}) {
  const messages = useApp((s) => s.messages[channelId])
  const hasMore = useApp((s) => s.hasMore[channelId])
  const loaded = useApp((s) => s.loaded[channelId])
  const loadMessages = useApp((s) => s.loadMessages)
  const hasMoreAfter = useApp((s) => !!s.hasMoreAfter[channelId])
  const highlightId = useApp((s) => s.highlightMessageId)
  const loadNewer = useApp((s) => s.loadNewer)
  const resetChannel = useApp((s) => s.resetChannel)
  const clearHighlight = useApp((s) => s.clearHighlight)
  const containerRef = useRef<HTMLDivElement>(null)
  const contentRef = useRef<HTMLDivElement>(null)
  const [loadingMore, setLoadingMore] = useState(false)
  const [loadingNewer, setLoadingNewer] = useState(false)
  const pinnedToBottom = useRef(true)
  const prevHeight = useRef(0)
  const prevChannel = useRef('')
  const loadingMoreRef = useRef(false)
  loadingMoreRef.current = loadingMore
  const ready = loaded || !!messages

  // Track whether the user is near the bottom.
  function onScroll() {
    const el = containerRef.current
    if (!el) return
    pinnedToBottom.current = !hasMoreAfter && el.scrollHeight - el.scrollTop - el.clientHeight < 80
    if (hasMoreAfter && !loadingNewer && el.scrollHeight - el.scrollTop - el.clientHeight < 200) {
      setLoadingNewer(true)
      loadNewer(channelId)
        .catch(() => {})
        .finally(() => setLoadingNewer(false))
    }
    if (el.scrollTop < 200 && hasMore && !loadingMore && messages?.length) {
      setLoadingMore(true)
      prevHeight.current = el.scrollHeight
      loadMessages(channelId, messages[0].id)
        .catch(() => {})
        .finally(() => setLoadingMore(false))
    }
  }

  // Keep scroll position stable when older messages prepend; stick to bottom on new ones.
  useLayoutEffect(() => {
    const el = containerRef.current
    if (!el) return
    if (prevChannel.current !== channelId) {
      prevChannel.current = channelId
      if (!hasMoreAfter) el.scrollTop = el.scrollHeight
      pinnedToBottom.current = !hasMoreAfter
      // Images and embeds settle after this effect; re-pin after first paint
      // (the resize observer below handles everything that loads later).
      requestAnimationFrame(() => {
        if (pinnedToBottom.current) el.scrollTop = el.scrollHeight
      })
      return
    }
    if (loadingMore && prevHeight.current) {
      el.scrollTop += el.scrollHeight - prevHeight.current
      prevHeight.current = el.scrollHeight
    } else if (pinnedToBottom.current) {
      el.scrollTop = el.scrollHeight
    }
  }, [messages, channelId, loadingMore])

  // GIFs, images without known dimensions, videos and link embeds grow the
  // content after it renders. While the user is at the bottom, follow that
  // growth so a freshly opened chat ends on the latest message rather than
  // drifting up as media loads.
  useEffect(() => {
    const el = containerRef.current
    const content = contentRef.current
    if (!el || !content) return
    const ro = new ResizeObserver(() => {
      if (pinnedToBottom.current && !loadingMoreRef.current) el.scrollTop = el.scrollHeight
    })
    ro.observe(content)
    return () => ro.disconnect()
  }, [channelId, ready])

  // Scroll a jumped-to message into view and flash it briefly.
  useEffect(() => {
    if (!highlightId) return
    const target = containerRef.current?.querySelector(`[data-mid="${CSS.escape(highlightId)}"]`)
    if (!target) return
    pinnedToBottom.current = false
    target.scrollIntoView({ block: 'center' })
    const t = setTimeout(clearHighlight, 2500)
    return () => clearTimeout(t)
  }, [highlightId, messages, clearHighlight])

  if (!ready) {
    return (
      <div className="flex flex-1 items-center justify-center">
        <Spinner />
      </div>
    )
  }

  const list = messages ?? []
  return (
    <div
      ref={containerRef}
      onScroll={onScroll}
      className="flex-1 overflow-y-auto overscroll-contain px-2 pb-2 pt-4 [overflow-anchor:none] sm:px-4"
    >
      <div ref={contentRef}>
      {loadingMore && (
        <div className="flex justify-center py-2">
          <Spinner className="h-4 w-4" />
        </div>
      )}
      {!hasMore && (
        <p className="px-2 pb-4 text-center text-xs text-ink-2">This is the beginning of the conversation.</p>
      )}
      {list.map((m, i) => {
        const prev = list[i - 1]
        const newDay = !prev || !sameDay(prev.created_at, m.created_at)
        return (
          <div key={m.id}>
            {newDay && (
              <div className="my-3 flex items-center gap-3 px-2">
                <div className="h-px flex-1 bg-edge" />
                <span className="text-xs font-medium text-ink-2">{formatDay(m.created_at)}</span>
                <div className="h-px flex-1 bg-edge" />
              </div>
            )}
            <MessageItem message={m} grouped={!newDay && isGrouped(prev, m)} onReply={onReply} highlighted={m.id === highlightId} />
          </div>
        )
      })}
      {loadingNewer && (
        <div className="flex justify-center py-2">
          <Spinner className="h-4 w-4" />
        </div>
      )}
      {hasMoreAfter && (
        <div className="sticky bottom-1 flex justify-center">
          <button
            onClick={() => resetChannel(channelId)}
            className="rounded-full bg-accent px-3 py-1.5 text-xs font-medium text-accent-text shadow hover:bg-accent-hover"
          >
            Jump to latest ↓
          </button>
        </div>
      )}
      </div>
    </div>
  )
}

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
  const containerRef = useRef<HTMLDivElement>(null)
  const [loadingMore, setLoadingMore] = useState(false)
  const pinnedToBottom = useRef(true)
  const prevHeight = useRef(0)
  const prevChannel = useRef('')

  // Track whether the user is near the bottom.
  function onScroll() {
    const el = containerRef.current
    if (!el) return
    pinnedToBottom.current = el.scrollHeight - el.scrollTop - el.clientHeight < 80
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
      el.scrollTop = el.scrollHeight
      pinnedToBottom.current = true
      return
    }
    if (loadingMore && prevHeight.current) {
      el.scrollTop += el.scrollHeight - prevHeight.current
      prevHeight.current = el.scrollHeight
    } else if (pinnedToBottom.current) {
      el.scrollTop = el.scrollHeight
    }
  }, [messages, channelId, loadingMore])

  if (!loaded && !messages) {
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
      className="flex-1 overflow-y-auto overscroll-contain px-2 pb-2 pt-4 sm:px-4"
    >
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
            <MessageItem message={m} grouped={!newDay && isGrouped(prev, m)} onReply={onReply} />
          </div>
        )
      })}
    </div>
  )
}

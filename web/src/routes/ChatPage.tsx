import { useEffect, useMemo, useRef, useState } from 'react'
import { useParams } from 'react-router'
import { Hash, Users } from 'lucide-react'
import { dmTitle, displayName, useApp } from '../store'
import { PageHeader } from './AppShell'
import { MessageList } from '../components/chat/MessageList'
import { Composer } from '../components/chat/Composer'
import { Avatar } from '../components/Avatar'
import { cx } from '../components/ui'
import type { Message } from '../lib/types'

export default function ChatPage({ onMenu }: { onMenu: () => void }) {
  const { channelId = '', groupId } = useParams()
  const channel = useApp((s) => s.channels[channelId])
  const users = useApp((s) => s.users)
  const me = useApp((s) => s.me)
  const loadMessages = useApp((s) => s.loadMessages)
  const markRead = useApp((s) => s.markRead)
  const latest = useApp((s) => s.latestIds[channelId])
  const [membersOpen, setMembersOpen] = useState(false)
  const [replyTo, setReplyTo] = useState<Message | null>(null)

  useEffect(() => {
    setReplyTo(null)
    if (channelId) loadMessages(channelId).catch(() => {})
  }, [channelId, loadMessages])

  // Mark read when viewing and new messages arrive while visible.
  useEffect(() => {
    if (!channelId || !latest) return
    const mark = () => document.visibilityState === 'visible' && markRead(channelId)
    mark()
    document.addEventListener('visibilitychange', mark)
    return () => document.removeEventListener('visibilitychange', mark)
  }, [channelId, latest, markRead])

  if (!channel || !me) {
    return (
      <div className="flex flex-1 items-center justify-center text-ink-2">
        <PageHeaderPlaceholder onMenu={onMenu} />
        Channel not found
      </div>
    )
  }

  const isDM = channel.kind === 'dm'
  const title = isDM ? dmTitle(channel, me.id, users) : `${channel.name}`

  return (
    <div className="flex min-h-0 flex-1">
      <div className="flex min-w-0 flex-1 flex-col">
        <PageHeader
          onMenu={onMenu}
          icon={isDM ? undefined : <Hash size={18} />}
          title={title}
          subtitle={channel.topic || undefined}
          right={
            !isDM && (
              <button
                className="rounded p-1.5 text-ink-1 hover:bg-bg-3 hover:text-ink"
                title="Members"
                onClick={() => setMembersOpen((v) => !v)}
              >
                <Users size={18} />
              </button>
            )
          }
        />
        <MessageList channelId={channelId} onReply={setReplyTo} />
        <TypingLine channelId={channelId} />
        <Composer channelId={channelId} replyTo={replyTo} onClearReply={() => setReplyTo(null)} />
      </div>
      {!isDM && groupId && <MemberPanel groupId={groupId} open={membersOpen} onClose={() => setMembersOpen(false)} />}
    </div>
  )
}

function PageHeaderPlaceholder({ onMenu }: { onMenu: () => void }) {
  return (
    <div className="absolute left-0 right-0 top-0">
      <PageHeader onMenu={onMenu} title="" />
    </div>
  )
}

function TypingLine({ channelId }: { channelId: string }) {
  const typing = useApp((s) => s.typing[channelId])
  const users = useApp((s) => s.users)
  const names = useMemo(() => {
    const now = Date.now()
    return Object.entries(typing ?? {})
      .filter(([, exp]) => exp > now)
      .map(([uid]) => displayName(users, uid))
  }, [typing, users])
  return (
    <div className="h-5 px-4 text-xs text-ink-2">
      {names.length > 0 && (
        <span className="animate-pulse">
          {names.join(', ')} {names.length === 1 ? 'is' : 'are'} typing…
        </span>
      )}
    </div>
  )
}

function MemberPanel({ groupId, open, onClose }: { groupId: string; open: boolean; onClose: () => void }) {
  const members = useApp((s) => s.groupMembers[groupId]) ?? []
  const users = useApp((s) => s.users)
  const online = useApp((s) => s.online)

  const sorted = useMemo(() => {
    return [...members].sort((a, b) => {
      const ao = online[a.user_id] ? 0 : 1
      const bo = online[b.user_id] ? 0 : 1
      if (ao !== bo) return ao - bo
      return displayName(users, a.user_id).localeCompare(displayName(users, b.user_id))
    })
  }, [members, users, online])

  return (
    <>
      {open && <div className="fixed inset-0 z-30 bg-black/40 lg:hidden" onClick={onClose} />}
      <aside
        className={cx(
          'fixed inset-y-0 right-0 z-40 w-60 translate-x-full bg-bg-1 border-l border-edge transition-transform',
          open && 'translate-x-0',
          'lg:static lg:z-auto', open ? 'lg:block' : 'lg:hidden',
        )}
      >
        <div className="border-b border-edge px-4 py-3 text-sm font-semibold text-ink">
          Members — {members.length}
        </div>
        <div className="space-y-0.5 overflow-y-auto p-2">
          {sorted.map((m) => {
            const u = users[m.user_id]
            return (
              <div key={m.user_id} className="flex items-center gap-2 rounded-lg px-2 py-1.5">
                <Avatar user={u} size={30} online={!!online[m.user_id]} />
                <div className="min-w-0">
                  <div className={cx('truncate text-sm', online[m.user_id] ? 'text-ink' : 'text-ink-2')}>
                    {displayName(users, m.user_id)}
                    {m.role !== 'member' && (
                      <span className="ml-1 rounded bg-bg-3 px-1 text-[10px] uppercase text-ink-1">{m.role}</span>
                    )}
                  </div>
                  {u?.status_text && <div className="truncate text-xs text-ink-2">{u.status_text}</div>}
                </div>
              </div>
            )
          })}
        </div>
      </aside>
    </>
  )
}

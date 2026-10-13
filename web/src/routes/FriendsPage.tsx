import { useMemo, useState } from 'react'
import { useNavigate } from 'react-router'
import { Check, MessageCircle, UserMinus, UserPlus, X } from 'lucide-react'
import { api, ApiError } from '../lib/api'
import { displayName, useApp } from '../store'
import { Avatar } from '../components/Avatar'
import { Button, cx, ErrorText, Input } from '../components/ui'
import { PageHeader } from './AppShell'

export default function FriendsPage({ onMenu }: { onMenu: () => void }) {
  const friends = useApp((s) => s.friends)
  const users = useApp((s) => s.users)
  const online = useApp((s) => s.online)
  const me = useApp((s) => s.me)
  const navigate = useNavigate()
  const [username, setUsername] = useState('')
  const [feedback, setFeedback] = useState<{ ok?: string; err?: string }>({})

  const { accepted, incoming, outgoing, blocked } = useMemo(() => {
    const accepted = friends.filter((f) => f.friendship.status === 'accepted')
    const incoming = friends.filter(
      (f) => f.friendship.status === 'pending' && f.friendship.addressee_id === me?.id,
    )
    const outgoing = friends.filter(
      (f) => f.friendship.status === 'pending' && f.friendship.requester_id === me?.id,
    )
    const blocked = friends.filter(
      (f) => f.friendship.status === 'blocked' && f.friendship.requester_id === me?.id,
    )
    const byName = (a: typeof friends[0], b: typeof friends[0]) =>
      displayName(users, a.user_id).localeCompare(displayName(users, b.user_id))
    return {
      accepted: [...accepted].sort(byName),
      incoming, outgoing, blocked,
    }
  }, [friends, users, me])

  async function sendRequest(e: React.FormEvent) {
    e.preventDefault()
    setFeedback({})
    try {
      await api.post('/api/v1/friends/requests', { username: username.trim() })
      setFeedback({ ok: `Request sent to ${username.trim()}` })
      setUsername('')
    } catch (err) {
      setFeedback({ err: err instanceof ApiError ? err.message : 'Could not send request' })
    }
  }

  async function openDM(userId: string) {
    const res = await api.post<{ channel: { id: string } }>('/api/v1/dms', { user_id: userId })
    navigate(`/app/dm/${res.channel.id}`)
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <PageHeader onMenu={onMenu} title="Friends" />
      <div className="flex-1 overflow-y-auto p-4">
        <form className="mb-6 flex max-w-md gap-2" onSubmit={sendRequest}>
          <Input
            value={username}
            onChange={(e) => setUsername(e.target.value)}
            placeholder="Add a friend by username"
          />
          <Button type="submit" disabled={!username.trim()}>
            <UserPlus size={16} /> Add
          </Button>
        </form>
        {feedback.ok && <p className="mb-4 text-sm text-success">{feedback.ok}</p>}
        <ErrorText>{feedback.err}</ErrorText>

        {incoming.length > 0 && (
          <Section title={`Incoming requests — ${incoming.length}`}>
            {incoming.map((f) => (
              <Row key={f.friendship.id} userId={f.user_id}>
                <ActionBtn
                  title="Accept"
                  className="text-success"
                  onClick={() => api.post(`/api/v1/friends/requests/${f.friendship.id}/accept`).catch(() => {})}
                >
                  <Check size={18} />
                </ActionBtn>
                <ActionBtn
                  title="Decline"
                  className="text-danger"
                  onClick={() => api.post(`/api/v1/friends/requests/${f.friendship.id}/decline`).catch(() => {})}
                >
                  <X size={18} />
                </ActionBtn>
              </Row>
            ))}
          </Section>
        )}

        {outgoing.length > 0 && (
          <Section title={`Pending — ${outgoing.length}`}>
            {outgoing.map((f) => (
              <Row key={f.friendship.id} userId={f.user_id}>
                <ActionBtn
                  title="Cancel request"
                  className="text-danger"
                  onClick={() => api.del(`/api/v1/friends/${f.user_id}`).catch(() => {})}
                >
                  <X size={18} />
                </ActionBtn>
              </Row>
            ))}
          </Section>
        )}

        <Section title={`Friends — ${accepted.length}`}>
          {accepted.length === 0 && (
            <p className="text-sm text-ink-2">
              No friends yet. Ask the server admin for usernames, or share a group invite.
            </p>
          )}
          {accepted.map((f) => (
            <Row
              key={f.friendship.id}
              userId={f.user_id}
              online={!!online[f.user_id]}
              onClick={() => openDM(f.user_id)}
            >
              <ActionBtn title="Message" onClick={() => openDM(f.user_id)}>
                <MessageCircle size={18} />
              </ActionBtn>
              <ActionBtn
                title="Unfriend"
                className="text-danger"
                onClick={() => {
                  if (confirm(`Remove ${displayName(users, f.user_id)} from friends?`)) {
                    api.del(`/api/v1/friends/${f.user_id}`).catch(() => {})
                  }
                }}
              >
                <UserMinus size={18} />
              </ActionBtn>
            </Row>
          ))}
        </Section>

        {blocked.length > 0 && (
          <Section title={`Blocked — ${blocked.length}`}>
            {blocked.map((f) => (
              <Row key={f.friendship.id} userId={f.user_id}>
                <Button
                  variant="ghost"
                  className="px-2 py-1 text-xs"
                  onClick={() => api.del(`/api/v1/friends/${f.user_id}/block`).catch(() => {})}
                >
                  Unblock
                </Button>
              </Row>
            ))}
          </Section>
        )}
      </div>
    </div>
  )
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div className="mb-6">
      <h2 className="mb-2 text-xs font-semibold uppercase tracking-wide text-ink-2">{title}</h2>
      <div className="space-y-1">{children}</div>
    </div>
  )
}

function Row({
  userId,
  online,
  onClick,
  children,
}: {
  userId: string
  online?: boolean
  onClick?: () => void
  children?: React.ReactNode
}) {
  const users = useApp((s) => s.users)
  const u = users[userId]
  return (
    <div
      onClick={onClick}
      role={onClick ? 'button' : undefined}
      className={cx(
        'flex max-w-md items-center gap-3 rounded-xl px-2 py-2 hover:bg-bg-3/60',
        onClick && 'cursor-pointer',
      )}
    >
      <Avatar user={u} size={36} online={online} />
      <div className="min-w-0 flex-1">
        <div className="truncate text-sm font-medium text-ink">{displayName(users, userId)}</div>
        <div className="truncate text-xs text-ink-2">
          {u?.status_text || (u ? `@${u.username}` : '')}
        </div>
      </div>
      {/* Action buttons act on their own; keep the row click out of them. */}
      <div className="flex gap-1" onClick={(e) => e.stopPropagation()}>
        {children}
      </div>
    </div>
  )
}

function ActionBtn({
  children,
  title,
  className,
  onClick,
}: {
  children: React.ReactNode
  title: string
  className?: string
  onClick: () => void
}) {
  return (
    <button
      title={title}
      onClick={onClick}
      className={cx('rounded-full bg-bg-2 p-2 text-ink-1 hover:bg-bg-3 hover:text-ink', className)}
    >
      {children}
    </button>
  )
}

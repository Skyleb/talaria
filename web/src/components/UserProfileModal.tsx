import { useState } from 'react'
import { useNavigate } from 'react-router'
import { Check, Clock, MessageCircle, UserPlus } from 'lucide-react'
import { api } from '../lib/api'
import { useApp } from '../store'
import { Avatar } from './Avatar'
import { Button, ErrorText, Modal } from './ui'

// UserProfileModal: the card shown when tapping any avatar or name in the
// app. Offers the right action for the relationship: add friend, accept a
// pending request, or message (DMs are friends-only server-side).
export function UserProfileModal() {
  const userId = useApp((s) => s.profileUserId)
  const closeProfile = useApp((s) => s.closeProfile)
  const users = useApp((s) => s.users)
  const friends = useApp((s) => s.friends)
  const online = useApp((s) => s.online)
  const me = useApp((s) => s.me)
  const navigate = useNavigate()
  const [error, setError] = useState('')
  const [sent, setSent] = useState(false)

  const u = userId ? users[userId] : null
  if (!userId || !u || !me) return null

  const entry = friends.find((f) => f.user_id === userId)
  const status = entry?.friendship.status
  const isSelf = userId === me.id
  const isFriend = status === 'accepted'
  const outgoing = status === 'pending' && entry?.friendship.requester_id === me.id
  const incoming = status === 'pending' && entry?.friendship.addressee_id === me.id

  function close() {
    setError('')
    setSent(false)
    closeProfile()
  }

  async function addFriend() {
    setError('')
    try {
      await api.post('/api/v1/friends/requests', { username: u!.username })
      setSent(true)
    } catch (e: any) {
      setError(e?.message ?? 'Could not send the request')
    }
  }

  async function accept() {
    setError('')
    try {
      await api.post(`/api/v1/friends/requests/${entry!.friendship.id}/accept`)
    } catch (e: any) {
      setError(e?.message ?? 'Could not accept the request')
    }
  }

  async function message() {
    setError('')
    try {
      const res = await api.post<{ channel: { id: string } }>('/api/v1/dms', { user_id: userId })
      close()
      navigate(`/app/dm/${res.channel.id}`)
    } catch (e: any) {
      setError(e?.message ?? 'Could not open the conversation')
    }
  }

  return (
    <Modal open onClose={close} title="">
      <div className="-mt-8 flex flex-col items-center gap-2 text-center">
        <Avatar user={u} size={80} online={!!online[userId]} />
        <div>
          <div className="text-lg font-bold text-ink">{u.display_name || u.username}</div>
          <div className="text-sm text-ink-2">@{u.username}</div>
        </div>
        {u.status_text && <p className="text-sm text-ink-1">{u.status_text}</p>}

        <div className="mt-3 flex w-full flex-col gap-2">
          {isSelf && <p className="text-sm text-ink-2">This is you.</p>}
          {isFriend && (
            <>
              <p className="flex items-center justify-center gap-1.5 text-sm text-success">
                <Check size={15} /> You're friends
              </p>
              <Button className="w-full" onClick={message}>
                <MessageCircle size={16} /> Message
              </Button>
            </>
          )}
          {incoming && (
            <Button className="w-full" onClick={accept}>
              <Check size={16} /> Accept friend request
            </Button>
          )}
          {(outgoing || sent) && !isFriend && (
            <p className="flex items-center justify-center gap-1.5 text-sm text-ink-1">
              <Clock size={15} /> Friend request sent
            </p>
          )}
          {!isSelf && !isFriend && !incoming && !outgoing && !sent && (
            <Button className="w-full" onClick={addFriend}>
              <UserPlus size={16} /> Add friend
            </Button>
          )}
          <ErrorText>{error}</ErrorText>
        </div>
      </div>
    </Modal>
  )
}

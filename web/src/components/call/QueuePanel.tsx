// QueuePanel: the listening queue as a drawer inside the call view (right
// side on desktop, bottom sheet on phones).
import { useState } from 'react'
import { ListMusic, Plus, Save, X } from 'lucide-react'
import { useListen } from '../../listen'
import { useApp } from '../../store'
import { cx, ErrorText } from '../ui'
import { AddModal, Members, QueueRow, SaveModal } from '../listen/queue'

export function QueuePanel({ channelId, onClose }: { channelId: string; onClose: () => void }) {
  const st = useListen((s) => s.sessions[channelId])
  const me = useApp((s) => s.me)!
  const { end, select, removeItem, transferHost } = useListen.getState()
  const [addOpen, setAddOpen] = useState(false)
  const [saveOpen, setSaveOpen] = useState(false)
  const [error, setError] = useState('')
  const session = st?.session
  if (!st || !session) return null
  const isHost = session.host_user_id === me.id

  function guard(p: Promise<unknown>) {
    setError('')
    p.catch((e: any) => setError(e?.message ?? 'That did not work'))
  }

  return (
    <>
      <div className="fixed inset-0 z-[55] bg-black/40 sm:hidden" onClick={onClose} />
      <aside
        className={cx(
          'fixed z-[56] flex flex-col bg-bg-1 shadow-xl',
          'inset-x-0 bottom-0 max-h-[70dvh] rounded-t-2xl pb-[env(safe-area-inset-bottom)]',
          'sm:inset-y-0 sm:left-auto sm:right-0 sm:w-80 sm:max-h-none sm:rounded-none sm:border-l sm:border-edge sm:pt-[env(safe-area-inset-top)]',
        )}
      >
        <div className="flex items-center gap-2 border-b border-edge px-3 py-2 text-xs font-semibold uppercase tracking-wide text-ink-2">
          <ListMusic size={14} /> Up next · {st.items.length}
          <div className="ml-auto flex items-center gap-1">
            <Members channelId={channelId} isHost={isHost} onMakeHost={(uid) => guard(transferHost(channelId, uid))} />
            <button
              title="Save this queue as a playlist"
              onClick={() => setSaveOpen(true)}
              disabled={st.items.length === 0}
              className="rounded-lg p-1.5 text-ink-1 hover:bg-bg-3 hover:text-ink disabled:opacity-40"
            >
              <Save size={14} />
            </button>
            <button title="Add to the queue" onClick={() => setAddOpen(true)} className="rounded-lg bg-accent/15 p-1.5 text-accent hover:bg-accent/25">
              <Plus size={14} />
            </button>
            <button title="Close" onClick={onClose} className="rounded-lg p-1.5 text-ink-1 hover:bg-bg-3 hover:text-ink">
              <X size={14} />
            </button>
          </div>
        </div>
        <div className="min-h-0 flex-1 overflow-y-auto p-1.5">
          {st.items.length === 0 && (
            <p className="px-2 py-6 text-center text-xs text-ink-2">The queue is empty. Add a song, video, or playlist.</p>
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
        <div className="px-3">
          <ErrorText>{error}</ErrorText>
        </div>
        {isHost && (
          <button
            onClick={() => {
              if (confirm('Stop listening together for everyone? The call continues.')) guard(end(channelId))
            }}
            className="border-t border-edge px-3 py-2 text-center text-xs text-ink-2 hover:text-danger"
          >
            Stop listening for everyone
          </button>
        )}
      </aside>
      <AddModal open={addOpen} onClose={() => setAddOpen(false)} channelId={channelId} />
      <SaveModal open={saveOpen} onClose={() => setSaveOpen(false)} channelId={channelId} />
    </>
  )
}

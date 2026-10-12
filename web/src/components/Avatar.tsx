import { avatarUrl } from '../lib/api'
import type { User } from '../lib/types'
import { cx } from './ui'

const palette = [
  '#6457f5', '#2d9d68', '#d6455d', '#c87f1e', '#1f8fb5', '#9a4ec7', '#5a7d2a',
]

export function Avatar({
  user,
  size = 40,
  online,
  className,
}: {
  user?: User
  size?: number
  online?: boolean
  className?: string
}) {
  const initial = (user?.display_name || user?.username || '?').slice(0, 1).toUpperCase()
  const colorIdx = user ? [...user.id].reduce((a, c) => a + c.charCodeAt(0), 0) % palette.length : 0
  return (
    <div className={cx('relative shrink-0', className)} style={{ width: size, height: size }}>
      {user?.avatar_file_id ? (
        <img
          src={avatarUrl(user.avatar_file_id, size <= 40 ? 64 : 128)}
          alt=""
          className="h-full w-full rounded-full object-cover"
          draggable={false}
        />
      ) : (
        <div
          className="flex h-full w-full items-center justify-center rounded-full font-semibold text-white"
          style={{ backgroundColor: palette[colorIdx], fontSize: size * 0.42 }}
        >
          {initial}
        </div>
      )}
      {online !== undefined && (
        <span
          className={cx(
            'absolute bottom-0 right-0 block rounded-full border-2 border-bg-1',
            online ? 'bg-success' : 'bg-ink-2',
          )}
          style={{ width: size * 0.3, height: size * 0.3 }}
        />
      )}
    </div>
  )
}

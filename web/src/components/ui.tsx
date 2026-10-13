// Small shared UI primitives, styled with the theme tokens.
import {
  type ButtonHTMLAttributes,
  type ComponentProps,
  type ReactNode,
  type SelectHTMLAttributes,
  useEffect,
} from 'react'

export function cx(...parts: Array<string | false | null | undefined>) {
  return parts.filter(Boolean).join(' ')
}

export function Button({
  variant = 'primary',
  className,
  ...props
}: ButtonHTMLAttributes<HTMLButtonElement> & { variant?: 'primary' | 'ghost' | 'danger' }) {
  const base =
    'inline-flex items-center justify-center gap-2 rounded-xl px-4 py-2 text-sm font-semibold transition-all ' +
    'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent/60 ' +
    'active:scale-[0.98] disabled:opacity-50 disabled:cursor-not-allowed disabled:active:scale-100'
  const styles = {
    primary: 'bg-accent text-accent-text shadow-sm hover:bg-accent-hover',
    ghost: 'bg-transparent text-ink-1 hover:bg-bg-3 hover:text-ink',
    danger: 'bg-danger text-white hover:opacity-90',
  }[variant]
  return <button className={cx(base, styles, className)} {...props} />
}

export function Input({ className, ...props }: ComponentProps<'input'>) {
  return (
    <input
      className={cx(
        'w-full rounded-xl border border-edge bg-bg-0 px-3 py-2 text-sm text-ink',
        'placeholder:text-ink-2 focus:border-accent focus:outline-none focus:ring-2 focus:ring-accent/25',
        className,
      )}
      {...props}
    />
  )
}

export function Select({ className, ...props }: SelectHTMLAttributes<HTMLSelectElement>) {
  return (
    <select
      className={cx(
        'w-full rounded-xl border border-edge bg-bg-0 px-3 py-2 text-sm text-ink',
        'focus:border-accent focus:outline-none focus:ring-2 focus:ring-accent/25',
        className,
      )}
      {...props}
    />
  )
}

// Segmented: a row of mutually exclusive choices (used for theme mode).
export function Segmented<T extends string>({
  value,
  onChange,
  options,
}: {
  value: T
  onChange: (v: T) => void
  options: { value: T; label: ReactNode }[]
}) {
  return (
    <div className="inline-flex rounded-xl bg-bg-2 p-1">
      {options.map((o) => (
        <button
          key={o.value}
          onClick={() => onChange(o.value)}
          className={cx(
            'flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-sm font-medium transition-all',
            value === o.value
              ? 'bg-bg-1 text-ink shadow-sm'
              : 'text-ink-1 hover:text-ink',
          )}
        >
          {o.label}
        </button>
      ))}
    </div>
  )
}

export function Label({ children }: { children: ReactNode }) {
  return <label className="mb-1 block text-xs font-semibold uppercase tracking-wide text-ink-1">{children}</label>
}

export function ErrorText({ children }: { children: ReactNode }) {
  if (!children) return null
  return <p className="text-sm text-danger">{children}</p>
}

export function Spinner({ className }: { className?: string }) {
  return (
    <div
      className={cx(
        'h-6 w-6 animate-spin rounded-full border-2 border-edge border-t-accent',
        className,
      )}
      role="status"
      aria-label="loading"
    />
  )
}

// Logo: the app icon (three wings on the accent gradient) as inline SVG so it
// follows the active theme.
export function Logo({ size = 32, className }: { size?: number; className?: string }) {
  return (
    <svg
      viewBox="0 0 512 512"
      width={size}
      height={size}
      className={cx('shrink-0', className)}
      style={{ borderRadius: size * 0.22 }}
      aria-label="Talaria"
      role="img"
    >
      <defs>
        <linearGradient id="talaria-logo-gradient" x1="0" y1="0" x2="1" y2="1">
          <stop offset="0" style={{ stopColor: 'var(--accent)' }} />
          <stop offset="1" style={{ stopColor: 'var(--accent-hover)' }} />
        </linearGradient>
      </defs>
      <rect width="512" height="512" fill="url(#talaria-logo-gradient)" />
      <g fill="#ffffff">
        <path d="M120 372 C150 250 260 150 420 130 C380 190 330 230 260 270 C220 295 170 340 120 372 Z" opacity="0.96" />
        <path d="M136 392 C190 320 290 270 410 250 C360 300 300 330 240 352 C200 366 165 384 136 392 Z" opacity="0.82" />
        <path d="M152 408 C210 365 300 340 392 346 C340 375 290 390 236 400 C200 406 172 410 152 408 Z" opacity="0.66" />
      </g>
    </svg>
  )
}

export function Modal({
  open,
  onClose,
  title,
  children,
}: {
  open: boolean
  onClose: () => void
  title: string
  children: ReactNode
}) {
  useEffect(() => {
    if (!open) return
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && onClose()
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [open, onClose])
  if (!open) return null
  return (
    <div
      className="fixed inset-0 z-50 flex items-end justify-center bg-black/50 sm:items-center"
      onClick={onClose}
    >
      <div
        className="max-h-[85dvh] w-full overflow-y-auto rounded-t-2xl bg-bg-1 p-5 shadow-xl sm:max-w-md sm:rounded-2xl"
        onClick={(e) => e.stopPropagation()}
      >
        <h2 className="mb-4 text-lg font-semibold text-ink">{title}</h2>
        {children}
      </div>
    </div>
  )
}

// Full-page centered card used by auth/setup screens.
export function AuthCard({
  title,
  subtitle,
  children,
}: {
  title: string
  subtitle?: string
  children: ReactNode
}) {
  return (
    <div className="flex min-h-full items-center justify-center bg-bg-0 p-4">
      <div className="w-full max-w-sm rounded-3xl border border-edge bg-bg-1 p-7 shadow-lg">
        <div className="mb-6 text-center">
          <Logo size={64} className="mx-auto mb-3 shadow-md" />
          <h1 className="text-xl font-bold text-ink">{title}</h1>
          <p className="mt-1 text-sm text-ink-1">{subtitle ?? 'Your own place to hang out'}</p>
        </div>
        {children}
      </div>
    </div>
  )
}

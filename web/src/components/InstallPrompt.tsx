// SetupPrompt (exported as InstallPrompt): a one-time "get set up" card for
// new users covering the two things worth doing once — installing Talaria
// as an app and turning on notifications. Each row shows a check when done.
// "Not now" snoozes it for a few days; "Don't ask again" hides it for good.
// Settings always has both options, and the card says so.
import { useEffect, useState } from 'react'
import { Bell, Check, Download, X } from 'lucide-react'
import {
  canInstall, enablePush, isIOS, isStandalone, onInstallChange, promptInstall, pushState,
  pushSupported, type PushState,
} from '../lib/notifications'
import { Button } from './ui'

const NEVER_KEY = 'talaria-install-prompt'
const SNOOZE_KEY = 'talaria-install-snooze'
const SNOOZE_MS = 3 * 24 * 3600 * 1000

export function InstallPrompt() {
  const [installable, setInstallable] = useState(canInstall())
  const [installed, setInstalled] = useState(isStandalone())
  const [push, setPush] = useState<PushState | null>(null)
  const [pushErr, setPushErr] = useState('')
  const [busy, setBusy] = useState(false)
  const [ready, setReady] = useState(false)
  const [hidden, setHidden] = useState(false)
  const [showHow, setShowHow] = useState(false)

  useEffect(() => onInstallChange(() => setInstallable(canInstall())), [])
  useEffect(() => {
    pushState().then(setPush)
    const onInstalled = () => setInstalled(true)
    window.addEventListener('appinstalled', onInstalled)
    return () => window.removeEventListener('appinstalled', onInstalled)
  }, [])
  // Give the app a moment to settle before asking anything.
  useEffect(() => {
    const t = setTimeout(() => setReady(true), 4000)
    return () => clearTimeout(t)
  }, [])

  if (!ready || hidden || push === null) return null
  if (localStorage.getItem(NEVER_KEY) === 'never') return null
  if (Date.now() < Number(localStorage.getItem(SNOOZE_KEY) ?? 0)) return null

  const ios = isIOS()
  const canOfferInstall = !installed && (installable || ios)
  const notificationsDone = push === 'enabled'
  const canOfferPush = pushSupported() && (push === 'off' || push === 'enabled')
  // Nothing left to offer (installed and notifications on, or unsupported).
  if (!canOfferInstall && !(canOfferPush && !notificationsDone) && push !== 'ios-needs-install') return null

  function notNow() {
    localStorage.setItem(SNOOZE_KEY, String(Date.now() + SNOOZE_MS))
    setHidden(true)
  }
  function never() {
    localStorage.setItem(NEVER_KEY, 'never')
    setHidden(true)
  }
  async function turnOnPush() {
    setBusy(true)
    setPushErr('')
    const res = await enablePush().catch((e: any) => ({ ok: false, reason: e?.message }))
    if (!res.ok) setPushErr(res.reason ?? 'Could not turn on notifications')
    setPush(await pushState())
    setBusy(false)
  }

  return (
    <div className="fixed inset-x-3 bottom-3 z-40 sm:inset-x-auto sm:right-4 sm:bottom-4 sm:w-[26rem]">
      <div className="rounded-2xl border border-edge bg-bg-1 p-4 shadow-xl">
        <div className="flex items-start gap-3">
          <div
            className="flex h-11 w-11 shrink-0 items-center justify-center rounded-xl text-2xl shadow"
            style={{ background: 'linear-gradient(135deg, var(--accent), var(--accent-hover))' }}
          >
            🪽
          </div>
          <div className="min-w-0 flex-1">
            <div className="font-semibold text-ink">Get the most out of Talaria</div>
            <p className="mt-0.5 text-sm text-ink-1">Two quick things, both optional.</p>
          </div>
          <button onClick={notNow} title="Not now" className="rounded-lg p-1 text-ink-2 hover:bg-bg-3 hover:text-ink">
            <X size={16} />
          </button>
        </div>

        <div className="mt-3 space-y-2">
          {/* Install */}
          <Row
            icon={<Download size={16} />}
            done={installed}
            title={ios ? 'Add to your Home Screen' : 'Install as an app'}
            detail={installed ? 'Installed' : ios ? 'Own icon, full screen, and it enables notifications on iPhone.' : 'Own icon and window, like a regular app.'}
            action={
              installed ? null : ios ? (
                !showHow && (
                  <Button className="px-3 py-1.5 text-xs" onClick={() => setShowHow(true)}>
                    Show me how
                  </Button>
                )
              ) : installable ? (
                <Button className="px-3 py-1.5 text-xs" onClick={() => promptInstall()}>
                  Install
                </Button>
              ) : null
            }
          />
          {ios && showHow && !installed && (
            <ol className="ml-9 list-decimal space-y-0.5 pl-4 text-sm text-ink-1">
              <li>Tap the <strong>Share</strong> button in Safari (the square with an arrow).</li>
              <li>Choose <strong>Add to Home Screen</strong>, then <strong>Add</strong>.</li>
              <li>Open Talaria from your Home Screen and come back here for notifications.</li>
            </ol>
          )}

          {/* Notifications */}
          <Row
            icon={<Bell size={16} />}
            done={notificationsDone}
            title="Turn on notifications"
            detail={
              notificationsDone
                ? 'On for this device'
                : push === 'ios-needs-install'
                  ? 'Available once Talaria is on your Home Screen.'
                  : push === 'denied'
                    ? "Blocked in your browser's site settings."
                    : push === 'unsupported'
                      ? 'Not supported in this browser.'
                      : 'Direct messages, @mentions, and calls, even when Talaria is closed.'
            }
            action={
              !notificationsDone && push === 'off' ? (
                <Button className="px-3 py-1.5 text-xs" onClick={turnOnPush} disabled={busy}>
                  {busy ? 'Working…' : 'Turn on'}
                </Button>
              ) : null
            }
          />
          {pushErr && <p className="ml-9 text-xs text-danger">{pushErr}</p>}
        </div>

        <div className="mt-3 flex flex-wrap items-center gap-2">
          <p className="text-xs text-ink-2">You can always do both later from Settings.</p>
          <div className="ml-auto flex items-center gap-2">
            <Button variant="ghost" className="px-3 py-1.5 text-sm" onClick={notNow}>
              Not now
            </Button>
            <button onClick={never} className="text-xs text-ink-2 hover:text-ink hover:underline">
              Don't ask again
            </button>
          </div>
        </div>
      </div>
    </div>
  )
}

function Row({
  icon, done, title, detail, action,
}: {
  icon: React.ReactNode
  done: boolean
  title: string
  detail: string
  action: React.ReactNode
}) {
  return (
    <div className="flex items-center gap-3 rounded-xl bg-bg-2 px-3 py-2">
      <span className={done ? 'text-success' : 'text-accent'}>{done ? <Check size={16} /> : icon}</span>
      <div className="min-w-0 flex-1">
        <div className={`text-sm font-medium ${done ? 'text-ink-1 line-through' : 'text-ink'}`}>{title}</div>
        <div className="text-xs text-ink-2">{detail}</div>
      </div>
      {action}
    </div>
  )
}

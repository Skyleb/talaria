// Web Push plumbing: service worker registration, permission, subscribe/
// unsubscribe against the server, and the app-icon unread badge.
import { api } from './api'

export type PushState = 'unsupported' | 'ios-needs-install' | 'denied' | 'enabled' | 'off'

function isIOS(): boolean {
  return /iP(hone|ad|od)/.test(navigator.userAgent)
}

export function isStandalone(): boolean {
  return matchMedia('(display-mode: standalone)').matches || (navigator as any).standalone === true
}

export function pushSupported(): boolean {
  return 'serviceWorker' in navigator && 'PushManager' in window && 'Notification' in window
}

export async function registerServiceWorker(): Promise<ServiceWorkerRegistration | null> {
  if (!('serviceWorker' in navigator)) return null
  try {
    return await navigator.serviceWorker.register('/sw.js')
  } catch {
    return null
  }
}

export async function pushState(): Promise<PushState> {
  if (!pushSupported()) return isIOS() && !isStandalone() ? 'ios-needs-install' : 'unsupported'
  if (Notification.permission === 'denied') return 'denied'
  const reg = await navigator.serviceWorker.getRegistration()
  const sub = await reg?.pushManager.getSubscription()
  return sub ? 'enabled' : 'off'
}

function urlBase64ToUint8Array(b64: string): Uint8Array {
  const padding = '='.repeat((4 - (b64.length % 4)) % 4)
  const base64 = (b64 + padding).replace(/-/g, '+').replace(/_/g, '/')
  const raw = atob(base64)
  const out = new Uint8Array(raw.length)
  for (let i = 0; i < raw.length; i++) out[i] = raw.charCodeAt(i)
  return out
}

export async function enablePush(): Promise<{ ok: boolean; reason?: string }> {
  if (!pushSupported()) return { ok: false, reason: 'Notifications are not supported in this browser.' }
  const perm = await Notification.requestPermission()
  if (perm !== 'granted') return { ok: false, reason: 'Permission was not granted.' }
  const reg = (await navigator.serviceWorker.getRegistration()) ?? (await registerServiceWorker())
  if (!reg) return { ok: false, reason: 'Could not start the background worker.' }
  await navigator.serviceWorker.ready
  const { public_key } = await api.get<{ public_key: string }>('/api/v1/push/vapid')
  let sub = await reg.pushManager.getSubscription()
  if (!sub) {
    sub = await reg.pushManager.subscribe({
      userVisibleOnly: true,
      applicationServerKey: urlBase64ToUint8Array(public_key) as BufferSource,
    })
  }
  await api.post('/api/v1/push/subscriptions', sub.toJSON())
  return { ok: true }
}

export async function disablePush(): Promise<void> {
  const reg = await navigator.serviceWorker.getRegistration()
  const sub = await reg?.pushManager.getSubscription()
  if (!sub) return
  await fetch('/api/v1/push/subscriptions', {
    method: 'DELETE',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ endpoint: sub.endpoint }),
    credentials: 'same-origin',
  }).catch(() => {})
  await sub.unsubscribe().catch(() => {})
}

// syncPush re-registers an existing device subscription with the server on
// sign-in, so a subscription created under another account (or lost in a
// restore) is attributed to whoever is signed in now.
export async function syncPush(): Promise<void> {
  if (!pushSupported() || Notification.permission !== 'granted') return
  const reg = await navigator.serviceWorker.getRegistration()
  const sub = await reg?.pushManager.getSubscription()
  if (sub) await api.post('/api/v1/push/subscriptions', sub.toJSON()).catch(() => {})
}

export function setAppBadge(n: number) {
  const nav = navigator as any
  if (n > 0) nav.setAppBadge?.(n)
  else nav.clearAppBadge?.()
}

// --- Install prompt (Android/desktop Chromium) ---
// The browser offers installation via a deferred event; we keep it so the
// Settings page can show an "Install" button at a sensible moment.
let deferredInstall: any = null
const installListeners = new Set<() => void>()
window.addEventListener('beforeinstallprompt', (e) => {
  e.preventDefault()
  deferredInstall = e
  installListeners.forEach((fn) => fn())
})
window.addEventListener('appinstalled', () => {
  deferredInstall = null
  installListeners.forEach((fn) => fn())
})

export function canInstall(): boolean {
  return !!deferredInstall
}

export function onInstallChange(fn: () => void): () => void {
  installListeners.add(fn)
  return () => {
    installListeners.delete(fn)
  }
}

export async function promptInstall(): Promise<void> {
  if (!deferredInstall) return
  deferredInstall.prompt()
  await deferredInstall.userChoice.catch(() => {})
  deferredInstall = null
  installListeners.forEach((fn) => fn())
}

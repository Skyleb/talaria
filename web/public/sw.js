// Talaria service worker: receives Web Push notifications and routes taps
// back into the app. It deliberately does no asset caching: the app is
// served by the same box it talks to, so there is nothing to speed up and
// stale bundles would only cause confusion.

self.addEventListener('install', () => self.skipWaiting())
self.addEventListener('activate', (e) => e.waitUntil(self.clients.claim()))

self.addEventListener('push', (e) => {
  let data = {}
  try {
    data = e.data ? e.data.json() : {}
  } catch (err) {
    data = { title: 'Talaria', body: e.data ? e.data.text() : '' }
  }
  e.waitUntil(
    (async () => {
      // If the user is already looking at that conversation, stay quiet.
      const clients = await self.clients.matchAll({ type: 'window', includeUncontrolled: true })
      const looking = clients.some((c) => {
        if (c.visibilityState !== 'visible' || !c.focused || !data.channel_id) return false
        try {
          return new URL(c.url).pathname.endsWith('/' + data.channel_id)
        } catch (err) {
          return false
        }
      })
      if (looking) return
      const isCall = data.type === 'call'
      await self.registration.showNotification(data.title || 'Talaria', {
        body: data.body || '',
        icon: data.icon || '/icons/icon-192.png',
        badge: '/icons/badge-96.png',
        tag: data.tag || undefined,
        renotify: true,
        requireInteraction: isCall,
        vibrate: isCall ? [250, 100, 250, 100, 250] : [80],
        data: { url: data.url || '/app' },
      })
    })(),
  )
})

self.addEventListener('notificationclick', (e) => {
  e.notification.close()
  const url = (e.notification.data && e.notification.data.url) || '/app'
  e.waitUntil(
    (async () => {
      const clients = await self.clients.matchAll({ type: 'window', includeUncontrolled: true })
      for (const c of clients) {
        if ('focus' in c) {
          await c.focus()
          if ('navigate' in c) {
            try {
              await c.navigate(url)
            } catch (err) {
              // uncontrolled client; fall through to opening a window
            }
          }
          return
        }
      }
      await self.clients.openWindow(url)
    })(),
  )
})

// The page's service worker. It caches nothing: every request goes to the
// network, exactly as it would without this file. It exists so a phone
// offers to install the page, and it is where Web Push lands.
//
// A push carries {title, body, url, tag} (internal/web/push.Message): it
// becomes a notification, and tapping it opens url ("/#FD-012") — in the
// board's own window when one is open (focused, and told where to go),
// else in a new one. Nothing is answered from a notification: an answer
// is read before it is given.

self.addEventListener('install', () => self.skipWaiting())
self.addEventListener('activate', (e) => e.waitUntil(self.clients.claim()))

function message (data) {
  if (!data) return {}
  try { return data.json() || {} } catch { return { body: data.text() } }
}

self.addEventListener('push', (e) => {
  const m = message(e.data)
  const url = typeof m.url === 'string' && /^\/(?!\/)/.test(m.url) ? m.url : '/'
  e.waitUntil(self.registration.showNotification(m.title || 'gummi needs you', {
    body: m.body || '',
    tag: m.tag || undefined,
    renotify: !!m.tag,
    icon: '/assets/icon.svg',
    badge: '/assets/icon.svg',
    data: { url }
  }))
})

self.addEventListener('notificationclick', (e) => {
  e.notification.close()
  let target = new URL(e.notification.data?.url || '/', self.location.origin)
  if (target.origin !== self.location.origin) target = new URL('/', self.location.origin)
  e.waitUntil(openBoard(target))
})

// openBoard focuses a window already on the board and sends it to the
// card, or opens one. Only a same-origin window counts: the push came from
// this board, and its link must not land on another.
async function openBoard (target) {
  const wins = await self.clients.matchAll({ type: 'window', includeUncontrolled: true })
  const win = wins.find(c => new URL(c.url).origin === target.origin)
  if (win) {
    await win.focus()
    win.postMessage({ type: 'gummi:open', url: target.pathname + target.search + target.hash })
    return win
  }
  return self.clients.openWindow(target.href)
}

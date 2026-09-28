// push.js — "Notifications on this device": Web Push from the board's own
// host (DESIGN §20.4). Turning it on asks the browser for permission,
// subscribes the service worker with the server's VAPID key
// (GET /api/push/key) and hands the subscription to the server
// (POST /api/push/subscribe), which keeps one per device. Off unsubscribes
// and tells the server (DELETE). A browser that cannot (no service worker
// or push service, an insecure origin, notifications blocked) says so in
// plain words instead of offering a switch that does nothing.

import { h, clear, append } from './dom.js?v=__ASSET_V__'
import { get, post, del } from './api.js?v=__ASSET_V__'
import { openModal } from './views.js?v=__ASSET_V__'
import { toast } from './toast.js?v=__ASSET_V__'

// registerWorker registers sw.js and follows what it asks the page to open
// (a tapped notification, when this window was already on the board).
export function registerWorker () {
  if (!('serviceWorker' in navigator)) return
  navigator.serviceWorker.register('/sw.js').catch(() => { /* a page without one still works */ })
  navigator.serviceWorker.addEventListener('message', (e) => {
    const m = e.data || {}
    if (m.type !== 'gummi:open' || typeof m.url !== 'string') return
    const u = new URL(m.url, location.origin)
    if (u.origin !== location.origin) return
    if (u.hash && u.pathname === location.pathname) location.hash = u.hash
    else location.assign(u.href)
  })
}

const WORDS = {
  on: ['On', 'This device gets a notification when a card stops for you — a gate, a question, a spent budget. Tapping it opens the card.'],
  off: ['Off', 'Get a notification on this device when a card stops for you — a gate, a question, a spent budget. Tapping it opens the card; answering still happens here.'],
  denied: ['Blocked', 'Notifications are blocked for this site in the browser’s settings. Allow them there, then come back here to turn them on.'],
  unsupported: ['Not available', 'This browser cannot receive notifications from a page. On an iPhone or iPad, add the board to the Home Screen and open it from there.'],
  insecure: ['Not available', 'Notifications need a secure address: HTTPS (gummi web --tailscale --ts-tls, or --tls-cert), or this machine’s own localhost.'],
  unavailable: ['Not set up', 'This board’s server does not send notifications.'],
  unknown: ['…', 'Checking this browser…']
}

// status reads where this device stands: supported at all, allowed, and
// subscribed.
export async function status () {
  if (!('serviceWorker' in navigator) || !('PushManager' in window) || !('Notification' in window)) return { state: 'unsupported' }
  if (!window.isSecureContext) return { state: 'insecure' }
  if (Notification.permission === 'denied') return { state: 'denied' }
  const reg = await navigator.serviceWorker.getRegistration('/')
  const sub = await reg?.pushManager.getSubscription()
  return { state: sub ? 'on' : 'off', sub }
}

function keyBytes (b64) {
  const s = b64.replace(/-/g, '+').replace(/_/g, '/') + '==='.slice((b64.length + 3) % 4)
  return Uint8Array.from(atob(s), c => c.charCodeAt(0))
}

// enable subscribes this device. It throws an Error whose message is for a
// person; its .state, when set, is the status the refusal leaves.
export async function enable () {
  let key
  try {
    key = (await get('/api/push/key')).key
  } catch (err) {
    throw Object.assign(new Error(err.message), { state: err.status === 404 ? 'unavailable' : null })
  }
  const perm = await Notification.requestPermission()
  if (perm === 'denied') throw Object.assign(new Error('Notifications were blocked for this site.'), { state: 'denied' })
  if (perm !== 'granted') throw new Error('The browser did not allow notifications. Try again, and choose Allow.')
  const reg = await navigator.serviceWorker.register('/sw.js')
  await navigator.serviceWorker.ready
  let sub
  try {
    sub = await reg.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: keyBytes(key) })
  } catch (err) {
    throw new Error(`This browser could not subscribe (${err.message || err.name}). A browser without a push service — a headless one, or one with it switched off — cannot be notified.`)
  }
  try {
    await post('/api/push/subscribe', sub.toJSON())
  } catch (err) {
    await sub.unsubscribe().catch(() => {})
    throw err
  }
}

// disable unsubscribes this device, here and on the server.
export async function disable () {
  const reg = await navigator.serviceWorker.getRegistration('/')
  const sub = await reg?.pushManager.getSubscription()
  if (sub) await sub.unsubscribe().catch(() => {})
  await del('/api/push/subscribe')
}

// openPush is the More menu's dialog: the device's state and its switch.
export function openPush () {
  const body = h('div', { class: 'push-box', testid: 'push-body' })
  const m = openModal({ title: 'Notifications on this device', testid: 'push-dialog', body })
  let busy = false
  let problem = ''
  const draw = (st) => {
    clear(body)
    const [word, text] = WORDS[st] || WORDS.unknown
    append(body, [
      h('div', { class: ['push-state', st], testid: 'push-state', data: { state: st } }, h('span', { class: 'dotp', 'aria-hidden': 'true' }), word),
      h('p', { class: 'push-text' }, text),
      problem ? h('p', { class: 'aerr', testid: 'push-error', role: 'alert' }, problem) : null,
      st === 'off' ? h('button', { class: 'btn pri', type: 'button', testid: 'push-on', disabled: busy, onclick: () => flip(true) }, 'Turn on') : null,
      st === 'on' ? h('button', { class: 'btn', type: 'button', testid: 'push-off', disabled: busy, onclick: () => flip(false) }, 'Turn off') : null])
  }
  const refresh = async () => {
    try { draw((await status()).state) } catch (err) { problem = err.message; draw('off') }
  }
  const flip = async (on) => {
    busy = true
    problem = ''
    refresh()
    try {
      if (on) await enable()
      else await disable()
      toast(on ? 'Notifications are on for this device' : 'Notifications are off for this device')
      busy = false
      await refresh()
    } catch (err) {
      busy = false
      problem = err.message
      if (err.state) draw(err.state)
      else await refresh()
    }
  }
  draw('unknown')
  refresh()
  return m
}

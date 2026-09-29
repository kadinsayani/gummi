// api.js — the page's one door to the server: same-origin JSON fetches that
// throw an ApiError carrying the status and the server's error body
// (internal/webapi.Error), so callers can tell a 409 race from a route that
// is not built yet.
//
// A question the server stopped at — an input a flow needs, a confirmation
// to give, a line that reads as a new card — comes back 202 Accepted with
// that same body (webapi.StatusQuestion): it is ordinary control flow, not a
// failed load, so the browser logs nothing. Here it becomes the very ApiError
// a 409 "needs" / "confirm" / "newcard" used to, status 409 and all, so
// every caller asks it exactly as it always has.

// QUESTIONS are the error words a 202 carries a question with
// (webapi.IsQuestion).
const QUESTIONS = new Set(['needs', 'confirm', 'newcard'])

export class ApiError extends Error {
  constructor (status, data, fallback) {
    super((data && data.error) || fallback || `HTTP ${status}`)
    this.status = status
    this.data = data || {}
  }

  // notBuilt is a route this server does not answer yet (501), or at all.
  get notBuilt () { return this.status === 501 || this.status === 404 || this.status === 405 }
}

let onUnauthorized = null
export function setUnauthorizedHandler (fn) { onUnauthorized = fn }

// A read that failed on the way (the board unreachable, a 5xx) was
// usually asked for by an event saying something changed; nothing will
// say so again, so the page would keep drawing what it had. onMissed is
// told of each such failure and onRead of each read that went through,
// so the page can fetch again until one does.
let reads = { missed: null, ok: null }
export function setReadHandlers ({ missed, ok }) { reads = { missed, ok } }

export async function api (method, path, body) {
  const init = { method, credentials: 'same-origin', headers: { Accept: 'application/json' } }
  if (body !== undefined) {
    init.headers['Content-Type'] = 'application/json'
    init.body = JSON.stringify(body)
  }
  const read = method === 'GET'
  let res
  try {
    res = await fetch(path, init)
  } catch (err) {
    if (read) reads.missed?.()
    throw new ApiError(0, null, 'the board cannot be reached')
  }
  if (read) {
    if (res.status >= 500 && res.status !== 501) reads.missed?.()
    else reads.ok?.()
  }
  const text = await res.text()
  let data = null
  if (text) {
    try { data = JSON.parse(text) } catch { data = null }
  }
  if (!res.ok) {
    if (res.status === 401 && onUnauthorized) onUnauthorized()
    throw new ApiError(res.status, data, res.statusText)
  }
  if (res.status === 202 && data && QUESTIONS.has(data.error)) {
    // a question, not a result: the page answers it where it asks
    const q = new ApiError(409, data)
    q.question = true
    throw q
  }
  return data
}

export const get = (path) => api('GET', path)
export const post = (path, body = {}) => api('POST', path, body)
export const del = (path) => api('DELETE', path)

// cardPath builds /api/cards/<id>/<rest> with the id escaped.
export function cardPath (id, rest = '') {
  return `/api/cards/${encodeURIComponent(id)}${rest ? '/' + rest : ''}`
}

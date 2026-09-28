// api.js — the page's one door to the server: same-origin JSON fetches that
// throw an ApiError carrying the status and the server's error body
// (internal/webapi.Error), so callers can tell a 409 race from a route that
// is not built yet.

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

export async function api (method, path, body) {
  const init = { method, credentials: 'same-origin', headers: { Accept: 'application/json' } }
  if (body !== undefined) {
    init.headers['Content-Type'] = 'application/json'
    init.body = JSON.stringify(body)
  }
  let res
  try {
    res = await fetch(path, init)
  } catch (err) {
    throw new ApiError(0, null, 'the board cannot be reached')
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
  return data
}

export const get = (path) => api('GET', path)
export const post = (path, body = {}) => api('POST', path, body)
export const del = (path) => api('DELETE', path)

// cardPath builds /api/cards/<id>/<rest> with the id escaped.
export function cardPath (id, rest = '') {
  return `/api/cards/${encodeURIComponent(id)}${rest ? '/' + rest : ''}`
}

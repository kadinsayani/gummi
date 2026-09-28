// selection.js — the open card and what the page holds about it: the head
// (GET /api/cards/{id}), the thread (upserted by key from ?after=<seq>) and
// the live moment (GET …/live). It reacts to the event stream: a "card"
// event refetches the head and the thread's newer items, a "live" event the
// live block. It never decides anything about the card; it only fetches.

import { get, cardPath } from './api.js?v=__ASSET_V__'
import { on, set, state, rows } from './store.js?v=__ASSET_V__'
import { write as writeHash } from './router.js?v=__ASSET_V__'
import { isMobile } from './dom.js?v=__ASSET_V__'

export async function loadBoard () {
  try {
    const b = await get('/api/board')
    const patch = { board: b }
    if (Array.isArray(b.viewers) && b.viewers.length) patch.viewers = b.viewers
    set(patch)
    // the open card's head came from its own route; when the board says
    // its row moved (a stage, waits, the budget), read the head again
    const r = state.sel && b.rows?.find(x => x.id === state.sel)
    if (r && state.card && rowMoved(r, state.card)) loadCard(state.sel)
  } catch (err) {
    if (!state.board) set({ board: { repo: state.session?.repo || '', rows: [], counts: { needs: 0, running: 0 }, today: { spent: 0 }, viewers: [] } })
  }
}

const HEAD_FIELDS = ['stage', 'status', 'envelope', 'autopilot', 'landed', 'profile', 'repo', 'elsewhere']

function rowMoved (r, c) {
  return HEAD_FIELDS.some(k => (r[k] ?? null) !== (c[k] ?? null)) ||
    JSON.stringify(r.waits || []) !== JSON.stringify(c.waits || []) ||
    JSON.stringify(r.stack || null) !== JSON.stringify(c.stack || null)
}

// select opens a card. tab, when given, wins; otherwise the panel follows
// the card's pinned decision (the spec for a design gate, the diff for a
// failed verify), and stays where it was when there is none.
export async function select (id, { tab = null, view = true } = {}) {
  if (!id) return
  const changed = id !== state.sel
  if (changed) {
    set({ sel: id, card: null, cardErr: null, thread: null, live: null, hi: 0, showNext: null, mdecOpen: false })
  }
  if (tab && tab !== state.tab) set({ tab })
  writeHash(id, tab)
  if (view && isMobile()) set({ view: 'thread' })
  const card = await loadCard(id)
  if (changed && !tab && card?.decision && card.decision.anchor !== 'thread' && state.sel === id) {
    set({ tab: card.decision.anchor })
  }
  if (changed) {
    loadThread(id, true)
    loadLive(id)
  }
}

export async function loadCard (id = state.sel) {
  if (!id) return null
  try {
    const c = await get(cardPath(id))
    if (state.sel !== id) return c
    const patch = { card: c, cardErr: null }
    const n = c.decision?.options?.length || 0
    if (state.hi >= n) patch.hi = 0
    set(patch)
    return c
  } catch (err) {
    if (state.sel === id) set({ cardErr: err })
    return null
  }
}

export async function loadThread (id = state.sel, full = false) {
  if (!id) return
  const after = !full && state.thread?.items ? state.thread.lastSeq || 0 : 0
  try {
    const t = await get(cardPath(id, 'thread') + (after ? `?after=${after}` : ''))
    if (state.sel !== id) return
    const items = after ? upsert(state.thread.items, t.items || []) : (t.items || [])
    const lastSeq = Math.max(t.lastSeq || 0, after)
    set({ thread: { items, lastSeq } })
    if (t.live !== undefined && !liveRoute) set({ live: t.live || null })
  } catch (err) {
    if (state.sel !== id) return
    set({ thread: { items: state.thread?.items || [], lastSeq: state.thread?.lastSeq || 0, unavailable: err.notBuilt, err } })
  }
}

// upsert merges newer items into the list by key: an item that grew
// replaces its old self in place; a new one is appended.
export function upsert (items, fresh) {
  const out = items.slice()
  const at = new Map(out.map((it, i) => [it.key, i]))
  for (const it of fresh) {
    if (at.has(it.key)) out[at.get(it.key)] = it
    else { at.set(it.key, out.length); out.push(it) }
  }
  return out
}

// The live route may not exist on an older server; the thread's own live
// field stands in for it then.
let liveRoute = true
let liveBusy = false
let liveAgain = false
export async function loadLive (id = state.sel) {
  if (!id || !liveRoute) { if (!liveRoute) loadThread(id); return }
  if (liveBusy) { liveAgain = true; return }
  liveBusy = true
  try {
    const l = await get(cardPath(id, 'live'))
    if (state.sel === id) set({ live: l || null })
  } catch (err) {
    if (err.notBuilt) liveRoute = false
  } finally {
    liveBusy = false
    if (liveAgain) { liveAgain = false; loadLive() }
  }
}

// refresh is what a "card" event for the open card does.
export function refresh (id = state.sel) {
  if (!id || id !== state.sel) return
  loadCard(id)
  loadThread(id)
  set({ cardRev: (state.cardRev || 0) + 1 })
}

export function refreshAll () {
  loadBoard()
  if (state.sel) {
    loadCard(state.sel)
    loadThread(state.sel, true)
    loadLive(state.sel)
    set({ cardRev: (state.cardRev || 0) + 1 })
  }
}

// nextNeeding opens the next card that needs a person, after the open one.
export function nextNeeding (toast) {
  const list = rows().filter(r => r.status === 'needs')
  if (!list.length) { toast?.('Nothing needs you'); return }
  const i = list.findIndex(r => r.id === state.sel)
  select(list[(i + 1) % list.length].id)
}

// step walks the rail: +1 is the next visible row, -1 the previous one.
export function step (dir, ids) {
  if (!ids.length) return
  const i = ids.indexOf(state.sel)
  const n = i < 0 ? (dir > 0 ? 0 : ids.length - 1) : (i + dir + ids.length) % ids.length
  select(ids[n], { view: false })
}

export function initSelection () {
  // a card that left the board (deleted) takes the selection with it
  on('board', () => {
    if (state.sel && state.board && !rows().some(r => r.id === state.sel) && state.card) {
      const first = rows().find(r => r.status === 'needs') || rows()[0]
      if (first) select(first.id, { view: false })
    }
  })
}

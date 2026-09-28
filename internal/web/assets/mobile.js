// mobile.js — the phone layout (≤ 760px): one of three views at a time —
// the cards, the thread, the documents — switched from the bottom bar,
// which also counts what needs you. The pinned decision follows into the
// cards and documents views as a docked bar (decision.js draws it).

import { $, h, clear, icon, isMobile } from './dom.js?v=__ASSET_V__'
import { on, set, state } from './store.js?v=__ASSET_V__'
import { pushLayer } from './back.js?v=__ASSET_V__'

const VIEWS = [['cards', 'Cards', 'cards'], ['thread', 'Thread', 'thread'], ['panel', 'Spec · Diff', 'doc']]

export function initMobile () {
  const nav = $('#mnav')
  for (const [v, label, ic] of VIEWS) {
    nav.append(h('button', { type: 'button', data: { v }, testid: `mnav-${v}`, 'aria-label': label, onclick: () => set({ view: v }) },
      icon(ic), label, v === 'cards' ? h('span', { class: 'dotn', id: 'm-needs', 'aria-hidden': 'true' }) : null))
  }
  on(['view'], apply)
  on(['view'], layer)
  on(['board'], count)
  window.addEventListener('resize', () => { apply(); layer() })
  apply()
  layer()
}

// On a phone the cards are the root, and the thread or the documents sit on
// them as one layer (back.js): back from either returns to the cards, and
// back from the cards leaves the page. Moving between the thread and the
// documents is not a step back.
let viewDone = null
function layer () {
  const up = isMobile() && state.view !== 'cards'
  if (up && !viewDone) {
    viewDone = pushLayer(() => { viewDone = null; set({ view: 'cards' }) })
  } else if (!up && viewDone) {
    const done = viewDone
    viewDone = null
    done()
  }
}

function apply () {
  $('#app').dataset.view = state.view
  for (const b of $('#mnav').children) {
    const onv = b.dataset.v === state.view
    b.classList.toggle('on', onv)
    b.setAttribute('aria-current', onv ? 'page' : 'false')
  }
  if (isMobile() && state.view === 'thread') {
    const sc = $('#thread')
    requestAnimationFrame(() => { sc.scrollTop = sc.scrollHeight })
  }
}

function count () {
  const el = $('#m-needs')
  if (!el) return
  const n = state.board?.counts?.needs || 0
  clear(el).append(String(n))
  el.hidden = !n
}

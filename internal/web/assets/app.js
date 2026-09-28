// app.js — the page's entry. It asks who this browser is (GET /api/session)
// and shows either the pairing form or the board; for the board it wires
// the modules together, loads the rows and the card the address names, and
// opens the event stream that keeps them current.

import { $, isMobile } from './dom.js?v=__ASSET_V__'
import { get, post, setUnauthorizedHandler } from './api.js?v=__ASSET_V__'
import { set, state, rows, on } from './store.js?v=__ASSET_V__'
import { connect, close as closeEvents } from './events.js?v=__ASSET_V__'
import { parse, onRoute } from './router.js?v=__ASSET_V__'
import { initTheme } from './theme.js?v=__ASSET_V__'
import { initViewport } from './viewport.js?v=__ASSET_V__'
import { initBack } from './back.js?v=__ASSET_V__'
import { toast } from './toast.js?v=__ASSET_V__'
import { setViewContext, openModal, closeOverlay } from './views.js?v=__ASSET_V__'
import { onEvent } from './events.js?v=__ASSET_V__'
import * as api from './api.js?v=__ASSET_V__'
import { initTop } from './top.js?v=__ASSET_V__'
import { initRail, toggleRail, visibleIds } from './rail.js?v=__ASSET_V__'
import { initHead } from './head.js?v=__ASSET_V__'
import { initThread, jumpToStage } from './thread.js?v=__ASSET_V__'
import { initDecision } from './decision.js?v=__ASSET_V__'
import { initComposer } from './composer.js?v=__ASSET_V__'
import { initPanel, setTab, togglePanel } from './panel.js?v=__ASSET_V__'
import { initMobile } from './mobile.js?v=__ASSET_V__'
import { initKeys } from './keys.js?v=__ASSET_V__'
import { palette, keysHelp } from './palette.js?v=__ASSET_V__'
import { showPair } from './pair.js?v=__ASSET_V__'
import { h } from './dom.js?v=__ASSET_V__'
import {
  initSelection, loadBoard, select, refresh, refreshAll, loadLive, nextNeeding, step
} from './selection.js?v=__ASSET_V__'
import { initResume } from './resume.js?v=__ASSET_V__'
import { registerWorker } from './push.js?v=__ASSET_V__'
import './views/index.js?v=__ASSET_V__'

let started = false

async function boot () {
  initTheme()
  initViewport()
  initBack()
  let session
  try {
    session = await get('/api/session')
  } catch (err) {
    $('#boot').textContent = `The board did not answer: ${err.message}`
    return
  }
  set({ session })
  $('#boot').hidden = true
  if (!session.authed && !session.openAccess) {
    $('#app').hidden = true
    showPair(session, () => { $('#pair').hidden = true; boot() })
    return
  }
  $('#pair').hidden = true
  $('#app').hidden = false
  startBoard()
}

function focusComposer () {
  if (isMobile()) set({ view: 'thread' })
  $('#composer-input').focus()
}

async function unpair () {
  openModal({
    title: 'Unpair this browser',
    testid: 'unpair-dialog',
    body: h('p', null, 'This browser forgets its device token. Pairing again needs a new code from the terminal running gummi web.'),
    actions: [
      { label: 'Cancel' },
      {
        label: 'Unpair',
        danger: true,
        primary: true,
        testid: 'unpair-confirm',
        onClick: async () => {
          try { await post('/api/unpair', {}) } catch (err) { toast(err.message, { err: true }); return false }
          closeEvents()
          location.hash = ''
          location.reload()
        }
      }
    ]
  })
}

async function startBoard () {
  if (started) return
  started = true
  const ctx = {
    select: (id, o) => select(id, o),
    setTab,
    togglePanel,
    jumpToStage,
    refresh,
    clearComposer: () => {}
  }
  setViewContext(() => ({
    api,
    select: ctx.select,
    toast,
    state,
    onStore: on,
    onEvent,
    openModal,
    refreshBoard: loadBoard
  }))
  initTop({ nextNeeding: () => nextNeeding(toast), palette: () => palette(ctx.select), keysHelp, toggleRail })
  initRail({ select: ctx.select, unpair })
  initHead(ctx)
  initThread()
  initDecision(ctx)
  initComposer(ctx)
  initPanel(ctx)
  initMobile()
  initSelection()
  initResume({ loadBoard, select: ctx.select })
  registerWorker()
  initKeys({
    palette: () => palette(ctx.select),
    keysHelp,
    step: (d) => step(d, visibleIds()),
    nextNeeding: () => nextNeeding(toast),
    setTab,
    toggleRail,
    togglePanel,
    focusComposer
  })
  setUnauthorizedHandler(() => { closeEvents(); closeOverlay(); location.reload() })

  let routed = false
  onRoute(({ id, tab }) => {
    routed = true
    if (id && (id !== state.sel || (tab && tab !== state.tab))) select(id, { tab })
  }, () => ({ id: state.sel, tab: state.tab }))
  await loadBoard()
  if (routed && state.sel) return connectEvents()
  const route = parse()
  if (route.tab) set({ tab: route.tab })
  const first = (route.id && rows().some(r => r.id === route.id) && route.id) ||
    rows().find(r => r.status === 'needs')?.id || rows()[0]?.id
  if (route.id && first !== route.id) toast(`${route.id} is not on this board`)
  if (first) await select(first, { tab: route.id === first ? route.tab : null, view: false })

  connectEvents()
}

function connectEvents () {
  connect({
    board: () => loadBoard(),
    card: (c) => { if (c.id === state.sel) refresh(c.id) },
    live: (c) => { if (c.id === state.sel) loadLive(c.id) },
    toast: (c) => toast(c.text, { err: c.err }),
    viewers: (c) => set({ viewers: c.viewers || [] }),
    resync: () => refreshAll()
  })
}

boot()

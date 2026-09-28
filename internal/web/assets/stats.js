// stats.js — the Stats tab: the card's run as cardrun folds it (the same
// fold as the TUI's run tab and `gummi status --stats`): what it cost, how
// much was rework, where the hours went, and every pass.

import { h, cr, dur, stageVar } from './dom.js?v=__ASSET_V__'
import { get, cardPath } from './api.js?v=__ASSET_V__'

export const statsTab = {
  name: 'stats',
  label: 'Stats',
  key: 'g r',
  fetch: (id) => get(cardPath(id, 'stats')),
  empty: (s) => !s || !s.sessions?.length,
  render
}

function render (pane, entry, ctx) {
  const s = entry.data
  const ps = s?.sessions || []
  if (!ps.length) {
    // a freeform card has turns, not stage passes: say so rather than
    // "nothing has run" beside a head that shows what it spent
    pane.append(ctx.card?.stage === 'open'
      ? h('div', { class: 'empty', testid: 'stats-none' }, h('b', null, 'No stage passes'), 'A freeform card works in turns, not stages. What it has spent is in its head.')
      : h('div', { class: 'empty', testid: 'stats-none' }, h('b', null, 'Nothing has run yet'), 'Passes and their credits appear here once a stage starts.'))
    return
  }
  const m = s.money || {}
  const c = s.clock || {}
  const max = Math.max(...ps.map(p => p.credits || 0), 0.1)
  const env = s.envelope?.credits
  pane.append(h('div', { class: 'sect', testid: 'stats' },
    h('div', { class: 'tiles' },
      tile(`${cr(m.credits)}`, 'cr', env ? `spent of ${env} envelope` : 'spent', 'stats-spent'),
      tile(String(ps.filter(p => p.credits > 0 || p.turns > 0).length), '', 'agent passes', 'stats-passes'),
      tile(cr(m.rework), 'cr', 'rework', 'stats-rework'),
      tile(dur(c.agentMs), '', 'agent time'),
      tile(dur(c.onYouMs), '', 'waiting on you')),
    h('div', { class: 'tablewrap', tabindex: '0', role: 'region', 'aria-label': 'Passes' }, h('table', { class: 'passes', testid: 'stats-table' },
      h('thead', null, h('tr', null, h('th', null, 'pass'), h('th', null, 'role'), h('th', null, 'model'), h('th', { class: 'num' }, 'credits'), h('th', { class: 'barc' }, h('span', { class: 'sr-only' }, 'share')), h('th', { class: 'num' }, 'time'))),
      h('tbody', null, ps.map(p => h('tr', { class: p.redo && 'rework', style: { '--sc': stageVar(p.stage) } },
        h('td', null, [p.stage, p.flavor && p.flavor !== 'work' ? p.flavor : null].filter(Boolean).join(' · ')),
        h('td', null, p.role || '—'),
        h('td', { class: 'mono' }, p.model || '—'),
        h('td', { class: 'num' }, p.credits ? cr(p.credits) : '—'),
        h('td', { class: 'barc' }, h('div', { class: 'b', style: { '--w': ((p.credits || 0) / max * 100) + '%' } })),
        h('td', { class: 'num' }, p.ended ? dur(new Date(p.ended) - new Date(p.started)) : 'running')))))),
    // spend no pass holds (a goal's lead, a one-shot), named so the rows
    // above plus this line come to the figure the card's head prints
    m.elsewhere > 0
      ? h('p', { class: 'foot-note' }, `${cr(m.elsewhere)} cr on turns that are not passes`,
        (m.elsewhereBy || []).length ? ' — ' + m.elsewhereBy.map(b => `${b.name} ${cr(b.credits)}`).join(', ') : '')
      : null,
    h('p', { class: 'foot-note' }, 'The same numbers as ', h('span', { class: 'mono' }, `gummi status --stats ${ctx.id}`), '.')))
}

function tile (v, unit, k, testid) {
  return h('div', { class: 'tile', testid }, h('div', { class: 'v' }, v, unit ? h('small', null, ' ' + unit) : null), h('div', { class: 'k' }, k))
}

// actions.js — a card's menu (Card.actions): the things the TUI offers on a
// card besides answering its decision. An action that needs input (a
// message, a number, a profile, a repository, a mode, cards, or a yes)
// collects it in a dialog first, prefilled with the action's default (the
// drafted landing message, the current budget, the dependencies already
// set); then POST /api/cards/{id}/actions/{action} runs it on the server,
// through the code the TUI's key runs.
//
// The server may still stop on a question the request did not answer (409
// "needs" or "confirm", with the question): the dialog asks it and sends
// again. A yes is never the page's to give on its own: an action that asks
// one (delete, clean, hand off …) is sent bare first, and the question the
// server answers with — its own words, line breaks and all — is what the
// dialog shows; the yes sent back is the token that question came with,
// which answers that question and nothing else. A refusal stays in the
// dialog, in the board's own words.

import { h, clear } from './dom.js?v=__ASSET_V__'
import { post, cardPath } from './api.js?v=__ASSET_V__'
import { openModal } from './views.js?v=__ASSET_V__'
import { toast } from './toast.js?v=__ASSET_V__'
import { state, set, rows } from './store.js?v=__ASSET_V__'

const NOUN = { message: 'Message', number: 'Credits', profile: 'Profile', repo: 'Repository', mode: 'Mode', cards: 'Waits for', text: 'Value' }

function cap (s) { s = String(s || ''); return s ? s[0].toUpperCase() + s.slice(1) : s }

export async function runAction (card, a) {
  if (!a.needs || a.needs === 'confirm') {
    try {
      await send(card.id, a, {})
    } catch (err) {
      // an action with no input can still stop on a question: ask it
      if (err.status === 409 && (err.data?.error === 'needs' || err.data?.error === 'confirm')) {
        dialog(card, a, { ask: err.data })
      } else {
        toast(err.message, { err: true })
      }
    }
    return
  }
  dialog(card, a)
}

// dialog collects an action's input. ask is a 409 the server answered a
// first try with: the question it stopped on, and what it needs.
function dialog (card, a, { ask = null } = {}) {
  const fields = new Map() // need -> { el, value() }
  const body = h('div', { class: 'aform' })
  const question = h('p', { class: 'aq', testid: 'action-question' }, a.detail ? cap(a.detail) + '.' : `${cap(a.label)} on ${card.id}.`)
  const error = h('p', { class: 'aerr', testid: 'action-error', role: 'alert', hidden: true })
  // the tokens of the questions the server asked and this dialog showed:
  // clicking yes under one sends it back, with any asked before it
  const confirms = []
  let asked = ''
  body.append(question)
  const addField = (need) => {
    if (!need || need === 'confirm' || fields.has(need)) return
    const f = fieldFor(card, a, need)
    fields.set(need, f)
    body.append(f.el)
  }
  addField(a.needs)
  body.append(error)
  const takeAsk = (e) => {
    if (e.error === 'confirm' || e.needs === 'confirm') {
      // the server's question, verbatim; its token is the only yes
      asked = e.confirm || ''
      question.classList.add('asked')
      clear(question).append(sentence(e.text) || `${cap(a.label)} ${card.id}?`)
      go.textContent = `Yes, ${a.label}`
      go.classList.add('danger')
    } else {
      addField(e.needs)
      clear(question).append(sentence(e.text) || question.textContent)
      // a landing stopped to have its drafted message read: it is the
      // field's value now, to read and edit before it lands
      if (e.draft != null && e.needs === 'message') {
        const f = fields.get('message')
        const ta = f?.el.querySelector('textarea')
        if (ta && !ta.value.trim() && e.draft.trim()) {
          ta.value = e.draft
          f.drafted?.()
          ta.focus(); ta.setSelectionRange(0, 0); ta.scrollTop = 0
        }
      }
    }
  }
  const m = openModal({
    title: `${cap(a.label)} · ${card.id}`,
    testid: 'action-dialog',
    // the head redraws under a dialog: focus goes back to its menu button
    returnTo: '[data-testid="card-actions"]',
    body,
    actions: [
      { label: 'Cancel', testid: 'action-cancel' },
      {
        label: cap(a.label),
        primary: true,
        danger: a.danger,
        testid: 'action-confirm',
        onClick: async () => {
          const req = {}
          for (const [need, f] of fields) {
            const v = f.value()
            if (v === undefined) { f.focus?.(); return false }
            Object.assign(req, v)
            void need
          }
          if (asked) { confirms.push(asked); asked = '' }
          if (confirms.length) req.confirm = confirms.join(' ')
          error.hidden = true
          go.disabled = true
          try {
            await send(card.id, a, req)
            return true
          } catch (err) {
            const e = err.data || {}
            if (err.status === 409 && (e.error === 'needs' || e.error === 'confirm')) {
              takeAsk(e)
            } else {
              clear(error).append(sentence(err.message))
              error.hidden = false
            }
            return false
          } finally {
            go.disabled = false
          }
        }
      }
    ]
  })
  const go = m.el.querySelector('[data-testid="action-confirm"]')
  if (ask) takeAsk(ask)
  // the message box opens with the cursor at the start: the default is to
  // be read before it is sent
  const ta = m.el.querySelector('textarea')
  if (ta) { ta.focus(); ta.setSelectionRange(0, 0); ta.scrollTop = 0 }
}

function fieldFor (card, a, need) {
  const label = h('span', { class: 'fl' }, NOUN[need] || 'Value')
  const def = a.needs === need ? (a.default || '') : ''
  if (need === 'message') {
    const landing = /^(merge|squash)$/.test(a.id)
    const ta = h('textarea', { id: 'action-input', testid: 'action-input', value: def, rows: landing ? 8 : 4, spellcheck: 'true' })
    const hint = landing ? h('span', { class: 'fh', testid: 'action-hint' }, messageHint(a, def ? 'default' : 'none')) : null
    return {
      el: h('label', { class: 'field' }, label, ta, hint),
      value: () => ({ message: ta.value.trim() }),
      focus: () => ta.focus(),
      // the draft the server stopped to have read is in the box now: the
      // hint says so rather than that nothing was drafted
      drafted: () => { if (hint) clear(hint).append(messageHint(a, 'drafted')) }
    }
  }
  if (need === 'number') {
    const inp = h('input', { id: 'action-input', testid: 'action-input', type: 'number', min: '0', inputmode: 'numeric', value: def })
    return {
      el: h('label', { class: 'field' }, label, inp, a.id === 'envelope' ? h('span', { class: 'fh' }, '0 means uncapped.') : null),
      value: () => inp.value === '' ? undefined : { number: Number(inp.value) },
      focus: () => inp.focus()
    }
  }
  if (need === 'profile' || need === 'repo' || need === 'mode') {
    let choices = a.choices || []
    if (need === 'mode' && !choices.length) choices = [{ value: 'autopilot', label: 'autopilot', detail: 'gates cross unattended' }, { value: 'attended', label: 'attended', detail: 'a person crosses every gate' }]
    const cur = def || (need === 'profile' ? card.profile : need === 'repo' ? card.repo : '')
    const sel = h('select', { id: 'action-input', testid: 'action-input' },
      choices.map(c => h('option', { value: c.value, selected: c.value === cur }, c.detail ? `${c.label} — ${c.detail}` : c.label)))
    const key = need
    return { el: h('label', { class: 'field' }, label, sel), value: () => ({ [key]: sel.value }), focus: () => sel.focus() }
  }
  if (need === 'cards') return cardsField(card, a, label, def)
  if (a.id === 'prlink') clear(label).append('Pull request')
  const inp = h('input', {
    id: 'action-input', testid: 'action-input', value: def, autocomplete: 'off', spellcheck: 'false',
    placeholder: a.id === 'prlink' ? 'https://github.com/owner/repo/pull/7, or 7' : null
  })
  return { el: h('label', { class: 'field' }, label, inp), value: () => ({ message: inp.value.trim() }), focus: () => inp.focus() }
}

// messageHint is the line under a landing's or a squash's message: where
// the message in the box came from (the verify gate's draft, one drafted
// just now, or none yet) and what it becomes. A squash collapses the
// branch where it is — nothing lands — so it never says "lands".
function messageHint (a, from) {
  const squash = a.id === 'squash'
  const becomes = squash ? 'this is the one commit the branch becomes' : 'this is what lands'
  if (from === 'none') return 'Nothing was drafted yet: leave it empty and gummi drafts one for you to read first (this can take a minute), or write it.'
  const where = from === 'drafted' ? 'Drafted by gummi just now.' : squash ? 'Drafted for this branch.' : 'Drafted when verify passed.'
  return `${where} Read it, edit it if you like — ${becomes}.`
}

// cardsField is a multi-select of the board's other cards, the ones set
// already ticked. Clearing every tick clears the dependencies.
function cardsField (card, a, label, def) {
  const set0 = new Set(String(def).split(',').map(s => s.trim()).filter(Boolean))
  const cands = rows().filter(r => r.id !== card.id && (r.status !== 'done' || set0.has(r.id)))
  const filter = h('input', { class: 'cfilter', testid: 'action-cards-filter', placeholder: 'Filter cards', 'aria-label': 'Filter cards', autocomplete: 'off' })
  const boxes = cands.map(r => {
    const cb = h('input', { type: 'checkbox', value: r.id, checked: set0.has(r.id), testid: `action-card-${r.id}` })
    return { r, cb, el: h('label', { class: 'cpick' }, cb, h('span', { class: 'id' }, r.id), h('span', { class: 't' }, r.title), h('span', { class: 's' }, r.stage)) }
  })
  const list = h('div', { class: 'cpicks', id: 'action-input', testid: 'action-input', role: 'group', 'aria-label': 'Cards' }, boxes.map(b => b.el))
  filter.addEventListener('input', () => {
    const q = filter.value.toLowerCase().trim()
    for (const b of boxes) b.el.hidden = !!q && !`${b.r.id} ${b.r.title}`.toLowerCase().includes(q)
  })
  return {
    el: h('div', { class: 'field' }, label, boxes.length > 6 ? filter : null, boxes.length ? list : h('span', { class: 'fh' }, 'No other card to wait for.')),
    value: () => ({ cards: boxes.filter(b => b.cb.checked).map(b => b.r.id) }),
    focus: () => boxes[0]?.cb.focus()
  }
}

// send runs the action. It throws the ApiError for the caller to place.
async function send (id, a, body) {
  // every action is sent against the stop the page shows, so one meant
  // for it is refused with "moved" rather than run at another; the server
  // refuses one that carries none while a decision is pinned
  const d = state.sel === id ? state.card?.decision : null
  if (d?.against?.token && !body.against) body = { ...body, against: d.against.token }
  const res = await post(cardPath(id, `actions/${encodeURIComponent(a.id)}`), body)
  // what the action changed is read again, documents included
  if (res && res.id === state.sel) set({ card: res, cardRev: (state.cardRev || 0) + 1 })
  if (res?.ok && !res.id) toast(`${cap(a.label)}: ${id} is gone`)
  else toast(`${cap(a.label)} · ${id}`)
  return res
}

function sentence (s) {
  s = String(s || '').trim()
  return s ? s[0].toUpperCase() + s.slice(1) : ''
}

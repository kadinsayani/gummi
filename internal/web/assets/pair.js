// pair.js — the pairing form a browser without a device cookie sees: the
// six-digit code gummi printed in its terminal, and the name of the person
// pairing (devices paired under one name are one person; a code printed by
// `gummi web pair --name` carries its person, and the server says so if a
// name is missing). A wrong code says how many tries it has left; a new
// code can be asked for, and is printed in the terminal, never sent to the
// page — while one is still live, asking only says so.

import { $, h, clear } from './dom.js?v=__ASSET_V__'
import { post } from './api.js?v=__ASSET_V__'

export function showPair (session, onPaired) {
  const root = $('#pair')
  clear(root)
  const name = h('input', { id: 'pair-name', testid: 'pair-name', name: 'name', maxlength: '40', autocomplete: 'nickname' })
  const code = h('input', { id: 'pair-code', testid: 'pair-code', class: 'code', name: 'code', inputmode: 'numeric', autocomplete: 'one-time-code', pattern: '[0-9]*', maxlength: '6', placeholder: '······', spellcheck: 'false', required: true })
  const msg = h('p', { class: 'msg-err', testid: 'pair-error', role: 'alert' })
  const note = h('p', { class: 'msg-ok', testid: 'pair-note', role: 'status' })
  const btn = h('button', { class: 'btn pri', type: 'submit', testid: 'pair-submit' }, 'Pair')
  const form = h('form', { autocomplete: 'off', novalidate: true, testid: 'pair-form' },
    h('label', { class: 'field', for: 'pair-name' }, 'Your name', name),
    h('label', { class: 'field', for: 'pair-code' }, 'Pairing code', code),
    btn, msg)
  form.addEventListener('submit', async (e) => {
    e.preventDefault()
    msg.textContent = ''
    note.textContent = ''
    const c = code.value.replace(/\D/g, '')
    if (c.length !== 6) { msg.textContent = 'The code is six digits.'; code.focus(); return }
    btn.disabled = true
    try {
      await post('/api/pair', { code: c, name: name.value.trim() })
      onPaired()
    } catch (err) {
      const left = err.data?.remaining
      msg.textContent = err.message + (left != null ? ` ${left === 1 ? '1 try' : left + ' tries'} left on this code.` : '')
      code.select()
    } finally {
      btn.disabled = false
    }
  })
  const again = h('button', {
    class: 'link', type: 'button', testid: 'pair-new',
    onclick: async () => {
      msg.textContent = ''
      try {
        const r = await post('/api/pair/request', {})
        note.textContent = r?.live
          ? 'A code is already showing in the terminal running gummi web. Use that one; a new one can be printed once it is used or expires.'
          : 'A new code is in the terminal running gummi web.'
      } catch (err) {
        msg.textContent = err.message
      }
    }
  }, 'Print a new code in the terminal')
  root.append(h('section', { class: 'pair-card', 'aria-labelledby': 'pair-title' },
    h('div', { class: 'brand' }, h('i', { 'aria-hidden': 'true' }), 'gummi'),
    h('h1', { id: 'pair-title' }, 'Pair this browser'),
    h('p', null, session.pairingLive
      ? ['gummi printed a six-digit code in the terminal running ', h('code', { class: 'mono' }, 'gummi web'), '. It is good for a few minutes and dies after three wrong guesses.']
      : ['Run ', h('code', { class: 'mono' }, 'gummi web pair'), ' on the machine hosting the board, or ask for a code below; it is printed in that terminal.']),
    form, note, again))
  root.hidden = false
  name.focus()
}

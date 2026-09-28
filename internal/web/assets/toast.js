// toast.js — short notices at the bottom of the screen: the server's own
// (the TUI's status band, over SSE) and the page's answers to what a person
// just did. A failure stays a little longer and reads in the error colour.
//
// A notice of several lines keeps its lines (the TUI shows those in its
// band, not as a one-row pill). One that hands the reader a command to run
// — an automatic stack replay's `git push` lines — stays a minute rather
// than 2.6 seconds, since nobody copies a command that fast, and while the
// pointer or focus is on it. It shows its sentence with a Copy for the
// commands and folds the commands themselves behind Show, so it never
// stands over the page it is about (the stacks view prints the same lines
// in full).

import { h } from './dom.js?v=__ASSET_V__'

const MAX = 3

// commandLines are the lines of a notice that are a command to run: the
// indented `git …` lines the TUI's notices put under their sentence.
function commandLines (text) {
  return text.split('\n').map(l => l.trim()).filter(l => /^git\s/.test(l))
}

export function toast (text, opts = {}) {
  const box = document.getElementById('toasts')
  if (!box || !text) return
  // the page's own answer and the server's broadcast of the same outcome
  // often say the same sentence: show it once
  if ([...box.children].some(el => el.dataset.text === text)) return
  const multi = text.includes('\n')
  const cmds = multi ? commandLines(text) : []
  const sticky = cmds.length > 0
  const head = sticky ? text.split('\n').filter(l => !/^git\s/.test(l.trim())).join('\n').trim() : text
  const detail = sticky ? h('span', { class: 'toast-cmds', testid: 'toast-cmds', hidden: true }, cmds.join('\n')) : null
  const el = h('div', { class: ['toast', opts.err && 'err', multi && 'multi', sticky && 'sticky'], testid: 'toast', role: opts.err ? 'alert' : null },
    sticky ? h('span', { class: 'toast-text' }, head, detail) : text)
  el.dataset.text = text
  if (sticky) {
    const show = h('button', {
      class: 'toast-btn', type: 'button', testid: 'toast-show', 'aria-expanded': 'false',
      onclick: () => { detail.hidden = !detail.hidden; show.setAttribute('aria-expanded', String(!detail.hidden)); show.textContent = detail.hidden ? 'Show' : 'Hide' }
    }, 'Show')
    const copy = h('button', {
      class: 'toast-btn', type: 'button', testid: 'toast-copy',
      onclick: () => (navigator.clipboard?.writeText(cmds.join('\n')) ?? Promise.reject(new Error('no clipboard')))
        .then(() => { copy.textContent = 'Copied' }, () => { copy.textContent = 'Select to copy' })
    }, cmds.length > 1 ? 'Copy all' : 'Copy')
    const close = h('button', { class: 'toast-btn', type: 'button', testid: 'toast-close', 'aria-label': 'Dismiss', onclick: () => el.remove() }, '×')
    el.append(h('span', { class: 'toast-acts' }, show, copy, close))
  }
  box.append(el)
  // an overflow evicts the oldest notice that will go on its own first;
  // one waiting for its command to be copied goes only when all are
  while (box.children.length > MAX) {
    const drop = [...box.children].find(c => !c.classList.contains('sticky')) || box.firstChild
    drop.remove()
  }
  if (!sticky) { setTimeout(() => el.remove(), opts.ms || (opts.err ? 5000 : multi ? 8000 : 2600)); return }
  // a notice holding commands goes after a minute, but never from under
  // a pointer or a focus that is on it
  const expire = () => {
    if (el.matches(':hover') || el.contains(document.activeElement)) { setTimeout(expire, 5000); return }
    el.remove()
  }
  setTimeout(expire, opts.ms || 60000)
}

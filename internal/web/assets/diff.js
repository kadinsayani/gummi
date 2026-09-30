// diff.js — the Diff tab: the card's branch against its base, file by file,
// with the review comments anchored in it. Clicking a line number opens a
// comment box (POST …/diff/annotations with the line's raw-diff index);
// comments can be resolved or deleted, and "Request changes" sends the open
// ones to the implementer (POST …/diff/changes) — asking first when that
// sends the card back. "Viewed" ticks are kept per card in
// this browser. When the branch moves while someone reads, the new diff is
// announced with a banner, never swapped in under the reader.

import { h, plural, storage } from './dom.js?v=__ASSET_V__'
import { get, post, del, cardPath } from './api.js?v=__ASSET_V__'
import { toast } from './toast.js?v=__ASSET_V__'
import { state } from './store.js?v=__ASSET_V__'
import { changesButton } from './actions.js?v=__ASSET_V__'

// "Since you last read it": the branch head this browser showed on the
// previous visit to a card's diff is the baseline, and the toggle asks the
// server to mark what changed after it (?since=). The baseline is taken
// once per card per page load, so reading the diff does not move it.
const baseline = new Map() // card id -> rev read on the previous visit
const sinceOn = new Set() // cards showing only what changed since

function noteRead (id, rev) {
  if (!rev) return
  if (!baseline.has(id)) {
    const prev = storage.get(`readRev:${id}`, null)
    baseline.set(id, prev && prev !== rev ? prev : null)
  }
  storage.set(`readRev:${id}`, rev)
}

export const diffTab = {
  name: 'diff',
  label: 'Diff',
  key: 'g d',
  fetch: (id) => {
    const since = sinceOn.has(id) ? baseline.get(id) : null
    return get(cardPath(id, 'diff') + (since ? `?since=${encodeURIComponent(since)}` : ''))
  },
  empty: (d) => !d || !d.files?.length,
  count: (d) => d?.files?.length || 0,
  // hold keeps the diff a person is reading when the branch moved: the
  // panel shows a banner and swaps only when asked
  hold: (old, fresh) => !!old && !!old.files?.length && !!fresh && old.rev !== fresh.rev,
  render
}

// A big diff is not drawn whole: every line is a row of spans, and 6000
// changed lines made ~37k nodes and two seconds of layout before a reader
// saw anything. A file longer than BIG_FILE lines, or any file of more than
// SMALL_FILE lines once BUDGET lines are already on screen, starts folded
// behind a "Show N lines" button. A file with an open comment is always
// drawn (the comment is why someone is here), and one a reader unfolded
// stays unfolded for the page's life.
const BIG_FILE = 400
const SMALL_FILE = 40
const BUDGET = 1500
const unfolded = new Map() // card id -> Set of paths a reader unfolded

function lineCount (f) { return (f.hunks || []).reduce((n, hk) => n + (hk.lines?.length || 0), 0) }

const viewedKey = (id) => `viewed:${id}`
function viewed (id) { return new Set(storage.get(viewedKey(id), [])) }
function setViewed (id, set) { storage.set(viewedKey(id), [...set]) }

function render (pane, entry, ctx) {
  const d = entry.data
  if (!d || !d.files?.length) {
    pane.append(h('div', { class: 'empty', testid: 'diff-none' }, h('b', null, 'No code yet'),
      d?.why || (ctx.card?.stage === 'plan' || ctx.card?.stage === 'todo'
        ? 'The plan stage only writes the spec. Code arrives with implement.'
        : 'This card has no changes against its base.')))
    return
  }
  noteRead(ctx.id, d.rev)
  const since = baseline.get(ctx.id)
  const onlySince = sinceOn.has(ctx.id) && !!d.since
  const files = onlySince ? d.files.filter(f => f.since) : d.files
  const seen = viewed(ctx.id)
  const anns = d.annotations || []
  const add = d.files.reduce((a, f) => a + (f.add || 0), 0)
  const rem = d.files.reduce((a, f) => a + (f.del || 0), 0)
  const noted = new Set(anns.filter(a => !a.resolved).map(a => a.file))

  pane.append(h('div', { class: 'diffhead', testid: 'diff-head' },
    h('span', null, 'against ', h('span', { class: 'mono' }, d.base || '—')),
    h('span', null, 'at ', h('span', { class: 'mono', testid: 'diff-rev' }, String(d.rev || '').slice(0, 7))),
    h('span', { class: 'add mono' }, `+${add}`), h('span', { class: 'del mono' }, `−${rem}`),
    since ? h('span', { class: 'seg', role: 'group', 'aria-label': 'Which changes', testid: 'diff-since' },
      h('button', { type: 'button', class: !sinceOn.has(ctx.id) && 'on', testid: 'diff-since-all', 'aria-pressed': String(!sinceOn.has(ctx.id)), onclick: () => { sinceOn.delete(ctx.id); ctx.swap(null) } }, 'All changes'),
      h('button', { type: 'button', class: sinceOn.has(ctx.id) && 'on', testid: 'diff-since-new', 'aria-pressed': String(sinceOn.has(ctx.id)), onclick: () => { sinceOn.add(ctx.id); ctx.swap(null) } }, `Since ${since.slice(0, 7)}`))
      : null))

  if (entry.fresh) {
    pane.append(h('div', { class: 'fresh', testid: 'diff-fresh', role: 'status' },
      h('span', { class: 'spinner' }),
      h('span', null, `The branch moved to ${String(entry.fresh.rev || '').slice(0, 7)} since you opened this diff.`),
      h('button', { class: 'btn', type: 'button', testid: 'diff-fresh-show', onclick: () => ctx.swap(entry.fresh) }, 'Show it')))
  }
  if (d.pendingComments) {
    // they go with an answer only when the open decision has one that
    // carries them (a send-back); otherwise they just wait on the diff
    const carried = state.sel === ctx.id && state.card?.decision?.options?.some(o => o.carriesComments)
    pane.append(h('div', { class: 'pending' }, h('span', { testid: 'diff-pending' }, carried
      ? `${plural(d.pendingComments, 'comment')} will go with your next answer.`
      : `${plural(d.pendingComments, 'comment')} on this diff ${d.pendingComments === 1 ? 'is' : 'are'} still open.`),
    changesButton(ctx.id, 'diff', [ctx.card?.stage, d.rev, anns.filter(a => !a.resolved).map(a => a.id).sort((a, b) => a - b)].join('|'),
      'Send the open comments to the implementer, or back to plan with a design note')))
  }

  if (onlySince && !files.length) pane.append(h('div', { class: 'empty' }, h('b', null, 'Nothing new'), `No file changed after ${since.slice(0, 7)}.`))
  pane.append(h('div', { class: 'files', testid: 'diff-files' }, files.map((f) => ({ f, i: d.files.indexOf(f) })).map(({ f, i }) => h('button', {
    class: seen.has(f.path) && 'viewed', type: 'button', testid: `diff-file-${i}`, title: f.path,
    onclick: () => {
      pane.querySelector(`[data-testid="diff-unfold-${i}"]`)?.click()
      pane.querySelector(`#f-${i}`)?.scrollIntoView({ block: 'start' })
    }
  },
  h('span', { class: 'p' }, noted.has(f.path) ? h('span', { class: 'dot', 'aria-label': 'has comments' }, '● ') : null, seen.has(f.path) ? '✓ ' : '', f.path,
    f.since && d.since ? h('span', { class: 'since' }, 'new') : null),
  h('span', null, h('span', { class: 'add' }, `+${f.add}`), ' ', h('span', { class: 'del' }, `−${f.del}`))))))

  if (!unfolded.has(ctx.id)) unfolded.set(ctx.id, new Set())
  const open = unfolded.get(ctx.id)
  let drawn = 0
  files.forEach((f) => {
    const i = d.files.indexOf(f)
    const isViewed = seen.has(f.path)
    const n = lineCount(f)
    // a viewed file's lines are hidden (app.css), so they are not drawn
    // either; unticking it redraws the tab
    const folded = !isViewed && !noted.has(f.path) && !open.has(f.path) &&
      (n > BIG_FILE || (n > SMALL_FILE && drawn + n > BUDGET))
    if (!isViewed && !folded) drawn += n
    const hunks = () => (f.hunks || []).map(hk => h('div', { class: 'hunk' },
      h('div', { class: 'hh' }, hk.header),
      (hk.lines || []).map(ln => lineEl(ln, anns, ctx))))
    const fold = folded
      ? h('button', {
        class: 'fold', type: 'button', testid: `diff-unfold-${i}`,
        onclick: (e) => { open.add(f.path); e.currentTarget.replaceWith(...hunks()) }
      }, `Show ${plural(n, 'line')}`, h('span', null, n > BIG_FILE ? ' · a large file, folded to keep the diff quick' : ' · folded to keep the diff quick'))
      : null
    const box = h('input', {
      type: 'checkbox', checked: isViewed, testid: `diff-viewed-${i}`,
      onchange: (e) => {
        const s = viewed(ctx.id)
        if (e.target.checked) s.add(f.path); else s.delete(f.path)
        setViewed(ctx.id, s)
        ctx.rerender()
      }
    })
    const orphans = anns.filter(a => a.file === f.path && a.idx < 0)
    pane.append(h('section', { class: ['file', isViewed && 'viewed'], id: `f-${i}`, testid: `diff-filebox-${i}`, 'aria-label': f.path },
      h('div', { class: 'fh' }, h('span', { class: 'p', title: f.oldPath ? `${f.oldPath} → ${f.path}` : f.path }, f.oldPath ? `${f.oldPath} → ${f.path}` : f.path),
        f.status && f.status !== 'modified' ? h('span', { class: 'fstatus' }, f.status) : null,
        h('span', { class: 'add' }, `+${f.add}`), h('span', { class: 'del' }, `−${f.del}`),
        h('label', null, box, 'Viewed')),
      f.binary ? h('div', { class: 'binary' }, 'Binary file; not shown.') : null,
      orphans.length ? h('div', { class: 'orphans', testid: 'diff-orphans' }, annotBox(orphans, ctx, true)) : null,
      isViewed ? null : fold || hunks()))
  })
  pane.append(h('div', { class: 'foot-space' }))
}

function lineEl (ln, anns, ctx) {
  const cls = ln.t === '+' ? 'a' : ln.t === '-' ? 'd' : ''
  const open = (e) => openComment(e.currentTarget.closest('.ln'), ln, ctx)
  const num = (v, c) => h('span', { class: c, role: 'button', tabindex: '0', 'aria-label': `Comment on line ${ln.new || ln.old || ''}`, onclick: open, onkeydown: (e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); open(e) } } }, v ? String(v) : '')
  const code = h('span')
  code.innerHTML = hl(ln.text) || ' ' // hl escapes everything it is given
  const row = h('div', { class: ['ln', cls, ln.since && 'since'], testid: `diff-line-${ln.idx}`, data: { idx: String(ln.idx) } },
    num(ln.old, 'o'), num(ln.new, 'n'), h('span', { class: 'm', 'aria-hidden': 'true' }, ln.t === ' ' ? '' : ln.t), code)
  const here = anns.filter(a => a.idx === ln.idx && a.idx >= 0)
  if (!here.length) return row
  const frag = document.createDocumentFragment()
  frag.append(row, annotBox(here, ctx, false))
  return frag
}

function annotBox (list, ctx, orphan) {
  return h('div', { class: 'annot' }, list.map(a => h('div', { class: ['a1', a.resolved && 'resolved'], testid: `annotation-${a.id}` },
    h('span', { class: 'who' }, h('b', null, a.by || (a.source === 'pr' ? 'reviewer' : 'Comment')), a.source === 'pr' ? h('span', { class: 'src' }, 'PR') : null,
      a.resolved ? h('span', null, 'resolved') : null,
      h('span', { class: 'acts' },
        h('button', { type: 'button', testid: `annotation-resolve-${a.id}`, onclick: () => act(ctx, a, a.resolved ? 'reopen' : 'resolve') }, a.resolved ? 'Reopen' : 'Resolve'),
        h('button', { type: 'button', testid: `annotation-delete-${a.id}`, onclick: () => act(ctx, a, 'delete') }, 'Delete'))),
    orphan && a.excerpt ? h('span', { class: 'ex', title: 'The line this was written against is gone' }, a.excerpt) : null,
    h('span', { class: 'tx' }, a.comment))))
}

async function act (ctx, a, what) {
  try {
    const path = cardPath(ctx.id, `diff/annotations/${a.id}`)
    const res = what === 'delete' ? await del(path) : await post(path + '/resolve', { resolved: what !== 'reopen' })
    toast({ delete: 'Comment deleted', resolve: 'Comment resolved', reopen: 'Comment reopened' }[what])
    ctx.swap(res && res.files ? res : null)
  } catch (err) {
    toast(err.notBuilt ? 'Changing comments from the web is not available yet' : err.message, { err: !err.notBuilt })
  }
}

function openComment (lnEl, ln, ctx) {
  const next = lnEl.nextElementSibling
  if (next?.classList.contains('draft')) { next.querySelector('textarea')?.focus(); return }
  const ta = h('textarea', { testid: 'annotation-input', 'aria-label': `Comment on line ${ln.new || ln.old}`, placeholder: 'Comment on this line. It goes with your next send-back to this card.' })
  const save = async (e) => {
    const comment = ta.value.trim()
    if (!comment) { ta.focus(); return }
    const btn = e.currentTarget
    btn.disabled = true
    try {
      const d = await post(cardPath(ctx.id, 'diff/annotations'), { idx: ln.idx, comment, text: ln.text })
      toast(ctx.card?.decision ? 'Comment added. It goes with your next answer.' : 'Comment added')
      ctx.swap(d && d.files ? d : null)
    } catch (err) {
      btn.disabled = false
      if (err.status === 409) { toast('The diff moved under that line; here it is as it stands now. Your comment is still in the box.'); ctx.swap(null); return }
      toast(err.notBuilt ? 'Diff comments from the web are not available yet' : err.message, { err: !err.notBuilt })
    }
  }
  const box = h('div', { class: 'annot draft', testid: 'annotation-draft' },
    h('div', { class: 'a1' }, h('span', { class: 'who' }, h('b', null, ctx.person || 'you'), ` on line ${ln.new || ln.old}`), ta),
    h('div', { class: 'act' },
      h('button', { class: 'btn', type: 'button', onclick: () => box.remove() }, 'Cancel'),
      h('button', { class: 'btn pri', type: 'button', testid: 'annotation-save', onclick: save }, 'Comment')))
  ta.addEventListener('keydown', (e) => {
    if (e.key === 'Escape') { e.stopPropagation(); box.remove() }
    if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) box.querySelector('[data-testid="annotation-save"]').click()
  })
  lnEl.after(box)
  ta.focus()
}

// hl escapes a line of code and tints keywords, strings, comments and calls.
// Only fixed <span class> tags are added after escaping, so no text from the
// diff can become markup.
const ESC = { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' }
function esc (s) { return String(s).replace(/[&<>"]/g, c => ESC[c]) }
export function hl (line) {
  line = String(line ?? '')
  if (/^\s*(\/\/|#(?!include)|--)/.test(line)) return `<span class="tk-c">${esc(line)}</span>`
  // split off strings first so nothing inside them is tinted
  const parts = line.split(/("(?:[^"\\]|\\.)*"|'(?:[^'\\]|\\.)*'|`[^`]*`)/)
  let out = ''
  for (let i = 0; i < parts.length; i++) {
    const p = parts[i]
    if (i % 2 === 1) { out += `<span class="tk-s">${esc(p)}</span>`; continue }
    const cm = p.indexOf('//')
    const code = cm >= 0 ? p.slice(0, cm) : p
    const rest = cm >= 0 ? p.slice(cm) : ''
    out += esc(code)
      .replace(/\b(func|return|if|else|for|range|var|const|let|import|package|export|async|await|continue|break|nil|null|true|false|type|struct|interface|def|class|switch|case|default|go|defer|select|chan|map|new|function|from)\b/g, '<span class="tk-k">$1</span>')
      .replace(/\b([A-Za-z_]\w*)(?=\()/g, '<span class="tk-f">$1</span>')
    if (rest) { out += `<span class="tk-c">${esc(rest + parts.slice(i + 1).join(''))}</span>`; break }
  }
  return out
}


// views/bugs.js — Import bugs (DESIGN §12.4): list a repository's GitHub
// issues through gh, tick the ones to take, and mint them into todo as
// `gummi bugs ingest` does. The server fetches again on import and mints
// what gh says, so a list gone stale answers with the refs it no longer
// offers (missing) instead of inventing cards.

import { h, clear, plural, storage } from '../dom.js?v=__ASSET_V__'
import { registerView } from '../views.js?v=__ASSET_V__'
import { field, choose, errorBox, cardLinks } from './kit.js?v=__ASSET_V__'

registerView('bugs', {
  title: 'Import bugs',
  css: 'views/bugs.css',
  mount (body, ctx) {
    const q = { repo: '', label: 'bug', state: 'open', limit: '30', ...storage.get('bugs-query', {}) }
    const v = { list: null, loading: false, err: null, picked: new Set(), result: null, importing: false, alive: true }
    body.classList.add('vbugs')

    const repo = h('input', { testid: 'bugs-repo', value: q.repo, placeholder: 'owner/repo (the checkout’s remote)', oninput: (e) => { q.repo = e.target.value.trim() } })
    const label = h('input', { testid: 'bugs-label', value: q.label, placeholder: 'any label', oninput: (e) => { q.label = e.target.value.trim() } })
    const state = choose(['open', 'closed', 'all'], { value: q.state, testid: 'bugs-state', onchange: (e) => { q.state = e.target.value } })
    const limit = h('input', { testid: 'bugs-limit', type: 'number', min: '1', max: '500', inputmode: 'numeric', value: q.limit, oninput: (e) => { q.limit = e.target.value } })
    const fetchBtn = h('button', { type: 'submit', class: 'btn', testid: 'bugs-fetch' }, 'List issues')
    const filters = h('form', { class: 'vrow', testid: 'bugs-filters', onsubmit: (e) => { e.preventDefault(); fetchList() } },
      field('Repository', repo), field('Label', label, { cls: 'narrow' }), field('State', state, { cls: 'narrow' }), field('Limit', limit, { cls: 'narrow' }), fetchBtn)
    const out = h('div', { class: 'vstack' })
    body.append(filters, out)

    function params () {
      const p = new URLSearchParams()
      if (q.repo) p.set('repo', q.repo)
      p.set('label', q.label)
      p.set('state', q.state)
      if (String(q.limit).trim()) p.set('limit', String(q.limit).trim())
      return p
    }

    async function fetchList ({ keepResult = false } = {}) {
      storage.set('bugs-query', q)
      v.loading = true
      v.err = null
      if (!keepResult) v.result = null
      fetchBtn.disabled = true
      draw()
      try {
        v.list = await ctx.api.get(`/api/bugs?${params()}`)
        v.picked = new Set([...v.picked].filter(r => v.list.proposals?.some(p => p.ref === r)))
      } catch (err) {
        v.list = null
        v.err = err
      }
      v.loading = false
      fetchBtn.disabled = false
      if (v.alive) draw()
    }

    function draw () {
      clear(out)
      if (v.err) out.append(errorBox(v.err))
      if (v.result) out.append(result(v.result))
      if (v.loading) {
        out.append(h('div', { class: 'vbusy', testid: 'bugs-loading' }, h('span', { class: 'spinner' }), 'asking gh for the issues…'))
        return
      }
      const l = v.list
      if (!l) return
      if (l.error) {
        out.append(h('div', { class: 'verr', testid: 'bugs-gh-error' }, h('b', null, 'gh could not list the issues'), '\n', l.error))
        return
      }
      const props = l.proposals || []
      const skipped = l.skipped || []
      if (!props.length && !skipped.length) {
        out.append(h('div', { class: 'empty', testid: 'bugs-empty' }, h('b', null, 'No issues match'), `${l.source || 'gh'} has none with these filters.`))
        return
      }
      const all = h('input', {
        type: 'checkbox',
        testid: 'bugs-all',
        'aria-label': 'Choose every issue',
        checked: props.length > 0 && v.picked.size === props.length,
        indeterminate: v.picked.size > 0 && v.picked.size < props.length,
        disabled: !props.length,
        onchange: (e) => { v.picked = e.target.checked ? new Set(props.map(p => p.ref)) : new Set(); draw() }
      })
      out.append(
        h('div', { class: 'vhead' }, all, h('b', null, props.length ? plural(props.length, 'issue') : 'No new issues'), h('span', { class: 'src' }, l.source || ''),
          skipped.length ? h('span', null, `${skipped.length} already on the board`) : null),
        h('ul', { class: 'issues', testid: 'bugs-list' },
          props.map(p => issue(p)),
          skipped.map(s => onBoard(s))))
      const n = v.picked.size
      out.append(h('div', { class: 'vfoot' },
        h('span', { class: 'vnote grow' }, n ? `${plural(n, 'issue')} chosen` : 'Choose the issues to import'),
        h('button', { type: 'button', class: 'btn pri', testid: 'bugs-import', disabled: !n || v.importing, onclick: importPicked }, n ? `Import ${plural(n, 'bug')}` : 'Import')))
    }

    function issue (p) {
      const on = v.picked.has(p.ref)
      const box = h('input', {
        type: 'checkbox',
        checked: on,
        testid: `bugs-pick-${p.number || p.ref}`,
        'aria-label': `Import #${p.number} ${p.title}`,
        onchange: (e) => { if (e.target.checked) v.picked.add(p.ref); else v.picked.delete(p.ref); draw() }
      })
      return h('li', { class: ['issue', on && 'on'], testid: `bugs-issue-${p.number || p.ref}` },
        h('label', { class: 'irow' },
          box,
          h('span', { class: 'num' }, p.number ? `#${p.number}` : ''),
          h('span', { class: 'im' },
            h('span', { class: 't' }, p.title),
            h('span', { class: 'meta' },
              p.author ? h('span', { class: 'by', testid: 'bug-author' }, `by @${p.author}`) : null,
              (p.labels || []).map(l => h('span', { class: 'lab' }, l)),
              p.severity && !(p.labels || []).includes(p.severity) ? h('span', { class: ['sev', `sev-${p.severity}`] }, `severity ${p.severity}`) : null,
              p.state && p.state.toLowerCase() !== 'open' ? h('span', { class: 'ist' }, p.state.toLowerCase()) : null,
              p.oneLiner && p.oneLiner !== p.title ? h('span', { class: 'ol' }, p.oneLiner) : null))))
    }

    function onBoard (s) {
      const num = (/#(\d+)$/.exec(s.ref) || /(\d+)$/.exec(s.ref) || [])[1]
      return h('li', { class: 'issue done', testid: `bugs-onboard-${num || s.ref}` },
        h('div', { class: 'irow' },
          h('span', { class: 'mark', 'aria-hidden': 'true' }, '✓'),
          h('span', { class: 'num' }, num ? `#${num}` : ''),
          h('span', { class: 'im' },
            h('span', { class: 't' }, s.title || s.ref),
            h('span', { class: 'meta' }, 'on the board as ',
              h('button', { type: 'button', class: 'link', onclick: () => { ctx.close(); ctx.select(s.card) } }, s.card)))))
    }

    async function importPicked () {
      if (!v.picked.size) return
      v.importing = true
      draw()
      const req = { refs: [...v.picked], label: q.label, state: q.state }
      if (q.repo) req.repo = q.repo
      const limit = parseInt(String(q.limit).trim(), 10)
      if (limit > 0) req.limit = limit
      try {
        v.result = await ctx.api.post('/api/bugs', req)
        v.err = null
        v.picked = new Set()
      } catch (err) {
        v.err = err
      }
      v.importing = false
      ctx.refreshBoard?.()
      if (v.err) return draw()
      // what is on the board now lists as such
      await fetchList({ keepResult: true })
    }

    function result (r) {
      const created = r.created || []
      return h('div', { class: 'vstack', testid: 'bugs-result' },
        created.length ? h('div', { class: 'vhead' }, h('b', null, `Created ${plural(created.length, 'bug')} in todo`)) : null,
        created.length ? cardLinks(created, ctx, 'bugs-created') : null,
        r.missing?.length
          ? h('div', { class: 'vnote warn', testid: 'bugs-missing' }, `Not offered any more (already on the board, closed, or filtered out): ${r.missing.join(', ')}`)
          : null)
    }

    fetchList()
    return () => { v.alive = false }
  }
})

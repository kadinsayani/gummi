// theme.js — light or dark. The system decides until a person picks one;
// the pick is kept per browser (theme-boot.js applies it before paint).

import { icon } from './dom.js?v=__ASSET_V__'

const KEY = 'gummi-web:theme'

export function isDark () {
  const t = document.documentElement.dataset.theme
  return t ? t === 'dark' : matchMedia('(prefers-color-scheme: dark)').matches
}

function paint () {
  const btn = document.getElementById('btn-theme')
  const dark = isDark()
  if (btn) {
    btn.replaceChildren(icon(dark ? 'sun' : 'moon'))
    btn.setAttribute('aria-label', dark ? 'Switch to the light theme' : 'Switch to the dark theme')
    btn.title = btn.getAttribute('aria-label')
    btn.dataset.theme = dark ? 'dark' : 'light'
  }
  const meta = document.getElementById('theme-color')
  if (meta) meta.setAttribute('content', dark ? '#121118' : '#EFEDF6')
}

export function toggle () {
  const next = isDark() ? 'light' : 'dark'
  document.documentElement.dataset.theme = next
  try { localStorage.setItem(KEY, next) } catch { /* not kept */ }
  paint()
}

export function initTheme () {
  paint()
  document.getElementById('btn-theme')?.addEventListener('click', toggle)
  matchMedia('(prefers-color-scheme: dark)').addEventListener?.('change', paint)
}

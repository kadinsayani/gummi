// viewport.js — the part of the screen the page may use. A phone's keyboard
// takes the bottom of the screen, and not every browser tells the layout:
// Chrome resizes the page (interactive-widget=resizes-content in the
// viewport meta), Safari only shrinks the visual viewport and scrolls it.
// So the app is sized from the visual viewport (--app-h, moved by --app-top),
// and while a field has the keyboard up the page is in its typing layout
// (html.kb): the nav and the top bar step aside, the card head and the pinned
// decision shrink, and the thread and the composer keep the room.

const KEYBOARD_MIN = 120 // a toolbar sliding away is tens of px; a keyboard a third of the screen

let tallest = 0
let width = 0

function editing () {
  const el = document.activeElement
  return !!el && (el.tagName === 'TEXTAREA' || (el.tagName === 'INPUT' && !/^(checkbox|radio|button|submit)$/.test(el.type)) || el.isContentEditable)
}

function sync () {
  const root = document.documentElement
  const vv = window.visualViewport
  const h = Math.round(vv ? vv.height : window.innerHeight)
  const top = Math.round(vv ? vv.offsetTop : 0)
  // the tallest the page has been at this width is the screen without a
  // keyboard; a rotation or a new window width starts the measure again
  if (window.innerWidth !== width) { width = window.innerWidth; tallest = 0 }
  tallest = Math.max(tallest, h, window.innerHeight)
  root.style.setProperty('--app-h', h + 'px')
  root.style.setProperty('--app-top', top + 'px')
  root.classList.toggle('kb', editing() && tallest - h > KEYBOARD_MIN)
}

let inited = false
export function initViewport () {
  if (inited) { sync(); return }
  inited = true
  const vv = window.visualViewport
  if (vv) {
    vv.addEventListener('resize', sync)
    vv.addEventListener('scroll', sync) // Safari scrolls it rather than resizing the page
  }
  window.addEventListener('resize', sync)
  // the keyboard's arrival and departure follow focus; measure once it has settled
  document.addEventListener('focusin', () => { sync(); setTimeout(sync, 250) })
  document.addEventListener('focusout', () => setTimeout(sync, 50))
  sync()
}

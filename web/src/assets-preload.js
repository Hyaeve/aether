// Start local logo requests before the authenticated card pages are mounted.
export function preloadLogos() {
  for (const href of ['/providers/115.ico', '/providers/mobile.png', '/providers/tianyi.png', '/providers/quark.png', '/providers/openlist.svg', '/providers/tmdb.svg', '/media/abs.png', '/media/emby.png', '/media/fnmovie.png']) {
    const link = document.createElement('link')
    link.rel = 'preload'; link.as = 'image'; link.href = href
    document.head.appendChild(link)
  }
}

const CACHE = 'aether-public-offline-v1'
const OFFLINE = '/pwa/offline.html'
const APP_PAGE = /^\/(?:$|dashboard$|storage(?:\/(?:shares|downloads|trash))?$|files(?:\/(?:webdav|mounts))?$|transfer(?:\/backup(?:\/settings)?)?$|tasks(?:\/(?:panel|strm|cas|ed2k|cache|scrape|automation|organize)(?:\/(?:pool|settings))?)?$|virtual\/(?:115|mobile|quark|cas)(?:\/settings)?$|links\/(?:manage|cache)$|tools(?:\/(?:market|115-rapid))?$|notifications(?:\/(?:telegram|transfers))?$|logs$|settings\/[a-z]+$)/

self.addEventListener('install', event => {
  event.waitUntil(caches.open(CACHE).then(cache => cache.add(new Request(OFFLINE, { cache: 'reload', credentials: 'omit' }))))
})
self.addEventListener('activate', event => {
  event.waitUntil(caches.keys().then(keys => Promise.all(keys.filter(key => key.startsWith('aether-public-offline-') && key !== CACHE).map(key => caches.delete(key)))).then(() => self.clients.claim()))
})
self.addEventListener('fetch', event => {
  const request = event.request, url = new URL(request.url)
  // Only public offline HTML is stored. Never cache authenticated pages, APIs,
  // provider responses, media, signed links, or mutations.
  if (request.method !== 'GET' || url.origin !== self.location.origin || request.mode !== 'navigate' || !APP_PAGE.test(url.pathname)) return
  event.respondWith(fetch(request).catch(async () => (await caches.match(OFFLINE)) || Response.error()))
})

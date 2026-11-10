// Service worker, hand-written. vite.pwa.mjs copies it to dist/app/sw.js, filling in the version
// (a hash of the precached files) and the precache URL list below.
//
// - Precaches the app shell (index.html as "/", JS, CSS, icons, latin fonts); other hashed files
//   under /assets/ are cached the first time they are used. All of it lives in one cache named
//   by the build version; activate deletes every older one.
// - Navigations are answered with the cached index.html, so the shell opens offline.
// - /api/ is never touched: always the network, and it fails fast when offline.
// - A new worker installs and waits. The page shows "Update available" and sends
//   {type: 'SKIP_WAITING'} when the person taps Reload; nothing swaps mid-session.
// - Android share target: POST /share-target puts the shared images in the capture queue's
//   IndexedDB store (same database and shape as src/lib/capture/store.ts) and redirects to /capture.

const VERSION = '__VERSION__'
const PRECACHE = /** @type {string[]} */ (__PRECACHE__)
const CACHE = 'shell-' + VERSION
const PREFIX = 'shell-'

self.addEventListener('install', (event) => {
  event.waitUntil(
    caches.open(CACHE).then((c) => c.addAll(PRECACHE.map((u) => new Request(u, { cache: 'reload' })))),
  )
})

self.addEventListener('activate', (event) => {
  event.waitUntil(
    caches
      .keys()
      .then((keys) => Promise.all(keys.filter((k) => k.startsWith(PREFIX) && k !== CACHE).map((k) => caches.delete(k))))
      .then(() => self.clients.claim()),
  )
})

self.addEventListener('message', (event) => {
  if (event.data && event.data.type === 'SKIP_WAITING') self.skipWaiting()
})

self.addEventListener('fetch', (event) => {
  const req = event.request
  const url = new URL(req.url)
  if (url.origin !== self.location.origin) return
  if (url.pathname.startsWith('/api/')) return // network only, no caching, no fallback

  if (req.method === 'POST' && url.pathname === '/share-target') {
    event.respondWith(handleShare(req))
    return
  }
  if (req.method !== 'GET') return

  if (req.mode === 'navigate') {
    event.respondWith(
      caches.open(CACHE).then(async (c) => (await c.match('/')) || fetch(req)),
    )
    return
  }
  if (url.pathname.startsWith('/assets/')) {
    // Hashed file names never change: cache first, fill on first use.
    event.respondWith(
      caches.open(CACHE).then(async (c) => {
        const hit = await c.match(req)
        if (hit) return hit
        const res = await fetch(req)
        if (res.ok) c.put(req, res.clone())
        return res
      }),
    )
    return
  }
  // Other precached files (icons, manifest): cache first, else network.
  event.respondWith(caches.open(CACHE).then(async (c) => (await c.match(req)) || fetch(req)))
})

// ---- share target ----

const ALPHABET = '0123456789ABCDEFGHJKMNPQRSTVWXYZ'
function ulid() {
  let ms = Date.now()
  let t = ''
  for (let i = 0; i < 10; i++) {
    t = ALPHABET[ms % 32] + t
    ms = Math.floor(ms / 32)
  }
  const r = crypto.getRandomValues(new Uint8Array(16))
  let out = t
  for (let i = 0; i < 16; i++) out += ALPHABET[r[i] & 31]
  return out
}

function openDb() {
  return new Promise((resolve, reject) => {
    const req = indexedDB.open('inventory-capture', 1)
    req.onupgradeneeded = () => req.result.createObjectStore('entries', { keyPath: 'id' })
    req.onsuccess = () => resolve(req.result)
    req.onerror = () => reject(req.error)
  })
}

async function handleShare(req) {
  try {
    const form = await req.formData()
    const files = form.getAll('photos').filter((f) => f instanceof File && f.type.startsWith('image/'))
    if (files.length) {
      const db = await openDb()
      await new Promise((resolve, reject) => {
        const tx = db.transaction('entries', 'readwrite')
        const os = tx.objectStore('entries')
        const all = os.getAll()
        all.onsuccess = () => {
          let seq = Math.max(-1, ...all.result.map((e) => e.seq)) + 1
          // Each shared photo becomes its own draft item, placed later in Capture.
          for (const blob of files) {
            os.put({ id: ulid(), groupId: ulid(), role: 'item', placeId: null, blob, state: 'waiting', attempts: 0, error: null, seq: seq++, nextAt: 0 })
          }
        }
        tx.oncomplete = () => resolve(undefined)
        tx.onerror = tx.onabort = () => reject(tx.error)
      })
      db.close()
    }
  } catch {
    // Fall through to Capture; the person can pick the photos there.
  }
  return Response.redirect('/capture', 303)
}

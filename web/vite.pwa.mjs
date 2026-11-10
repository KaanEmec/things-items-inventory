import { createHash } from 'node:crypto'
import { readdirSync, readFileSync, statSync, writeFileSync } from 'node:fs'
import { join, relative, sep } from 'node:path'

// PWA, written by hand (no Workbox / plugin). Three jobs:
//  1. manifest.webmanifest, named from APP_NAME in src/lib/app.ts (the one place the name lives),
//     with colours from files/design/tokens.json (chrome = theme, phoneBg = background);
//  2. replace __APP_NAME__ in index.html;
//  3. after the build, write sw.js from src/lib/pwa/sw.js with the precache list and a cache
//     version hashed from the content of those files.
export function pwa() {
  const appName = () => /APP_NAME\s*=\s*'([^']+)'/.exec(readFileSync('src/lib/app.ts', 'utf8'))[1]
  const manifest = () => {
    const name = appName()
    return {
      id: '/',
      name,
      short_name: name,
      description: 'Inventory of what you own and where it is.',
      start_url: '/',
      scope: '/',
      display: 'standalone',
      orientation: 'any',
      theme_color: '#edece7',
      background_color: '#f4f3ef',
      categories: ['productivity', 'lifestyle'],
      icons: [
        { src: '/icons/icon-192.png', sizes: '192x192', type: 'image/png', purpose: 'any' },
        { src: '/icons/icon-512.png', sizes: '512x512', type: 'image/png', purpose: 'any' },
        { src: '/icons/maskable-512.png', sizes: '512x512', type: 'image/png', purpose: 'maskable' },
        { src: '/icon.svg', sizes: 'any', type: 'image/svg+xml', purpose: 'any' },
      ],
      shortcuts: [
        { name: 'Capture', short_name: 'Capture', url: '/capture', icons: [{ src: '/icons/icon-192.png', sizes: '192x192', type: 'image/png' }] },
        { name: 'Quick add', short_name: 'Quick add', url: '/quick-add', icons: [{ src: '/icons/icon-192.png', sizes: '192x192', type: 'image/png' }] },
      ],
      // Android only: photos shared from the gallery. The service worker handles the POST.
      share_target: {
        action: '/share-target',
        method: 'POST',
        enctype: 'multipart/form-data',
        params: { files: [{ name: 'photos', accept: ['image/*'] }] },
      },
    }
  }
  const walk = (dir) =>
    readdirSync(dir).flatMap((f) => {
      const p = join(dir, f)
      return statSync(p).isDirectory() ? walk(p) : [p]
    })
  let outDir = 'dist/app'

  /** @type {import('vite').Plugin} */
  const plugin = {
    name: 'stash-pwa',
    configResolved(c) {
      outDir = c.build.outDir
    },
    transformIndexHtml: (html) => html.replaceAll('__APP_NAME__', appName()),
    configureServer(server) {
      server.middlewares.use('/manifest.webmanifest', (_req, res) => {
        res.setHeader('Content-Type', 'application/manifest+json')
        res.end(JSON.stringify(manifest()))
      })
    },
    closeBundle: {
      order: 'post',
      handler() {
        if (this.environment?.mode === 'dev') return
        let root
        try {
          root = outDir
          statSync(join(root, 'index.html'))
        } catch {
          return
        }
        writeFileSync(join(root, 'manifest.webmanifest'), JSON.stringify(manifest()))
        const files = walk(root)
          .map((p) => relative(root, p).split(sep).join('/'))
          .filter((f) => f !== 'sw.js' && !f.endsWith('.map'))
          // Latin fonts ship with the shell; other font subsets are cached when first used.
          .filter((f) => !/\.woff2?$/.test(f) || /-latin-(?!ext).*\.woff2$/.test(f))
          .sort()
        const hash = createHash('sha256')
        for (const f of files) hash.update(f).update(readFileSync(join(root, f)))
        const urls = files.map((f) => (f === 'index.html' ? '/' : '/' + f))
        const src = readFileSync('src/lib/pwa/sw.js', 'utf8')
          .replace('__VERSION__', hash.digest('hex').slice(0, 12))
          .replace('__PRECACHE__', JSON.stringify(urls))
        writeFileSync(join(root, 'sw.js'), src)
      },
    },
  }
  return plugin
}

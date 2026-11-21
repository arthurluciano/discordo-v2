import { createHash } from 'node:crypto'
import { readFile, writeFile } from 'node:fs/promises'
import { resolve } from 'node:path'
import { defineConfig, type Plugin } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

/** The Go server (`make run`). In production it serves this app itself, on the same origin. */
const API_ORIGIN = 'http://localhost:8080'

const SERVICE_WORKER_FILE = 'sw.js'
const BUILD_ID_PLACEHOLDER = '__BUILD_ID__'

/** Carimba no `sw.js` um hash dos arquivos gerados: qualquer mudanca no app muda o SW, e e isso
 * que faz o navegador instalar a versao nova e o app oferecer a atualizacao. */
const serviceWorkerBuildId = (): Plugin => {
  let outDir = ''
  return {
    name: 'service-worker-build-id',
    apply: 'build',
    configResolved: (config) => {
      outDir = resolve(config.root, config.build.outDir)
    },
    writeBundle: async (_options, bundle) => {
      const buildId = createHash('sha256').update(Object.keys(bundle).sort().join('\n')).digest('hex').slice(0, 12)
      const serviceWorkerPath = resolve(outDir, SERVICE_WORKER_FILE)
      const source = await readFile(serviceWorkerPath, 'utf8')
      await writeFile(serviceWorkerPath, source.replaceAll(BUILD_ID_PLACEHOLDER, buildId))
    },
  }
}

export default defineConfig({
  plugins: [react(), tailwindcss(), serviceWorkerBuildId()],
  resolve: {
    alias: {
      '@': new URL('./src', import.meta.url).pathname,
    },
  },
  server: {
    port: 5173,
    strictPort: true,
    // The API lives at the root, so only its real prefixes are forwarded; a new prefix must be added here.
    // The Host header is kept (no changeOrigin): the server's CSRF check compares it with Origin.
    proxy: Object.fromEntries([
      ...['/auth', '/users', '/guilds', '/channels', '/invites', '/attachments', '/images', '/relationships', '/blocks', '/dms', '/world', '/healthz']
        .map((prefix) => [prefix, API_ORIGIN]),
      ['/gateway', { target: API_ORIGIN, ws: true }],
    ]),
  },
})

import { contentRoot, generateManifest } from './content.mjs'
import path from 'node:path'

export function contentPlugin() {
  return {
    name: 'docs-content',
    generateBundle(_options, bundle) {
      for (const output of Object.values(bundle)) {
        if (output.type !== 'chunk') continue
        for (const id of Object.keys(output.modules)) {
          if (/node_modules[/](refractor|prismjs)[/]/.test(id)) throw new Error(`Build-time syntax compiler leaked into bundled module: ${id}`)
        }
      }
    },
    configureServer(server) {
      let timer
      let pending = Promise.resolve()
      const refresh = (file) => {
        if (!file.startsWith(contentRoot + path.sep) || !file.endsWith('.mdx')) return
        clearTimeout(timer)
        timer = setTimeout(() => {
          pending = pending.then(async () => {
            try {
              await generateManifest()
              server.ws.send({ type: 'full-reload' })
            } catch (error) {
              server.config.logger.error(error.message)
              server.ws.send({ type: 'error', err: { message: error.message, stack: error.stack } })
            }
          })
        }, 80)
      }
      server.watcher.add(contentRoot)
      for (const event of ['add', 'change', 'unlink']) server.watcher.on(event, refresh)
      server.httpServer?.once('close', () => {
        clearTimeout(timer)
        for (const event of ['add', 'change', 'unlink']) server.watcher.off(event, refresh)
      })
    },
  }
}

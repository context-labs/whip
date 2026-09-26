import { spawn } from 'node:child_process'
import { setTimeout } from 'node:timers/promises'

const worker = spawn('wrangler', ['dev', '--local', '--config', 'apps/docs/dist/deploy/wrangler.json', '--port', '3103', '--inspector-port', '0'], { stdio: 'inherit' })
try {
  let ready = false
  for (let attempt = 0; attempt < 60; attempt++) {
    if (worker.exitCode !== null) throw new Error('Local Worker exited before ready')
    try { ready = (await fetch('http://127.0.0.1:3103/whipcode/docs/quickstart')).ok } catch {}
    if (ready) break
    await setTimeout(500)
  }
  if (!ready) throw new Error('Local Worker did not become ready')
  const smoke = spawn(process.execPath, ['apps/docs/scripts/worker-smoke.mjs'], { stdio: 'inherit' })
  const code = await new Promise(resolve => smoke.once('exit', resolve))
  if (code !== 0) throw new Error(`Worker smoke exited ${code}`)
} finally {
  if (worker.exitCode === null) {
    worker.kill('SIGTERM')
    await new Promise(resolve => worker.once('exit', resolve))
  }
}

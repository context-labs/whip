import { build } from 'esbuild';
import { createServer } from 'node:http';
import { mkdir, readFile, copyFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import { join } from 'node:path';
const directory = fileURLToPath(new URL('.', import.meta.url));
await mkdir(join(directory, 'dist'), { recursive: true });
await build({ entryPoints: [join(directory, 'app.tsx')], outdir: join(directory, 'dist'), bundle: true, format: 'esm', platform: 'browser', target: 'es2022', minify: true, define: { 'process.env.NODE_ENV': '"production"' } });
for (const name of ['index.html', 'style.css']) await copyFile(join(directory, name), join(directory, 'dist', name));
export async function startExample(port = 3000) {
  const server = createServer(async (request, response) => {
    const path = new URL(request.url, 'http://localhost').pathname;
    const name = { '/': 'index.html', '/app.js': 'app.js', '/style.css': 'style.css' }[path];
    if (!name) { response.writeHead(404); response.end(); return; }
    response.setHeader('Content-Type', { 'index.html': 'text/html; charset=utf-8', 'app.js': 'text/javascript; charset=utf-8', 'style.css': 'text/css; charset=utf-8' }[name]);
    response.setHeader('Content-Security-Policy', "default-src 'self'; script-src 'self'; style-src 'self'; connect-src 'self' http: https: ws: wss:; object-src 'none'; base-uri 'none'");
    response.end(await readFile(join(directory, 'dist', name)));
  });
  await new Promise((resolve, reject) => { server.once('error', reject); server.listen(port, '127.0.0.1', resolve); });
  return { url: `http://127.0.0.1:${server.address().port}`, close: () => new Promise((resolve, reject) => server.close(error => error ? reject(error) : resolve())) };
}
if (process.argv[1] === fileURLToPath(import.meta.url) && !process.argv.includes('--build')) {
  console.log('WHIP client example:', (await startExample()).url, '(attach to an existing runtime)');
}

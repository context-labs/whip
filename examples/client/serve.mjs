import { build } from 'esbuild';
import { createServer } from 'node:http';
import { mkdir, readFile, copyFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import { join } from 'node:path';
const directory = fileURLToPath(new URL('.', import.meta.url));
await mkdir(join(directory, 'dist'), { recursive: true });
await build({ entryPoints: [join(directory, 'app.tsx')], outdir: join(directory, 'dist'), bundle: true, format: 'esm', platform: 'browser', target: 'es2022', minify: true, define: { 'process.env.NODE_ENV': '"production"' } });
for (const name of ['index.html', 'style.css']) await copyFile(join(directory, name), join(directory, 'dist', name));
if (!process.argv.includes('--build')) {
  createServer(async (request, response) => {
    const path = new URL(request.url, 'http://localhost').pathname;
    const name = { '/': 'index.html', '/app.js': 'app.js', '/style.css': 'style.css' }[path];
    if (!name) { response.writeHead(404); response.end(); return; }
    response.setHeader('Content-Type', { 'index.html': 'text/html; charset=utf-8', 'app.js': 'text/javascript; charset=utf-8', 'style.css': 'text/css; charset=utf-8' }[name]);
    response.setHeader('Content-Security-Policy', "default-src 'self'; script-src 'self'; style-src 'self'; connect-src 'self' http: https: ws: wss:; object-src 'none'; base-uri 'none'");
    response.end(await readFile(join(directory, 'dist', name)));
  }).listen(3000, '127.0.0.1', () => console.log('WHIP client example: http://localhost:3000 (attach to an existing daemon)'));
}

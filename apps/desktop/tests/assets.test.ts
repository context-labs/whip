import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { mkdir, mkdtemp, readFile, rename, rm, symlink, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import path from 'node:path';
import test, { type TestContext } from 'node:test';
import { createAssetHandler, desktopURL, isDesktopURL, type RendererManifest } from '../src/assets';

const html = '<html><script type="module" src="/assets/app-Abc12345.js"></script></html>';
const csp = "default-src 'none'; script-src 'self'; style-src 'self'; frame-ancestors 'none'";
const script = 'export const app = true;';
const hash = (bytes: string | Uint8Array) => createHash('sha256').update(bytes).digest('hex');

async function fixture(t: TestContext) {
  const root = await mkdtemp(path.join(tmpdir(), 'whip-desktop-assets-'));
  t.after(() => rm(root, { recursive: true, force: true }));
  const directory = path.join(root, 'renderer');
  await mkdir(path.join(directory, 'assets'), { recursive: true });
  const content: Record<string, string> = {
    'index.html': html, 'assets/app-Abc12345.js': script,
    'assets/app-Abc12345.css': 'body { color: black; }', 'assets/font-Abc12345.woff2': 'font bytes',
    'manifest.json': '{"name":"Whip"}',
  };
  const files: RendererManifest['files'] = {};
  for (const [name, bytes] of Object.entries(content).sort(([a], [b]) => a.localeCompare(b))) {
    await writeFile(path.join(directory, name), bytes);
    files[name] = { bytes: Buffer.byteLength(bytes), sha256: hash(bytes) };
  }
  const manifest: RendererManifest = { schema: 1, files, csp, digest: hash(JSON.stringify({ files, csp })) };
  const handler = createAssetHandler(directory, manifest);
  return { root, directory, manifest, handler,
    request: (name = '/', method = 'GET') => handler({ url: desktopURL + name, method }),
  };
}

test('accepts only the exact desktop origin, without credentials or alternate ports', async t => {
  const f = await fixture(t);
  assert.equal(isDesktopURL(desktopURL), true);
  for (const url of [
    'https://bundle/', 'file:///index.html', 'whip-app:bundle', 'null',
    'whip-app://bundle.example/', 'whip-app://bundle:80/', 'whip-app://BUNDLE/',
    'whip-app://user@bundle/', 'whip-app://user:secret@bundle/',
  ]) {
    assert.equal(isDesktopURL(url), false, url);
    assert.equal((await f.handler({ url, method: 'GET' })).status, 404, url);
  }
});

test('serves exact artifact bytes, asset MIME/cache headers and the shared CSP', async t => {
  const f = await fixture(t);
  for (const [name, type] of [
    ['/index.html', 'text/html; charset=utf-8'], ['/assets/app-Abc12345.js', 'text/javascript; charset=utf-8'],
    ['/assets/app-Abc12345.css', 'text/css; charset=utf-8'], ['/assets/font-Abc12345.woff2', 'font/woff2'],
    ['/manifest.json', 'application/json; charset=utf-8'],
  ]) {
    const response = await f.request(name!);
    assert.equal(response.status, 200);
    assert.equal(response.headers.get('Content-Type'), type);
    assert.equal(response.headers.get('Content-Security-Policy'), csp);
    assert.equal(response.headers.get('X-Content-Type-Options'), 'nosniff');
    assert.equal(response.headers.get('Referrer-Policy'), 'no-referrer');
    assert.equal(response.headers.get('Cache-Control'), name!.startsWith('/assets/') ? 'public, max-age=31536000, immutable' : 'no-cache');
    assert.deepEqual(Buffer.from(await response.arrayBuffer()), await readFile(path.join(f.directory, name!)));
  }
  const head = await f.request('/assets/app-Abc12345.js', 'HEAD');
  assert.equal(head.status, 200);
  assert.equal(head.headers.get('Content-Type'), 'text/javascript; charset=utf-8');
  assert.equal(await head.text(), '');
});

test('supports SPA deep navigation while missing assets and API paths return 404', async t => {
  const f = await fixture(t);
  for (const route of ['/', '/settings', '/h/runtime/s/root?agent=child']) {
    const response = await f.request(route);
    assert.equal(response.status, 200, route);
    assert.equal(await response.text(), html);
    assert.equal(response.headers.get('Cache-Control'), 'no-cache');
  }
  for (const route of ['/assets', '/assets/missing', '/assets/missing.js', '/missing.js', '/api', '/api/v3/ws', '/api/v3/content']) {
    const response = await f.request(route);
    assert.equal(response.status, 404, route);
    assert.equal(await response.text(), '');
  }
  await writeFile(path.join(f.directory, 'unlisted.js'), 'not in the artifact');
  assert.equal((await f.request('/unlisted.js')).status, 404);
});

test('rejects traversal, hidden files, invalid encodings and filesystem separators', async t => {
  const f = await fixture(t);
  for (const route of [
    '/../index.html', '/%2e%2e/index.html', '/assets/../index.html', '/assets/%2e%2e/index.html',
    '/%2e%2e%2findex.html', '/assets/%2e%2e%2findex.html', '/.git/config', '/%2egit/config',
    '/assets/.private', '/assets/%5cprivate', '/assets/%00private', '/assets/%0aprivate', '/assets/%zz',
  ]) assert.equal((await f.request(route)).status, 404, route);
});

test('allows only GET/HEAD and preserves security headers on errors', async t => {
  const f = await fixture(t);
  for (const method of ['POST', 'PUT', 'DELETE', 'OPTIONS']) {
    const response = await f.request('/', method);
    assert.equal(response.status, 405, method);
    assert.equal(response.headers.get('Allow'), 'GET, HEAD');
    assert.equal(response.headers.get('Content-Security-Policy'), csp);
    assert.equal(response.headers.get('X-Content-Type-Options'), 'nosniff');
    assert.equal(response.headers.get('Referrer-Policy'), 'no-referrer');
    assert.equal(await response.text(), '');
  }
});

test('refuses changed, missing and symlinked allowlisted files', async t => {
  const f = await fixture(t);
  const filename = path.join(f.directory, 'assets/app-Abc12345.js');
  await writeFile(filename, script.replace('true', 'null'));
  assert.equal((await f.request('/assets/app-Abc12345.js')).status, 503, 'same-sized tampering must fail the hash check');
  await rm(filename);
  assert.equal((await f.request('/assets/app-Abc12345.js')).status, 503);
  const outside = path.join(f.root, 'outside.js');
  await writeFile(outside, script);
  await symlink(outside, filename);
  assert.equal((await f.request('/assets/app-Abc12345.js')).status, 503);
});

test('refuses allowlisted assets reached through a symlinked parent directory', async t => {
  const f = await fixture(t);
  const outside = path.join(f.root, 'outside-assets');
  await rename(path.join(f.directory, 'assets'), outside);
  await symlink(outside, path.join(f.directory, 'assets'));
  assert.equal((await f.request('/assets/app-Abc12345.js')).status, 503);
});

test('refuses a symlink replacing the renderer directory itself', async t => {
  const f = await fixture(t);
  const outside = path.join(f.root, 'outside-renderer');
  await rename(f.directory, outside);
  await symlink(outside, f.directory);
  assert.equal((await f.request('/')).status, 503);
});

test('fails closed for invalid manifest metadata and oversized file claims', async t => {
  const f = await fixture(t);
  assert.throws(() => createAssetHandler(f.directory, { ...f.manifest, schema: 2 } as unknown as RendererManifest), /manifest/);
  assert.throws(() => createAssetHandler(f.directory, { ...f.manifest, files: {} }), /manifest/);
  assert.throws(() => createAssetHandler(f.directory, { ...f.manifest, csp: csp + '\r\nX-Evil: true' }), /manifest/);
  for (const bytes of [-1, 0.5, Number.MAX_SAFE_INTEGER, (32 << 20) + 1]) {
    f.manifest.files['assets/app-Abc12345.js']!.bytes = bytes;
    assert.equal((await f.request('/assets/app-Abc12345.js')).status, 503, String(bytes));
  }
  f.manifest.files['assets/app-Abc12345.js'] = { bytes: Buffer.byteLength(script), sha256: 'invalid' };
  assert.equal((await f.request('/assets/app-Abc12345.js')).status, 503);
});

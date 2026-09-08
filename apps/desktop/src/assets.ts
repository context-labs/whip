import { createHash } from 'node:crypto';
import { lstat, readFile } from 'node:fs/promises';
import path from 'node:path';

export const desktopScheme = 'whip-app';
export const desktopURL = 'whip-app://bundle';
export interface RendererManifest {
  schema: 1;
  csp: string;
  digest: string;
  files: Record<string, { bytes: number; sha256: string }>;
}
const mime: Record<string, string> = {
  '.html': 'text/html; charset=utf-8', '.js': 'text/javascript; charset=utf-8',
  '.css': 'text/css; charset=utf-8', '.json': 'application/json; charset=utf-8',
  '.woff2': 'font/woff2', '.woff': 'font/woff', '.svg': 'image/svg+xml',
  '.png': 'image/png', '.ico': 'image/x-icon', '.jpg': 'image/jpeg', '.webp': 'image/webp',
  '.wasm': 'application/wasm',
};

export function isDesktopURL(value: string): boolean {
  try {
    const url = new URL(value);
    return url.protocol === `${desktopScheme}:` && url.host === 'bundle' && !url.username && !url.password;
  } catch { return false; }
}

/** Only serve files listed by the verified build artifact; never expose the app root. */
export function createAssetHandler(directory: string, manifest: RendererManifest) {
  if (manifest.schema !== 1 || !manifest.files?.['index.html'] || !manifest.csp || /[\r\n]/.test(manifest.csp))
    throw new Error('The packaged renderer manifest is invalid');
  return async (request: Pick<Request, 'url' | 'method'>): Promise<Response> => {
    const headers = new Headers({ 'Content-Security-Policy': manifest.csp,
      'X-Content-Type-Options': 'nosniff', 'Referrer-Policy': 'no-referrer', 'Cache-Control': 'no-cache' });
    const error = (status: number) => new Response(null, { status, headers });
    if (!isDesktopURL(request.url)) return error(404);
    if (!['GET', 'HEAD'].includes(request.method)) { headers.set('Allow', 'GET, HEAD'); return error(405); }
    let pathname: string;
    try { pathname = decodeURIComponent(/^[^:]+:\/\/[^/?#]*([^?#]*)/.exec(request.url)?.[1] || '/'); }
    catch { return error(404); }
    const segments = pathname.split('/').slice(1);
    if (pathname.includes('\\') || /[\u0000-\u001f\u007f]/.test(pathname) || segments.some(part => part.startsWith('.')) || segments[0] === 'api')
      return error(404);
    let name = pathname.replace(/^\//, '') || 'index.html';
    if (!Object.hasOwn(manifest.files, name)) {
      if (name === 'assets' || name.startsWith('assets/') || path.posix.extname(name)) return error(404);
      name = 'index.html';
    }
    const entry = manifest.files[name]!;
    if (!Number.isSafeInteger(entry.bytes) || entry.bytes < 0 || entry.bytes > 32 << 20 || !/^[a-f0-9]{64}$/.test(entry.sha256)) return error(503);
    try {
      for (const parent of ['', ...name.split('/').slice(0, -1).map((_, index, parts) => parts.slice(0, index + 1).join('/'))]) {
        const stat = await lstat(path.join(directory, parent));
        if (!stat.isDirectory() || stat.isSymbolicLink()) return error(503);
      }
      const filename = path.join(directory, name);
      const stat = await lstat(filename);
      if (!stat.isFile() || stat.isSymbolicLink() || stat.size !== entry.bytes) return error(503);
      const bytes = await readFile(filename);
      if (createHash('sha256').update(bytes).digest('hex') !== entry.sha256) return error(503);
      headers.set('Content-Type', mime[path.extname(name)] ?? 'application/octet-stream');
      if (name.startsWith('assets/') && /-[A-Za-z0-9_-]{8,}\.[a-zA-Z0-9]+$/.test(name))
        headers.set('Cache-Control', 'public, max-age=31536000, immutable');
      return new Response(request.method === 'HEAD' ? null : Uint8Array.from(bytes), { headers });
    } catch { return error(503); }
  };
}

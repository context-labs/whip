/** Only a server origin is accepted. SDK owns the protocol socket/content paths. */
export function serverOrigin(input: string, development = false): string {
  const value = input.trim();
  if (!value || value.length > 2048) throw new Error('Enter your Whip server URL.');
  let url: URL;
  try { url = new URL(value.includes('://') ? value : `https://${value}`); }
  catch { throw new Error('Enter a valid server URL, such as https://whip.example.ts.net.'); }
  if (url.username || url.password || url.search || url.hash || url.pathname !== '/')
    throw new Error('Use only the server address, without a path, credentials, query, or fragment.');
  const local = url.hostname === 'localhost' || url.hostname === '127.0.0.1' || url.hostname === '[::1]';
  if (url.protocol !== 'https:' && !(development && local && url.protocol === 'http:'))
    throw new Error('Use the HTTPS address provided by Tailscale Serve.');
  return url.origin;
}

export function draftKey(runtimeId: string, rootId: string, agentId: string) {
  return JSON.stringify([runtimeId, rootId, agentId]);
}

/** Message links are user activated. Daemon output cannot invoke app/file URLs. */
export function externalLink(input: string): string | undefined {
  try {
    const url = new URL(input);
    if (['https:', 'http:', 'mailto:'].includes(url.protocol) && !url.username && !url.password)
      return url.href;
  } catch { /* Not an external URL. */ }
}

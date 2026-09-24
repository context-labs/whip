/** Saved/native browser addresses are bounded independently of the aggregate workspace record. */
export const MAX_BROWSER_URL_BYTES = 8192;
export const MAX_BROWSER_TABS = 8;
export function browserURL(value: string): string {
  if (/[\0-\x1f\x7f]/.test(value)) throw new Error('The address contains control characters.');
  const text = value.trim();
  const url = new URL(text);
  if (!['http:', 'https:'].includes(url.protocol) && text !== 'about:blank') throw new Error('Use an HTTP or HTTPS address.');
  if (url.username || url.password) throw new Error('Addresses with embedded credentials are not supported.');
  if (new TextEncoder().encode(url.href).byteLength > MAX_BROWSER_URL_BYTES) throw new Error('The address exceeds 8 KiB.');
  return url.href;
}
/** No search service or HTTPS downgrade: an explicit scheme is always retained. */
export function browserAddress(value: string): string {
  const text = value.trim();
  if (!text) return 'about:blank';
  const local = /^(?:localhost|127(?:\.\d{1,3}){3}|\[::1\])(?::\d+)?(?:[/?#]|$)/i.test(text);
  const hostPort = /^(?:[a-z0-9.-]+|\[[0-9a-f:]+\]):\d+(?:[/?#]|$)/i.test(text);
  if (local || hostPort) return browserURL(`http://${text}`);
  if (/^[a-z][a-z0-9+.-]*:/i.test(text)) return browserURL(text);
  if (/\s/.test(text) || !/^[^/?#]+\.[^/?#]+/.test(text)) throw new Error('Enter a web address; searches are not sent to a search provider.');
  return browserURL(`https://${text}`);
}
/** Preview addresses name only an exact remote loopback, never an inferred hostname. */
export function browserPreviewAddress(value: string): string {
  if (!/^https?:\/\/(?:127\.0\.0\.1|\[::1\])(?::[0-9]+)?(?:[/?#]|$)/i.test(value.trim())) {
    throw new Error('Use an HTTP or HTTPS URL with literal 127.0.0.1 or [::1].');
  }
  const url = browserURL(value);
  if (new URL(url).port === '0') throw new Error('The preview port must be between 1 and 65535.');
  return url;
}
export function browserTitle(value: string): string { return [...value.replace(/[\0\r\n]/g, ' ')].slice(0, 128).join(''); }

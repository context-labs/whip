/** Deep links identify saved runtimes, never connection credentials or commands. */
export function sessionLinkPath(value: string, scheme: 'whip' | 'whip-beta' = 'whip'): string {
  if (typeof value !== 'string' || value.length > 8192 || /[\u0000-\u0020\u007f\\]/.test(value)) throw new Error('Invalid Whip session link');
  const url = new URL(value);
  if (url.protocol !== `${scheme}:` || url.host !== 'session' || url.username || url.password || url.hash) throw new Error('Invalid Whip session link');
  const segments = url.pathname.split('/').slice(1);
  if (segments.length !== 2 || segments.some(segment => !segment || !/^[a-zA-Z0-9_-]{1,128}$/.test(decodeURIComponent(segment))))
    throw new Error('Invalid Whip session identity');
  return `/h/${segments[0]}/s/${segments[1]}${url.search}`;
}

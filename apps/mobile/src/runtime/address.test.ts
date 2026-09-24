/** @jest-environment node */
import { externalLink, serverOrigin } from './address';

test('server setup canonicalizes only a private endpoint origin', () => {
  expect(serverOrigin(' Whip.Example.ts.net:443 ')).toBe('https://whip.example.ts.net');
  expect(serverOrigin('https://whip.example.ts.net:8443/')).toBe('https://whip.example.ts.net:8443');
  for (const value of ['https://user:secret@whip.example.ts.net', 'https://whip.example.ts.net/path', 'https://whip.example.ts.net?token=x', 'https://whip.example.ts.net/#open', 'whip://session/root', 'javascript:alert(1)', 'file:///etc/hosts', 'http://whip.example.ts.net', 'x'.repeat(2049)]) {
    expect(() => serverOrigin(value)).toThrow();
  }
});
test('HTTP is confined to loopback in development', () => {
  for (const origin of ['http://localhost:9876', 'http://127.0.0.1:9876', 'http://[::1]:9876']) {
    expect(() => serverOrigin(origin)).toThrow();
    expect(serverOrigin(origin, true)).toBe(origin);
  }
  expect(() => serverOrigin('http://100.64.0.2:9876', true)).toThrow();
  expect(() => serverOrigin('http://localhost.evil.example', true)).toThrow();
});
test('transcript links cannot execute scripts or navigate the app', () => {
  for (const value of ['whip://server?url=https://other.example', 'exp://host', 'javascript:alert(1)', 'file:///private/data', 'data:text/html,hello', '//example.com', 'intent://host', 'https://user:secret@example.com']) expect(externalLink(value)).toBeUndefined();
  expect(externalLink('https://example.com/docs?q=react#native')).toBe('https://example.com/docs?q=react#native');
  expect(externalLink('mailto:person@example.com')).toBe('mailto:person@example.com');
});

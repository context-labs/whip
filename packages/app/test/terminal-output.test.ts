import { expect, it } from 'vitest';
import { providerFixture } from './provider-fixture';
import { readTerminalOutput } from '../src/terminal-output';

async function fixture() {
  const f = await providerFixture();
  const ref = { id: 'shell', process_epoch: 'boot' };
  const from = '9007199254740993', end = '9007199254740996';
  const page = { terminal: { ...ref, cwd: '/work', shell: '/bin/sh', cols: 80, rows: 24, closing: false, exited: false, exit_code: 0, signal: '', start: from, end, created_at: '2026-09-28T00:00:00Z' }, from, next: end, end, truncated: false, data_base64: btoa('abc') };
  f.data.handlers['terminal.read'] = () => page;
  return { ...f, ref, page, from };
}
it('preserves exact byte cursors beyond the safe integer range', async () => {
  const f = await fixture(), signal = new AbortController().signal;
  const result = await readTerminalOutput(f.client, f.ref, f.from, signal);
  expect(new TextDecoder().decode(result.bytes)).toBe('abc'); expect(result.reset).toBe(false);
  expect(result.page.next).toBe('9007199254740996');
  expect(f.calls.at(-1)?.params).toEqual({ ...f.ref, cursor: f.from, limit: 32768 });
});
it('resets only with explicit bounded replay truncation evidence', async () => {
  const f = await fixture(); f.page.truncated = true;
  expect((await readTerminalOutput(f.client, f.ref, '0', new AbortController().signal)).reset).toBe(true);
});
it.each(['identity', 'epoch', 'length', 'end', 'unmarked-gap'])('rejects inconsistent %s output evidence', async mode => {
  const f = await fixture();
  if (mode === 'identity') f.page.terminal.id = 'another';
  if (mode === 'epoch') f.page.terminal.process_epoch = 'another';
  if (mode === 'length') f.page.next = '9007199254740995';
  if (mode === 'end') f.page.terminal.end = '9007199254740997';
  await expect(readTerminalOutput(f.client, f.ref, mode === 'unmarked-gap' ? '0' : f.from, new AbortController().signal)).rejects.toThrow('Invalid terminal output evidence');
});
it('rejects a replaced process before requesting shell output', async () => {
  const f = await fixture(); await expect(readTerminalOutput(f.client, { ...f.ref, process_epoch: 'old' }, '0', new AbortController().signal)).rejects.toThrow('earlier host process');
  expect(f.count('terminal.read')).toBe(0);
});
it('drops an output response cancelled before it reaches the renderer', async () => {
  const f = await fixture(), controller = new AbortController();
  f.data.handlers['terminal.read'] = () => { controller.abort(); return f.page; };
  await expect(readTerminalOutput(f.client, f.ref, f.from, controller.signal)).rejects.toThrow();
});

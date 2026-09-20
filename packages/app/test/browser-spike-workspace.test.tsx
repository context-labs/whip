import { StrictMode, useLayoutEffect, useState } from 'react';
import { act, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { isSessionTab, SessionTabs, type SessionTab } from '../src/session-tabs';
import { browserDIPBounds, createOverlayHolds, parseBrowserDescriptor, presentationReceiver } from './browser-spike-contract';

const browser = { id: 'browser_1', kind: 'browser', url: 'https://example.com/', titleHint: 'Example' };
const rect = { x: 10.25, y: 68.5, width: 400.5, height: 300.25 };
const content = { width: 1200, height: 900 };

describe('Phase 0 browser workspace contract sketch (not native acceptance)', () => {
  it('preserves a browser descriptor without a platform, stripping every live authority field', () => {
    const value = parseBrowserDescriptor({ ...browser, environmentId: 'preview_dev', attachmentId: 'old',
      providerId: 'old', runtimeId: 'host', rootId: 'root', documentRevision: 7, agentId: 'agent',
      partition: 'persist:privileged', approvedPorts: [443] });
    expect(value).toEqual({ ...browser, environmentId: 'preview_dev' });
    expect(parseBrowserDescriptor(JSON.parse(JSON.stringify(value)))).toEqual(value);
    expect(isSessionTab(value as unknown as SessionTab)).toBe(false);
  });

  it('rejects unsafe/oversized descriptors without turning an unavailable preview into a local tab', () => {
    for (const url of ['javascript:alert(1)', 'file:///private', 'whip://app', 'https://u:p@example.com/',
      'https://example.com/\n', `https://example.com/${'x'.repeat(8192)}`, 'not a URL']) {
      expect(parseBrowserDescriptor({ ...browser, url })).toBeUndefined();
    }
    expect(parseBrowserDescriptor({ ...browser, environmentId: 'bad/id' })).toBeUndefined();
    expect(parseBrowserDescriptor({ ...browser, environmentId: 'missing_environment' })?.environmentId).toBe('missing_environment');
    expect(parseBrowserDescriptor({ ...browser, url: 'about:blank' })?.url).toBe('about:blank');
    expect(parseBrowserDescriptor({ ...browser, titleHint: '🌐'.repeat(200) })?.titleHint).toBe('🌐'.repeat(128));
    expect(parseBrowserDescriptor({ ...browser, url: `https://example.com/${'x'.repeat(5000)}` })).toBeDefined();
  });

  it('leaves current session/draft/terminal move semantics and the session lease guard intact', () => {
    const tabs = new SessionTabs();
    tabs.open('runtime', 'root');
    const draft = tabs.openNew({});
    const moved = tabs.split(draft.id, 'right');
    expect(moved).toBe(draft.id);
    expect(tabs.workspace().tabs.filter(tab => tab.id === draft.id)).toHaveLength(1);
    expect(tabs.workspace().tabs.filter(isSessionTab)).toHaveLength(1);
    tabs.dispose();
  });

  it('maps CSS viewport edges with renderer zoom, not devicePixelRatio', () => {
    expect(browserDIPBounds(rect, 1.25, content)).toEqual({ x: 13, y: 86, width: 500, height: 374 });
    expect(browserDIPBounds(rect, 1, content)).toEqual({ x: 11, y: 69, width: 399, height: 299 });
    // DIP input is independent of Retina scale; moving to another display does not multiply these coordinates.
    expect(browserDIPBounds({ x: -10, y: -20, width: 2000, height: 2000 }, 1, content)).toEqual({ x: 0, y: 0, ...content });
  });

  it('fails closed on empty, clipped-away and non-finite slot geometry', () => {
    for (const invalid of [{ ...rect, width: 0 }, { ...rect, height: -1 }, { ...rect, x: NaN },
      { ...rect, y: Infinity }, { ...rect, x: 1300 }, { x: -100, y: -100, width: 10, height: 10 }]) {
      expect(browserDIPBounds(invalid, 1, content)).toBeUndefined();
    }
    expect(browserDIPBounds(rect, 0, content)).toBeUndefined();
    expect(browserDIPBounds(rect, Infinity, content)).toBeUndefined();
  });

  it('uses a complete bounded slot snapshot, with stale revision/renderer epoch rejection', () => {
    const receiver = presentationReceiver('renderer_2');
    const apply = (revision: number, slots: string[], blocked = false, epoch = 'renderer_2') => receiver.apply({ epoch, revision, slots, blocked });
    expect(apply(1, ['a', 'b', 'c', 'd'])).toBe(true);
    expect(apply(2, ['a', 'b'], true)).toBe(true);
    expect(receiver.visible).toEqual([]);
    expect(apply(1, ['a'])).toBe(false);
    expect(apply(99, ['a'], false, 'renderer_1')).toBe(false);
    expect(apply(3, ['a', 'a'])).toBe(false);
    expect(apply(3, ['a', 'b', 'c', 'd', 'e'])).toBe(false);
    // Settings, a hidden document and route unmount all send the same empty complete presentation.
    expect(apply(4, [])).toBe(true);
    expect(receiver.visible).toEqual([]);
    expect(apply(5, ['b'])).toBe(true);
    expect(receiver.visible).toEqual(['b']);
  });

  it('holds nested menu/dialog/permission surfaces until the last unique owner exits', async () => {
    const present = vi.fn(async () => {});
    const holds = createOverlayHolds(present);
    const dialog = holds.acquire(), menu = holds.acquire(), permission = holds.acquire();
    await Promise.all([dialog.ready, menu.ready, permission.ready]);
    menu.release(); menu.release(); dialog.release();
    expect(holds.size).toBe(1);
    expect(present.mock.calls).toEqual([[true, 1]]);
    permission.release();
    expect(present.mock.calls).toEqual([[true, 1], [false, 2]]);
    expect(holds.size).toBe(0);
  });

  it('bounds leaked owners and retains no state proportional to completed overlay cycles', async () => {
    const holds = createOverlayHolds(async () => {});
    for (let i = 0; i < 2000; i++) { const hold = holds.acquire(); await hold.ready; hold.release(); }
    expect(holds.size).toBe(0);
    const owners = Array.from({ length: 128 }, () => holds.acquire());
    expect(() => holds.acquire()).toThrow('Too many overlays');
    owners.forEach(owner => owner.release());
    expect(holds.size).toBe(0);
  });
});

/** A UI fixture for the hide-before-interactive handshake; does not render an Electron view. */
function OverlayGate({ holds }: { holds: ReturnType<typeof createOverlayHolds> }) {
  const [ready, setReady] = useState(false);
  useLayoutEffect(() => {
    let alive = true;
    const hold = holds.acquire();
    void hold.ready.then(() => { if (alive) setReady(true); });
    return () => { alive = false; hold.release(); };
  }, [holds]);
  return ready ? <div role="dialog" aria-label="Permission fixture"><button>Allow once</button></div> : null;
}

it('does not expose approval controls before hide ACK, and balances StrictMode/unmount cleanup', async () => {
  const acknowledgments: (() => void)[] = [];
  const holds = createOverlayHolds((blocked) => blocked ? new Promise<void>(resolve => acknowledgments.push(resolve)) : Promise.resolve());
  const view = render(<StrictMode><OverlayGate holds={holds}/></StrictMode>);
  expect(screen.queryByRole('dialog')).toBeNull();
  expect(holds.size).toBe(1);
  expect(acknowledgments).toHaveLength(2);
  await act(async () => { acknowledgments[0]!(); });
  expect(screen.queryByRole('dialog')).toBeNull();
  await act(async () => { acknowledgments[1]!(); });
  expect(screen.getByRole('button', { name: 'Allow once' })).toBeTruthy();
  view.unmount();
  expect(holds.size).toBe(0);
});

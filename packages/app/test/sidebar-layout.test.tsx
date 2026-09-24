import { act, fireEvent, render, renderHook, screen, waitFor } from '@testing-library/react';
import { useRef, useState, type ReactNode } from 'react';
import { expect, it, vi } from 'vitest';
import { RuntimeContext } from '../src/context';
import type { AppRuntime } from '../src/runtime';
import { SidebarResize, useSidebarLayout } from '../src/sidebar-layout';
import { sidebarStorageKey } from '../src/sidebar-state';

it('retains window preferences and falls back visibly to memory on denied writes', async () => {
  const disk = new Map([[sidebarStorageKey, JSON.stringify({ version: 1, width: 400, hidden: true, hosts: [] })]]);
  let denied = false;
  const report = vi.fn();
  const runtime = { report, platform: { windowStorage: { getItem: (key: string) => disk.get(key), setItem: (key: string, value: string) => { if (denied) throw Error('denied'); disk.set(key, value); } } } } as unknown as AppRuntime;
  const wrapper = ({ children }: { children: ReactNode }) => <RuntimeContext.Provider value={runtime}>{children}</RuntimeContext.Provider>;
  const { result } = renderHook(useSidebarLayout, { wrapper });
  expect(result.current.state).toMatchObject({ width: 400, hidden: true });
  denied = true;
  act(() => result.current.setState(state => ({ ...state, hidden: false })));
  await waitFor(() => expect(report).toHaveBeenCalledTimes(1));
  act(() => result.current.setState(state => ({ ...state, width: 320 })));
  expect(result.current.state).toMatchObject({ hidden: false, width: 320 });
  expect(report).toHaveBeenCalledTimes(1);
});
it('resizes with keyboard and restores toggle focus on collapse', () => {
  function Harness() {
    const [width, setWidth] = useState(320), [hidden, setHidden] = useState(false);
    const ref = useRef<HTMLButtonElement>(null);
    return <><button ref={ref}>Navigation</button>{!hidden && <SidebarResize width={width} maxWidth={420} onResize={setWidth} onHide={() => setHidden(true)} toggleRef={ref} />}</>;
  }
  render(<Harness />);
  const separator = screen.getByRole('separator');
  fireEvent.keyDown(separator, { key: 'ArrowRight' }); expect(separator.getAttribute('aria-valuenow')).toBe('328');
  fireEvent.keyDown(separator, { key: 'End' }); expect(separator.getAttribute('aria-valuenow')).toBe('420');
  fireEvent.keyDown(separator, { key: 'ArrowRight' }); expect(separator.getAttribute('aria-valuenow')).toBe('420');
  fireEvent.keyDown(separator, { key: 'Home' }); expect(separator.getAttribute('aria-valuenow')).toBe('256');
  fireEvent.keyDown(separator, { key: 'Enter' }); expect(screen.queryByRole('separator')).toBeNull();
  expect(document.activeElement).toBe(screen.getByRole('button', { name: 'Navigation' }));
});
it('reports denied reads once and retains usable in-memory state', () => {
  const report = vi.fn(), setItem = vi.fn();
  const runtime = { report, platform: { windowStorage: { getItem: () => { throw Error('denied'); }, setItem } } } as unknown as AppRuntime;
  const { result } = renderHook(useSidebarLayout, { wrapper: ({ children }) => <RuntimeContext.Provider value={runtime}>{children}</RuntimeContext.Provider> });
  expect(result.current.state.width).toBe(320);
  expect(report).toHaveBeenCalledTimes(1); expect(setItem).not.toHaveBeenCalled();
  act(() => result.current.setState(state => ({ ...state, hidden: true })));
  expect(result.current.state.hidden).toBe(true); expect(report).toHaveBeenCalledTimes(1);
});

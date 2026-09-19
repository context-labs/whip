import { createRef } from 'react';
import { createPortal } from 'react-dom';
import { act, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { ChatDropSurface, ChatFileDrop, FileDropNavigationGuard } from '../src/chat-file-drop';

afterEach(() => vi.useRealTimers());

const image = () => new File(['image'], 'screenshot.png', { type: 'image/png' });
const transfer = (files: File[] = [image()]) => ({ types: ['Files'], files, items: [], dropEffect: 'none' });
function fixture() {
  const target = createRef<HTMLDivElement>();
  const onFiles = vi.fn(), onError = vi.fn();
  const app = (unavailable?: string, scope = 'host:root:child', handler = onFiles) => <>
    <FileDropNavigationGuard />
    <ChatDropSurface ref={target}>
      <p>Transcript text</p><form><textarea aria-label="Draft" /><button>Queued message</button>
        <ChatFileDrop target={target} scope={scope} unavailable={unavailable} onFiles={handler} onError={onError} />
      </form>
      {createPortal(<div role="dialog">Outside dialog</div>, document.body)}
    </ChatDropSurface>
    <aside>Sidebar</aside>
  </>;
  return { app, target, onFiles, onError };
}

it('accepts multiple files in order across the pane, including nested composer/queue content, once per drop', () => {
  const f = fixture(); render(f.app());
  const files = [image(), new File(['notes'], 'notes.txt', { type: 'text/plain' })];
  for (const target of [screen.getByText('Transcript text'), screen.getByRole('textbox'), screen.getByRole('button')]) {
    const dataTransfer = transfer(files);
    fireEvent.dragEnter(target, { dataTransfer });
    expect(screen.getByRole('status').textContent).toBe('Drop to attach');
    expect(fireEvent.dragOver(target, { dataTransfer })).toBe(false);
    expect(dataTransfer.dropEffect).toBe('copy');
    fireEvent.drop(target, { dataTransfer });
    expect(screen.queryByRole('status')).toBeNull();
  }
  expect(f.onFiles.mock.calls).toEqual([[files], [files], [files]]);
  expect(f.onError).not.toHaveBeenCalled();
});

it('keeps the highlight across nested boundaries and clears it on leaving the pane', () => {
  const f = fixture(); render(f.app()); const dataTransfer = transfer();
  const text = screen.getByText('Transcript text'), input = screen.getByRole('textbox');
  fireEvent.dragEnter(text, { dataTransfer });
  const overlay = screen.getByRole('status');
  fireEvent.dragEnter(input, { dataTransfer });
  fireEvent.dragLeave(text, { dataTransfer });
  expect(screen.getByRole('status')).toBe(overlay);
  fireEvent.dragOver(input, { dataTransfer });
  expect(screen.getByRole('status')).toBe(overlay);
  fireEvent.dragLeave(input, { dataTransfer });
  expect(screen.queryByRole('status')).toBeNull();
});

it('does not confuse text/link/tab drags or portaled dialogs with file attachments', () => {
  const f = fixture(); render(f.app());
  const text = screen.getByText('Transcript text');
  for (const type of ['text/plain', 'text/uri-list', 'application/x-whip-tab']) {
    const dataTransfer = { types: [type], files: [], items: [], dropEffect: 'move' };
    expect(fireEvent.dragOver(text, { dataTransfer })).toBe(true);
    expect(fireEvent.drop(text, { dataTransfer })).toBe(true);
  }
  fireEvent.drop(screen.getByRole('dialog'), { dataTransfer: transfer() });
  expect(screen.queryByRole('status')).toBeNull();
  expect(f.onFiles).not.toHaveBeenCalled();
});

it('prevents unhandled file navigation without changing a valid target cursor or routing outside files', () => {
  const f = fixture(); render(f.app()); const dataTransfer = transfer();
  expect(fireEvent.dragOver(screen.getByText('Transcript text'), { dataTransfer })).toBe(false);
  expect(dataTransfer.dropEffect).toBe('copy');
  expect(fireEvent.dragOver(screen.getByText('Sidebar'), { dataTransfer })).toBe(false);
  expect(dataTransfer.dropEffect).toBe('none');
  expect(screen.queryByRole('status')).toBeNull();
  expect(fireEvent.drop(screen.getByText('Sidebar'), { dataTransfer })).toBe(false);
  expect(f.onFiles).not.toHaveBeenCalled();
});

it('rechecks availability at drop and reports why without discarding files into an unavailable composer', () => {
  const f = fixture(); const view = render(f.app()); const dataTransfer = transfer();
  fireEvent.dragEnter(screen.getByText('Transcript text'), { dataTransfer });
  view.rerender(f.app('Reconnect to attach files.'));
  expect(screen.getByRole('status').textContent).toBe('Reconnect to attach files.');
  fireEvent.dragOver(screen.getByRole('textbox'), { dataTransfer });
  expect(dataTransfer.dropEffect).toBe('none');
  fireEvent.drop(screen.getByRole('textbox'), { dataTransfer });
  expect(f.onError).toHaveBeenCalledExactlyOnceWith('Reconnect to attach files.');
  expect(f.onFiles).not.toHaveBeenCalled();
});

it('blocks background attachment when a modal opens and uses the new recipient after navigation', () => {
  const f = fixture(); const view = render(f.app()); const dataTransfer = transfer();
  fireEvent.dragEnter(screen.getByText('Transcript text'), { dataTransfer });
  screen.getByRole('dialog').setAttribute('aria-modal', 'true');
  fireEvent.dragOver(screen.getByRole('textbox'), { dataTransfer });
  fireEvent.drop(screen.getByRole('textbox'), { dataTransfer });
  expect(f.onFiles).not.toHaveBeenCalled();
  expect(screen.queryByRole('status')).toBeNull();
  screen.getByRole('dialog').removeAttribute('aria-modal');
  const next = vi.fn(); view.rerender(f.app(undefined, 'other:root:child', next));
  fireEvent.drop(screen.getByRole('textbox'), { dataTransfer });
  expect(next).toHaveBeenCalledExactlyOnceWith(dataTransfer.files);
  expect(f.onFiles).not.toHaveBeenCalled();
});

it('only highlights and attaches to the pane under the drag, preserving keyboard focus', () => {
  const first = fixture(), second = fixture();
  render(<>{first.app(undefined, 'first')}{second.app(undefined, 'second')}</>);
  const inputs = screen.getAllByRole('textbox'), dataTransfer = transfer();
  inputs[0]!.focus();
  fireEvent.dragEnter(inputs[0]!, { dataTransfer });
  fireEvent.dragEnter(inputs[1]!, { dataTransfer });
  expect(first.target.current!.querySelector('[data-chat-file-drop]')).toBeNull();
  expect(second.target.current!.querySelector('[data-chat-file-drop]')).not.toBeNull();
  fireEvent.drop(inputs[1]!, { dataTransfer });
  expect(first.onFiles).not.toHaveBeenCalled();
  expect(second.onFiles).toHaveBeenCalledExactlyOnceWith(dataTransfer.files);
  expect(document.activeElement).toBe(inputs[0]);
});

it('clears cancelled/hidden/expired drags and removes listeners on unmount', () => {
  vi.useFakeTimers();
  const f = fixture(); const view = render(f.app()); const dataTransfer = transfer();
  const input = screen.getByRole('textbox');
  for (const clear of [() => fireEvent.keyDown(window, { key: 'Escape' }), () => fireEvent.blur(window),
    () => fireEvent.dragEnd(window), () => act(() => vi.advanceTimersByTime(1500))]) {
    fireEvent.dragEnter(input, { dataTransfer });
    expect(screen.getByRole('status')).toBeTruthy(); clear();
    expect(screen.queryByRole('status')).toBeNull();
  }
  fireEvent.dragEnter(input, { dataTransfer });
  view.unmount();
  expect(vi.getTimerCount()).toBe(0);
  expect(fireEvent.drop(input, { dataTransfer })).toBe(true);
  expect(f.onFiles).not.toHaveBeenCalled();
});

it('rejects folders and unreadable drops and reports attachment failures', async () => {
  const f = fixture(); render(f.app()); const input = screen.getByRole('textbox');
  fireEvent.drop(input, { dataTransfer: { ...transfer(), items: [{ kind: 'file', webkitGetAsEntry: () => ({ isDirectory: true }) }] } });
  fireEvent.drop(input, { dataTransfer: transfer([]) });
  expect(f.onFiles).not.toHaveBeenCalled();
  expect(f.onError.mock.calls.flat()).toEqual(['Drop individual files to attach them. Folders are not supported.', 'These files could not be read. Try the Attach button.']);
  f.onFiles.mockRejectedValueOnce(new Error('File limit reached'));
  await act(async () => { fireEvent.drop(input, { dataTransfer: transfer() }); });
  expect(f.onError).toHaveBeenLastCalledWith('File limit reached');
});

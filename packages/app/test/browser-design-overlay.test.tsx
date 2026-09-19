import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import userEvent from '@testing-library/user-event';
import { defaultDisplayPreferences, themeCatalog } from '@whip/ui';
import { BrowserDesignOverlay, BrowserDesignOverlayRoot } from '../src/browser-design-overlay';
import type { BrowserDesignModel } from '../src/browser-design-types';

vi.hoisted(() => { globalThis.ResizeObserver = class { observe() {} unobserve() {} disconnect() {} }; });
afterEach(() => { cleanup(); delete window.whipBrowserDesign; });
function model(prompt = ''): BrowserDesignModel { return {
  state: { epoch: 'e', tabId: 't', generation: 'g', designId: 'd', documentRevision: 1, selectionRevision: 1, status: 'active', viewport: { width: 1280, height: 800 }, elements: [{ id: 'one', label: '<script>hostile text</script>', number: 1, color: 'blue', bounds: { x: 100, y: 100, width: 80, height: 30 } }] },
  draft: { prompt, recipients: [{ id: 'chat', label: 'Conversation · Local', available: true }], recipientId: 'chat', screenshot: true, delivery: 'queue', busy: false, uncertain: false, theme: { name: 'default', mode: 'light' } },
}; }
describe('isolated design overlay interactions', () => {
  it('renders a measured two-row inspection label and suppresses a coincident hover border', () => {
    vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockReturnValue({ x: 0, y: 0, left: 0, top: 0, right: 280, bottom: 52, width: 280, height: 52, toJSON() {} });
    const next = model();
    next.state.hover = { ...next.state.elements[0]!, id: 'independent-hover-id', label: 'h1 · Inference\n infrastructure for AI-native teams' };
    const view = render(<BrowserDesignOverlay model={next} onIntent={vi.fn()}/>);
    const tip = screen.getByRole('tooltip');
    expect(tip.textContent).toContain('Inference infrastructure for AI-native teams');
    expect(tip.querySelector('strong')).toBeNull();
    expect(tip.style.top).toBe('40px');
    const hiddenClass = document.querySelector('[data-design-hover]')!.className;
    view.rerender(<BrowserDesignOverlay model={{ ...next, state: { ...next.state, hover: { ...next.state.hover, bounds: { x: 300, y: 100, width: 80, height: 30 } } } }} onIntent={vi.fn()}/>);
    expect(document.querySelector('[data-design-hover]')!.className).not.toBe(hiddenClass);
  });
  it('prevents native text-selection gestures on the picker without disabling prompt selection', () => {
    render(<BrowserDesignOverlay model={model()} onIntent={vi.fn()}/>);
    const picker = screen.getByRole('button', { name: 'Pick an element' });
    expect(fireEvent.pointerDown(picker)).toBe(false);
    expect(document.activeElement).toBe(picker);
    expect(fireEvent.pointerDown(screen.getByRole('textbox'))).toBe(true);
  });
  it('keeps one hover DOM node, snaps geometry updates and reduced motion, and never delays picks', () => {
    const onIntent = vi.fn();
    const hovered = (id: string, x: number): BrowserDesignModel => {
      const next = model();
      next.state.hoverGeometryRevision = 0;
      next.state.hover = { id, label: id, number: 0, color: 'blue', bounds: { x, y: 20, width: 100, height: 40 } };
      return next;
    };
    const view = render(<BrowserDesignOverlay model={hovered('a', 10)} onIntent={onIntent}/>);
    const outline = document.querySelector('[data-design-hover]') as HTMLElement;
    const snapClass = outline.className;
    const selected = outline.previousElementSibling as HTMLElement;
    const selectedClass = selected.className;
    view.rerender(<BrowserDesignOverlay model={hovered('b', 200)} onIntent={onIntent}/>);
    expect(document.querySelector('[data-design-hover]')).toBe(outline);
    expect(outline.className).not.toBe(snapClass);
    const movingClass = outline.className;
    expect(outline.style.left).toBe('200px');
    fireEvent.click(screen.getByRole('button', { name: 'Pick an element' }), { clientX: 220, clientY: 35 });
    expect(onIntent).toHaveBeenLastCalledWith({ kind: 'pick', x: 220, y: 35, additive: false });
    view.rerender(<BrowserDesignOverlay model={hovered('b', 200)} onIntent={onIntent}/>);
    expect(outline.className).toBe(movingClass);
    view.rerender(<BrowserDesignOverlay model={hovered('c', 400)} onIntent={onIntent}/>);
    expect(outline.className).toBe(movingClass);
    expect(selected.className).toBe(selectedClass);
    expect(selected.style.left).toBe('100px');
    view.rerender(<BrowserDesignOverlay model={hovered('c', 401)} onIntent={onIntent}/>);
    expect(outline.className).toBe(snapClass);
    const reduced = hovered('d', 600);
    reduced.draft.theme.display = { ...defaultDisplayPreferences, motion: 'reduce' };
    view.rerender(<BrowserDesignOverlay model={reduced} onIntent={onIntent}/>);
    expect(outline.className).toBe(snapClass);
    const invalidated = hovered('e', 700); invalidated.state.hoverGeometryRevision = 1;
    view.rerender(<BrowserDesignOverlay model={invalidated} onIntent={onIntent}/>);
    expect(outline.className).toBe(snapClass);
    view.rerender(<BrowserDesignOverlay model={{ ...invalidated, state: { ...invalidated.state, status: 'stale' } }} onIntent={onIntent}/>);
    expect(document.querySelector('[data-design-hover]')).toBeNull();
    view.rerender(<BrowserDesignOverlay model={hovered('a', 10)} onIntent={onIntent}/>);
    expect(document.querySelector('[data-design-hover]')?.className).toBe(snapClass);
  });
  it('mirrors a validated custom palette and display preferences without an app runtime', async () => {
    const projected = model();
    projected.draft.theme = { name: 'custom-design-test', mode: 'dark', palette: { ...themeCatalog[0]!, id: 'custom-design-test', dark: true }, display: { ...defaultDisplayPreferences, uiFont: 'system', uiSize: 20, motion: 'reduce', contrast: 'more' } };
    window.whipBrowserDesign = { snapshot: async () => projected, intent: async () => {}, onModel: () => () => {} };
    render(<BrowserDesignOverlayRoot/>);
    await waitFor(() => expect(document.documentElement.dataset.theme).toBe('custom-design-test'));
    expect(document.documentElement.dataset.motion).toBe('reduce');
    expect(document.documentElement.style.getPropertyValue('--whip-size-13')).toBe('20px');
    expect(document.documentElement.style.getPropertyValue('--whip-font-sans')).not.toContain('Inter Variable');
    expect(document.documentElement.style.backgroundColor).toBe('transparent');
  });
  it('uses shared screenshot and submit controls without page picks', async () => {
    const user = userEvent.setup(); const onIntent = vi.fn();
    const view = render(<BrowserDesignOverlay model={model('Change')} onIntent={onIntent}/>);
    expect(document.querySelector('select')).toBeNull();
    await user.click(screen.getByRole('checkbox', { name: 'Include Screenshots' }));
    expect(onIntent).toHaveBeenLastCalledWith({ kind: 'screenshot', value: false });
    await user.click(screen.getByRole('button', { name: 'Send design change' }));
    expect(onIntent.mock.calls.slice(-2).map(call => call[0])).toEqual([{ kind: 'prompt', value: 'Change' }, { kind: 'send' }]);
    const sending = screen.getByRole('button', { name: 'Sending design change' });
    expect(sending.getAttribute('aria-busy')).toBe('true');
    expect((sending as HTMLButtonElement).disabled).toBe(true);
    expect(onIntent.mock.calls.some(([intent]) => intent.kind === 'pick')).toBe(false);
    const uncertain = model('Change'); uncertain.draft.uncertain = true;
    view.rerender(<BrowserDesignOverlay model={uncertain} onIntent={onIntent}/>);
    expect(screen.getByRole('checkbox', { name: 'Include Screenshots' }).getAttribute('aria-disabled')).toBe('true');
  });
  it('sizes authored multiline text to a bounded height and shrinks after admission', () => {
    const onIntent = vi.fn(); const view = render(<BrowserDesignOverlay model={model()} onIntent={onIntent}/>);
    const input = screen.getByRole('textbox', { name: 'Describe the change' }) as HTMLTextAreaElement;
    let measuredHeight = 144;
    Object.defineProperty(input, 'scrollHeight', { configurable: true, get: () => measuredHeight });
    fireEvent.change(input, { target: { value: 'First line\nSecond line' } });
    expect(input.style.height).toBe('144px');
    measuredHeight = 500;
    fireEvent.change(input, { target: { value: 'A much longer change' } });
    expect(input.style.height).toBe('220px');
    measuredHeight = 20;
    const accepted = model(); accepted.draft.promptReset = 1;
    view.rerender(<BrowserDesignOverlay model={accepted} onIntent={onIntent}/>);
    expect(input.style.height).toBe('78px');
  });
  it('opens isolated portalled conversations, preserves unavailable options, and consumes menu Escape', async () => {
    const user = userEvent.setup(); const onIntent = vi.fn(); const projected = model('Change');
    projected.draft.recipients.push({ id: 'other', label: 'Other conversation', available: true }, { id: 'offline', label: 'Offline conversation', available: false });
    render(<BrowserDesignOverlay model={projected} onIntent={onIntent}/>);
    const trigger = screen.getByRole('combobox', { name: 'Conversation' });
    await user.click(trigger);
    const list = await screen.findByRole('listbox');
    expect(screen.getByRole('form', { name: 'Describe a design change' }).contains(list)).toBe(false);
    expect(screen.getByRole('option', { name: 'Offline conversation (unavailable)' }).getAttribute('aria-disabled')).toBe('true');
    await user.click(screen.getByRole('option', { name: 'Other conversation' }));
    expect(onIntent).toHaveBeenLastCalledWith({ kind: 'recipient', id: 'other' });
    await waitFor(() => expect(document.activeElement).toBe(trigger));
    await user.click(trigger); await screen.findByRole('listbox');
    await user.keyboard('{Escape}');
    await waitFor(() => expect(screen.queryByRole('listbox')).toBeNull());
    expect(onIntent.mock.calls.some(([intent]) => intent.kind === 'stop' || intent.kind === 'pick')).toBe(false);
    expect(document.activeElement).toBe(trigger);
  });
  it('does not turn dropdown typeahead into ancestor selection', async () => {
    const user = userEvent.setup(); const onIntent = vi.fn();
    render(<BrowserDesignOverlay model={model('Change')} onIntent={onIntent}/>);
    await user.click(screen.getByRole('combobox', { name: 'Conversation' }));
    const list = await screen.findByRole('listbox');
    fireEvent.keyDown(list, { key: 'Backspace' });
    await user.keyboard('{Escape}');
    expect(onIntent.mock.calls.some(([intent]) => intent.kind === 'pick' || intent.kind === 'ancestor' || intent.kind === 'stop')).toBe(false);
  });
  it('leaves an ambiguous conversation unset and Send disabled', () => {
    const projected = model('Change'); projected.draft.recipientId = '';
    render(<BrowserDesignOverlay model={projected} onIntent={vi.fn()}/>);
    expect(screen.getByRole('combobox', { name: 'Conversation' }).textContent).toContain('Choose a conversation…');
    expect((screen.getByRole('button', { name: 'Send design change' }) as HTMLButtonElement).disabled).toBe(true);
  });
  it('changes Queue/Steer through keyboard dropdown interaction without submitting', async () => {
    const user = userEvent.setup(); const onIntent = vi.fn();
    render(<BrowserDesignOverlay model={model('Change')} onIntent={onIntent}/>);
    const trigger = screen.getByRole('combobox', { name: 'Delivery when busy' });
    trigger.focus(); await user.keyboard('{ArrowDown}'); await screen.findByRole('listbox');
    await user.keyboard('{End}{Enter}');
    expect(onIntent).toHaveBeenLastCalledWith({ kind: 'delivery', value: 'steer' });
    expect(onIntent.mock.calls.some(([intent]) => intent.kind === 'pick' || intent.kind === 'send')).toBe(false);
  });
  it('never rewinds newer local text when earlier IPC echoes arrive', () => {
    const onIntent = vi.fn(); const view = render(<BrowserDesignOverlay model={model()} onIntent={onIntent}/>);
    const input = screen.getByRole('textbox', { name: 'Describe the change' }) as HTMLTextAreaElement;
    fireEvent.change(input, { target: { value: 'A' } }); fireEvent.change(input, { target: { value: 'AB' } });
    view.rerender(<BrowserDesignOverlay model={model('A')} onIntent={onIntent}/>); expect(input.value).toBe('AB');
    fireEvent.change(input, { target: { value: 'ABC日本' } });
    view.rerender(<BrowserDesignOverlay model={model('AB')} onIntent={onIntent}/>); expect(input.value).toBe('ABC日本');
    view.rerender(<BrowserDesignOverlay model={model('ABC日本')} onIntent={onIntent}/>); expect(input.value).toBe('ABC日本');
    fireEvent.keyDown(input, { key: 'Enter', ctrlKey: true }); expect(onIntent).toHaveBeenLastCalledWith({ kind: 'send' });
    const accepted = model(''); accepted.draft.promptReset = 1;
    view.rerender(<BrowserDesignOverlay model={accepted} onIntent={onIntent}/>); expect(input.value).toBe('');
  });
  it('never treats matching older text as acknowledgement of an A → AB → A edit', () => {
    const onIntent = vi.fn(); const view = render(<BrowserDesignOverlay model={model()} onIntent={onIntent}/>);
    const input = screen.getByRole('textbox', { name: 'Describe the change' }) as HTMLTextAreaElement;
    fireEvent.change(input, { target: { value: 'A' } }); fireEvent.change(input, { target: { value: 'AB' } }); fireEvent.change(input, { target: { value: 'A' } });
    view.rerender(<BrowserDesignOverlay model={model('A')} onIntent={onIntent}/>);
    view.rerender(<BrowserDesignOverlay model={model('AB')} onIntent={onIntent}/>); expect(input.value).toBe('A');
    fireEvent.keyDown(input, { key: 'Enter', ctrlKey: true });
    expect(onIntent.mock.calls.slice(-2).map(call => call[0])).toEqual([{ kind: 'prompt', value: 'A' }, { kind: 'send' }]);
    expect(input.disabled).toBe(true);
  });
  it('distinguishes a delayed empty edit echo from an admission reset', () => {
    const onIntent = vi.fn(); const view = render(<BrowserDesignOverlay model={model('Old')} onIntent={onIntent}/>);
    const input = screen.getByRole('textbox', { name: 'Describe the change' }) as HTMLTextAreaElement;
    fireEvent.change(input, { target: { value: '' } }); fireEvent.change(input, { target: { value: 'New' } });
    view.rerender(<BrowserDesignOverlay model={model('')} onIntent={onIntent}/>); expect(input.value).toBe('New');
    view.rerender(<BrowserDesignOverlay model={model('New')} onIntent={onIntent}/>); expect(input.value).toBe('New');
  });
  it('preserves multiline and IME input; composer actions never become picks', () => {
    const onIntent = vi.fn(); render(<BrowserDesignOverlay model={model('Change')} onIntent={onIntent}/>);
    const input = screen.getByRole('textbox', { name: 'Describe the change' });
    fireEvent.keyDown(input, { key: 'Enter', ctrlKey: true, isComposing: true });
    fireEvent.keyDown(input, { key: 'Enter' }); fireEvent.click(input);
    expect(onIntent).not.toHaveBeenCalled();
    fireEvent.keyDown(input, { key: 'Enter', metaKey: true }); expect(onIntent).toHaveBeenCalledWith({ kind: 'send' });
    expect(document.querySelector('script')).toBeNull();
  });
  it('keeps keyboard target traversal on the picker, with additive selection', () => {
    const onIntent = vi.fn(); const empty = model(); empty.state.elements = [];
    render(<BrowserDesignOverlay model={empty} onIntent={onIntent}/>);
    const picker = screen.getByRole('button', { name: 'Pick an element' }); expect(document.activeElement).toBe(picker);
    fireEvent.keyDown(picker, { key: 'Tab' }); expect(onIntent).toHaveBeenLastCalledWith({ kind: 'next' });
    fireEvent.keyDown(picker, { key: 'Tab', shiftKey: true }); expect(onIntent).toHaveBeenLastCalledWith({ kind: 'previous' });
    fireEvent.keyDown(picker, { key: 'Enter', shiftKey: true }); expect(onIntent).toHaveBeenLastCalledWith({ kind: 'pick-hover', additive: true });
    fireEvent.keyDown(picker, { key: 'ArrowUp' }); expect(onIntent).toHaveBeenLastCalledWith({ kind: 'ancestor' });
  });
  it('returns to picking after admission and opens the next composer without the previous preview', () => {
    const onIntent = vi.fn(); const view = render(<BrowserDesignOverlay model={model('Change')} onIntent={onIntent}/>);
    fireEvent.click(screen.getByRole('button', { name: 'Preview' }));
    expect(screen.getByRole('region', { name: 'Design evidence' })).toBeTruthy();
    const accepted = model(); accepted.draft.promptReset = 1;
    accepted.state.elements = []; accepted.state.selectionRevision = 2;
    view.rerender(<BrowserDesignOverlay model={accepted} onIntent={onIntent}/>);
    expect(screen.queryByRole('form', { name: 'Describe a design change' })).toBeNull();
    expect(screen.queryByRole('region', { name: 'Design evidence' })).toBeNull();
    expect(document.activeElement).toBe(screen.getByRole('button', { name: 'Pick an element' }));
    const next = model(); next.draft.promptReset = 1; next.state.selectionRevision = 3;
    view.rerender(<BrowserDesignOverlay model={next} onIntent={onIntent}/>);
    expect((screen.getByRole('textbox', { name: 'Describe the change' }) as HTMLTextAreaElement).value).toBe('');
    expect(screen.queryByRole('region', { name: 'Design evidence' })).toBeNull();
    expect(onIntent).not.toHaveBeenCalledWith({ kind: 'stop' });
  });
  it('releases screenshot preview payload on close and Escape does not exit until preview closes', () => {
    const onIntent = vi.fn(); render(<BrowserDesignOverlay model={model('Change')} onIntent={onIntent}/>);
    fireEvent.click(screen.getByRole('button', { name: 'Preview' })); expect(onIntent).toHaveBeenLastCalledWith({ kind: 'capture' });
    fireEvent.keyDown(screen.getByRole('textbox', { name: 'Describe the change' }), { key: 'Escape' });
    expect(onIntent).toHaveBeenLastCalledWith({ kind: 'evidence-close' });
    fireEvent.keyDown(screen.getByRole('textbox', { name: 'Describe the change' }), { key: 'Escape' });
    expect(onIntent).toHaveBeenLastCalledWith({ kind: 'stop' });
  });
});

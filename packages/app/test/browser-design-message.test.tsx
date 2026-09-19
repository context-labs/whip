import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { RuntimeContext } from '../src/context';
import type { AppRuntime } from '../src/runtime';
import { MessageRow } from '../src/timeline';
import { messagePresentation } from '../src/conversation-rows';

afterEach(cleanup);
it('renders persisted evidence as a reference, not authored prose, with one screenshot', () => {
  const content = [
    { type: 'text', text: 'Make this heading smaller' },
    { type: 'text', text: '{"selector":"#heading","styles":{"fontSize":"40px"}}' },
    { type: 'image_url', image_url: { url: 'data:image/png;base64,AA==' } },
  ];
  const parts = messagePresentation(content, { version: 1, design_context: {
    context_attachment_id: 'context', screenshot_attachment_id: 'image', elements: [{ label: 'h1 · Heading' }],
    element_count: 1, context_part_index: 1, screenshot_part_index: 2, page_url: 'https://example.com/settings',
  } });
  const { container } = render(<RuntimeContext.Provider value={{ report: vi.fn() } as unknown as AppRuntime}>
    <MessageRow row={{ id: 'persisted', role: 'user', ...parts }} readBody={vi.fn()}/>
  </RuntimeContext.Provider>);
  expect(container.querySelector('[data-user-bubble]')?.textContent).toBe('Make this heading smaller');
  expect(screen.getByText('Design Mode')).toBeTruthy();
  expect(screen.queryByText('h1 · Heading')).toBeNull();
  expect(screen.queryByText(content[1]!.text!)).toBeNull();
  expect(screen.getAllByRole('button', { name: 'Open design screenshot' })).toHaveLength(1);
  fireEvent.click(screen.getByRole('button', { name: 'Design Mode details' }));
  expect(screen.getByRole('dialog', { name: 'Captured page context' })).toBeTruthy();
  expect(screen.getByText('h1 · Heading')).toBeTruthy();
  expect(screen.getByText(content[1]!.text!)).toBeTruthy();
});

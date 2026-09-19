import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { expect, it, vi } from 'vitest';
import { UIProvider } from '@whip/ui';
import { BrowserDesignAttachment } from '../src/browser-design-attachment';

const context = {
  title: 'Checkout', url: 'https://shop.example/cart?private=value', element_count: 6,
  elements: [
    { label: 'Submit order', selector: 'button[data-submit]' },
    { label: 'Total', selector: '#total' },
    { label: '<img src=x onerror=alert(1)>', selector: '.unsafe-label' },
    { label: 'Shipping', selector: '#shipping' },
  ],
};

it('keeps evidence compact, inspects plain text, and copies raw context without fetching supplied text', async () => {
  const readContext = vi.fn();
  const writeText = vi.fn().mockResolvedValue(undefined);
  vi.stubGlobal('navigator', { ...navigator, clipboard: { writeText } });
  const rawText = '<script>alert("untrusted")</script>';
  render(<UIProvider><BrowserDesignAttachment context={context} rawText={rawText} readContext={readContext}
    screenshot={<img src="data:image/png;base64," alt="Captured screenshot" />} /></UIProvider>);
  expect(screen.getByText('+4 elements')).toBeTruthy();
  expect(screen.getByText('/cart')).toBeTruthy();
  expect(screen.getByRole('img', { name: 'Captured screenshot' })).toBeTruthy();
  expect(screen.queryByText('Shipping')).toBeNull();
  expect(screen.queryByText(rawText)).toBeNull();
  const trigger = screen.getByRole('button', { name: 'View captured page context' });
  trigger.focus(); fireEvent.click(trigger);
  const dialog = await screen.findByRole('dialog', { name: 'Captured page context' });
  expect(within(dialog).getByText(context.url)).toBeTruthy();
  expect(within(dialog).getByText('Showing 4 of 6 elements. Open raw context for the full captured evidence.')).toBeTruthy();
  expect(within(dialog).getByText('button[data-submit]')).toBeTruthy();
  expect(within(dialog).getByText(context.elements[2]!.label)).toBeTruthy();
  expect(dialog.querySelector('img, script, a')).toBeNull();
  const details = within(dialog).getByText('Raw context').closest('details')!;
  expect(details.open).toBe(false);
  fireEvent.click(within(dialog).getByText('Raw context'));
  expect(within(dialog).getByText(rawText)).toBeTruthy();
  fireEvent.click(within(dialog).getByRole('button', { name: 'Copy raw context' }));
  await waitFor(() => expect(writeText).toHaveBeenCalledWith(rawText));
  expect(readContext).not.toHaveBeenCalled();
  fireEvent.keyDown(dialog, { key: 'Escape' });
  await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
  await waitFor(() => expect(document.activeElement).toBe(trigger));
  vi.unstubAllGlobals();
});

it('loads only on inspection, exposes retry, and aborts closed reads without admitting late results', async () => {
  let signal: AbortSignal | undefined;
  let resolve: (text: string) => void = () => {};
  const readContext = vi.fn<(signal: AbortSignal) => Promise<string>>()
    .mockRejectedValueOnce(new Error('offline'))
    .mockImplementationOnce(value => {
      signal = value;
      return new Promise<string>(done => { resolve = done; });
    })
    .mockResolvedValue('fresh raw context');
  render(<UIProvider><BrowserDesignAttachment context={context} readContext={readContext} /></UIProvider>);
  expect(readContext).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole('button', { name: 'View captured page context' }));
  const dialog = await screen.findByRole('dialog');
  fireEvent.click(within(dialog).getByText('Raw context'));
  expect(await within(dialog).findByText('Could not load raw context.')).toBeTruthy();
  fireEvent.click(within(dialog).getByRole('button', { name: 'Retry' }));
  await waitFor(() => expect(readContext).toHaveBeenCalledTimes(2));
  fireEvent.keyDown(dialog, { key: 'Escape' });
  await waitFor(() => expect(signal?.aborted).toBe(true));
  await act(async () => resolve('late raw context'));
  fireEvent.click(screen.getByRole('button', { name: 'View captured page context' }));
  const reopened = await screen.findByRole('dialog');
  fireEvent.click(within(reopened).getByText('Raw context'));
  expect(await within(reopened).findByText('fresh raw context')).toBeTruthy();
  expect(screen.queryByText('late raw context')).toBeNull();
});

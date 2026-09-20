import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { expect, it, vi } from 'vitest';
import { UIProvider } from '@whip/ui';
import { ComposerAttachments } from '../src/composer-attachments';
import type { CompositionAttachment } from '../src/compositions';

const image: CompositionAttachment = { id: 'one', name: 'shot.png', size: 20, mediaType: 'image/png', previewUrl: 'blob:one' };
const ready: CompositionAttachment = { ...image, value: { kind: 'image', name: image.name, ref: 'content' } };
function app(attachments: readonly CompositionAttachment[], onRemove = vi.fn()) {
  return <UIProvider><ComposerAttachments attachments={attachments} owner="host:root:root" onRemove={onRemove} /></UIProvider>;
}

it('shows a local preview before upload finishes and settles without remounting the image', () => {
  const rendered = render(app([image]));
  const img = screen.getByRole('img', { name: 'shot.png' });
  expect(screen.getByRole('img', { name: 'Loading preview of shot.png' })).toBeTruthy();
  fireEvent.load(img);
  expect(screen.queryByRole('img', { name: 'Loading preview of shot.png' })).toBeNull();
  expect(screen.getByRole('img', { name: 'Uploading shot.png' })).toBeTruthy();
  rendered.rerender(app([ready]));
  expect(screen.getByRole('img', { name: 'shot.png' })).toBe(img);
  expect(screen.queryByRole('img', { name: 'Uploading shot.png' })).toBeNull();
  expect(screen.queryByText(/Ready/)).toBeNull();
});

it('keeps duplicate filenames, mixed files, and removal separate from preview activation', () => {
  const onRemove = vi.fn();
  render(app([ready, { ...ready, id: 'two', previewUrl: 'blob:two' },
    { id: 'text', name: 'notes.txt', size: 10, value: { kind: 'text', name: 'notes.txt', ref: 'text' } }], onRemove));
  expect(screen.getAllByRole('button', { name: 'Preview shot.png' })).toHaveLength(2);
  expect(screen.getByText('notes.txt · Ready')).toBeTruthy();
  fireEvent.click(screen.getAllByRole('button', { name: 'Remove shot.png' })[1]!);
  expect(onRemove).toHaveBeenCalledExactlyOnceWith('two');
  expect(screen.queryByRole('dialog')).toBeNull();
});

it('opens the original and returns focus to its thumbnail after Escape', async () => {
  render(app([ready]));
  fireEvent.load(screen.getByRole('img', { name: 'shot.png' }));
  const trigger = screen.getByRole('button', { name: 'Preview shot.png' });
  trigger.focus(); fireEvent.click(trigger);
  const dialog = await screen.findByRole('dialog', { name: 'shot.png' });
  expect(dialog.querySelector('img')?.getAttribute('src')).toBe('blob:one');
  fireEvent.keyDown(dialog, { key: 'Escape' });
  await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
  await waitFor(() => expect(document.activeElement).toBe(trigger));
});

it('distinguishes decode failure from upload failure and keeps removal available', () => {
  const rendered = render(app([ready]));
  fireEvent.error(screen.getByRole('img', { name: 'shot.png' }));
  expect(screen.getByText('Preview unavailable')).toBeTruthy();
  expect((screen.getByRole('button', { name: 'Preview shot.png' }) as HTMLButtonElement).disabled).toBe(true);
  expect(screen.queryByText('Upload failed')).toBeNull();
  rendered.rerender(app([{ ...image, error: 'Connection lost' }]));
  expect(screen.getByText('Upload failed')).toBeTruthy();
  expect(screen.getByText('shot.png could not upload')).toBeTruthy();
  expect((screen.getByRole('button', { name: 'Remove shot.png' }) as HTMLButtonElement).disabled).toBe(false);
});

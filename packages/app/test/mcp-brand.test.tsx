import { fireEvent, render } from '@testing-library/react';
import { expect, it } from 'vitest';
import { MCPBrandIcon, tintIndex } from '../src/mcp-brand';
import { tinyPNG } from './mcp-import-fake';
import marks from '../src/assets/mcp-brands.json';

it('shows the mark when there is one and a monogram when there is not, or when the mark fails to decode', () => {
  const { container, rerender } = render(<MCPBrandIcon name="exa" src={tinyPNG} />);
  const image = container.querySelector('img');
  expect(image?.getAttribute('src')).toBe(tinyPNG);
  expect(image?.getAttribute('alt')).toBe('');
  expect(container.textContent).toBe('');
  fireEvent.error(image!);
  expect(container.querySelector('img')).toBeNull();
  expect(container.textContent).toBe('E');
  rerender(<MCPBrandIcon name="paper" />);
  expect(container.textContent).toBe('P');
  expect(container.firstElementChild?.getAttribute('aria-hidden')).toBe('true');
});

it('gives a name the same tint every time and spreads names across the palette', () => {
  expect(tintIndex('paper')).toBe(tintIndex('paper'));
  expect(new Set(['paper', 'exa', 'ahrefs', 'figma', 'linear', 'notion', 'chrome', 'playwright', 'executor', 'getleads'].map(tintIndex)).size).toBeGreaterThan(2);
});

it('bundles a small mark for each listed domain as a data URI', () => {
  const entries = Object.entries(marks as Record<string, string>);
  expect(entries.length).toBeGreaterThan(150);
  for (const [domain, src] of entries) {
    expect(domain).toMatch(/^[a-z0-9.-]+\.[a-z]+$/);
    expect(src).toMatch(/^data:image\/(png|jpeg|webp|svg\+xml);base64,/);
    expect(src.length).toBeLessThan(20_000);
  }
  expect(marks).toHaveProperty(['figma.com']);
  expect(marks).toHaveProperty(['ahrefs.com']);
});

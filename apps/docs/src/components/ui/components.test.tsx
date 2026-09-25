import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { renderToStaticMarkup } from 'react-dom/server';
import { evaluate } from '@mdx-js/mdx';
import * as runtime from 'react/jsx-runtime';
import source from '../stories/CodeFixture.mdx?raw';
import { mdxOptions } from '../../../scripts/mdx-plugins.mjs';
import { docsComponents } from '../../features/docs/docs-components';
import { Callout } from './Callout';
import { CopyButton } from './CopyButton';
import { CodeBlock } from './CodeBlock';
import { SplitButton } from './Button';
import { MobileNavigation } from '../navigation';
import { ThemeMenu } from './theme/ThemeMenu';
import { applyTheme, readThemePreference, saveThemePreference } from './theme/theme';

beforeEach(() => {
  vi.stubGlobal('matchMedia', vi.fn(() => ({ matches: false, addEventListener: vi.fn(), removeEventListener: vi.fn() })));
  vi.stubGlobal('ResizeObserver', class { observe() {} unobserve() {} disconnect() {} });
  Element.prototype.scrollIntoView = vi.fn();
});
afterEach(() => { cleanup(); vi.unstubAllGlobals(); localStorage.clear(); document.documentElement.removeAttribute('data-theme'); });

async function fixture() {
  return (await evaluate(source, { ...mdxOptions, ...runtime })).default;
}

describe('documentation components', () => {
  it('keeps static callouts out of live alert announcements', () => {
    render(<Callout title="Review permissions" type="alert"><p>Read the requested action.</p></Callout>);
    expect(screen.queryByRole('alert')).toBeNull();
    expect(screen.getByRole('complementary').getAttribute('aria-labelledby')).toBeTruthy();
  });
  it('renders compiled tokens, never code as HTML', () => {
    const html = renderToStaticMarkup(<CodeBlock data-language="text" data-raw={'<script>&\n'}><code><span className="token string">{'<script>&\n'}</span></code></CodeBlock>);
    expect(html).toContain('token string');
    expect(html).toContain('&lt;script&gt;&amp;');
    expect(html).not.toContain('<script>');
    expect(html).not.toContain('<button');
  });
  it('copies exact text and reports success', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText } });
    const raw = '  世界\t<script>&\n';
    render(<CopyButton text={raw} />);
    fireEvent.click(screen.getByRole('button', { name: 'Copy code' }));
    await waitFor(() => expect(writeText).toHaveBeenCalledWith(raw));
    await screen.findByRole('button', { name: 'Copied' });
    expect(screen.getByRole('status').textContent).toBe('Copied');
  });
  it('offers manual selection when clipboard access is denied', async () => {
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText: vi.fn().mockRejectedValue(new Error('denied')) } });
    render(<CopyButton text="example" />);
    fireEvent.click(screen.getByRole('button', { name: 'Copy code' }));
    await waitFor(() => expect(screen.getByRole('status').textContent).toContain('Select and copy the code manually'));
  });
  it('renders every real MDX tab in server HTML without JavaScript', async () => {
    const Fixture = await fixture();
    const html = renderToStaticMarkup(<Fixture components={docsComponents} />);
    expect(new DOMParser().parseFromString(html, 'text/html').body.textContent).toContain('whipcode --help');
    expect(html).toContain('TypeScript');
    expect(html).toContain('Python');
    expect(html).toContain('token keyword');
    expect(html).not.toContain('role="tab"');
    expect(html).not.toContain('hidden=""');
  });
  it('moves tabs with the keyboard and copies only the selected original snippet', async () => {
    const user = userEvent.setup();
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText } });
    const Fixture = await fixture();
    render(<Fixture components={docsComponents} />);
    const first = screen.getByRole('tab', { name: 'CLI' });
    first.focus();
    await user.keyboard('{ArrowRight}');
    const second = screen.getByRole('tab', { name: 'TypeScript' });
    await waitFor(() => expect(second.getAttribute('aria-selected')).toBe('true'));
    await user.click(screen.getByRole('button', { name: 'Copy code' }));
    await waitFor(() => expect(writeText).toHaveBeenCalledTimes(1));
    expect(writeText.mock.calls[0][0]).toBe('// A build-time syntax example\nconst greeting: string = "Hello, 世界";\nfunction greet(name: string): string {\n  return `${greeting}, ${name}`;\n}\nconsole.log(greet("reader"));\n');
  });
  it('uses an operable keyboard menu for split actions', async () => {
    const user = userEvent.setup(); const alternate = vi.fn();
    render(<SplitButton label="Run example" onClick={() => {}} actions={[{ label: 'Alternate', onClick: alternate }]} />);
    await user.click(screen.getByRole('button', { name: 'More actions for Run example' }));
    await user.click(await screen.findByRole('menuitem', { name: 'Alternate' }));
    expect(alternate).toHaveBeenCalledOnce();
  });
  it('closes mobile navigation on Escape and restores trigger focus', async () => {
    const user = userEvent.setup();
    render(<MobileNavigation title="Documentation"><a href="/docs">Docs overview</a></MobileNavigation>);
    const trigger = screen.getByRole('button', { name: 'Documentation' });
    await user.click(trigger);
    expect(screen.getByRole('dialog')).toBeTruthy();
    await user.keyboard('{Escape}');
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
    await waitFor(() => expect(document.activeElement).toBe(trigger));
  });
  it('selects a persistent theme without changing syntax markup', async () => {
    const user = userEvent.setup();
    render(<ThemeMenu />);
    await user.click(screen.getByRole('button', { name: 'Colour theme: system' }));
    await user.click(await screen.findByRole('menuitemradio', { name: 'Dark' }));
    expect(document.documentElement.dataset.theme).toBe('dark');
    expect(readThemePreference()).toBe('dark');
    saveThemePreference('light'); expect(document.documentElement.dataset.theme).toBe('light');
    applyTheme('system'); expect(document.documentElement.dataset.theme).toBe('light');
  });
});

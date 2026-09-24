const violations: string[] = [];
document.addEventListener('securitypolicyviolation', event => violations.push(`${event.violatedDirective}: ${event.blockedURI}`));
const failures: string[] = [];
window.addEventListener('error', event => failures.push(event.message));
window.addEventListener('unhandledrejection', event => failures.push(String(event.reason)));

async function waitFor<T>(read: () => T): Promise<NonNullable<T>> {
  const deadline = performance.now() + 10_000;
  while (performance.now() < deadline) {
    const value = read();
    if (value) return value as NonNullable<T>;
    await new Promise(resolve => setTimeout(resolve, 40));
  }
  throw new Error('Timed out waiting for a rendered component');
}
function button(name: string) {
  const target = [...document.querySelectorAll('button')].find(node => (node.getAttribute('aria-label') ?? node.textContent)?.trim() === name);
  if (!target) throw new Error(`Missing button: ${name}`);
  return target;
}
function assert(value: unknown, message: string): asserts value {
  if (!value) throw new Error(message);
}
export async function runBrowserSmoke() {
  if (new URLSearchParams(location.search).get('smoke') !== '1') return;
  const report: Record<string, unknown> = {browser: 'safari', userAgent: navigator.userAgent};
  try {
    const code = await waitFor(() => document.querySelector<HTMLElement>('figure[data-highlighted="true"] [aria-label="Starlark example"]'));
    assert(code.textContent?.includes('return agents.get'), 'Missing highlighted text');
    const keyword = code.querySelector('[data-token="keyword"]')?.firstChild;
    assert(keyword, 'Missing highlighted keyword');
    const range = document.createRange(); range.selectNodeContents(keyword);
    const selection = getSelection(); assert(selection, 'Missing selection capability');
    selection.removeAllRanges(); selection.addRange(range); const selectedText = selection.toString();
    button('Switch theme').click();
    await waitFor(() => document.documentElement.dataset.theme === 'light');
    assert(code.querySelector('[data-token="keyword"]')?.firstChild === keyword, 'Theme switch replaced the code text node');
    assert(selection.toString() === selectedText, 'Theme switch lost code selection');
    button('Custom code theme').click();
    await waitFor(() => document.documentElement.dataset.theme === 'custom:csp');
    const token = code.querySelector<HTMLElement>('[data-token="keyword"]'); assert(token, 'Missing custom keyword');
    const tokenStyle = getComputedStyle(token);
    assert(getComputedStyle(code).backgroundColor === 'rgb(16, 32, 48)', 'Custom code background was discarded');
    assert(tokenStyle.backgroundColor === 'rgb(32, 48, 64)' && tokenStyle.fontWeight === '700' && tokenStyle.fontStyle === 'italic' && tokenStyle.textDecorationLine.includes('underline'), 'Custom Chroma attributes were discarded');
    assert(!document.querySelector('[aria-label="Escaped JavaScript"] img, [aria-label="Escaped JavaScript"] script'), 'Source text became HTML');
    assert(document.body.textContent?.includes('Showing a bounded excerpt'), 'Large output lacks a truncation notice');
    button('Open dialog').click();
    const dialog = await waitFor(() => document.querySelector('[role="dialog"]'));
    assert(dialog.textContent?.includes('return True'), 'Portaled code is missing');
    (document.activeElement ?? document).dispatchEvent(new KeyboardEvent('keydown', {key: 'Escape', code: 'Escape', bubbles: true}));
    await waitFor(() => !document.querySelector('[role="dialog"]'));
    button('Open menu').click();
    const item = await waitFor(() => document.querySelector<HTMLElement>('[role="menuitem"]'));
    item.click();
    await waitFor(() => !document.querySelector('[role="menu"]'));
    const exactCode = code.textContent;
    button('Increase readability').click();
    await waitFor(() => document.documentElement.dataset.contrast === 'more');
    assert(getComputedStyle(code).fontSize === '24px' && getComputedStyle(code).whiteSpace === 'pre-wrap', 'Display preferences did not apply under CSP');
    assert(code.textContent === exactCode, 'Wrapping changed original code text');
    const rangeInput = document.querySelector<HTMLInputElement>('input[type="range"]');
    assert(rangeInput?.getAttribute('aria-valuetext') === 'Compact', 'Slider has no named accessible value');
    button('Reset display').click();
    await waitFor(() => getComputedStyle(code).fontSize === '12px');
    await new Promise(resolve => setTimeout(resolve, 100));
    assert(!violations.length && !failures.length, JSON.stringify({violations, failures}));
    Object.assign(report, {passed: true, codeSelectionRetained: true, customChromaAttributes: true, scriptNodes: 0, cspViolations: 0});
  } catch (error) {Object.assign(report, {passed: false, error: String(error), violations, failures});}
  document.title = report.passed ? 'WHIP Safari smoke passed' : 'WHIP Safari smoke failed';
  await fetch('/result', {method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify(report)});
}

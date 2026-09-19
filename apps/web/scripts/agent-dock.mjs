import assert from 'node:assert/strict';
import { join } from 'node:path';
import { expect } from '@playwright/test';

// Production renderer and existing stopped REPL children only. No provider runs,
// fabricated agent events, or additional transcript subscriptions are needed.
export async function checkAgentDock({ page, client, root, frames, directory, name }) {
  const child = 'repl-child';
  const snapshot = await client.session(root).snapshot();
  assert.ok(snapshot.agents.some(agent => agent.id === child && agent.parent_id === root));
  const children = new Set(snapshot.agents.filter(agent => agent.parent_id === root).map(agent => agent.id));
  const childReads = () => frames.filter(frame => frame.method === 'history.page' && frame.params?.agent_id === child);
  const rosterReads = () => frames.filter(frame => frame.method === 'history.page' && children.has(frame.params?.agent_id));
  const screenshot = label => page.screenshot({ path: join(directory, `${name}-agent-dock-${label}.png`) });
  const rootInput = page.getByRole('textbox', { name: 'Message WHIP', exact: true });
  const childInput = page.getByRole('textbox', { name: 'Message this agent', exact: true });
  const rootPanel = rootInput.locator('xpath=ancestor::section[@data-workspace-view]');
  const childPanel = childInput.locator('xpath=ancestor::section[@data-workspace-view]');
  const dock = rootPanel.getByRole('region', { name: 'Session agents', exact: true });
  const row = dock.locator(`[data-agent-dock-row="${child}"]`);
  const disclosure = dock.getByRole('button', { name: /^Agents/ });
  const form = rootInput.locator('xpath=ancestor::form');
  const preview = form.getByRole('button', { name: 'Preview agent-dock.png', exact: true });
  const tabs = page.locator('[data-workspace-tab]');
  const reading = rootPanel.getByRole('region', { name: 'Conversation', exact: true });
  const latest = rootPanel.getByRole('button', { name: 'Latest', exact: true });
  const readingChecks = [];
  const settle = () => page.waitForTimeout(250);
  const alignedRows = async () => {
    const divider = dock.locator('[data-agent-dock-content]');
    await expect(divider).toHaveCSS('border-top-width', '1px');
    const border = await divider.boundingBox();
    const composer = await form.evaluate(element => {
      const bounds = element.getBoundingClientRect();
      const style = getComputedStyle(element);
      return { left: bounds.left + parseFloat(style.paddingLeft), right: bounds.right - parseFloat(style.paddingRight) };
    });
    assert.ok(Math.abs(border.x - composer.left) < 1, 'Divider starts at the composer edge');
    assert.ok(Math.abs(border.x + border.width - composer.right) < 1, 'Divider ends at the composer edge');
    const rows = dock.locator('[data-agent-dock-rows]');
    if (await rows.count()) {
      const viewport = await rows.boundingBox();
      const dockBounds = await dock.boundingBox();
      assert.ok(Math.abs(viewport.y + viewport.height - dockBounds.y - dockBounds.height) < 1,
        'No empty strip clips rows at the bottom of the dock');
      // Pending requests or the explicit narrow-pane fallback may sit between them.
      if (await dock.evaluate(element => element.nextElementSibling?.tagName === 'FORM')) {
        const composerBounds = await form.boundingBox();
        assert.ok(Math.abs(viewport.y + viewport.height - composerBounds.y) < 1,
          'Agent rows scroll all the way to the composer without a clipping gap');
      }
    }
    await expect(disclosure).toHaveCSS('display', 'flex');
    await expect(disclosure.locator('span').last()).toHaveCSS('white-space', 'nowrap');
    const bounds = await dock.locator('[data-agent-dock-row]').evaluateAll(rows => rows.map(row => {
      const rect = row.getBoundingClientRect();
      const time = row.querySelector('[data-agent-dock-duration]').getBoundingClientRect();
      return { right: time.right, rowRight: rect.right };
    }));
    for (const rect of bounds) {
      assert.ok(Math.abs(rect.right - bounds[0].right) < 1, 'Durations share a right-aligned column');
      assert.ok(rect.right <= rect.rowRight, 'Duration stays inside the row');
    }
  };
  const atEnd = () => expect.poll(() => reading.evaluate(element => element.scrollHeight - element.clientHeight - element.scrollTop)).toBeLessThanOrEqual(2);
  const anchor = () => reading.evaluate(element => {
    const viewport = element.getBoundingClientRect();
    const row = [...element.querySelectorAll('[data-reading-id]')].find(row => {
      const rect = row.getBoundingClientRect();
      return rect.bottom > viewport.top && rect.top < viewport.bottom;
    });
    return row ? { id: row.dataset.readingId, offset: row.getBoundingClientRect().top - viewport.top } : null;
  });
  const assertAnchor = async (before, label) => {
    await settle();
    const after = await reading.evaluate((element, id) => {
      const row = [...element.querySelectorAll('[data-reading-id]')].find(row => row.dataset.readingId === id);
      return row ? { id, offset: row.getBoundingClientRect().top - element.getBoundingClientRect().top } : null;
    }, before.id);
    assert.ok(after, `${label}: reading anchor must remain mounted`);
    const drift = Math.abs(after.offset - before.offset);
    assert.ok(drift <= 2, `${label}: reading anchor moved ${drift}px`);
    readingChecks.push({ label, drift });
  };
  const draft = 'Keep this root draft while I inspect delegated work.';
  const childDraft = 'This draft belongs only to the child.';
  const initialTabs = await tabs.count();
  const rootView = await rootPanel.getAttribute('data-workspace-view');
  const rootPane = await rootPanel.getAttribute('data-workspace-pane');
  await expect(dock).toBeVisible();
  await alignedRows();
  await expect(disclosure).toHaveAttribute('aria-expanded', 'false');
  await expect(row).toHaveCount(0);
  assert.equal(rosterReads().length, 0, 'Mounting the dock must not read child history');
  await rootInput.fill(draft);
  const png = await page.evaluate(() => {
    const canvas = document.createElement('canvas'); canvas.width = 80; canvas.height = 60;
    const ctx = canvas.getContext('2d'); ctx.fillStyle = '#456347'; ctx.fillRect(0, 0, 80, 60);
    return canvas.toDataURL('image/png').split(',')[1];
  });
  await form.locator('input[type=file]').setInputFiles({ name: 'agent-dock.png', mimeType: 'image/png', buffer: Buffer.from(png, 'base64') });
  await expect(preview).toBeVisible();
  await expect(form.getByRole('button', { name: 'Send message', exact: true })).toBeEnabled();
  const previewSource = await preview.locator('img').getAttribute('src');
  const preserveRoot = async () => {
    await expect(rootInput).toHaveValue(draft);
    await expect(preview).toBeVisible();
    await expect(preview.locator('img')).toHaveAttribute('src', previewSource);
    await expect(rootPanel).toHaveAttribute('data-workspace-view', rootView);
    await expect(rootPanel).toHaveAttribute('data-workspace-pane', rootPane);
  };
  const bounded = async () => {
    await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    for (const input of [rootInput, childInput]) {
      if (await input.isVisible()) await expect(input).toBeInViewport();
    }
    assert.ok(await dock.evaluate(element => element.scrollWidth <= element.clientWidth + 1), 'Dock must stay within its pane');
  };
  const rightSplit = async () => {
    await expect(rootInput).toBeVisible();
    await expect(childInput).toBeVisible();
    await expect(tabs).toHaveCount(initialTabs + 1);
    assert.notEqual(await childPanel.getAttribute('data-workspace-pane'), rootPane);
    await expect.poll(async () => {
      const left = await rootPanel.boundingBox(), right = await childPanel.boundingBox();
      return !!left && !!right && right.x >= left.x + left.width - 1;
    }).toBe(true);
    await preserveRoot();
    await bounded();
  };
  const closeChild = async () => {
    const id = await childPanel.getAttribute('data-workspace-view');
    await page.locator(`[data-workspace-tab="${id}"]`).getByRole('button', { name: /^Close / }).click();
    await expect(childInput).toHaveCount(0);
    await expect(tabs).toHaveCount(initialTabs);
    await preserveRoot();
  };

  if (await latest.count()) await latest.click();
  await atEnd();
  await disclosure.hover();
  await expect(disclosure).toHaveCSS('background-color', 'rgba(0, 0, 0, 0)');
  await expect(disclosure).toHaveCSS('border-top-width', '0px');
  await disclosure.click();
  await expect(disclosure).toHaveCSS('background-color', 'rgba(0, 0, 0, 0)');
  assert.equal(await disclosure.evaluate(element => element.matches(':focus-visible')), false,
    'Pointer clicks do not leave a keyboard focus ring on the plain disclosure');
  await disclosure.click();
  await screenshot('finished-collapsed');
  await disclosure.focus();
  await page.keyboard.press('Tab');
  await page.keyboard.press('Shift+Tab');
  await expect(disclosure).toBeFocused();
  await page.keyboard.press('Enter');
  await expect(disclosure).toHaveAttribute('aria-expanded', 'true');
  assert.ok(await disclosure.evaluate(element => parseFloat(getComputedStyle(element).outlineWidth) > 0
    && getComputedStyle(element).outlineStyle !== 'none'), 'Keyboard focus remains visible');
  assert.equal(await disclosure.evaluate(element => element.matches(':focus-visible')), true);
  await expect(row).toBeVisible();
  assert.equal(rosterReads().length, 0, 'Expanding the roster must remain metadata-only');
  await atEnd();
  readingChecks.push({ label: 'Roster expansion preserves end-following' });
  await screenshot('finished-expanded');
  await disclosure.click();
  await atEnd();
  await reading.hover(); await page.mouse.wheel(0, -400);
  await expect(latest).toBeVisible();
  await settle();
  const beforeDisclosure = await anchor();
  assert.ok(beforeDisclosure, 'Existing fixture must provide a visible reading anchor');
  await disclosure.click();
  await assertAnchor(beforeDisclosure, 'Roster expansion while reading');
  await disclosure.click();
  await assertAnchor(beforeDisclosure, 'Roster collapse while reading');
  await disclosure.click();
  await assertAnchor(beforeDisclosure, 'Roster re-expansion while reading');
  const beforeSplit = await anchor();
  assert.equal(rosterReads().length, 0, 'Dock interactions must not eagerly read any direct child');
  await row.click();
  await rightSplit();
  await assertAnchor(beforeSplit, 'Opening child split while reading');
  await expect.poll(() => childReads().length).toBeGreaterThan(0);
  await expect(childInput).toHaveValue('');
  await childInput.fill(childDraft);
  await preserveRoot();
  const childView = await childPanel.getAttribute('data-workspace-view');
  await row.click();
  await rightSplit();
  await expect(childPanel).toHaveAttribute('data-workspace-view', childView);
  await expect(childInput).toHaveValue(childDraft);
  // Selection must not expand the disclosure implicitly.
  await expect(row).toHaveCount(1);
  await expect(row).toHaveAttribute('aria-current', 'true');
  await expect(dock.locator('[data-agent-dock-content]')).toHaveCSS('border-top-width', '1px');
  await expect(dock.locator('.lucide-columns2')).toHaveCount(0);
  await expect(dock.getByRole('button', { name: 'All agents', exact: true })).toHaveCount(0);
  const rowColumns = await row.evaluate(element => {
    const name = element.children[1].getBoundingClientRect();
    const status = element.querySelector('[data-agent-dock-status]').getBoundingClientRect();
    const calls = element.querySelector('[data-agent-dock-calls]').getBoundingClientRect();
    const time = element.querySelector('[data-agent-dock-duration]').getBoundingClientRect();
    return { name: name.toJSON(), status: status.toJSON(), calls: calls.toJSON(), time: time.toJSON() };
  });
  if (rowColumns.status.y > rowColumns.name.y + 5) {
    assert.ok(Math.abs(rowColumns.status.x - rowColumns.name.x) < 1, 'Narrow status aligns below the name');
    assert.ok(Math.abs(rowColumns.calls.right - rowColumns.time.right) < 1, 'Narrow calls align below duration');
  } else {
    assert.ok(rowColumns.status.x >= rowColumns.name.right, 'Wide status occupies the middle column');
    assert.ok(rowColumns.time.x >= rowColumns.calls.right, 'Wide duration follows model calls');
  }
  await alignedRows();
  const rowSurface = await row.evaluate(element => ({
    background: getComputedStyle(element).backgroundColor,
    radius: getComputedStyle(element).borderRadius,
  }));
  assert.notEqual(rowSurface.background, 'rgba(0, 0, 0, 0)');
  assert.equal(rowSurface.radius, '6px');
  await screenshot('right-split');
  await disclosure.click();
  await expect(row).toHaveCount(0);
  await rootPanel.locator(`[data-inline-agent="${child}"]`).getByRole('button', { name: /^Launched / }).click();
  await rightSplit();
  await expect(disclosure).toHaveAttribute('aria-expanded', 'false');
  await expect(row).toHaveCount(0);
  await expect(childPanel).toHaveAttribute('data-workspace-view', childView);
  await disclosure.click();

  // The compact chronological launch record takes the identical route.
  const inline = rootPanel.locator(`[data-inline-agent="${child}"]`);
  await expect(inline).toHaveCount(1);
  await expect(inline).toContainText('Launched');
  const launch = inline.getByRole('button', { name: /^Launched / });
  await launch.click();
  await rightSplit();
  await expect(childPanel).toHaveAttribute('data-workspace-view', childView);
  await closeChild();
  await launch.click();
  await rightSplit();
  await expect(childInput).toHaveValue(childDraft);
  await screenshot('inline-split');

  for (const width of [1440, 1100, 1280]) {
    await page.setViewportSize({ width, height: 900 });
    await rightSplit();
  }
  await closeChild();
  await page.emulateMedia({ reducedMotion: 'reduce' });
  for (const width of [390, 320]) {
    await page.setViewportSize({ width, height: 844 });
    await preserveRoot();
    if (!(await row.isVisible())) await disclosure.click();
    await row.click();
    await expect(rootInput).toBeVisible();
    await expect(childInput).toHaveCount(0);
    await expect(page.locator('[data-workspace-view]')).toHaveCount(1);
    const fallback = rootPanel.getByRole('button', { name: 'Open in tab', exact: true });
    await expect(fallback).toBeVisible();
    await bounded();
    await alignedRows();
    await screenshot(`narrow-${width}-explicit-fallback`);
    await fallback.click();
    await expect(childInput).toBeVisible();
    await expect(childInput).toHaveValue(childDraft);
    await expect(childPanel).toHaveAttribute('data-workspace-pane', rootPane);
    await screenshot(`narrow-${width}-child-tab`);
    // Compact chrome omits the desktop tab strip. Widen before closing so we
    // also prove explicit fallback did not leave a hidden split behind.
    await page.setViewportSize({ width: 1280, height: 900 });
    await expect(tabs).toHaveCount(initialTabs + 1);
    await expect(page.locator('[data-workspace-view]')).toHaveCount(1);
    await expect(childPanel).toHaveAttribute('data-workspace-pane', rootPane);
    await closeChild();
  }
  await page.setViewportSize({ width: 1280, height: 900 });
  await preserveRoot();
  await bounded();
  await expect(page.locator('[data-workspace-view]')).toHaveCount(1);
  await screenshot('restored-wide');
  for (const appearance of [
    { theme: 'light', uiSize: 20, codeSize: 24, width: 320 },
    { theme: 'claude-code', uiSize: 12, codeSize: 10, width: 1280 },
  ]) {
    await page.evaluate(value => {
      localStorage.setItem('whip.appearance.theme.v1', JSON.stringify({ version: 1, id: value.theme }));
      localStorage.setItem('whip.appearance.display.v1', JSON.stringify({ version: 1, display: {
        uiFont: 'system', codeFont: 'system', uiSize: value.uiSize, codeSize: value.codeSize,
        wrapCode: true, contrast: 'more', motion: 'reduce',
      } }));
    }, appearance);
    await page.setViewportSize({ width: appearance.width, height: 900 });
    await page.reload();
    await expect(disclosure).toHaveAttribute('aria-expanded', 'false');
    await bounded();
    await screenshot(`${appearance.theme}-collapsed`);
    await disclosure.click();
    await expect(row).toBeVisible();
    await bounded();
    await alignedRows();
    await screenshot(`${appearance.theme}-expanded`);
    assert.deepEqual(await page.evaluate(() => window.cspErrors), []);
  }
  await page.emulateMedia({ reducedMotion: 'no-preference' });
  assert.deepEqual(await page.evaluate(() => window.cspErrors), []);
  return {
    childHistoryReads: childReads().length, readingChecks,
    checks: ['single-line summary and keyboard disclosure', 'no eager child transcript reads',
      'right split preserves root draft and attachment', 'isolated root and child composers',
      'repeated click reuses child view', 'inline launch shares split routing',
      'wide split resize', '390/320px explicit tab fallback without hidden split',
      'drafts survive child tab closure', 'light/dark and 320px large type', 'reduced motion and CSP'],
  };
}

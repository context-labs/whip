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
  const finished = dock.getByRole('button', { name: /^Finished \(/ });
  const form = rootInput.locator('xpath=ancestor::form');
  const preview = form.getByRole('button', { name: 'Preview agent-dock.png', exact: true });
  const tabs = page.locator('[data-workspace-tab]');
  const reading = rootPanel.getByRole('region', { name: 'Conversation', exact: true });
  const latest = rootPanel.getByRole('button', { name: 'Latest', exact: true });
  const readingChecks = [];
  const settle = () => page.waitForTimeout(250);
  const alignedActions = async () => {
    const footer = dock.locator('[data-agent-dock-actions]');
    await expect(footer).toHaveCSS('display', 'flex');
    await expect(footer).toHaveCSS('align-items', 'center');
    const bounds = await footer.locator('button').evaluateAll(buttons => buttons.map(button => {
      const rect = button.getBoundingClientRect();
      return { y: rect.y, height: rect.height };
    }));
    assert.ok(bounds.length > 0);
    for (const rect of bounds) {
      assert.ok(Math.abs(rect.y - bounds[0].y) < 1, 'Footer buttons share a horizontal row');
      assert.ok(Math.abs(rect.height - bounds[0].height) < 1, 'Footer buttons have matching heights');
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
  await alignedActions();
  await expect(finished).toHaveAttribute('aria-expanded', 'false');
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
  await screenshot('finished-collapsed');
  await finished.focus(); await page.keyboard.press('Enter');
  await expect(finished).toHaveAttribute('aria-expanded', 'true');
  await expect(row).toBeVisible();
  assert.equal(rosterReads().length, 0, 'Expanding Finished must remain metadata-only');
  await atEnd();
  readingChecks.push({ label: 'Finished expansion preserves end-following' });
  await screenshot('finished-expanded');
  await finished.click();
  await atEnd();
  await reading.hover(); await page.mouse.wheel(0, -400);
  await expect(latest).toBeVisible();
  await settle();
  const beforeDisclosure = await anchor();
  assert.ok(beforeDisclosure, 'Existing fixture must provide a visible reading anchor');
  await finished.click();
  await assertAnchor(beforeDisclosure, 'Finished expansion while reading');
  await finished.click();
  await assertAnchor(beforeDisclosure, 'Finished collapse while reading');
  await finished.click();
  await assertAnchor(beforeDisclosure, 'Finished re-expansion while reading');
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
  // Selection must not pull settled work out of Finished or expand it implicitly.
  await expect(dock.getByLabel('Finished agents').locator(`[data-agent-dock-row="${child}"]`)).toHaveCount(1);
  await expect(row).toHaveAttribute('aria-current', 'true');
  assert.equal(await dock.evaluate(element => getComputedStyle(element).borderTopWidth), '1px');
  await expect(dock.locator('.lucide-columns2')).toHaveCount(0);
  await expect(dock.getByRole('button', { name: 'All agents', exact: true })).toHaveCount(0);
  const statusPosition = await row.evaluate(element => {
    const bounds = element.getBoundingClientRect();
    const status = element.querySelector('[data-agent-dock-status]').getBoundingClientRect();
    return (status.x - bounds.x) / bounds.width;
  });
  assert.ok(statusPosition > 0.3 && statusPosition < 0.5, 'Status occupies the middle column');
  const callPosition = await row.evaluate(element => {
    const bounds = element.getBoundingClientRect();
    const calls = element.querySelector('[data-agent-dock-calls]').getBoundingClientRect();
    return (calls.right - bounds.x) / bounds.width;
  });
  assert.ok(callPosition > 0.9 && callPosition <= 1, 'Call count occupies the right column');
  const rowSurface = await row.evaluate(element => ({
    background: getComputedStyle(element).backgroundColor,
    radius: getComputedStyle(element).borderRadius,
  }));
  assert.notEqual(rowSurface.background, 'rgba(0, 0, 0, 0)');
  assert.equal(rowSurface.radius, '6px');
  await screenshot('right-split');
  await finished.click();
  await expect(row).toHaveCount(0);
  await rootPanel.locator(`[data-inline-agent="${child}"]`).getByRole('button', { name: /^Launched / }).click();
  await rightSplit();
  await expect(finished).toHaveAttribute('aria-expanded', 'false');
  await expect(row).toHaveCount(0);
  await expect(childPanel).toHaveAttribute('data-workspace-view', childView);
  await finished.click();

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
    if (!(await row.isVisible())) await finished.click();
    await row.click();
    await expect(rootInput).toBeVisible();
    await expect(childInput).toHaveCount(0);
    await expect(page.locator('[data-workspace-view]')).toHaveCount(1);
    const fallback = rootPanel.getByRole('button', { name: 'Open in tab', exact: true });
    await expect(fallback).toBeVisible();
    await bounded();
    await alignedActions();
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
  await page.emulateMedia({ reducedMotion: 'no-preference' });
  assert.deepEqual(await page.evaluate(() => window.cspErrors), []);
  return {
    childHistoryReads: childReads().length, readingChecks,
    checks: ['finished collapsed and keyboard disclosure', 'no eager child transcript reads',
      'right split preserves root draft and attachment', 'isolated root and child composers',
      'repeated click reuses child view', 'inline launch shares split routing',
      'wide split resize', '390/320px explicit tab fallback without hidden split',
      'drafts survive child tab closure', 'reduced motion and CSP'],
  };
}

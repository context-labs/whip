// The staged desktop owns a disposable local host, then uses the ordinary URL
// connection UI for the distinct test gateway. This keeps WebSocket-only fault
// probes honest; it does not pretend they intercept the local IPC transport.
import assert from 'node:assert/strict';
import { expect } from '@playwright/test';
import { startFixture } from './native-fixture.mjs';
import { isolateDesktopPerformance, launchDesktopPerformance, finishDesktopPerformance } from './performance-desktop.mjs';

export async function openDesktopRemote(fixture) {
  let local, isolation, host;
  try {
    local = await startFixture({ managedDirectory: true, lifetimeMs: 900000 });
    isolation = await isolateDesktopPerformance();
    host = await launchDesktopPerformance(local, isolation);
    assert.notEqual(local.info.runtime_id, fixture.info.runtime_id);
    const { page } = host;
    await page.setViewportSize({ width: 1280, height: 900 });
    await page.locator('#whip-session-navigation').getByRole('button', { name: 'Manage servers', exact: true }).click();
    await page.getByRole('button', { name: 'Add server', exact: true }).click();
    const dialog = page.getByRole('dialog', { name: 'Add server', exact: true });
    await dialog.getByRole('textbox', { name: /Server name/ }).fill('Owned activity gateway');
    await dialog.getByRole('textbox', { name: 'Server address', exact: true }).fill(fixture.info.web);
    await dialog.getByRole('button', { name: 'Connect', exact: true }).click();
    await expect(dialog).toBeHidden();
    return { ...host, close: succeeded => finishDesktopPerformance(isolation, host, local, succeeded) };
  } catch (error) {
    if (isolation) await finishDesktopPerformance(isolation, host, local, false, [error]);
    else await local?.close();
    throw error;
  }
}

import assert from 'node:assert/strict';
import { mkdir, writeFile } from 'node:fs/promises';
import { join } from 'node:path';
import { startFixture } from './native-fixture.mjs';
import { startSafariDriver } from './native-safari-driver.mjs';
import { safariWorkflows } from './native-safari-workflows.mjs';

const directory = process.env.WHIP_WEB_BROWSER_RESULTS ?? '/tmp/whip-native-safari';
await mkdir(directory, { recursive: true });
const report = { browser: 'actual Safari', checks: [], status: 'pending' };
let driver, fixture;
try {
  // Refuse honestly before building/starting a runtime if the machine has not
  // opted into Safari Remote Automation. Do not change any browser setting.
  driver = await startSafariDriver(); report.capabilities = driver.capabilities;
  fixture = await startFixture({ lifetimeMs: 180000 });
  await safariWorkflows(driver, fixture, directory, report);
  assert(/Safari\//.test(report.userAgent) && !/Chrome\//.test(report.userAgent));
  report.status = 'passed';
} catch (error) {
  report.status = 'failed'; report.error = String(error.stack ?? error);
  if (/Remote Automation|not enabled|not authorized|Allow remote automation/i.test(report.error)) report.prerequisite = 'Enable Safari Remote Automation explicitly on the test machine, then rerun. This probe did not change that setting.';
  if (driver) report.page = await driver.evaluate(() => document.body.innerText.slice(0, 32768)).catch(() => null);
  process.exitCode = 1; console.error(error);
} finally {
  for (const close of [() => driver?.close(), () => fixture?.close()]) {
    try { await close(); } catch (error) { report.status = 'failed'; (report.cleanupErrors ??= []).push(String(error.stack ?? error).slice(0, 4096)); process.exitCode = 1; }
  }
  await writeFile(join(directory, 'safari-report.json'), JSON.stringify(report, null, 2));
}
console.log(JSON.stringify(report, null, 2));

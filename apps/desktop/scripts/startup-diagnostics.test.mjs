import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import test from 'node:test';
import { captureStartupFailure, startupDiagnostic } from './startup-diagnostics.mjs';

const evidence = { source: { commit: 'a'.repeat(40), dirty: false }, rendererDigest: 'b'.repeat(64), signed: true, notarized: true };
const report = { state: 'timeout', target: 'home', rendererDigest: evidence.rendererDigest, windowCreatedMs: 100, domReadyMs: 150,
  finishedMs: 30_100, shell: { lowerMs: 160, upperMs: 165 }, checks: { host: true, home: false, noNotice: true, fonts: true, painted: false },
  instrumentation: { probes: 900, pollIntervalMs: 25, paintWaitBoundMs: 250, wallMs: 1000, maxWallMs: 4 } };
const failure = () => ({ scenario: 'first-launch', warmup: false, state: 'runner-failed', lastReport: report, launchExit: { code: 0, signal: null } });

test('retains only bounded startup/source/health fields from the last attempt', () => {
  const result = JSON.parse(startupDiagnostic({ completed: false, evidence, archiveSHA256: 'c'.repeat(64), results: [report, failure()] }));
  assert.deepEqual(result.source, { commit: 'a'.repeat(40), dirty: false, rendererDigest: 'b'.repeat(64), archiveSHA256: 'c'.repeat(64), signed: true, notarized: true });
  assert.equal(result.recordsObserved, 2);
  assert.deepEqual(result.lastAttempt, { scenario: 'first-launch', warmup: false, state: 'runner-failed', probeState: 'timeout', target: 'home', rendererDigestMatches: true,
    timings: { windowCreatedMs: 100, domReadyMs: 150, finishedMs: 30_100, shell: { lowerMs: 160, upperMs: 165 }, connected: {}, usable: {} },
    checks: report.checks, instrumentation: report.instrumentation, daemon: {}, launchServices: { code: 0, signaled: false, spawnFailed: false } });
});
test('never serializes arbitrary strings, private paths, configuration, transcripts, environment, errors or nested extras', () => {
  const secret = '/private/fixture/token-and-transcript-canary';
  const polluted = { ...report, target: secret, state: secret, rendererDigest: secret, domReadyMs: secret, windowCreatedMs: -1, finishedMs: Infinity,
    shell: { lowerMs: NaN, upperMs: 3_600_001, extra: secret }, checks: { host: secret, transcript: secret, extra: secret },
    instrumentation: { probes: secret, wallMs: Infinity }, config: { key: secret }, environment: { HOME: secret }, messages: [secret], stack: secret };
  const last = { ...failure(), scenario: secret, warmup: secret, state: secret, lastReport: polluted, launchError: secret,
    daemon: { state: secret, privateSocketVerified: secret, socket: secret }, launchExit: { code: secret, signal: secret, error: secret } };
  const text = startupDiagnostic({ completed: secret, evidence: { source: { commit: secret, dirty: secret }, rendererDigest: secret, bundle: secret, environment: secret },
    archiveSHA256: secret, results: new Array(10_000).fill(last) });
  assert(Buffer.byteLength(text) <= 8192);
  assert(!text.includes(secret));
  const result = JSON.parse(text);
  assert.deepEqual(result.source, {});
  assert.deepEqual(result.lastAttempt.checks, {});
  assert.deepEqual(result.lastAttempt.timings, { shell: {}, connected: {}, usable: {} });
  assert.deepEqual(result.lastAttempt.daemon, {});
  assert.deepEqual(result.lastAttempt.launchServices, { signaled: true, spawnFailed: true });
  assert.equal(result.recordsObserved, 10_000);
  assert.equal(result.completed, undefined);
});
test('captures fixture health and reports before owned cleanup without replacing the original failure', async () => {
  const order = []; const original = new Error('acceptance failed'); const failed = failure(); let text;
  await assert.rejects(async () => {
    try { throw original; }
    catch (error) {
      await captureStartupFailure(failed, async () => { order.push('status'); return { state: 'running', socket: '/private/socket', pid: 123, token: 'secret' }; }, async () => {
        order.push('report');
        text = startupDiagnostic({ evidence, results: [failed] });
        throw new Error('diagnostic write failed');
      });
      throw error;
    } finally { order.push('cleanup'); }
  }, error => error === original);
  assert.deepEqual(order, ['status', 'report', 'cleanup']);
  assert.deepEqual(failed.daemon, { state: 'running', privateSocketVerified: true });
  assert(!text.includes('/private/socket') && !text.includes('secret'));
});
test('failed fixture status becomes unavailable without leaking its error or preventing the report', async () => {
  const failed = failure(); let calls = 0; let text;
  await captureStartupFailure(failed, async () => { calls++; throw new Error('private credential'); }, () => {
    calls++;
    text = startupDiagnostic({ results: [failed] });
  });
  assert.equal(calls, 2);
  assert.deepEqual(failed.daemon, { state: 'unavailable', privateSocketVerified: false });
  assert(!text.includes('private credential'));
});
test('passing diagnostics need no health call and omit unsupported input', () => {
  const text = startupDiagnostic({ completed: true, evidence, results: [{ ...report, state: 'complete' }] });
  assert.equal(JSON.parse(text).completed, true);
  assert.deepEqual(JSON.parse(text).lastAttempt.daemon, {});
  assert.equal(JSON.parse(startupDiagnostic({})).schema, 1);
});
test('only the minimized sidecar uploads on failure; release payload remains success-only', async () => {
  const workflow = await readFile(new URL('../../../.github/workflows/desktop-release.yml', import.meta.url), 'utf8');
  const diagnosticStep = workflow.split('      - name: Sanitized startup diagnostics (including failures)')[1].split('      - name:')[0];
  assert(diagnosticStep.includes('if: always()'));
  assert(diagnosticStep.includes('path: apps/desktop/out/diagnostics/startup.json'));
  assert(diagnosticStep.includes('retention-days: 7'));
  const payloadStep = workflow.split('      - uses: actions/upload-artifact@')[1].split('      - name:')[0];
  assert(payloadStep.includes('name: desktop-release-arm64'));
  assert(!payloadStep.includes('if:'));
  const runner = await readFile(new URL('./startup.mjs', import.meta.url), 'utf8');
  assert(runner.indexOf('await captureStartupFailure(') < runner.indexOf('const stillOwned ='));
});

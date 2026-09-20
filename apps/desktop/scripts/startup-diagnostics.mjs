// Public failure evidence is a fixed-schema projection, never a copy/redaction of
// renderer reports, daemon status, errors, fixture environment or release evidence.
const choice = (value, allowed) => allowed.includes(value) ? value : undefined;
const flag = value => typeof value === 'boolean' ? value : undefined;
const number = value => Number.isFinite(value) && value >= 0 && value <= 3_600_000 ? value : undefined;
const hash = (value, length) => typeof value === 'string' && new RegExp(`^[a-f0-9]{${length}}$`).test(value) ? value : undefined;
const numbers = (value, keys) => Object.fromEntries(keys.map(key => [key, number(value?.[key])]));
const states = ['collecting', 'complete', 'timeout', 'renderer-failed', 'closed', 'unexpected-origin', 'runner-failed', 'sampling'];

export function startupDiagnostic({ completed, evidence = {}, archiveSHA256, results = [] }) {
  // One last attempt is sufficient for failure triage and bounds output regardless
  // of sample count. No PIDs, paths, route/session IDs or arbitrary strings escape.
  const last = results.at(-1) ?? {};
  const probe = last.lastReport ?? last;
  const expectedDigest = hash(evidence.rendererDigest, 64);
  const observedDigest = hash(probe.rendererDigest, 64);
  const exit = last.launchExit ?? probe.launchServicesExit;
  const diagnostic = {
    schema: 1, completed: flag(completed), recordsObserved: number(results.length),
    source: { commit: hash(evidence.source?.commit, 40), dirty: flag(evidence.source?.dirty),
      rendererDigest: expectedDigest, archiveSHA256: hash(archiveSHA256, 64), signed: flag(evidence.signed), notarized: flag(evidence.notarized) },
    lastAttempt: {
      scenario: choice(last.scenario, ['first-launch', 'prepare', 'prepare-session', 'warm-attach', 'retained-start', 'idle']),
      warmup: flag(last.warmup), state: choice(last.state, states),
      probeState: choice(probe.state, states), target: choice(probe.target, ['home', 'retained-session']),
      rendererDigestMatches: expectedDigest && observedDigest ? expectedDigest === observedDigest : undefined,
      timings: { ...numbers(probe, ['windowCreatedMs', 'domReadyMs', 'finishedMs', 'receivedMs']),
        shell: numbers(probe.shell, ['lowerMs', 'upperMs']), connected: numbers(probe.connected, ['lowerMs', 'upperMs']), usable: numbers(probe.usable, ['lowerMs', 'upperMs']) },
      checks: Object.fromEntries(['host', 'noNotice', 'home', 'session', 'transcript', 'visible', 'fonts', 'painted'].map(key => [key, flag(probe.checks?.[key])])),
      instrumentation: numbers(probe.instrumentation, ['pollIntervalMs', 'paintWaitBoundMs', 'probes', 'wallMs', 'maxWallMs']),
      daemon: { state: choice(last.daemon?.state, ['running', 'stopped', 'unavailable']), privateSocketVerified: flag(last.daemon?.privateSocketVerified) },
      launchServices: exit ? { code: Number.isInteger(exit.code) && exit.code >= 0 && exit.code <= 255 ? exit.code : undefined,
        signaled: !!exit.signal, spawnFailed: !!exit.error } : undefined,
    },
  };
  const text = JSON.stringify(diagnostic) + '\n';
  if (Buffer.byteLength(text) > 8192) throw new Error('Startup diagnostic exceeds its limit');
  return text;
}

// Called only on failure, before owned GUI/daemon cleanup. readStatus must use
// fixtureStatus's private socket verification and existing bounded command runner.
export async function captureStartupFailure(failure, readStatus, report) {
  try {
    const status = await readStatus();
    failure.daemon = { state: choice(status.state, ['running', 'stopped']), privateSocketVerified: true };
  } catch {
    failure.daemon = { state: 'unavailable', privateSocketVerified: false };
  }
  // Diagnostics must not replace the original acceptance error, even if logging fails.
  try { await report(); } catch { /* original failure is rethrown by the caller */ }
}

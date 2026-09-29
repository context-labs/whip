/** Interpret the runtime's execute-result envelope without altering its raw text.
 * Unknown/unsafe numeric values stay available verbatim rather than being rounded. */
function object(value: unknown): value is Record<string, unknown> { return !!value && typeof value === 'object' && !Array.isArray(value); }
export function safeJSON(text: string): unknown {
  return JSON.parse(text, (_key, value) => {
    if (typeof value === 'number' && (!Number.isFinite(value) || Number.isInteger(value) && !Number.isSafeInteger(value))) throw new Error('JSON number needs an exact reader');
    return value;
  });
}
export function executionOutput(raw: string) {
  const fallback = { output: raw, value: undefined as string | undefined, error: undefined as string | undefined, steps: undefined as number | undefined, jobs: undefined as number | undefined, warning: undefined as string | undefined, restored: undefined as string | undefined };
  try {
    const envelope = safeJSON(raw);
    if (!object(envelope)) return fallback;
    const error = typeof envelope.error === 'string' ? envelope.error : undefined;
    const result = envelope.result;
    if (!object(result)) return { ...fallback, error };
    if (result.execution_engine !== 'starlark' && result.execution_engine !== 'quickjs') return fallback;
    const metrics = object(result.metrics) ? result.metrics : {};
    const scratch = object(result.scratch) ? result.scratch : {};
    const restore = object(result.restored) ? result.restored : {};
    const names = Array.isArray(restore.restored) && restore.restored.every(value => typeof value === 'string') ? restore.restored : [];
    return {
      output: typeof result.output === 'string' ? result.output : '',
      value: result.has_value === true ? JSON.stringify(result.value, null, 2) : undefined,
      error,
      steps: result.execution_engine === 'starlark' && typeof result.steps === 'number' ? result.steps : undefined,
      jobs: result.execution_engine === 'quickjs' && typeof metrics.quickjs_jobs === 'number' ? metrics.quickjs_jobs : undefined,
      warning: typeof scratch.warning === 'string' ? scratch.warning : undefined,
      restored: result.restored ? `Worker restarted; restored ${names.length} saved names${Array.isArray(restore.failed) && restore.failed.length ? `, ${restore.failed.length} unavailable` : ''}.` : undefined,
    };
  } catch { return fallback; }
}

export function recordedDuration(start: string, end: string | null): string | undefined {
  if (end === null) return undefined;
  const milliseconds = Date.parse(end) - Date.parse(start);
  if (!Number.isFinite(milliseconds) || milliseconds < 0) return undefined;
  return milliseconds < 1 ? '<1ms' : milliseconds < 1000 ? `${milliseconds}ms` : `${(milliseconds / 1000).toFixed(1)}s`;
}

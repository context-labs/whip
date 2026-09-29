import assert from 'node:assert/strict';

// Serialized into the owned test page. No synthetic events or historical buffer.
export function startKeyboardProbe(selector) {
  if (window.__finishKeyboardProbe) throw new Error('Keyboard probe already active');
  const target = document.querySelector(selector);
  if (!target || document.activeElement !== target || document.visibilityState !== 'visible')
    throw new Error('Visible focused keyboard target required');
  if (!PerformanceObserver.supportedEntryTypes.includes('event') || !performance.eventCounts)
    throw new Error('Native EventTiming/eventCounts required');
  const state = { keys: [], entries: [], phases: [], frames: [], overflow: false, dropped: 0 };
  const initial = performance.eventCounts.get('keydown') ?? 0;
  const retain = (items, value, limit) => { if (items.length >= limit) state.overflow = true; else items.push(value); };
  const key = event => {
    if (event.target === target && event.key === 'x') retain(state.keys, { startTime: event.timeStamp, trusted: event.isTrusted }, 128);
  };
  const input = event => {
    if (event.target !== target) return;
    const started = performance.now();
    requestAnimationFrame(() => retain(state.frames, performance.now() - started, 128));
  };
  const collect = entries => {
    for (const entry of entries) {
      if (!['keydown', 'keypress', 'beforeinput', 'input', 'keyup'].includes(entry.name)) continue;
      const value = { name: entry.name, startTime: entry.startTime, duration: entry.duration,
        processingStart: entry.processingStart, processingEnd: entry.processingEnd, interactionId: entry.interactionId };
      retain(state.phases, value, 512);
      if (entry.name === 'keydown') retain(state.entries, value, 128);
    }
  };
  const observer = new PerformanceObserver((list, _observer, options) => {
    state.dropped += options?.droppedEntriesCount ?? 0;
    collect(list.getEntries());
  });
  observer.observe({ type: 'event', durationThreshold: 16 });
  document.addEventListener('keydown', key, true);
  document.addEventListener('input', input, true);
  window.__finishKeyboardProbe = () => {
    collect(observer.takeRecords()); observer.disconnect();
    document.removeEventListener('keydown', key, true);
    document.removeEventListener('input', input, true);
    delete window.__finishKeyboardProbe;
    return { ...state, browserEventCount: (performance.eventCounts.get('keydown') ?? 0) - initial,
      visible: document.visibilityState === 'visible', focused: document.activeElement === target };
  };
}

export function distribution(samples) {
  const sorted = samples.slice().sort((a, b) => a - b);
  return { count: sorted.length, median: sorted[Math.floor(sorted.length / 2)] ?? null,
    p95: sorted[Math.ceil(sorted.length * 0.95) - 1] ?? null, max: sorted.at(-1) ?? null };
}

export function keyboardResult(raw, count = 40) {
  assert.equal(raw.overflow, false); assert.equal(raw.dropped, 0);
  assert(raw.visible && raw.focused, 'Keyboard target lost visibility/focus');
  assert.equal(raw.keys.length, count); assert.equal(raw.browserEventCount, count);
  assert.equal(raw.frames.length, count);
  assert(raw.keys.every(key => key.trusted), 'Untrusted keyboard input');
  const matched = new Set();
  const durations = raw.keys.map(key => {
    const entries = raw.entries.filter(entry => Math.abs(entry.startTime - key.startTime) <= 0.2);
    assert(entries.length <= 1, 'Ambiguous keyboard entry');
    if (!entries.length) return null;
    const entry = entries[0]; assert(!matched.has(entry), 'Reused keyboard entry'); matched.add(entry);
    assert(Number.isFinite(entry.duration) && entry.duration >= 16 && entry.duration % 8 === 0);
    assert(entry.interactionId > 0);
    assert(Number.isFinite(entry.processingStart) && entry.processingStart >= entry.startTime);
    assert(Number.isFinite(entry.processingEnd) && entry.processingEnd >= entry.processingStart);
    return entry.duration;
  });
  assert(raw.entries.every(entry => matched.has(entry) || entry.startTime < raw.keys[0].startTime - 0.2 ||
    entry.startTime > raw.keys.at(-1).startTime + 0.2), 'Unmatched timing inside measured interval');
  const reported = durations.filter(value => value !== null).sort((a, b) => a - b);
  const missing = count - reported.length, rank = Math.ceil(count * 0.95);
  const upper = distribution(durations.map(value => value === null ? Infinity : value + 4));
  for (const key of ['median', 'p95', 'max']) if (!Number.isFinite(upper[key])) upper[key] = null;
  return { count, reported: reported.length, missing, p95Rounded: missing === 0 ? reported[rank - 1] : null,
    lower: distribution(durations.map(value => value === null ? 0 : value - 4)),
    upper, reportedDistribution: distribution(reported),
    inputToRAF: distribution(raw.frames), raw,
    boundary: 'Native trusted keydown to next rendering completion estimate, rounded to 8ms; not physical display timing. Missing entries may be below the 16ms threshold or delayed despite frame/250ms drainage; they remain unknown (0 to unbounded), never evidence of fast input. A null upper percentile means unknown. Reported entries use duration ±4ms. Input-to-RAF excludes rendering/paint.' };
}

// Opt-in fixture diagnostics only. This observes the existing reader without
// retaining elements, input text, history payloads, or changing scroll/focus.
export function installAnchorTrace({ limit = 256 } = {}) {
  if (!Number.isSafeInteger(limit) || limit < 1 || limit > 512) throw new RangeError('Invalid anchor trace limit');
  if (window.__whipAnchorTrace) throw new Error('Anchor trace already installed');
  const samples = [];
  let discarded = 0, closed = false;
  const boundedID = value => typeof value === 'string' && value.length <= 512 ? value : null;
  const capture = event => {
    if (closed) return;
    const viewport = document.querySelector('[aria-label="Conversation"][role="region"]');
    const bounds = viewport?.getBoundingClientRect();
    let anchor = null, examined = 0, rowsTruncated = false;
    if (viewport) for (const row of viewport.querySelectorAll('[data-reading-id]')) {
      if (++examined > 128) { rowsTruncated = true; break; }
      const rect = row.getBoundingClientRect();
      if (rect.bottom > bounds.top && rect.top < bounds.bottom) {
        anchor = { id: boundedID(row.dataset.readingId), offset: rect.top - bounds.top };
        break;
      }
    }
    let latestVisible = false, controlsTruncated = false, controlsExamined = 0;
    for (const button of viewport?.parentElement?.querySelectorAll('button') ?? []) {
      if (++controlsExamined > 128) { controlsTruncated = true; break; }
      if (button.textContent?.trim() === 'Latest' && button.getClientRects().length) { latestVisible = true; break; }
    }
    const key = event?.type === 'keydown' && ['ArrowUp', 'ArrowDown', 'PageUp', 'PageDown', 'Home', 'End', ' '].includes(event.key) ? event.key : null;
    const sample = { at: performance.now(), type: event?.type ?? 'start', trusted: event?.isTrusted ?? false,
      visibility: document.visibilityState, documentFocused: document.hasFocus(),
      scrollTarget: event?.type === 'scroll' ? event.target === viewport ? 'reader' : 'other' : null,
      view: boundedID(viewport?.closest('[data-workspace-view]')?.dataset.workspaceView),
      top: viewport?.scrollTop ?? null, height: viewport?.clientHeight ?? null, contentHeight: viewport?.scrollHeight ?? null,
      anchor, rowsTruncated, latestVisible, controlsTruncated,
      key, wheelY: event?.type === 'wheel' ? event.deltaY : null,
      pointerType: event?.type === 'pointerdown' ? ['mouse', 'pen', 'touch'].includes(event.pointerType) ? event.pointerType : 'other' : null };
    if (samples.length === limit) { samples.shift(); discarded++; }
    samples.push(sample);
  };
  const events = ['scroll', 'scrollend', 'wheel', 'keydown', 'pointerdown', 'focusin', 'visibilitychange'];
  const close = () => {
    if (closed) return;
    closed = true;
    for (const type of events) document.removeEventListener(type, capture, true);
    window.removeEventListener('popstate', capture);
    window.removeEventListener('pagehide', close);
  };
  window.__whipAnchorTrace = { finish() {
    close(); delete window.__whipAnchorTrace;
    return { samples, discarded, limit, boundary: 'Opt-in passive sequential DOM/event samples during cached switching; layout reads can affect timing. Neither these timings nor memory are performance acceptance. Rows and controls examined per sample <=128 each; IDs <=512 characters; no retained DOM nodes or input text.' };
  } };
  for (const type of events) document.addEventListener(type, capture, { capture: true, passive: true });
  window.addEventListener('popstate', capture);
  window.addEventListener('pagehide', close, { once: true });
  capture();
}

export function finishAnchorTrace() {
  return window.__whipAnchorTrace?.finish() ?? null;
}

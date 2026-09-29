// Fixture-only, passive evidence for the first small upward gesture. Geometry
// reads can affect timing; these samples are not a performance measurement.
export function installActivityScrollTrace({ limit = 256 } = {}) {
  if (!Number.isSafeInteger(limit) || limit < 1 || limit > 512) throw new RangeError('Invalid activity trace limit');
  if (window.__whipActivityScrollTrace) throw new Error('Activity scroll trace already installed');
  let root = document.querySelector('[role="region"][aria-label="Conversation"]');
  if (!root) throw new Error('Conversation is not mounted');
  const samples = [];
  let discarded = 0, closed = false, ended = null, frame = 0, frames = 0;
  const capture = (type, event) => {
    if (closed) return;
    let latest = false, examined = 0, controlsTruncated = false;
    for (const button of root.parentElement?.querySelectorAll('button') ?? []) {
      if (++examined > 128) { controlsTruncated = true; break; }
      if (button.textContent?.trim() === 'Latest' && button.getClientRects().length) { latest = true; break; }
    }
    const nested = [];
    let node = event?.type === 'wheel' ? event.target : null;
    for (; node instanceof Element && node !== root && nested.length < 8; node = node.parentElement) {
      const style = getComputedStyle(node);
      nested.push({ tag: node.tagName.slice(0, 32), top: node.scrollTop, height: node.clientHeight, total: node.scrollHeight,
        scrollable: /auto|scroll/.test(style.overflowY), contained: /contain|none/.test(style.overscrollBehaviorY) });
    }
    const sample = { at: performance.now(), type, top: root.scrollTop, height: root.clientHeight, total: root.scrollHeight,
      width: root.clientWidth, sizerHeight: root.firstElementChild?.firstElementChild?.getBoundingClientRect().height ?? null,
      latest, controlsTruncated, target: event?.target === root ? 'reader' : event ? 'descendant' : null,
      wheelY: event?.type === 'wheel' ? event.deltaY : null, wheelMode: event?.type === 'wheel' ? event.deltaMode : null,
      trusted: event?.isTrusted ?? false, prevented: event?.defaultPrevented ?? false, nested, nestedTruncated: node instanceof Element && node !== root };
    if (samples.length === limit) { samples.shift(); discarded++; }
    samples.push(sample);
  };
  const tick = () => {
    frame = 0;
    if (closed) return;
    if (!root.isConnected) { close('unmounted'); return; }
    capture('frame');
    if (++frames < 8 && !closed) frame = requestAnimationFrame(tick);
  };
  const event = value => {
    capture(value.type, value);
    if (value.type === 'wheel' && !frame && !frames) frame = requestAnimationFrame(tick);
  };
  const bubbled = value => { if (root?.contains(value.target)) capture('wheel-bubbled', value); };
  const observer = new ResizeObserver(() => {
    if (closed) return;
    if (!root.isConnected) { close('unmounted'); return; }
    capture('resize');
  });
  const pagehide = () => close('pagehide');
  const close = reason => {
    if (closed) return;
    closed = true; ended = reason;
    for (const type of ['wheel', 'scroll', 'scrollend']) root.removeEventListener(type, event, true);
    document.removeEventListener('wheel', bubbled);
    window.removeEventListener('pagehide', pagehide);
    observer.disconnect(); cancelAnimationFrame(frame); clearTimeout(timer); root = null;
  };
  const timer = setTimeout(() => close('deadline'), 10_000);
  window.__whipActivityScrollTrace = { finish() {
    capture('finish'); close('finished'); delete window.__whipActivityScrollTrace;
    return { samples, discarded, limit, ended,
      boundary: 'Passive sequential fixture geometry/event samples, not performance acceptance. At most 8 frames after the first wheel; 10s observer lifetime; 128 controls and 8 target ancestors per sample. No retained message/input content or DOM nodes.' };
  } };
  for (const type of ['wheel', 'scroll', 'scrollend']) root.addEventListener(type, event, { capture: true, passive: true });
  document.addEventListener('wheel', bubbled, { passive: true });
  window.addEventListener('pagehide', pagehide, { once: true });
  observer.observe(root); if (root.firstElementChild) observer.observe(root.firstElementChild);
  capture('start');
}

export function finishActivityScrollTrace() {
  return window.__whipActivityScrollTrace?.finish() ?? null;
}

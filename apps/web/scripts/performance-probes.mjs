// Self-contained: Playwright serializes this test instrumentation into the page.
export function installPerformanceProbes({ desktop, rootId }) {
  window.__performanceEventLatency = [];
  window.__performanceCommitDOM = [];
  window.__performanceProbeOverflow = false;
  window.__performanceIPCFrames = 0;
  window.__performanceContentHandles = [];
  const pending = [];
  const seenCommits = new Set();
  const enqueue = (event, received) => {
    const committed = event?.payload?.text?.includes('commit-probe-');
    if (event?.kind !== 'stream.text' ||
        !(committed || event.payload?.text?.includes('delta-'))) return;
    if (committed && seenCommits.has(event.seq)) return;
    if (pending.length >= 512 || (committed && seenCommits.size >= 128)) {
      window.__performanceProbeOverflow = true;
      return;
    }
    if (committed) seenCommits.add(event.seq);
    pending.push({
      sequence: event.seq,
      text: event.payload.text.replaceAll("**", ""),
      start: received,
    });
  };
  const receive = data => {
        try {
          const message = JSON.parse(data);
          const handle = message.result?.content ?? (message.result?.reference_id ? message.result : undefined);
          if (handle?.digest && !window.__performanceContentHandles.some(item => item.reference_id === handle.reference_id)) {
            if (window.__performanceContentHandles.length >= 16) window.__performanceProbeOverflow = true;
            else window.__performanceContentHandles.push({ reference_id: handle.reference_id, digest: handle.digest, size: handle.size });
          }
          const received = performance.now();
          const event = message.params?.event;
          if (event?.root_id === rootId) enqueue(event, received);
          // A refresh can deliver a committed probe in the snapshot instead of
          // a notification. Its own event sequence identifies the same commit.
          // Keep ordinary delta latency samples notification-only.
          if (message.result?.root_id === rootId) {
            for (const item of message.result.presentation ?? []) {
              if (item.payload?.text?.includes('commit-probe-')) enqueue(item, received);
            }
          }
        } catch {}
  };
  if (desktop) {
    if (!window.whipDesktop) throw new Error('Desktop performance probe requires the real preload bridge');
    const stop = window.whipDesktop.onEvent(event => {
      if (event.kind === 'frame') { window.__performanceIPCFrames++; receive(event.frame); }
    });
    window.addEventListener('pagehide', stop, { once: true });
  } else {
    const NativeWebSocket = window.WebSocket;
    window.WebSocket = class extends NativeWebSocket {
      constructor(...args) { super(...args); this.addEventListener('message', message => receive(message.data)); }
    };
  }
  new MutationObserver(() => {
    const live = [...document.querySelectorAll('[data-message-id^=\"live:\"]')]
      .map((element) => element.textContent)
      .join('\n');
    for (let index = pending.length - 1; index >= 0; index--) {
      if (!live.includes(pending[index].text)) continue;
      const stamp = performance.now();
      if (pending[index].text.includes('commit-probe-')) {
        if (window.__performanceCommitDOM.length >= 128)
          window.__performanceProbeOverflow = true;
        else
          window.__performanceCommitDOM.push({
            sequence: pending[index].sequence,
            marker: pending[index].text,
            browser_ms: stamp,
            received_ms: pending[index].start,
          });
      } else if (window.__performanceEventLatency.length < 512) {
        window.__performanceEventLatency.push(stamp - pending[index].start);
      } else window.__performanceProbeOverflow = true;
      pending.splice(index, 1);
    }
  }).observe(document, { subtree: true, childList: true, characterData: true });
}

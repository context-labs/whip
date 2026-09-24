/** Legacy RN controllers must publish the first reason before notifying listeners. */
export function installAbortReason(Controller: typeof AbortController = AbortController) {
  const probe = new Controller(); const reason = {};
  probe.abort(reason);
  if (probe.signal.reason === reason) return;
  const abort = Controller.prototype.abort;
  Object.defineProperty(Controller.prototype, 'abort', {
    configurable: true, writable: true,
    value: function (this: AbortController, reason?: unknown) {
      const signal = this.signal;
      if (signal.aborted) return;
      Object.defineProperty(signal, 'reason', { configurable: true,
        value: reason === undefined ? new DOMException('The operation was aborted.', 'AbortError') : reason });
      abort.call(this, reason);
    },
  });
}

/** Expo supplies signal composition; RN's base signals lack the checkpoint. */
export function installAbortCheckpoint(prototype: { throwIfAborted?: (this: AbortSignal) => void } = AbortSignal.prototype) {
  if (typeof prototype.throwIfAborted === 'function') return;
  Object.defineProperty(prototype, 'throwIfAborted', {
    configurable: true, writable: true,
    value: function (this: AbortSignal) {
      if (this.aborted) throw ('reason' in this ? this.reason : new DOMException('The operation was aborted.', 'AbortError'));
    },
  });
}
installAbortReason();
installAbortCheckpoint();

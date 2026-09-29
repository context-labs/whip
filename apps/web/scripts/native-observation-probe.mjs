// Passive, document-local evidence. It never changes a frame, reply or close.
// Serialize this function into the owned page before its application starts.
export function installObservationProbe() {
  const NativeWebSocket = globalThis.WebSocket;
  const documentID = crypto.randomUUID();
  let connectionID = 0, clientInitialization = 0, overflow = false;
  const live = new Set(), maximum = new Map(), overlaps = [], faults = [];
  const bounded = (values, value, limit) => {
    if (values.length >= limit) overflow = true;
    else values.push(value);
  };
  const snapshot = () => ({ documentID, overflow, maximum: Object.fromEntries(maximum),
    connections: connectionID, clientInitializations: clientInitialization,
    overlaps: structuredClone(overlaps), faults: [...faults] });
  const publish = () => { void globalThis.replObservationEvidence?.(snapshot()).catch(() => {}); };
  globalThis.__readObservationProbe = snapshot;
  globalThis.WebSocket = class extends NativeWebSocket {
    constructor(...args) {
      super(...args);
      const connection = ++connectionID;
      const state = { connection, documentID, clientInitialization, runtime: null, epoch: null,
        pending: null, initialization: null };
      if (live.size >= 128) overflow = true;
      else live.add(state);
      const settle = reason => {
        if (!state.pending) return;
        state.pending.settled = reason;
        state.pending.settledAt = performance.now();
        state.pending = null;
      };
      this.__retireObservation = reason => { settle(reason); live.delete(state); };
      this.addEventListener('message', event => {
        try {
          if (typeof event.data !== 'string' || event.data.length > (8 << 20)) return;
          const reply = JSON.parse(event.data);
          if (reply.id === state.initialization) {
            state.runtime = reply.result?.runtime_id ?? null;
            state.epoch = reply.result?.process_epoch ?? null;
          }
          if (reply.id === state.pending?.requestID) settle(reply.error ? 'error_reply' : 'reply');
        } catch (error) { bounded(faults, String(error).slice(0, 1024), 16); }
      });
      this.addEventListener('close', () => this.__retireObservation('close_event'));
      this.addEventListener('error', () => this.__retireObservation('error_event'));
      this.__recordObservation = text => {
        try {
          if (typeof text !== 'string' || text.length > (8 << 20)) return;
          const request = JSON.parse(text);
          if (request.method === 'initialize') {
            state.initialization = request.id;
            if (request.id === 'initialize') clientInitialization++;
            state.clientInitialization = clientInitialization;
          }
          if (request.method !== 'sessions.observe') return;
          if (state.pending) throw new Error('Two observation requests on one unary connection');
          if (!state.runtime || !state.epoch) throw new Error('Observation lacks verified initialization evidence');
          const owner = request.params?.session_id;
          if (typeof owner !== 'string' || owner.length > 256) throw new Error('Invalid observation owner');
          const value = { connection, documentID, clientInitialization: state.clientInitialization,
            runtime: state.runtime, epoch: state.epoch, owner, requestID: request.id,
            sentAt: performance.now(), settled: null, settledAt: null };
          state.pending = value;
          const sameOwner = [...live].flatMap(item => item.pending && item.runtime === state.runtime &&
            item.epoch === state.epoch && item.pending.owner === owner ? [item.pending] : []);
          const key = JSON.stringify([state.runtime, state.epoch, owner]);
          let changed = false;
          if (!maximum.has(key) && maximum.size >= 256) overflow = true;
          else if (sameOwner.length > (maximum.get(key) ?? 0)) {
            maximum.set(key, sameOwner.length);
            changed = true;
          }
          if (sameOwner.length > 1) bounded(overlaps, sameOwner, 32);
          if (changed) publish();
        } catch (error) { bounded(faults, String(error).slice(0, 1024), 16); }
      };
    }
    send(text) { const result = super.send(text); this.__recordObservation(text); return result; }
    close(...args) { const result = super.close(...args); this.__retireObservation('close_called'); return result; }
  };
  addEventListener('pagehide', () => {
    for (const state of live) {
      if (state.pending) { state.pending.settled = 'document_retired'; state.pending.settledAt = performance.now(); }
    }
    live.clear();
    publish();
  }, { once: true });
}

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

function assertObservationDocument(evidence) {
  if (!evidence || typeof evidence.documentID !== 'string' || !evidence.documentID ||
      evidence.documentID.length > 256 || evidence.overflow !== false ||
      !Array.isArray(evidence.faults) || evidence.faults.length ||
      !Array.isArray(evidence.overlaps) || !evidence.maximum || typeof evidence.maximum !== 'object')
    throw new Error('Missing, faulty or incomplete observation lifetime evidence');
  const entries = Object.entries(evidence.maximum);
  if (entries.length > 256) throw new Error('Observation owner evidence overflow');
  for (const [key, count] of entries) {
    const identity = JSON.parse(key);
    if (!Array.isArray(identity) || identity.length !== 3 ||
        identity.some(value => typeof value !== 'string' || !value || value.length > 256) ||
        !Number.isSafeInteger(count) || count < 1)
      throw new Error('Invalid verified observation owner evidence');
    if (count > 1) throw new Error(`Duplicate views must share the owner observation: ${evidence.documentID} ${key} (${count})`);
  }
  if (evidence.overlaps.length) throw new Error('Active observation overlap was retained');
}

// Check every publication before replacing its document summary: an older
// binding delivery must not erase a violation already observed by this process.
export function retainObservationEvidence(documents, evidence, recordError) {
  try { assertObservationDocument(evidence); } catch (error) { recordError(error); }
  const index = documents.findIndex(item => item.documentID === evidence?.documentID);
  if (!evidence || (index < 0 && documents.length >= 32) || Buffer.byteLength(JSON.stringify(evidence)) > (128 << 10))
    recordError(new Error('Observation lifetime evidence overflow'));
  else if (index < 0) documents.push(evidence);
  else documents[index] = evidence;
}

export function assertSharedObservations(documents) {
  if (!Array.isArray(documents) || documents.length === 0 || documents.length > 32)
    throw new Error('Missing or incomplete observation document evidence');
  for (const evidence of documents) assertObservationDocument(evidence);
  if (!documents.some(evidence => Object.keys(evidence.maximum).length > 0))
    throw new Error('No verified session observation was recorded');
}

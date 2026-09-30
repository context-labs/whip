import assert from 'node:assert/strict';

const identityKeys = new Set(['runtime_id', 'connection_id', 'root_id', 'agent_id', 'turn_id', 'subscription_id', 'reference_id', 'parent_id', 'trace_id', 'span_id', 'call_id', 'invocation_id', 'operation_id']);
const dateKeys = new Set(['created_at', 'updated_at', 'sent_at', 'started_at', 'completed_at', 'finished_at', 'ended_at', 'last_active_at']);
const nanoKeys = new Set(['start_ns', 'end_ns', 'server_time_ns', 'startTimeUnixNano', 'endTimeUnixNano']);

// Normalize named nondeterminism only. Array order, multiplicity, empty values,
// revisions, cursors, error messages and content digests remain significant.
export function normalize(value, { directory = '', identities = [] } = {}) {
  const ids = new Map(identities);
  let next = 0;
  const identify = raw => { if (raw && !ids.has(raw)) ids.set(raw, `<identity:${++next}>`); };
  function discover(item) {
    if (Array.isArray(item)) { for (const child of item) discover(child); return; }
    if (!item || typeof item !== 'object') return;
    if (item.started_at && item.finished_at) assert.ok(Date.parse(item.finished_at) >= Date.parse(item.started_at), 'turn finished before it started');
    if (item.start_ns && item.end_ns && item.end_ns !== '0') assert.ok(BigInt(item.end_ns) >= BigInt(item.start_ns), 'span ended before it started');
    for (const [key, child] of Object.entries(item)) {
      if (identityKeys.has(key) && typeof child === 'string') identify(child);
      if (key === 'id' && typeof child === 'string' && (item.trace_id || item.parent_id !== undefined || item.kind === 'agent')) identify(child);
      discover(child);
    }
  }
  discover(value);
  function visit(item, key = '') {
    if (Array.isArray(item)) return item.map(child => visit(child));
    if (item && typeof item === 'object') return Object.fromEntries(Object.entries(item).map(([name, child]) => [name, visit(child, name)]));
    if (typeof item === 'string') {
      if (dateKeys.has(key) && item !== '') {
        assert.ok(Number.isFinite(Date.parse(item)), `invalid ${key}: ${item}`);
        return '<timestamp>';
      }
      if (nanoKeys.has(key)) {
        assert.match(item, /^\d+$/, `invalid ${key}`);
        return item === '0' ? '0' : '<nanoseconds>';
      }
      if (ids.has(item)) return ids.get(item);
      const capability = /^(files|mcp|shell|tools):(.+)$/.exec(item);
      if (capability && ids.has(capability[2])) return capability[1] + ':' + ids.get(capability[2]);
      if (directory && item.includes(directory)) return item.replaceAll(directory, '<fixture>');
    }
    return item;
  }
  return visit(value);
}

export function compare(expected, actual, label = 'compatibility transcript') {
  assert.deepEqual(actual, expected, label);
}

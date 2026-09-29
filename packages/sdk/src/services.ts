import type { Operations } from '@whip/protocol';
import type { Client } from './index.js';
import { Session } from './session.js';
import type { CallOptions } from './wire.js';

type Params<M extends keyof Operations> = Operations[M]['params'];
export class Trees {
  constructor(private readonly client: Client) {}
  get(treeID: string, options: CallOptions = {}) { return this.client.call('trees.get', { tree_id: treeID }, options); }
  list(params: Omit<Params<'trees.list'>, 'limit'> & { limit?: number } = {}, options: CallOptions = {}) { return this.client.listTrees({ limit: 100, ...params }, options); }
  async summaries(rootIDs: readonly string[], options: CallOptions = {}) {
    const [first, ...rest] = rootIDs;
    if (first === undefined) throw new RangeError('Select at least one root');
    const result = await this.client.call('trees.summaries', { root_ids: [first, ...rest] }, options);
    const remaining = new Set(rootIDs);
    for (const id of [...result.items.map(item => item.root_id), ...result.missing_root_ids]) {
      if (!remaining.delete(id)) throw new TypeError('Tree summary ownership mismatch');
    }
    if (remaining.size) throw new TypeError('Tree summary omitted a requested root');
    return result;
  }
  /** A bounded fresh activity ordering; catalog_revision does not freeze activity or provide a cursor. */
  recent(limit = 50, options: CallOptions = {}) { return this.client.call('trees.recent', { limit }, options); }
  catalog(options: CallOptions = {}) { return this.client.treeCatalog(options); }
  create(params: Omit<Params<'trees.create'>, 'creation_id'>, creationID: string, options: CallOptions = {}) { return this.client.createTree(params, creationID, options); }
  creation(creationID: string, options: CallOptions = {}) { return this.client.getTreeCreation(creationID, options); }
  update(treeID: string, expectedRevision: string, metadata: Params<'trees.update'>['metadata'], options: CallOptions = {}) { return this.client.call('trees.update', { tree_id: treeID, expected_revision: expectedRevision, metadata }, options); }
}
export class Sessions {
  constructor(private readonly client: Client) {}
  /** Trusted-client editor recall across owners. An empty page may still have a cursor.
   * Text never carries attachment/design/receipt authority and must not be replayed automatically. */
  async recentInputText(params: Omit<Params<'inputs.recent_text'>, 'limit'> & { limit?: number } = {}, options: CallOptions = {}) {
    const request = { limit: 100, ...params };
    const value = await this.client.call('inputs.recent_text', request, options);
    const items = value.items ?? [];
    if (value.scanned_count > request.limit || items.length + value.skipped_count > value.scanned_count) throw new TypeError('Input recall count mismatch');
    let previous = request.before_ordinal && request.before_ordinal !== '0' ? BigInt(request.before_ordinal) : null;
    let bytes = 0;
    const ids = new Set<string>();
    for (const item of items) {
      const ordinal = BigInt(item.ordinal);
      if (ordinal <= 0n || previous !== null && ordinal >= previous || ids.has(item.input_id)) throw new TypeError('Input recall ordering or identity mismatch');
      previous = ordinal; ids.add(item.input_id);
      bytes += new TextEncoder().encode(item.text).byteLength;
      if (bytes > 262144) throw new TypeError('Input recall text exceeds byte bound');
    }
    if (value.next_cursor !== null) {
      const cursor = BigInt(value.next_cursor);
      if (!value.scanned_count || cursor <= 0n || previous !== null && (items.length ? cursor > previous : cursor >= previous)) throw new TypeError('Input recall continuation mismatch');
    }
    return { ...value, items };
  }
  handle(sessionID: string) { return new Session(this.client, sessionID); }
  get(sessionID: string, options: CallOptions = {}) { return this.handle(sessionID).get(options); }
  list(treeID: string, params: Omit<Params<'sessions.list'>, 'tree_id' | 'limit'> & { limit?: number } = {}, options: CallOptions = {}) { return this.client.call('sessions.list', { limit: 100, ...params, tree_id: treeID }, options); }
}

/** Native host controls and saved declarations. Reads never initiate attachments. */
export class Hosts {
  constructor(private readonly client: Client) {}
  async status(options: CallOptions = {}) {
    const value = await this.client.call('host.status', {}, options);
    if (value.runtime_id !== this.client.runtimeID) throw new TypeError('Host status runtime identity mismatch');
    return value;
  }
  /** Local-only explicit process control. Never replay an uncertain stop against another epoch. */
  stop(processEpoch: string, options: CallOptions = {}) {
    return this.client.call('host.stop', { runtime_id: this.client.runtimeID, process_epoch: processEpoch }, options);
  }
  externalBrowser(options: CallOptions = {}) { return this.client.call('host.external_browser', {}, options); }
  /** Host availability CAS only. Never launches a browser or retries an uncertain write. */
  setExternalBrowser(expectedRevision: string, configuration: Params<'host.set_external_browser'>['configuration'], options: CallOptions = {}) {
    return this.client.call('host.set_external_browser', { expected_revision: expectedRevision, configuration }, options);
  }
  externalBrowserSessions(sessionID: string, options: CallOptions = {}) { return this.client.call('browser.external_sessions', { session_id: sessionID }, options); }
  /** Creates a new prepared generation; a subsequent browser operation still needs permission. */
  reconnectExternalBrowser(rootID: string, name: string, generation: string, options: CallOptions = {}) { return this.client.call('browser.reconnect_external', { root_id: rootID, name, generation }, options); }
  disconnectExternalBrowser(rootID: string, name: string, generation: string, options: CallOptions = {}) { return this.client.call('browser.disconnect_external', { root_id: rootID, name, generation }, options); }
  browserDriver(options: CallOptions = {}) { return this.client.call('host.browser_driver', {}, options); }
  /** Changes future batches only. Reread after uncertain delivery; process environment pins may reject an edit. */
  setBrowserDriver(expectedRevision: string, driver: Params<'host.set_browser_driver'>['driver'], options: CallOptions = {}) {
    return this.client.call('host.set_browser_driver', { expected_revision: expectedRevision, driver }, options);
  }
  /** Reads only the host's explicitly published source; never publishes a default file. */
  standingInstructions(options: CallOptions = {}) { return this.client.call('host.standing.read', {}, options); }
  /** Raw text CAS. After uncertain delivery, read and reconcile; never replay automatically. */
  writeStandingInstructions(expectedRevision: string, text: string, options: CallOptions = {}) {
    return this.client.call('host.standing.write', { expected_revision: expectedRevision, text }, options);
  }
  skillRoots(options: CallOptions = {}) { return this.client.call('host.skills.roots', {}, options); }
  /** Explicit publication only. Existing names cannot be rebound and no grant is created. */
  publishSkillRoot(expectedRevision: string, id: string, path: string, options: CallOptions = {}) {
    return this.client.call('host.skills.publish', { expected_revision: expectedRevision, id, path }, options);
  }
  /** Applies to future builtin resolutions; does not edit existing sessions or grant access. */
  setDefaultSkillRoots(expectedRevision: string, roots: string[], options: CallOptions = {}) {
    return this.client.call('host.skills.set_defaults', { expected_revision: expectedRevision, roots }, options);
  }
  executionDefaults(options: CallOptions = {}) { return this.client.call('host.execution_defaults', {}, options); }
  /** Saves the Execution form atomically. Zero attempts and null goal limit retain host default intent. */
  setExecutionPreferences(expectedRevision: string, preferences: Params<'host.set_execution_preferences'>['preferences'], options: CallOptions = {}) {
    return this.client.call('host.set_execution_preferences', { expected_revision: expectedRevision, preferences }, options);
  }
  /** Attempts include the initial request; goal continuations exclude its initial input. Reread after uncertain CAS delivery. */
  setExecutionDefaults(expectedRevision: string, defaults: Params<'host.set_execution_defaults'>['defaults'], options: CallOptions = {}) {
    return this.client.call('host.set_execution_defaults', { expected_revision: expectedRevision, defaults }, options);
  }
  profiles(options: CallOptions = {}) { return this.client.call('host.profiles', {}, options); }
  /** After uncertain delivery, reread and reconcile the shared host revision. */
  setProfiles(expectedRevision: string, profiles: Params<'host.set_profiles'>['profiles'], options: CallOptions = {}) {
    return this.client.call('host.set_profiles', { expected_revision: expectedRevision, profiles }, options);
  }
}

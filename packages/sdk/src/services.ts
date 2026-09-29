import type { Operations } from '@whip/protocol';
import type { Client } from './index.js';
import { Session } from './session.js';
import type { CallOptions } from './wire.js';

type Params<M extends keyof Operations> = Operations[M]['params'];
export class Trees {
  constructor(private readonly client: Client) {}
  get(treeID: string, options: CallOptions = {}) { return this.client.call('trees.get', { tree_id: treeID }, options); }
  list(params: Omit<Params<'trees.list'>, 'limit'> & { limit?: number } = {}, options: CallOptions = {}) { return this.client.listTrees({ limit: 100, ...params }, options); }
  catalog(options: CallOptions = {}) { return this.client.treeCatalog(options); }
  create(params: Omit<Params<'trees.create'>, 'creation_id'>, creationID: string, options: CallOptions = {}) { return this.client.createTree(params, creationID, options); }
  creation(creationID: string, options: CallOptions = {}) { return this.client.getTreeCreation(creationID, options); }
  update(treeID: string, expectedRevision: string, metadata: Params<'trees.update'>['metadata'], options: CallOptions = {}) { return this.client.call('trees.update', { tree_id: treeID, expected_revision: expectedRevision, metadata }, options); }
}
export class Sessions {
  constructor(private readonly client: Client) {}
  handle(sessionID: string) { return new Session(this.client, sessionID); }
  get(sessionID: string, options: CallOptions = {}) { return this.handle(sessionID).get(options); }
  list(treeID: string, params: Omit<Params<'sessions.list'>, 'tree_id' | 'limit'> & { limit?: number } = {}, options: CallOptions = {}) { return this.client.call('sessions.list', { limit: 100, ...params, tree_id: treeID }, options); }
}

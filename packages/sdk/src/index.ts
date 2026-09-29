import { assertValid } from '@whip/protocol';
import type { Admission, InitializeResult, Operations, RequestIdentity, SessionObservation } from '@whip/protocol';
import { delay } from './value.js';
import { Session } from './session.js';
import { Agents } from './agents.js';
import { Trees, Sessions, Hosts } from './services.js';
import { DurableCommand } from './command.js';
import type { DurableMethod, RecoveryJournal } from './command.js';
import { decodeResponse, operation, RemoteError } from './wire.js';
import type { CallOptions, Method, Transport } from './wire.js';

export { DeliveryError, RemoteError } from './wire.js';
export type { CallOptions, Transport } from './wire.js';
export type * from '@whip/protocol';

/** No conversation or execution state lives here. Previews are disposable runtime projections. */
export class Client {
  private sequence = 0;
  readonly trees = new Trees(this);
  readonly agents = new Agents(this);
  readonly hosts = new Hosts(this);
  readonly sessions = new Sessions(this);
  session(sessionID: string): Session { return this.sessions.handle(sessionID); }
  private constructor(private readonly transport: Transport, private readonly initial: InitializeResult, readonly clientID: string) {}

  static async connect(transport: Transport, options: { clientID: string; expectedRuntimeID?: string } & CallOptions): Promise<Client> {
    assertValid('RequestIdentity', { client_id: options.clientID, request_id: 'validate' });
    const params = { major: 4, ...(options.expectedRuntimeID ? { expected_runtime_id: options.expectedRuntimeID } : {}) };
    assertValid('InitializeParams', params);
    const response = await transport({ jsonrpc: '2.0', id: 'initialize', method: 'initialize', params }, options.expectedRuntimeID, options);
    const initial = decodeResponse('initialize', 'initialize', response);
    if (options.expectedRuntimeID && initial.runtime_id !== options.expectedRuntimeID) throw new TypeError('Runtime identity mismatch');
    return new Client(transport, structuredClone(initial), options.clientID);
  }

  get runtimeID(): string { return this.initial.runtime_id; }
  get processEpoch(): string { return this.initial.process_epoch; }
  get builtins(): NonNullable<InitializeResult['builtins']> { return structuredClone(this.initial.builtins ?? []); }

  async call<M extends Exclude<Method, 'initialize'>>(method: M, params: Operations[M]['params'], options: CallOptions = {}): Promise<Operations[M]['result']> {
    options.signal?.throwIfAborted();
    assertValid(operation(method).params, params);
    const id = 'rpc-' + ++this.sequence;
    const response = await this.transport({ jsonrpc: '2.0', id, method, params }, this.runtimeID, options);
    return decodeResponse(method, id, response);
  }

  command<M extends DurableMethod>(method: M, params: Operations[M]['params'], options: { journal?: RecoveryJournal } = {}): DurableCommand<M> {
    return DurableCommand.prepare(this, method, params, options);
  }

  /** Bounded ephemeral executor progress/decisions; null after its owning turn ends. */
  executorActivity(sessionID: string, options: CallOptions = {}): Promise<Operations['executor.activity']['result']> {
    return this.call('executor.activity', { session_id: sessionID }, options);
  }

  /** Human workspace shell, independent of sessions. A lost open acknowledgement
   * requires listTerminals inspection; never replay open automatically. */
  openTerminal(params: Omit<Operations['terminal.open']['params'], 'process_epoch'>, options: CallOptions = {}): Promise<Operations['terminal.open']['result']> {
    return this.call('terminal.open', { ...params, process_epoch: this.processEpoch }, options);
  }

  /** Ephemeral handles from this exact process generation, including retained exits. */
  listTerminals(options: CallOptions = {}): Promise<Operations['terminal.list']['result']> {
    return this.call('terminal.list', { process_epoch: this.processEpoch }, options);
  }

  /** Read one bounded replay page. A truncated page requires discarding the missing
   * byte range; disconnecting or stopping reads leaves the shell running. */
  readTerminal(ref: Operations['terminal.close']['params'], cursor: string, limit = 32768, options: CallOptions = {}): Promise<Operations['terminal.read']['result']> {
    return this.call('terminal.read', { id: ref.id, process_epoch: ref.process_epoch, cursor, limit }, options);
  }

  /** Never replay keystrokes after any uncertain write outcome. No input receipt
   * or terminal transcript is persisted, and aborting observation is not close. */
  writeTerminal(ref: Operations['terminal.close']['params'], bytes: Uint8Array, options: CallOptions = {}): Promise<Operations['terminal.write']['result']> {
    if (bytes.byteLength < 1 || bytes.byteLength > 16384) throw new TypeError('Terminal input requires 1..16384 bytes');
    return this.call('terminal.write', { id: ref.id, process_epoch: ref.process_epoch, data_base64: btoa(String.fromCharCode(...bytes)) }, options);
  }

  resizeTerminal(ref: Operations['terminal.close']['params'], cols: number, rows: number, options: CallOptions = {}): Promise<Operations['terminal.resize']['result']> {
    return this.call('terminal.resize', { id: ref.id, process_epoch: ref.process_epoch, cols, rows }, options);
  }

  /** Explicitly stops and forgets the shell after process and output owners join. */
  closeTerminal(ref: Operations['terminal.close']['params'], options: CallOptions = {}): Promise<Operations['terminal.close']['result']> {
    return this.call('terminal.close', { id: ref.id, process_epoch: ref.process_epoch }, options);
  }

  /** Offline setup templates. Does not discover routes or read credentials. */
  providerPresets(options: CallOptions = {}): Promise<Operations['providers.presets']['result']> {
    return this.call('providers.presets', {}, options);
  }

  /** Reviewed offline metadata, separate from live account membership. */
  bundledProviderModels(provider: string, options: CallOptions = {}): Promise<Operations['providers.bundled']['result']> {
    return this.call('providers.bundled', { provider }, options);
  }

  /** Reads explicit host routes and local source status; never runs a credential command. */
  listProviders(options: CallOptions = {}): Promise<Operations['providers.list']['result']> {
    return this.call('providers.list', {}, options);
  }

  /** Explicit CAS edit. After lost delivery, reread; never automatically replay key publication. */
  createProvider(params: Operations['providers.create']['params'], options: CallOptions = {}): Promise<Operations['providers.create']['result']> {
    return this.call('providers.create', params, options);
  }

  updateProvider(params: Operations['providers.update']['params'], options: CallOptions = {}): Promise<Operations['providers.update']['result']> {
    return this.call('providers.update', params, options);
  }

  /** Preserves credential files and rejects dangling defaults. */
  removeProvider(params: Operations['providers.remove']['params'], options: CallOptions = {}): Promise<Operations['providers.remove']['result']> {
    return this.call('providers.remove', params, options);
  }

  /** Saves a complete selection; applies to newly admitted roots, without changing existing sessions. */
  setProviderDefaults(params: Operations['providers.defaults']['params'], options: CallOptions = {}): Promise<Operations['providers.defaults']['result']> {
    return this.call('providers.defaults', params, options);
  }

  setProviderCompactionModel(params: Operations['providers.compaction']['params'], options: CallOptions = {}): Promise<Operations['providers.compaction']['result']> {
    return this.call('providers.compaction', params, options);
  }

  /** Cached observation only. A missing or unverified scope never initiates discovery. */
  providerCatalog(provider: string, options: CallOptions = {}): Promise<Operations['providers.catalog']['result']> {
    return this.call('providers.catalog', { provider }, options);
  }

  /** Explicit bounded network discovery. Inspect failure even when same-scope models are retained. */
  refreshProviderCatalog(provider: string, options: CallOptions = {}): Promise<Operations['providers.refresh']['result']> {
    return this.call('providers.refresh', { provider }, options);
  }

  /** Local evidence only; no catalog response establishes inference readiness. */
  providerReadiness(selection: Operations['providers.readiness']['params']['selection'], options: CallOptions = {}): Promise<Operations['providers.readiness']['result']> {
    return this.call('providers.readiness', { selection }, options);
  }

  getPermissionPolicy(sessionID: string, options: CallOptions = {}): Promise<Operations['permissions.policy']['result']> {
    return this.call('permissions.policy', { session_id: sessionID }, options);
  }

  /** Persist the edit ID and exact payload before delivery; an explicit retry returns the original receipt. */
  setPermissionMode(params: Omit<Operations['permissions.set_mode']['params'], 'edit_id'>, editID: string, options: CallOptions = {}): Promise<Operations['permissions.set_mode']['result']> {
    return this.call('permissions.set_mode', { ...params, edit_id: editID }, options);
  }

  getPermissionModeEdit(sessionID: string, editID: string, options: CallOptions = {}): Promise<Operations['permissions.mode_edit']['result']> {
    return this.call('permissions.mode_edit', { session_id: sessionID, edit_id: editID }, options);
  }

  getDefaultPermissionMode(options: CallOptions = {}): Promise<Operations['host.permission_default']['result']> {
    return this.call('host.permission_default', {}, options);
  }

  /** A host publication error requires a fresh read; never replay against a newly observed revision automatically. */
  setDefaultPermissionMode(params: Operations['host.set_permission_default']['params'], options: CallOptions = {}): Promise<Operations['host.set_permission_default']['result']> {
    return this.call('host.set_permission_default', params, options);
  }

  /** Live operation preview. Reading creates no process and retains no client cache. */
  shellInteraction(sessionID: string, cursor = '0', options: CallOptions = {}): Promise<Operations['shell.interaction']['result']> {
    return this.call('shell.interaction', { session_id: sessionID, cursor }, options);
  }

  /** Human keystrokes; acknowledge queue admission only. Never replay into a different operation. */
  shellInput(params: Operations['shell.input']['params'], options: CallOptions = {}): Promise<Operations['shell.input']['result']> {
    return this.call('shell.input', params, options);
  }

  /** Durable question evidence; reading never creates or resumes a waiter. */
  getQuestion(sessionID: string, operationID: string, options: CallOptions = {}): Promise<Operations['questions.get']['result']> {
    return this.call('questions.get', { session_id: sessionID, operation_id: operationID }, options);
  }

  listQuestions(params: Operations['questions.list']['params'], options: CallOptions = {}): Promise<Operations['questions.list']['result']> {
    return this.call('questions.list', params, options);
  }

  /** Preserve these exact answers on uncertain delivery. Inspect getQuestion; never regenerate or auto-answer. */
  answerQuestion(sessionID: string, operationID: string, answers: Operations['questions.answer']['params']['answers'], options: CallOptions = {}): Promise<Operations['questions.answer']['result']> {
    return this.call('questions.answer', { session_id: sessionID, operation_id: operationID, answers }, options);
  }

  /** Saved declarations only; never connects or resolves credentials. */
  computerStatus(options: CallOptions = {}): Promise<Operations['computer.status']['result']> { return this.call('computer.status', {}, options); }
  configureComputer(params: Operations['computer.configure']['params'], options: CallOptions = {}): Promise<Operations['computer.configure']['result']> { return this.call('computer.configure', params, options); }
  reconnectComputer(generation: string, options: CallOptions = {}): Promise<Operations['computer.reconnect']['result']> { return this.call('computer.reconnect', { generation }, { timeoutMs: 160_000, ...options }); }
  disconnectComputer(generation: string, options: CallOptions = {}): Promise<Operations['computer.disconnect']['result']> { return this.call('computer.disconnect', { generation }, options); }
  mcpConfiguration(options: CallOptions = {}): Promise<Operations['mcp.configuration']['result']> { return this.call('mcp.configuration', {}, options); }
  /** Explicit CAS publication. Reread configuration after lost delivery; never automatically replay. */
  configureMCP(params: Operations['mcp.configure']['params'], options: CallOptions = {}): Promise<Operations['mcp.configure']['result']> { return this.call('mcp.configure', params, options); }
  mcpImportCandidates(sessionID: string | null = null, options: CallOptions = {}): Promise<Operations['mcp.import.candidates']['result']> { return this.call('mcp.import.candidates', { session_id: sessionID }, options); }
  /** Saves the exact fingerprinted candidates. Connecting is a separate explicit refresh. */
  importMCP(params: Operations['mcp.import.apply']['params'], options: CallOptions = {}): Promise<Operations['mcp.import.apply']['result']> { return this.call('mcp.import.apply', params, options); }
  mcpStatus(sessionID: string, options: CallOptions = {}): Promise<Operations['mcp.status']['result']> { return this.call('mcp.status', { session_id: sessionID }, options); }
  /** Additive discovery; existing and disabled live entries are preserved. Inspect changed before choosing reload. */
  refreshMCP(sessionID: string, options: CallOptions = {}): Promise<Operations['mcp.refresh']['result']> { return this.call('mcp.refresh', { session_id: sessionID }, options); }
  /** Explicitly retires the root's shared connections and reloads current declarations. */
  reloadMCP(sessionID: string, options: CallOptions = {}): Promise<Operations['mcp.reload']['result']> { return this.call('mcp.reload', { session_id: sessionID }, options); }
  reconnectMCP(sessionID: string, server: string, options: CallOptions = {}): Promise<Operations['mcp.reconnect']['result']> { return this.call('mcp.reconnect', { session_id: sessionID, server }, options); }
  enableMCP(sessionID: string, server: string, options: CallOptions = {}): Promise<Operations['mcp.enable']['result']> { return this.call('mcp.enable', { session_id: sessionID, server }, options); }
  disableMCP(sessionID: string, server: string, options: CallOptions = {}): Promise<Operations['mcp.disable']['result']> { return this.call('mcp.disable', { session_id: sessionID, server }, options); }
  /** Attachments never confer native trust or replace declared servers. */
  attachMCP(params: Operations['mcp.attach']['params'], options: CallOptions = {}): Promise<Operations['mcp.attach']['result']> { return this.call('mcp.attach', params, options); }
  mcpTools(sessionID: string, server: string, options: CallOptions = {}): Promise<Operations['mcp.tools']['result']> { return this.call('mcp.tools', { session_id: sessionID, server }, options); }
  mcpInstructions(sessionID: string, server: string, options: CallOptions = {}): Promise<Operations['mcp.instructions']['result']> { return this.call('mcp.instructions', { session_id: sessionID, server }, options); }
  mcpBrandIcons(keys: string[], options: CallOptions = {}): Promise<Operations['mcp.brand.icons']['result']> { return this.call('mcp.brand.icons', { keys }, options); }

  /** Read-only observation; never starts a server or grants workspace access. */
  languageServerStatus(sessionID: string, options: CallOptions = {}): Promise<Operations['lsp.status']['result']> {
    return this.call('lsp.status', { session_id: sessionID }, options);
  }

  /** Accepted login belongs to the host. Recover lost delivery with list/get; never replay begin automatically. */
  beginInferenceLogin(options: CallOptions = {}): Promise<Operations['accounts.inference.begin']['result']> {
    return this.call('accounts.inference.begin', {}, options);
  }

  getInferenceLogin(flowID: string, options: CallOptions = {}): Promise<Operations['accounts.inference.get']['result']> {
    return this.call('accounts.inference.get', { flow_id: flowID }, options);
  }

  listInferenceLogins(options: CallOptions = {}): Promise<Operations['accounts.inference.list']['result']> {
    return this.call('accounts.inference.list', {}, options);
  }

  cancelInferenceLogin(flowID: string, options: CallOptions = {}): Promise<Operations['accounts.inference.cancel']['result']> {
    return this.call('accounts.inference.cancel', { flow_id: flowID }, options);
  }

  selectInferenceTeam(flowID: string, teamID: string, options: CallOptions = {}): Promise<Operations['accounts.inference.team']['result']> {
    return this.call('accounts.inference.team', { flow_id: flowID, team_id: teamID }, options);
  }

  selectInferenceProject(flowID: string, projectID: string, options: CallOptions = {}): Promise<Operations['accounts.inference.project']['result']> {
    return this.call('accounts.inference.project', { flow_id: flowID, project_id: projectID }, options);
  }

  /** Explicit remote mutation. An uncertain result requires account inspection before another creation. */
  createInferenceProject(flowID: string, name: string, options: CallOptions = {}): Promise<Operations['accounts.inference.create_project']['result']> {
    return this.call('accounts.inference.create_project', { flow_id: flowID, name }, options);
  }

  /** Retries a known recoverable step without reminting an uncertain remote key or project. */
  retryInferenceLogin(flowID: string, options: CallOptions = {}): Promise<Operations['accounts.inference.retry']['result']> {
    return this.call('accounts.inference.retry', { flow_id: flowID }, options);
  }

  /** Explicit key rotation. Recover lost delivery with list/get; never automatically start another rotation. */
  rotateInferenceKey(options: CallOptions = {}): Promise<Operations['accounts.inference.rotate']['result']> {
    return this.call('accounts.inference.rotate', {}, options);
  }

  /** Local management, inference-key, and route evidence; this does not verify provider readiness. */
  inferenceAccountStatus(options: CallOptions = {}): Promise<Operations['accounts.inference.status']['result']> {
    return this.call('accounts.inference.status', {}, options);
  }

  /** Install the canonical route using stored credentials. Leaves model defaults unchanged. */
  setupInferenceAccount(options: CallOptions = {}): Promise<Operations['accounts.inference.setup']['result']> {
    return this.call('accounts.inference.setup', {}, options);
  }

  /** Revoke local authority first and report remote cleanup separately. */
  logoutInferenceAccount(options: CallOptions = {}): Promise<Operations['accounts.inference.logout']['result']> {
    return this.call('accounts.inference.logout', {}, options);
  }

  listInferenceCleanup(options: CallOptions = {}): Promise<Operations['accounts.inference.cleanup']['result']> {
    return this.call('accounts.inference.cleanup', {}, options);
  }

  /** Explicitly retry retained cleanup. Expired cleanup evidence cannot establish remote success. */
  retryInferenceCleanup(options: CallOptions = {}): Promise<Operations['accounts.inference.retry_cleanup']['result']> {
    return this.call('accounts.inference.retry_cleanup', {}, options);
  }

  /** Accepted login belongs to this host. Recover a lost acknowledgement with listOpenAILogins/getOpenAILogin. */
  beginOpenAILogin(options: CallOptions = {}): Promise<Operations['accounts.openai.begin']['result']> {
    return this.call('accounts.openai.begin', {}, options);
  }

  getOpenAILogin(flowID: string, options: CallOptions = {}): Promise<Operations['accounts.openai.get']['result']> {
    return this.call('accounts.openai.get', { flow_id: flowID }, options);
  }

  listOpenAILogins(options: CallOptions = {}): Promise<Operations['accounts.openai.list']['result']> {
    return this.call('accounts.openai.list', {}, options);
  }

  cancelOpenAILogin(flowID: string, options: CallOptions = {}): Promise<Operations['accounts.openai.cancel']['result']> {
    return this.call('accounts.openai.cancel', { flow_id: flowID }, options);
  }

  /** Local stored-credential and route evidence; this never refreshes credentials or verifies connectivity. */
  openAIAccountStatus(options: CallOptions = {}): Promise<Operations['accounts.openai.status']['result']> {
    return this.call('accounts.openai.status', {}, options);
  }

  /** Retry route setup using saved credentials; defaults and custom routes remain unchanged. */
  setupOpenAIAccount(options: CallOptions = {}): Promise<Operations['accounts.openai.setup']['result']> {
    return this.call('accounts.openai.setup', {}, options);
  }

  logoutOpenAIAccount(options: CallOptions = {}): Promise<Operations['accounts.openai.logout']['result']> {
    return this.call('accounts.openai.logout', {}, options);
  }

  /** Fixed-revision canonical updates. A null span deletes that ID; conflicts require a fresh scan. */
  inspectWorkspace(sessionID: string, options: CallOptions = {}): Promise<Operations['workspace.inspect']['result']> {
    return this.call('workspace.inspect', { session_id: sessionID }, options);
  }

  setWorkingDirectory(params: Operations['workspace.set']['params'], options: CallOptions = {}): Promise<Operations['workspace.set']['result']> {
    return this.call('workspace.set', params, options);
  }

  configureRun(params: Operations['run.configure']['params'], options: CallOptions = {}): Promise<Operations['run.configure']['result']> {
    return this.call('run.configure', params, options);
  }

  tracePage(params: Operations['trace.page']['params'], options: CallOptions = {}): Promise<Operations['trace.page']['result']> {
    return this.call('trace.page', params, options);
  }

  /** Complete bounded OTLP file in root-owned content. No network export or automatic conflict retry. */
  exportTrace(params: Operations['trace.export']['params'], options: CallOptions = {}): Promise<Operations['trace.export']['result']> {
    return this.call('trace.export', params, options);
  }

  /** Advisory exact-owner activity. Refresh from the beginning to find newly active earlier owners. */
  hostAttention(params: Operations['host.attention']['params'], options: CallOptions = {}): Promise<Operations['host.attention']['result']> {
    return this.call('host.attention', params, options);
  }

  hostDirectories(params: Operations['host.directories.list']['params'], options: CallOptions = {}): Promise<Operations['host.directories.list']['result']> {
    return this.call('host.directories.list', params, options);
  }

  pickHostDirectory(start = '', options: CallOptions = {}): Promise<Operations['host.directory.pick']['result']> {
    return this.call('host.directory.pick', { start }, { timeoutMs: 130_000, ...options });
  }

  completeHostSkills(params: Operations['host.skills.complete']['params'], options: CallOptions = {}): Promise<Operations['host.skills.complete']['result']> {
    return this.call('host.skills.complete', params, options);
  }

  hostThemes(options: CallOptions = {}): Promise<Operations['host.themes.list']['result']> {
    return this.call('host.themes.list', {}, options);
  }

  resolveHostTheme(params: Operations['host.themes.resolve']['params'], options: CallOptions = {}): Promise<Operations['host.themes.resolve']['result']> {
    return this.call('host.themes.resolve', params, options);
  }

  /** Fixed declared host surface; listing never starts a resource or grants authority. */
  hostToolSchemas(sessionID: string, options: CallOptions = {}): Promise<Operations['tool.schemas']['result']> {
    return this.call('tool.schemas', { session_id: sessionID }, options);
  }

  /** Accepts direct human work. Keep requestID and exact bytes for receipt recovery; abort only stops observation. */
  callTool(sessionID: string, operation: Operations['tool.call']['params']['operation'], requestID: string, options: CallOptions = {}): Promise<Admission> {
    return this.call('tool.call', { session_id: sessionID, identity: this.identity(requestID), operation }, options);
  }

  runShell(sessionID: string, command: string, requestID: string, options: CallOptions & { timeout?: number; interactive?: boolean } = {}): Promise<Admission> {
    const { timeout, interactive = false, ...callOptions } = options;
    return this.call('shell.run', { session_id: sessionID, identity: this.identity(requestID), command, interactive, ...(timeout === undefined ? {} : { timeout }) }, callOptions);
  }

  /** Keep this requestID and exact payload until admission is known, including after a lost acknowledgement. */
  submit(sessionID: string, parts: Operations['sessions.submit']['params']['parts'], requestID: string, options: CallOptions & { designContext?: Operations['sessions.submit']['params']['design_context'] } = {}): Promise<Admission> {
    const { designContext, ...callOptions } = options;
    return this.call('sessions.submit', { session_id: sessionID, source: 'user', parts, identity: this.identity(requestID), ...(designContext === undefined ? {} : { design_context: designContext }) }, callOptions);
  }

  /** Queues context maintenance with ordinary admission, cancellation and receipt recovery. */
  compact(sessionID: string, requestID: string, options: CallOptions = {}): Promise<Admission> {
    return this.call('sessions.compact', { session_id: sessionID, identity: this.identity(requestID) }, options);
  }

  /** Keep editID and this exact snapshot/boundary when retrying an uncertain rewind. */
  rewind(params: Omit<Operations['sessions.rewind']['params'], 'edit_id'>, editID: string, options: CallOptions = {}): Promise<Operations['sessions.rewind']['result']> {
    return this.call('sessions.rewind', { ...params, edit_id: editID }, options);
  }

  /** Keep forkID and the exact source snapshot; a deleted destination remains deleted on retry. */
  fork(params: Omit<Operations['sessions.fork']['params'], 'fork_id'>, forkID: string, options: CallOptions = {}): Promise<Operations['sessions.fork']['result']> {
    return this.call('sessions.fork', { ...params, fork_id: forkID }, options);
  }

  /** Child identity, initial input and delegated authority share one recoverable admission. */
  spawn(params: Omit<Operations['sessions.spawn']['params'], 'identity'>, requestID: string, options: CallOptions = {}): Promise<Operations['sessions.spawn']['result']> {
    return this.call('sessions.spawn', { ...params, identity: this.identity(requestID) }, options);
  }

  /** Keep scheduleID and the exact template when retrying; interval anchoring happens once on the host. */
  createSchedule(params: Omit<Operations['schedules.create']['params'], 'schedule_id'>, scheduleID: string, options: CallOptions = {}): Promise<Operations['schedules.create']['result']> {
    return this.call('schedules.create', { ...params, schedule_id: scheduleID }, options);
  }

  /** Keep both IDs and the exact payload. Delivery failure never authorizes a new capture automatically. */
  captureWorkspace(sessionID: string, snapshotID: string, actionID: string, options: CallOptions = {}): Promise<Operations['workspace.capture']['result']> {
    return this.call('workspace.capture', { session_id: sessionID, snapshot_id: snapshotID, action_id: actionID }, options);
  }

  /** Restores the captured tracked-path overlay. An uncertain action must be inspected, never automatically replayed. */
  restoreWorkspace(sessionID: string, snapshotID: string, actionID: string, options: CallOptions = {}): Promise<Operations['workspace.restore']['result']> {
    return this.call('workspace.restore', { session_id: sessionID, snapshot_id: snapshotID, action_id: actionID }, options);
  }

  /** Explicitly releases a retained pin. Preserve actionID for exact retries, including after session deletion. */
  releaseWorkspace(sessionID: string, snapshotID: string, actionID: string, options: CallOptions = {}): Promise<Operations['workspace.release']['result']> {
    return this.call('workspace.release', { session_id: sessionID, snapshot_id: snapshotID, action_id: actionID }, options);
  }

  /** Observation only: a claimed or uncertain action never triggers Git work. */
  getWorkspaceAction(sessionID: string, actionID: string, options: CallOptions = {}): Promise<Operations['workspace.action']['result']> {
    return this.call('workspace.action', { session_id: sessionID, action_id: actionID }, options);
  }

  getWorkspaceSnapshot(sessionID: string, snapshotID: string, options: CallOptions = {}): Promise<Operations['workspace.snapshot']['result']> {
    return this.call('workspace.snapshot', { session_id: sessionID, snapshot_id: snapshotID }, options);
  }

  /** Bounded metadata, including released records. Continue with the last ID as after; no cache is retained here. */
  listWorkspaceSnapshots(params: Operations['workspace.snapshots']['params'], options: CallOptions = {}): Promise<Operations['workspace.snapshots']['result']> {
    return this.call('workspace.snapshots', params, options);
  }

  /** Keep goalID and the exact creation payload after an uncertain acknowledgement. Current does not imply armed. */
  createGoal(params: Omit<Operations['goals.create']['params'], 'goal_id'>, goalID: string, options: CallOptions = {}): Promise<Operations['goals.create']['result']> {
    return this.call('goals.create', { ...params, goal_id: goalID }, options);
  }

  /** Keep requestID and the exact payload on retries; use recover/wait for ordinary admission and turn outcome. */
  formulateGoal(params: Omit<Operations['goals.formulate']['params'], 'identity'>, requestID: string, options: CallOptions = {}): Promise<Admission> {
    return this.call('goals.formulate', { ...params, identity: this.identity(requestID) }, options);
  }

  /** Historical acceptance belongs to this candidate, even if its maintenance turn later failed or was interrupted. */
  getGoalFormulation(sessionID: string, attemptID: string, options: CallOptions = {}): Promise<Operations['goals.formulation']['result']> {
    return this.call('goals.formulation', { session_id: sessionID, attempt_id: attemptID }, options);
  }

  /** Persist creationID and the exact payload before sending. An explicit retry preserves the original destination. */
  createTree(params: Omit<Operations['trees.create']['params'], 'creation_id'>, creationID: string, options: CallOptions = {}): Promise<Operations['trees.create']['result']> {
    return this.call('trees.create', { ...params, creation_id: creationID }, options);
  }

  /** Original creation evidence and the current destination, including a durable deletion tombstone. */
  getTreeCreation(creationID: string, options: CallOptions = {}): Promise<Operations['trees.creation']['result']> {
    return this.call('trees.creation', { creation_id: creationID }, options);
  }

  /** Invalidation head for every tree, including unopened and off-page metadata. No polling is started. */
  treeCatalog(options: CallOptions = {}): Promise<Operations['trees.catalog']['result']> {
    return this.call('trees.catalog', {}, options);
  }

  /** Capture revision on the first page and pass expected_revision on later pages. A conflict requires a fresh traversal. */
  listTrees(params: Operations['trees.list']['params'], options: CallOptions = {}): Promise<Operations['trees.list']['result']> {
    return this.call('trees.list', params, options);
  }

  /** Streams bounded pages at one revision. A changed catalog throws CONFLICT; yielded pages must then be discarded. */
  async *treePages(params: Omit<Operations['trees.list']['params'], 'after'>, options: CallOptions = {}): AsyncGenerator<Operations['trees.list']['result']> {
    const request = structuredClone(params);
    let after: string | undefined;
    let revision = request.expected_revision;
    for (;;) {
      const page = await this.listTrees({ ...request, after, expected_revision: revision }, options);
      if (revision !== undefined && page.revision !== revision) throw new TypeError('Catalog revision mismatch');
      revision = page.revision;
      const next = page.next_cursor;
      if (next !== null && after !== undefined && next <= after) throw new TypeError('Catalog cursor did not advance');
      yield page;
      if (next === null) return;
      after = next;
    }
  }

  /** All immutable revisions, without configuration bodies or a mutable latest alias. */
  listDefinitions(params: Operations['definitions.list']['params'], options: CallOptions = {}): Promise<Operations['definitions.list']['result']> {
    return this.call('definitions.list', params, options);
  }

  /** Immutable naming intent. Its receipt_identity may precede admission; this read never starts work. */
  getAutomaticTitleDecision(treeID: string, options: CallOptions = {}): Promise<Operations['trees.title_decision']['result']> {
    return this.call('trees.title_decision', { tree_id: treeID }, options);
  }

  /** Historical application evidence. Current selected naming belongs only to trees.get metadata. */
  getAutomaticTitleResult(treeID: string, attemptID: string, options: CallOptions = {}): Promise<Operations['trees.title_result']['result']> {
    return this.call('trees.title_result', { tree_id: treeID, attempt_id: attemptID }, options);
  }

  currentGoal(sessionID: string, options: CallOptions = {}): Promise<Operations['goals.current']['result']> {
    return this.call('goals.current', { session_id: sessionID }, options);
  }

  getGoal(sessionID: string, goalID: string, options: CallOptions = {}): Promise<Operations['goals.get']['result']> {
    return this.call('goals.get', { session_id: sessionID, goal_id: goalID }, options);
  }

  /** Resume uses an ordinary recoverable input identity; it does not reset the continuation allowance. */
  resumeGoal(sessionID: string, goal: Operations['goals.resume']['params']['goal'], requestID: string, options: CallOptions = {}): Promise<Admission> {
    return this.call('goals.resume', { session_id: sessionID, goal, identity: this.identity(requestID) }, options);
  }

  /** Requests cancellation of the exact goal-owned turn, if any. A captured human turn continues. */
  cancelGoal(sessionID: string, goalID: string, options: CallOptions = {}): Promise<Operations['goals.cancel']['result']> {
    return this.call('goals.cancel', { session_id: sessionID, goal_id: goalID }, options);
  }

  /** Keep a globally unique mailID and the same payload when retrying an uncertain send. */
  sendMail(params: Omit<Operations['mail.send']['params'], 'mail_id'>, mailID: string, options: CallOptions = {}): Promise<Operations['mail.send']['result']> {
    return this.call('mail.send', { ...params, mail_id: mailID }, options);
  }

  /** Retain versionID and the exact encoded JSON payload when a write acknowledgement is lost. */
  writeState(params: Omit<Operations['state.write']['params'], 'version_id'>, versionID: string, options: CallOptions = {}): Promise<Operations['state.write']['result']> {
    return this.call('state.write', { ...params, version_id: versionID }, options);
  }

  /** Appends strings or arrays against an explicit revision; conflicts never overwrite another writer. */
  appendState(params: Omit<Operations['state.append']['params'], 'version_id'>, versionID: string, options: CallOptions = {}): Promise<Operations['state.append']['result']> {
    return this.call('state.append', { ...params, version_id: versionID }, options);
  }

  /** Subscribes from an observed revision; creation atomically catches up with the current head. */
  subscribeState(params: Omit<Operations['state.subscribe']['params'], 'subscription_id'>, subscriptionID: string, options: CallOptions = {}): Promise<Operations['state.subscribe']['result']> {
    return this.call('state.subscribe', { ...params, subscription_id: subscriptionID }, options);
  }

  recover(requestID: string, options: CallOptions = {}): Promise<Admission> {
    return this.call('receipts.get', this.identity(requestID), options);
  }

  /** Aborting this wait affects only the observer. Use inputs.cancel/turns.cancel to cancel work. */
  async wait(requestID: string, options: CallOptions = {}): Promise<Admission> {
    for (;;) {
      const result = await this.recover(requestID, options);
      if (result.receipt.deleted_at || result.input?.state === 'cancelled' || result.turn?.finished_at) return result;
      await delay(25, options.signal);
    }
  }

  /**
   * Read committed pages and disposable previews. Replace a preview by message_id
   * when its committed message arrives; a null preview or changed epoch clears it.
   * A changed snapshot.revision replaces all prior history, including an empty page.
   * This iterator retains only cursors and revisions, never a transcript.
   * Aborting observation does not cancel the session's work.
   */
  async *observe(sessionID: string, options: CallOptions & { after?: string; expectedRevision?: string } = {}): AsyncGenerator<SessionObservation> {
    let after = options.after ?? '0';
    let expectedRevision = options.expectedRevision;
    if (BigInt(after) !== 0n && expectedRevision === undefined) throw new TypeError('Resuming observation requires its history revision');
    let previous: string | undefined;
    for (;;) {
      let snapshot: SessionObservation;
      try {
        snapshot = await this.call('sessions.observe', {
          session_id: sessionID, after, limit: 100,
          ...(expectedRevision === undefined ? {} : { expected_revision: expectedRevision }),
        }, { signal: options.signal });
      } catch (error) {
        if (!(error instanceof RemoteError) || error.kind !== 'CONFLICT' || expectedRevision === undefined) throw error;
        after = '0';
        expectedRevision = undefined;
        previous = undefined;
        continue;
      }
      if (expectedRevision !== undefined && snapshot.snapshot.revision !== expectedRevision) throw new TypeError('Observation history revision did not match');
      expectedRevision = snapshot.snapshot.revision;
      const messages = snapshot.messages ?? [];
      const preview = snapshot.preview;
      const revision = expectedRevision + ':' + snapshot.epoch + ':' + (preview ? preview.attempt_id + ':' + preview.revision : 'none');
      for (const message of messages) {
        if (BigInt(message.sequence) <= BigInt(after)) throw new TypeError('Observation history cursor did not advance');
        after = message.sequence;
      }
      if (messages.length || previous !== revision) {
        previous = revision;
        yield snapshot;
      }
      if (messages.length) continue; // Drain bounded history pages before polling.
      await delay(100, options.signal);
    }
  }

  browserAttachments(sessionID: string, options: CallOptions = {}): Promise<Operations['browser.attachments']['result']> {
    return this.call('browser.attachments', { session_id: sessionID }, options);
  }

  /** Explicitly asks the offered native provider for bounded current metadata. */
  browserTabs(sessionID: string, options: CallOptions = {}): Promise<Operations['browser.tabs']['result']> {
    return this.call('browser.tabs', { session_id: sessionID }, options);
  }

  private identity(requestID: string): RequestIdentity { return { client_id: this.clientID, request_id: requestID }; }
}

export { ExecutorClient } from './executors.js';
export type { DuplexTransport } from './executors.js';

export { DurableCommand, RecoveryJournal, RecoveryError, RecoveryPersistenceError, recoveryNamespace, maxRecoveryRecordBytes } from './command.js';
export type { DurableMethod, RecoveryRecord, RecoveryStorage, RecoveryCheck, RecoveryEvidence } from './command.js';

export { Session } from './session.js';
export { Trees, Sessions, Hosts } from './services.js';

export type { Session as SessionRecord } from '@whip/protocol';

export { framedTransport } from './framed.js';
export type { FramedConnection, FrameHandlers, FramedConnector } from './framed.js';

export { BrowserProviderClient } from './browser-provider.js';
export { browserProviderFramed } from './browser-framed.js';

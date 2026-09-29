import { useEffect, useMemo, useState, type FormEvent } from 'react';
import { createRoot } from 'react-dom/client';
import { Client, DurableCommand, RecoveryJournal, type RecoveryRecord, type Operations, type ContentReference, type Session } from '@whip/sdk';
import { discoverGateway, browserSocket } from '@whip/sdk/browser';
import { createSessionView, createExecutionView, createTreeCatalogView, cellExecutionRows } from '@whip/sdk/state';
import { useSessionView, useExecutionView, useTreeCatalogView } from '@whip/sdk/react';
import { browserRecoveryStorage } from './recovery';

const clientID = localStorage.getItem('whip.example.clientID') ?? crypto.randomUUID();
localStorage.setItem('whip.example.clientID', clientID);
const storage = browserRecoveryStorage(localStorage, navigator.locks);
const journal = new RecoveryJournal(storage, { maxRecords: 32, maxBytes: 4 << 20 });
const message = (error: unknown) => error instanceof Error ? error.message : String(error);
const deadline = () => ({ signal: AbortSignal.timeout(120000) });

function App() {
  const [endpoint, setEndpoint] = useState(localStorage.getItem('whip.example.endpoint') ?? 'http://127.0.0.1:8080');
  const [selected, setSelected] = useState('');
  const [client, setClient] = useState<Client>();
  const [connected, setConnected] = useState(false), [error, setError] = useState('');
  useEffect(() => {
    if (!selected) return;
    const lifetime = new AbortController(); let calls = new AbortController(), timer: ReturnType<typeof setTimeout>;
    let active: Client | undefined, pinned = localStorage.getItem('whip.example.runtime:' + selected) ?? undefined;
    // This loop only rediscovers identity and reconnects observation. It never
    // submits, retries, starts the host, rebinds an executor, or restores grants.
    const observe = async () => {
      try {
        const info = await discoverGateway(selected, { signal: lifetime.signal, expectedRuntimeID: pinned });
        if (!active || active.processEpoch !== info.process_epoch || calls.signal.aborted) {
          calls.abort(); calls = new AbortController();
          const transport = browserSocket(selected, { expectedRuntimeID: info.runtime_id, expectedProcessEpoch: info.process_epoch });
          const signal = calls.signal;
          active = await Client.connect((request, expected, options) => transport(request, expected, { ...options, signal: AbortSignal.any([signal, lifetime.signal, ...(options.signal ? [options.signal] : [])]) }), { clientID, expectedRuntimeID: info.runtime_id, signal: lifetime.signal });
          pinned = active.runtimeID; localStorage.setItem('whip.example.runtime:' + selected, pinned);
          if (!lifetime.signal.aborted) setClient(active);
        }
        await active.hosts.status({ signal: lifetime.signal });
        if (!lifetime.signal.aborted) { setConnected(true); setError(''); }
      } catch (error) { calls.abort(); if (!lifetime.signal.aborted) { setConnected(false); setError(message(error)); } }
      finally { if (!lifetime.signal.aborted) timer = setTimeout(() => void observe(), 1000); }
    };
    setConnected(false); void observe();
    return () => { lifetime.abort(); calls.abort(); clearTimeout(timer); };
  }, [selected]);
  function connect(event: FormEvent) { event.preventDefault(); if (selected !== endpoint) setClient(undefined); localStorage.setItem('whip.example.endpoint', endpoint); setSelected(endpoint); }
  return <><header><span className="wordmark">WHIP</span><span className="muted">Native client example</span><form onSubmit={connect}><label htmlFor="endpoint">Host</label><input id="endpoint" value={endpoint} onChange={event => setEndpoint(event.target.value)} required /><button>Connect</button></form></header>
    {client ? <Connected key={client.runtimeID} client={client} connected={connected} /> : <div className="empty"><h1>Connect to your execution host</h1><p>Enter the URL of an explicitly enabled v4 gateway. This example never starts a runtime.</p></div>}
    {error && <p className="workspace" role="status">Connection unavailable: {error}</p>}</>;
}
function Connected({ client, connected }: { client: Client; connected: boolean }) {
  const list = useMemo(() => createTreeCatalogView(client, { maxItems: 100, maxBytes: 1 << 20 }), [client]);
  const catalog = useTreeCatalogView(list);
  const [rootID, select] = useState(''), [cwd, setCwd] = useState(''), [error, setError] = useState('');
  const [records, setRecords] = useState<readonly RecoveryRecord[]>([]);
  const refreshRecovery = async () => setRecords((await journal.list()).filter(record => record.runtimeID === client.runtimeID && record.clientID === clientID));
  useEffect(() => { void list.start(); return () => { void list.dispose(); }; }, [list]);
  useEffect(() => { void refreshRecovery().catch(error => setError(message(error))); }, [client, rootID]);
  async function create(event: FormEvent) {
    event.preventDefault(); setError('');
    try {
      const definition = client.builtins.find(item => item.id === 'coding'); if (!definition) throw new Error('Coding definition unavailable');
      const command = client.command('trees.create', { creation_id: crypto.randomUUID(), definition, working_directory: cwd, metadata: { title: null, pinned: false, archived: false }, overrides: { automatic_title: false } }, { journal });
      const result = await command.send(deadline());
      if (!result.root) throw new Error('Created tree was deleted');
      select(result.root.id); await command.forget(); await list.refresh();
    } catch (error) { setError(message(error)); } finally { await refreshRecovery().catch(error => setError(message(error))); }
  }
  return <main><aside><div className="row"><h2>Sessions</h2><span role="status">{connected ? 'connected' : 'reconnecting'}</span></div>
    <form onSubmit={create}><label htmlFor="cwd">Working directory on host</label><input id="cwd" value={cwd} onChange={event => setCwd(event.target.value)} placeholder="/absolute/host/path" required /><button disabled={!connected}>New session</button></form>
    <ul>{catalog.items.map(item => <li key={item.tree.id}><button aria-current={rootID === item.root_id} onClick={() => select(item.root_id)}>{item.tree.metadata.title || 'Untitled session'}<div className="path">{item.working_directory}</div></button></li>)}</ul>
    {catalog.nextCursor && <button onClick={() => void list.next().catch(error => setError(message(error)))}>Next sessions</button>}
    <button onClick={() => void refreshRecovery().catch(error => setError(message(error)))}>Refresh recovery records</button>
    {records.length > 0 && <details><summary>Recover submitted commands ({records.length})</summary>{records.map(record => <Recovery key={record.request} record={record} client={client} connected={connected} changed={refreshRecovery} />)}</details>}
    {(error || catalog.error) && <p role="alert">{error || catalog.error?.message}</p>}</aside>
    {rootID ? <Conversation key={rootID} client={client} connected={connected} rootID={rootID} changed={refreshRecovery} /> : <div className="empty"><h1>Your work stays on the host</h1><p>Select or create a session. Accepted work survives a disconnected viewer.</p></div>}</main>;
}
function Recovery({ record, client, connected, changed }: { record: RecoveryRecord; client: Client; connected: boolean; changed(): Promise<void> }) {
  const command = useMemo(() => DurableCommand.recover(client, record, { journal }), [client, record]);
  const [status, setStatus] = useState(record.accepted ? 'Acknowledged; outcome not checked' : 'Delivery unknown');
  const run = async (action: 'check' | 'retry' | 'forget') => {
    try {
      if (action === 'check') { const result = await command.check(deadline()); setStatus(result.state === 'found' ? 'Exact request accepted' : result.state === 'identity_only' ? 'Identity found; payload not verified' : result.state); }
      if (action === 'retry') { await command.retry(deadline()); setStatus('Exact request acknowledged'); }
      if (action === 'forget') { if (!confirm('Forget local tracking? This does not cancel work. An unresolved outcome may be lost.')) return; await command.forget(); }
      await changed();
    } catch (error) { setStatus(message(error)); }
  };
  const params = command.params;
  const identity = 'identity' in params ? params.identity.request_id : 'creation_id' in params ? params.creation_id : command.method;
  return <div className="recovery"><p>{command.method} · {identity} · {status}</p><button disabled={!connected} onClick={() => void run('check')}>Check only</button><button disabled={!connected} onClick={() => void run('retry')}>Retry exact request</button><button onClick={() => void run('forget')}>Forget tracking</button></div>;
}
type SessionRecord = Operations['sessions.get']['result'];
function Conversation({ client, connected, rootID, changed }: { client: Client; connected: boolean; rootID: string; changed(): Promise<void> }) {
  const [selectedID, setSelected] = useState(rootID), [children, setChildren] = useState<SessionRecord[]>([]), [moreChildren, setMoreChildren] = useState(false);
  const [treeID, setTreeID] = useState(''), [error, setError] = useState('');
  useEffect(() => {
    const controller = new AbortController();
    void client.session(rootID).get({ signal: controller.signal }).then(async root => { setTreeID(root.tree_id); const page = await client.sessions.list(root.tree_id, { limit: 100 }, { signal: controller.signal }); if (!controller.signal.aborted) { setChildren((page.items ?? []).filter(item => item.id !== rootID)); setMoreChildren((page.items?.length ?? 0) === 100); } }).catch(error => { if (!controller.signal.aborted) setError(message(error)); });
    return () => controller.abort();
  }, [client, rootID]);
  async function refreshChildren() { const page = await client.sessions.list(treeID, { limit: 100 }, deadline()); setChildren((page.items ?? []).filter(item => item.id !== rootID)); setMoreChildren((page.items?.length ?? 0) === 100); }
  return <section className="workspace"><div className="row"><h1>Session</h1><label>Inspect session <select value={selectedID} onChange={event => setSelected(event.target.value)}><option value={rootID}>Root</option>{children.map(child => <option key={child.id} value={child.id}>{child.definition.id} · {child.id}</option>)}</select></label><button disabled={!connected || !treeID} onClick={() => void refreshChildren().catch(error => setError(message(error)))}>Refresh children</button></div>
    {moreChildren && <p>Only the first 100 sessions are loaded.</p>}{error && <p role="alert">{error}</p>}
    <SessionPane key={selectedID} client={client} connected={connected} sessionID={selectedID} rootID={rootID} changed={changed} /></section>;
}
function SessionPane({ client, connected, sessionID, rootID, changed }: { client: Client; connected: boolean; sessionID: string; rootID: string; changed(): Promise<void> }) {
  const session = useMemo(() => client.session(sessionID), [client, sessionID]);
  const view = useMemo(() => createSessionView(session, { maxMessages: 200, maxBytes: 2 << 20 }), [session]);
  const execution = useMemo(() => createExecutionView(session, view, { maxTurns: 8, maxBytes: 1 << 20, pollIntervalMs: 250 }), [session, view]);
  const state = useSessionView(view), executed = useExecutionView(execution);
  const draftKey = `whip.example.draft:${client.runtimeID}:${sessionID}`;
  const [draft, setDraft] = useState(() => sessionStorage.getItem(draftKey) ?? ''), [status, setStatus] = useState(''), [error, setError] = useState('');
  const [attachment, setAttachment] = useState<ContentReference>(), [active, setActive] = useState<DurableCommand<'sessions.submit'>>();
  useEffect(() => { void view.start(); void execution.start(); return () => { void execution.dispose(); void view.dispose(); }; }, [view, execution]);
  useEffect(() => {
    try {
      if (!draft) { sessionStorage.removeItem(draftKey); return; }
      const keys = Object.keys(sessionStorage).filter(key => key.startsWith('whip.example.draft:'));
      if (!keys.includes(draftKey) && keys.length >= 32) throw new Error('Draft storage is full; resolve another draft before navigating away.');
      if (new TextEncoder().encode(draft).byteLength > 65536) throw new Error('Draft exceeds 64KiB and cannot be saved.');
      sessionStorage.setItem(draftKey, draft);
    } catch (error) { setError(message(error)); }
  }, [draftKey, draft]);
  async function submit(event: FormEvent) {
    event.preventDefault(); setError(''); const text = draft;
    try {
      const parts: Operations['sessions.submit']['params']['parts'] = [{ type: 'text', text }];
      if (attachment) parts.push({ type: 'content', reference_id: attachment.id });
      const command = session.submission(parts, crypto.randomUUID(), { journal }); setActive(command); setStatus('Submitting');
      await command.send(deadline()); setDraft(current => current === text ? '' : current); setAttachment(current => current === attachment ? undefined : current); setStatus('Accepted'); await changed();
      const settled = await command.wait(deadline()); setStatus(settled.turn?.state ?? settled.input?.state ?? 'Receipt retained');
      if (settled.turn?.failure) setError(settled.turn.failure);
      await command.forget(); setActive(undefined); await changed();
    } catch (error) { setError(message(error)); setStatus('Delivery or outcome unknown; check recovery before retrying'); await changed().catch(() => {}); }
  }
  const rows = cellExecutionRows(executed, state.history.messages);
  return <><div className="row"><span role="status">{state.status}</span><span className="path">{sessionID}</span></div>
    {!connected && <p className="notice">Reconnecting. Your draft stays here; accepted work stays on the host.</p>}
    {state.truncated && <p className="notice">This transcript is bounded. Older or oversized evidence may be outside the loaded window.</p>}
    {executed.truncated && <p className="notice">This execution window is bounded; some cells or operations are not loaded.</p>}
    {executed.olderCursor && <button onClick={() => void execution.loadOlder().catch(error => setError(message(error)))}>Older executions</button>}
    {executed.windowBefore && <button onClick={() => void execution.latest().catch(error => setError(message(error)))}>Latest executions</button>}
    <div className="transcript">{state.history.olderCursor && <button onClick={() => void view.loadOlder().catch(error => setError(message(error)))}>Load older messages</button>}
      {state.history.latestMissing && <button onClick={() => void view.latest().catch(error => setError(message(error)))}>Return to latest</button>}
      {state.history.messages.map(item => <article key={item.id}><span className="role">{item.role}</span>{item.parts.map((part, index) => part.type === 'text' ? <pre key={index}>{part.text}</pre> : part.type === 'content' ? <button key={index} onClick={async () => { try { setError(new TextDecoder().decode(await session.content.readBytes(part.reference_id, { maxBytes: 1 << 20 }))); } catch (error) { setError(message(error)); } }}>Read scoped attachment</button> : null)}</article>)}
      {state.preview && <article className="preview"><span className="role">Provisional assistant</span><pre>{state.preview.text}</pre>{state.preview.truncated && <p>Provisional response is truncated.</p>}</article>}
      {rows.map(row => <article className="cell" key={row.cell.id}><span className="role">Cell · {row.cell.state}</span>{row.call && <pre>{JSON.stringify(row.call.value.arguments)}</pre>}{row.result ? <pre className="output">{row.result.value.output}</pre> : row.output && <><pre className="output">{row.output.text}</pre>{row.output.truncated && <p>Live output truncated at 64KiB.</p>}</>}</article>)}
    </div>
    <Decisions session={session} connected={connected} version={state.activity?.active_turn?.id ?? ''} root={sessionID === rootID} />
    {(error || state.error) && <p role="alert">{error || state.error?.message}</p>}
    <form className="composer" onSubmit={submit}><label htmlFor="prompt">{sessionID === rootID ? 'Message the root agent' : 'Message the selected child'}</label><textarea id="prompt" maxLength={16384} value={draft} onChange={event => setDraft(event.target.value)} /><div className="row"><label>Attach file <input type="file" disabled={!connected} onChange={async event => { const file = event.target.files?.[0]; if (!file) return; try { if (file.size > (4 << 20)) throw new Error('Attachments are limited to 4MiB'); setAttachment(await session.content.upload(crypto.randomUUID(), file.type || 'application/octet-stream', new Uint8Array(await file.arrayBuffer()))); } catch (error) { setError(message(error)); } }} /></label><button disabled={!connected || !draft.trim()}>Send</button></div>
      {attachment && <button type="button" onClick={async () => { try { setError(new TextDecoder().decode(await session.content.readBytes(attachment, { maxBytes: 1 << 20 }))); } catch (error) { setError(message(error)); } }}>Read attachment · {attachment.size} bytes</button>}
      <div className="row"><span role="status">{status}</span>{active && <button type="button" disabled={!connected} onClick={async () => { try { const check = await active.check(deadline()); if (check.state !== 'found' || !('input' in check.evidence) || !check.evidence.input) throw new Error('Exact accepted input unavailable'); await session.inputs.cancel(check.evidence.input.id, deadline()); setStatus('Cancellation requested'); } catch (error) { setError(message(error)); } }}>Cancel accepted input</button>}</div></form></>;
}
type Question = Operations['questions.get']['result'];
function Decisions({ session, connected, version, root }: { session: Session; connected: boolean; version: string; root: boolean }) {
  const [questions, setQuestions] = useState<Question[]>([]), [operations, setOperations] = useState<Operations['operations.get']['result'][]>([]), [error, setError] = useState('');
  useEffect(() => {
    const controller = new AbortController(); let timer: ReturnType<typeof setTimeout>;
    const read = async () => {
      try {
        if (!connected) { setQuestions([]); setOperations([]); return; }
        const options = { signal: controller.signal };
        const [questions, permissions] = await Promise.all([session.questions.list({ pending_only: true, limit: 8 }, options), session.permissions.list({ pending_only: true, limit: 8 }, options)]);
        const operations = await Promise.all((permissions.items ?? []).map(item => session.operations.get(item.operation_id, options)));
        if (new TextEncoder().encode(JSON.stringify(operations)).byteLength > 65536) throw new Error('Permission details exceed this example’s display limit');
        if (!controller.signal.aborted) { setQuestions(questions.items); setOperations(operations); setError(''); }
      } catch (error) { if (!controller.signal.aborted) setError(message(error)); }
      finally { if (!controller.signal.aborted) timer = setTimeout(() => void read(), 500); }
    };
    void read(); return () => { controller.abort(); clearTimeout(timer); };
  }, [session, connected, version]);
  return <>{questions.map(question => <QuestionForm key={question.operation_id} question={question} session={session} disabled={!connected || !root} />)}{operations.map(operation => <div className="question" key={operation.id}><h2>Permission requested</h2><pre>{operation.capability}\n{operation.resource}\n{JSON.stringify(operation.arguments, null, 2)}</pre><div className="row">{[true, false].map(allow => <button key={String(allow)} disabled={!connected || !root} onClick={async () => { try { await session.permissions.resolve(operation.id, allow); } catch (error) { setError(message(error)); } }}>{allow ? 'Allow once' : 'Deny'}</button>)}</div></div>)}{error && <p role="alert">{error}</p>}</>;
}
function QuestionForm({ question, session, disabled }: { question: Question; session: Session; disabled: boolean }) {
  const [answers, setAnswers] = useState<string[][]>(() => question.request.questions.map(() => [])), [error, setError] = useState('');
  const submit = async (dismissed: boolean) => { try { const [first, ...rest] = answers.map(answer => ({ answer: dismissed ? [] : answer, dismissed })); if (!first) throw new Error('Missing question'); await session.questions.answer(question.operation_id, [first, ...rest]); } catch (error) { setError(message(error)); } };
  return <form className="question" onSubmit={event => { event.preventDefault(); void submit(false); }}>{question.request.questions.map((item, index) => <fieldset key={index}><legend>{item.question}</legend>{item.options.map(option => <label key={option.label}><input type={item.multiple ? 'checkbox' : 'radio'} name={question.operation_id + index} checked={answers[index].includes(option.label)} onChange={event => setAnswers(current => current.map((answer, i) => i !== index ? answer : item.multiple ? event.target.checked ? [...answer, option.label] : answer.filter(value => value !== option.label) : [option.label]))} />{option.label}{option.recommended ? ' (Recommended)' : ''} · {option.description}</label>)}<input aria-label="Your answer" placeholder="Or type your answer" onChange={event => setAnswers(current => current.map((answer, i) => i === index ? [event.target.value] : answer))} /></fieldset>)}<button disabled={disabled}>Answer</button><button type="button" disabled={disabled} onClick={() => void submit(true)}>Dismiss</button>{error && <p role="alert">{error}</p>}</form>;
}
createRoot(document.getElementById('root')!).render(<App />);

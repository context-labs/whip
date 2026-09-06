import { useEffect, useMemo, useState, type FormEvent } from 'react';
import { createRoot } from 'react-dom/client';
import { createWhipClient, isTerminal, type WhipClient, type CommandHandle, type RecoveryRecord, type RecoveryStorage, type ContentReference } from '@whip/sdk';
import { createSessionView, createSessionListView, type SessionView, type HistoryView, type DeepReadonly } from '@whip/sdk/state';
import { useSessionView, useSessionListView, useWhipConnection } from '@whip/sdk/react';

const clientId = localStorage.getItem('whip.example.clientId') ?? crypto.randomUUID();
localStorage.setItem('whip.example.clientId', clientId);
const key = (record: RecoveryRecord) => `whip.example.recovery:${record.runtimeId}:${record.clientId}:${record.commandId}`;
const storage: RecoveryStorage = {
  async list() { return Object.keys(localStorage).filter(key => key.startsWith('whip.example.recovery:')).map(key => JSON.parse(localStorage.getItem(key)!)); },
  async put(record) { localStorage.setItem(key(record), JSON.stringify(record)); },
  async delete(record) { localStorage.removeItem(key(record)); },
};
const message = (error: unknown) => error instanceof Error ? error.message : String(error);

function App() {
  const [endpoint, setEndpoint] = useState(localStorage.getItem('whip.example.endpoint') ?? 'http://127.0.0.1:8080');
  const [client, setClient] = useState<WhipClient>();
  const [error, setError] = useState('');
  useEffect(() => () => client?.close(), [client]);
  async function connect(event: FormEvent) {
    event.preventDefault(); client?.close(); setError('');
    try {
      const next = createWhipClient({ endpoint, clientId, clientKind: 'human', recoveryStorage: storage });
      setClient(next); localStorage.setItem('whip.example.endpoint', endpoint); await next.connect();
    } catch (error) { setError(message(error)); }
  }
  return <><header><span className="wordmark">WHIP</span><span className="muted">Client example</span><form onSubmit={connect}><label htmlFor="endpoint">Daemon</label><input id="endpoint" value={endpoint} onChange={event => setEndpoint(event.target.value)} required /><button>Connect</button></form></header>
    {error && <p role="alert" className="workspace">{error}</p>}
    {client ? <Connected key={endpoint + clientId + String(client === undefined)} client={client} /> : <div className="empty"><h1>Connect to your execution host</h1><p>Start WHIP with its network listener enabled, then enter the endpoint reported by daemon status.</p></div>}</>;
}
function Connected({ client }: { client: WhipClient }) {
  const connection = useWhipConnection(client);
  const list = useMemo(() => createSessionListView(client), [client]);
  const catalog = useSessionListView(list);
  const [rootId, select] = useState('');
  const [cwd, setCwd] = useState('');
  const [error, setError] = useState('');
  const [recoveries, setRecoveries] = useState<readonly RecoveryRecord[]>([]);
  useEffect(() => { void list.start(); return () => { void list.dispose(); }; }, [list]);
  useEffect(() => { void client.recoveryRecords().then(records => setRecoveries(records.filter(record => record.clientId === clientId && record.runtimeId === connection.info?.runtime_id))); }, [client, connection.info?.runtime_id]);
  async function create(event: FormEvent) {
    event.preventDefault(); setError('');
    try {
      const command = client.sessions.create({ cwd }); const outcome = await command.result();
      if (outcome.status !== 'succeeded' || !outcome.result) throw new Error(outcome.failure?.message ?? 'Session creation did not succeed');
      await client.forget(command.record); select(outcome.result.root_id); await list.refresh();
    } catch (error) { setError(message(error)); }
  }
  return <main><aside><div className="row"><h2>Sessions</h2><span className="muted" role="status">{connection.state}</span></div>
    <form onSubmit={create}><label htmlFor="cwd">Working directory on host</label><input id="cwd" value={cwd} onChange={event => setCwd(event.target.value)} placeholder="/path/to/project" required /><button disabled={connection.state !== 'connected'}>New session</button></form>
    <ul>{catalog.page?.items?.map(session => <li key={session.id}><button aria-current={rootId === session.id} onClick={() => select(session.id)}>{session.title || 'Untitled session'}<div className="path">{session.cwd}</div></button></li>)}</ul>
    {catalog.page?.has_more && <button onClick={() => void list.loadMore()}>More sessions</button>}
    <p className="muted">{connection.info?.host_platform} · {connection.info?.host_architecture}</p>
    {recoveries.length > 0 && <details><summary>Recover submitted commands ({recoveries.length})</summary>{recoveries.map(record => <button key={record.commandId} onClick={async () => { try { const outcome = await client.recover(record).status(); if (record.rootId) select(record.rootId); if (isTerminal(outcome.status)) { await client.forget(record); setRecoveries(items => items.filter(item => item.commandId !== record.commandId)); } setError(`${record.operation}: ${outcome.status}`); } catch (error) { setError(message(error)); } }}>{record.operation} · {record.commandId.slice(0, 8)}</button>)}</details>}
    {(error || catalog.error) && <p role="alert">{error || catalog.error?.message}</p>}</aside>
    {rootId ? <Conversation key={connection.info?.runtime_id + rootId} client={client} rootId={rootId} /> : <div className="empty"><h1>Your work stays on the host</h1><p>Choose a session or create one to start. Disconnecting leaves accepted work running.</p></div>}</main>;
}
function Conversation({ client, rootId }: { client: WhipClient; rootId: string }) {
  const session = useMemo(() => client.session(rootId), [client, rootId]);
  const view = useMemo(() => createSessionView(session), [session]);
  const state = useSessionView(view);
  const connection = useWhipConnection(client);
  const [agentId, setAgent] = useState(rootId);
  const [draft, setDraft] = useState(() => sessionStorage.getItem(`whip.example.draft:${rootId}`) ?? '');
  const [active, setActive] = useState<CommandHandle<'submit'>>();
  const [status, setStatus] = useState('');
  const [error, setError] = useState('');
  const [attachment, setAttachment] = useState<ContentReference>();
  useEffect(() => { void view.start(); return () => { void view.dispose(); }; }, [view]);
  useEffect(() => { sessionStorage.setItem(`whip.example.draft:${rootId}`, draft); }, [rootId, draft]);
  async function submit(event: FormEvent) {
    event.preventDefault(); setError('');
    const submittedDraft = draft;
    try {
      const text = attachment ? `${draft}\nAttached content reference: ${attachment.handle.reference_id}` : draft;
      const command = session.submit({ text }); setActive(command); setStatus('Submitting');
      await command.accepted(); setDraft(current => current === submittedDraft ? '' : current); setAttachment(current => current === attachment ? undefined : current); setStatus('Accepted');
      const outcome = await command.result(); setStatus(outcome.status);
      if (outcome.failure) setError(outcome.failure.message);
      await client.forget(command.record);
    } catch (error) { setError(message(error)); setStatus('Check command status before retrying'); }
  }
  const history = state.history[agentId];
  const presentation = agentId === rootId ? state.root?.presentation : state.root?.agent_presentations?.[agentId];
  return <section className="workspace"><div className="row"><h1>{state.root?.meta.title || 'Untitled session'}</h1><span className="muted" role="status">{state.status}</span></div>
    <div className="row"><label>Inspect agent <select value={agentId} onChange={async event => { const id = event.target.value; if (agentId !== rootId) view.closeAgent(agentId); setAgent(id); try { await view.openAgent(id); } catch (error) { setError(message(error)); } }}><option value={rootId}>Root</option>{state.root?.agents?.filter(agent => agent.id !== rootId).map(agent => <option value={agent.id} key={agent.id}>{agent.name || agent.id} · {agent.status}</option>)}</select></label><span className="path">{state.root?.meta.cwd}</span></div>
    {(state.status === 'stale' || connection.state !== 'connected') && <p className="notice">Reconnecting. Your draft stays here; accepted work stays on the host.</p>}
    {(state.truncated || state.unavailable) && <p className="notice">Some output is outside this view’s limits. Read its content reference or load history for more.</p>}
    <div className="transcript">{history?.hasMore && <button onClick={() => void view.loadOlder(agentId).catch(error => setError(message(error)))}>Load older messages</button>}
      {groupMailboxMessages(history?.messages ?? []).map(({ entry, count }) => <article key={entry.seq}>{count > 1 ? <details><summary>Mailbox update · {count} identical deliveries in loaded history</summary><pre>{entry.message!.content}</pre></details> : <><span className="role">{entry.message?.role ?? entry.role}</span>{entry.message ? <pre>{entry.message.content}</pre> : entry.body && <button onClick={async () => { try { const text = await client.content(entry.body!, { rootId, agentId }).readText({ maxBytes: 1 << 20 }); setError(text); } catch (error) { setError(message(error)); } }}>Read stored message</button>}</>}</article>)}
      {presentation?.map(event => <article key={event.seq}><span className="role">{event.kind}</span><pre>{presentationText(event.payload)}</pre></article>)}
    </div>
    {state.root?.questions?.filter(question => question.question_id).map(question => <Question key={question.question_id} id={question.question_id!} question={question.question ?? ''} options={(question.options ?? []).map(option => option.label)} multiple={question.multiple ?? false} view={view} onError={setError} />)}
    {state.root?.permissions?.filter(permission => permission.status === 'pending').map(permission => <div className="question" key={permission.id}><h2>Permission requested</h2><pre>{permission.command || permission.operation}</pre><p>{permission.canonical_path}</p><div className="row">{[true, false].map(allow => <button key={String(allow)} disabled={connection.state !== 'connected'} onClick={async () => { try { await client.permissions.decide({ root_id: rootId, permission_id: permission.id, allow }); } catch (error) { setError(message(error)); } }}>{allow ? 'Allow once' : 'Deny'}</button>)}</div></div>)}
    {(error || state.error) && <p role="alert">{error || state.error?.message}</p>}
    <form className="composer" onSubmit={submit}><label htmlFor="prompt">Message the root agent</label><textarea id="prompt" value={draft} onChange={event => setDraft(event.target.value)} placeholder="What would you like to work on?" /><div className="row"><label>Attach file <input type="file" disabled={connection.state !== 'connected'} onChange={async event => { const file = event.target.files?.[0]; if (!file) return; try { setAttachment(await client.upload(new Uint8Array(await file.arrayBuffer()), { rootId, mediaType: file.type || 'application/octet-stream' })); } catch (error) { setError(message(error)); } }} /></label><button className="primary" disabled={connection.state !== 'connected' || !draft.trim()}>Send</button></div>
      {attachment && <button type="button" onClick={async () => { try { setError(await attachment.readText({ maxBytes: 1 << 20 })); } catch (error) { setError(message(error)); } }}>Read attachment · {attachment.handle.size} bytes</button>}
      <div className="row"><span className="muted" role="status">{status}</span>{active && !isTerminal(status) && <button type="button" onClick={async () => { try { await active.cancel().result(); setStatus('Cancellation requested'); } catch (error) { setError(message(error)); } }}>Cancel this command</button>}</div>
    </form></section>;
}
// Group repeated internal delivery attempts only in the example's presentation.
// The SDK retains every transcript sequence, including identical authored messages.
function groupMailboxMessages(messages: DeepReadonly<HistoryView['messages']>) {
  const groups: { entry: (typeof messages)[number]; count: number }[] = [];
  const isDigest = (entry: (typeof messages)[number]) => entry.message?.role === 'user'
    && !entry.message.authored && entry.message.content.startsWith('Mailbox digest:');
  for (const entry of messages) {
    const previous = groups[groups.length - 1];
    if (previous && isDigest(entry) && isDigest(previous.entry) && entry.message!.content === previous.entry.message!.content) previous.count++;
    else groups.push({ entry, count: 1 });
  }
  return groups;
}
function presentationText(payload: unknown): string {
  if (payload && typeof payload === 'object') {
    const value = payload as Record<string, unknown>;
    for (const key of ['text', 'result', 'args', 'command']) if (typeof value[key] === 'string') return value[key];
  }
  return JSON.stringify(payload, null, 2);
}
function Question({ id, question, options, multiple, view, onError }: { id: string; question: string; options: readonly string[]; multiple: boolean; view: SessionView; onError(error: string): void }) {
  const [answer, setAnswer] = useState<string[]>([]);
  const [text, setText] = useState('');
  return <form className="question" onSubmit={async event => { event.preventDefault(); try { await view.session.answerQuestion(id, options.length ? answer : [text]).result(); } catch (error) { onError(message(error)); } }}><h2>{question}</h2>
    {options.map(option => <label key={option}><input type={multiple ? 'checkbox' : 'radio'} name={id} checked={answer.includes(option)} onChange={event => setAnswer(multiple ? event.target.checked ? [...answer, option] : answer.filter(value => value !== option) : [option])} /> {option}</label>)}
    {!options.length && <input aria-label="Your answer" value={text} onChange={event => setText(event.target.value)} />}
    <button>Answer</button></form>;
}
createRoot(document.getElementById('root')!).render(<App />);

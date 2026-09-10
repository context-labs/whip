import { expect, it } from 'vitest';
import { AppRuntime } from '../src/runtime';
import { welcomeDraftKey } from '../src/welcome-submission';

function fixture() {
  const values = new Map<string, string>();
  const storage = { keys: () => [...values.keys()], getItem: (key: string) => values.get(key) ?? null, setItem: (key: string, value: string) => { values.set(key, value); }, removeItem: (key: string) => { values.delete(key); } };
  const runtime = new AppRuntime({ defaultEndpoint: 'http://localhost:8080', storage, windowStorage: storage, copy: async () => {}, download: async () => {}, openExternal: async () => {} });
  return runtime;
}

it('keeps a plain legacy import discoverable at capacity and recovers after closing a tab without reload', async () => {
  const app = fixture();
  try {
    for (let i = 0; i < 32; i++) app.tabs.open('host', `root-${i}`);
    app.setDraft('host:welcome:prompt', 'Legacy unfinished task'); app.flushDrafts();
    const id = (await app.welcome.importLegacy('host'))!;
    expect(() => app.recoverWelcome(id)).toThrow('32');
    expect(app.orphanWelcomeDrafts()).toEqual([id]);
    expect(app.welcome.list()).toEqual([]);
    app.tabs.closeViews([app.tabs.workspace().tabs[0]!.id]);
    const restored = app.recoverWelcome(id);
    expect(restored).toMatchObject({ id, kind: 'new', runtimeId: 'host' });
    expect(app.draft(welcomeDraftKey(restored.id))).toBe('Legacy unfinished task');
    expect(app.orphanWelcomeDrafts()).toEqual([]);
  } finally { app.dispose(); }
});

it('recovers newer programmatic text left after promotion into a separate draft without overwriting the session', () => {
  const app = fixture();
  try {
    const tab = app.tabs.openNew({ runtimeId: 'host' });
    app.setDraft(welcomeDraftKey(tab.id), 'A newer revision'); app.flushDrafts();
    app.tabs.promoteNew(tab.id, 'host', 'created');
    expect(app.orphanWelcomeDrafts()).toEqual([tab.id]);
    const recovered = app.recoverWelcome(tab.id);
    expect(recovered.kind).toBe('new'); expect(recovered.id).not.toBe(tab.id);
    expect(app.tabs.workspace().tabs.find(item => item.id === tab.id)).toMatchObject({ kind: 'chat', rootId: 'created' });
    expect(app.draft(welcomeDraftKey(recovered.id))).toBe('A newer revision');
    expect(app.draft(welcomeDraftKey(tab.id))).toBe('');
    expect(app.orphanWelcomeDrafts()).toEqual([]);
  } finally { app.dispose(); }
});

it('retains orphan text when recovery capacity or copying fails', () => {
  const app = fixture();
  try {
    const tab = app.tabs.openNew({ runtimeId: 'host' });
    app.setDraft(welcomeDraftKey(tab.id), 'Keep this task'); app.flushDrafts();
    app.tabs.promoteNew(tab.id, 'host', 'created');
    for (let i = 1; i < 32; i++) app.tabs.open('host', `root-${i}`);
    expect(() => app.recoverWelcome(tab.id)).toThrow('32');
    expect(app.draft(welcomeDraftKey(tab.id))).toBe('Keep this task');
    expect(app.orphanWelcomeDrafts()).toEqual([tab.id]);
  } finally { app.dispose(); }
});

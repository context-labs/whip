import { describe, expect, it } from 'vitest';
import { RecoveryJournal, recoveryNamespace, type RecoveryRecord } from '@whip/sdk';
import { recoveryStorage } from '../src/recovery-storage';
import type { AppStorage } from '../src/platform';

function fixture() {
  const values = new Map<string, string>();
  let serial: Promise<unknown> = Promise.resolve();
  const storage: AppStorage = {
    keys: () => [...values.keys()], getItem: key => values.get(key) ?? null,
    setItem: (key, value) => { values.set(key, value); }, removeItem: key => { values.delete(key); },
    transaction: (_key, update) => {
      const next = serial.then(update); serial = next.catch(() => {}); return next;
    },
  };
  return { values, storage };
}
const record = (id: string, accepted = false): RecoveryRecord => ({ namespace: recoveryNamespace, version: 1, runtimeID: 'runtime', clientID: 'human', accepted,
  request: JSON.stringify({ method: 'sessions.submit', params: { session_id: 'owner', identity: { client_id: 'human', request_id: id }, source: 'user', parts: [{ type: 'text', text: 'saved exact input' }] } }) });

describe('v4 app recovery storage', () => {
  it('coordinates two window journals at capacity without evicting unresolved work', async () => {
    const { storage, values } = fixture();
    values.set('whip.web.recovery.v1', 'old identities are retained');
    const first = new RecoveryJournal(recoveryStorage(storage)), second = new RecoveryJournal(recoveryStorage(storage));
    for (let index = 0; index < 63; index++) await first.put(record(`original-${index}`));
    const outcomes = await Promise.allSettled([first.put(record('window-one')), second.put(record('window-two'))]);
    expect(outcomes.filter(outcome => outcome.status === 'fulfilled')).toHaveLength(1);
    const saved = await first.list();
    expect(saved).toHaveLength(64);
    for (let index = 0; index < 63; index++) expect(saved.some(value => value.request === record(`original-${index}`).request)).toBe(true);
    expect(values.get('whip.web.recovery.v1')).toBe('old identities are retained');
    await second.forget(saved[0]);
    await second.put(record('explicit-space'));
    expect(await first.list()).toHaveLength(64);
  });
  it('never downgrades known acceptance or replaces a colliding request', async () => {
    const { storage } = fixture();
    const first = new RecoveryJournal(recoveryStorage(storage)), second = new RecoveryJournal(recoveryStorage(storage));
    await first.put(record('same', true));
    await second.put(record('same', false));
    expect((await second.list())[0].accepted).toBe(true);
    const changed = { ...record('same'), request: record('same').request.replace('saved exact input', 'different input') };
    await expect(second.put(changed)).rejects.toThrow('different request');
    expect((await first.list())[0].request).toBe(record('same').request);
  });
  it('fails before claimed durability when locking or persistent writes are unavailable', async () => {
    const { storage, values } = fixture();
    delete storage.transaction;
    await expect(new RecoveryJournal(recoveryStorage(storage)).put(record('first'))).rejects.toThrow('locking');
    expect(values.size).toBe(0);
    let persistent = true;
    const fallback: AppStorage = { ...storage, get persistent() { return persistent; }, transaction: async (_key, update) => update(),
      setItem(key, value) { values.set(key, value); persistent = false; } };
    await expect(new RecoveryJournal(recoveryStorage(fallback)).put(record('second'))).rejects.toThrow('could not be saved');
    const memory: AppStorage = { ...storage, persistent: false };
    await new RecoveryJournal(recoveryStorage(memory)).put(record('memory'));
  });
  it('preserves corrupt, excessive and foreign records for deliberate recovery', async () => {
    const { storage, values } = fixture();
    for (const raw of ['{', '[]'.repeat(5 << 20), JSON.stringify([{ key: 'foreign', record: { namespace: 'old' } }])]) {
      values.set('whip.web.recovery.v4', raw);
      const journal = new RecoveryJournal(recoveryStorage(storage));
      await expect(journal.list()).rejects.toThrow();
      await expect(journal.put(record('new'))).rejects.toThrow();
      expect(values.get('whip.web.recovery.v4')).toBe(raw);
    }
  });
});

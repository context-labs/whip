import { describe, expect, it } from 'vitest';
import { hostsStorageKey, localProfile, readConnections, saveConnections, selectedHostStorageKey, urlProfile, validateProfile } from '../src/connections';
import type { AppStorage } from '../src/platform';

function storage(entries: [string, string][] = []): AppStorage {
  const values = new Map(entries);
  return { keys: () => [...values.keys()], getItem: key => values.get(key) ?? null,
    setItem: (key, value) => { values.set(key, value); }, removeItem: key => { values.delete(key); } };
}

describe('execution host profiles', () => {
  it('migrates legacy hosts once without changing client, drafts or rollback records', () => {
    const old = JSON.stringify(['https://host.example', 'https://host.example/', 'javascript:bad']);
    const disk = storage([['whip.web.hosts.v1', old], ['whip.web.endpoint', '"https://host.example"'],
      ['whip.web.client.v1', 'same-client'], ['whip.web.draft.v1:runtime:root:agent', 'unsent']]);
    const migrated = readConnections(disk, localProfile, ['local', 'url', 'ssh']);
    expect(migrated.hosts).toHaveLength(1);
    expect(migrated.selected.target).toEqual({ kind: 'url', endpoint: 'https://host.example/' });
    expect(migrated.notice).toContain('invalid');
    expect(disk.getItem('whip.web.hosts.v1')).toBe(old);
    expect(disk.getItem('whip.web.client.v1')).toBe('same-client');
    expect(disk.getItem('whip.web.draft.v1:runtime:root:agent')).toBe('unsent');
    disk.setItem('whip.web.endpoint', '"http://old.example"');
    expect(readConnections(disk, localProfile, ['local', 'url']).selected).toEqual(migrated.selected);
  });

  it('does not silently attach to This Mac when a saved host is corrupt or unavailable', () => {
    const legacy = storage([['whip.web.endpoint', '"javascript:invalid"'], ['whip.web.hosts.v1', '["https://valid.example"]']]);
    const migration = readConnections(legacy, localProfile, ['local', 'url']);
    expect(migration.needsSelection).toBe(true);
    expect(legacy.getItem(selectedHostStorageKey)).toBeNull();
    expect(legacy.getItem('whip.web.endpoint')).toBe('"javascript:invalid"');
    for (const raw of ['broken', 'null', '{}', JSON.stringify(Array.from({ length: 17 }, () => localProfile))]) {
      const disk = storage([[hostsStorageKey, raw]]);
      const result = readConnections(disk, localProfile, ['local']);
      expect(result.needsSelection).toBe(true);
      expect(disk.getItem(hostsStorageKey)).toBe(raw);
    }
    const disk = storage();
    const remote = validateProfile({ id: 'ssh:test', label: 'Server', target: { kind: 'ssh', host: 'example' } });
    saveConnections(disk, [], remote);
    expect(readConnections(disk, urlProfile('https://local.example'), ['url']).needsSelection).toBe(true);
    disk.setItem(selectedHostStorageKey, '"missing-profile"');
    expect(readConnections(disk, localProfile, ['local', 'ssh']).needsSelection).toBe(true);
  });

  it('bounds saved profiles and retains stable runtime identity', () => {
    const disk = storage();
    let hosts = [] as ReturnType<typeof urlProfile>[];
    for (let i = 0; i < 20; i++) hosts = saveConnections(disk, hosts, urlProfile(`https://host-${i}.example`));
    const selected = { ...hosts[0]!, runtimeId: 'verified-runtime' };
    saveConnections(disk, hosts, selected);
    const restored = readConnections(disk, localProfile, ['url', 'local']);
    expect(restored.hosts).toHaveLength(16);
    expect(restored.selected.runtimeId).toBe('verified-runtime');
  });

  it('requires selection after partial writes or null records while leaving a fresh store alone', () => {
    const host = urlProfile('https://previous.example');
    const records: [string, string][][] = [
      [[hostsStorageKey, JSON.stringify([host])]],
      [[hostsStorageKey, JSON.stringify([host])], [selectedHostStorageKey, 'null']],
      [[hostsStorageKey, '[]']],
      [[selectedHostStorageKey, JSON.stringify(host.id)]],
      [[selectedHostStorageKey, 'null']],
      [['whip.web.hosts.v1', '["https://previous.example"]']],
      [['whip.web.hosts.v1', '["https://previous.example"]'], ['whip.web.endpoint', 'null']],
      [['whip.web.endpoint', 'null']],
    ];
    for (const entries of records) {
      const disk = storage(entries);
      const restored = readConnections(disk, localProfile, ['local', 'url']);
      expect(restored.needsSelection, JSON.stringify(entries)).toBe(true);
      expect(disk.keys().map(key => [key, disk.getItem(key)])).toEqual(entries);
    }
    const fresh = storage([['whip.web.client.v1', 'existing-client']]);
    expect(readConnections(fresh, localProfile, ['local', 'url'])).toEqual({ hosts: [], selected: localProfile });
    expect(fresh.getItem(hostsStorageKey)).toBeNull();
  });

  it('rejects credentials, fragments, option injection and malformed SSH fields', () => {
    for (const endpoint of ['file:///tmp/socket', 'javascript:alert(1)', 'https://user:secret@host', 'https://host/#secret', 'http://host\n'])
      expect(() => urlProfile(endpoint)).toThrow();
    for (const target of [{ host: '-oProxyCommand=bad' }, { host: 'host', user: '-root' }, { host: 'host', port: 0 },
      { host: 'host', port: 22.5 }, { host: 'host', remoteExecutable: '/bin/whip\nwhoami' }])
      expect(() => validateProfile({ id: 'ssh:test', label: 'Host', target: { kind: 'ssh', ...target } })).toThrow();
    const profile = validateProfile({ id: 'ssh:test', label: 'Host', password: 'not persisted',
      target: { kind: 'ssh', host: '[::1]', port: 2222, password: 'not persisted', remoteExecutable: '/a path/whip' } });
    expect(JSON.stringify(profile)).not.toContain('password');
    expect(profile.target).toEqual({ kind: 'ssh', host: '[::1]', port: 2222, remoteExecutable: '/a path/whip' });
  });
});

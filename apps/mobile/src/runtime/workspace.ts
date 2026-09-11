import { MobileRuntime, type SavedHost } from './runtime';
import type { MobileStorage } from './storage';

export type WorkspaceSnapshot = { hosts: readonly SavedHost[]; connections: readonly MobileRuntime[]; selectedHostId?: string; active: boolean; revision: number };
/** Device owner. Each existing MobileRuntime retains its isolated command/view engine. */
export class MobileWorkspace {
  readonly settings: MobileRuntime;
  private state: WorkspaceSnapshot = { hosts: [], connections: [], active: true, revision: 0 };
  private listeners = new Set<() => void>();
  private runtimes = new Map<string, MobileRuntime>();
  private unsubscribers = new Map<string, () => void>();
  private attempts = new Map<string, number>();
  private disposed = false;
  private generation = 0;
  private profiles: Promise<void> = Promise.resolve();
  constructor(readonly storage: MobileStorage, private readonly makeRuntime = (storage: MobileStorage) => new MobileRuntime(storage)) { this.settings = makeRuntime(storage); }
  getSnapshot = () => this.state;
  subscribe = (fn: () => void) => { this.listeners.add(fn); return () => { this.listeners.delete(fn); }; };
  private update(patch: Partial<WorkspaceSnapshot> = {}) { this.state = Object.freeze({ ...this.state, ...patch, connections: [...this.runtimes.values()], revision: this.state.revision + 1 }); for (const fn of this.listeners) fn(); }
  async start() {
    await this.settings.start(false);
    const hosts = this.settings.getSnapshot().hosts;
    const selectedHostId = await this.storage.get<string>('settings', 'selectedHost');
    const remembered = await this.storage.get<string[]>('settings', 'connectedHosts');
    this.update({ hosts, selectedHostId });
    const ids = new Set(remembered ?? (selectedHostId ? [selectedHostId] : []));
    void (async () => {
      const selected = hosts.filter(h => ids.has(h.id));
      for (let i = 0; i < selected.length && !this.disposed; i += 2) await Promise.all(selected.slice(i, i + 2).map(host => this.connect(host, false).catch(() => {})));

    })().catch(this.settings.report);
  }
  runtime(id?: string) { return id ? this.runtimes.get(id) : undefined; }
  sessionRuntime(runtimeId: string, hostId?: string) {
    const matches = [...this.runtimes.values()].filter(r => r.getSnapshot().host?.runtimeId === runtimeId && (!hostId || r.getSnapshot().host?.id === hostId));
    return matches.length === 1 ? matches[0] : undefined;
  }
  selectedRuntime() { return this.runtime(this.state.selectedHostId); }
  newHost(url: string, name = '') { return this.settings.newHost(url, name); }
  private refreshProfiles() {
    const work = this.profiles.catch(() => {}).then(async () => {
      const hosts = (await this.storage.list<SavedHost>('hosts')).map(r => r.value);
      this.settings.setHostProfiles(hosts);
      for (const runtime of this.runtimes.values()) runtime.setHostProfiles(hosts);
      this.update({ hosts });
    });
    this.profiles = work; return work;
  }
  private async remember() { await this.storage.set('settings', 'connectedHosts', [...this.runtimes.keys()]); }
  async connect(host: SavedHost, select = true) {
    if (this.disposed) throw new Error('Whip is closed.');
    if (!this.state.hosts.some(h => h.id === host.id) && new Set([...this.state.hosts.map(h => h.id), ...this.runtimes.keys(), ...this.attempts.keys()]).size >= 4) throw new Error('Keep up to four hosts on this phone.');
    if (this.state.hosts.some(h => h.id !== host.id && h.url === host.url)) throw new Error('This address is already saved. Edit the existing host.');
    const current = this.runtimes.get(host.id);
    if (current?.getSnapshot().ready && current.getSnapshot().host?.url === host.url && current.getSnapshot().host?.name === host.name) {
      if (select) await this.select(host.id); return current;
    }
    const generation = ++this.generation; this.attempts.set(host.id, generation);
    if (current) { this.unsubscribers.get(host.id)?.(); this.runtimes.delete(host.id); await current.dispose(false); }
    if (this.disposed || this.attempts.get(host.id) !== generation) throw new Error('Connection cancelled.');
    const runtime = this.makeRuntime(this.storage); runtime.setActive(this.state.active);
    await runtime.start(false);
    if (this.disposed || this.attempts.get(host.id) !== generation) { await runtime.dispose(false); throw new Error('Connection cancelled.'); }
    this.runtimes.set(host.id, runtime); this.unsubscribers.set(host.id, runtime.subscribe(() => this.update())); this.update();
    try {
      await runtime.connect(host, { select: false, list: false });
      if (this.attempts.get(host.id) !== generation) throw new Error('Connection cancelled.');
      const runtimeId = runtime.getSnapshot().host?.runtimeId;
      if ([...this.runtimes.values()].some(other => other !== runtime && other.getSnapshot().host?.runtimeId === runtimeId && other.getSnapshot().client)) {
        await runtime.detach(); runtime.report(new Error('This runtime is already connected through another host.')); throw new Error('This runtime is already connected through another host.');
      }
      if (select) await this.remember();
      if (select) await this.select(host.id);
      return runtime;
    } finally { await this.refreshProfiles(); }
  }
  async select(id: string) { if (!this.runtimes.has(id)) throw new Error('Connect this host first.'); await this.storage.set('settings', 'selectedHost', id); this.update({ selectedHostId: id }); }
  async disconnect(id: string) {
    this.attempts.delete(id);
    const runtime = this.runtimes.get(id); this.unsubscribers.get(id)?.(); this.unsubscribers.delete(id); this.runtimes.delete(id); this.update();
    await runtime?.dispose(false); await this.remember();
  }
  async removeHost(id: string) { await this.disconnect(id); await this.storage.delete('hosts', id); if (this.state.selectedHostId === id) { await this.storage.delete('settings', 'selectedHost'); this.update({ selectedHostId: undefined }); } await this.refreshProfiles(); }
  setActive(active: boolean) { this.settings.setActive(active); for (const runtime of this.runtimes.values()) runtime.setActive(active); this.update({ active }); }
  async dispose() { this.disposed = true; for (const fn of this.unsubscribers.values()) fn(); await Promise.all([...this.runtimes.values()].map(r => r.dispose(false))); await this.settings.dispose(false); await this.storage.close(); this.runtimes.clear(); this.listeners.clear(); }
}

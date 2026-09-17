// Crosses the real renderer/preload/IPC boundary: permissive platform mocks miss exact-key serialization bugs.
import { BrowserWorkspace } from '../../../packages/app/src/browser-workspace';
import { SessionTabs } from '../../../packages/app/src/session-tabs';
import type { DesktopBridge } from '@whip/app/desktop-bridge';
const check = (value: unknown, message: string) => { if (!value) throw new Error(message); };
(globalThis as any).runWorkspaceBoundary = async () => {
  const platform = ((globalThis as any).whipDesktop as DesktopBridge).browser!;
  const tabs = new SessionTabs();
  const saved = tabs.openBrowser({ id: 'workspace-restored', url: 'about:blank', titleHint: 'Saved address', environmentId: 'metadata-only-environment' });
  const errors: string[] = [];
  const workspace = new BrowserWorkspace(platform, tabs, error => errors.push(String(error)));
  try {
    const initial = await platform.snapshot();
    const rawRejected = await platform.restore({ epoch: initial.epoch, tabs: [saved] }).then(() => false, () => true);
    check(rawRejected, 'Native restore must still reject raw workspace kind/extra keys');
    await workspace.start();
    const restored = workspace.getSnapshot().tabs.find(tab => tab.id === saved.id);
    check(restored?.url === 'about:blank' && restored.environmentId === saved.environmentId && restored.title === saved.titleHint, 'Persisted address/title/environment metadata did not survive native restore');
    check(restored.status === 'restored', 'Metadata-only restore must not realize preview authority');
    const created = await workspace.create('about:blank');
    await workspace.act(created.id, { kind: 'zoom', factor: 1 }); // Wait for post-admission metadata sync too.
    check(workspace.getSnapshot().tabs.some(tab => tab.id === created.id), 'First New Browser did not survive renderer admission/restore synchronization');
    check(errors.length === 0, 'Workspace/native seam errors: ' + errors.join('; '));
    return { ok: true, rawRejected, restored: saved.id, created: created.id };
  } finally {
    workspace.dispose();
    const inventory = await platform.snapshot();
    for (const tab of inventory.tabs) await platform.close({ epoch: inventory.epoch, tabId: tab.id, generation: tab.generation });
  }
};

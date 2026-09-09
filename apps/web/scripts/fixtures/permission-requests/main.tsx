import { useState } from 'react';
import { createRoot } from 'react-dom/client';
import * as stylex from '@stylexjs/stylex';
import { initializeTheme, ThemeProvider, UIProvider } from '@whip/ui';
import { colors, scale, typography } from '@whip/ui/tokens.stylex';
import '@whip/ui/reset.css';
import '@whip/ui/fonts.css';
import type { Session } from '@whip/sdk';
import type { RootSnapshot } from '@whip/protocol';
import { PendingRequests } from '../../../../../packages/app/src/requests';
import { Composer } from '../../../../../packages/app/src/composer';
import { CompositionStore } from '../../../../../packages/app/src/compositions';
import { RuntimeContext } from '../../../../../packages/app/src/context';
import type { AppRuntime } from '../../../../../packages/app/src/runtime';

// Actual shared controls, with synthetic requests and no daemon or command execution.
const query = new URLSearchParams(location.search);
localStorage.setItem('whip.appearance.theme.v1', JSON.stringify({ version: 1, id: query.get('theme') ?? 'dark' }));
initializeTheme({ storage: localStorage });
const snapshot = { commands: [] };
const runtime = {
  compositions: new CompositionStore(),
  getSnapshot: () => snapshot, subscribe: () => () => {},
  draft: () => '', subscribeDraft: () => () => {},
  report: (error: unknown) => { throw error; },
} as unknown as AppRuntime;
const initial = {
  agents: [{ id: 'root:explorer', name: query.has('long') ? 'Explore repository '.repeat(12) : 'Explore repository' }],
  permissions: [
    { id: 'first', agent_id: 'root:explorer', status: 'pending', operation: 'bash', rule: 'bash:go test *',
      command: query.has('long') ? `go test ${'./internal/very-long-package-name'.repeat(30)}\n`.repeat(16) : 'go test ./internal/permission/...'},
    { id: 'second', agent_id: 'root', status: 'pending', operation: 'read', canonical_path: '/project/README.md', rule: 'read:/project/*' },
  ],
} as RootSnapshot;

function Fixture() {
  const [root, setRoot] = useState(initial);
  const [session] = useState(() => {
    let resolved: string | undefined;
    return {
      value: { rootId: 'root', client: { permissions: { decide: async ({ permission_id }: { permission_id: string }) => { resolved = permission_id; } } } } as unknown as Session,
      refresh: async () => setRoot(previous => ({ ...previous, permissions: previous.permissions!.filter(item => item.id !== resolved) })),
    };
  });
  return <RuntimeContext.Provider value={runtime}><ThemeProvider storage={localStorage}><UIProvider>
    <main {...stylex.props(styles.page)}>
      <div {...stylex.props(styles.conversation)}><p>The agent is ready to run the focused permission tests.</p></div>
      <PendingRequests root={root} session={session.value} disabled={false} refresh={session.refresh} />
      <Composer session={session.value} agentId="root" runtimeId="fixture" connected />
    </main>
  </UIProvider></ThemeProvider></RuntimeContext.Provider>;
}
const styles = stylex.create({
  page: { height: '100dvh', display: 'flex', flexDirection: 'column', overflowY: 'auto', backgroundColor: colors.background, color: colors.foreground, fontFamily: typography.sans, fontSize: 14 },
  conversation: { flex: 1, minHeight: 64, width: '100%', maxWidth: 864, alignSelf: 'center', padding: { default: scale.space6, [scale.phone]: scale.space3 } },
});
createRoot(document.getElementById('root')!).render(<Fixture />);

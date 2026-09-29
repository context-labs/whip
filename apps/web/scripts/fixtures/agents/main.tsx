// Actual agent editor with the full host catalog; no daemon or registration side effects.
import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { defaultDisplayPreferences, displayStorageKey, initializeTheme, ThemeProvider, UIProvider } from '@whip/ui';
import type { WhipClient } from '@whip/sdk';
import type { Definition } from '@whip/sdk/agents';
import type { ReactNode } from 'react';
import * as stylex from '@stylexjs/stylex';
import { scale } from '@whip/ui/tokens.stylex';
import '@whip/ui/fonts.css';
import '@whip/ui/reset.css';
import { RuntimeContext } from '../../../../../packages/app/src/context';
import type { AppRuntime } from '../../../../../packages/app/src/runtime';
import { AgentsSettings } from '../../../../../packages/app/src/settings/agents';

const query = new URLSearchParams(location.search);
localStorage.setItem('whip.appearance.theme.v1', JSON.stringify({ version: 1, id: query.get('theme') ?? 'dark' }));
localStorage.setItem(displayStorageKey, JSON.stringify({ version: 1, display: { ...defaultDisplayPreferences, uiSize: Number(query.get('size') ?? 13) } }));
initializeTheme({ storage: localStorage });

const coding: Definition = {
  id: 'coding',
  instructions: { persona: 'You are a coding agent.', rules: 'Rules.', project_files: ['CLAUDE.md'], skill_discovery: true, standing_instructions: true },
  modules: ['context', 'files', 'shell', 'browser', 'computer', 'models', 'agents', 'messages', 'mcp', 'state', 'artifacts', 'schedules', 'permissions', 'user'], capabilities: ['read', 'write', 'shell', 'browser', 'computer', 'mcp'],
  model: { model: '', provider: '', effort: '' }, compaction: { model: '', provider: '', threshold: 0 }, mcp: { servers: null },
  tools: null, output: null, children: {}, surface: { auto_title: true, goal_loop: true }, hooks: null,
};
const triage: Definition = { ...coding, id: 'support-triage', instructions: { ...coding.instructions, persona: 'You triage tickets.', project_files: null }, modules: ['context', 'files'], capabilities: ['read'], surface: { auto_title: true, goal_loop: false } };

const registered = [{ id: 'support-triage', revision: 'b'.repeat(64), built_in: false, registered_by: 'app', created_at: '2026-09-11T00:00:00Z' }];
const client = {
  getSnapshot: () => ({ state: 'connected', info: { runtime_id: 'host-a' } }),
  supports: () => true,
  agents: {
    list: async () => ({ items: [{ id: 'coding', revision: '', built_in: true, registered_by: '', created_at: '' }, ...registered] }),
    get: async (id: string) => ({ definition: id === 'coding' ? coding : triage, revision: id === 'coding' ? '' : 'b'.repeat(64), built_in: id === 'coding', registered_by: '', created_at: '' }),
    register: async (definition: Definition) => {
      registered.push({ id: definition.id, revision: 'c'.repeat(64), built_in: false, registered_by: 'app', created_at: '2026-09-11T00:00:01Z' });
      return { id: definition.id, revision: 'c'.repeat(64), created: true };
    },
  },
} as unknown as WhipClient;

const queries = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } });
const runtime = { queries, report: () => {}, getSnapshot: () => ({ commands: [] }), subscribe: () => () => {} } as unknown as AppRuntime;

function Harness(children: ReactNode) {
  return (
    <RuntimeContext.Provider value={runtime}>
      <ThemeProvider storage={localStorage}>
        <UIProvider>
          <QueryClientProvider client={queries}>{children}</QueryClientProvider>
        </UIProvider>
      </ThemeProvider>
    </RuntimeContext.Provider>
  );
}

const styles = stylex.create({ page: { maxWidth: 960, marginInline: 'auto', padding: scale.space4 } });

const element = document.getElementById('root');
if (!element) throw new Error('root missing');
createRoot(element).render(
  <StrictMode>
    {Harness(
      <div {...stylex.props(styles.page)}>
        <AgentsSettings client={client} enabled />
      </div>,
    )}
  </StrictMode>,
);

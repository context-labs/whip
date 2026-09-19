import { createRoot } from 'react-dom/client';
import { createRootRoute, createRouter, RouterProvider } from '@tanstack/react-router';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import * as stylex from '@stylexjs/stylex';
import { initializeTheme, ThemeProvider, UIProvider } from '@whip/ui';
import '@whip/ui/reset.css';
import '@whip/ui/fonts.css';
import { SessionTopBar } from '../../../../../packages/app/src/session-top-bar';
import { BrowserView } from '../../../../../packages/app/src/browser-view';
import { BrowserProviderControls } from '../../../../../packages/app/src/browser-provider-controls';
import { RuntimeContext } from '../../../../../packages/app/src/context';
import type { AppRuntime } from '../../../../../packages/app/src/runtime';
import type { BrowserTab } from '../../../../../packages/app/src/session-tabs';

// Real renderer controls, inert native bridge: no daemon, network grants, or page navigation.
const query = new URLSearchParams(location.search);
localStorage.setItem('whip.appearance.theme.v1', JSON.stringify({ version: 1, id: query.get('theme') ?? 'dark' }));
initializeTheme({ storage: localStorage });
const tab: BrowserTab = { kind: 'browser', id: 'browser_fixture', titleHint: 'Browser', url: 'http://localhost:3000/', ...(query.has('ssh') ? { environmentId: 'preview_fixture' } : {}) };
const inventory = { tabs: [{ ...tab, status: 'ready', loading: query.has('loading'), canGoBack: true, canGoForward: false, zoomFactor: 1 }] };
const snapshot = { hosts: [] }, associations: unknown[] = [];
const actions: unknown[] = [];
Object.assign(window, { browserActions: actions });
const subscribe = () => () => {};
const runtime = {
  getSnapshot: () => snapshot, subscribe,
  browser: { subscribe, getSnapshot: () => inventory, register: subscribe, onEvent: subscribe,
    act: async (_id: string, action: unknown) => { actions.push(action); } },
  browserAssociations: { subscribe, getSnapshot: () => associations },
  platform: { browser: query.has('unavailable') ? undefined : { createPreview: async () => {} }, browserAgent: !query.has('unavailable'), copy: async () => {}, openExternal: async () => {} },
} as unknown as AppRuntime;
const queries = new QueryClient();
function Fixture() {
  return <RuntimeContext.Provider value={runtime}><QueryClientProvider client={queries}><ThemeProvider storage={localStorage}><UIProvider>
    <div {...stylex.props(styles.shell)}>
      <SessionTopBar host="This Mac" cwd="/workspace/whip" kind="chat" activity="Idle" onRepl={() => {}} onTrace={() => {}} onDetails={() => {}}/>
      <main {...stylex.props(styles.page)}><BrowserView tab={tab} attachmentControls={<BrowserProviderControls tabId={tab.id}/>}/></main>
    </div>
  </UIProvider></ThemeProvider></QueryClientProvider></RuntimeContext.Provider>;
}
const styles = stylex.create({ shell: { display: 'flex', flexDirection: 'column', height: '100dvh' }, page: { flex: 1, minHeight: 0 } });
const router = createRouter({ routeTree: createRootRoute({ component: Fixture }) });
createRoot(document.getElementById('root')!).render(<RouterProvider router={router}/>);

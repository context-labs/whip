import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { ThemeProvider, UIProvider, defaultDisplayPreferences, displayStorageKey } from '@whip/ui';
import { WorkspaceTabsFixture } from '../../../stories/WorkspaceTabs.fixture';
import '@whip/ui/reset.css';
import '@whip/ui/fonts.css';
const params = new URLSearchParams(location.search);
createRoot(document.getElementById('root')!).render(<StrictMode><ThemeProvider initialTheme={params.get('theme') ?? 'light'} storage={{getItem: key => params.has('reduce') && key === displayStorageKey ? JSON.stringify({version: 1, display: {...defaultDisplayPreferences, motion: 'reduce'}}) : null, setItem: () => {}}}><UIProvider><WorkspaceTabsFixture many={params.has('many')} links={!params.has('buttons')} rtl={params.has('rtl')}/></UIProvider></ThemeProvider></StrictMode>);

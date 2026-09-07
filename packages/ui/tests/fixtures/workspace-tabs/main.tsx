import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { ThemeProvider, UIProvider } from '@whip/ui';
import { WorkspaceTabsFixture } from '../../../stories/WorkspaceTabs.fixture';
import '@whip/ui/reset.css';
import '@whip/ui/fonts.css';
const params = new URLSearchParams(location.search);
createRoot(document.getElementById('root')!).render(<StrictMode><ThemeProvider initialTheme={params.get('theme') ?? 'light'}><UIProvider><WorkspaceTabsFixture many={params.has('many')} links={!params.has('buttons')} rtl={params.has('rtl')}/></UIProvider></ThemeProvider></StrictMode>);

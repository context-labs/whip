import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { ThemeProvider, UIProvider } from '@whip/ui';
import { WorkspaceLayoutFixture } from '../../../stories/WorkspaceLayout.fixture';
import { ExternalFixture } from './external';
import '@whip/ui/reset.css';
import '@whip/ui/fonts.css';
const params = new URLSearchParams(location.search);
createRoot(document.getElementById('root')!).render(<StrictMode><ThemeProvider initialTheme={params.get('theme') ?? 'dark'}><UIProvider>{params.has('external') ? <ExternalFixture params={params}/> : <WorkspaceLayoutFixture/>}</UIProvider></ThemeProvider></StrictMode>);

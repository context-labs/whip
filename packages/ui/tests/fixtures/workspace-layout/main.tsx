import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { ThemeProvider, UIProvider } from '@whip/ui';
import { WorkspaceLayoutFixture } from '../../../stories/WorkspaceLayout.fixture';
import '@whip/ui/reset.css';
import '@whip/ui/fonts.css';
const params = new URLSearchParams(location.search);
createRoot(document.getElementById('root')!).render(<StrictMode><ThemeProvider initialTheme={params.get('theme') ?? 'dark'}><UIProvider><WorkspaceLayoutFixture/></UIProvider></ThemeProvider></StrictMode>);

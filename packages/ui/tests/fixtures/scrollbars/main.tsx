import { createRoot } from 'react-dom/client';
import { ThemeProvider, UIProvider } from '@whip/ui';
import { ScrollbarsExample } from '../../../stories/Scrollbars.fixture';
import '@whip/ui/reset.css';
import '@whip/ui/fonts.css';

createRoot(document.getElementById('root')!).render(<ThemeProvider initialTheme="dark"><UIProvider><ScrollbarsExample/></UIProvider></ThemeProvider>);

import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { BrowserDesignOverlayRoot } from '@whip/app/browser-design';
import '@whip/ui/fonts.css';
import '@whip/ui/reset.css';

// This native surface has only the design bridge, never the application runtime.
const element = document.getElementById('root');
if (!element) throw new Error('The Design Mode root is missing');
const root = createRoot(element);
root.render(<StrictMode><BrowserDesignOverlayRoot /></StrictMode>);
if (import.meta.hot) import.meta.hot.dispose(() => root.unmount());

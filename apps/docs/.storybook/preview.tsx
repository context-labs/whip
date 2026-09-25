import type { Preview } from '@storybook/react-vite';
import { useEffect, type ReactNode } from 'react';
import '../src/styles/index.css';
import '../src/components/stories/board.css';

function StoryTheme({ theme, children }: { theme: string; children: ReactNode }) {
  useEffect(() => { document.documentElement.dataset.theme = theme; }, [theme]);
  return <div className="story-theme">{children}</div>;
}
const preview: Preview = {
  globalTypes: { theme: { description: 'Carbonfox colour mode', toolbar: { icon: 'circlehollow', items: ['dark', 'light'] } } },
  initialGlobals: { theme: 'dark' },
  decorators: [(Story, context) => <StoryTheme theme={context.globals.theme}><Story /></StoryTheme>],
  parameters: { layout: 'fullscreen', backgrounds: { disable: true }, a11y: { test: 'error' } },
};
export default preview;

import {createContext} from 'react';

export type CopyText = (text: string) => Promise<void>;

export const ClipboardContext = createContext<CopyText>(async text => {
  if (!navigator.clipboard) throw new Error('Clipboard access requires HTTPS or localhost. Select the text to copy it manually.');
  await navigator.clipboard.writeText(text);
});

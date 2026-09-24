import {createContext, useContext, useState, type ReactNode} from 'react';
import {CodeBlock, MermaidBlock, type CodeBlockProps} from '@whip/ui';

type View = 'diagram' | 'source';
// Timeline owns this bounded presentation cache, not the SDK or persisted history.
export const DiagramChoices = createContext<Map<string, View> | undefined>(undefined);
export const MarkdownReadiness = createContext<{live?: boolean; truncated?: boolean}>({});

/** Only Markdown fences opt into diagrams. Tool/REPL CodeBlocks remain source. */
export function MarkdownCodeBlock({code, language, blockId, live, truncated, renderText}: {
  code: string; language?: string; blockId?: string; live?: boolean; truncated?: boolean;
  renderText?: CodeBlockProps['renderText'];
}) {
  const readiness = useContext(MarkdownReadiness);
  if (language?.toLowerCase() !== 'mermaid') return <CodeBlock code={code} language={language} renderText={renderText} truncated={truncated ?? readiness.truncated} />;
  return <DiagramFence key={blockId} code={code} blockId={blockId} live={live ?? readiness.live}
    truncated={truncated ?? readiness.truncated} renderText={renderText} />;
}

function DiagramFence({blockId, ...props}: {
  blockId?: string; code: string; live?: boolean; truncated?: boolean;
  renderText?(text: string, offset: number): ReactNode;
}) {
  const choices = useContext(DiagramChoices);
  const [view, setView] = useState<View | undefined>(() => blockId ? choices?.get(blockId) : undefined);
  const changeView = (next: View) => {
    setView(next);
    if (!blockId || !choices) return;
    choices.delete(blockId);
    choices.set(blockId, next);
    let bytes = [...choices.keys()].reduce((sum, key) => sum + key.length * 2, 0);
    for (const key of choices.keys()) {
      if (choices.size <= 128 && bytes <= 512 * 1024) break;
      choices.delete(key); bytes -= key.length * 2;
    }
  };
  return <MermaidBlock {...props} view={view} onViewChange={changeView} />;
}

import { DragDropProvider } from '@dnd-kit/react';
import { AutoScroller, PointerSensor, PointerActivationConstraints } from '@dnd-kit/dom';
import { createContext, useContext, useLayoutEffect, useRef } from 'react';
import type { ReactNode } from 'react';
import { useWorkspaceTabDrag } from './workspace-tab-drag';
import type { WorkspaceDragCallbacks } from './workspace-tab-drag';

export const workspacePointerSensors = [PointerSensor.configure({
  activationConstraints: [new PointerActivationConstraints.Distance({ value: 6 })],
  preventActivation: event => event.pointerType === 'touch' || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey,
})];
// No vertical scrolling of sidebar/page ancestors during a tab gesture. Default
// feedback/selection plugins also conflict with the renderer's strict CSP.
export const workspaceDragPlugins = [AutoScroller.configure({ threshold: { x: .2, y: 0 } })];

type Scope = { register(callbacks: WorkspaceDragCallbacks): () => void; external: boolean; visible: boolean };
export const workspaceDragContext = createContext<Scope | null>(null);

/** The shell shares one drag controller between external sources and its workspace. */
export function WorkspaceDragScope({ children }: { children: ReactNode }) {
  const target = useRef<WorkspaceDragCallbacks | null>(null);
  const drag = useWorkspaceTabDrag({
    locate: (...args) => target.current?.locate(...args) ?? null,
    onDrop: (...args) => target.current?.onDrop(...args),
    onPreview: preview => target.current?.onPreview?.(preview),
  });
  const register = useRef((callbacks: WorkspaceDragCallbacks) => {
    target.current = callbacks;
    return () => { if (target.current === callbacks) target.current = null; };
  }).current;
  return <DragDropProvider sensors={workspacePointerSensors} plugins={workspaceDragPlugins} {...drag.handlers}>
    <workspaceDragContext.Provider value={{ register, external: drag.external, visible: drag.visible }}>
      {children}{drag.preview}
    </workspaceDragContext.Provider>
  </DragDropProvider>;
}

export function useWorkspaceDragTarget(callbacks: WorkspaceDragCallbacks, enabled = true) {
  const scope = useContext(workspaceDragContext);
  const latest = useRef(callbacks);
  latest.current = callbacks;
  useLayoutEffect(() => enabled ? scope?.register({
    locate: (...args) => latest.current.locate(...args),
    onDrop: (...args) => latest.current.onDrop(...args),
    onPreview: preview => latest.current.onPreview?.(preview),
  }) : undefined, [scope?.register, enabled]);
}

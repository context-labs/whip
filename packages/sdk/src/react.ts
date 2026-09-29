import { useSyncExternalStore } from 'react';
import type { ExecutionView, SessionView, TreeCatalogView } from './state.js';

/** Render the SDK's immutable snapshot. The application owns observation
 * start/suspend/dispose, including shared leases and StrictMode remounts. */
export function useSessionView(view: SessionView) {
  return useSyncExternalStore(view.subscribe, view.getSnapshot, view.getSnapshot);
}

export function useTreeCatalogView(view: TreeCatalogView) {
  return useSyncExternalStore(view.subscribe, view.getSnapshot, view.getSnapshot);
}

export function useExecutionView(view: ExecutionView) {
  return useSyncExternalStore(view.subscribe, view.getSnapshot, view.getSnapshot);
}

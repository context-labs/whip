import { useSyncExternalStore } from 'react';
import type { WhipClient } from './client.js';
import type { SessionListView, SessionView } from './state.js';

/** The application owns start/dispose, including across StrictMode remounts. */
export function useSessionView(view: SessionView) {
  return useSyncExternalStore(view.subscribe, view.getSnapshot, view.getSnapshot);
}

export function useSessionListView(view: SessionListView) {
  return useSyncExternalStore(view.subscribe, view.getSnapshot, view.getSnapshot);
}

export function useWhipConnection(client: WhipClient) {
  return useSyncExternalStore(client.subscribe, client.getSnapshot, client.getSnapshot);
}

/** Browser capability v1. Serialized values only; native objects and control grants stay in the host. */
export interface BrowserTarget { epoch: string; tabId: string; generation: string }
export interface BrowserRestoreTab { id: string; url: string; titleHint?: string; environmentId?: string }
export interface BrowserTabState {
  id: string;
  generation: string;
  documentGeneration: number;
  status: 'restored' | 'ready' | 'unavailable' | 'crashed';
  url: string;
  pendingURL?: string;
  title: string;
  loading: boolean;
  canGoBack: boolean;
  canGoForward: boolean;
  zoomFactor: number;
  environmentId?: string;
  error?: { code: string; message: string };
}
export interface BrowserInventory { epoch: string; revision: number; tabs: BrowserTabState[] }
export interface BrowserPresentation {
  epoch: string;
  revision: number;
  blocked: boolean;
  slots: Array<{ tabId: string; slotId: string; bounds: { x: number; y: number; width: number; height: number } }>;
}
export type BrowserAction =
  | { kind: 'navigate'; url: string }
  | { kind: 'back' | 'forward' | 'stop' | 'focus' }
  | { kind: 'reload'; ignoreCache?: boolean }
  | { kind: 'find'; text: string; forward?: boolean; findNext?: boolean }
  | { kind: 'stop-find'; action: 'clear' | 'keep' | 'activate' }
  | { kind: 'zoom'; factor: number }
  | { kind: 'devtools'; open: boolean }
  | { kind: 'clear-profile' };
export type BrowserShortcut = 'design-toggle' | 'address' | 'find' | 'reload' | 'back' | 'forward' | 'close' | 'zoom-in' | 'zoom-out' | 'zoom-reset' | 'commands' | 'tab-next' | 'tab-previous' | 'new-browser';
export type BrowserEvent =
  | { kind: 'snapshot'; snapshot: BrowserInventory }
  | ({ kind: 'focused' } & BrowserTarget)
  | ({ kind: 'shortcut'; shortcut: BrowserShortcut } & BrowserTarget)
  | ({ kind: 'find'; requestId: number; matches: number; activeMatchOrdinal: number; final: boolean } & BrowserTarget);
export interface BrowserPlatform {
  readonly design?: import('./browser-design-types').BrowserDesignPlatform;
  readonly version: 1;
  snapshot(): Promise<BrowserInventory>;
  /** Metadata-only, insert-if-absent restoration. Never navigates an existing page. */
  restore(input: { epoch: string; tabs: BrowserRestoreTab[] }): Promise<BrowserInventory>;
  /** New entries are provisional until the workspace acknowledges admission. */
  create(input: { epoch: string; url: string; environmentId?: string }): Promise<BrowserTabState>;
  /** Explicit human confirmation; returned metadata still requires workspace admission. */
  createPreview?(input: { epoch: string; connectionId: string; runtimeId: string; projectId: string; url: string }): Promise<BrowserTabState | undefined>;
  admitted(target: BrowserTarget): Promise<void>;
  /** Resolves after native visibility has changed. Overlay interaction must await the hide ACK. */
  present(input: BrowserPresentation): Promise<void>;
  act(input: BrowserTarget & { action: BrowserAction }): Promise<void>;
  close(target: BrowserTarget): Promise<{ status: 'closed' | 'cancelled' }>;
  onEvent(listener: (event: BrowserEvent) => void): () => void;
}

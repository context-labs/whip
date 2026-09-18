import type { BrowserInventoryRequest, BrowserInventoryResultParams, BrowserCommand, BrowserCommandCancel, BrowserCommandResultParams, BrowserProviderBindParams, BrowserProviderBindResult, BrowserProviderEventParams } from '@whip/protocol';
import type { BrowserTabState } from './browser-types';

export type BrowserAgentScope = BrowserCommand['scope'];
export type BrowserAgentPreview = NonNullable<BrowserAgentScope['preview']>;
export interface BrowserAgentIdentity {
  desktopId: string;
  windowId: string;
  createProfileId: string;
  tabs: NonNullable<BrowserProviderBindParams['offered_tabs']>;
}
export interface BrowserAgentSelection {
  offer: BrowserProviderBindParams;
  provider: BrowserProviderBindResult;
  /** Exact prepared native connection; omitted for non-SSH providers. */
  connectionId?: string;
  projectId?: string;
}
export type BrowserAgentEvent =
  | { kind: 'provider'; event: BrowserProviderEventParams }
  | { kind: 'admission'; commandId: string; rootId: string; providerEpoch: string; epoch: string; tab: BrowserTabState };
export type BrowserAgentResult = BrowserCommandResultParams & { screenshotBytes?: Uint8Array };
/** Trusted provider transport only; never available to a website guest. */
export interface BrowserAgentBridge {
  identity(): Promise<BrowserAgentIdentity>;
  /** Metadata-only verified SSH offer; never starts routes before permission. */
  preview(input: { connectionId: string; runtimeId: string; projectId: string; loopback: '127.0.0.1' | '::1' }): Promise<BrowserAgentPreview>;
  select(input: BrowserAgentSelection): Promise<void>;
  inventory?(request: BrowserInventoryRequest): Promise<BrowserInventoryResultParams>;
  dispatch(command: BrowserCommand): Promise<BrowserAgentResult>;
  cancel(input: BrowserCommandCancel): void;
  release(input: { rootId: string; providerEpoch: string }): Promise<void>;
  onEvent(listener: (event: BrowserAgentEvent) => void): () => void;
}

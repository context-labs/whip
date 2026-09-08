import type { ConnectionProfile, ConnectionTarget } from './connections';
import type { LocalRuntimeStatus } from './platform';
export type { LocalRuntimeStatus } from './platform';

/** Serialized contract only. Electron implementation and IPC objects stay in the host. */
export interface HostPrompt {
  id: string;
  attemptId: string;
  title: string;
  message: string;
  fields: { label: string; secret: boolean }[];
  confirmLabel: string;
}
export type DesktopEvent =
  | { kind: 'progress'; attemptId: string; message: string }
  | { kind: 'frame'; id: string; sequence: number; frame: string }
  | { kind: 'sent'; id: string; sequence: number; buffered: number }
  | { kind: 'buffered'; id: string; bytes: number }
  | { kind: 'closed'; id: string; error: string }
  | { kind: 'prompt'; prompt: HostPrompt }
  | { kind: 'prompt-dismissed'; id: string }
  | { kind: 'close-request'; id: string; reason: 'quit' | 'reload' | 'update' }
  | { kind: 'navigate'; path: string }
  | { kind: 'close-tab' }
  | { kind: 'attention-wakeup' }
  | { kind: 'update'; state: 'checking' | 'available' | 'downloaded' | 'current' | 'error'; version?: string; error?: string };

export interface DesktopBridge {
  readonly version: 1;
  readonly appVersion: string;
  readonly sessionScheme?: 'whip' | 'whip-beta';
  readonly connectionKinds: readonly ConnectionTarget['kind'][];
  onEvent(listener: (event: DesktopEvent) => void): () => void;
  prepareConnection(id: string, profile: ConnectionProfile): Promise<void>;
  releaseConnection(id: string): void;
  openTransport(id: string, connectionId: string): Promise<void>;
  sendTransport(id: string, sequence: number, frame: string): void;
  acknowledgeTransport(id: string, sequence: number): void;
  closeTransport(id: string): void;
  copy(text: string): Promise<void>;
  openExternal(url: string): Promise<void>;
  pickDirectory(): Promise<string | undefined>;
  testLocalRuntime(): Promise<LocalRuntimeStatus>;
  chooseLocalRuntime(): Promise<LocalRuntimeStatus>;
  installLocalRuntime(): Promise<LocalRuntimeStatus>;
  restartLocalRuntime(): Promise<LocalRuntimeStatus>;
  beginSave(filename: string, mediaType: string, bytes: number): Promise<string | undefined>;
  writeSave(id: string, offset: number, bytes: Uint8Array): Promise<void>;
  finishSave(id: string): Promise<void>;
  cancelSave(id: string): Promise<void>;
  answerPrompt(id: string, values: string[] | null): Promise<void>;
  replyClose(id: string, result: { attachments: boolean; error?: string }): void;
  notify(options: { id: string; title: string; body: string; path: string }): Promise<void>;
  setNotificationsEnabled(enabled: boolean): void;
  hideWindow(): void;
  checkForUpdates(): Promise<void>;
  installUpdate(): Promise<void>;
  ready(): void;
}

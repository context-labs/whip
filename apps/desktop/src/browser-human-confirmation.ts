import type { BrowserWindow, MessageBoxOptions, MessageBoxReturnValue } from 'electron';
import type { HostPrompt } from '@whip/app/desktop-bridge';

type ShowMessageBox = (parent: BrowserWindow, options: MessageBoxOptions) => Promise<MessageBoxReturnValue>;

/** Human network consent is a native sheet, never an SSH authentication attempt. */
export function nativeHumanPreviewConfirmation(window: BrowserWindow, showMessageBox: ShowMessageBox) {
  let pending = false;
  return async (_attemptId: string, value: Omit<HostPrompt, 'id' | 'attemptId'>, signal: AbortSignal): Promise<string[] | null> => {
    if (value.fields.length) throw new Error('Preview confirmation cannot request credentials');
    if (signal.aborted || window.isDestroyed()) return null;
    if (pending) throw new Error('An SSH preview confirmation is already open');
    pending = true;
    try {
      window.show(); window.focus();
      const result = await showMessageBox(window, {
        type: 'question', title: value.title, message: value.title, detail: value.message,
        buttons: ['Cancel', value.confirmLabel], defaultId: 0, cancelId: 0, noLink: true, signal,
      });
      return !signal.aborted && !window.isDestroyed() && result.response === 1 ? [] : null;
    } finally { pending = false; }
  };
}

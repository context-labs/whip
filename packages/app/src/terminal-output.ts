import type { Client, TerminalRef } from '@whip/sdk';

/** Only a bounded page from this exact shell lifetime may reach the renderer. */
export async function readTerminalOutput(client: Client, ref: TerminalRef, cursor: string, signal: AbortSignal, waitMs?: number) {
  if (client.processEpoch !== ref.process_epoch) throw new Error('This shell belongs to an earlier host process.');
  const page = await client.readTerminal(ref, cursor, 32768, { signal, ...(waitMs === undefined ? {} : { waitMs }) });
  signal.throwIfAborted();
  const { terminal } = page;
  const bytes = Uint8Array.from(atob(page.data_base64), char => char.charCodeAt(0));
  if (terminal.id !== ref.id || terminal.process_epoch !== ref.process_epoch || bytes.length > 32768
    || BigInt(page.from) + BigInt(bytes.length) !== BigInt(page.next) || BigInt(page.next) > BigInt(page.end)
    || page.end !== terminal.end || BigInt(page.from) < BigInt(terminal.start)
    || (page.from !== cursor && !page.truncated)) throw new Error('Invalid terminal output evidence');
  return { page, bytes, reset: page.truncated || page.from !== cursor };
}

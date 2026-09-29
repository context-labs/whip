import { randomUUID } from 'node:crypto';

// An opt-in actual provider stream: the incomplete call cannot execute until
// its second fragment arrives. Cells, operations and stdout remain runtime-owned.
export const liveReplCode = 'for index in range(8):\n  print("live line %d" % (index + 1))\nfiles.read(path="repl-evidence.txt")\nfiles.read(path="repl-evidence.txt")\ntools.fixture_wait(key="repl-execution")\nrepl_saved = 42\nrepl_saved';
export async function replResponse({ last, delta, wait, signal }) {
  if (last.role === 'tool') return { role: 'assistant', content: 'Live REPL execution completed.' };
  const args = JSON.stringify({ code: liveReplCode }), middle = Math.floor(args.length / 2);
  delta({ role: 'assistant', tool_calls: [{ index: 0, id: randomUUID(), type: 'function', function: { name: 'execute', arguments: args.slice(0, middle) } }] });
  await wait('repl-code', signal);
  delta({ tool_calls: [{ index: 0, function: { arguments: args.slice(middle) } }] });
  // The call is already emitted; retain the normal tool_calls finish reason
  // without asking the common fixture writer to emit it a second time.
  return { role: 'assistant', content: null, tool_calls: [] };
}

import { randomUUID } from 'node:crypto';

// Native HTTP provider replies only. Mail delivery, execution identity and
// streamed observations are produced by the real runtime and engine.
export async function chatPolishResponse({ messages, last, delta, wait, signal }) {
  const text = message => typeof message.content === 'string' ? message.content : '';
  const index = messages.findLastIndex(message => message.role === 'user' && text(message).startsWith('polish:'));
  if (index < 0) return;
  const prompt = text(messages[index]);
  const executed = messages.slice(index + 1).some(message => message.role === 'tool');
  const execute = code => ({ role: 'assistant', content: null, tool_calls: [{
    id: randomUUID(), type: 'function', function: { name: 'execute', arguments: JSON.stringify({ code }) },
  }] });
  if (prompt === 'polish:saved') {
    if (!executed) return execute('print("The surveys ran in the native engine.")');
    return { role: 'assistant', content: last.role === 'tool'
      ? 'Both surveys are complete; the reports remain available for inspection.'
      : 'The follow-up update remains available for inspection.' };
  }
  if (prompt === 'polish:live') {
    if (!executed) {
      delta({ role: 'assistant', reasoning_content: 'Reasoning begins. ' + 'Inspecting recorded evidence. '.repeat(210) });
      await wait('polish-reasoning-more', signal);
      delta({ reasoning_content: '\nThe complete reasoning tail remains readable.' });
      await wait('polish-execute', signal);
      return execute('files.read(path="polish.txt")\nprint("Native execution evidence")\ntools.fixture_wait(key="polish-cell")');
    }
    delta({ role: 'assistant', content: 'I am inspecting the repository.' });
    await wait('polish-prose-more', signal);
    delta({ content: '\n\nThe inspection has finished.' });
    await wait('polish-complete', signal);
    return { role: 'assistant', content: null };
  }
  throw new Error('Unknown chat polish fixture response');
}

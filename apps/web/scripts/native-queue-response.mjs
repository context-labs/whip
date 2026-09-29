// Two explicit provider holds exercise the production steering boundary. No
// queue, transcript, input or execution state is written by this helper.
export async function queueResponse({ text, stream, delta, wait, signal }) {
  if (text !== 'queue:boundary' && text !== 'Queue B with images') return;
  if (!stream) throw new Error('Queue fixture requires real provider streaming');
  const opening = text === 'queue:boundary';
  delta({ role: 'assistant', content: opening ? 'Before the queue boundary.' : 'After the queue boundary.' });
  await wait(opening ? 'queue-boundary' : 'queue-finish', signal);
  return { role: 'assistant', content: null };
}

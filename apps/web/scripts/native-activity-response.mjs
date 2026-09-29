import { randomUUID } from 'node:crypto';

// Opt-in HTTP provider responses for the activity/reading probes. Every tool
// call runs in the actual native engine; holds delay provider/executor replies,
// never fabricate runtime operations, activity or transcript records.
export async function activityResponse({ text, last, delta, wait, signal }) {
  if (!text.startsWith('activity:')) return;
  const execute = code => ({ role: 'assistant', content: null, tool_calls: [{
    id: randomUUID(), type: 'function', function: { name: 'execute', arguments: JSON.stringify({ code }) },
  }] });
  const pause = key => wait(key, signal);
  if (text === 'activity:saved') {
    if (last.role === 'tool') return { role: 'assistant', content: 'Saved activity complete.' };
    delta({ role: 'assistant', reasoning_content: 'Saved reasoning' });
    await pause('activity-saved-reasoning');
    return execute('print(files.read(path="persisted.md")["output"])');
  }
  if (text === 'activity:work') {
    if (last.role !== 'tool') {
      delta({ role: 'assistant', reasoning_content: 'Inspecting the three source files.' });
      return execute('for path in ["source/one.md", "source/two.md", "source/three.md"]:\n  files.read(path=path)\ntools.fixture_wait(key="activity-reads")\nchild=agents.spawn(prompt="hold:activity-child", overrides={"report_mode":"message"})\nagents.wait_after_cell(input_ids=[child["input_id"]])\nprint("Review complete.")');
    }
    delta({ role: 'assistant', content: 'Review complete.' });
    await pause('activity-complete');
    return { role: 'assistant', content: null };
  }
  if (text === 'activity:tree') {
    if (last.role === 'tool') return { role: 'assistant', content: 'Tree complete.' };
    return execute('for index in range(128):\n  files.read(path="source/file-%d.md" % index)\ntools.fixture_wait(key="activity-tree")');
  }
  if (text === 'activity:prose') {
    delta({ role: 'assistant', content: '# Streaming report\n\n**Hello' });
    await pause('activity-prose-more');
    delta({ content: ' world 🌍**\n\n```js\nconst answer = 42;\n```\n' });
    await pause('activity-prose-end');
    return { role: 'assistant', content: null };
  }
  if (text === 'activity:list') {
    delta({ role: 'assistant', content: '1. **dotwhip-diver** examines its own child.\n' });
    await pause('activity-list-more');
    delta({ content: '2. **fs-sweeper** examines code or data, delegating safely.\n' });
    await pause('activity-list-end');
    delta({ content: '3. **env-profiler** checks the environment.\n' });
    await pause('activity-list-complete');
    return { role: 'assistant', content: null };
  }
  if (text === 'activity:scroll') {
    delta({ role: 'assistant', content: 'Streaming paragraph. ' + 'Initial readable text remains selectable. '.repeat(100) });
    for (let index = 0; index < 16; index++) {
      await pause(`activity-scroll-${index}`);
      delta({ content: ([6, 9].includes(index) ? '\n\nAnother block. ' : '') + `Growth ${index}. ` + 'More readable text for the retained scroll check. '.repeat(18) });
    }
    await pause('activity-scroll-complete');
    return { role: 'assistant', content: null };
  }
  throw new Error('Unknown activity fixture response');
}

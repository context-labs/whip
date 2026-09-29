import assert from 'node:assert/strict';
import { test } from 'node:test';
import { deadline, eventually, startFixture } from './native-fixture.mjs';

test('synthetic host profile is isolated and available only through an explicitly enabled human terminal', { timeout: 120000 }, async () => {
  await assert.rejects(startFixture({ terminalProfile: 'true' }), /must be a boolean/);
  const fixture = await startFixture({ networkTerminals: true, terminalProfile: true });
  try {
    const client = await fixture.connect('terminal-profile-fixture');
    const shell = await client.openTerminal({ cwd: fixture.directory, cols: 80, rows: 24 }, deadline());
    const ref = { id: shell.id, process_epoch: shell.process_epoch };
    await client.writeTerminal(ref, new TextEncoder().encode('whip_fixture_alias; printf "|%s|%s|%s|%s|%s\\n" "$WHIP_FIXTURE_PROFILE" "$WHIP_FIXTURE_PROMPT_LABEL" "$HOME" "$ZDOTDIR" "$PATH"\n'), deadline());
    const output = await eventually(async () => {
      const page = await client.readTerminal(ref, '0', 32768, { ...deadline(), waitMs: 250 });
      const text = Buffer.from(page.data_base64, 'base64').toString();
      return text.includes('fixture-alias-ok|fixture-profile-ok|fixture-human|') && text;
    }, { description: 'synthetic host profile startup' });
    assert(output.includes('fixture-human>'));
    assert(output.includes(fixture.directory + '/home|'));
    assert(output.includes(fixture.directory + '/home/shell|'));
    assert(output.includes(fixture.directory + '/home/profile-bin:'));
    assert.deepEqual(await fixture.effects(), []);
    await client.closeTerminal(ref, deadline());
    assert.equal((await client.listTerminals(deadline())).items.length, 0);
  } finally { await fixture.close(); }
});

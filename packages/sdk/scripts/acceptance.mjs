import { run } from './fixture.mjs';

await run(process.execPath, ['--test', '--test-concurrency=1', 'packages/sdk/test/fixture.test.mjs', 'packages/sdk/test/daemon.acceptance.mjs', 'examples/agents/incident-commander.acceptance.mjs']);

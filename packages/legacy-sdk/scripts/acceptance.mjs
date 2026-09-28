import { run } from './fixture.mjs';

await run(process.execPath, ['--test', '--test-concurrency=1', 'packages/legacy-sdk/test/fixture.test.mjs', 'packages/legacy-sdk/test/daemon.acceptance.mjs', 'examples/agents/support-triage.acceptance.mjs', 'examples/agents/incident-commander.acceptance.mjs']);

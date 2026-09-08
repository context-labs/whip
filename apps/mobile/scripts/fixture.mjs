import { parseArgs } from 'node:util';
import { fixtureExternalOrigin, startFixture } from '../../../packages/sdk/scripts/fixture.mjs';

const { values } = parseArgs({
  options: { minutes: { type: 'string', default: '30' }, origin: { type: 'string' }, help: { type: 'boolean' } },
});
if (values.help) {
  console.log('Usage: node apps/mobile/scripts/fixture.mjs [--minutes=1..30] [--origin=https://host.ts.net:8443]\nStarts an isolated loopback fake-provider host; Ctrl-C removes its temporary data.\n--origin allows one exact HTTPS proxy origin; it does not configure a proxy.');
} else {
  const minutes = Number(values.minutes);
  if (!Number.isInteger(minutes) || minutes < 1 || minutes > 30) {
    throw new RangeError('--minutes must be an integer from 1 to 30');
  }
  const externalOrigin = fixtureExternalOrigin(values.origin);
  const stopped = Promise.withResolvers();
  const stop = () => stopped.resolve();
  process.once('SIGINT', stop);
  process.once('SIGTERM', stop);
  let fixture;
  let timer;
  try {
    console.log('Compiling isolated fake-provider fixture...');
    fixture = await startFixture({ lifetimeMs: minutes * 60_000, externalOrigin });
    const { endpoint, runtime_id: runtimeId, root_id: rootId } = fixture.info;
    const server = new URL(endpoint);
    server.protocol = 'http:';
    server.pathname = '/';
    console.log(`Server URL: ${externalOrigin ?? server.origin}`);
    if (externalOrigin) console.log(`Loopback proxy target: ${server.origin}`);
    console.log(`Fixture cwd: ${fixture.directory}`);
    console.log(`Root: ${rootId}`);
    console.log(`Web conversation: ${externalOrigin ?? server.origin}/h/${runtimeId}/s/${rootId}`);
    if (!externalOrigin) console.log('Loopback HTTP requires a development mobile build.');
    console.log(`Fixture expires within ${minutes} minutes; Ctrl-C cleans up.`);
    // End normally before the subprocess's independent safety deadline fires.
    timer = setTimeout(stop, minutes * 60_000 - 1000);
    await Promise.race([
      stopped.promise,
      fixture.exited.then(([code, signal]) => {
        throw new Error(`Fixture exited unexpectedly (${code ?? signal})`);
      }),
    ]);
  } finally {
    clearTimeout(timer);
    process.removeListener('SIGINT', stop);
    process.removeListener('SIGTERM', stop);
    await fixture?.close();
  }
}

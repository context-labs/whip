// The live daemon fixture lives in the SDK's testing entry (packages/sdk/src/testing-node.ts)
// so consumers can import it as @whip/sdk/testing/node; scripts keep this path.
// Build the SDK first: every script that imports this runs after npm run build.
export { repository, fixtureExternalOrigin, run, eventually, startFixture, liveDaemon } from '../dist/testing-node.js';

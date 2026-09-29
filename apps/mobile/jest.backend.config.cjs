// Keep the compiled backend fixture on real Node HTTP/WebSocket APIs. The Expo
// preset replaces fetch with a native-module mock used by ordinary UI tests.
const { preset, ...base } = require('./jest.config.cjs');
module.exports = { ...base, testEnvironment: 'node', testMatch: ['<rootDir>/test/native-backend.fixture.ts'] };

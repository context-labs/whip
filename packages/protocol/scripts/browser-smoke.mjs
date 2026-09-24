// Browser acceptance exercises the built SDK under a strict CSP. The SDK runner
// owns an isolated daemon fixture; no external bridge or handwritten RPC client.
await import('../../sdk/scripts/browser-smoke.mjs');

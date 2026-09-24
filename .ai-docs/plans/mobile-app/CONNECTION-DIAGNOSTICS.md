# Mobile connection diagnostics

Branch: `mobile-app`

Goal: make the reported silent connection failure visible in the server sheet,
and let the owner test the exact phone-to-host path before saving a host.
The user explicitly requested implementation and debugging; this extends the
accepted mobile plan. No new login, transport protocol, dependency or public route.

Evidence: `server.tsx` reports failures only to the root layout's banner, outside
its native modal. Errors before assigning the attempt ID are swallowed altogether.
The configured HTTPS discovery and SDK WebSocket endpoint respond from the Mac;
this does not establish phone VPN or per-app network access.

Implementation:
- `runtime/connection-test.ts`: temporary SDK client; timed HTTPS discovery,
  protocol compatibility, WSS initialization/identity and a bounded session read.
  Close on success, failure, timeout, navigation or background. Never save a host,
  submit work, or replace the selected client. Use exact SDK paths, not URLs from
  a server response. Map known failures to actionable, honest messages.
- `app/server.tsx`: local progress/errors, Test Connection, cancel, result invalidation
  after editing the URL, and success that distinguishes an empty runtime. Catch
  failures that occur before a host ID exists. Keep normal Connect semantics.
- `components/connection.tsx`: offer connection diagnostics for saved hosts.
- Keep diagnostics ephemeral; no transcript, keys or raw response bodies stored.

Tests: HTTP rejection/HTML/protocol mismatch; hung requests and abort cleanup;
WSS and session-read failures; expected identity mismatch; test doesn't navigate,
connect or persist; Connect errors stay visible in the modal; early failures;
unmount/background/edit races. Existing mobile suite, type check and export.
Run required repository check and an independent adversarial review, then native
preview validation/rebuild as supported by the existing Expo setup.

Docs: update `docs/features.md`, canonical mobile/frontend guide, release evidence.
The beta roadmap gate remains open until physical-phone workflows pass.


Completed: probe, inline errors, native close reasons, regression suites, docs,
repository check, independent review and real-host iOS simulator acceptance.
Review corrected headless discovery semantics and bounded streaming. Replacement
signed phone build b595d18a-8b5e-4804-8211-2b3b0d9c392c is verified and ready. The iPhone's
own diagnostic result remains required to identify its actual transport failure.

The physical phone disconnected before direct installation; use the exact EAS
build page or reconnect it. Native simulator was shut down after acceptance.

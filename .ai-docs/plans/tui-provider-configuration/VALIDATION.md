# Validation — September 9, 2026

Implemented in the dirty `codex/provider-onboarding` working tree based on
`7058e007fec0a4f2e926f3b26dcb9e06ed75be5b`. Existing unrelated work was retained.

## Automated checks

Passed after implementation:

- `task check` — the full repository gate, including Go tests/vet, protocol
  generation drift, SDK and web checks, renderer and local-update/Docker scripts.
  An initial failure identified missing workflow-inventory rows for the four new
  SDK operations; those were added and the full gate passed again.
- `go test -race ./internal/config ./internal/llm ./internal/daemon ./internal/protocol`
- `go vet ./internal/config ./internal/llm ./internal/daemon ./internal/protocol`
- `go test -race ./internal/tui` — passed again after the final refresh/reload fixes.
- `go test -race ./...` — the final repository-wide race suite passed.
- `npm run check -w @whip/protocol` — 12 tests plus generated-contract checks.
- `npm run check -w @whip/sdk` — 285 tests.
- `git diff --check`.

Backend regressions exercise redacted reads, no credential-command execution on
read, atomic revision checks, concurrent edits/cancellation, reserved IDs, alias
ownership and limits, invalid/auth-failed/unverified discovery, cache failures,
removal blockers, and no-auth HTTP requests. TUI regressions exercise actual
keyboard mutations and request payloads, refresh rebasing, draft/secret lifetime,
stale clipboard/replies, reload, explicit defaults, and focus visibility at
32/48/80 columns in dark/light themes. Standalone ACP also admits explicit no-auth.

Independent adversarial review found two defects: refresh could replay stale
untouched fields, and browser re-login could reselect the current pair without
reloading its client. Both were fixed and re-reviewed with no remaining concrete
findings. Docker testing also prompted explicit manual-alias confirmation when a
new endpoint invalidates the old catalog-only model name.

## Docker and browser acceptance

Built from the working tree with `scripts/docker/onboarding.Dockerfile`, Node 24,
Go 1.27.0 and explicit dirty renderer provenance. The final image used:

- Image: `whip-provider-config:acceptance`
- Image configuration digest: `d870f372500deed130ab9d449add2c54ec689d425f82877488f86d1af519abba`
- Renderer digest: `1aac5812a8fc2f4d22b67864f3e295189c922af28a010f42b73c839bcd8bed4a`
- Disposable container: `whip-provider-config-acceptance`, normal entrypoint,
  interactive terminal, no host credential/config mounts.
- Browser: `http://localhost:4001`; port 4000 remained owned by the user's existing
  onboarding container. Added only the test host/origin allowlist for port 4001.

A deterministic local OpenAI-compatible HTTP fixture exposed `/v1`, `/v2` and
`/local` roots on port 42001. The first two required a fake fixture bearer key;
`/local` asserted an absent Authorization header and returned 404 for `/models`.
It asserted the exact API model ID and `rlm_exec` tool schema, requested execution
of `{"answer": 6 * 7}`, checked the actual tool result was 42 with positive steps,
and streamed the final answer. No external model calls or paid requests were used.

Verified entirely through TUI keyboard input:

1. Fresh container opened the focused provider search with Inference.net first
   and **Add custom provider…** reachable. No configured custom connection leaked
   from the earlier acceptance container.
2. Created **Fixture endpoint** with `/v1` and a masked API key. Setup issued only
   model discovery; the model/default confirmation did not send a prompt.
3. Explicitly sent a prompt; a real `rlm_exec` invocation returned 42.
4. Opened Manage with **ctrl+e**, changed the URL to `/v2` and explicitly reentered
   the credential. Chose **Reload current session**. The next explicit prompt
   used `/v2/chat/completions` while preserving the model/provider and history.
5. Edited to `/local`, explicitly chose None, entered `fixture-coding` manually,
   and selected Save without verification. The next screen offered the new
   `fixture-endpoint/fixture-coding` alias, followed by explicit default selection.
   A prompt completed through `/local/chat/completions` with no auth header.
6. Reloaded the actual web app's Providers & models page. It showed the custom
   connection and default `fixture-endpoint/fixture-coding · fixture-endpoint`.
7. `whip daemon restart` succeeded on the final image (generation 2, PID 175).
   The configuration SHA-256 remained
   `df56f43ba129d59fc1edf5518b54ea4dfeedb807aaf62c5d52911b3dcb374e36`.
8. A second TUI used `whip --resume cxl5fwg75hf4xuv62rqa`, restored history and the
   manual/no-auth selection, and completed another explicit prompt with result 42.

The earlier acceptance image observed one intermittent `daemon restart` timeout
despite a successful generation change and working resumed session. The final
image's restart command exited successfully; no lifecycle-command change is
claimed by this feature. An endpoint without `/models` can still emit the existing
non-blocking discovery warning during route selection/reopen; manual inference
was verified both before and after restart.

The test TUI sessions, container, fixture server and browser tab were cleaned up.
The user's original port-4000 container and local installation were left running.

## Documented limits

- No new custom-provider form in web/desktop; the shared SDK APIs are available.
- No provider database, custom headers, generic OAuth, or additional API adapters.
- Removing the last provider with no models is blocked by the existing empty-file
  recovery guard; use disable or configure another connection first.
- File serialization retains existing semantics, including possible JSONC comment
  loss and process-local locking; this does not introduce a filesystem watcher or
  cross-process editor lock.

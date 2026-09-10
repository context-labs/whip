# Models.dev provider discovery acceptance

Executed against the local dirty checkout on 2026-09-09/10, using the production onboarding Dockerfile, built web renderer, Go daemon and TUI. Final image: `sha256:7a418930771a822f10ead5e59ed072f72d3d59451344b7304029084ecd17067b` (`image-id`, `build-final.log`). Version: `dev-modelsdev-7058e007fec0-dirty`.

No host credentials or source checkout were mounted. All credential files contained synthetic fixture strings. No paid inference calls, real account login, native app UI automation or installed Whip updates occurred. Unrelated quant containers were left running.

## Verified

- Fresh default entrypoint starts daemon/web and opens TUI directly to the provider picker. No providers checked, Inference.net first with one Recommended label, search and keyboard footer intact, no discovery-status footer. Captured 100x36 and 60x28 terminals.
- Production Chromium web New Chat opens provider setup and dispatches `provider.discover` once. Captured 1280px and 390px viewports; no page errors.
- With sources-only configuration, startup automatically creates `openrouter` and `cerebras` definitions containing `apiKeyEnv` references only. The configured key-file/env-file paths survive; raw fixture values are absent from the saved config.
- TUI marks both detected providers, and management shows the key-file path. Web Settings shows Key file and Environment file badges; respective dialogs show named references and paths. Narrow dialogs and TUI management wrap paths without losing controls.
- Explicit Settings Refresh sends discovery. Repeated SDK discovery/list calls preserve the exact configuration revision; they do not rewrite no-op config.
- Same-home explicit daemon stop/start succeeded (all commands exit 0), reported stopped state between processes, and advanced PID 105 -> 243 / generation 2 -> 3. Runtime ID, configuration revision, provider references and source paths remained unchanged; both providers were available after reconnect.
- Removing the configured OpenRouter key file keeps its definition/reference/revision, marks it unavailable with `configuration_error`, and identifies the unreadable source without exposing its value. Cerebras stays available. Restoring the file and rediscovering returns OpenRouter to available with unchanged config/revision.
- Owned test containers, TUI harnesses and HTTP screenshot server were stopped/removed. See `cleanup.json`.

## Evidence

- `clean-tui-xterm.png`, `clean-tui-narrow-xterm.png`
- `clean-web.png`, `clean-web-narrow.png`, `clean-web-acceptance.json`
- `seeded-tui-xterm.png`, `seeded-tui-key-file-xterm.png`, `seeded-tui-key-file-narrow-xterm.png`
- `seeded-web-settings.png`, `seeded-web-key-file.png`, `seeded-web-env-file.png`, `seeded-web-env-file-narrow.png`
- `seeded-web-missing-file.png`, `seeded-tui-missing-file-xterm.png`, `seeded-tui-restored-file-xterm.png`
- `before-restart-state.json`, `after-restart-state.json`, `explicit-stop-start.json`
- `file-removed.json`, `file-restored.json`, `web-acceptance.json`

TUI screenshots are Chromium captures of xterm replaying the actual Docker PTY ANSI output, not native Terminal screenshots. Raw terminal event JSON and parsed text accompany each capture. Web screenshots capture the actual packaged production renderer served by the container.

## Observed caveats

An initial `daemon restart --timeout 5s` while the TUI remained attached returned a stop-timeout although a new daemon was already running (generation 1 -> 2). The unchanged runtime and config were verified, then the attached TUI was closed and the explicit stop/start test above passed cleanly. Root traced this to existing daemon lifecycle waiting for any owner lock while the TUI can auto-start its replacement; this feature does not change daemon lifecycle.

The captured missing-file TUI management screen reported the same source error both at provider level and in the aggregate discovery warning. Root removed that duplication in a final rendering-only change after this image was tested; the retained screenshot shows the pre-fix state. That small rendering fix was verified separately by root tests, without repeating this Docker matrix. No extra notice appears in the picker.

Live credential rotation/inference, provider authentication and delayed-response guards were covered separately by root's deterministic daemon tests. These acceptance runs did not send requests to paid model completion endpoints. They did not claim physical mobile, Safari, VoiceOver or native desktop screenshot coverage.

## Client validation

- Focused provider client suite: 31 tests pass, `/tmp/whip-modelsdev-client-tests.log`.
- Full app suite was run during integration: all app behavior tests passed; its sole workflow-inventory documentation drift was corrected by root, whose final `task check` passed all 490 app tests.
- Desktop source suite: 80 pass, 9 existing platform/integration skips, `/tmp/whip-modelsdev-desktop-tests.log`.
- SDK build and app/desktop TypeScript checks passed: `/tmp/whip-modelsdev-sdk-build.log`, `/tmp/whip-modelsdev-app-types.log`, `/tmp/whip-modelsdev-desktop-types.log`.
- Modified source snapshots are under `/tmp/whip-modelsdev-clients-before`.


## Final streaming request

A separate fresh container on the same tested image performed an actual streaming
request against a local fixture endpoint using a configured env-file reference.
The exact Bearer header, model, streaming response, exit status and absence of
literal keys in saved configuration were verified. See
[evidence/docker-streaming.json](evidence/docker-streaming.json). The fixture used
synthetic credentials and its container/mock server were cleaned up.

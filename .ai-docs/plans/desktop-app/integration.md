# Desktop / multi-host integration — 2026-09-08

The integration combines desktop checkpoint `7ff457fb5` with main-folder
`whip-rlm` at `f548ca59d`. The original research, source worktree, signed packages
and its `.dev` runtime data remain preserved. The target branch name stays
`whip-rlm`; no publication, tag, installed-app replacement or user-daemon restart
is part of this integration.

## Resolved architecture

- Main’s `HostConnections` owns every live client and session list. Each desktop
  local/SSH record owns setup cancellation, progress and transport disposal.
  Changing focused work leaves other connections, panes and drafts intact.
- Local daemon configuration owns revision-checked shared URL profiles. Native
  local/SSH profiles stay in device v2 storage; focused profile ID uses v3 storage
  without duplicating shared URL records. Legacy desktop URL profiles remain
  available for verified import. Corrupt/full records are preserved and surfaced.
- Replacement identity requires explicit consent in the shared host manager.
  Retyping an existing SSH destination cannot discard its remembered identity.
  Mixed-host v3 tabs and earlier layout recovery remain owned by main’s tab store.
- Native links and notifications resolve the source runtime. Stale navigation
  cannot override manual navigation. Notification scans remain bounded per host;
  enabling/disabling their native capability belongs to the window.
- Native folder selection applies to This Mac; picker replies retire with their
  route/client. Cmd-W uses the existing focused-tab close action. Durable text,
  memory-only draft warnings and attachment close confirmation are preserved.
- Native bridge limits support 16 prepared hosts and 32 transport handles, while
  per-transport queues, frame bounds and acknowledgement checks remain enforced.
- One Vite artifact supplies both Go web assets and Electron ASAR. Current
  whip/whipcode distribution, schema and protocol implementations are retained.
  MCP connection contexts retain manager ownership and transport cancellation.
- Model detail cards retain collision-aware placement with main’s busy and
  disconnected selection guards. Canonical architecture is in `docs/frontend.md`.

## Functional validation

- `task check`: passed (Go formatting, vet, whipvet, all Go tests, contracts, SDK,
  frontend and UI checks). Subsequent focused frontend checks passed as the
  integration’s connection-preservation coverage was completed.
- Frontend: 33 files, 292 tests. Desktop TypeScript: passed.
- Desktop native suite: 66 tests, zero skips, including actual isolated SSH
  authentication, forwarding, recovery and cancellation; distribution/provenance/
  publisher suite: 75 tests. Startup-script self-test only; no startup benchmark.
- Go race checks: MCP manager/HTTP transport, desktop helpers and daemon lifecycle;
  tagged desktop askpass/supervisor integration checks passed.
- Chromium and Firefox production browser workflows passed, including 9 multi-host
  workflows, 13 session-tab workflows and 7 split-workspace workflows per browser.
- Model picker: 20 narrow/short/resized/scrolled theme/browser scenarios passed.
- Staged desktop smoke passed: managed local runtime without TCP, desktop-origin
  URL connection, independent disconnect, settings reload, multiple-host restore
  and unchanged daemon PID after GUI exit/relaunch; no renderer errors.

Fresh signed-package evidence belongs to `apps/desktop/out/release/evidence.json`
and the local integration validation report, produced after this source is
committed. Earlier files under `evidence/` describe the desktop checkpoint and
must not be used to certify the merged package. Version 0.1.1 identifies this
local integrated package; it is not a published release.

Release publication still needs its dedicated hosted update feed and upload
credentials. Clean-machine/manual acceptance and a notarized N-to-N+1 update
remain release gates. Existing performance evidence is retained; no new
performance experiment was run during integration.

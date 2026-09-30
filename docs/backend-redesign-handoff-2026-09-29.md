# Whip migration handoff — 2026-09-29

> Historical handoff checkpoint. The subsequent [desktop/web inventory](frontend-desktop-web-parity-inventory.md) is complete for its stated source scope, and user-approved provider/onboarding corrections are recorded in [increment 12](frontend-ux-restoration-progress.md#increment-12--provider-onboarding-decisions). The instructions below preserve the handoff's original state; current decisions and remaining work live in those records.

## Read this first

The native Go backend, protocol-v4 SDK and supported clients have been implemented in an **unmerged draft stack**, and the retired core has been removed. A substantial frontend restoration followed. **Frontend UX parity is not complete.** The user’s latest review exposed ordinary-flow differences that the prior acceptance work did not catch.

The next session must **finish a complete baseline-to-current parity inventory and plan before changing product code**. It must discuss each proposed change to protocol-driven screens or controls with the user. Do not start by removing the two onboarding differences, broadly reverting components, or rerunning every test.

This document supersedes broad “restoration complete” / “parity complete” statements in earlier closeout documents. Their exact test results and historical observations remain evidence; their completion language is not proof of overall UX equivalence.

### Latest user decisions

The user explicitly answered:

- **What should the next session do first?** “Finish a complete parity inventory and plan before making changes.”
- **When a new screen or control exists mainly to expose protocol details, should the next session remove it from ordinary flows where possible, even if that means adjusting the SDK or backend?** “Decide each case with me before changing it.”
- **Should the handoff authorize implementation, validation, and draft PR updates, retaining prohibitions on merge/deployment/installed-runtime changes?** “Yes.”

The final “Yes” supersedes an earlier asynchronous answer of “Implement locally; ask before updating PRs.” It does **not** override the inventory-first or case-by-case decision requirements.

Previously established intent still applies:

- The UX reference is the latest development implementation **including the captured uncommitted refinements**, not merely the original migration baseline.
- Preserve ordinary UX with the smallest necessary changes to the old frontend. Small SDK/backend additions may be proposed when they preserve that experience.
- Keep new protocol complexity behind the implementation wherever the agreed product decisions permit.
- Rare, truthful recovery for genuinely uncertain outcomes is accepted. This is not blanket approval for routine extra steps, technical notices or separate recovery screens.
- Preserve the original development checkout and unrelated changes.
- The measured **72 ms typing p95 is accepted**. Do not resume performance optimization or a performance investigation campaign.
- No merge, deployment, release publication, signed installation/update, or modification of the normal installed app/runtime without explicit authorization.
- Prior authorization to launch isolated temporary review apps does not authorize changes to the normal installation or real account activity.

## 1. Exact starting state and locations

Verified while preparing this handoff:

| Item | Value |
| --- | --- |
| Current implementation checkout | /private/tmp/whip-native-final-acceptance |
| Branch | codex/frontend-ux-parity-acceptance |
| Implementation/documentation checkpoint before this handoff | e10d77a3e40d2bd788cab5431096f7353d6119c1 |
| Upstream at verification | Same e10d77a3e head |
| Current draft PR | [#298](https://github.com/context-labs/whip/pull/298) |
| PR state | OPEN, draft, unmerged |
| PR base | codex/frontend-ux-stream-handover |
| Hosted checks reported for #298 | Empty statusCheckRollup; no hosted success claim |
| Packaged production source | f91cdb610b45919c3adb2498d94293725410f6d0 |
| Final native renderer SHA-256 | a78d2ad301fe33eb545ff565b51b363fccb8aa68dbc9d6e28442c8ad3536c2ce |
| Native runtime SHA-256 | 9133dd178f25d9697a0aaf1002e78c7843b4ea3768bd6ef6ffd9f630862bcacd |
| Current versions | Protocol 4.0; host config 22; fresh store schema 57 |
| Reconstructed UX reference | /private/tmp/whip-ux-reference |
| Latest temporary review app/profile | /private/tmp/whip-restored-review-v780xk59 |
| Previous temporary review profile | /private/tmp/whip-restored-review-tyqp86c8 — stopped, preserved |
| Older migration review installation | /private/tmp/whip-review-wpUH7k — separate preserved data; do not target accidentally |
| Original development checkout — protect | /Users/samheutmaker/Desktop/context-labs/src/rlm/whip |
| Original continuation checkout — preserve | /Users/samheutmaker/.codex/worktrees/backend-redesign/whip |
| Original September 28 handoff | /Users/samheutmaker/.codex/worktrees/backend-redesign/whip-handoff-2026-09-28.md |

The current product source under internal, packages/app/src, packages/ui/src, packages/sdk/src, apps/desktop/src and apps/mobile/src has **no differences between packaged f91cdb610 and e10d77a3e**. Later commits in that interval concern fixtures/evidence/docs. The recent screenshots are not explained by accidentally inspecting an older pre-restoration renderer.

This handoff adds local documentation after e10d77a3e. See the final “Handoff preparation record” for exactly what was written. Do not mistake expected documentation changes for unrecognized product changes.

### Initial verification in the new session

Read-only commands first:

~~~sh
cd /private/tmp/whip-native-final-acceptance
git status --short --branch
git rev-parse HEAD
git log -15 --oneline
git diff --stat
git diff --cached --stat
gh pr view 298 --json url,state,isDraft,headRefName,headRefOid,baseRefName,statusCheckRollup
git diff --name-only f91cdb610b45919c3adb2498d94293725410f6d0 HEAD -- internal packages/app/src packages/ui/src packages/sdk/src apps/desktop/src apps/mobile/src
~~~

Inspect new changes rather than resetting them. If this /private/tmp checkout is gone, recover from the verified published branch in an isolated checkout; first confirm that its current head and contents are still the intended source. Local /tmp evidence and the private UX overlay are not recoverable from hashes alone.

Do not cherry-pick every branch visible in git worktree list. The repository has many retained leaf worktrees whose patches are already integrated. Use exact ancestry and patch comparison before adopting anything.

Do not assume an earlier agent ID, process ID, tool session or background test still exists. No implementation/test command is intentionally left running by this handoff preparation.

## 2. What defines the baseline

The user chose **development 12f0ea0768b7d769765596c35c049fe80edfaeba plus 14 captured working-tree refinements**.

- Historical migration baseline: e3fed9c91918d9c36766dd47d878c1b5466238d1. Useful for attribution only.
- Migration foundation before UX restoration: 446160bb49ce00334de4d9b70030949f5e6f9702, draft [#283](https://github.com/context-labs/whip/pull/283).
- Overlay identity: 04255978030cbe6289a7eeb2d02b92d1f47c7d52361a0d3ef0f6f6a70140c7c6.
- Private snapshot: /private/tmp/whip-frontend-reference-o_1z8_kl.
- Reconstructed reference checkout: /private/tmp/whip-ux-reference.
- Reference renderer: 16092ad369f4c920d46d3aa169bf93c670b3348bd9395dd432030d6439019e46.
- Exact file hashes and paths: [frontend-ux-reference.json](frontend-ux-reference.json).

The overlay contains eight browser scripts, docs/frontend.md, session-tab-strip.tsx, session-top-bar.tsx and its test, UI overlays.tsx and the UI workspace-tabs script. The manifest is authoritative. A plain git diff against 12f0ea076 misses these refinements; compare the reconstructed working files.

The snapshot is private and was not published in the draft PR. Do not publish the unrelated development patch or substitute a moving development checkout. If it is unavailable on the next host, obtain the approved snapshot before claiming comparative parity.

For an exact baseline check without mutating the original checkout:

~~~sh
python3 - <<'PY'
import hashlib, json
from pathlib import Path
root = Path('/private/tmp/whip-native-final-acceptance')
ref = Path('/private/tmp/whip-ux-reference')
manifest = json.loads((root / 'docs/frontend-ux-reference.json').read_text())
for item in manifest['overlay_files']:
    actual = hashlib.sha256((ref / item['path']).read_bytes()).hexdigest()
    if actual != item['sha256']:
        raise SystemExit('Reference mismatch: ' + item['path'])
print('All captured reference files match')
PY
~~~

Also check reference HEAD independently. Do not build in, reset, clean, stage, stash or edit the original development checkout.

## 3. Required reading and document precedence

Read, in this order:

1. This handoff, especially user decisions and the new audit inventory.
2. Current AGENTS.md and any applicable nested instructions.
3. [frontend.md](frontend.md): canonical implemented package boundaries and state ownership. Follow its area-specific source links. Its implementation descriptions do not themselves establish UX acceptance.
4. [frontend-ux-reference.json](frontend-ux-reference.json).
5. [frontend-ux-restoration-plan.md](frontend-ux-restoration-plan.md), [progress record](frontend-ux-restoration-progress.md), and [acceptance index](frontend-ux-restoration-acceptance.md), with their new correction notices. Earlier G1–G11 and A1–A12 are useful starting points, not a complete current inventory.
6. [backend-domain.md](backend-domain.md): current ownership, persistence, authority, admission, execution and recovery.
7. [backend-redesign-plan.md](backend-redesign-plan.md), [backend-redesign-development.md](backend-redesign-development.md), [backend-redesign-closeout.md](backend-redesign-closeout.md).
8. [backend-native-core-retirement.md](backend-native-core-retirement.md) and [candidate validation](backend-native-candidate-validation.md).
9. [SDK README](../packages/sdk/README.md), [Taskfile](../Taskfile.yaml), root package.json, and current CI workflows.
10. Relevant feature requirements and the **approved baseline source and tests**, not only newly written native tests.

Historical plans preserve earlier proposals; they do not override the latest user decisions or current implementation contracts. Source can also contain deliberate product drift: “this is how native currently works” is not proof that the user approved its UX.

## 4. Larger migration: what happened and what is done

### Original redesign scope

This was a deliberately breaking backend redesign in **Go**, not a move to TypeScript. Old tables/configuration/protocol/runtime identities did not require backward compatibility. That permission did not authorize unnoticed feature loss or frontend redesign.

The intended architecture has explicit domain ownership, immutable definition/configuration capture, one root/child execution path, exact durable admission/retry identities, bounded observation, separate human resources and agent authority, and truthful settlement/recovery.

At the September 28 handoff (#236 / 07641b736), phases 0–4 were complete, phase 5 was in progress, and client adoption/core retirement were pending. That document also identified tested history, rewind and Inference credential leaves to reuse. Those historical “still missing” lists are now obsolete; do not recreate them.

There had already been one important completion correction: phase 4 initially
omitted ordinary authored-mail scoped evidence/evidence-only messages. Automatic
child completion reports did not replace that capability. Drafts #213–#215
repaired it, with audited checkpoint 1244d7cd269b761278540db9ee838875e00a5992
and passing hosted run 36467683567. This is part of the process history: checking
the actual retained behavior uncovered a gap that broad completion labels missed.

### Subsequent implementation

The draft stack delivered retained backend capabilities including history revision/provenance, fork/imports, rewind and separate workspace operations, automatic titles, managed Inference.net flows, provider defaults/catalogs, human questions, permission modes, immutable definitions, gateway/trust, executors, shell/PTY, MCP, browser/computer controls, traces and client-facing services.

The native protocol/SDK and supported shared Web/Desktop, Expo mobile, Go client, CLI/TUI, ACP and examples were adopted. The retired core, legacy SDK/protocol and temporary source-scope exclusions were deleted. The retirement record maps useful old behavior to native replacement evidence rather than claiming equivalent test counts.

The first human-verification candidate was 674347705b7d3fc162146a4bfd5a7174ff57de13 / [#280](https://github.com/context-labs/whip/pull/280). Its hosted run [36594047984](https://github.com/context-labs/whip/actions/runs/36594047984) had **38 successful jobs, one failed Desktop leaf and two failed dependent aggregates**. The overall run was failed, not green. Later local checks do not rewrite that history.

The user accepted the 72 ms typing result, stopped further performance work, deferred selected environment issues, and requested a temporary local desktop/runtime for human verification.

### Human verification found two important backend defects

**Provider default failure (#282):** selecting kimi-k3-fast used its real 1,048,576-token ceiling, but host configuration rejected values over 1,000,000. The generic “invalid provider setup operation” error hid the mismatch. Commit f8a88d3b2bedb95bc1e42e3a28b8b25e5e1e660a aligned configuration, dispatch and recorded request validation. Tests failed on the old source, then passed. The temporary app saved the exact model/ceiling without making an inference call.

**Sub-agent permission inheritance (#283):** inspection of session_LXF5LNK73AS5UDNLKGYY7Q4ME5 found 19 admitted children denied ordinary operations with “no delegated authority.” Full Access roots had no standing grants to copy, and automatic authority had been limited to roots. New default children now capture eligible same-workspace automatic authority, with ancestor/revision validation and preserved explicit restrictions. Schema 56 added the relevant policy storage. Existing failed children were not backfilled or replayed. Both engines and affected store/runtime/CLI/permission tests passed.

Preserve both fixes. Neither was merely a provider/model problem or a cosmetic frontend issue.

### Frontend restoration

The user then clarified that the backend/SDK migration should retain the previous frontend UX with minimal changes. Research pinned the approved reference in #287; implementation was then explicitly authorized.

The restoration proceeded as focused branches and integrated checkpoints:

| Draft slice | Focus |
| --- | --- |
| #287 | Reference capture, research and restoration plan above #283 |
| #288 | Opening/readiness: avoid optional metadata gating ordinary UI |
| #289 | Human terminal environment fidelity and bounded output waits |
| #290 | Approval/question recovery and readable activity subjects |
| #291 | Durable ordered display presentation, including reasoning; schema 57 |
| #292 | Completed-response footer, copy, timestamps and history actions |
| #293 | Bounded SDK execution/presentation views and exact hydration |
| #294 | Provider lifecycle, disabled state and atomic category settings |
| #295 | Projects, folders, latest tab/dialog/agent-editor refinements |
| #297 | Ordered chat and stable REPL stream-to-completed presentation |
| #298 | Combined integration, follow-up fixes, comparative/fault evidence and closeout |

The progress record contains exact leaf and integrated commits. Reuse them. Do not recreate the old reducers or replace the native SDK to recover presentation.

### Honest phase status now

| Area | Status |
| --- | --- |
| Phases 0–4 | Implemented in the unmerged stack; historical acceptance includes earlier audited corrections |
| Phase 5 retained backend capabilities | Broad implementation delivered, with targeted provider/child-policy fixes; remaining parity inventory may expose behavior or capability gaps |
| Phase 6 supported-client adoption | Native client adoption implemented; UX-equivalence acceptance remains incomplete |
| Phase 7 core retirement | Retired implementation and legacy packages removed; normal product gates restored |
| Phase 7 overall release readiness | Not complete/authorized: historical failed hosted candidate, current head without hosted results, remaining environment/human checks, and unresolved UX parity |
| Frontend restoration | Substantial tested progress, **not complete parity** |
| Current task | Documentation handoff; next session begins inventory/planning, not fixes |

Do not collapse “implemented,” “locally tested,” “hosted green,” “human-approved UX,” and “release-ready” into one completion flag.

## 5. Development process and what to preserve

The useful development pattern was:

1. Identify a durable invariant or user-visible regression and its owner.
2. Inspect retained source/tests and the native contract.
3. Implement a small independently testable slice in an isolated leaf when useful.
4. Run focused tests and relevant actual-process/browser checks.
5. Review ownership, cancellation, atomicity, authority and bounds.
6. Integrate exact tested commits; validate combined behavior.
7. Record commands/results/limits and update stacked draft PRs.

This produced real backend and integration improvements. Examples worth preserving:

- One session execution model for roots and children, with scoped delegation.
- Exact receipts and original-payload retries; no automatic replay of uncertain external work.
- Durable ordered assistant display metadata separate from model input/private continuation.
- Stable preview → canonical call → execution cell identities.
- Shared immutable execution-body retention within existing bounds.
- Cached-child Back/Forward reading-anchor correction.
- Projects recency-reorder anchor correction.
- Pinned approval/question attempts and retained authored question drafts across client replacement.
- Explicit history reads available during observation recovery.
- Idle rewind using atomic active-work/revision/tail checks rather than a synthetic stop/restart.
- Whole-category provider/execution settings saves.
- Separate human terminal environment from restricted agent execution environment.

### Where the process failed

The major failure was **treating native correctness plus selected visual restorations as overall UX parity**.

- Backend defaults and state ownership changed which frontend branches became ordinary.
- Components were reused without completely matching their previous preconditions, transitions, action counts and failure behavior.
- New protocol concepts became user-facing controls/copy instead of remaining implementation details.
- Some ported tests asserted the migrated behavior rather than comparing it with the approved baseline.
- Successful final states received more coverage than transient opening/loading/reconnection states.
- Aggregate passing test counts obscured missing workflow coverage.
- A finite A1–A12 plan was treated as a complete inventory without first proving that its scope captured every changed interaction.
- Earlier completion language was stronger than the evidence.

Concrete test mistakes:

- The reference provider runner seeds an already-selected custom model and enters Settings (provider-settings-reference.mjs:43,52–58). The native runner clears defaults and explicitly asserts “Use Inference.net → Use kimi-k3-fast” (provider-settings-restoration.mjs:31,54–66). These are different initial conditions and outcomes.
- The REPL test checks capability labels such as files.read but not the old path/argument summaries.
- A terminal test waiting for live output misses recovery wording shown during an ordinary pending open.
- Conversation reconnect retention did not establish retention of New Chat, terminal canvas/selection, Projects or folder dialogs.
- A child-route test searched for “Message WHIP,” although both baseline and current child composers use “Message this agent.” The earlier claim that the completed child had no composer was incorrect. The final test establishes root draft/caret restoration, not independent child-composer equivalence.
- The reference REPL harness encountered a baseline contrast failure; the narrower successful comparator was not a complete baseline run.
- A passing rerun does not explain an earlier unattributed failure.

The next inventory must record these gaps explicitly, and use baseline behavior as the expectation rather than silently adapting assertions.

## 6. New audit inventory — seed findings, NOT an exhaustive inventory or approved fix list

The user’s latest screenshots show:
- An added “Draft before connecting” button.
- A default-model confirmation step in the ordinary Inference.net onboarding path.
- Changed provider availability/groupings, some of which depend on environment/configuration rather than a UI regression.

The following findings came from read-only source comparison and retained evidence. Except where earlier tests or user screenshots are identified, they were **not newly runtime-reproduced in the audit**. Line numbers refer to the e10d77a3e current tree and the approved reconstructed reference and may move after edits.

Every proposed product change below remains subject to the user’s case-by-case decision. “Avoidable” means not inherently forced by the protocol; it does not mean permission has been granted to remove or redesign it now.

| ID | Area and observed difference | Source / confidence / next inventory obligation |
| --- | --- | --- |
| UX-01 | Added Draft before connecting path bypasses setup. | Current welcome.tsx:93,162,258–261; absent from baseline. Introduced during migration at d90668cc3. Source-confirmed and user-observed. Product choice, not SDK necessity. |
| UX-02 | Normal Inference.net first-run now requires model confirmation. | Baseline internal/config/config.go:652–673 defaults to kimi-k3-fast; daemon/provider_selection.go:28–73 becomes ready with credentials. Current internal/config/host.go:114–121 is deliberately unconfigured. Chooser JSX existed in both versions; changed reachability/default policy is the regression. Compare identical fresh-host/account cases. |
| UX-03 | Discovery changed from discovering/publishing missing routes to passive candidate reads plus explicit publication. | Baseline provider-setup.tsx:19–20,62 and daemon/provider_discovery.go; current settings/provider-connections.tsx:40–58. Legitimate authority separation does not automatically justify another user step after Connect/Use. |
| UX-04 | Model suggestion is less contextual, especially for custom routes. | Baseline provider_selection.go:86–114 considers current/configured choices; current provider-setup.tsx:29 uses first preset suggestion. Compare single custom-model and already-selected-provider cases. |
| UX-05 | Ordinary terminal opening immediately presents lost-ack recovery language. | Current session-tab-routing.ts:124–128 navigates before reply; terminal-view.tsx:148,207–213 shows “acknowledgement is unconfirmed” while sending. Baseline waited, then showed Starting terminal. Preserve true uncertain-open protection; review normal pending UX separately. |
| UX-06 | Transient reconnect removes presentation on several surfaces. | Current hosts.ts:282–289 removes client; welcome.tsx:57–68, session-tab-strip.tsx:302–306 and session-sidebar.tsx:296–300 depend on it; runtime.ts:239 clears queries; directory-picker.tsx:39 closes on connectivity changes. Baseline retained stale client/list/canvas. Draft text persists; focus/selection/dialog continuity is a separate obligation. |
| UX-07 | Open terminal here can fall back to HOME before session cwd metadata arrives. | Current session-tab-routing.ts:115 ignores rootId for fallback. Baseline routing passed it to daemon/terminal_rpc.go:125–142 for session-directory resolution. Source-confirmed conditional path, not newly reproduced. |
| UX-08 | Composer can be disabled by observation or independent selected/root metadata failure. | Current conversation.tsx:231–268,1024–1028; baseline :433–434 had fewer gates. Native identity validation is necessary; inventory whether the read-side failure must block sending. Chat older-history reads now use hostConnected, REPL still uses stricter connected. |
| UX-09 | Projects paging/refresh changes. | Current sidebar :160–163,232–247; 100/page, 500-item/2 MiB displayed window. Periodic activity refresh stops after expansion/older paging; metadata invalidation still works. Do not call it completely frozen. Baseline list continued refresh. |
| UX-10 | Queued-message Load more replaces a cursor page instead of extending visible collection. | Current conversation.tsx:283–300,996–1019 adds Return to first queued messages; baseline composer-queue.tsx:66–72 and SDK state.ts:253–256 append. Internal bounds need not dictate this exact interaction. |
| UX-11 | Large messages move from inline rendered content to size notice/raw-data modal. | Baseline timeline.tsx:447–482 auto-hydrated up to 64 MiB. Current history-gap.tsx:25–30 and conversation.tsx:649–679,1177–1227 read 1 MiB, display 128 KiB code, download up to 4 MiB JSON. Source-confirmed material presentation/limit change. Preserve boundedness but inventory readable old behavior. |
| UX-12 | REPL host-call rows omit argument/path summaries. | Baseline repl-view.tsx:114–118 versus current :121–125, capability only. Arguments exist; restoration commit 5b7f56d45e retained this omission. Compare full rows, not just capability labels. |
| UX-13 | Worker restart notice moves from independent history row to cell result. | Baseline repl-view.tsx:66–70; current :118 and execution-output.ts:21–31. Grouping/narration difference confirmed. Whether an unmatched restart is lost needs further contract investigation; not proven. |
| UX-14 | Child conversation title uses definition ID instead of human name. | Baseline conversation.tsx:311–314; current :692–699. Readable name is available via definition lookup elsewhere. |
| UX-15 | “Last turn” status derives from selected execution-history page. | Current conversation.tsx:361–363 uses active ?? evidence.turns[0]; SDK execution-state.ts:158–178,276–285 replaces window on older paging. Baseline agent-turn-notice.tsx:34–42 used independent last_turn. Source-confirmed conditional coupling; reproduce before fixing. |
| UX-16 | Observation cadence differs across independent views. | Baseline event observation/reconciliation; native SessionView, ExecutionView, queue and metadata refresh independently. A possible contributor to perceived jank, not a measured diagnosis. Inventory transition behavior; do not start performance optimization. |
| UX-17 | Changing model resets supported reasoning effort. | Baseline settings/configuration.tsx:129–133 preserves compatible effort; current settings/provider-defaults.tsx:62 always clears it. |
| UX-18 | Default/Off, goal/retry explanations and stale-save policy changed. | Current provider-defaults.tsx:55–78 and configuration.tsx:64–76. Some are semantic clarifications; atomic category Save is restored. Do not repeat obsolete claims that every field still saves independently. Classify each difference with the user. |
| UX-19 | External-source Disconnect is offered and then errors; advanced provider editor expanded. | Current provider-connections.tsx:255–270,302–318; baseline suppressed inapplicable Disconnect and explained external ownership inline. Private/shared credentials must stay protected. |
| UX-20 | Import switches and MCP application behavior differ. | Baseline configuration.tsx:170–174 and daemon/provider_service.go:216–232; native settings select sources for explicit import review. Current details/integrations.tsx:72–97,138–178 offers separate Refresh/Reload and explicit application. Product semantics require comparison/decision. |
| UX-21 | Agent editor fields and choice labels changed. | Baseline settings/agents.tsx:160–164 had separate Persona/Rules and capabilities; current :166–177 has ID, Display name, merged Instructions, module visibility and SDK-executor explanation. definitions.ts:27 displays revision hashes. Keep immutable identity/authority; UI choices are open. |
| UX-22 | Approval workflow no longer offers inline old Remember scopes. | Baseline requests.tsx:173–184 versus current :279–329 Allow once/Deny and separate grants. Root-only prompts are intentional native contract; Root agent is not a mislabeled child, but named-root attribution is lost. Broad remembered rules cannot be silently equated with exact grants. |
| UX-23 | Pending questions have a visible four-item window. | Current requests.tsx:119–125,155–171. Boundedness is sensible; this exact window/disclosure differs. Old backend was not necessarily unbounded. Keep exact reply/draft retention. |
| UX-24 | History action copy and recovery moved toward protocol concepts/settings detours. | Current conversation.tsx:1140–1173 mentions complete exchanges, checkpoints, revision/tail and explicitly no file restore, then directs failed requests to Recovery settings. Baseline used message-relative copy and described possible tracked-file restoration. Full old filesystem semantics need investigation; do not promise implicit native file rollback. |
| UX-25 | Saved commands and advanced browser/computer/inspection surfaces expanded. | Current settings/recovery.tsx; settings/external-browser.tsx; details/integrations.tsx; model-inspection.tsx. Separate real new functionality from incidental protocol exposure. User decides each proposed removal/reorganization. |
| UX-26 | Mobile has additional product differences. | new-session.tsx:210 revision labels; session/[rootId].tsx:70 header changes, :229 oversized-message host-app detour; components/session-controls.tsx added lifecycle/history/trace controls. Existing mobile validation is not full parity. Extend inventory to mobile rather than assuming shared web findings cover it. |
| UX-27 | Startup visuals survived, but readiness/search warming changed. | startup-screen.tsx, welcome-host-picker.tsx, sidebar-layout.tsx and sidebar style file are byte-identical. Native runtime.ts:328–350 has separate inventory/readiness; old :319–325 included readiness. Baseline search warmup is absent. No newly measured loading regression established solely from this. |

Source-file scope comparison, excluding tests, is additional context, not a UX percentage:

| Source area | Baseline files | Existing changed | Added | Removed | Unchanged |
| --- | ---: | ---: | ---: | ---: | ---: |
| packages/app/src | 141 | 89 | 21 | 0 | 52 |
| packages/ui/src | 35 | 2 | 0 | 0 | 33 |
| apps/desktop/src | 34 | 9 | 1 | 0 | 25 |
| apps/mobile/src | 47 | 26 | 15 | 0 | 21 |
| packages/sdk/src | 22 | 10 | 17 | 12 | 0 |

These were byte comparisons of .ts/.tsx/.css/.mjs/.js files, excluding test/spec/__tests__ files, against actual reconstructed working files. Changed files include data binding and ownership code. The original UI foundations were retained; the application behavior changes were nevertheless extensive.

## 7. What the next inventory and plan must deliver

This is the next session’s immediate deliverable, **before code changes**.

### Inventory method

- Enumerate the complete supported frontend surface from routes, menus, commands, settings, component entry points and baseline tests. Include desktop/web and mobile; account for CLI/TUI/ACP workflows when a shared semantic change affects them.
- Include shared UI, desktop native bridges, web bootstrap and product-facing docs behavior where the migration changed them. Do not turn this into an unrelated docs-site or visual redesign.
- Compare baseline and native with **identical preconditions**: credentials present/absent, selected defaults, fresh/existing data, host identity, agent/root selection, connection state and viewport/theme.
- Trace a user journey from entry through ordinary success, loading, navigation, completion, refresh, reconnect, interruption and recovery.
- Compare interaction count, automatic choices, copy, focus/caret, selection, disclosure, scroll anchoring, loading footprint, readiness and error placement—not just final screenshots.
- Separate code-established differences, reproduced behavior, retained historical evidence and untested hypotheses.
- Distinguish original migration changes, restoration changes, baseline refinements, baseline defects and environment-dependent differences.
- Preserve existing native invariants and tested fixes. Do not import entire legacy files with retired state ownership merely because their JSX is familiar.

Each inventory row should contain:

| Required field | Purpose |
| --- | --- |
| Stable ID and workflow | Makes decisions and evidence traceable |
| Exact baseline/current source | Avoids comparing against a moving target |
| Preconditions and action sequence | Makes the comparison reproducible |
| Expected/actual result | Captures the actual UX difference |
| Intermediate-state behavior | Covers the polish missed by endpoint-only tests |
| Cause/owner | Backend default, SDK projection, app lifecycle, component, launcher/environment, or unknown |
| Evidence/confidence | Source proof, runtime reproduction, old report, hypothesis |
| User impact | Normal path versus rare failure; severity without exaggeration |
| Native constraint | What actually cannot be copied safely |
| Options and minimal changes | Include SDK/backend option when it preserves UX |
| Proposed acceptance | Same journey and negative assertions for unwanted steps |
| User decision | Pending / approved / declined / explicit exception |
| Dependencies and affected clients | Makes implementation order and scope reviewable |

### Socratic decision process

Use the inventory to ask concrete questions rather than requesting blanket approval to “restore parity.” For each disputed case, show the baseline experience, the native difference, what constraint is real, and the smallest alternatives. Ask what outcome matters and which tradeoff the user accepts.

Important decision topics include normal provider auto-defaulting, the Draft bypass, remembered permission scope, agent authoring fields/labels, import application behavior, large-message presentation/limits, in-context versus settings recovery, history/file restoration, and added advanced controls.

Do not infer that the user wants every new feature deleted. Do not infer approval to expand authority simply because the baseline had a familiar control. Do not reopen already settled global questions such as the baseline choice, accepted 72 ms result or prohibitions on deployment.

A sensible **planning order**, not authorization to implement, is:
1. Complete the inventory across all surfaces.
2. Review frequent entry paths: onboarding/defaults, normal loading, terminal open, reconnect continuity and composer readiness.
3. Review reading/conversation/RLM: large content, queue/history navigation, operation summaries, agent names, restarts and last-turn state.
4. Review configuration/authority: provider management, model defaults, imports, agent editor, permissions and questions.
5. Review advanced/mobile surfaces and unresolved platform acceptance.
6. Present a dependency-ordered plan of small increments and user decisions.
7. Only after the required decisions are resolved, implement and validate those agreed increments and update draft PRs.

### Differential acceptance rules

- Use the same scenario/preconditions against the pinned reference and native candidate.
- Record both expected **presence and absence** of controls/steps. The native test must not quietly endorse a new step.
- Capture transient states by deliberately holding a read/reply; do not merely wait until the final working state.
- Compare semantics as well as screenshots. Readiness/default selection may differ even with identical JSX.
- Keep exact failed reports. Label reruns and source/artifact changes.
- Do not widen geometry thresholds or remove old assertions to obtain green.
- A limitation/intentional change needs explicit user disposition; it is not silently counted as parity.
- State each test’s actual scope. Child navigation is not child-input independence; completed-code selection is not selection during every streaming append.
- Add focused regression coverage appropriate to approved changes, not large duplicated test suites for trivial forwarding.
- Reuse unchanged tests/checkpoints rather than repeatedly running broad suites without a new reason.

## 8. Existing evidence to reuse, and its limits

The [acceptance index](frontend-ux-restoration-acceptance.md) and [progress record](frontend-ux-restoration-progress.md) contain exact source/artifact attribution. These are **historical results, not fresh runs in this handoff task**.

Recorded final local validation includes:
- Shared renderer: 1,480 tests in 117 files, types and production packaging.
- SDK: 223 tests, source/test types, packed consumer, browser/native smoke.
- Protocol: 18 interop tests and generation drift.
- Full store race suite; affected runtime/model/runner/provider/account/configuration/RPC/terminal checks. The full runtime run exposed three architecture-boundary imports, subsequently corrected and checked at affected owners; do not claim a second unchanged full runtime race rerun.
- Native CLI/TUI/ACP complete gate and compiled-client tests/vet.
- Mobile: 226 cases across the main run and storage rerun, real-backend/fixture cases, both exports and Expo checks. No physical-device claim.
- Isolated unsigned desktop package verification and onboarding, normal/failed-turn, terminal and editor workflows.
- Real socket-loss permission/question recovery, provider faults, queued inputs, response-history controls, content, navigation and 10,000-message history recovery.

Passing these does not establish every item in section 6 or complete parity.

### Useful local evidence

| Evidence | Location |
| --- | --- |
| Packaged desktop acceptance | /private/tmp/whip-ux-desktop-final-acceptance |
| Provider reference comparison | /private/tmp/whip-provider-reference-comparison/report.json |
| Native provider/settings restoration | /private/tmp/whip-ux-pack6-provider-restoration/report.json |
| Provider/settings evidence index | /private/tmp/whip-ux-final-provider-settings-evidence/README.md |
| Permission recovery | /private/tmp/whip-ux-pack6-permission-recovery/report.json |
| Native opening/submission comparison | /private/tmp/whip-ux-parity-native-r2/report.json |
| Reference comparison | /private/tmp/whip-ux-parity-reference-r3, plus reference-a1, reference-a11 and reference-firefox directories |
| Final execution/REPL evidence | /private/tmp/whip-ux-final-main-pack6/executions and executions-firefox-final |
| Earlier execution checkpoint | /private/tmp/whip-ux-repl-acceptance/final-executions-28b9/results.json |
| Response history controls | /private/tmp/whip-ux-existing-navigation-pack6/footer-chromium and footer-firefox |
| History recovery | /private/tmp/whip-ux-final-history-recovery2/results.json |
| Projects and anchor evidence | /private/tmp/whip-ux-a12-projects-final, whip-ux-a12-sidebar-anchor-final, whip-ux-a12-sidebar-anchor-firefox |
| Desktop zoom | /private/tmp/whip-ux-a12-zoom-final-capture/report.json |
| Baseline REPL harness failure | /private/tmp/whip-ux-repl-reference-results |
| Prior provider/child bug runtime evidence | /private/tmp/whip-review-wpUH7k and its backup/evidence files |

Local temporary evidence can expire. Verify existence and contents before relying on it; do not regenerate a “passing” report to replace an unavailable failed one.

### Historical native findings

- **NATIVE-01:** original reading-intent geometry later passed in both browsers. The earlier intermittent Firefox event’s cause was not proven.
- **NATIVE-02:** reused tested observer-diagnostic checkpoint fde8bd22020b42ff32977df4c985ccde4545c98e. It qualifies observations by document/runtime/epoch/session. Raw physical per-RPC sockets are not equivalent to active observers. The original product cause remains unproven.
- **NATIVE-03:** actual delayed execution evidence changed display identity. Preserving canonical row identity repaired the original 20-switch Back/Forward criterion within 2 px.
- **NATIVE-04:** subsequent isolated desktop discovery/navigation passed. The earlier hosted ERR_ABORTED remains unattributed.
- A combined Firefox run reported four raw connection/navigation messages after behavior checks; that report remains failed. A bounded diagnostic replay corrected a separate lifetime socket-counter problem and passed later. Do not claim the counter correction explains every earlier message.
- Baseline light-theme REPL contrast and nested provider discard-focus defects were recorded. They are baseline limitations, not permission for unrelated redesign.
- Partial-code selection during appended streaming text is lost in both versions; completed-code selection through settlement was separately demonstrated.
- Missing historical reasoning cannot be reconstructed. New schema57 presentation preservation does not repair bytes never recorded.

### Deferred environment/release work

Still distinct from automated local correctness:
- Actual Safari requires a prepared Remote Automation environment; WebKit/driver fixtures are not actual Safari.
- Physical mobile suspension/network/process-death/storage/accessibility/keyboard checks remain.
- Real account/login/live-provider checks need an explicitly prepared target and credentials. Do not make billable calls or mutate real accounts merely to complete a matrix.
- Real Chrome extension and OS TCC/accessibility consent remain prepared-environment checks.
- Signed/notarized/Finder/quarantine/install/update and release publication were skipped/not authorized.
- Current #298 has no hosted status checks reported. Earlier hosted failures stay failed; do not imply green final CI.
- Automated geometry/screenshot checks are not human acceptance.

## 9. Temporary runtime and screenshots

The user asked for a fresh disposable app/backend review environment twice. Latest root:
**/private/tmp/whip-restored-review-v780xk59**

It contains the real production native backend and desktop bundle, not the fake-provider fixture. The launcher isolates WHIPCODE_HOME/state, desktop userData, socket and executable paths. It preserves real HOME/SHELL/ZDOTDIR so ordinary terminal configuration works. Environment credential detection is possible; no credentials were copied into this handoff.

Inspect the launcher before using it:
- start.command
- stop-backend.command
- launch.py
- launch-status.json and available manifests/logs

A status read is:

~~~sh
python3 /private/tmp/whip-restored-review-v780xk59/launch.py --status
~~~

Do not assume the PIDs from prior turns are current. Do not restart, delete data, or switch the running app merely to begin the planning audit. A fresh profile should not be confused with an old profile lacking migrated settings; record environment/defaults for every comparison.

The earlier launcher’s disposable HOME independently hid personal shell startup configuration. That environment issue must not be confused with a remaining frontend rewrite. Current human terminal inheritance has focused coverage, not proof that every personal shell plugin/prompt works.

The Go files under apps/web/scripts/fixtures/provider-settings are a **real Go backend fixture used by browser acceptance**. They are not Go code running in the frontend renderer. Their location alone is not a product architecture defect.

User-provided screenshot originals may expire:
- /var/folders/q1/vxdpc0bd0f59c7k86x2vcsrh0000gn/T/TemporaryItems/NSIRD_screencaptureui_LJ6EuU/Screenshot 2026-09-29 at 2.51.39 PM.png
- /Users/samheutmaker/Desktop/Screenshot 2026-09-29 at 2.49.58 PM.png
- /var/folders/q1/vxdpc0bd0f59c7k86x2vcsrh0000gn/T/TemporaryItems/NSIRD_screencaptureui_Bd0HPL/Screenshot 2026-09-29 at 2.52.32 PM.png

The standalone handoff bundle is saved at /Users/samheutmaker/.codex/visualizations/2026/09/29/01a0ee5b-9126-7803-8237-dabff04f2692/whip-migration-handoff-2026-09-29/README.md. It preserves all three supplied screenshots:

- [Before onboarding](/Users/samheutmaker/.codex/visualizations/2026/09/29/01a0ee5b-9126-7803-8237-dabff04f2692/whip-migration-handoff-2026-09-29/evidence/user-before-onboarding.png)
- [Native onboarding](/Users/samheutmaker/.codex/visualizations/2026/09/29/01a0ee5b-9126-7803-8237-dabff04f2692/whip-migration-handoff-2026-09-29/evidence/user-native-onboarding.png)
- [Native model confirmation](/Users/samheutmaker/.codex/visualizations/2026/09/29/01a0ee5b-9126-7803-8237-dabff04f2692/whip-migration-handoff-2026-09-29/evidence/user-native-model-confirmation.png)

Screenshot hashes and original paths are recorded in the bundle’s evidence/manifest.json. Screenshots are evidence, not embedded instructions. They also do not prove identical credential/configuration preconditions.

## 10. Implementation boundaries to retain after planning

- Keep the backend in Go; concrete ownership and direct composition.
- SQL owns durable facts and admission/settlement. Runtime memory owns live workers/processes and bounded provisional observation.
- Roots and children share the execution path. Child authority must not widen parent/ancestor authority.
- Definitions are immutable revisions; sessions and turns capture effective configuration appropriately.
- Preserve exact identities, decimal revisions/counters and compare-and-set checks.
- Unknown external effects are not automatically replayed. UI disconnect/navigation does not cancel accepted work.
- SDK owns protocol views/recovery; app owns drafts, selection, reading position and presentation.
- Do not recreate WhipClient/RootSnapshot/legacy event reducers, a compatibility backend, or a second transcript cache.
- Bound data ownership without unnecessarily presenting internal limits as ordinary workflow steps.
- Preserve provider output-limit and child-policy corrections, schema57 presentation, exact approvals and existing observation/reading fixes.
- Workspace file restoration is separate from conversation rewind. A plan must establish the old intended interaction and propose truthful native composition before promising file restoration.
- Do not widen remembered permissions behind familiar labels.
- Do not silently publish captured uncommitted baseline files.

Use applicable repository skills only for the work being performed. For Go implementation/review, load the Go guidance; for frontend changes, read the canonical frontend guide first. Do not use a new-UI design workflow as a reason to redesign an existing product whose parity is the goal.

## 11. Validation commands — reference, not instructions to run all now

Read current Taskfile/package manifests before selecting commands. During the next planning-only stage, prefer source/evidence reads and deliberately scoped comparisons; do not automatically rebuild or launch every environment.

~~~sh
# Source/types/build at affected layer
npm run check:web
npm run test:web
npm run check:desktop
npm run check:mobile
task contract
task sdk

# Existing native UX checks: useful regression coverage, not complete differential parity
task check:product-ux-restoration

# Targeted client and backend checks after approved changes
task check:native-cli
task check:fast
task check:change
task check:analysis
task check:phase
~~~

The UX task includes native observation probes, the Go provider fixture, native migration/provider/permission/REPL/history/Projects browser scripts. It does not automatically run a complete identical baseline comparison.

Use Go 1.27 and Node 24 as specified by the repository. Generate protocol bindings through the existing Go registry pipeline; do not edit generated declarations manually. Use actual affected race/process suites when warranted. Do not repeat broad tests without changed code, a failure or an unresolved concern that justifies the rerun.

Use isolated disposable data for runtime checks. Never use update:local or an installed-daemon restart as an accidental validation shortcut. Keep exact commit, renderer/runtime identity, fixture conditions and command outcomes with each report.

## 12. Suggested opening prompt for the next session

~~~text
Read this handoff completely:
/Users/samheutmaker/.codex/visualizations/2026/09/29/01a0ee5b-9126-7803-8237-dabff04f2692/whip-migration-handoff-2026-09-29/README.md

Work from /private/tmp/whip-native-final-acceptance after verifying its status and draft PR #298. Preserve the original development checkout and all unrelated changes.

First complete the baseline-to-current frontend parity inventory and a detailed plan. Do not change product code yet. The approved reference is development 12f0ea076 plus the captured 14-file overlay, reconstructed in /private/tmp/whip-ux-reference; verify the manifest.

Treat the handoff’s UX findings as a seed inventory, not an exhaustive list or an approved fix list. Compare complete ordinary and exceptional journeys under identical conditions. Ask me to decide each proposed change to protocol-driven UI before changing it. Once planning and those decisions are complete, implementation, validation, and draft PR updates are authorized.

Reuse the existing tested migration/restoration checkpoints. Do not restart performance optimization. Do not merge, deploy, publish a release, or modify the normal installed app/runtime without explicit authorization. Keep progress and evidence precise; existing passing native tests do not establish complete UX parity.
~~~

## 13. Handoff preparation record

Prepared on 2026-09-29 following the user’s review and the three explicit decisions above.

Read-only checks verified current HEAD/upstream, draft #298 state/base/check rollup, product-source equivalence to the packaged source, current versions, baseline manifest and relevant history/evidence records. No new build, test suite, inference request, login, application launch, runtime restart or product code change was performed to prepare this document.

The write-up and brief supersession notes in the restoration plan/progress/acceptance records plus a chronological development-log entry are documentation-only local changes. A standalone copy and available user-provided screenshots are saved outside /tmp in the Codex artifact directory. No PR update, commit, push, merge or deployment is performed by this handoff preparation.

The next session should verify actual Git status and use the document’s recorded source checkpoint rather than assuming the documentation file is already committed. The standalone copy is recovery/navigation material, not a second architectural source of truth.

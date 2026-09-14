# Eval harness simplification

Status: plan settled with Sam on 2026-09-14 (decisions 1 to 9); implementation not started.
Branch: `compaction-loop-and-ui-cleanup` on kuzco-4090. Companion: [../modal-evals/RESUME-2026-09-13.md](../modal-evals/RESUME-2026-09-13.md) for the incidents that motivated this.

## Why this matters

The harness was built in one 28-hour session for a first paid run on a new platform, with an adversarial reviewer adding gates for every failure it could imagine. That was defensible then. For the actual purpose, frequent evals of an internal tool, the gates now cost more than they protect:

- On 2026-09-14 a 90-attempt campaign was cancelled by a filename heuristic. One attempt's exported transcript contained the word `proxy` followed by a colon inside a `.json` file, the integrity scan called it a potential proxy config, the worker marked `security_failure`, and the controller cancelled the run. No secret was involved.
- The native cohort lost 5 of 90 attempts to a 300-second dependency-bootstrap timeout the harness imposes on top of the runner's own 600 seconds. Those tasks never reached the model.
- Provider stream errors become `agent_error` and usually a failed grade because whip treats them as non-retryable and the harness never retries. Two of eight smoke tasks were lost this way today.
- One trial with a single unknown-usage model call turns a run with 8 of 8 passes into "status partial, complete cost unknown", the same label a cancelled campaign gets.
- The Modal path carries ownership proofs (tags plus VM-local claim receipts plus nonces), durable pre-create intents, a reconcile command, a private SDK download hook, and a qualification-receipt settings file, all defending against events that have never happened in a dedicated environment.

Size today: 4,580 lines in `evals/whip_evals`, 2,200 lines of tests under `evals/tests` (the historical `runtime-ab` study and its 1,400 test lines are out of scope and untouched). Target: about a third of the harness and 40% of the tests gone, with every remaining line protecting something that affects a grade, a cost figure, a frozen input, or the ability to debug a trial.

## Principles

1. **Grades come from the native runner, untouched.** Nothing in the harness overrides, retries, or replaces a graded attempt.
2. **Frozen inputs stay frozen.** Bundle contents are hashed, the binary and tasks are pinned, launches are exactly-once, and cancelled or failed attempts stay in the denominator.
3. **A fault in one attempt is that attempt's fault.** Nothing cancels a campaign except a human.
4. **Scan for real secrets, not for words.** Exact byte match of the provider key only.
5. **Keep the evidence the report actually reads.** Grade, cost, tokens, duration, whip's transcript and database for debugging. Drop cross-checks that only detect tampering by ourselves.
6. **Delete defenses against unlikely events.** Foreign sandboxes in our own Modal environment, hostile tar members in archives we wrote, a coordinator racing itself.

## Decisions

1. **Only an explicit human `cancel` ends a run.** Every automatic condition (scan finding, integrity error, exception in an attempt thread, unproven VM exit, native container cleanup past its deadline) becomes a per-attempt mark shown in `status` and the report. Accepted cost: a leaked VM bills until its own timeout; a stuck native container holds host capacity until the run ends.
2. **Delete evidence scanning entirely**, exact-key scan included. Rationale: the provider key reaches only whip's environment inside the agent container, and Pier's proxy token is already kept off the evidence volume by the private `/tmp` compose redirect; both are prevention at the source. `integrity.py` goes; the worker keeps only a plain file inventory (path, size, sha256) for the fetch to verify.
3. **Dependency bootstrap: 600 s budget (the runner's own) and two automatic retries.** Setup precedes any model call, so retrying it is not pass-chasing; the attempt stays ungraded only if all three tries fail, and the report records the retry count.
4. **Sandbox identity is its name plus Modal tags.** Delete the VM-local claim receipt, the owner nonce, and the re-proof before every mutation. One state-dict record per attempt remains (written around create) as the durable ledger; the `attempt_already_claimed` race check no longer cancels anything (decision 1).
5. **Workers exit as soon as their completion marker is written; the fetch step verifies.** Delete the export-ACK protocol, the worker's 600 s wait, the controller's pre-ACK rehash through the Volume API, and the obsolete-ACK logic that the late-ACK fix added. The controller waits for VM exit and reads the marker, retrying briefly for Volume visibility lag. Hash verification happens once, at fetch, against the marker's file inventory.
6. **Delete fake-provider fixture campaigns on Modal.** Native `doctor --integration` keeps the fake provider for harness self-tests; the cloud path is exercised with a one-task real smoke.
7. **Whip retries a streamed request once when it fails before any token arrives** (hand-off to the whip work; `internal/llm/openai.go` treats SSE error chunks as non-retryable today), and the harness reports such failures as `provider_error`, separate from `agent_error`. Future campaigns pick up the retry when the pinned binary moves.
8. **Keep the full per-trial export, drop the cross-checks.** Trials still retain metrics, outcome, state, events, CLI output, a copy of `sessions.db`, and every content body, so any trial can be debugged later. Delete snapshot cross-verification, per-body digest re-checks, and the cell-diagnostics resolver.
9. **This session lands the whip-side stream retry** in `internal/llm/openai.go` with a test, after the harness work, coordinating with the other session on the shared branch.

## Inventory

| Mechanism | Today | Proposed | Risk if changed | Where |
|---|---|---|---|---|
| Security failure cancels campaign | any attempt with a scan finding cancels the run | mark the attempt; never cancel | none: the finding is per attempt | `modal_cloud.execute` |
| Integrity-incomplete cancels campaign | scan error or any non-regular file cancels the run | mark the attempt | none | `modal_cloud.execute` |
| Exception in attempt thread cancels campaign | any exception marks `indeterminate` and cancels | mark `indeterminate`, continue | a stuck attempt runs to its outer watchdog | `modal_cloud.execute` |
| Unproven VM exit cancels campaign | `cleanup_complete` false cancels | mark, continue; `status` lists unproven VMs | a leaked VM bills until its own timeout (outer watchdog + 30 min) | `modal_cloud.execute`, `execution.run_pool` |
| Native cleanup failure halts dispatch | one container cleanup past 240 s stops all remaining trials | record, continue; report lists it | a stuck container holds host resources; capacity accounting stays conservative | `execution.run_pool` |
| Proxy-config heuristic | filename or field-name pattern in any `.json`/`.yaml`/... marks security failure | delete | Pier proxy token persistence is already prevented at the source (private `/tmp` compose) | `integrity.py`, `modal_worker.py` |
| Exact-key scan | marks security failure, cancels | keep; mark the attempt and redact the file in the fetched copy | none | `integrity.py`, `modal_cli.fetch` |
| Tar-member and non-regular-file scanning | scans inside archives, tracks symlinks/FIFOs, errors on change-during-read | delete; hash regular files only | none | `integrity.py` |
| Dependency bootstrap timeout | 300 s, fatal, ungraded | 600 s (the runner's own), one setup retry before any model call | none to grading; setup is pre-model | `adapter.setup` |
| Provider stream error | whip treats SSE error chunk as non-retryable; harness never retries | whip retries a stream that failed before its first token once (whip change, hand-off); harness attributes `provider_error` separately from `agent_error` | none | `internal/llm/openai.go:879`, `report.termination` |
| Daemon freeze at finality | SIGSTOP the verified daemon so agent-started services survive grading | keep | removing it would kill services some verifiers need | `observe.freeze_daemon` |
| Snapshot cross-verification | re-read `sessions.db`, compare to `state.json`, compare content bodies | delete | detects only our own export bugs | `report.verify_snapshot` |
| Per-body content digest check | rehash every exported content body | delete; keep the body export | detects only our own copy bugs | `report.normalize_trial` |
| Cell diagnostics resolver | resolves every tool result to count cell errors | delete | diagnostics only | `result_evidence.py` |
| Accounting completeness | all-or-nothing flag; unknown-usage call zeroes tokens | keep the flag; always report known cost and token totals plus the count of unknown calls | none | `report.normalize_trial` |
| Run status word | `complete` only if every trial graded and evidence complete, else `partial` | `complete`, `cancelled`, `incomplete` with the reason counts | none | `report.build_result`, `modal_cloud.coordinate` |
| Ownership proofs | tags plus VM-local claim receipt plus owner nonce, re-proved before every mutation | sandbox name within our app is the identity | none in a dedicated environment | `modal_cloud.owned_worker`, `launch_command`, `modal_worker` |
| Durable pre-create intent | state-dict record before `Sandbox.create`, `attempt_already_claimed` cancels | keep one record per attempt, written after create; no claim race handling | a lost create response could orphan one VM, which its timeout ends | `modal_cloud.execute` |
| Reconcile command and reconciliation proofs | attach to a dead coordinator, prove exits by identity | delete; `status` reads the state dict and `modal container list` | a dead coordinator leaves attempts `running` in status until their VM timeout | `modal_cli.status`, `retain_partial` |
| Fetch collector | private `Volume._read_file_into_fileobj` hook, 8 streams, hard-link publication, SDK version pin | public `volume.read_file` per file with a small thread pool | slower on very large fetches | `modal_cli.collect_files` |
| Settings and qualification receipts | `settings.json` with qualification status and receipt id, validated against campaign | repo file `evals/frontier/modal.json` (image id, shapes, jobs cap); no receipts | none | `modal_cloud.validate_settings`, CLI |
| Bundle verification | USTAR tar, per-member mode/pax/sparse checks, publication by hard link | allowlist tar plus SHA-256 of the archive and of each file | none | `modal_bundle` |
| Modal fixture campaigns | fake-provider campaigns on Modal, up to 90 fixtures | delete; native `doctor --integration` keeps the fake provider; cloud path is tested with a one-task real smoke | a cloud regression costs one cheap real task to find | `modal_bundle`, `cli` |
| Late-ACK skip | obsolete ACK skipped on proven integer exit | keep | none | `modal_cloud.execute` |
| Worker export wait | worker idles up to 600 s for the controller's ACK | keep, or hash on the worker and let the controller trust the marker (follow-up) | none | `modal_worker.main` |

## Items

Each item: why, target, steps, tests, size. Phases are ordered by leverage; A can land alone.

### Phase A: stop losing runs and attempts to non-failures

**A1. No automatic campaign cancellation** (decision 1).
Why: five per-attempt conditions cancel a 90-attempt run today; one fired on a heuristic.
Target: `cancel` from the CLI is the only path to `cancelled`. Everything else marks the attempt.
Steps: in `modal_cloud.execute`, remove every `cancelled.set()` except none (the cancel watcher thread in `coordinate` remains the sole setter); attempt states become `completed`, `failed`, `indeterminate`, `interrupted`, `cleanup_unproven`. In `execution.run_pool`, a result with incomplete cleanup no longer sets the pool's cancel flag; its capacity stays reserved (conservative) and the record carries `cleanup_incomplete`. `coordinate` reports `cancelled` only when the cancel key was written by the CLI, otherwise `completed` with per-state counts. Native `run.py` gets the same status logic.
Tests: replace the cancel-expectation tests in `test_modal_cloud` and `test_pool` with "a faulted attempt does not stop its siblings"; keep the CLI-cancel test.
Size: about 40 lines removed, 15 changed.

**A2. Delete evidence scanning** (decision 2).
Why: the proxy heuristic cancelled a run; the exact-key scan has never found anything and the key is prevented at the source.
Target: the worker records a plain inventory of exported files (path, size, sha256) for the fetch to verify; no scanning states exist.
Steps: delete `whip_evals/integrity.py`; in `modal_worker`, replace `inventory(...)` with `tree_files` output in `complete.json`; drop `security_failure` and `integrity_complete` from worker results, controller records, `status`, `fetch`, and the report's error codes; keep Pier's private `/tmp` proxy compose redirect in `adapter.py` (that is the real control).
Tests: delete integrity tests; adjust worker and fetch tests to the inventory-only marker.
Size: 218 lines deleted plus about 40 across worker, controller, fetch, report.

**A3. Dependency bootstrap: 600 s and two retries** (decision 3).
Why: 5 of 90 native attempts were lost before the model ran, to a harness-imposed 300 s.
Target: each bootstrap try gets 600 s; up to three tries; the setup receipt records attempts and durations; still ungraded if all fail.
Steps: `adapter.setup` loops the dependency step up to three times on failure, writing `setup-dependencies.json` with per-try timing; raise the native runner's `override_setup_timeout_sec` and the `timing_envelope` setup component to cover three tries (1,800 s plus the existing ripgrep and binary checks). Setup retries are pre-model, so they do not touch the no-retry rule for graded attempts.
Tests: a fake environment whose first two dependency execs fail; envelope arithmetic test updated.
Size: about 30 lines changed.

**A4. Provider errors are attributed as `provider_error`** (decision 7, harness half).
Why: a gateway stall and a model failure both read `agent_error` today.
Target: `report.termination` recognises whip's `api error:` prefix, stream timeouts, and HTTP 5xx/429 text in `cli.stderr` and structured CLI errors, and emits `provider_error`; `summarize` counts it; the Markdown report lists it under coverage.
Tests: fixture stderr strings for the three shapes; a grade is still recorded when the verifier ran.
Size: about 25 lines.

**A5. Honest status and cost words.**
Why: `partial` currently means both "cancelled" and "one call's usage unknown"; "complete cost unknown" hides a known figure.
Target: run status is `complete`, `cancelled`, or `incomplete`, with counts of ungraded, evidence-incomplete, and provider-error trials beside it; cost is always the known sum plus the number of unknown-usage calls, and token totals are always shown.
Steps: `report.build_result`, `summarize`, `write_report`; `modal_cloud.coordinate` status; README wording.
Tests: `test_reports` status and cost cases.
Size: about 40 lines changed.

### Phase B: the Modal path without the ceremony

**B1. Identity by name and tags** (decision 4).
Steps: delete the VM-local claim file from `launch_command`, the `owner_id` nonce, and `owned_worker`'s claim re-proof; keep `sandbox_name(run, trial)` and the tags set at create; look up by name for poll, cancel, terminate. The Pier proxy redirect in `adapter.py` keys off a new `WHIP_EVAL_CLOUD=1` env flag instead of the owner id.
Tests: name-based lookup; tags still set.
Size: about 120 lines removed.

**B2. Marker-then-exit export** (decision 5).
Steps: `modal_worker.main` writes `complete.json` and returns; delete the 600 s ACK wait, `/tmp/whip-eval-exported`, `EXPORT_SECONDS`, `durable_completion`'s per-file rehash, and the obsolete-ACK branches in `execute`. The controller waits for VM exit (`sandbox.wait`), then reads the marker with a short retry loop (Volume visibility lag was measured at 5 to 9 s), records the attempt, and moves on. `fetch` verifies every hash in the marker inventory; a mismatch marks the attempt `evidence_incomplete`.
Tests: worker exits without waiting; controller records completion after exit; fetch flags a tampered file.
Size: about 90 lines removed in controller and worker.

**B3. Delete `reconcile` and reconciliation proofs.**
Steps: remove the command, `retain_partial`'s reconciliation branch, and the reconciliation state keys; `status` reads the state dict and, with `--live`, cross-checks `modal container list` for stragglers. `retain_partial` keeps the greatest observed cost from interim or final metrics for an attempt that never completed.
Size: about 60 lines removed.

**B4. Public-API fetch.**
Steps: replace the private `_read_file_into_fileobj` hook and the 1.5.5 pin with `evidence.read_file` per entry under a small thread pool, writing to a temp file and renaming; keep "never overwrite a prior fetch".
Size: about 30 lines changed; SDK-version coupling gone.

**B5. Repo config instead of settings receipts.**
Steps: add `evals/frontier/modal.json` with environment, app, worker image id, shapes, and max jobs; `whip-eval modal submit` reads it; delete `--settings`, `validate_settings`'s qualification fields, and the fixture/jobs cross-checks; keep the controller-digest handshake (it stopped a stale deployment from running a campaign and costs nothing).
Size: about 50 lines removed, one 15-line config file added.

**B6. Bundle verification to what protects inputs.**
Steps: keep the allowlist tar, the archive SHA-256, and per-file SHA-256s; drop mode, pax, sparse, and permission checks and hard-link publication; keep "a run id is never overwritten".
Size: about 40 lines removed.

**B7. Delete Modal fixture campaigns** (decision 6).
Steps: remove `prepare_fixture_campaign`, `_prepare_fixtures`, fixture staging in `prepare_campaign`, the `--fixture` and `--fixture-repetitions` flags, and fixture settings checks. Native `doctor --integration` and `observe --fixture` stay.
Size: about 90 lines removed plus their tests.

**B8. Worker preflight to what matters.**
Steps: keep the resource-fit check and the runner-version match; drop the Docker TCP listener probe and the hard-link proof file.
Size: about 20 lines removed.

### Phase C: evidence pipeline (decision 8)

**C1. Keep the export, drop the cross-checks.**
Steps: delete `report.verify_snapshot`, the per-body digest loop, `result_evidence.py`, and the cell-diagnostics counters; keep the required-files list, but a missing file marks `evidence_incomplete` rather than feeding a chain of derived errors; keep `accounting_complete` semantics (unknown-usage calls still make the total unknown, per A5 the known part is shown).
Steps: keep `freeze_daemon` (SIGSTOP on the verified daemon) because agent-started services must survive into grading; keep the interim metrics probe for crash-time cost but drop its staleness statistics.
Tests: `test_reports` and `test_contract` evidence cases pruned to the remaining rules.
Size: about 250 lines removed (`result_evidence.py` whole, report checks, observer stats).

### Phase D: whip retries a stalled stream once (decision 9)

Steps: in `internal/llm/openai.go`, when a streamed request fails before any content or reasoning delta arrived, whether by SSE error chunk, transport error, or an idle-stream timeout, retry the request once with a short backoff; failures after the first token stay non-retryable because a partial answer cannot be resumed. Accounting records the failed attempt as before (`attempt_number` increments). Coordinate the edit with the other session on `compaction-loop-and-ui-cleanup`.
Tests: `internal/llm` fake server that errors once before the first delta then succeeds; errors after a delta surface unchanged.
Size: about 40 lines plus test.

### Phase E: tests, docs, cleanup

Prune `evals/tests` to the remaining behaviour (target about 1,300 lines from 2,200); rewrite the "Detached Modal campaigns" section of `evals/README.md` to describe the simplified path in one screen; retire the qualification-era guidance in `.ai-docs/plans/modal-evals/README.md` with a pointer here; remove the 15 GB of qualification bundles and caches from the box after confirming the closures and reports are committed (evidence stays).

## Verification

1. Unit tests green after each phase.
2. Native: `whip-eval doctor --integration` (eight fake-provider fixtures, both runners) after Phases A and C.
3. Cloud: one real one-task smoke after Phase B (`smoke` profile limited to `openssl-selfsigned-cert`, about 10 cents), checking marker-then-exit, name-based cancel of a second deliberately started attempt, and fetch verification.
4. A full 30 x 3 campaign when Sam wants the next data point, with the new status and attribution in the report.

## Sequencing and effort

Phase A first, on its own commit series: it removes the causes of both incidents and is about a day. Phase B is the largest deletion, one to two days, best done as one change per item so each can be reverted alone. Phase C is half a day. Phase D is half a day and is the only change outside `evals/`. Phase E closes.

## Preserved / changed / not built

- **Preserved:** native grading and native deadlines; frozen bundles, pinned binary and tasks, exactly-once submission, a run id never reused; cancelled and failed attempts stay in the denominator; no retries or replacements of graded attempts; the daemon freeze at finality; the full per-trial export; cost accounting that never converts unknown to zero; the controller-digest handshake; the Pier proxy-token redirect; `doctor --integration` with the fake provider; the `runtime-ab` study untouched.
- **Changed:** blast radius of every automatic condition (attempt, not campaign); setup budget and retries; provider-error attribution; status and cost wording; sandbox identity; export protocol; fetch implementation; configuration source; bundle checks; report cross-checks; whip's stream retry.
- **Not built:** any new safeguard; a watchdog or heartbeat service; reconciliation of dead coordinators; secret scanning of any kind; fixture campaigns on Modal; changes to task content, model settings, or grading.

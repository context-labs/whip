# Draft #280 hosted validation — final

Run: https://github.com/context-labs/whip/actions/runs/36594047984  
Exact head: `674347705b7d3fc162146a4bfd5a7174ff57de13`  
Started: 2026-09-29T15:56:45Z  
Final update: 2026-09-29T16:19:25Z  
Conclusion: **failure — 38 successful jobs, one failed Desktop leaf, two dependent failed aggregates, zero skipped jobs** (41 total).

## Sole failed leaf

[Desktop 109501057523](https://github.com/context-labs/whip/actions/runs/36594047984/job/109501057523) failed in the real native browser fixture after the successful “new-create limit eight” assertion. The exact `act → navigate` for tab `2587d0aa-1483-42f4-b4de-3f3e3188358e`, generation `9802023c-428a-4811-83cd-53cdb1a5faeb`, guest WebContents 8, rejected navigation to the owned loopback `/page-3` with **ERR_ABORTED (-3)**. The recorded sequence has load/commit, a second load/start, rejection at 6879 ms, navigation-failed at 6904 ms, then a commit of the same URL at 6909 ms. `BrowserManager.navigate` throws “Browser navigation failed.” The final inventory says ready/loading false with `navigation_failed` retained. The window is visible and focused, with four visible guests. This is the earlier native navigation failure family, not the hidden-control first-screenshot timeout. The log alone does not establish a product or fixture root cause.

Exact log: `/tmp/whip-280-job-109501057523.log:1360–1839`. Bounded structured evidence extracted unchanged from its timestamped log: `/tmp/whip-280-desktop-navigation-failure.json` (51 events, exact step, window/guest geometry and inventory). The separate ERR_FAILED (-2) for a destroyed earlier about:blank guest is not the failing page-3 action.

Desktop types, packaging/verification, all 166 post-build unit tests, 116 distribution tests, startup probes and the isolated provider-transport regression passed first. The native browser fixture passed disabled-feature, foreign/subframe rejection, metadata projection, epoch handoff, real human-consent abort and eight-tab admission assertions before the navigation failure. Later staged onboarding/workspace/failure-smoke/terminal/editor scripts were **not reached in this run**; their previous successful #278/#279 evidence stays separately attributable.

The Desktop upload step reported no files under `test-results/redesign/desktop/`, so **this run has no desktop-ci-evidence artifact**. Its bounded failure evidence is preserved in the downloaded log and extracted JSON, not a claimed uploaded screenshot/package report.

The `product / go` aggregate 109503803331 records every input successful except Desktop; `redesign` 109503848325 propagates that failure. Their logs are saved beside the Desktop log. No other failed leaf or skipped job occurred.

## Passing combined evidence

All 38 preceding leaf jobs pass: both client platforms, both native-browser platforms, all Go race partitions, analysis, four cross-build targets, both ordinary builds, Swift driver, UI, mobile, every web product family, docs, examples, evals and both distribution platforms.

The three formerly intermittent fixture families are successful on this exact frozen run: **activity 109494261810**, **Settings 109494262079**, and **conversation 109494262295**. This records a hosted pass for the corrected Settings ownership assertion and unchanged Firefox activity/REPL assertions. It does not prove a permanent root-cause fix for the earlier Firefox small-scroll/REPL incidents. The later REPL ownership correction remains unintegrated and is not credited to this head.

Performance-helper/web gate 109494261903 passes. It is not a new quiet native performance measurement. The user's accepted 72 ms typing result and natural-retention investigation are recorded separately; no new performance experiment or forced GC was used by this validation task.

## Acceptance disposition

Per the current instruction to prepare the first human-verification version promptly, record the native Desktop navigation failure and earlier Firefox/REPL follow-ups for later work; do not start another repair/retest cycle. Signed installation/update verification is explicitly skipped for this acceptance. Existing platform/manual/live-provider boundaries remain honest, and no signed/notarized/Finder acceptance is inferred from disposable fixtures. No merge, deployment, installed-runtime/account mutation, CI rerun or cancellation occurred.

## Complete job inventory

| Job ID | Job | Result |
| --- | --- | --- |
| 109494261516 | product / analysis | success |
| 109494261810 | product / products (ubuntu-latest, product-activity) | success |
| 109494261821 | product / build (linux/amd64) | success |
| 109494261826 | product / build (darwin/amd64) | success |
| 109494261849 | product / checks (ubuntu-latest, native-browser) | success |
| 109494261860 | product / checks (ubuntu-latest, race-runtime-middle) | success |
| 109494261863 | product / products (ubuntu-latest, product-distributions) | success |
| 109494261895 | product / driver | success |
| 109494261903 | product / products (ubuntu-latest, product-performance) | success |
| 109494261916 | product / products (macos-15, product-distributions) | success |
| 109494261960 | product / products (ubuntu-latest, product-ui) | success |
| 109494261971 | product / products (ubuntu-latest, product-browser) | success |
| 109494262027 | product / build (darwin/arm64) | success |
| 109494262043 | product / checks (ubuntu-latest, clients) | success |
| 109494262066 | product / products (ubuntu-latest, product-content) | success |
| 109494262079 | product / products (ubuntu-latest, product-settings) | success |
| 109494262107 | product / products (ubuntu-latest, product-examples) | success |
| 109494262115 | product / checks (ubuntu-latest, race-runtime-first) | success |
| 109494262149 | product / build (linux/arm64) | success |
| 109494262164 | product / checks (ubuntu-latest, build) | success |
| 109494262215 | product / checks (macos-15, race-other) | success |
| 109494262224 | product / checks (ubuntu-latest, race-runtime-rest) | success |
| 109494262269 | product / checks (macos-15, race-store-first) | success |
| 109494262290 | product / products (ubuntu-latest, product-evals) | success |
| 109494262295 | product / products (ubuntu-latest, product-conversation) | success |
| 109494262296 | product / checks (macos-15, race-store-rest) | success |
| 109494262307 | product / checks (macos-15, native-browser) | success |
| 109494262313 | product / checks (ubuntu-latest, race-store-rest) | success |
| 109494262334 | product / products (ubuntu-latest, product-docs) | success |
| 109494262349 | product / mobile / checks | success |
| 109494262361 | product / checks (ubuntu-latest, race-other) | success |
| 109494262417 | product / checks (ubuntu-latest, race-store-first) | success |
| 109494262456 | product / checks (macos-15, clients) | success |
| 109494262507 | product / checks (macos-15, race-runtime-middle) | success |
| 109494262597 | product / products (ubuntu-latest, product-web) | success |
| 109494262632 | product / checks (macos-15, race-runtime-rest) | success |
| 109494263102 | product / checks (macos-15, race-runtime-first) | success |
| 109494263336 | product / checks (macos-15, build) | success |
| 109501057523 | product / desktop / check | failure |
| 109503803331 | product / go | failure |
| 109503848325 | redesign | failure |

## Artifact inventory

The final API inventory contains the following artifacts; metadata is saved in `/tmp/whip-280-artifacts.json`. No Desktop artifact is present.

| Artifact ID | Name | Bytes |
| --- | --- | ---: |
| 11046396384 | ci-renderer | 2936161 |
| 11046385910 | product-evidence-ubuntu-latest-product-content | 4134524 |
| 11046280083 | coverage-macos-15-race-runtime-rest | 23850 |
| 11046220209 | product-evidence-ubuntu-latest-product-performance | 10864 |
| 11046180522 | coverage-macos-15-race-store-first | 47659 |
| 11046132234 | coverage-ubuntu-latest-clients | 224821 |
| 11046060692 | coverage-ubuntu-latest-race-runtime-rest | 23878 |
| 11046035619 | coverage-ubuntu-latest-race-store-first | 47646 |
| 11045986256 | product-evidence-ubuntu-latest-product-conversation | 1928847 |
| 11045915294 | coverage-macos-15-race-store-rest | 46917 |
| 11045907204 | coverage-macos-15-clients | 226167 |
| 11045890227 | docs-static | 253481 |
| 11045870438 | product-evidence-ubuntu-latest-product-ui | 7239265 |
| 11045661035 | product-evidence-ubuntu-latest-product-activity | 5675456 |
| 11045625369 | coverage-ubuntu-latest-race-runtime-middle | 23844 |
| 11045423155 | coverage-ubuntu-latest-race-other | 163111 |
| 11045292375 | coverage-macos-15-native-browser | 31902 |
| 11045284126 | product-evidence-ubuntu-latest-product-browser | 5798353 |
| 11045253198 | coverage-macos-15-race-runtime-first | 23995 |
| 11045237760 | coverage-ubuntu-latest-race-runtime-first | 23958 |
| 11045197272 | coverage-macos-15-race-runtime-middle | 23855 |
| 11045112255 | coverage-ubuntu-latest-native-browser | 31912 |
| 11045089532 | coverage-macos-15-race-other | 163131 |
| 11044959684 | product-evidence-ubuntu-latest-product-settings | 3480787 |
| 11044884260 | coverage-ubuntu-latest-race-store-rest | 46921 |

## Observation record

Final API snapshots: `/tmp/whip-280-run.json` and `/tmp/whip-280-jobs.json`. Changed-state record: `/tmp/whip-280-observe.log` and `/tmp/whip-280-observations.jsonl`. The monitor used 60/120/180-second unchanged backoff, an 80-minute ceiling, 45-second API/log deadlines and 16 MiB failed-log limits. It observed final status at 16:19:47Z and joined. Parent explicitly authorized updating the draft body with the final outcome; exact before/after and body payloads are retained under `/tmp/whip-280-pr-*`.

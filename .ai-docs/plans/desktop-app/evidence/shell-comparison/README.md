# Disposable shell comparison — September 8, 2026

This experiment supports retaining Electron as the implementation candidate. On this machine, the Electron wrapper reached the same retained session sooner than the Electrobun wrapper. Electrobun had a substantially smaller installed bundle. The results do **not** establish a signed release benchmark, minimum-spec performance, equivalent native security, or full product parity.

## Inputs and isolation

- Electron **44.2.0** versus stable Electrobun **2.0.1**, Cottontail **0.5.0**, Hutch **0.24.3**, system **WKWebView** (`renderer: native`, no CEF).
- Apple M4 Max, 128 GiB RAM, macOS 26.3.1 (a), build 25D771280a, AC power. This is not the proposed M1/8 GiB reference machine.
- Frozen production renderer: **23 files, 3,636,689 bytes**, digest `b1edb2b5e8ba2eb7724790dcc78199d5281d671248c871932fe0f39578a1ddad`. The manifest records a dirty desktop worktree based on `dd7aaa3a7f9b8c00bd4ec095b978def1c9231805`. Later worktree rebuilds were not substituted into this comparison.
- The frozen renderer was copied unchanged into Electrobun's `Resources/app/views/bundle` and into the isolated fixture's HTTP document root. Its manifest was verified against both copies. Electron consumed that same frozen directory for bundled-scheme probes and the same fixture HTTP origin for measured session launches.
- Both windows had an observed **1200 × 800 content viewport**. Electron used `useContentSize: true`.
- The existing integration-only SDK fixture used a fresh private Whip home/database and fake provider. It seeded **10,000 root messages, 100 retained children with 100 messages each, and a 1.4 MB tool body** through production storage paths. Both shells selected the same URL connection and actual retained root. The daemon was already running before launch timing began.
- No user daemon, SSH profile, key, provider credential, or production Whip home was used. Native capabilities outside this comparison were stubbed. The fixture was closed and its temporary home removed.

## Launch results

The accepted collection ran from **05:56:52 to 05:59:17 UTC** after the parent task's CPU-heavy validation cleared. There were 32 launches per engine, alternating which engine went first in each pair. The first two per engine were warmups; the remaining **30 per engine** all met the renderer criterion and exited with code 0. An earlier collection that overlapped validation was discarded entirely. OS caches were warm; neither caches nor Gatekeeper state were reset.

The timer starts immediately before spawning the shell executable and ends when the collector receives the renderer's usable event. That event requires the shared composer to be enabled and `Root message 10000` to be present, followed by font readiness, two animation frames, and the same crypto/storage/WebLocks checks. It therefore includes instrumentation and local HTTP delivery, and is not an empty-window measurement.

| Instrumented retained-session readiness | Electron 44.2.0 | Electrobun 2.0.1 native |
| --- | ---: | ---: |
| Median | **1,001 ms** | **1,682 ms** |
| p95, nearest rank | **1,289 ms** | **2,715 ms** |
| Minimum–maximum | 780–1,562 ms | 1,420–3,373 ms |
| Failures / abnormal exits | 0 / 0 | 0 / 0 |

The application's earlier `whip-shell-ready` callback had median/p95 **873/1,160 ms** in Electron and **1,580/2,545 ms** in Electrobun. That callback does not prove a connected retained session and is reported separately.

Electron ran the small experiment main/preload files using its installed development runtime. Electrobun ran an expanded stable `.app` with signing/notarization disabled. Its self-extractor first run was completed before accepted measurements. These are different distribution forms; no shipping fuses were weakened. Repeat with signed/notarized release artifacts, the complete daemon/SSH host, and the reference hardware before accepting product startup targets. No statistical confidence interval or cross-machine generalization is claimed.

## Memory and disk boundaries

Memory is `ps` RSS, sampled **1.5 seconds after usable** on the last three launches per engine. Summed RSS can double-count shared pages and is not physical footprint.

| Median RSS, three samples | Electron | Electrobun |
| --- | ---: | ---: |
| Launcher and descendant process tree | 469.0 MiB | 403.2 MiB |
| Newly appeared WebKit XPC processes outside that tree | 0 | 177.3 MiB |
| Tree plus those WebKit candidates | 469.0 MiB | 580.1 MiB |
| Isolated fixture daemon, separate | 63.7 MiB | 63.7 MiB |

Electrobun's WebKit GPU, Networking and WebContent helpers had PPID 1. The harness captured processes that appeared during each launch and were absent immediately before it; it did **not** establish kernel coalition ownership. Their totals are explicitly candidates, not an authoritative physical-memory attribution. The small number of settled samples also limits interpretation. This run provides no evidence of an aggregate memory advantage for Electrobun. Both configurations warrant investigation against the proposed 350 MiB threshold before release.

Logical file sizes, excluding symlink targets already counted elsewhere:

- Electrobun expanded app: **67,512,242 bytes (64.4 MiB)** including the renderer. Its update archive was 17,793,582 bytes. Signing, notarization, DMG creation and CEF were disabled.
- Electron runtime: **301,231,912 bytes (287.3 MiB)**, plus the **3.5 MiB** renderer and tiny experimental main/preload files outside that runtime directory.

These are shell sizes, not full Whip distribution sizes. The Go runtime and other shared product helpers are excluded from both. Compressed installer size and a complete signed update were not compared.

## Compatibility observed

The bundled-scheme probe loaded the actual production application in both engines with a static desktop bridge and disconnected host selection. Early host bootstrap changed `/index.html` to `/` using history replacement because `/index.html` is not an application route. This changed no renderer bytes.

Both engines passed actual operations for secure-context status, `crypto.randomUUID()`, SHA-256 with `crypto.subtle`, a Web Lock callback, localStorage read/write, sessionStorage read/write, and an IndexedDB transaction. Inter's bundled font face loaded; the computed foreground color was `rgb(238, 238, 238)`, the shared stylesheet applied, and the shared SVG navigation rendered. The retained-session run had nine message DOM nodes at the observed position in both engines. Unused JetBrains Mono faces remained unloaded, so this does not assert that all font subsets were exercised. There was no screenshot/pixel-difference or accessibility audit.

Both engines restored the localStorage marker on subsequent complete process launches. Electrobun's default `BrowserWindow` does not expose a `partition` option, but its native SDK maps an omitted partition to **`persist:default`**. The earlier inference that the default would be ephemeral was incorrect; the observed persistence and the SDK mapping agree. Persistence at this one origin does not prove the product's whole tab/draft restoration flow.

The measured retained session used the real SDK's URL transport against the isolated daemon. It rendered the shared navigation, Markdown transcript and composer with the fixture's production CSP delivered by HTTP. No renderer fork or browser-specific source change was required. This establishes substantially more than an empty shell, but it does not test sustained streaming, keyboard/IME latency, clipboard, downloads, drag/drop, OS dialogs, native notifications, reconnects, SSH, or daemon crash ownership in Electrobun.

## Packaging findings

1. **One renderer artifact is feasible.** Electrobun's `build.copy` can place the exact Vite output under `views/bundle`; Electron can serve those same files through its protocol handler. Keep the common renderer manifest and verification rather than recompiling a second UI.
2. **Host URL routing must be implemented.** The default Electrobun `views://bundle/index.html` opens an unmatched app route unless the host starts the router at `/`. A direct `views://bundle/settings` request produced an empty response and never loaded the renderer. Its native asset handler reads an exact file path. Electron's experimental explicit SPA fallback returned the unchanged index for `/settings`, and the shared Settings UI rendered. In `deep-routes.json`, Electron's `failed` event means the generic probe still expected the home heading; the recorded body shows Settings successfully loaded. This route probe was not included in timings.
3. **CSP is a separate acceptance requirement.** Electrobun 2.0.1's native `views://` handler returns an `NSURLResponse` with MIME/length, without the product's CSP response header. The bundled-scheme pilot did not establish equivalent CSP enforcement. The realistic comparison instead used the same loopback HTTP server, which supplied the product CSP. A shipping Electrobun host would need a supported response-header/route strategy or another reviewed asset-serving design; relaxing CSP is not a solution.
4. **Bridge and lifecycle work remain real work.** The experiment exposed only the versioned desktop bridge needed to bootstrap and used direct URL transport. Electron used sandbox/context isolation with a contextBridge exposure; Electrobun used `sandbox: true` and its custom preload. Those flags do not prove equivalent security boundaries. Owned daemon startup, transport, askpass, update/quit coordination and native capabilities remain outside this framework comparison.
5. **System WebKit requires an OS matrix.** The current Mac passed the exercised browser APIs. This does not prove behavior on the minimum supported macOS or across future WebKit updates.

These findings do not justify switching the shipping framework. They support continuing the Electron packaging plan while keeping performance and complete distribution validation as explicit gates.

## Reproduction

Use Node and Go from the repository's supported toolchain, installed workspace dependencies with Electron 44.2.0, and a fresh disposable directory. Do not run shell generation inside shipping app sources. The original frozen renderer is not duplicated in Git; its local archive is `/tmp/whip-desktop-shell-comparison-renderer-b1edb2b5.tar.gz`. Use an archived copy matching `renderer-manifest.json` to reproduce these exact inputs. A newer renderer can rerun the experiment but is a different measurement.

```sh
export WHIP_COMPARISON_REPOSITORY=/absolute/path/to/whip-desktop-app
experiment=$(mktemp -d -t whip-shell-comparison)
cd "$experiment"
cp -R /absolute/path/to/frozen/renderer ./renderer
cp /absolute/path/to/frozen/renderer-manifest.json ./renderer-manifest.json
node "$WHIP_COMPARISON_REPOSITORY/.ai-docs/plans/desktop-app/evidence/shell-comparison/create-shells.mjs"
# Choose a fresh app identifier in the disposable electrobun.config.ts.
npm pack electrobun@2.0.1 --ignore-scripts --pack-destination .
tar -xzf electrobun-2.0.1.tgz
export HUTCH_HOME="$experiment/hutch"
node package/bin/electrobun.cjs build --env=stable
node "$WHIP_COMPARISON_REPOSITORY/.ai-docs/plans/desktop-app/evidence/shell-comparison/run-comparison.mjs" --scheme
node "$WHIP_COMPARISON_REPOSITORY/.ai-docs/plans/desktop-app/evidence/shell-comparison/run-comparison.mjs" --deep-route
node "$WHIP_COMPARISON_REPOSITORY/.ai-docs/plans/desktop-app/evidence/shell-comparison/run-comparison.mjs"
node "$WHIP_COMPARISON_REPOSITORY/.ai-docs/plans/desktop-app/evidence/shell-comparison/summarize.mjs"
```

The scheme warmup expands Electrobun's self-extractor at the build path. The existing fixture has a four-minute lifetime after startup; the accepted collection completed within that bound. `SIGTERM` stops collection after the current pair, allowing fixture cleanup. Inspect recorded exits before treating any run as a warm sample. The scripts are disposable research instrumentation, not production launchers. To inspect Settings after a deep-route probe, use its recorded body rather than the generic home-readiness label.

`raw-samples.json` contains all 64 accepted/warmup launches, event times, readiness features, process rows, stdout, and exit status. `summary.json` is generated from those records. `environment.json` records the machine and logical sizes; the manifest binds renderer content. `bundled-scheme.json` and `deep-routes.json` contain compatibility probes only. `cleanup.json` records that all **288** tracked accepted-run process IDs were absent after completion. The unique experiment Application Support and WebKit directories were removed after preserving evidence. Temporary downloads and apps were removed; no shipping dependencies were changed.

## Primary sources checked

- [Electrobun 2.0.1 release](https://github.com/blackboardsh/electrobun/releases/tag/v2.0.1) — latest stable at the time of this experiment.
- [Official quick start](https://framework.blackboard.sh/electrobun/guides/hello-world/), [Hutch setup](https://framework.blackboard.sh/electrobun/guides/hutch/), and [build configuration](https://framework.blackboard.sh/electrobun/apis/cli/build-configuration/) — current toolchain and native-renderer build configuration. Version-matched source was checked where current docs could describe newer APIs.
- [Bundled assets](https://framework.blackboard.sh/electrobun/apis/bundled-assets/), [BrowserWindow](https://framework.blackboard.sh/electrobun/apis/browser-window/), and [BrowserView](https://framework.blackboard.sh/electrobun/apis/browser-view/) — copy mapping, preload, renderer and sandbox options.
- [2.0.1 native macOS implementation](https://github.com/blackboardsh/electrobun/blob/v2.0.1/package/src/native/macos/nativeWrapper.mm) — `readViewsFile`, `MyURLSchemeHandler`, and `createDataStoreForPartition`.
- [2.0.1 native SDK](https://github.com/blackboardsh/electrobun/blob/v2.0.1/package/src/sdks/main/proc/native.ts) — omitted partition becomes `persist:default`; [BrowserWindow source](https://github.com/blackboardsh/electrobun/blob/v2.0.1/package/src/sdks/main/core/BrowserWindow.ts) shows the initial view options.

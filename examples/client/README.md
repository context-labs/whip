# Native SDK client example

A small browser UI and Node program using only `@whip/sdk` v4. They connect to
an existing host; they never install or start a runtime.

From the repository root:

```sh
npm ci --ignore-scripts
npm run build -w @whip/sdk
npm run check -w @whip/client-example
npm start -w @whip/client-example
```

Open the printed local URL and enter an explicitly enabled gateway URL. Its
configuration must allow this example's exact origin. Discovery is read-only;
the saved runtime ID is pinned, and each connection also checks process epoch.
A different runtime at the same address is rejected. Observe the host's normal
network authorization and terminal restrictions.

```sh
node examples/client/node.mjs /absolute/path/to/runtime.sock /absolute/host/project "Review this project"
# An explicitly enabled HTTP(S) gateway is also supported.
WHIP_RUNTIME_ID=your-saved-runtime-id node examples/client/node.mjs http://127.0.0.1:8080 /absolute/host/project "Review this project"
```

The Node program prints exact creation and submission recovery records before
sending. A real application should persist them in caller-owned durable storage.
Running the program again creates new work; it does not recover a previous run.

The browser composes one `TreeCatalogView`, one selected-session `SessionView`,
and its `ExecutionView`. Root and child IDs stay distinct from tree IDs. There
is no event reducer, synthetic transcript, or second execution cache. Live stdout
is provisional and joins by exact cell/call identity; a committed result wins.
History rewinds and process restarts clear obsolete live evidence. Loaded
transcript, execution, catalog, child list and permission pages are bounded, with
visible limits and explicit older/latest controls where supported.

Drafts stay in tab-owned storage (32 nonempty drafts, at most 64KiB each).
Attachments are explicit owner-scoped content transfers up to 4MiB; inspecting
bytes never grants another session access. File selection itself is not restored
after reload. Human permission decisions display canonical operation scope and
arguments. Questions support choices, recommended options, free text, batches
and dismissal; child authority still comes from explicit delegated grants.

The opt-in recovery journal retains at most 32 exact requests / 4MiB in this
origin's local storage, including authored text. Cross-tab writes use an origin-wide
Web Lock and recheck bounds before sending; browsers without Web Locks fail before
submission. A reload or reconnect only
rebuilds observation. It never resubmits work. Recovery offers read-only checking,
an explicit exact-payload retry, and confirmed deletion of local tracking.
Identity-only evidence is labeled separately from a verified request match.
Forgetting a record never cancels host work. The Cancel button targets an exact
accepted input. Cancelling a local wait or closing this page does not.

Run the actual disposable acceptance fixture:

```sh
npm run pack:web
npm run smoke -w @whip/client-example
```

It uses production runtime processes, a private home, a local synthetic provider,
and Chromium. It covers transcript and scoped bytes; root/child selection;
live cells across page reload; offline process restart with an unsent draft;
Allow/Deny and batched human answers; a lost submission acknowledgement followed
by explicit exact recovery; and both Unix-socket and HTTP Node clients. It never
uses private retired SDK testing APIs, public provider accounts or installed
runtime state.

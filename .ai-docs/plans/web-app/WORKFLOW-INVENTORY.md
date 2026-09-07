# Web workflow inventory

Protocol v3 registry coverage, 2026-09-06. This inventory distinguishes implemented product surfaces from transport machinery, script-only operations, and explicit deferrals. A row marked **Web** identifies a concrete UI path; it does not claim that every path has already passed release-browser acceptance.

The operation list is checked against the generated manifest by `packages/app/test/workflow-inventory.test.ts`. Runtime behavior continues to use generated metadata rather than this document.

## Contract operations

| Registry operation | Status | Surface and behavior |
| --- | --- | --- |
| `rpc:command.status` | Internal | SDK command/query/ephemeral engine; application command notices, recovery, and typed service calls. |
| `rpc:command.submit` | Internal | SDK command/query/ephemeral engine; application command notices, recovery, and typed service calls. |
| `rpc:config.get` | Web | Host settings and inspector compaction defaults use captured configuration revisions. |
| `rpc:config.update` | Web | Host settings and inspector compaction defaults use captured configuration revisions. |
| `rpc:content.read` | Web | Composer attachments and explicit content previews/downloads through SDK content helpers. HTTP transfers on WebSocket; chunks on Unix. |
| `rpc:daemon.ping` | Internal | SDK connection and synchronized views, owned by AppRuntime. No application protocol reducer. |
| `rpc:daemon.restart` | Deferred | Process ownership remains CLI/future Electron shell. The web application attaches and detaches. |
| `rpc:daemon.stop` | Deferred | Process ownership remains CLI/future Electron shell. The web application attaches and detaches. |
| `rpc:events.replay` | Internal | SDK connection and synchronized views, owned by AppRuntime. No application protocol reducer. |
| `rpc:events.subscribe` | Internal | SDK connection and synchronized views, owned by AppRuntime. No application protocol reducer. |
| `rpc:events.unsubscribe` | Internal | SDK connection and synchronized views, owned by AppRuntime. No application protocol reducer. |
| `rpc:history.page` | Web | Conversation pagination and inspector collections through the shared SDK view; bounded references remain explicit. |
| `rpc:host.attention` | Web | Session sidebar/search and paged host attention; inactive roots are not opened. |
| `rpc:host.directories.list` | Web | Welcome host directory picker and composer host completions. |
| `rpc:host.themes.list` | Web | Appearance settings: shared builtin/custom host themes and bounded JSON import. |
| `rpc:host.themes.resolve` | Web | Appearance settings: shared builtin/custom host themes and bounded JSON import. |
| `rpc:initialize` | Internal | SDK connection and synchronized views, owned by AppRuntime. No application protocol reducer. |
| `rpc:mailbox.list` | Web | Inspector → Mail & shared state → Agent mailbox; revision-bound pages and explicit body/evidence reads. |
| `rpc:mailbox.read` | Web | Inspector → Mail & shared state → Agent mailbox; revision-bound pages and explicit body/evidence reads. |
| `rpc:operation.invoke` | Internal | SDK command/query/ephemeral engine; application command notices, recovery, and typed service calls. |
| `rpc:permission.decide` | Web | Shared request tray; competing answers resolve on the host. No signing or connection authentication. |
| `rpc:provider.key.rotate` | Web | Provider settings: rotate an existing Inference machine key on the execution host; refresh status after uncertainty without replaying the action. |
| `rpc:provider.key.set` | Web | Provider settings and reconnectable host-owned login choices. Secret submissions are ephemeral and never cached. |
| `rpc:provider.login.begin` | Web | Provider settings and reconnectable host-owned login choices. Secret submissions are ephemeral and never cached. |
| `rpc:provider.login.cancel` | Web | Provider settings and reconnectable host-owned login choices. Secret submissions are ephemeral and never cached. |
| `rpc:provider.login.list` | Web | Provider settings and reconnectable host-owned login choices. Secret submissions are ephemeral and never cached. |
| `rpc:provider.login.project.create` | Web | Provider settings and reconnectable host-owned login choices. Secret submissions are ephemeral and never cached. |
| `rpc:provider.login.project.select` | Web | Provider settings and reconnectable host-owned login choices. Secret submissions are ephemeral and never cached. |
| `rpc:provider.login.status` | Web | Provider settings and reconnectable host-owned login choices. Secret submissions are ephemeral and never cached. |
| `rpc:provider.login.team.select` | Web | Provider settings and reconnectable host-owned login choices. Secret submissions are ephemeral and never cached. |
| `rpc:provider.logout` | Web | Provider settings and reconnectable host-owned login choices. Secret submissions are ephemeral and never cached. |
| `rpc:provider.status` | Web | Provider settings and reconnectable host-owned login choices. Secret submissions are ephemeral and never cached. |
| `rpc:provider.validate` | Web | Provider settings: validate an entered key without saving it. The normal key-setup service already validates before saving, avoiding duplicate requests. |
| `rpc:query` | Internal | SDK command/query/ephemeral engine; application command notices, recovery, and typed service calls. |
| `rpc:root.collection` | Web | Conversation pagination and inspector collections through the shared SDK view; bounded references remain explicit. |
| `rpc:root.snapshot` | Internal | SDK connection and synchronized views, owned by AppRuntime. No application protocol reducer. |
| `rpc:sessions.list` | Web | Session sidebar/search and paged host attention; inactive roots are not opened. |
| `rpc:sessions.revision` | Internal | SDK connection and synchronized views, owned by AppRuntime. No application protocol reducer. |
| `rpc:upload.begin` | Web | Composer attachments and explicit content previews/downloads through SDK content helpers. HTTP transfers on WebSocket; chunks on Unix. |
| `rpc:upload.chunk` | Web | Composer attachments and explicit content previews/downloads through SDK content helpers. HTTP transfers on WebSocket; chunks on Unix. |
| `rpc:upload.finish` | Web | Composer attachments and explicit content previews/downloads through SDK content helpers. HTTP transfers on WebSocket; chunks on Unix. |
| `rpc:workspace.complete` | Web | Welcome host directory picker and composer host completions. |
| `runtime:agent.control` | Web | Inspector agent tree exposes only daemon-advertised stop/delete controls. |
| `runtime:agent.delete` | Web | Inspector agent tree exposes only daemon-advertised stop/delete controls. |
| `runtime:agent.submit` | Web | Root/child composer with explicit delivery, application-owned drafts, and scoped attachments. |
| `runtime:agent.transcript` | Internal | Web uses bounded SDK root snapshots/collections and history pages instead of unbounded convenience queries. |
| `runtime:agent.turn.cancel` | Web | Conversation and inspector target the captured active turn, including children. |
| `runtime:agents.list` | Internal | Web uses bounded SDK root snapshots/collections and history pages instead of unbounded convenience queries. |
| `runtime:browser.set_driver` | Web | Inspector → Host integrations: host capability/status/diagnostics, browser driver and computer app policy, bounded tool schema inspection. |
| `runtime:browser.status` | Web | Inspector → Host integrations: host capability/status/diagnostics, browser driver and computer app policy, bounded tool schema inspection. |
| `runtime:budget.cap` | Web | Inspector → Usage & authority: exact decimal counters and advertised budget controls/revocation. |
| `runtime:cancel` | Web | Conversation and inspector target the captured active turn, including children. |
| `runtime:capability.revoke` | Web | Inspector → Usage & authority: exact decimal counters and advertised budget controls/revocation. |
| `runtime:compaction.configure` | Internal | Web edits the same host compaction defaults using config.update revision checks, then explicitly reloads the idle session. |
| `runtime:computer.allow` | Web | Inspector → Host integrations: host capability/status/diagnostics, browser driver and computer app policy, bounded tool schema inspection. |
| `runtime:computer.deny` | Web | Inspector → Host integrations: host capability/status/diagnostics, browser driver and computer app policy, bounded tool schema inspection. |
| `runtime:computer.status` | Web | Inspector → Host integrations: host capability/status/diagnostics, browser driver and computer app policy, bounded tool schema inspection. |
| `runtime:context.audit` | Web | Inspector → Context & model → Applied context/workspace; paths and context describe the execution host. |
| `runtime:daemon.checkpoint` | Internal | Execution-host persistence control, available to SDK/CLI; not a product action. |
| `runtime:goal.from-context` | Web | Inspector → Goals & schedules: save/run/clear/form goal, create/delete schedules. |
| `runtime:goal.run` | Web | Inspector → Goals & schedules: save/run/clear/form goal, create/delete schedules. |
| `runtime:goal.set` | Web | Inspector → Goals & schedules: save/run/clear/form goal, create/delete schedules. |
| `runtime:history.clear` | Web | Welcome/sidebar/session menu and conversation history controls. Fork/rewind/clear use displayed history revision; rewind confirms possible file restoration. |
| `runtime:history.compact` | Web | Inspector → Context & model → Compaction: compact, inspect retained summaries, undo latest compaction. |
| `runtime:history.compact.log` | Web | Inspector → Context & model → Compaction: compact, inspect retained summaries, undo latest compaction. |
| `runtime:history.compact.retry` | Web | Inspector → Context & model → Compaction: compact, inspect retained summaries, undo latest compaction. |
| `runtime:history.rewind` | Web | Welcome/sidebar/session menu and conversation history controls. Fork/rewind/clear use displayed history revision; rewind confirms possible file restoration. |
| `runtime:history.user.list` | Internal | Web navigation uses lightweight paged sessions.list metadata and selected-session history; no global prompt-history mirror. |
| `runtime:lsp.status` | Web | Inspector → Host integrations: host capability/status/diagnostics, browser driver and computer app policy, bounded tool schema inspection. |
| `runtime:mcp.attach` | Web | Inspector → Host integrations → MCP: status/lifecycle/imports and ephemeral private session configuration. |
| `runtime:mcp.disable` | Web | Inspector → Host integrations → MCP: status/lifecycle/imports and ephemeral private session configuration. |
| `runtime:mcp.enable` | Web | Inspector → Host integrations → MCP: status/lifecycle/imports and ephemeral private session configuration. |
| `runtime:mcp.import.configure` | Web | Inspector → Host integrations → MCP: status/lifecycle/imports and ephemeral private session configuration. |
| `runtime:mcp.import.status` | Web | Inspector → Host integrations → MCP: status/lifecycle/imports and ephemeral private session configuration. |
| `runtime:mcp.reconnect` | Web | Inspector → Host integrations → MCP: status/lifecycle/imports and ephemeral private session configuration. |
| `runtime:mcp.status` | Web | Inspector → Host integrations → MCP: status/lifecycle/imports and ephemeral private session configuration. |
| `runtime:permission.forget` | Web | Inspector → Permissions: connected-client/host policy, deny interactive permissions, inspect and forget saved rules. |
| `runtime:permission.mode` | Web | Inspector → Permissions: connected-client/host policy, deny interactive permissions, inspect and forget saved rules. |
| `runtime:permission.rules` | Web | Inspector → Permissions: connected-client/host policy, deny interactive permissions, inspect and forget saved rules. |
| `runtime:provider.catalogs` | Web | Rootless welcome/provider settings and inspector model catalog. |
| `runtime:question.answer` | Web | Shared request tray: options, multiple answers, freeform input, and dismissal. |
| `runtime:run.configure` | Web | Inspector → Context & model → Model & reasoning, with idle checks and explicit advanced runtime overrides. |
| `runtime:schedule.create` | Web | Inspector → Goals & schedules: save/run/clear/form goal, create/delete schedules. |
| `runtime:schedule.delete` | Web | Inspector → Goals & schedules: save/run/clear/form goal, create/delete schedules. |
| `runtime:schedule.list` | Internal | Schedule inspection comes from bounded root snapshot/collection pages. |
| `runtime:session.autotitle` | Web | Inspector context: enable automatic titles. The existing one-way operation has no disable or readback contract, so the UI does not invent a toggle. |
| `runtime:session.create` | Web | Welcome/sidebar/session menu and conversation history controls. Fork/rewind/clear use displayed history revision; rewind confirms possible file restoration. |
| `runtime:session.delete` | Web | Welcome/sidebar/session menu and conversation history controls. Fork/rewind/clear use displayed history revision; rewind confirms possible file restoration. |
| `runtime:session.effort` | Web | Inspector → Context & model → Model & reasoning, with idle checks and explicit advanced runtime overrides. |
| `runtime:session.effort.get` | Internal | The web reads authoritative model/effort from the shared root snapshot instead of duplicate queries. |
| `runtime:session.fork` | Web | Welcome/sidebar/session menu and conversation history controls. Fork/rewind/clear use displayed history revision; rewind confirms possible file restoration. |
| `runtime:session.list` | Internal | Web navigation uses lightweight paged sessions.list metadata and selected-session history; no global prompt-history mirror. |
| `runtime:session.model` | Web | Inspector → Context & model → Model & reasoning, with idle checks and explicit advanced runtime overrides. |
| `runtime:session.model.get` | Internal | The web reads authoritative model/effort from the shared root snapshot instead of duplicate queries. |
| `runtime:session.open` | Web | Welcome/sidebar/session menu and conversation history controls. Fork/rewind/clear use displayed history revision; rewind confirms possible file restoration. |
| `runtime:session.preview` | Internal | Web navigation uses lightweight paged sessions.list metadata and selected-session history; no global prompt-history mirror. |
| `runtime:session.reload` | Web | Inspector → Context & model → Model & reasoning, with idle checks and explicit advanced runtime overrides. |
| `runtime:session.rename` | Web | Welcome/sidebar/session menu and conversation history controls. Fork/rewind/clear use displayed history revision; rewind confirms possible file restoration. |
| `runtime:shell.run` | Deferred | Shell commands and interactive terminals are explicitly excluded from this web milestone. |
| `runtime:steer` | Web | Root/child composer with explicit delivery, application-owned drafts, and scoped attachments. |
| `runtime:submit` | Web | Root/child composer with explicit delivery, application-owned drafts, and scoped attachments. |
| `runtime:terminal.input` | Deferred | Shell commands and interactive terminals are explicitly excluded from this web milestone. |
| `runtime:tool.call` | Deferred | Manual execution console remains outside this conversation-first milestone. TUI has no direct tool.call invocation workflow; daemon adapters/scripts retain typed access. Agent execution, schema inspection, and permissions remain in scope. |
| `runtime:tool.configure` | Web | Inspector → Permissions: connected-client/host policy, deny interactive permissions, inspect and forget saved rules. |
| `runtime:tool.schema` | Web | Inspector → Host integrations: host capability/status/diagnostics, browser driver and computer app policy, bounded tool schema inspection. |
| `runtime:workspace.inspect` | Web | Inspector → Context & model → Applied context/workspace; paths and context describe the execution host. |
| `runtime:workspace.set` | Web | Inspector → Context & model → Applied context/workspace; paths and context describe the execution host. |

## Runtime state beyond individual operations

- Agent tree status, active turns, pending questions/permissions, budgets, grants, schedules, blackboard, and Starlark/tool presentation come from the SDK SessionView. Inspector sections mount only while selected; their polling and uncached queries stop when unobserved.
- Mailbox pages use the existing store, not the command inbox or another history database. The UI reads bodies/evidence explicitly and never acknowledges or completes mail. Revision conflicts restart paging from the first page.
- The command inbox is shown as authoritative admitted input while work runs. It is separate from committed history; turn commit persists messages and consumes input atomically. The app does not fabricate transcript IDs or deduplicate by text.
- Transcript content can be text or multimodal content parts. Images/text uploads have exact root/recipient grants and bounded, integrity-checked host resolution. Large transcript/collection entries remain references with explicit bounded read/download controls.
- Executed Starlark programs, host calls, and results are inspectable through bounded transcript/live evidence. **Raw VM scratch globals are unavailable:** they are not reconstructible from this evidence, and this milestone adds no mutable VM introspection API.
- The protocol currently has no authoritative readback of the connected-client permission policy or the complete per-session run-override configuration. The inspector labels controls as settings to apply rather than pretending its local defaults are observed host state.
- Child deletion/stop controls come from actual allowed_controls. Targeted cancellation uses active_turns. No UI action expands a child transcript into a parent model context.

## Evidence and remaining acceptance

- Host, mailbox, directory/theme discovery, attachment grants/limits/integrity, clear revision checks, and generated contract behavior have Go tests; affected race suites run separately.
- Built SDK acceptance exercises Unix/WebSocket equivalents, multimodal snapshot/history/SessionView, changed payloads and grants, clear revision invalidation, and queued attachment crash recovery.
- `packages/app/test/inspector.test.tsx` covers actual agent control payloads, stale-target avoidance, goal/schedule commands, saved-rule deletion, bounded mailbox paging/revision recovery, and ephemeral MCP secret handling. It is a component workflow test using a fake SDK service, not a real provider/OS integration test.
- Application lifetime/recovery, conversation/request workflows, theme accessibility, production packaging, and browser/mobile tests are tracked by their own suites and the main plan. Provider validation/rotation and the one-way automatic-title enable action have focused component tests, including secret disposal on host switch. Arbitrary manual execution, terminal/editor/review work, and runtime process management remain deferred; this inventory does not assert full TUI parity.

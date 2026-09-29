# Backend domain and persistence

This is the implemented domain contract for the
[backend redesign](backend-redesign-plan.md). The new runtime, runner, RPC and
SDK and supported applications use these records directly. The retired runtime
and SDK have been removed; [the retirement record](backend-native-core-retirement.md)
maps their retained behavior to native implementations. Final combined and
platform acceptance remains separate from this implemented contract.

## Ownership

| Fact | Authority |
| --- | --- |
| Runtime identity and schema version | Fresh SQLite database |
| Host provider declarations | Revisioned host file; config authority reads fresh bytes and serializes publication |
| ChatGPT credentials and login generation | One command-owned account manager and its private credential file |
| Inference.net management credentials and machine key | One command-owned credential manager with separate lifetimes in its private file |
| Pending account approval | Bounded host-service flow in memory; never session input or SQL execution state |
| Definition defaults and declarations | Immutable definition revision document |
| Tree metadata and common engine | Tree row |
| Reusable subtree capacity limits | Revisioned resource-limit rows; usage derived from live owning records |
| Scoped execution permission | One SQL permit per executing turn; released at a settled wait boundary or terminal settlement |
| Permanent logical-write usage | Immutable SQL write charges and captured ancestor associations; retained until tree deletion |
| Permanent model spending limits | Revisioned budget-limit rows; usage derived from immutable attempts |
| Session identity, immutable parent/tree, definition origin, lifecycle | Session row |
| Effective configuration | Immutable configuration revision selected by the session |
| Configuration used by execution | Turn's pinned configuration revision |
| Captured instruction source paths, byte counts and digests | One immutable instruction manifest per ordinary turn in SQLite; full composed text lives only in that turn's execution memory |
| Schedule template and next occurrence | One schedule row; immutable admitted occurrences belong to ordinary inputs |
| Goal objective, allowance, revision and state | One goal row; current selection derives from its owner's latest creation ordinal |
| Accepted goal work | Ordinary input with immutable goal ID/revision provenance |
| Goal selected for execution | Immutable goal ID/revision on the turn; text reads the goal row |
| Goal completion intent | Successful authorized operation; the goal retains terminal turn/operation references |
| Goal formulation source | Immutable maintenance-input snapshot of the raw-history window and activation request |
| Goal formulation result | Immutable attempt-linked candidate and rejection; accepted goals retain the origin attempt |
| Accepted input kind and payload | Input row (`prompt`, `host_operation`, `compact`, `goal_formulation` or `automatic_title`); turn kind is a read projection |
| Request identity and payload digest | Receipt row |
| Execution outcome | Turn row; input and receipt outcomes are derived |
| Automatic report awaiting publication | Parent-owned completion slot with exact terminal outcome snapshot |
| Published completion report | Immutable parent-owned content plus canonical revisioned mail |
| User transcript payload | Reference to the accepted input; no second body copy |
| Assistant/tool transcript payload | Message row |
| Derived context summary and exact raw coverage | Immutable compaction row, linked to its accounted model attempt |
| Selected context summary | One revisioned context-head row per session |
| Provider dispatch, usage, price snapshot and cost | Model-attempt row, linked to its completed message |
| Stateless helper origin | Immutable operation ID and batch index on each model attempt; ordered output belongs to the operation result |
| Code dispatch, outcome and exact REPL boundary | Cell row, linked to its assistant call and tool-result message |
| Checkpoint compatibility metadata and body reference | Immutable terminal cell checkpoint |
| Host-operation intent, dispatch and outcome | Operation row, owned through its cell or directly through a human-operation turn; never a fabricated cell |
| Exact session/capability/resource authority | Immutable grant row; revocation is a terminal fact |
| One-use consent decision | Permission row, linked to its exact operation |
| Checkpoint image bytes | Durable immutable blob file, verified before restore |
| Content digest and size | Immutable content-body metadata row |
| Session access and declared media type | Immutable content-reference row |
| Content bytes | Durable immutable blob file, verified when read |
| Provider endpoint and credential reference | Explicit host configuration file; subscription routing is adapter-owned |
| Subscription credentials | Private host credential file; captures and refresh work live in manager memory |
| Incomplete provider text, reasoning and tool-call previews | Bounded runtime memory; never transcript rows |
| Resolved credential, worker, interpreter, process or client | Execution memory; never session rows |

Root and child sessions have the same records and store methods. Root lookup is
derived from the sole null parent in a tree. Tree creation inserts tree and root
in one transaction. Foreign keys enforce same-tree parents, a partial unique
index allows only one root, and immutable topology plus existing-parent insertion
prevents cycles. SQL guards only reject invalid writes; they never orchestrate
work. No public operation reparents a session.

Definitions are content-addressed immutable revisions, including built-ins.
Registering a changed document produces another revision, leaving existing
references intact. Available tools, hooks and child templates are declarations;
they grant no authority and contain no executable bindings.
Opening storage idempotently registers the built-ins shipped with that binary.
An edited built-in adds its content-addressed revision; existing sessions keep
their exact original references. Definition lookup always requires a revision.
Schemas are validated and resolved locally; remote references cannot fetch
network data or change a pinned declaration later.

Configuration resolution is host/parent defaults, pinned definition defaults,
then explicit overrides. Patches replace whole fields. Nil inherits; an empty
collection clears. An explicit output policy with a null schema clears a
structured output contract. Resolution owns deep copies of all collections and
schemas. Dynamic project files and skill discovery are policies, not frozen
contents. An ordinary turn refreshes authorized workspace sources once and
freezes the resulting instruction text for its execution.

Model selection also captures nullable `temperature` (0–2) and `top_p` (0–1).
Only finite values are accepted. Null or omitted values use the provider default;
explicit zero is retained. Replacing the model replaces the complete selection,
so omitted sampling fields clear previous choices. Children inherit the full
selection unless a definition or override replaces it. Compaction inherits that
same selection when its model is null; an explicit compaction model carries its
own effort and sampling values. Each prepared attempt freezes those values.
Chat profiles support both fields. API Responses and subscription routes reject
explicit sampling before authentication or dispatch.

Configuration updates compare the expected revision and append a new immutable
revision. A running turn retains its captured revision; the next claim captures
the current one. Changing model selection never rewrites history, topology or
checkpoints. Host route changes affect future dispatch through the named route.
Each prepared request records its actual route/pricing snapshot on the model attempt.
When spawning without a new definition, the child copies the parent's current
effective configuration before applying overrides. It does not reapply the
original template and accidentally undo explicit parent updates. Selecting a
different pinned definition applies that template between parent defaults and
overrides. Working directories are absolute and fixed for a session's lifetime.

## Atomic transitions

1. **Admission:** identity is (client_id, request_id) within one runtime.
   The digest covers operation kind and typed submission, including recipient and source.
   A receipt and input commit together. Equal retries return the same receipt;
   changed payload reuse conflicts. Queues are bounded. No worker starts here.
2. **Claim:** an eligible queued input or due mail starts a new turn; the
   current config revision is captured. A prompt-backed user transcript entry
   references its input, and mail entries reference immutable mail revisions.
   Compact inputs create neither kind of transcript entry and consume no mail. One transaction and a partial unique index enforce one active turn
   per session. Independent store connections must obey the same invariant.
   Each turn claims at most one input; mail-only turns claim none. Input batching
   is not implicit. FIFO input order comes from the insertion ordinal.
3. **Transcript append:** stable message identity and per-session sequence make
   retries idempotent. Completed entries survive restart even before turn finish.
   Tool calls are assistant parts; tool results reference their call ID and
   occupy their own tool message. Input cannot inject either. Another assistant
   message cannot pass unanswered calls. Root and child use the same table.
   Provider deltas are disposable previews keyed to the eventual message ID.
   Only validated completed responses become transcript messages.
4. **Finish:** optional completed messages and the terminal turn outcome commit
   together. Input and receipt observations join the turn rather than copying
   terminal states. A failed persistence attempt can retry this transaction; it
   must not call a provider or repeat an effect.
5. **Model dispatch and settlement:** reservation precedes exclusive dispatch.
   One attempt identifies one actual request, with a logical-call identity for
   retries. Terminal outcome, usage, calculated/provider cost and its completed
   assistant message commit together. Retrying settlement cannot redispatch.
   Missing usage or prices remain unknown; explicitly free prices can prove zero.
   Arithmetic overflow preserves output and evidence with unknown cost.
6. **Recovery:** opening a store never starts or interrupts execution. After
   acquiring exclusive runtime ownership, the runtime explicitly interrupts
   nonterminal turns. Unclaimed queued inputs remain queued; claimed inputs stay
   linked to the interrupted turn and are never automatically requeued.
   In the same transaction, reserved attempts become cancelled with known zero
   cost and dispatched attempts become uncertain. A turn cannot finish while
   an attempt, operation or code cell remains unsettled. Waiting/ready operations
   become cancelled; dispatched operations become uncertain. Pending permissions
   are cancelled before cells settle, so a lost live waiter cannot resume.
   Recovery also records an uncertain
   result for an admitted unfinished cell, and a not-dispatched result for an
   unanswered assistant call that never acquired a cell record.

Turn transitions are running → cancelling → cancelled/interrupted or
running → succeeded/failed/cancelled/interrupted. Terminal outcomes cannot
change. Cancellation requests persist intent; they do not claim that an external
effect has stopped. Operation dispatch and settlement record that evidence separately.

Queued input cancellation removes its eligibility. Claimed input cancellation
targets its turn. Stopping a session pauses admission/claims and requests
cancellation of active execution while retaining queued input. Resuming permits
claims after active execution has settled. Disconnecting a client changes none
of these records.
The stop transaction returns the exact cancelling turn identity as transient
cleanup data. Runtime cancellation uses that identity; a delayed stop response
cannot target a newer turn after reactivation. No duplicate lifecycle row is needed.

Deletion is explicit and atomic for a subtree (or the entire tree when targeting
its root). Active turns make deletion conflict until cancellation/cleanup has
completed. Descendant transcripts, inputs, turns and config revisions are removed.
Receipts retain identity/digest with a deletion marker, so retrying a deleted
submission cannot recreate work. Receipt retention or runtime identity reset
must be an explicit future retention policy. Definition documents belong to the
catalog, so deleting a session does not delete its template.
Queued inputs in the deleted subtree are discarded, with the same receipt
deletion markers. Stopping one session affects that session; it does not silently
stop descendants. Child admission is atomic; grant-backed child controls use the
same cancellation and deletion paths as root sessions.

Collection queries accept 1–100 rows and also stop at a 4 MiB payload budget.
Resume after the last returned session ID, definition ID/revision, or transcript
sequence. An empty page means the end; a short page can reflect the byte budget.
Messages and configuration documents are bounded at admission. SQL timestamps
use integer UTC microseconds, avoiding mixed-precision textual ordering.

## Goal records and admission

A goal row owns immutable text and continuation allowance, plus revision, state
and consumed continuations. The latest creation ordinal is current even after
completion or cancellation; no second selection pointer exists. At most one goal
per session is armed or paused. Replacing text creates a new goal ID and requires
the expected current ID/revision (or null if no goal has existed). Exact ID/digest
retries return the original admission before checking later lifecycle or state,
so replay cannot reselect an old goal.

Creation atomically replaces any open goal, cancels its unclaimed inputs, charges
the objective text and optionally admits the first ordinary prompt. Failure at
any write or capacity check rolls everything back. Goal inputs retain only their
provenance and a fixed instruction; they do not duplicate the objective text.
The internal `goal` receipt namespace cannot be submitted by public callers.
A missing continuation allowance means 100; explicit zero is supported.

Resume uses an ordinary caller request identity and expected goal revision. It
admits one prompt and advances the revision without resetting consumed allowance.
Busy, stopped, ineligible or exhausted goals refuse new admission. Cancellation
addresses a stable goal ID, cancels unclaimed goal inputs and returns only the
exact active goal-owned turn for runtime cancellation; a separate human turn
remains independently owned. Session deletion clears objective text but retains
an identity tombstone so an old creation retry cannot recreate work.

`goals_enabled` is copied configuration, not authority. Assistant definitions
enable eligibility by default; an explicit false override remains false. Existing
children and active turns retain their captured configurations.

Every ordinary prompt turn, including human, mail and schedule work, captures the
current armed goal ID/revision when eligible. A goal-owned input must match that
exact revision; stale or disabled queued inputs are cancelled in a committed
cleanup transaction. Maintenance turns capture no goal. The runner reads the immutable
objective once and freezes it with that turn's instructions through correction,
provider retries and compaction; helper requests do not inherit it.

Successful turn settlement either applies an authorized completion intent or
admits one ordinary continuation and increments the durable allowance count in
the same transaction. An already outstanding goal input prevents duplicate
continuation. Semantic queue/write pressure rolls back only the proposed
admission, then pauses the goal while preserving successful turn/accounting
settlement. SQL errors roll back the whole transaction for SQL-only retry.
Exhaustion, cancellation, interruption, failure, uncertain execution or invalid
captured output pause continuation. Current stop/eligibility changes suppress it.
The initial run does not consume an additional continuation; an ordinary turn
that already worked on a start=false goal still counts as having started.

`ApplyGoalCompletion` validates the exact captured goal and tree resource, then
dispatches and settles a normal authorized `goals.complete` operation atomically.
Its bounded UTF-8 evidence lives in the immutable operation arguments. It does
not immediately complete the goal. Finish requires a successful turn, valid
captured output, no uncertain execution and the unchanged armed goal; the goal
then stores only completion turn/operation references. Revocation before dispatch
blocks the intent. An already authorized successful operation remains evidence
after later revocation; goal cancellation or replacement still defeats completion.
The legacy `GOAL_MET` text heuristic is not used.

Runtime controls and `goals.create/current/get/resume/cancel` expose these same
store transitions. The SDK's matching methods retain no goal state. Creation
returns an optional ordinary admission whose receipt belongs to the reserved
`goal` client namespace; inspect it using its full returned identity. Caller-owned
resume receipts use the SDK's ordinary recover/wait helpers. A create retry can
return a superseded goal with `current: false`, or a deletion tombstone, without
changing selection. Current selection alone never implies the goal is armed.

Both guest engines expose typed `goals.complete` through the normal operation
dispatcher. Its success accepts an intent; clients observe the goal and terminal
turn independently to determine whether completion actually committed. Aborting
client observation does not cancel goal execution. Product-client adoption remains
pending.

`goals.formulate` admits an ordinary durable maintenance input. Admission freezes
an explicit raw-history tail (default eight messages, optional 2–100, at most
4 MiB); the ordinary turn claim captures its main model configuration. The
helper receives the source as inert JSON data, its fixed formulation instruction
and the complete captured model selection. It performs no tool execution,
content hydration, mail delivery, transcript write, output-contract correction,
compaction or ordinary preview, and discards private provider continuation.

Billing, a valid immutable candidate, activation CAS and optional initial goal
input settle in one transaction. Semantic activation refusal preserves billing
and the rejected candidate; a real SQL error rolls back all settlement for a
SQL-only retry. Invalid output is billed without creating a candidate. Replaying
settlement cannot activate an earlier rejection. A crash after successful
activation preserves the goal/input even if the maintenance turn is interrupted.

`goals.formulation` reads the candidate by owner and attempt ID. Its `accepted`
flag describes that immutable activation decision, independently of current goal
state and maintenance-turn outcome. The goal's nullable
`origin_formulation_attempt_id` preserves the association. Candidate evidence and
charge provenance survive child deletion; an old admission retry returns its
receipt tombstone. The SDK uses its ordinary caller identity and recover/wait
methods for formulation, without a separate job cache or provider invocation.

## Content boundary

Content-reference identity is `(owner_session_id, reference_id)`. The same opaque
handle may name different bytes in different sessions. Every read, registration
retry and permanent logical-write identity includes the owner; an exact retry in
one owner remains free. Bodies are still deduplicated by digest. A digest or a
handle belonging to another session does not grant access. Mail evidence binds
the recipient and reference through composite foreign keys, so it cannot point
at another owner's row. Explicit child/mail sharing still creates recipient
aliases; this foundation permits a later fork to preserve handles without
rewriting opaque text. Conversation rewind preserves these independent references.
Fork atomically preserves those owner-scoped handles in the destination.

`content.put` publishes bytes durably before registering body metadata and a
session reference. A caller-supplied reference ID makes upload retries idempotent;
changing its owner, bytes or media type conflicts. The body table owns digest and
size once, while references own session access and media interpretation. A digest
alone never authorizes reading. The trusted client can select a session; runner
hydration always uses the actual executing session's identity.

Input admission and completed-message insertion validate every reference in the
same SQL transaction as their write. Missing and foreign references produce the
same not-found outcome, without leaving an input, receipt or output. Existing
admission/message retries preserve their original idempotency behavior.

Bodies and reads are limited to 4 MiB. Each session may retain 1,024 references
and 64 MiB of referenced bytes (including repeated references to one body).
Provider context hydration is bounded in aggregate and checks file size/digest;
encoding also counts repeated occurrences. Text and supported images become
temporary provider payloads, while durable messages retain only reference IDs.
Special files and symlinks cannot substitute for a body during verified reads.

Session deletion removes references transactionally. Shared bytes remain available
to surviving owners; checkpoint references also retain their image files. Physical orphan collection runs only during exclusive
startup, before uploads or requests can run, in bounded directory batches. This
also collects files left by successful publication followed by failed SQL.
No live deletion can race the gap between publication and registration.

## Host and schema boundary

Initialization takes explicit paths and never discovers an installed daemon or
reads the retired home/config. A new host config is valid but unconfigured: users
must select a model/provider before ordinary model work. Explicit model-free
sessions can run [direct human operations](../internal/store/host_operation.go).
API credentials use explicit sources resolved during request preparation.
Subscription credentials
belong to the independent host account manager and its private file.

Current fresh [host configuration](../internal/config/host.go) is version 21;
the [SQLite schema](../internal/store/store.go) is version 57. Version numbers in
the implementation histories below identify their introducing checkpoints, not
additional formats accepted by the current binary.
SQLite has an application identifier and schema version. Schema55 and56 upgrade atomically to57. Existing child policy records become
ongoing inheritance relationships. Missing records are restored only from a
successful immutable agent spawn and its matching admission receipt proving
default grant selection and the same workspace. Older direct-client receipts
without the original selection remain restricted; the migration never guesses
that an explicit restriction was absent. Other applications and versions are rejected.
Reopening preserves runtime identity, session data and seeded revisions; separate
databases receive distinct identities. Downgrading requires restoring the
pre-upgrade database together with its matching binary.
The store owns database transactions only, with no resource-manager construction.

Generic API routes select one credential source. Configuration reads do not
resolve credentials, and missing credentials never fall back to another source,
the installed runtime or the retired `~/.inf/config`.

| `credential_source` | Declaration and resolution |
| --- | --- |
| `env` | One `credential_env` name, resolved from the explicit host lookup |
| `file` | One canonical absolute `credential_file` path to a bounded private regular file owned by the current user |
| `command` | `credential_command` with an absolute executable, argument array and explicitly inherited environment-variable names |
| `none` | No credential declaration or authorization header |
| `inference-net` | The separately owned machine-key binding described below |

An omitted source retains the existing environment-reference or no-auth shorthand;
it cannot infer file or command intent. Subscription routes reject all API source
fields. File resolution anchors every directory component without following
symlinks; use canonical paths, including `/private/var` rather than `/var` on macOS.
The immediate parent must belong to the current user and deny other-user writes;
the file must deny other-user reads/writes. Reads check ownership, mode, size and
modification stability and never return file contents in errors.

Credential commands run without a shell, stdin or inherited ambient environment,
in `/`, with only the named environment values. Arguments and environment are
bounded, execution has a ten-second deadline, and combined stdout/stderr is
limited to 64 KiB. Cancellation or completion kills remaining process-group
members and joins the leader and bounded output readers. Diagnostics never echo
arguments, environment, stdout or stderr. Resolution accepts one bounded printable
key with surrounding whitespace removed. Both Chat and Responses freeze that key
at preparation, so retries neither rerun commands nor reread replaced files.
Private pasted/named-key installation and public provider setup remain separate work.

Inference.net routes select their private machine-key owner explicitly with
`credential_source: "inference-net"`. Only `openai-chat` at the exact
`https://api.inference.net/v1` gateway can use that source, and it cannot also
supply `credential_env`. Matching a URL never implicitly selects credentials.
Environment and unauthenticated routes remain independent. The command constructs
one `inferenceauth.Manager` after acquiring runtime ownership, injects it before
starting execution, and closes it after runtime work has joined. Construction
anchors the explicit directory but does not read the private record; unrelated
providers and scripted execution never load it.

The manager stores management authorization separately from machine-key identity
and team/project scope. Management expiry does not expire an otherwise available
machine key. Preparation captures one key and generation for all recorded retries;
checks after reservation and immediately before HTTP reject replacement or logout.
Private keys and management tokens never enter request snapshots, session records
or public projections. These checks prove authorization at each check, without
claiming atomic revocation of an HTTP request already dispatched.

The private record uses owned regular files, bounded reads, durable atomic
publication and directory confirmation before first use. Visible replacements
invalidate earlier captures even if durability confirmation fails. Known local
publication failures retry persistence without minting another key. Logout revokes
local captures before removal; unresolved removal blocks use until explicit retry.
The command also owns one `inferenceaccount.Service`, borrowing this manager and
the revisioned host configuration authority. Device authorization, team/project
selection, explicit project creation and key rotation are bounded host flows,
independent of SQL session receipts. The pinned control-plane client has no cookie
jar or redirects; responses, choices, flow count and durations are bounded. A
lost acknowledgement is recovered by list/get. A prior-process ID reports
interrupted, and an uncertain remote creation is never retried automatically.

Newly approved management credentials cannot inherit another account's machine
key. Known credential publication retries persist the saved result without
reminting; setup installs only the exact managed gateway and changes no defaults.
Key replacement is durable before old-key archival. Management expiry remains
independent from machine-key validity. Safe status distinguishes both local
credential lifetimes from declared route availability, without proving network
readiness.

Logout first revokes local authority, then reports remote key/session cleanup
separately. Cleanup retries retain only the exact previous authority and cannot
borrow a newer account. Local persistence failure remains explicit even after
remote cleanup succeeds. Flows and cleanup each retain at most 64 entries for
15 minutes within the current process. Expiry/restart is lost evidence, never
proof of remote success. RPC borrows these command-owned services through
`HostServices`; clients expose explicit calls without a second account manager.

The new runtime implements the OpenAI-compatible Chat Completions, API Responses
and ChatGPT subscription adapters and both Starlark and QuickJS subprocess engines. These are protocol/engine adapters,
not a hardcoded commercial model or credentials. These cover the retained
inference protocols. ChatGPT account onboarding now has host-owned RPC/SDK
controls, as does Inference.net onboarding; presets, catalogs, readiness and
product integrations are still being ported.

Chat requests omit `reasoning_effort` when the captured selection is `off`.
Their `prompt_cache_key` comes from the session ID, so it is stable across turns
and retries without another stored value; IDs longer than 64 bytes use their
SHA-256 hex digest. The pinned Cerebras, Groq, DeepSeek, Fireworks, Together and
DeepInfra API roots omit this field. The exact `https://api.deepseek.com` root
also requests `thinking: {type: "disabled"}` for `deepseek-v4-` models and omits
the wire effort, because the current transcript cannot replay that provider's
reasoning content. Only complete preset roots select these profiles after
trailing-slash normalization; custom paths, ports and lookalike hosts retain the
generic encoding. The attempt retains the original selected effort and a digest
of the actual encoded body, frozen before admission. These are locally tested
wire contracts, not live-provider availability claims.

Host routes select `openai-chat` or `openai-responses` explicitly. Responses uses
`/responses`, `store: false`, encrypted reasoning inclusion, bounded text/function
streaming, user images, and the same frozen output cap, credential resolution,
attempt accounting and sanitized failure rules. A `response.completed` envelope
with completed status is required; completed item events supply output if the
terminal envelope omits it. Failed, incomplete or malformed output never becomes
executable parts. Independently valid usage and cost still settle. Only complete,
recognized HTTP context rejections permit the runner's recorded smaller-request
replan; streams and transport failures never enable an automatic retry.

Responses continuation is immutable private provider evidence on its assistant
message, committed in the same transaction as the message and attempt. It is
bounded to 1 MiB including its encoded envelope. A credential-keyed HMAC binds it
to the resolved route and model without storing credentials; the adapter also
re-decodes its visible parts before replay. Route, credential, model or visible
part changes use the canonical visible transcript instead. There is no mutable
provider conversation cache. Restart reads the selected messages' private values
through an owner-scoped store query with a 4 MiB cumulative prefetch bound;
sizes are checked before loading blobs. Private bytes count toward ordinary request and context pressure
limits, so excess follows normal compaction. Compaction helpers receive only the
canonical visible history and discard their own continuation. Public history,
search, attempts, operations, RPC and guest context reads never include opaque
provider state.

Host routes may also select `openai-codex`. That host declaration requires empty
`base_url` and `credential_env`: the subscription adapter owns its fixed endpoint,
and the host's independent `openaiauth.Manager` owns account credentials. The
command constructs one manager, resolves credentials lazily at preparation, and
joins its refresh work after runtime shutdown. API and scripted routes do not
read subscription credentials. No credential enters a session, request snapshot,
operation result or public history.

An unspecified output cap resolves to the pinned model-specific natural ceiling
before generic host defaults. The subscription endpoint has no wire output cap;
unknown model ceilings and explicitly smaller caps fail before credential
capture. Context bounds remain explicit host declarations. Missing prices stay
unknown rather than becoming free or borrowing an API rate; finite budgets
refuse requests whose exposure cannot be bounded.

Captures contain account credentials and login generation. Logout or any login
installation invalidates prepared work; token rotation within a login does not.
The runner rechecks after reservation, then the adapter checks again before HTTP.
These checks are not an atomic transaction with the network. Before ledger
dispatch, refusal releases the unused reservation. After ledger dispatch it
retains conservative failed/unknown evidence while sending no HTTP request.
A completed 401 must settle before one refresh can produce another separately
reserved attempt. Request body, model, route, prices and digest remain frozen;
uncertainty, settlement failure or a second rejection cannot trigger another
refresh. Redirects are refused, and recognized hard quota errors are permanent.

Subscription private continuation binds to fixed route, account and model,
surviving token rotation and restart for newly authorized requests. A different
account/model uses visible transcript reconstruction. Helpers receive no opaque
state. Public login/status/logout and device-flow onboarding use the separate
host boundary below; account catalogs and product-client adoption remain open.
This adapter is not a live-provider availability verification.

`config.Authority` retains an explicit directory, not a second configuration
copy. A snapshot reads bounded complete UTF-8 JSON and returns an exact-file-bytes
revision. Update compares that revision, applies a short side-effect-free patch
and atomically publishes/syncs the same file read by provider preparation. All
package writers share its lock; arbitrary external editors do not participate in
that compare-and-set contract. Anchored owned-file access rejects unsafe writers,
symlinks and nonregular files. Existing readable declarations remain valid;
new publications use mode 0600. A post-publication error requires rereading before
retry; an unchanged retry confirms directory durability without rewriting data.

The command also owns one `account.Service` borrowing its sole credential manager
and configuration authority. It closes and joins device work before closing the
manager. Runtime sessions and guest tools do not own account flows. The service
accepts one active login, retains at most 64 flow records for 15 minutes, and uses
process-epoch identities: an earlier process's ID reads as interrupted. Begin
returns accepted host work before device HTTP; a disconnect does not cancel it,
and repeated Begin recovers the active flow. Cancel, logout and shutdown join
starting requests, polling and credential publication.

The fixed `accounts.openai.begin/get/list/cancel/status/setup/logout` RPCs and
matching SDK methods return bounded safe approval/status projections. Device
secrets, tokens and captured credentials never enter responses or SQL. Approval
URL/code disappear on terminal flows; absent metadata and expiry are explicit
nulls. Status reads local saved-state and route evidence only: expired saved
credentials can still be `stored`, and `configured` says nothing about network
reachability, model access or pricing. Reads neither refresh credentials nor
fetch catalogs.

Credential publication syncs the file and its parent directory. First load also
confirms the directory entry for saved credentials or their absence before
authorizing that state; a failed confirmation can be retried locally. A replacement
that fails before publication leaves the prior login intact; a published but
unconfirmed replacement invalidates old captures and remains blocked until its
known local bytes are persisted. Rotated tokens use the same persistence-only
retry and are never exchanged again merely because a file write failed. Status
shows unresolved storage as unavailable without retrying a pending write or logout. Explicit Setup can
confirm pending local publication without network work; pending logout still
requires explicit Logout retry. A failed logout immediately revokes in-process
captures, reports unavailable storage and does not claim durable sign-out.

A completed login saves credentials before installing the fixed subscription
route. Setup never changes defaults or overwrites a conflicting custom route.
A route publication failure becomes `setup_required`; explicit Setup retries
with saved credentials without another login. Safe account error kinds distinguish
credential, setup, configuration and logout problems. Logout revokes local
captures and removes saved credentials while preserving host route declarations.
The SDK keeps no account cache, starts no browser and implements no polling loop;
product clients own the bounded visible observation lifetime.

Host output ceilings accept the provider catalog's range up to one billion
tokens, including the bundled 1,048,576-token ceiling for `kimi-k3-fast`.
Configuration, dispatch and recorded request snapshots preserve the same ceiling.
An internal model request may narrow its captured output ceiling with
`OutputTokenLimit`. Nil retains the host ceiling; explicit values must be
1–1000000 and cannot widen it. API encoders, the immutable request digest and the
attempt reservation use the same effective limit. An invalid host route remains
invalid even when a caller supplies a smaller bound. Subscription requests below
the natural ceiling fail before credential capture because that wire protocol
cannot enforce them. Equal or wider bounds retain the natural reservation and
omit a wire cap. The scripted fixture records the narrowed reservation but does
not simulate tokenization or truncate its deterministic acknowledgement.

The stateless-helper ledger records paired `operation_id` and `batch_index`
provenance on attempts with purpose `model_helper`. The logical call identity
derives from those values; provider retries get separate attempt rows. Admission
requires a dispatched `models.call` or `models.batch` operation in the same turn
and tree scope, strict bounded arguments, a valid item index and a prepared cap
within the admitted request. Helpers cannot publish assistant messages or private
continuation. The operation owns aggregate output; there is no item-response
table or second provider ledger.

Operation settlement waits for every linked attempt, and cell settlement waits
for operations. Recovery settles attempts before their dependent operations and
cells in one transaction. Undispatched reservations become known-zero
cancellations; dispatched work becomes uncertain without replay or invented
output. Operation IDs on attempts are immutable provenance rather than cascading
foreign keys: child deletion removes operations but preserves permanent ancestor
charges and their origin until root deletion.

Dispatch also rechecks current ancestor budget exposure after reservation,
counting that reservation once. A sibling settling above its reservation, or
revealing unbounded exposure, can prevent a later dispatch. Already dispatched
requests retain their actual output and charges even if they exceed the estimate.
The public attempt projection includes nullable `operation_id` and `batch_index`,
with a zero-based index for each helper item and no assistant-message association.
The common runner executes at most four items concurrently in a batch of 1–32
prompts. It uses the active turn's captured model, effort and sampling, one user
prompt per item, and no session instructions, transcript, tools, output contract,
compaction, private continuation or ordinary reply preview. Requested token caps
use the same provider preparation and reservation path as ordinary calls.

Guest `models.call` returns one item; `models.batch` returns the ordered array.
Each item has nullable `attempt_id`, `failure` and `content_ref`, a `text` value,
`truncated` and decimal-string `bytes`. Small text is inline. Output above 8 KiB,
or requiring too much JSON escaping, is retained as immutable owner-scoped content
with a bounded UTF-8 preview. Each encoded item is at most 15 KiB so even a full
batch fits the ordinary operation-result limit. Automatic evidence consumes
content capacity, not another user logical-write allowance. If publication fails
after billing, the item reports output unavailable and keeps the charge; no
provider execution is repeated to recover text.

Confirmed semantic admission refusals and settled provider failures stay in their
item positions. Cancellation stops new items and joins all started work.
Unresolved accounting failures remain typed fatal errors even when joined with
cancellation; guest exception handling cannot keep the cell running. The runtime
classifies only error chains whose every leaf is a known semantic refusal. A
joined rollback or other persistence failure must never become a normal refusal.

Provider attempts also have an idle deadline: two minutes for Chat and five
minutes for API Responses or subscription requests. Response headers and positive
body reads count as progress. Active streams may exceed that interval, subject
to their existing absolute attempt deadline. The adapter freezes any internal
`IdleTimeout` override at preparation, including across credential refresh; this
is not a host-config field. One owned watchdog cancels a stalled request and joins
before execution returns. A confirmed completed response wins a simultaneous
idle expiry. Interrupted I/O remains uncertain, preserves independently reported
usage/cost, and cannot trigger automatic replay. Provisional text remains a
bounded preview and never becomes a completed transcript on a stall.

## Verification and remaining scope

The replacement implements persistence, runtime scheduling, RPC, SDK/Go clients,
model attempts, authorized content and host effects, code execution/checkpoints,
recursive sessions, budgets, mail and shared state. Product integrations and
client adoption are still being ported. The delivery plan tracks phase acceptance;
this guide describes the implemented ownership and behavior.

Passing tests against real temporary SQLite cover constraints,
transaction rollback, concurrent claims across connections, pinned configuration,
root/child history, cancellation, restart and deletion. Contract checks
validate actual Go JSON in TypeScript, including counters above JavaScript's
safe-integer range. Import checks prevent new core packages reaching the retired
orchestration. The [development guide](backend-redesign-development.md#phase-1-behavior-ownership-and-evidence)
maps every acceptance criterion to its tests and records local/hosted results.

## Execution and transport ownership

`runtime.Open` locks a private explicit directory, opens fresh host configuration
and SQLite, and removes a dead owner's socket. It starts no work. `Start` performs
recovery under that execution lock and starts a bounded scheduler. One worker
owns one claimed session turn; SQL also enforces this invariant. Workers do not
hold the scheduler mutex across provider calls or database operations. Closing
the runtime cancels and joins workers, closes owned kernels, then releases storage
and its lock. Successful subtree deletion also closes its live kernels.

`runner` projects the durable transcript into a provider request using the turn's
captured configuration. It has injected provider/transcript interfaces and no
runtime or SQL imports. Completed output is persisted with a stable message ID;
retrying that write or turn settlement never dispatches the provider again.
These cleanup writes have a separate five-second bound. Exhausting settlement
retries faults the runtime, leaving recovery to interrupt the nonterminal turn.
Dispatched attempts and uncertain effects have durable evidence.

Context selection and recorded compaction keep provider requests bounded while
retaining raw history. Indivisible inputs or exchanges that exceed the bound fail
explicitly. The scripted provider is an injected adapter on the same runner and
engine path and accepts only the `scripted/scripted` selection.

`rpc` serves newline-framed JSON-RPC on the private Unix socket. Every connection
must initialize with major 4; reconnections verify the original runtime identity.
Frames are bounded at 8 MiB, connections at 64, idle reads at 30 seconds, writes at
5 seconds, and request handlers at 10 seconds. The server joins its connection
handlers on shutdown. Request contexts own admission/observation only, never the
lifetime of accepted work. Protocol errors are typed without exposing internal
database or provider error details.

The SDK and Go client initially observe by polling receipts and reading bounded
history pages. They keep no event log or duplicate transcript authority. Stable
request identities recover lost acknowledgements; ambiguous transport failures
do not imply rejection. Explicit input/turn cancellation is separate from local
wait cancellation. Bounded preview observation is described below; product-facing
synchronized views are part of the later client cutover.


## Code execution and checkpoint boundary

The runner advertises `execute` and repeats model → code → model through injected
interfaces, with at most 32 logical model calls and 64 dispatched cells per turn.
Each retry has its own attempt under the logical call. Completed assistant calls
commit before code admission. Model tool declarations describe available syntax;
they do not authorize host effects. The tool dispatcher separately admits and
authorizes supported host operations.

A cell names its turn, committed assistant message and provider call ID. An atomic
begin admits exactly one execution. The runtime serializes each session's kernel
and pins process capacity through cell settlement, releasing capacity before the
next provider call. Kernels are disposable runtime-owned resources. Loading a
session or updating its model does not recreate its REPL or start execution.

The engine stages a checkpoint after evaluation. Immutable bytes are published
first; the tool-result message, cell outcome and checkpoint reference then commit
in one SQL transaction. An unchanged image may reuse the preceding body at the
new cell boundary. A failed publication/commit cannot make the candidate image
restorable. Startup collection retains committed checkpoint bodies and removes
orphans. Engine protocol sequence numbers are not transcript or cell watermarks.

A correlated engine result can report a language error while retaining useful
partially changed globals. It is distinct from transport loss/cancellation, which
leaves the cell outcome uncertain. A completed cell can also lack a usable
checkpoint. Both facts remain visible: its result survives, while further code
execution fails explicitly instead of loading an older image. Text-only turns and
history inspection remain possible; a fresh session starts a fresh REPL. No repair
or replay is automatic. Conversation rewind explicitly invalidates an existing
REPL boundary by advancing the history revision, including when keeping every
message.

Restoration verifies the body digest/size and engine build, ABI, profile and
fidelity. Starlark checkpoints are partial; skipped globals and restoration
failures are recorded in tool results. QuickJS checkpoints preserve the whole
image within their resource contract. Restoration never evaluates past cells or
reissues host calls. Both engines preserve state across model changes, eviction
and restart through the same runtime and storage path.


## Host operations and permission decisions

`internal/tool` prepares bounded host requests and coordinates their durable
admission, consent and dispatch through consumer-owned interfaces. It owns no SQL,
interpreter or scheduler. The runtime binds each host invocation to its running
cell; the store derives the owning turn and session from that cell. The operation
keeps normalized immutable arguments. Its ID is stable for the cell/invocation
identity; a repeated identity cannot execute the effect again.

A standing grant matches one session, capability and resource exactly. Workspace
file capabilities use the session's fixed working directory as their resource;
`files.read`, `files.write` and `files.patch` are separate capabilities. Workspace
scope permits relative paths within that directory, not arbitrary host paths.
Creating standing authority is an explicit trusted-client operation. Templates,
model instructions, child relationships and an existing workspace confer none.
Resolving a permission as approved creates a grant bound to that exact operation,
including its normalized arguments. The next invocation needs its own decision.

An operation moves from waiting → ready → dispatched → succeeded/failed/uncertain.
Waiting/ready operations can instead become denied/cancelled. Consent, one-use
grant creation and readiness commit together. Filesystem locks and root handles
are acquired after consent, then SQL rechecks the live owner and unrevoked grant
at dispatch. Revocation committed before dispatch prevents the effect; revocation
after dispatch cannot claim an already admitted effect stopped. No SQL transaction
contains filesystem execution. A cell cannot settle with unfinished operations.

The filesystem adapter uses rooted handles, canonical cancellable mutation locks,
regular-file checks and bounded UTF-8 requests/results. Writes/patches publish with
an atomic same-directory rename and sync; each prepared request executes once.
Read pages default to 2,000 lines with a 256 KiB source cap and 32 KiB output cap.
A null `next_offset` means an incomplete line/source cannot resume by line number.
Mutation errors conservatively record uncertainty. Successful effects retain that
outcome even when cancellation arrives before result presentation. SQL settlement
may retry for five seconds without invoking the effect again. Dispatched calls
have a 30-second execution context; waiting for consent remains cancellable.
Prepared handlers receive the committed operation ID. Only `models.call/batch`
may instead use their bounded model-attempt deadlines, still under turn
cancellation. This internal lifetime boundary does not yet expose guest helpers.

An accepted operation whose persistence or accounting cannot be reconciled
returns a typed fatal host error. The process consumes that marker locally,
terminates the worker before replying, cancels and joins outstanding host calls,
and publishes no new checkpoint. Guest exceptions cannot swallow this failure
or continue to another effect. The operation retains unresolved dispatched
evidence for recovery; completed effects and charges are not rolled back or
replayed. Ordinary settled errors remain catchable. Pre-admission rejection
never invokes the handler, and cell settlement still refuses unresolved work.

QuickJS no longer applies a separate ten-minute whole-cell timer while waiting
for host operations. Guest compute/time, VM job/request and RSS limits remain,
along with turn cancellation and handler deadlines. Suspended host waiting does
not consume guest compute time, matching the Starlark lifetime boundary.

A permission is durable decision evidence, not a live waiter. Restart cancels
undispatched operations and pending permissions, marks dispatched unsettled
operations uncertain, then reconciles cells/turns in the same transaction.
Completed operation evidence remains unchanged even if its cell's checkpoint was
lost. Late approval of a cancelled permission conflicts. No effect is replayed to
recover an interpreter image. Grants and operation evidence are retained until
explicit owner deletion; current limits are 1,024 grants per session and 1,024
operations per turn. Lists use bounded 100-item/4 MiB pages.

The SDK exposes `grants.create/list/revoke`, `permissions.list/resolve`,
`operations.get`, `turns.operations`, `cells.get` and `turns.cells`. These views
query durable records; they do not introduce another authority cache.



## Saved permission policy

Fresh schema37/config14 stores one permission policy per tree: `prompt` (Ask) or
`automatic` (Full Access), positive revision and update time. Only roots may edit
it, including stopped roots. Children display the same tree policy; execution
requires either an exact live delegated grant chain or an ongoing policy-inheritance
relationship. A default child inherits the parent policy when `grant_ids` is
omitted/null, the parent inherits that policy, and working directories match
exactly. This relationship is captured in Ask mode as well as Full Access.
Explicit subsets (including `[]`) never inherit policy authority. Every ancestor
hop and workspace is checked. Later mode changes propagate to existing default
children, including Ask → Full Access and off/on transitions. The recorded child
revision is an admission observation, not an expiration of this relationship.
Automatic mode never bypasses input validation, fixed workspace scopes, root-only
human questions, or explicit host consent for computer/MCP effects.

An eligible root or child without a matching grant may admit an operation under
its current `automatic` policy. The operation captures that policy revision;
dispatch checks the policy and every delegation hop again before the effect. An
actual mode change advances the revision, closes pending approvals, and denies
ready operations admitted through the old policy throughout the tree. Dispatched
effects keep their ordinary settlement path. Explicit grants
and intrinsic human questions retain their independent authority/lifetimes.

Mode edits have caller-chosen stable IDs and exact payload digests. SQL resolves
identical retries before current state or CAS, returning the original immutable
receipt even after later edits or tree deletion. Reusing an ID with different
payload conflicts. Same-value edits record a receipt at the existing revision
and do not retire operations or language servers. Only a newly committed actual
change invalidates the runtime’s language-server pool; replaying a historical
change cannot stop a newly created server.

New roots and forks capture the current host default through the shared config
authority; omission defaults to `prompt`. Explicit creation mode overrides that
default, and editing the default never changes existing trees. Invalid values
fail validation. Exact fork retries resolve before reading mutable host defaults.
The host default is edited through whole-file revision CAS and returns only the
safe selected mode and revision hash.

RPC/SDK expose policy reads, mode edit/receipt reads and host-default read/edit.
Callers retain the exact mode edit identity/payload before sending and explicitly
recover lost acknowledgements. The SDK does not automatically replay host CAS or
maintain a separate policy cache. Product UI adoption remains a Phase6 obligation.

## Provisional output and observation

The provider owns response assembly; the runner owns dispatch and commit. A
synchronous chunk callback publishes text, reasoning and incomplete tool arguments into a
runtime-owned preview for the active attempt. The runtime retains at most 64
previews of 128 KiB each, shared across text, reasoning and call fields. It uses
UTF-8-safe truncation and exposes truncation
explicitly. It retains no completed preview cache or replay log. Preview callbacks
cannot start effects, veto execution or inherit an observing client's lifetime.

Chat `reasoning_content` and Responses/subscription reasoning-summary deltas
populate `preview.reasoning`, independently from answer text. Reasoning-only
chunks advance preview revision. These fragments are never transcript parts,
assistant output or later provider context. Opaque private continuation remains
separate and is not exposed by this field. The adapter's raw-response and event
bounds still apply; discarded reasoning does not consume the final-message byte
allowance. Completed messages replace the entire preview, including reasoning.

OpenAI-compatible requests ask for SSE and usage. The adapter also accepts bounded
JSON responses. Streaming requires both a valid completion reason and the final
transport marker; malformed, truncated or cancelled streams return no executable
parts and are recorded as uncertain. Known usage/cost received before failure
remains evidence. Each retry has its own attempt and preview identity. The prepared
wire body, route and price snapshot remain frozen across retries.

`sessions.observe` returns bounded committed history after a decimal cursor, a
nullable preview and a fresh process epoch. A preview identifies its turn,
attempt and eventual committed message, with its own revision. It is not a
message and may contain incomplete JSON arguments. The runtime snapshots it before
reading attempt/history state, suppressing it when its committed message is
visible. SQL settlement retries keep the preview alive without redispatching.
Success replaces it by the committed message ID; failure discards it without
inventing an assistant message. Restart changes the epoch and drops all previews
while the ordinary durable attempt/turn recovery records uncertainty.

The SDK's `observe` async iterator drains history pages, then polls live state at
100 ms. It retains only the exact history cursor and last preview revision. A
slow or disconnected observer cannot block the provider or cancel accepted work.
Consumers upsert completed messages by ID, replace matching previews, and clear
a preview on null or epoch change. Polling reads current state, so losing a
notification cannot leave a durable history gap. Aborting observation stops only
the observer; execution cancellation remains an explicit operation.


## Child admission, delegation and waits

`sessions.spawn` requires a request identity, parent, initial text/content parts,
configuration overrides and `grant_ids`. The result contains the child session
and its ordinary input admission. Child identity, resolved configuration,
parent-scoped content references copied into new child references, delegated
grants, captured permission-policy delegation, initial input and receipt commit
together. No duplicate body bytes or
child-specific transcript path exists. `grant_ids: null` inherits currently valid
standing grants plus ongoing same-workspace permission-policy inheritance; an explicit
subset delegates only those grants and `[]` delegates none. Retries preserve this
original request and
return the same child/input even if parent defaults or grants have since changed.
A deleted receipt returns `session: null`; it cannot resurrect the child.

A child standing grant names an `issuer_id` belonging to its direct parent with
exactly the same capability/resource. One-use approvals cannot be delegated.
Dispatch validates every issuer hop and revocation. Revoking an ancestor grant
also denies ready descendant operations; it cannot undo effects already
dispatched. A child lacking delegated authority records a denial, not a prompt
that could widen its scope. The trusted client can explicitly delegate another
standing parent grant later. Stopping only a parent does not revoke its grants.

`agents.spawn(prompt=...)` normalizes the executing parent into immutable
operation arguments. Its tree-scoped authorization, child admission and operation
success commit in one transaction. The tool dispatcher distinguishes these
SQL-only coordination operations from externally dispatched effects. A failed
SQL commit cannot leave a child without its input or claim an operation succeeded.

`agents.wait_after_cell(input_ids=[...])` registers a wait and returns immediately.
Finish the cell; after its result/checkpoint commit and kernel lease release,
the runtime waits before the next model step. This replaces same-cell blocking
joins in the new runtime. Only descendant inputs can be targeted (never self,
ancestors, siblings or another tree), preventing wait cycles. Failed, interrupted
or cancelled child work resolves the wait as a durable outcome; it does not
implicitly fail or retry the parent. Deleting a target makes it unavailable and
fails that wait. A cell may register at most 128 distinct input targets. The
successful operation rows are the durable registrations; there is no duplicate
wait registry/table. Restart interrupts the parent turn rather than inventing a
suspended interpreter continuation; queued child work survives independently.

`agents.submit(session_id, parts)` admits another input to a direct child, sharing
only content references already authorized to the caller. Input, receipt and
operation success commit together. `agents.inspect(session_id, input_id, offset)`
reads that exact descendant input's outcome, never another turn's answer. Text is
paged at UTF-8 boundaries with a 16 KiB maximum, decimal-string offsets and total
bytes, message identity, truncation and omitted-part metadata. When inspecting an
unfinished turn, check the returned message identity before combining pages;
terminal output is stable. `agents.list(relation, after, limit)` returns bounded
parent, child or sibling identity/lifecycle metadata, without their configuration
or transcript. Each operation requires its own tree-scoped capability.

`agents.stop(session_id)` stops a proper descendant subtree, retaining queued
input and requesting cancellation of active turns in the same transaction. Only
after commit does the runtime cancel their workers; replay only services turns
already marked cancelling, so it cannot stop subsequently restarted work.
`agents.delete(session_id)` shares the ordinary subtree deletion transaction and
rejects active or cancelling work. Successful operation retries return their
original result even after the target disappears. Kernel cleanup follows commit.

The runtime separately owns active turn cancellation handles and physical worker
slots. SQL `turn_permits` authorize execution under every ancestor's
`runnable_descendants` limit. A running turn can wait without a permit; outcome and
execution permission have different lifetimes. Claim acquires permission before
consuming queued input. A boundary wait releases it only after all attempts, cells
and host operations settle, then frees the worker slot. Resumption reacquires
both before any new model or code execution. New attempts, cells and host-operation
dispatch require that permission. Finish and restart recovery release it atomically.

Resumptions alternate with fresh work. A blocked resumption does not hide later
waiters, and a bounded cursor pages past blocked queued sessions and wraps back to
earlier work. Physical slots are reserved before SQL acquisition without holding
the scheduler mutex; cancellation after acquisition releases unused permission.
`MaxActiveTurns` defaults to 1,024 and is limited to `Workers..1024`; at most
`MaxActiveTurns - Workers` turns may wait, preserving admission capacity for
progress. Limit exhaustion is explicit. One worker and one kernel slot can
execute a recursive chain because no waiting parent holds either resource.


## Model budgets and retained accounting

`budgets.list(session_id)` projects four local scopes: `model_calls`,
`model_tokens` (input plus output totals), `model_cost_nano_usd`, and
`model_elapsed_millis`. Each scope has a nullable limit and a compare-and-set
revision. An absent limit is unlimited locally; every ancestor's live cap still
applies. Null removes only the local cap, with the same inheritance semantics as
resource limits. `budgets.set` requires the current revision (zero for an unset scope).
Child admission may include explicit narrower `budgets`; limits are not copied
into each descendant. A child cannot explicitly widen a finite ancestor cap.
Changing a parent cap immediately constrains subsequent descendant reservations.

Reservation checks and attempt insertion commit together across every ancestor.
The request snapshot owns the input bound, output maximum, prices and timeout;
there is no separate mutable copy of these bounds or of consumed usage. Each
attempt captures immutable ancestor associations for accounting. Concurrent
siblings therefore cannot reserve the same remaining allowance. Frozen requests
are admitted or rejected as a whole; reservation never silently clamps their
output tokens after request encoding/digest calculation.

The projection distinguishes known `used`, unfinished `reserved`, and quantified
unresolved `uncertain` exposure. `incomplete` means some exposure cannot be bounded
from available evidence or represented in an int64 aggregate; finite limits
reject that uncertainty. Unknown usage/cost remains distinct from known zero.
Confirmed undispatched cancellation releases its reservation. Dispatched requests
count as calls even if their response is lost. Restart preserves unresolved
exposure, and retries are distinct actual attempts. Known overages remain visible
and prevent further admission. Setting a finite cap below already allocated
used/reserved/uncertain amounts is rejected. Overflow never wraps counters; exact
individual evidence remains in the ledger and a saturated aggregate is incomplete.

Attempt records outlive deletion of their child session/transcript, retaining
original turn/message identities as historical references. Captured ancestor
associations keep their usage charged to surviving ancestors. Deleting a whole
tree explicitly removes its accounting; deleting and recreating a child cannot
replenish allowance. Usage is stored once in the attempt ledger and budget views
are derived. Ancestors without finite limits skip historical aggregation during
reservation, while retaining associations for later inspection or limit changes.

For HTTP models, optional host `context_window_tokens` declares the provider's
maximum context, used as a conservative input bound rather than an estimated
request token count. It must be positive, at most one billion, and at least the
configured output maximum. Missing bounds/prices prevent finite token/cost
reservation when required; unlimited scopes can still execute with explicit
unknown evidence. This is a trusted host declaration, not independent proof of
a provider limit. Actual usage, charges and timeout overruns are never clamped
to it. Elapsed usage is monotonic execution time rounded up to milliseconds;
SQL settlement retries do not add execution usage. Provider timeouts are
cooperative, so an actual duration may exceed its reserved timeout.

## Reusable resource capacity

Resource limits belong to sessions, not the tree metadata or model accounting.
`resources.list(session_id)` returns each kind for the requested session and every
ancestor, nearest first. Each entry identifies its scope, revision, nullable local
limit, and current usage. A child's absent row has revision zero and no local cap;
ancestor caps still apply. The response is bounded by the absolute 128-edge ancestry
limit and the six supported kinds. It is a single database snapshot, not a cache.

| Kind | Usage within the owner's subtree | Capacity becomes available when |
| --- | --- | --- |
| `depth` | Greatest retained descendant edge distance; owner is depth zero | Deepest descendants are deleted |
| `descendants` | Retained descendant identities, excluding the owner | Descendants are deleted |
| `queued_inputs` | Unclaimed, uncancelled inputs, including the owner's | Inputs are claimed, cancelled while queued, or deleted |
| `active_operations` | Unsettled host operations, including permission waiters | Operations reach any terminal outcome |
| `subscriptions` | Active shared-state subscriptions | Subscriptions are cancelled or their owner is deleted |
| `runnable_descendants` | Execution permits held by proper descendants; excludes the owner | A settled parent wait yields, or a turn finishes/is recovered |

All applicable ancestor scopes are checked inside the same immediate transaction
that admits work. Siblings share their parent's allowance. In particular, queue
capacity is now aggregated across the subtree, replacing the old independent
per-session queue cap. Child creation acquires its identity and initial input
capacity atomically; denial leaves neither a child nor a receipt. Exact accepted
retries return their receipt before new capacity checks, including deletion markers.
No counters need repair after a crash. Stop, worker eviction, turn completion, and
model changes do not release retained session capacity. Claiming a mail-only turn
does not change queued-input usage. Recovery settles operations but retains
unclaimed inputs. Cancelling a subscription retains its historical row and committed
notification evidence; its separate physical retention bound still applies.

`resources.set` requires the scope's current revision. It rejects a limit below
current usage; stale revisions conflict. Removing a child's local cap with null
inherits ancestor enforcement. Explicit child limits cannot exceed ancestor caps;
for depth and descendants, subtract the ancestor-to-child distance because those
identities already consume ancestor capacity. A child depth cap of zero permits
that child but no descendants. Tightening a parent immediately constrains later
admission even if a child's older local cap is larger. Inspection returns all
scopes so callers can see which allowance is exhausted. A local allowance is not
reserved exclusively for that child.

Optional finite `resources` defaults and
creation overrides resolve once into root rows at revision one: depth 8,
127 descendants, 256 queued inputs, 64 active operations, 1,000 subscriptions,
and 64 runnable descendants.
Root limits cannot be null. Partial creation/configuration lists inherit the
remaining built-in defaults; duplicate kinds are rejected. Limits use decimal
strings, including host JSON and guest child-spawn arguments. Host edits never
change existing root limits. `TreePolicy` and its duplicate JSON column no longer
exist. The fresh database rejects previous schemas.
The absolute depth ceiling remains 128 for bounded hierarchy and grant traversal.

Capacity reuse never replenishes permanent model spend. Deleting a child releases
its retained resources while its immutable model attempts remain charged to live
ancestors. Per-value, retained-history and byte safety bounds remain at their
owning boundaries. Cumulative write allowances below have their own permanent
accounting and never count physical database rows or current disk usage.

## Cumulative logical-write allowances

`budgets.list/set` also exposes `logical_writes` and `logical_write_bytes`. Fresh
roots start with finite limits of 100,000 writes and 1 GiB at revision one; model
budgets remain unset by default. Every author and ancestor is charged. Children
may narrow a limit or clear it to inherit, but roots cannot clear these two caps.
Policy edits use the same compare-and-set and checked-exposure rules as model
budgets. Write budgets have no reserved or uncertain amount: action and charge
either commit together or both roll back.

| Explicit logical action | Charged owner | Bytes charged |
| --- | --- | --- |
| Initial child input and later child submission | Calling parent | UTF-8 text bytes; shared content aliases add no byte charge |
| Mail send or explicit replacement | Sender | Subject plus body bytes |
| Content registration | Reference owner | Body size, even when physical bytes deduplicate |
| State write or append | Author | Submitted JSON bytes; append charges only its submitted suffix |
| State subscription creation | Subscriber | Key bytes |
| Schedule creation | Owning session | Canonical expression plus input template text bytes |
| Scheduled occurrence admission | Owning session | Ordinary input text bytes |

Each accepted action consumes one logical write. Initial child input now follows
the same charging rule as later submission, closing a gap in the former spawn
path. Ordinary human input remains exempt. Automatic notifications, content
sharing aliases, transcript/checkpoint persistence, model/operation/turn settlement,
mail observation/acknowledgement/defer, cancellation/deletion, policy edits and
recovery do not consume this allowance. Schedule creation and each accepted
occurrence use this same transaction boundary.

`logical_writes` retains immutable source identity/revision, author, byte quantity
and tree ownership; `logical_write_ancestors` captures charge ancestry even where
no local cap exists. Totals derive from this evidence. There is no mutable counter
or reserve/reconcile lifecycle. Deleting a child does not refund surviving
ancestors. Deleting the whole tree removes its ledger. Exact accepted retries
return existing evidence before a new cap check, while a conflicting identity
remains a conflict. Overflow is visible and blocks further charged writes.

The runtime derives state `SubmittedBytes` before merging an append; guest and
client request types cannot choose that accounting amount. Content/state body
publication precedes the SQL transaction, so rejection can leave an unreferenced
immutable file for startup collection. These limits bound committed logical
writes, not pre-publication disk allocation. Physical retention limits still
apply at their owning boundaries. Exhaustion never prevents recording a completed
model or external effect, and does not prevent a human from submitting new input.

## Mail and presentation

Mail is retained inter-session communication, separate from accepted input.
A caller supplies a stable mail ID; its original typed payload digest makes an
identical send retry idempotent. A deleted recipient leaves a mail tombstone,
so retrying an uncertain send cannot resurrect deleted work. Sender deletion
leaves surviving recipients' mail intact. For session-authored mail, senders and
recipients must be in the same tree and be self, parent, child or siblings.
Mail metadata has an explicit source: `session` plus sender ID, `state` plus
subscription ID, or `completion` plus child ID. Clients and agents cannot forge runtime notification provenance
through the send API. Runtime-authored notifications use their own scoped source
identity and can reach any subscriber in the same tree.

A mail identity owns its current revision and handling state. Immutable revisions
own delivery class, availability, subject, body and an optional recipient-owned
`evidence_ref`. Transcript entries reference
the exact presented revision, so replacing pending mail cannot rewrite history.
A listing contains metadata and body size; an explicit read returns the body.
Human list/read operations do not start work, establish agent observations or
acknowledge delivery.

Authored mail may supply one sender-owned content reference as `evidence_ref`.
At admission, the store checks that ownership and creates a recipient-owned
reference to the same immutable body, in the same transaction as the mail,
revision, logical-write charge and guest-operation settlement. Self-mail reuses
the existing owned reference. Neither a digest nor a foreign reference grants
access. The body may be empty only when evidence is present. Sharing consumes
recipient retained-content capacity but does not upload or charge the body again.

Metadata, reads and transcript digests identify the recipient's reference. Model
context contains only its bounded identifier, never automatically hydrated bytes;
the agent uses authorized `artifacts.read` to read them. The send identity hashes
the original caller payload, so an exact retry returns the existing admission
without creating another alias, including after sender deletion and restart.
Admission metadata reflects the mail's current revision if it has since changed.
Replacing pending mail may attach
new evidence, while older revisions keep their exact references; deferral carries
the same reference without sharing or charging it again. Deleting a sender does
not remove recipient references. Recipient deletion releases its mail and owned
references together.

The scheduler derives readiness directly from due mail and queued inputs.
`queued` mail waits for an idle session, `steer` can also be presented at the next
model boundary after all preceding code calls settle, and `next_turn` waits for
another reason to start a turn. All due classes can be presented when a turn
starts. A mail-only turn has no input; execution, accounting, cancellation,
history and recovery use the ordinary turn path.

Turn observations record exact mail revisions. Listing through an agent helper
allows explicit handling but does not count as model presentation. Automatic
digests and explicit agent reads do. A successful turn marks only its presented,
still-current pending revisions delivered. Failed, cancelled and interrupted
turns leave mail pending. An unsuccessful latest turn blocks automatic mail-only
execution until an explicit input succeeds; new mail and a runtime restart do
not bypass that barrier. Delivery does not mean the agent has finished handling
the mail: `mail.complete` explicitly marks observed revisions done, while
`mail.defer` creates a new pending revision for a later availability time.

Guest helpers are scoped host operations with persisted authority and results.
Their observation/handling mutation and operation settlement share one SQL
transaction. They derive the sender/recipient from the executing session.
Client operations expose send and inspection; clients cannot impersonate a
successful agent presentation by acknowledging mail through an inspection API.

Mail bodies are limited to 16 KiB and subjects to 256 bytes. A digest presents at
most 20 items, each with at most 2 KiB and 20 body lines. Per recipient, storage
allows 1024 retained mail identities, 256 pending items and 20 unfinished items
from one source. Each source can create 30 identities in ten seconds. A mail ID
has at most 128 revisions and a turn at most 1024 observed revisions. Exhaustion
returns an explicit limit error; no body, revision or observation is silently
dropped. Completion publication leaves a pending outcome when these limits block
delivery, as described below.

## Explicit state and immutable values

Session state is private to its session; tree state is shared by members of that
one tree. Both use the same `state_versions` table. Each immutable row identifies
its owner, key, revision, author and content digest. The latest revision is the
head, derived from those rows; no mutable head cache or second current-value table
exists. The content store owns immutable JSON bytes. VM globals, conversation
history, ordinary content references and explicit state remain separate domains.

Every write compares an explicit expected revision: zero creates a key, a
positive value replaces that exact head. Appends concatenate two JSON strings or
two arrays against the same comparison. A failed comparison leaves metadata and
history unchanged. A caller retains a globally unique version ID and the exact
payload to recover a lost acknowledgement. Retrying an old successful write
returns its original version even when newer versions exist. Deleted private
owners cannot retry writes or read handles. State versions have no retry tombstone:
owner deletion returns not-found, and session IDs are never recreated by a write.

A version ID is an authorized handle. The digest alone grants no read authority,
and a state handle does not become an ordinary conversation-content reference.
Private versions are collected when their session is deleted. Shared versions
and history survive their author's deletion and are collected with their tree.
Startup collection under exclusive runtime ownership removes orphan publications;
normal execution never races file publication against collection.

State supports valid UTF-8 JSON to 64 MiB, nesting to 100 containers, and no
unpaired Unicode escapes. Numeric lexemes are retained without conversion to
float64. JSON storage accepts arbitrary numeric lexemes; a language adapter may
reject a number it cannot represent. Each tree retains at most 1024 versions and
1 GiB of logical version bytes, including private and shared history. Content
file deduplication does not reduce that logical budget. Limits reject writes;
no old version is silently evicted. These retained-data bounds are separate from
ancestor logical-write allowances.

Guest `state.get` returns version metadata plus inline JSON only up to 64 KiB.
Larger values return metadata without an inline `value`; a stored JSON null is
still an explicit value. `state.read` returns at most 64 KiB of base64 bytes from
an authorized immutable version. Ranges can split UTF-8 characters and are not
partial JSON values. The file is hashed in the same read that captures the range,
with bounded memory; corruption anywhere in the file prevents returning bytes.
List and history queries page metadata only. Client reads always return encoded
bytes, preserving exact numbers across JavaScript transports. Each client write
or append carries at most 4 MiB of JSON, and a value can grow to 64 MiB through
revision-checked appends without creating an oversized protocol frame.

Guest state helpers use the ordinary scoped operation ledger. The runtime
publishes validated immutable bytes before SQL. The transaction rechecks
permission, compares the expected revision, inserts the version and settles the
operation together. The ledger retains metadata and digests, never a second copy
of the state body. Read operations retain the exact observed version and hydrate
its body after the transaction. Unreferenced files from rejected or interrupted
writes are collectible. Shared state writes notify only explicit subscribers through ordinary mail;
private state writes never create notifications.


## State subscriptions

A subscription belongs to one session and one shared key. Creation supplies a
stable ID and an `after` revision. The transaction reads the current head and
notifies the subscriber if it has advanced, closing the gap between a client's
snapshot and subscription. A future cursor conflicts. There is one active
subscription per session/key, and at most 1000 retained subscriptions per tree.
Creation retries return the same subscription; retrying a cancelled subscription
cannot reactivate it. List queries are bounded and owner-scoped.

A shared write inserts its version, advances each subscription cursor and
creates or replaces its pending mail notification in one transaction. The cursor
means enqueued or authored revision, not successful agent processing. Own writes
advance the cursor without notifying that same session. A pending notification
is coalesced by adding an immutable mail revision; the state version's ID, key,
revision and author form its small JSON body. The value itself remains in the
state content store. A recipient's deferred availability time survives coalescing.
Presented revisions remain immutable, so a successful turn that saw an old
revision cannot acknowledge its newer replacement.

Notification admission obeys ordinary mail capacity limits, including the
128-revision limit on a pending identity. If any recipient lacks capacity, the
entire write, every cursor update and all notifications roll back. This is
explicit backpressure, not silent notification loss. After a notification is
delivered, a later change creates a new mail identity. Stopped recipients retain
pending mail; normal lifecycle and failure barriers govern when it can run.

Cancellation stops future notifications and preserves already committed mail as
handling evidence. Deleting a subscriber deletes its subscriptions and applies
ordinary recipient-mail deletion. Restart needs no callback or in-memory watcher:
SQL retains subscriptions and mail, and scheduler reconciliation finds due work.
Guest subscription admission/cancellation and operation outcomes share the same
transaction as their generated notifications. No separate subscription runner
or wakeup queue exists.


## Final output contracts

`Configuration.OutputSchema` is an optional JSON Schema captured by the turn's
configuration revision. Null or an omitted schema means no output contract.
The runner includes a configured schema in the model instructions, permits normal
tool work before the first final response, and validates the complete text-only
final response as one JSON value. One matching Markdown JSON fence is accepted.
Invalid raw replies remain ordinary durable assistant messages.

The first mismatch permits exactly one corrective model response through the
ordinary attempt ledger and budget admission. The correction notice is bounded
provisional request context, not another authored transcript entry. A second
mismatch or any corrective tool call fails the turn with `output_invalid`;
corrective tool calls never execute. Cancellation and uncertain provider outcomes
keep their ordinary semantics. This is not a replay of the turn or prior effects.

`turns.output` is a read projection of the last assistant message of a successful
turn and that turn's captured schema. It stores no second mutable output value.
Running turns report busy; failed, cancelled or interrupted turns and successful
turns without a contract return a null output record. A successful JSON `null`
value has a non-null record. The wire record contains the turn/message identities
and bounded `data_base64` JSON bytes, preserving numeric lexemes across JavaScript
clients. Later configuration edits do not change an earlier turn's output.

Schema admission and output validation use the same precise-number validator.
JSON numbers remain `json.Number`; constraints and values are compared without
conversion to binary floating point. Before exact arithmetic, each number is
limited to 4096 literal bytes and an absolute exponent of 4096. Larger values
fail explicitly rather than rounding or allocating unbounded integers. Schema
count constraints must also fit signed 64-bit integers, checked at schema
positions rather than ordinary instance fields. The default draft is 2020-12.
Schema documents must be self-contained: external HTTP, filesystem and custom-URL
loading is disabled, while references within the supplied document are allowed.
The pinned validator dependency is separate from the wire-schema generator.

## Child completion reports

`Configuration.ReportMode` is a value copied through the same parent, pinned
agent definition and explicit override precedence as other configuration fields.
Omission resolves to `notice`; each turn uses its captured configuration revision.
`notice` includes a 160-byte preview, `inline` includes up to 4 KiB, and `message`
suppresses successful automatic reports so a child can report explicitly. Failed,
cancelled and interrupted outcomes still report. Root turns have no report recipient.
Ordinary inbox digests remain bounded to 2 KiB per item; full evidence is explicitly
readable rather than silently admitted to the parent's model context.

Each child admission reserves one `completion_slots` row owned by its parent.
There are at most 128 slots per parent, including pending slots whose child has
been deleted. Terminal settlement and restart recovery capture the exact turn,
input, last assistant message identity, outcome, failure, policy and bounded text
in that transaction. They perform no filesystem operation or mail/content admission.
A later reported outcome replaces that child's pending snapshot; suppressed
message-mode success leaves an older pending failure intact. Settling a terminal
turn again cannot recreate a cleared report.

A bounded runtime publisher visits four metadata candidates per scheduler pass,
including when all turn workers are occupied. It advances past blocked children
and wraps its disposable cursor. Immutable JSON content is published to disk first;
parent-owned content registration, canonical queued mail and exact-slot clearing
then commit together. Completion mail has source `{kind: "completion", id: childID}`.
Pending mail from the same child coalesces by revision and preserves recipient
deferral. Published revisions and evidence remain immutable. Delivery failure due
to retention limits leaves inspectable pending evidence and never reruns the child.
As with other staged content, files left by rejected publication are collected on
runtime open. Automatic reports consume no logical-write allowance.

Host `completions.list/read` and guest `agents.pending_reports/read_report` inspect
pending snapshots; each read pins a child and exact turn token. Superseded or
published tokens return conflict so readers re-list. Evidence reads page JSON bytes
at up to 64 KiB; metadata lists exclude full text. Published mail uses the same
revision-owned `MailMetadata.evidence_ref` as authored attachments, pointing to
the existing parent-owned content without creating another alias. Its JSON body
contains outcome metadata and bounded previews; the digest retains the attachment
reference independently of body truncation. Read it via host `content.read` or
scoped guest `artifacts.read`. Neither route acknowledges mail. Deleting the child retains
pending and published parent evidence; deleting the parent removes its slots and
owned content references. A full retained mailbox or content allowance can leave
delivery pending indefinitely; no history is silently evicted to make room.

Failed or uncertain turns require explicit new input. Confirmed retryable model
attempts may retry within the active turn; retrying database settlement never
redispatches the model or host effect. Restart interrupts active work and does not
replay an entire turn. The failed-parent mail retry barrier also applies to
completion mail.


## Raw history and selected model context

Messages retain immutable bodies, history groups and opening-input markers.
Native groups identify their prompt turn. Imported groups may have no local
turn/input/attempt and instead carry immutable source provenance. These source
identities grant no access. A session's history revision starts at one; turns
capture it alongside configuration. Current-history queries exclude retired
messages, while exact evidence reads retain them. Sequences never repeat.

`context.snapshot` captures revision, active greatest sequence and active message
count in one read. Revision-aware full history pages read that boundary and their
bounded messages in the same SQL snapshot. Expected-revision checks reject a
cursor after rewind; later appends retain the revision and advance the tail.

Rewind requires a stopped owner with no active turn or uncancelled queued input.
The caller supplies a stable edit identity, expected revision, observed tail and
whole terminal group boundary (or zero). One transaction records an immutable
edit, advances revision, retires the suffix and clears incompatible summary
selection. Exact identity/payload retries return their original edit before
current lifecycle/CAS checks; a changed payload conflicts. An edit does not
rewind mail, goals, explicit state, spending or external files and effects.

Every new edit invalidates the REPL even when keeping all or no messages.
Revision-qualified kernel/checkpoint loading prevents restoration after a crash
between SQL commit and cache disposal. A delayed acknowledgement cannot dispose
a new-revision kernel. Old exact cell evidence remains readable. No code is
replayed to reconstruct state.

`sessions.fork` imports a selected terminal whole-group prefix into a fresh tree
and root. The initial command compares expected history and configuration
revisions plus the complete observed active tail. Later source work may run if
all included groups are terminal and the snapshot still matches. An immutable
fork identity and request digest are resolved before current source/default
validation; changed payload reuse conflicts. The receipt survives either tree's
deletion. Retry returns the original receipt with current tree/root projections,
or an explicit deleted result with null projections; it never recreates a tree.

A fork copies the source's effective configuration, pinned definition, engine and
working directory, without creating a Git worktree. It copies raw message parts,
new local group/message identities, opening-input markers and immutable immediate
source provenance. Imported messages have no fabricated local input, turn or
attempt. Selected compatible summary chains and pins get new local identities
and source provenance without invented billing. The runner groups and pins by
history group/opening input, including when execution links are null. Scoped
private continuations remain subject to the adapter's route/credential/model HMAC
and visible-part checks; copying does not authorize their replay under a new scope.

Every authorized owner content handle is copied unchanged, including handles in
opaque text; immutable body bytes remain shared. No children, grants, permission
decisions, schedules, independent mail, explicit state, operations, checkpoints,
spending or armed goals are copied. Presented mail retains only its history parts.
The destination starts with an empty REPL and fresh root resource/write limits.
Like fresh-tree creation, bounded initial import adds no logical-write charges;
future writes consume the destination's fresh allowance. Source charges are never
transferred or refunded. Title is explicitly supplied or null, and archive/pin
metadata starts false.

Import bounds are 10,000 messages, 1,000 groups, 64 MiB of serialized history,
4 MiB of private envelopes, 128 summaries, and 1,024 content references totaling
64 MiB including repeated references. Import hydrates one message at a time and
commits all metadata, references and the receipt together. Source deletion and
startup orphan collection cannot remove bodies still owned by a surviving fork.
Workspace snapshot/restore remains separate from conversation editing.

`context.list` and `context.search` expose their history revision and may require
the snapshot revision, so paging cannot silently mix current histories. Their
fixed through-sequence boundary keeps later appends out of the same scan. Metadata pages contain at most 100 records. Literal,
case-sensitive search scans at most 100 messages or 4 MiB of serialized parts
per call and returns at most one match per message. Follow `next_after` even when
there are no matches. Search does not load referenced content bodies.
`context.read` returns at most 64 KiB of exact serialized-parts bytes. Mail parts
are rendered from the immutable revision originally presented. These reads do
not present or acknowledge mail. The trusted client names the owner; guest
`context.inspect/read/search` always uses the actual executing session, with
ordinary scoped operation and grant checks.

Manual `sessions.compact` admits a `compact` input with empty parts. Receipt,
queue capacity, claim, captured configuration, cancellation and turn settlement
are the ordinary mechanisms. Maintenance does not execute cells, validate final
output contracts, acknowledge mail, or create child completion reports. Its
success cannot clear a failed prompt's mail retry barrier or erase an existing
pending child report. A compact input with no eligible older history succeeds
without calling a model.

Compactions are immutable derived text, bounded to 64 KiB. Each records its
attempt, base summary, expected context revision, exact covered raw sequence and
pinned message IDs. One context head selects a summary. The runner quotes that
summary as untrusted conversation data, adds pinned raw messages and the raw tail,
and keeps the resulting request in execution memory. It never rewrites transcript
rows or promotes a summary to system instructions. Helper responses have purpose
`compaction`, ordinary attempt accounting and no assistant message or reply preview.
Attempt settlement, usable summary evidence and conditional head selection commit
in one transaction. Stale selection or cancellation retains completed evidence
without selecting it. Repeating settlement after undo cannot select it again.

`context.select` permits idle-session undo to an ancestor summary or no summary,
with the expected head revision. It does not delete summary evidence, restore
REPL checkpoints, change files or admit execution. Reads expose summaries through
`context.compactions` metadata pages and `context.compaction` text.

The current policy retains four recent message-bearing turns for manual
compaction. Before an ordinary model request would exceed 100 messages or 4 MiB,
automatic compaction progressively retains four through one recent whole turns.
Both paths fold older history in bounded batches without splitting assistant calls
from their tool results, require a shorter replacement and exact forward coverage,
and allow at most 16 folds per turn. If those recent turns still exceed the local bound, the runner folds within
the latest turn while retaining its newest assistant message with all tool results
and later mail. Manual compaction uses the same fallback when its recent history
is still oversized. A partial prompt turn pins its exact opening input-backed
message; mail with user role cannot substitute for that pin. Mail-only turns
invent no input. Every intermediate fold has the same transactional pin guard,
including cuts inside older turns. Once an entire turn is covered its pin can be
dropped. Pin restoration uses at most 32 indexed message lookups, sorted by raw
sequence, and fails rather than returning an incomplete or oversized set.
An indivisible exchange, opening input or summary that cannot fit still fails.

A complete bounded non-streaming HTTP 400/413 response with a recognized
structured context-limit code permits at most one replan per ordinary turn.
Its rejected attempt must settle before compaction makes forward coverage
progress and a fresh model round begins. Generic errors, provider message text,
partial streams, transport uncertainty and settlement failures do not authorize
replay. The helper cannot recursively replan itself. Output-correction state
survives reconstruction, and completed cells are never repeated.

`configuration.compaction` captures one whole policy: an optional helper model
and a threshold of 1–100 percent. A patch with threshold zero resolves to the
default of 50 percent; a null model uses the captured conversation model. An
explicit helper route is used by manual, local-bound, proactive and reactive
folds, and an invalid route fails without fallback. Configuration updates affect
the next turn; children copy the resolved parent policy. The schema requires
this captured policy rather than re-resolving defaults when storage is reopened.

Proactive checks run before ordinary model requests and after a successful final
reply when that turn has not folded yet. They require a host-declared context
window. Within a turn, the latest validated ordinary input-token count measures
occupancy, with a saturating estimate of subsequent request growth. Known zero
differs from unknown usage. Until that turn reports usage, the runner estimates
the current instructions, messages, tools and referenced content. Warm and
restarted turns follow the same rule; no cross-turn usage cache is maintained.
Helper/child usage and admission reservation bounds never measure occupancy.
Changing the prepared route/window or folding invalidates the observation.

A heuristic alone cannot reject a request as overflowing. No replaceable source
means no helper dispatch; a later complete exchange can make folding useful.
After a useful fold, an estimated floor still above threshold suppresses further
proactive folds for that turn. Hard local bounds and one confirmed provider
rejection still apply. Unknown windows disable only proactive checks. Helpers
retain their own actual route/pricing and accounting; a failed post-final helper
fails the turn while preserving its already committed raw answer.

## Instruction capture

An ordinary turn resolves instructions once from its pinned configuration. The
runtime composes configured text, authorized project files, project skill
metadata, explicitly invoked skill bodies and the execution guide. The runner reuses this text through cells,
output correction, compaction and confirmed-rejection recovery. Later file or
configuration edits affect the next turn. Maintenance compaction does not read
external instruction sources.

Project files are unique, canonical workspace-relative paths, with at most 32
entries. Automatic reads require a standing `files.read` grant for the exact
workspace, including an unrevoked issuer chain. A short database transaction
admits the read; descriptor-confined filesystem I/O follows outside it. One-use
approvals cannot authorize capture. No grant omits project sources without
probing them. Revocation prevents later admission; it cannot retract bytes
already captured. Policies with no filesystem sources and inputs without skill
references perform no source filesystem reads.

Reads use an `os.Root`, reject escaping symlinks and nonregular files, and avoid
blocking on substituted FIFOs. Project files must be complete UTF-8 without NUL,
at most 64 KiB; size changes and short reads fail. Missing optional files are
omitted, while present broken sources fail before provider dispatch. Skill
discovery scans immediate directories under `.agents/skills`, with at most 8,192
entries and 1,024 skills. Each frontmatter block is bounded to 64 KiB, and only
metadata enters the catalog. The last sorted path wins for each exact name;
rendering, explicit selection and inspection share that winner set. Disabled
skills remain absent from the automatic model catalog but can be invoked explicitly.
The parser supports the retained scalar/block-scalar subset, validates known
fields, and preserves keys following block scalars. Complete composed base
instructions, including framing and the execution guide, are bounded to 1 MiB.

The database retains one immutable manifest per captured turn. It records the
base instruction byte count/digest and ordered source kind, root-relative
path, scope, nullable logical root ID, byte count and digest. `skill_metadata` digests cover consumed
frontmatter, including disabled and duplicate entries that affected discovery.
`invoked_skill` digests separately cover complete selected files.
`turns.instructions` reads this metadata without reopening files. Null means the
turn has no capture, including maintenance or a failed capture; missing turns
return not-found. An identical store retry is harmless; different metadata cannot
replace a capture. A failed audit write prevents provider dispatch.

Manifests do not reconstruct changed files or authorize later reads. The output
contract guide is separately derived from the pinned configuration, and each
actual provider request has its own digest. Instruction capture stores neither
full bodies nor a mutable session-level source cache. Explicit guest reads retain their ordinary operation results.

Explicit whitespace-separated `$name` tokens are resolved only from the newly
claimed canonical input's direct text parts, in first-reference order. Selection
is case-sensitive, trims trailing punctuation and deduplicates names. Unknown
names stay literal. Mail, historical inputs, tool results and attachment bodies
do not invoke skills. `discover_skills: false` suppresses automatic catalog
rendering; explicit references and human inspection can still discover authorized
skills. No read authority means no file probes or selection. A selected body
requires another current standing grant check before its descriptor-confined read;
losing authority after selection fails capture. The complete file is bounded to
256 KiB, must be UTF-8 without NUL, and its frontmatter must match the selected
metadata exactly. Body-only edits are read fresh at this admission point. All
sources together retain the 1,152-entry manifest and 1 MiB composition limits;
overflow fails before provider dispatch rather than dropping evidence.

Invoked bodies are current-turn instructions. Unlike legacy expansion, they are
not copied into durable user text or automatically carried into later turns.
Canonical input retains the literal `$name`; a later explicit invocation reads
current content. Active requests, including retries and compaction, reuse the
frozen body. Immutable metadata records what was read without duplicating bodies
in SQLite or claiming they can be reconstructed after edits.

`skills.list` supplies one live, read-only metadata API for completion and source
inspection. It reads current copied policy and standing authority in a database
snapshot, then reads files outside the transaction. It requires no runnable turn,
permit or kernel and works for idle or stopped sessions. It neither claims input,
creates permission decisions nor acknowledges mail. Results include disabled
winners and source metadata, never bodies or absolute host paths. Pages contain
at most 100 entries in name order, accept a case-sensitive prefix and exclusive
`after` name, and return `next_after` only when another match exists. Each page is
a fresh view; mutable catalog pagination is not a historical snapshot. Clients
use `turns.instructions` for historical capture evidence.

Host configuration explicitly maps at most 16 `skill_roots` IDs to absolute
directories. Captured instruction policy selects an ordered, unique list of those
IDs; an empty list selects none. Registry paths stay host-local, and unknown
selected IDs fail clearly. The instruction-root registry uses its runtime startup snapshot;
source files refresh for each capture. No HOME/environment discovery or implicit
registry grants exist. A host catalog or explicit body requires standing
`skills.read` authority for the exact named root, including the live issuer chain.
Missing authority omits that root without opening it. Human catalog inspection
uses the same authority. Root IDs and whole-field policy changes are copied into
children and frozen for active turns.

Host catalogs read immediate child directories from their root; workspace
catalogs still use `.agents/skills`. Policy order applies between host roots and
workspace entries come last, so workspace winners override host names, including
disabled winners. All roots share the existing total entry/skill/source limits.
Host source manifests use `scope: host` and a logical `root_id`; workspace sources
use `scope: workspace` and null. Source paths remain confined to their root;
escaping symlinks are rejected even if legacy global discovery allowed them.

Guest `skills.read` accepts explicit `scope` (`workspace`, `host`, or `project`),
`root_id` (null for workspace), exact `name`, a
decimal-string `offset`, `length` from 1 to 65,536, and optional full-file `sha256`.
The digest is required beyond offset zero. It resolves roots from the cell's
immutable turn configuration, then uses ordinary durable operation admission and
dispatch: `skills.read` on the host ID, `files.read` on the workspace, or
`instructions.read` on `project:<id>` for a selected project boundary. An
explicit one-use approval authorizes only that guest call, never automatic
catalog/body capture. The descriptor is opened after permission and released on
all outcomes. Each read validates a complete skill file within 256 KiB, checks
its expected digest and returns a bounded base64 byte page plus full digest,
size, offset and next offset. A changed file fails instead of mixing revisions.
This authorizes skill files only; neighboring host files and scripts gain no
authority. Catalog guidance names this tool and exposes no absolute host paths.

Standing user instructions use the explicitly configured host
`standing_instructions_file` and the captured `standing_instructions` policy
flag. Empty host configuration fails an enabled capture clearly; a configured
source without standing authority is omitted without probing it. Authority is
exactly `instructions.read` on resource `standing`, including the issuer chain;
workspace and named skill grants cannot substitute. One-use approvals do not
authorize automatic capture. No home-directory lookup, template seeding or file
creation occurs during execution or inspection.

An authorized read opens the configured parent root, then the single basename
with descriptor-relative `openat` and kernel `O_NOFOLLOW`. Go's `os.Root.OpenFile`
resolves final symlinks itself, so it cannot enforce this exact-file boundary.
The reader rejects all final symlinks, missing files, nonregular files, incomplete
or oversized reads, invalid UTF-8 and NUL. Complete input is bounded to 64 KiB
before filtering. As in the retained `me.md` convention, each line is trimmed;
blank lines and lines starting with `#` are omitted. The filtered rules join the
turn's frozen instructions; the `standing_instructions` source audit hashes the
full original file, including comments, even when no rules remain. It uses host
scope, logical root ID `standing` and only the basename. This singleton identity
belongs to `instructions.read`; an identically named skill root has separate
`skills.read` authority.

Configuration/file edits and revocation affect later turns; active turns retain
their captured rules. Children copy the flag and can receive delegated standing
authority. Restart preserves old audit and reads current rules on a new turn.
Skill catalog inspection and maintenance compaction never load this file.
Malformed authorized rules or audit failure stop before provider dispatch.

Authorized ancestors use explicit host `project_roots` (at most 16 named absolute
boundaries) and the copied nullable `instructions.project_root` selection. Neither
publishing nor selecting a root creates authority. One standing `instructions.read`
grant on exact resource `project:<id>` admits membership metadata and instruction
sources from that boundary through cwd, including cwd itself. It grants no arbitrary
file or script access and needs no additional workspace `files.read` grant. Project,
standing-file and host-skill authorities remain distinct even when names coincide.

Admission precedes canonicalization and filesystem opening. The runtime verifies
canonical membership against the opened boundary and cwd descriptors with file
identity checks, then reads only relative to the boundary's `os.Root`. Stable cwd
or boundary symlink aliases work; outside-boundary source symlinks fail. Changed
identities, broken authorized sources, chains over 128 directories, and paths over
4096 bytes fail before provider dispatch, without exposing absolute host paths.
An omitted/denied project source or a resolved unrelated boundary falls back only
when independent workspace read authority exists. An authorized missing boundary
is an explicit failure. No upward search occurs without project authority.

Project rules read broadest directory first, following configured filename order
at each directory. Skill roots follow host policy order, then the project chain
from boundary to cwd; increasingly specific entries win, including disabled skills.
The project chain replaces the separate cwd scan. The same winners serve automatic
catalogs, explicit current-input invocation, human `skills.list`, and project
`skills.read`. Each guest request names a scope/root/name, never an arbitrary
ancestor directory; its cell's captured policy and verified cwd chain determine
eligible files. Explicit invocation rechecks current standing project authority.
All roots share the existing composition, source, entry, skill and body limits.

Project audit sources use `scope: project`, the selected logical `root_id`, and a
boundary-relative path. Captured policy and immutable manifests survive restart;
new turns read current files. Descriptors close after capture or guest execution.
There is no new source database, cache, watcher, or claim that digest metadata can
reconstruct changed files. A filesystem membership check is not an atomic snapshot
of the directory tree; the source digests describe the bytes actually captured.


## Durable schedules

A session owns each schedule, including schedules created by a child. Host/client
`schedules.create` accepts a stable schedule ID, expression and ordinary input
parts. Guest `schedules.create` derives that ID from the durable operation. Exact
creation retries reuse the originally captured interval anchor, including after
cancellation or owner deletion; changed templates conflict. Expressions are
`@every <positive decimal><s|m|h|d>` or `@at <RFC3339 instant>`. Intervals must be
exact positive nanoseconds within signed 64-bit duration; instants support years
1–9999 and at most nine fractional digits. Normalization never rounds a slot.

The schedule row owns one nullable `next_due`, its immutable first due time and
interval/template, and explicit cancellation or scheduling failure. There is no
fire counter, last-fire cursor, second turn state or execution queue. Recurring
schedules are first due at their committed creation time; one-shots use the
explicit instant. SQLite stores slots as fixed-nine-digit UTC text, so offsets
and fractional seconds identify the same exact occurrence. The latest admitted
occurrence derives from input provenance and its ordinary receipt. Public input,
compaction and child admissions cannot use reserved client identities `schedule`
or `operation`; this prevents callers from occupying internal receipt keys.
Receipt inspection and waiting still accept those returned identities.

Each admission transaction checks exact pending slot, active owner, reusable
queued-input capacity, cumulative logical-write allowance, and absence of an
outstanding input for this schedule. It then inserts the ordinary prompt input
and receipt, charges the write, and advances `next_due` together. One-shots set it
to null. Any rollback retains the slot and leaks neither work nor charge. Stable
slot retries return the same input even after the cursor advances. Accepted
failed, cancelled or interrupted turns are never replayed as that occurrence.

Missed recurring occurrences catch up oldest first on their original grid. At
most one queued/running/cancelling input per schedule is outstanding; completion
permits the next distinct occurrence. This is deliberate backpressure, replacing
legacy attempts to enqueue many missed slots while an owner was busy. Stopping a
session retains its cursor and accepted inputs; reactivation resumes admission.
Cancelling a schedule stops future admission and retains already accepted work;
use input/turn cancellation separately. Deleting an inactive subtree cancels its
future schedules through identity/digest tombstones and retains ordinary deleted
input receipts. Child deletion does not refund permanent ancestor write usage.

The runtime visits eight due metadata candidates per scheduler pass, before
checking worker capacity. No client attachment, loaded session or live kernel is
required. A disposable `(due,id)` cursor and captured input-ordinal watermark
exclude schedules admitted during that sweep, so advancing fast recurring slots
cannot starve later due work. Blocked candidates advance the cursor; wrapping or
restart reconstructs the watermark. All admission truth remains in SQLite.

Reusable `schedules` capacity counts uncancelled future occurrences throughout
the subtree, including stopped and failed schedules. Roots default to 128. A
one-shot releases this capacity when its input is admitted; that input retains
its own queued capacity. Creation and each occurrence consume a logical write.
If computing a recurring successor exceeds the supported date range, the store
records `failure=successor_out_of_range` without consuming the current slot or
charging/admitting work. Other schedules continue. Inspect and cancel/recreate
the failed schedule; cancellation releases its reusable capacity.

`schedules.list` is owner-scoped and bounded to 100 metadata rows. `upcoming`
orders pending future slots by `(due,id)` and returns that exact continuation;
ordinary lists page by ID and include cancelled/completed schedules. Metadata
includes a bounded first-part text preview and latest input/receipt identity;
`schedules.get` returns the immutable full template. Inspection does not wake or
claim work. Guest create/list/cancel use existing durable operation dispatch and
exact invoking-session ownership; grants are scoped to the tree ID. Revoking a
create grant prevents new creation, while an already accepted schedule persists
until cancelled. Its future execution still checks current tool authority.


## Separate workspace actions

`workspace_actions` owns immutable human capture/restore/release identities and
their claimed/succeeded/uncertain outcomes. `workspace_snapshots` owns opaque
snapshot identities, private filesystem binding and Git object identity, and
write-once release evidence. These are not fabricated guest operations, turns or
cells. Actions bind their kind, owner and snapshot in a canonical request digest;
exact retries resolve before mutable owner, filesystem and pin checks. Retained
records survive owner deletion. Public DTOs expose neither filesystem bindings
nor Git object identifiers.

An atomic claim requires an idle active/stopped owner with no active turn, queued
input or ready mail. Claimed workspace actions block that owner's ordinary turn
claim. Capture creates an unreachable Git object from repository tracked state,
records the object before compare-and-swap pin publication, and retains the pin
until explicit release. Restore is a tracked-path overlay constrained to the
captured session directory. Untracked and later files can remain; staging is not
preserved. These actions do not rewind conversation, select checkpoints or reset
the REPL. Other sessions/tools/editors are not frozen.

Bindings include canonical worktree, gitdir/common-dir and scope directory
identities. Replacement/movement or unexpected pin identity fails closed. A Git
writer lock coordinates these runtime actions across processes, without claiming
to freeze external writers. Subprocess groups have bounded output and deadlines,
are cancelled and joined during shutdown, and ignore inherited Git environment.
Shutdown and context cancellation repeat group signals until the owned group
disappears, covering descendants forked during initial signal delivery.
RPC disconnection after a durable claim does not cancel or repeat the action.
Postclaim failures and restart recovery retain uncertainty, never automatic replay.
An explicit new release may finish cleanup when an uncertain earlier release has
already removed its pin. Retained pins block owner/subtree deletion.

Limits are 128 unreleased snapshots per owner, 1024 per runtime, 16 concurrent
workspace calls, 100 metadata items per page, 256 KiB per subprocess output stream,
and bounded 30-second preflight/workflow stages. Historical action receipts stay
durable rather than being evicted to allow identity reuse. Read calls do not run
Git. The runtime closes and joins workspace work before closing its store.


## Automatic root titles

Tree metadata owns the selected title. `automatic_title_decisions` stores one
immutable initialization decision; `automatic_title_results` stores attempt-linked
candidate/application evidence. Neither is another mutable title or job queue.
The first accepted authored root text supplies a whitespace-normalized 64-rune
fallback and at most300 runes of helper source. Attachment-only input leaves this
opportunity open. Children, forks and nonhuman initialization do not initiate it.
Explicit naming or a manual clear owns the decision permanently, including
same-value writes. Disabled helper policy still permits the immediate fallback;
source shorter than20 runes needs no helper.

Eligible intent captures the exact configuration revision, policy and complete
compaction-model override or main selection. At a free session boundary it admits
one ordinary maintenance input under the reserved `automatic-title` identity.
Human inputs and mail take precedence; queue capacity one cannot make fallback
naming reject foreground admission. The shared runner performs at most one
provider attempt with a 20-second deadline and the actual captured route output
ceiling. It does not read transcript, expand instructions or attachments, run
tools, use private continuation, or append conversation/output/preview messages.

Attempt settlement atomically records billing and a valid single-line candidate
of at most80 runes, then applies it only if the whole-tree metadata revision still
matches and the turn can apply it. Manual title, pin or archive changes supersede
a late candidate. Invalid candidates and superseded valid candidates remain billed;
exact settlement retries never overwrite later metadata. Result `applied` describes
the historical transaction, not current selection. Pending undispatched intent
can survive restart; claimed/interrupted attempts never re-arm or auto-replay.
Read-only decision/result calls do not create a receipt or wake work.


## Host provider setup and catalogs

The runtime creates the single `config.Authority` after acquiring the execution
lock and bootstrapping the explicit directory. Command-owned account/provider
services borrow it. Snapshot reads return current bounded file bytes and their
exact hash, without another mutable declaration cache. New roots capture current
host defaults/resources; existing resolved configurations do not change. Forks
capture current destination resources only for new admissions: exact prior
receipts resolve before a changed or invalid current host file is read. Startup
instruction registries remain a separately scoped runtime snapshot.

`providerhost.Service` owns bounded disposable catalog observations and borrows
credential managers. Route creation/update/removal and default/compaction changes
use explicit host revision CAS. Removing a selected default requires an atomic
replacement or clear; referenced compaction routes must be changed explicitly.
An uncatalogued explicit model selection remains valid on a configured route,
including after cache expiry/restart. This deliberately removes legacy dependence
on process-local catalog membership as configuration authority. Existing sessions
never drift to another selected model automatically.

Pasted credentials use a caller-stable key-publication ID and immutable private
0600 files. Publication precedes route CAS. Same ID/bytes retry safely; different
bytes conflict. Visible-but-unconfirmed publication is distinct from failure
before publication and requires inspection/persistence retry, not reminting a key.
At most256 files are retained; ambiguous orphan files are not automatically
removed. Public status omits key bytes and credential command arguments. Keeping
an existing credential is allowed only for unchanged endpoint/codec and without
a conflicting pasted key.

Catalog reads perform no discovery HTTP or credential command. Explicit refresh
uses bounded HTTP with no cookie jar or redirects, captures the route and exact
credential/account generation, and rejects stale completions. Failed discovery
retains same-scope cached models with an explicit failure; successful empty
responses clear them. Command-based scopes remain unverified until explicit
refresh. Managed account, file and environment changes invalidate scope evidence.
One authorized same-login subscription refresh can recover a401 without borrowing
a later account. Status/readiness report configuration, credential, catalog and
model evidence separately; inference remains `not_tested`.

Presets cover the ten retained API providers plus ChatGPT subscription routing;
they are setup templates over supported codecs. The bundled Models.dev snapshot
is retained metadata, not proof of authentication, quota or current availability.
Prices are exact nullable nano-USD per million tokens; null is unknown and zero
is explicitly free. Limits/efforts/modalities remain bounded metadata. Discovery
reads at most8MiB, a route retains at most1024 models/2MiB encoded model metadata,
and the service retains at most8192 models/16MiB globally. Inventory is a compact
projection within the bounded host declaration, and contains no executable
credential command. There is no implicit network refresh or SDK retry loop.


## Human questions

`user.ask` is a root-only intrinsic host operation. Its immutable arguments and
terminal result remain in the ordinary operation ledger. The small `questions`
relation adds only the operation identity, creation/deadline and closure reason;
it does not duplicate request or answer state. Child calls fail even if a caller
constructs a matching grant. Asking does not create a second permission prompt.

The guest may supply one question or an explicit batch of one to eight questions,
each with two to six unique trimmed options and at most one recommendation.
Single and multiple selection, bounded free text and per-question dismissal are
preserved. Explicit batches keep their shape even with one entry. Dismissal
clears its answer and returns to the surrounding turn. Request/answer documents
are capped at256KiB, text at4096 bytes, option labels at256 bytes, and question
operations at32 per turn. One root can have one live question at a time.

Beginning a question atomically dispatches its operation and records a fixed
five-minute deadline. Answering validates against captured intent and settles
the same operation. An exact normalized answer retry returns its original
outcome before deadline/lifecycle checks; a different terminal answer conflicts.
Cancellation and answer compete in one transaction, so the first committed
terminal outcome wins. Expired replies close the question and fail explicitly.

The live cell waits with bounded polling; cancellation joins SQL closure. Startup
closes interrupted questions without recreating a waiter or replaying a guest
cell. Committed answers remain available after crash. Existing uncertain-cell
recovery rules still apply to the interrupted REPL. Historical question reads
never start or resume execution. `questions.get/list/answer` and the SDK expose
scoped durable evidence and explicit answer submission; accepted work survives a
client's disconnect and no client auto-generates or auto-replays an answer.


## Workspace discovery and language diagnostics

`files.list` and literal `files.search` use the same canonical workspace,
descriptor confinement, dispatch grants and path revalidation as read/write/patch.
Listing returns at most2000 immediate names, sorted within its inspected portion.
Search returns at most100 matches, skips `.git`, descendant symlinks and unreadable
or non-text files, and reports skipped/truncated work. Traversal stops at10000
entries,64 directory levels or8MiB of searched bytes; rendered output is capped
at32KiB. These are bounded observations of a changing directory, not stable
pagination snapshots. Listing/search do not acquire mutation locks.

Host config15 declares up to16 enabled stdio language servers (including the
built-in `gopls` entry), with bounded argv, environment and matching rules. This
publishes availability, never session authority, and performs no installation.
The side-effect-free `internal/lspconfig` leaf owns declarations, validation and
built-in merging; `internal/lsp` owns transport/diagnostic behavior. Runtime owns a
bounded pool and process manager. Current host declarations are captured for each
prepared diagnostic operation through the shared config authority.

Successful write/patch settlement precedes optional automatic diagnostics. The
latter is a separate ordinary operation with a stable source-derived identity,
workspace resource, source operation and captured content hash. It needs standing
`lsp.diagnostics` authority or eligible current automatic policy; without that
authority it returns a skipped
observation without opening a permission request or starting a process. Explicit
`files.diagnostics` can obtain one-use approval, whose isolated server is joined
before the call returns. Standing grants and captured automatic policy permit
session-owned reuse. Child calls revalidate their exact grant or policy delegation
chain; parent revocation cannot leave delegated
language servers usable. Diagnostics never reverse a successful file write.

Diagnostics describe bounded captured UTF-8 content, with a captured workspace
identity and scoped project-root lookup. External writers are not frozen. The
pool has16 process slots and128 manager slots. Each manager has at most128 open
documents/4MiB content and128 diagnostic files; each file retains at most20 bounded
messages, and rendered output is32KiB. Initialization is bounded at10seconds and
matching-version diagnostics at1.5seconds; bounded sibling errors are included.
Frames are at most1MiB with8KiB headers and a bounded cancellable write queue.

Stop, cancellation, deletion, grant revocation and newly applied permission-mode
changes invalidate pool generation and
join its clients; runtime shutdown also joins its process owner before SQL closes.
Retirement waits for the owned process group to disappear, including descendants
that race the first kill signal. A delayed old prepared operation cannot recreate
a retired generation. Read-only `lsp.status`/SDK `languageServerStatus` expose
bounded safe metadata without spawning a server, granting authority or exposing
custom command/environment/startup errors. Status is ephemeral and resets on
restart. The temporary retained-config alias points inward to `lspconfig`
and must be removed with the retired core in Phase7.


## Captured definition bindings

Fresh schema38/config15 stores the resolved host module list and separate nullable
`tools_definition` / `hooks_definition` references in each configuration. Module
names and operation vocabulary come from the pure `internal/hostmodule` registry,
shared with workers and wire validation. An omitted module patch inherits; an
explicit empty list installs no host modules. The initial worker/checkpoint
factory uses configuration revision1, including this distinction.

Later configuration edits may disable and re-enable modules or custom tools
within the initial binding ceiling. They cannot introduce new names, alter exact
contracts or replace an executor owner. Required hooks cannot be removed;
optional hooks may be omitted. Model edits preserve REPL globals and saved
aliases. Every host call resolves the owning live cell and its captured turn
configuration before preparation/admission, so a retained alias cannot bypass a
new turn’s restrictions. A concurrently running turn keeps its captured policy.
Instructions describe that captured enabled subset as well.

Child creation compares its resolved bindings against the captured parent
configuration in the same admission transaction. A child cannot widen module or
tool availability or remove a required hook. Separately derived tool/hook source
references allow a child template to replace one declaration family while
inheriting the other. References identify exact registered immutable definitions;
SQL verifies their contracts, and they survive fork, restart and ancestor deletion.
Callers cannot write these owner references through configuration patches.

Arbitrary host/override declarations advertise syntax without inventing executor
ownership. A changed family loses that source reference; exact inherited subsets
retain it. Tools validate their captured input schema before executor lookup.
Their timeout contract preserves a five-minute default and fifteen-minute cap;
hooks preserve thirty seconds by default and at most one minute. Hook operation
filters distinguish null (all) from empty (none).

Live executors now bind exact immutable definition revisions on a persistent
initialized connection. The runtime owns a bounded registry (64 peers,128 leases,
16 leases/peer,128 calls,16 calls/peer and64 bind waiters). Replacement waits up
to five seconds for earlier calls; disconnect revokes that connection's leases.
No restart, reconnect, pending-list read or checkpoint recreates execution
availability or resends an invocation. Queues retain at most32 events/8MiB in
aggregate; event frames are at most1MiB, results512KiB and progress2KiB.

Custom input validates before consent. The dispatcher acquires the current exact
executor only after permission and before SQL dispatch, then sends the recorded
operation. Known handler failures and invalid returned schemas settle failure;
disconnection after dispatch remains uncertain. Hook rewrites validate against
the ordinary tool/spawn contracts. A required unavailable or denying hook blocks
the effect; optional failure produces bounded skipped evidence. Cancellation
never becomes optional success. before_spawn previews the ordinary child policy
and actual admission rechecks it transactionally. turn_start notices are bounded
and only the owning live turn may receive them.

Executor activity is disposable: the active turn retains at most eight hook
decisions and one progress preview with epoch/revision identity. It disappears
on turn completion/restart. Canonical arguments, operation outcomes and cell
failure remain SQL evidence; progress is never a second durable audit log.


## Root creation receipts and catalog revisions

Fresh schema39 adds caller-identified root creation and one global tree-catalog
revision. `trees.create` requires a stable `creation_id`; the caller persists
that identity and exact request before delivery. Its digest includes the caller’s
request, not mutable host defaults. Exact retries resolve before reading current
host configuration and are checked again in the admission transaction. The
receipt, tree, root, resolved defaults, policy, budgets/resources and catalog
increment commit atomically. Different-payload identity reuse conflicts.

A creation receipt is immutable, retained without foreign keys to live owners,
and records the original tree/root identities and timestamp. Creation and
`trees.creation` return the receipt plus nullable current tree/root projections
and `deleted`. A retry never resurrects a deleted root. These are admission
receipts, not synthetic execution inputs or attempts. Trusted in-process
`CreateTree` is a fresh-identity convenience over the same transaction; public
delivery uses the explicit caller identity. Host defaults cannot attach forged
custom-executor provenance through either path.

The positive int64 catalog head increments in the same transaction as root
creation/fork/deletion, metadata edits, authored fallback titles and applied
helper titles. Exact creation/fork retries and failed CAS leave it unchanged.
Same-value manual metadata edits keep their established revision/ownership
semantics and therefore increment the head. A helper whose title cannot apply
because the catalog counter is exhausted still settles incurred billing and
records an unapplied candidate; SQL failure rolls back the transaction.

`trees.catalog` is a lightweight invalidation head for every root, including
roots outside loaded pages. `trees.list` reads that revision and at most100
metadata summaries in one SQL snapshot, without hydrating configuration or
history. Each summary includes its root identity. Optional archived/pinned
filters preserve false versus omitted. Keyset pages return `next`; an optional
`expected_revision` rejects changed membership or metadata rather than mixing
snapshots. A conflict requires a deliberate new traversal. `definitions.list`
returns bounded immutable-revision metadata, never full declaration bodies.

The SDK exposes explicit creation, receipt/head/page reads and a bounded
revision-aware `treePages` iterator. It does not create a catalog cache, poller
or retry policy. Full SDK view ownership and product sidebar adoption remain
Phase6 work; this foundation does not mark those clients migrated.


## Browser gateway ownership and network controls

The v4 command starts `internal/gateway` only with explicit `-web`; its default
listener is loopback, and only a busy default port permits an ephemeral fallback.
The gateway borrows the actual runtime generation's `Done` lifetime. Startup and
every upstream connection must acknowledge protocol4, the pinned runtime ID and
process epoch, and `network_client: true` before forwarding application traffic.
One WebSocket owns one private socket, with no request multiplexing, reconnection
or replay. New initialization on an existing connection is rejected. Disconnect
stops observation, while accepted execution remains owned by the runtime.

Exact Host and Origin checks include an explicitly allowed `whip-app://bundle`;
null, duplicate and suffix origins do not acquire trust. These checks are not
user authentication: non-loopback access requires a trusted network or an
authenticated proxy. RPC captures its immutable network marker, and human
terminal families plus `shell.input` require explicit operator `-web-terminals`
authorization. Allowed network account/setup operations keep the same public
service boundary. The gateway cannot erase the marker with a later handshake.

Managed gateway failure does not own the local execution lifetime. Host status
publishes optional `web_state` (`starting`, `running`, `failed`) and bounded
`web_error`; absence means socket-only, and only a running gateway publishes its
endpoint. A new explicitly network-enabled launch waits for actual gateway
readiness and reports failure while leaving the core available. Starting an
already running host is idempotent and preserves its original network policy;
changing it requires explicit restart. Gateway shutdown/failure clears its
endpoint, and host shutdown still closes and joins the gateway. These are live
command-owned observations, never a durable session or second configuration.

Frames are bounded at8MiB, with48 WebSockets and16 concurrent content transfers,
write deadlines and socket backpressure. JSON envelopes are checked for duplicate
keys and compacted before newline framing. Shutdown closes and joins hijacked
connections. Scoped HTTP uploads/downloads delegate to the ordinary v4 content
owner and verify runtime/session/reference identity, exact length and digest.
The old64MiB gateway upload cap is deliberately replaced by the shared4MiB v4
content contract; there is no separate upload registry or digest-based authority.

`@whip/sdk/browser` offers ordinary calls and scoped content transfers. Each
connection verifies the selected host, local cancellation stops only observation,
and no transport automatically retries mutations. Packaged assets are the actual shared
native application. A build without assets still reports `available: false` and
returns503 at `/`; discovery never substitutes a placeholder or claims an
unpackaged renderer is available. Exact product acceptance remains in the
Phase6 development record.


## Session shell execution and human input

The runtime owns one bounded shell manager. It captures an owner generation and
canonical cwd identity before consent; acquisition reserves capacity after
consent and rechecks that identity before launch. Shell authority is not an OS
filesystem sandbox. Processes receive the capability manager's allowlisted base
environment, with no implicit provider credentials. Foreground commands cap at
120 seconds; background jobs may run until explicit stop or an optional timeout
up to24 hours. Jobs belong to sessions and survive turn cancellation/completion.
Session stop/deletion and runtime shutdown kill and join owned process groups;
loaded worker eviction does not stop a background job. Handles never restore.

There are at most128 owners,64 running commands (eight/owner), and128 retained
job records (32/owner). Pressure evicts completed records, never a running job.
Output retains a1MiB tail per command/job with exact decimal original/retained
byte counts. Results include an8KiB inline preview and explicit truncation;
large retained tails use an owner-scoped content reference. Failed content
publication preserves the known command result and reports missing retention,
never causes redispatch. Ordinary nonzero exit is structured command output;
known pre-launch errors are failed, while interrupted launched effects remain
uncertain with any retained partial evidence.

Interactive shell.run has a15-second inactivity deadline within its120-second
hard limit. Human shell.interaction reads a64KiB cursor-addressed preview;
shell.input addresses that exact owner and operation with at most16KiB and a
strict decimal sequence. Four queued inputs are permitted. Retrying the most
recent identical sequence acknowledges admission without enqueueing twice;
changed, older, future or retired identities conflict. Closing the operation
joins callbacks and clears pending input, so bytes never spill into a later
shell. The child controls PTY echo. These controls are separate from human
terminal tabs, and the gateway rejects shell.input unless network terminals
are explicitly enabled. Observation/disconnection does not cancel accepted work.

## MCP ownership, imports and delegated discovery

Fresh schema40/config16 introduces native MCP declarations and nullable captured
server selection: null inherits configured availability, while an empty list
selects none. Child selection can only narrow its parent's scope. Each runtime
owns one MCP connection manager and its private process manager, separate from
session kernels and human terminals. Session stop/deletion and runtime shutdown
cancel and join owned work; a reload does not replace unrelated session state.

Metadata inspection never implicitly connects a server. Guest catalog reads
validate the exact session, captured configuration and live delegated grants.
Calls and connections enter the ordinary operation/consent/dispatch ledger.
Only explicitly trusted server variants are eligible for saved automatic
permission policy. Untrusted variants remain excluded both at admission and
immediately before dispatch; SQL rejects a forged policy-revision bypass.

The host configuration service owns explicit compare-and-set writes, imports,
refresh/reload, enable/disable, attach and reconnect. Import candidates carry
fingerprints checked again at publication. Native declarations override project,
Codex, Claude and OpenCode sources in that order. Project imports default disabled;
copying a declaration does not imply trust. Unsupported OAuth/SSE configurations
remain visible with an unsupported outcome. No account or network action occurs
merely because a client reads status or an import candidate list.

Limits are128 owners and128 host connections,16 per owner,64 configured servers;
catalogs are2048 tools/2MiB per owner and8192 tools/8MiB per host. Wire frames cap
at16MiB with a64MiB shared wire allowance; decoded results have an independent
64MiB allowance and at most four held result slots through projection. Declared
configuration and metadata each cap at8MiB. Model-visible result previews cap
at32KiB; larger text/images use session-owned content references. At this
checkpoint image references are result metadata; same-turn model vision is a
following typed-attachment checkpoint, not an implied feature of JSON output.
Brand icons use64 domain entries, four active requests,4-second deadlines,
48KiB bodies and512 disk records. Their resolver owns its HTTP transport and
never borrows an account manager's replacement of the global default transport.

## Human terminal tabs and persistent browser executors

Human terminals are command-owned PTYs, independent of sessions, models and
agent permissions. Every terminal request carries the exact process epoch;
restart rejects old handles and restores no process or transcript. Open requires
an explicit absolute canonical directory, host-resolved login shell and the
process manager's allowlisted environment. Login profiles remain host controlled.
The16-handle registry retains a1MiB byte ring per terminal, accepts at most16KiB
per write and returns at most32KiB per read. Offsets are exact decimal counters;
truncation is explicit. Detaching a reader leaves the terminal running.

An uncertain open is resolved by listing the current epoch's terminals. Writes
have no durable receipt and must not be replayed after a lost acknowledgement.
Explicit close and command shutdown join output readers, attachments, the shell
and its captured foreground process group. Retiring a handle under its lock
prevents a delayed attachment from reviving an evicted terminal. Both terminal
and agent-shell masters use the same close-interruptible PTY primitive. Process
groups do not provide a sandbox or ownership of arbitrarily detached descendants.
Network clients require the operator's explicit terminal opt-in at the RPC owner.

`browserDuplex` supplies an identity-checked persistent transport to the same
ExecutorClient used over Unix sockets. Initialization pins runtime ID, optional
process epoch and the gateway network restriction before dependent calls. It
bounds32 pending requests,32 events and8MiB aggregate incoming bytes, supports one
event consumer and closes the peer on overflow or transport loss. Request and
startup deadlines do not impose a hidden lifetime on an established executor.
Closing revokes connection leases; accepted execution remains owned by Go.
There is no automatic reconnect, rebind or replay. Browser UI assets and product
client adoption remain Phase6 work.

## Direct human actions and guest utility parity

Fresh schema41 permits intrinsic guest permission inspection; schema42 adds
accepted `host_operation` inputs and direct turn provenance. `artifacts.put`
accepts up to128KiB of text and an optional256-byte source label, then writes
session-scoped immutable content only after ordinary consent. The stable
reference derives from the operation ID; an empty body is valid. Inspect returns
metadata, and read remains a separate owner-scoped operation. A digest alone
never grants access. `permissions.request` explains how ordinary effect requests
obtain consent; it creates no grant. `permissions.status` reads only the exact
owner's existing decision through an intrinsic, captured-configuration-checked
inspection capability. Neither automatic policy nor a forged policy revision
can widen that path.

Human `shell.run` and `tool.call` admit a typed fixed-surface input, using ordinary
stable request identity, receipt, queue, captured configuration, turn, execution
permit, hooks and dispatcher. The public catalog contains supported file/shell
and declared custom tools. These actions create no provider call, interpreter,
cell, synthetic conversation or automatic title/goal/report. The operation has
exactly one provenance: a cell or a direct turn. Schema constraints and shared
owner checks retain that distinction through consent, dispatch, result and
recovery. A wholly empty model selection is an explicit model-free session;
ordinary prompts/model helpers fail closed until configured.

Exact retries resolve before mutable checks. A new direct action rejects a busy
owner; later ordinary prompts may queue behind it. Other maintenance/goal work
cannot bypass that busy policy. Lost replies and restart use the same immutable
receipt and uncertain-effect rules as model work. Human origin does not grant
extra child authority or bypass effect consent. Terminal tabs and interactive
input remain separate transient host resources.

## Canonical history, activity and input discovery

`sessions.history_page` reads revision, active count, actual active tail and a
bounded page in one SQL statement. Forward and backward cursors are exclusive
exact counters; missing backward cursor selects the actual tail. Explicit zero
is an empty backward boundary. Every page returns messages in ascending order,
with a nullable continuation naming a returned sequence. Retirement gaps are
valid and never navigated by subtracting counts. A stale expected revision
rejects even an empty result. Pages cap at100 messages/4MiB and never load a
worker or claim input.

`sessions.activity` is a single-statement SQL projection of lifecycle, active
turn/input, queued count, pending permission/question counts, execution-permit
ownership and any claimed workspace action. An unfinished turn without a permit
is waiting work; absent provider preview is not evidence of idleness. Direct
human and cell operations contribute decisions through their actual owner.
`inputs.page` discovers accepted inputs by immutable ordinal, including work from
other clients. It returns at most100 metadata records with the first512 runes of
text, attachment count and explicit preview truncation; full bodies are fetched
only through owner-scoped `inputs.get`. Queued and all-input filters are explicit.
No read performs admission, resumes a waiter or starts execution.

`receipts.match` compares original typed parameters with the canonical Go-owned
admission digest and returns the existing admission only on an exact match.
It supports submissions, compaction, spawn, goal formulation/resume and direct
shell/tool input. Shared normalization and conversion functions are also used
by admission; JavaScript does not reimplement request hashing. Missing returns
not-found without writing, changed payload conflicts, and matching tombstones
survive deletion and restart. The operation is a read, not a dry-run or replay.
Other identity-only receipts do not by themselves prove full payload equality.

## Native controls, host previews and execution traces

Fresh schema43 adds trusted typed image references to tool settlement. Every
reference preserves its owner; arbitrary text cannot manufacture an image.
A cell holds at most8 references/16MiB until settlement. Provider preparation
hydrates the committed references for the same next round, with its existing
4MiB request bound and explicit failure if exceeded. Completed valid MCP images
share this path; interrupted or malformed chunks are not image evidence.

Schema44 and host config17 add explicit native computer connections and reviewed
application policy. One command-owned process manager is shared by shell,
language servers, picker and computer helper. Computer batches use one active
slot/four waiters, prevalidate all calls, and check current application policy
and SQL authority before every effect. Permission capture is inert. Revocation
and close cancel/join the helper; restart and interpreter discard never restore
accessibility handles. Automatic session policy cannot override host application
restrictions. Typed screenshots use ordinary content publication and settlement.

Directory, picker, skill and theme previews belong to bounded human host services,
not session execution. Picker work is limited to2 processes,120seconds and8KiB;
shutdown joins it. Skills use fresh host configuration and exact definition
policy, with canonical project scope. Theme reads stay beneath the explicit
runtime directory and reject blocking/special files. Tree attention is one
bounded SQL snapshot across exact root/child activity, including stopped queued
work and pending human decisions; it never hydrates workers.

Schema45 adds a latest-change identity index for canonical execution evidence.
The index stores source/owner/turn identities and a monotonic sequence, never a
second span body or state. Changes/deletions project through bounded snapshot
reads; tombstones survive source deletion. Fixed-revision pagination rejects
intervening changes and advances through filtered empty pages. Child causality
requires the exact successful spawn/submit operation receipt. OTLP export is
limited to4096 spans/4MiB/16 pages/10seconds, written as root-owned content with
no network transmission. Time precision and open spans are explicit. Historical
prepared provider input bodies are unavailable; current transcript is never
substituted as historical evidence.

Host config18 adds at most16 saved remote URL profiles with safe metadata only.
The shared configuration authority owns revision CAS; stale same-value writes
conflict, current same-value writes preserve the revision. Exact validated root
URLs and caller-observed runtime pins are retained. Reads/writes neither connect
to a saved host nor expose credentials. Native SSH/device profiles remain app
state. Gateway discovery is a bounded passive read; SDK transports enforce the
verified runtime and, when provided, process epoch before dependent requests.


## Authored design evidence and session controls

Display-only design provenance was introduced in fresh schema 48 after schema
46's workspace and run controls; schema 47 was allocated to browser integration.
That checkpoint used host config 18. The current versions are listed at the
[host and schema boundary](#host-and-schema-boundary); protocol remains 4. Only
the additive schema55-to-56 upgrade is supported; retired-core databases remain
unsupported.

`sessions.submit.design_context` optionally identifies a unique text content
reference and an optional unique image content reference in the submitted parts.
References are resolved in the exact recipient's scope before admission. The
immutable descriptor is part of the receipt digest. It is limited to 8 element
summaries, 1,000 selected elements, 160-byte labels, 256-byte selectors/titles,
2,048-byte page URLs and 8 KiB total. Human prompt inputs alone can carry it.
Native transcript entries derive the descriptor from their input; imported fork
entries retain it with their copied parts and owner references. Both history
readers derive exact part indices, including leading authored text. Callers cannot
supply indices. Literal tagged text never creates metadata. Model requests receive
the original text/content evidence; display metadata is absent from their shape.

`workspace.inspect/set` exposes the selected session's canonical working
directory and configuration revision. A set request has a stable ID and exact
payload digest, checks its receipt before mutable path/session state, and uses
configuration CAS. The whole tree must be idle, with no queued input or claimed
workspace action; the owner must release snapshots and live shell work first.
Only that tree's admission/claim gate is held while obsolete shell/LSP/root-MCP
resources are joined. Other trees remain schedulable. Existing grants are never
remapped to a new path. Child directories, history and REPL state are retained;
active turn inspection uses its captured configuration's directory.

`run.configure` captures root-only system override, max-turns, headless and cache
key settings with the same receipt/CAS/idle rules. Empty system text restores
composed instructions; required turn-start hooks still run. Explicit zero max
turns is uncapped; a positive limit permits that many tool rounds followed by one
recorded request without tools. Absent run configuration keeps the ordinary 32
round bound. Headless denies new human waits while existing standing/automatic
authority remains effective. Cache keys do not replace execution identity and
are mapped through provider-specific cache handling. Children do not inherit
root run configuration.

Live cell stdout is disposable runtime observation. `cells.output` reports one
nullable preview for an exact session, cell, turn, call and history revision,
scoped to the current process epoch. At most64 sessions retain at most64 KiB of
valid UTF-8 each. The engine's existing cumulative output callback replaces that
prefix; truncation is explicit. SQL reads suppress settled cells and retired
history, while the committed cell result remains the durable full outcome.
Closing or reopening the runtime cannot restore or replay this preview.


### Turn accounting and latest prefill

`usage.turn` / SDK `session.turns.usage(turnID)` selects the exact session-owned
turn and derives totals from its actual attempts, including helper and compaction
attempts. It excludes imported history, other turns and children. Committed
compactions remain counted after summary undo; they describe work performed.
Reported and estimated costs remain separate, as do unknown cost, missing token
quantities, overflow and uncertain dispatch. These counters are decimal strings
on the wire. Reading totals neither prepares nor dispatches a provider request.

`context.usage` / SDK `session.context.usage()` reports the latest ordinary or
final prefill with its captured model, attempt, turn, history tail and route
capacity. Provider-reported input, including zero, takes precedence over the
captured estimate; the source is explicit. A newer history tail marks that
prefill stale rather than claiming a current token count. Changed configuration,
history revision or context selection makes the old evidence unavailable with a
specific reason. Unknown capacity remains null. Helper requests and reservations
do not replace ordinary prefill evidence. Credential refresh does not rewrite
captured request evidence. Schema55 adds a derived lookup index, not a second
mutable token ledger.

### Native editor and MCP clients

ACP uses the canonical Go client for sessions, scoped content, observation,
prompts, cancellation and pending decisions. Preview reconciliation joins stable
message and attempt identities with committed history. Imported tool exchanges
retain their actual error flag and scoped images; an interrupted preview is
explicitly withdrawn. An append-only editor that cannot represent a history
rewind receives an explicit failure rather than a duplicate transcript. Closing
the editor joins its work without stopping the host.

MCP management and stdio tool hosting use the same native host. The ten retained
tool aliases are explicit adapters over native tools; they have no retired daemon
path. Tool hosting creates an owned model-free root with workspace read authority
and independently denied interactive requests. An explicit all-empty model
selection overrides even a configured host default; ordinary model prompts on
that root fail closed. A nonempty model configuration requires a name. Unknown
admission stops further calls and preserves the session for inspection instead
of resending an uncertain effect. Shutdown joins accepted work before deleting
only the endpoint-created root. The command-owned stdio wrapper bounds MCP lines,
arguments, batches, concurrency, output and blocked writes while preserving
ACP's separate frame limit.


## Long-home native socket placement

`internal/runtimepath` is the shared pure socket-address function. Short homes
keep `runtime.sock` in the selected runtime directory. If that path exceeds the
100-byte Unix bound, the socket alone uses `/tmp/whip-<uid>-<full SHA-256 of the
absolute runtime directory>/runtime.sock`. This fixed short location is
independent of `TMPDIR`, so launch and later discovery agree. Host configuration,
SQL, content and the sole execution/launch locks stay in the selected home.
Read-only discovery creates nothing.

The execution owner holds the durable runtime lock before creating or validating
the fallback directory. It must be a real0700 directory owned by the current UID;
symlinks, other owners, public modes and non-socket occupants are rejected without
repairing permissions or replacing files. Only that owner removes a stale socket.
RPC retains its100-byte bound and0600 socket. Closing the owner removes an empty
fallback directory only; there is no recursive cleanup of unexpected contents.

## Offline model catalog maintenance

`internal/modelcatalog` owns the reviewed Models.dev snapshot, provenance, codec
and immutable-copy helpers. The provider host and `cmd/modelgen` share that single
source; ordinary builds and checks never download a catalog. Generator policy
comes from native `providerhost.Presets` and explicit retained metadata overrides.
The generated desktop environment inventory still combines declared names with
reviewed upstream aliases. Offline candidates never establish live membership,
credentials, model defaults or price evidence for an actual provider attempt.

# Models and providers

The native host owns provider configuration, credential resolution, account
login, catalogs and execution. Clients send explicit setup or selection requests
and receive redacted metadata. Reading configuration never resolves a secret,
runs a credential command or makes a model request.

Native declarations live in `~/.whipcode/runtime-v4/host.json`, or under the
explicit `WHIPCODE_HOME`. The host creates a fresh namespace; it does not import
retired `config.json`, account captures, session tables or runtime identities.
Use [setup](setup.md) for first launch and the [SDK](../packages/sdk/README.md)
for typed configuration and account services.

## Provider connections in Settings

Settings and the terminal setup flow distinguish configured routes, available
setup templates, credential readiness and live model membership. A template or
an environment variable does not silently create a route or select a default.
Explicit discovery/setup can offer named host credentials; a pasted key is a
secret submission, never a value read back into forms or browser storage.

The client chooses whether a selection applies to the current session or host
defaults. Existing sessions retain captured configuration. Initial prompts and
drafts are not sent merely because setup or model discovery succeeds.
Concurrent edits use the observed host revision; a conflict requires rereading
instead of overwriting another client's setup.

The current reviewed templates include Inference.net, OpenRouter, OpenAI API,
ChatGPT subscription, Cerebras, DeepInfra, DeepSeek, Fireworks AI, Groq,
Together AI and xAI. Their exact endpoint, environment names and suggested
models live in [`providerhost.Presets`](../internal/providerhost/presets.go).
Suggestions do not prove that an account can access or execute a model.

## Supported provider types and custom endpoints

Native route kinds are:

| Kind | Execution contract |
| --- | --- |
| `openai-chat` | OpenAI-compatible Chat Completions, with captured compatibility/sampling rules. |
| `openai-responses` | Responses API with captured tools, usage and private continuation handling. |
| `openai-codex` | Fixed ChatGPT subscription endpoint using the host's account manager; no custom URL or API credential. |

There is no native Anthropic Messages or Gemini wire adapter. A model exposed by
a compatible gateway can use that gateway's supported route. Compatibility
checks and unknown metadata remain explicit; a successful catalog response is
not an inference test.

A custom API route declares its base URL, transport and one credential source.
Do not include an operation suffix such as `/chat/completions`, embedded URL
credentials, a query or a fragment. Custom route IDs do not inherit a preset's
endpoint or credential simply because their names match.

For example, this **provider entry** declares an explicitly unauthenticated
local Chat Completions server; it is not a complete host configuration:

```json
{
  "kind": "openai-chat",
  "base_url": "http://127.0.0.1:11434/v1",
  "credential_source": "none",
  "models": {
    "my-model": { "max_output_tokens": 4096, "timeout_millis": 120000, "max_attempts": 3 }
  }
}
```

The selected model uses `{ "provider": "route-id", "name": "my-model" }`.
Host model declarations supply dispatch limits and optional exact price evidence;
model selection itself belongs to the session. Unknown prices and context limits
remain unknown. Interactive updates go through revisioned host controls; explicit
local bootstrap uses the complete validated declaration.

## Local key discovery

Discovery and secret resolution are separate. Named environment credentials
resolve on the execution host, including for remote sessions. A desktop renderer
or phone does not supply its own environment to the host. Host process environment
changes require an explicit restart to affect that process.

The native host does not guess credential files belonging to other tools.
Choose one of the explicit sources below. Existing external credentials remain
owned by their original application; removing a WHIP route does not authorize
changing an external file or environment variable.

## Bundled Models.dev metadata

[`internal/modelcatalog`](../internal/modelcatalog) owns the reviewed snapshot
shared by provider discovery and generated desktop provider names.
`task models:check` validates it offline; `task models:update` performs an
explicit refresh. Bundled metadata supplies reviewed capability/limit information
and offline choices, not live account membership or measured price estimates.

Successful live catalogs determine current membership, including a successful
empty result. Sparse metadata stays unknown. Cache scope includes the route and
credential/account identity; login replacement or configuration changes cannot
reuse another account's catalog. Failure and staleness are surfaced without
inventing an available model. Current policies live in
[`providerhost/catalog.go`](../internal/providerhost/catalog.go) and
[`providerhost/models.go`](../internal/providerhost/models.go).

## Routing model

A turn captures its session configuration, route, model, sampling, instructions,
output limits and relevant account generation. Later edits do not mutate a
request already admitted for execution. A provider attempt has separate
reservation, dispatch and settlement facts. Credential checks occur before HTTP
and after a permitted refresh; an unrelated login cannot replace the captured
account mid-attempt.

The runtime supports captured temperature, top-p, top-k, repetition/penalty,
seed and reasoning controls only where the selected route/model contract allows
them. Unsupported settings fail explicitly. The same captured policy applies to
ordinary turns and stateless helper calls. See
[the provider adapter](../internal/model/model.go) and
[the agent loop](agent-loop.md).

## Key resolution

| Source | Native declaration and ownership |
| --- | --- |
| Environment | `credential_source: "env"` with one `credential_env` name. |
| File | `credential_source: "file"` with one clean absolute `credential_file`; bounded private file validation occurs on the host. |
| Command | `credential_source: "command"` with an absolute executable, argument vector and explicit environment-name allowlist. |
| No authentication | `credential_source: "none"`; other credential fields must be absent. |
| Managed Inference.net | `credential_source: "inference-net"` on the exact canonical chat route; uses the host's managed inference credential. |

A credential command is administrator-authored configuration, not shell text.
It runs with no stdin or ambient environment beyond declared names, has a ten
second deadline and bounded output, and joins its process group. Metadata reads
do not run it. Secret bytes never enter session configuration, history, catalogs,
protocol status or ordinary error diagnostics.

Pasted keys use private files and stable publication identity. An uncertain
publication is inspected/retried with that same identity; a setup conflict does
not delete a key that may have been durably published. See
[`config/credentials.go`](../internal/config/credentials.go),
[`config/provider_key.go`](../internal/config/provider_key.go) and
[`providerhost/setup.go`](../internal/providerhost/setup.go).

## OpenAI: ChatGPT subscription

ChatGPT device login belongs to the same host account manager used by model
execution. Clients begin, inspect or explicitly cancel the flow; disconnecting a
client does not cancel it. Public records expose bounded status, device approval
metadata and safe account details, never tokens or private continuation bodies.
Login replacement and logout invalidate captured generations.

The subscription route has a fixed endpoint. Its transport does not support a
wire output-token cap, so execution requires a reviewed natural model ceiling;
unknown ceilings or a smaller requested cap fail explicitly. This is separate
from model availability in the current account. The reviewed ceilings and
structured usage-limit diagnostics live in
[`model/subscription.go`](../internal/model/subscription.go).

Private Responses continuation evidence is scoped to the route/account/model
and authorized session content. Public transcripts never expose credentials or
private continuation material. Refresh is not permission to replay uncertain
stream output or completed code.

## OpenRouter and Inference.net

OpenRouter uses a compatible chat route with an explicit API credential. Setup
validates through bounded host discovery before publishing the chosen route;
its public catalog alone does not establish authenticated inference access.

Inference.net account login has bounded device, team, project and key-selection
steps. Management credentials and inference credentials are separate. The
managed inference source is valid only for the exact canonical Inference.net
endpoint; a custom gateway cannot acquire that account merely by choosing the
same provider name. Explicit API-key setup is also available. See
[`internal/inferenceaccount`](../internal/inferenceaccount) and
[`internal/inferenceauth`](../internal/inferenceauth).

## Token bookkeeping

Each dispatched ordinary or helper attempt retains usage independently from
its terminal success/failure. Missing usage is unknown, not zero. Input and
output are totals; cached input is a subset of input, while reasoning and cached
output are disjoint subsets of output. They are never added to those totals a
second time. Read-only usage inspection does not execute a provider request.

## Cost tracking

Price evidence uses integer nano-USD per million tokens. A missing rate is
unknown; zero explicitly means free. Calculated and reported costs retain their
source. Unknown special-category prices cannot be silently replaced with another
rate, and an overflow preserves usage/evidence instead of wrapping an integer.
Public large counters use decimal strings; clients must not round them through
JavaScript `Number`.

Budget checks reserve bounded work before dispatch and settle actual evidence
without erasing charges when output publication fails. Session/tree accounting,
helper attempts and provider calls stay distinguishable. See
[`session/model.go`](../internal/session/model.go) and the
[domain accounting contract](backend-domain.md#model-budgets-and-retained-accounting).

## Compaction model

Compaction has an explicit captured helper selection. Missing configuration
fails clearly; it does not silently choose a cheap model or a different provider.
Raw history stays intact while a selected summary records exact source coverage.
Context-rejection recovery is bounded and requires confirmed provider evidence
and forward summary progress. Generic transport errors and partial streams never
authorize replay. See [context and compaction](agent-loop.md#context-and-compaction).

## Read next

- [Setup](setup.md) for fresh native configuration and account ownership.
- [Runtime](rlm-runtime.md) for captured turns, cells and recovery.
- [SDK](../packages/sdk/README.md) for configuration, catalog and account methods.
- [Development record](backend-redesign-development.md) for exact deterministic,
  live-provider and platform acceptance evidence; a fixture pass is not a live
  provider qualification.

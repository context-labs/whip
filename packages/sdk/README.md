# WHIP SDK v4

This SDK talks directly to the new Go runtime. The retained product applications
currently use `@whip/legacy-sdk`; their later cutover is tracked in
[the redesign plan](../../docs/backend-redesign-plan.md).

```sh
npm ci
npm run build -w @whip/sdk
go run ./cmd/whip-runtime -directory /tmp/whip-example/state -scripted
# In another terminal, using the socket printed above:
node packages/sdk/examples/session.mjs /tmp/whip-example/state/runtime.sock
```

The directory must be private to its owner (mode 0700); the runtime creates it
when absent. It never discovers or attaches to an installed daemon. The scripted
fixture provider requires the model selection `scripted/scripted`.

`Client.connect(transport, {clientID, expectedRuntimeID?, signal?})` pins the
runtime identity. `unixSocket(path)` is available from `@whip/sdk/node`; the main
entry point imports no Node APIs. Each call has a bounded connection with a fresh
handshake, and `client.call(method, params)` uses generated v4 types and validators.

`submit(sessionID, parts, requestID)` returns a durable admission. Persist the
runtime ID, client ID, request ID, session ID and exact payload before submitting
when the application needs crash recovery. Reuse that identity and payload on an
uncertain retry. A `DeliveryError` cannot establish whether work was accepted;
`recover(requestID)` reads its receipt. Reusing an identity with different input
returns `RemoteError` with kind `CONFLICT`.

`wait(requestID, {signal})` polls durable state until the input is cancelled,
deleted, or its turn finishes. It returns failed/interrupted outcomes as data.
Aborting the wait only stops observation; `inputs.cancel` or `turns.cancel`
explicitly cancels execution. History uses bounded pages and exact decimal-string
cursors. The client keeps no transcript cache or second execution state machine.

`turns.attempts` reads bounded provider accounting with exact decimal counters.
Retries have separate attempt IDs, a shared logical-call ID, and a link to the
completed response. Unknown usage/cost is `null`, independently of known zero.

Use `content.put` with `{session_id, reference_id, media_type, data_base64}` to
upload up to 4 MiB. Generate and retain a unique reference ID before sending;
retrying it with the same owner, bytes and media type returns the same reference.
Submit `{type: 'content', reference_id}` parts alongside text. `content.read`
takes the owning session and reference IDs and returns verified `data_base64`.
References are session-scoped; a digest is not an access token. The runtime
hydrates authorized bytes for the provider while history keeps the reference.
Each session is limited to 1,024 references and 64 MiB of referenced bytes.

For configured HTTP providers, omit `-scripted` and use the host configuration
described in [the development guide](../../docs/backend-redesign-development.md#openai-compatible-dispatch-increment).

See [the runnable example](examples/session.mjs), [Go client](../../internal/client/client.go),
and [real process acceptance](../../scripts/redesign/v4-fixture.test.mjs). Streaming,
effect authority, engines and product-client adoption follow in later phases.

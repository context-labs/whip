# Whip documentation

This source tree uses the native Go backend and protocol v4. The redesign
is an unmerged draft stack; it does not replace an installed application or runtime.
Use the [development record](backend-redesign-development.md) for exact revisions,
validation and outstanding work. The [accepted plan](backend-redesign-plan.md)
tracks retained capabilities and final cutover requirements.

## Start here

- [Architecture](architecture.md): how clients, durable admission, execution and recovery fit together.
- [Backend domain](backend-domain.md): the implemented ownership and persistence contract.
- [Frontend guide](frontend.md): the canonical application architecture, state ownership, styling and validation guide.
- [TypeScript SDK](../packages/sdk/README.md): native connections, uniform sessions, durable commands, scoped content and bounded views.
- [Feature map](features.md): retained capability inventory and current native source references. [Core retirement](backend-native-core-retirement.md) maps removed implementation/test families to their replacements; [the gate audit](backend-native-gate-audit.md) records remaining acceptance.
- [Evaluation guide](../evals/README.md): canonical native trials, accounting and evidence.

## Run a native source build

Use the pinned Go toolchain, Node.js 24, the committed npm lockfile and Task.
The actual build and validation commands are in [Taskfile.yaml](../Taskfile.yaml).
Builds and tests must use disposable runtime storage while working on this draft.

A native runtime for local SDK experimentation can be started explicitly:

```sh
go run ./cmd/whip-runtime -directory /tmp/whip-example/state -scripted
```

That fixture prints its socket and identity. It uses synthetic local model
responses and has no external provider credentials. Follow the
[SDK example](../packages/sdk/README.md) to connect and select its scripted model.
Normal model work requires an explicitly configured host provider.

The supported CLI controls a native runtime with `whipcode daemon
status|start|stop|restart|logs`. With an existing native runtime, `whipcode web`
starts its own foreground gateway. `whipcode web --url <origin>` checks and opens
an existing gateway. Neither mode starts or replaces a runtime. Native storage lives in the
fresh `runtime-v4` namespace. Retired configuration, protocols and stores are not
compatibility inputs and remain untouched.

## Development references

- [Frontend validation](frontend.md#development-and-validation) and the [redesign workflow](../.github/workflows/backend-redesign.yml) define supported client gates.
- [Native CLI disposition](backend-native-cli-disposition.md) maps removed legacy fixtures to their native replacements and deliberate contract changes.
- [Package boundaries](frontend.md#packages-and-dependency-direction) identify which package owns a frontend change.
- [Public documentation source](../apps/docs/src/content/docs) contains the user-facing site; draft pages are intentionally not claims of completed documentation.

Older specialized guides are being reconciled as part of final cutover. They
must not override the native backend contract or frontend guide. Historical plans
and learning notes preserve research; they are not current implementation rules.

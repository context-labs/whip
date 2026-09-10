# Formal study freeze — 2026-09-10

Frozen before the first formal provider request. The four paid, off-manifest
pilot attempts all passed their task verifiers. Observed catalog-priced usage
was $0.539523; one interrupted QuickJS child call has unknown usage. The study
retains $1.852592 of pilot cost/exposure, including a full $1.50 reservation for
that uncertain trial. Pilot results do not enter formal scores.

The terminal pilot consumed 86,117 reported input tokens in Starlark and 107,554
in QuickJS. Both included a verifying child; QuickJS's root deleted its child
after receiving the verification message while another child provider call was
active. That produced genuine uncertainty and a later failed root mailbox turn.
The formal study retains the same production behavior and increases the common
tree token cap to 500,000 and root round cap to 60 for the harder repository
tasks. The $2.50 cost cap and 900-second agent deadline remain hard admission
controls; uncertain outcomes retain their reserved exposure.

## Design

- Four upstream tasks: Terminal-Bench 2 `regex-log`,
  `openssl-selfsigned-cert`; DeepSWE `anko-typed-variable-bindings`,
  `httpx-multipart-response-parsing`.
- Revisions: Terminal `2fd12b88aafdd04a52c298e3940bcb189f9766d6`;
  DeepSWE `435ee89ec2f2e2289f33b0da4f992f0b7b7266b9`.
- Two repetitions × two engines × four tasks = 16 trials, run serially.
  Seed 20260910 chooses balanced initial engine order and shuffled task order;
  the second repetition reverses each task's engine order.
- Same Whip binary SHA-256
  `d36c04ce815a30d5a4f659fef4672cd263fd62407f3d896db8e15b6309c8f524`.
  Source plus embedded assets reproduce that exact binary.
- Kimi K3 on inference.net, high reasoning effort, 8,192 maximum output tokens,
  temperature/top-p omitted (provider defaults), four workers, one active host
  call per cell in both engines, fresh home/tree/workspace per trial.
- Total formal cap $40; pilot cap $6; no automatic retries. Original verifier
  results remain authoritative even when runtime/accounting status is failed.
  No failed attempts are silently excluded or repaired by the evaluator.
- Local Docker/OrbStack on an Apple M4 Max; Linux amd64 task execution uses
  Rosetta emulation. Native microbenchmarks use Darwin/arm64 and are separate.
  The desktop has unrelated workloads; retained load data documents this limit.

## Instrumentation changes after pilot

The observer now exports root messages from canonical `messages` and descendant
messages from `transcript_messages`. Pilot backups already retain both tables;
their original exports and raw results remain untouched. Event-based cell
analysis retains deleted-child execution evidence. The observer also classifies
a later failed root turn as `agent_error` even if the initial CLI exited zero.

The DeepSWE images lack ripgrep and prohibit agent internet access. The adapter
therefore uploads the same checksum-verified static ripgrep 14.1.1 binary to all
formal environments. It runs in both original images with networking disabled.
The release archive checksum and licenses are retained; the formal recipe pins
its binary hash. This is environment setup, with no solution edits or broader
agent network permission.

## Analysis

Report every verifier outcome, runtime outcome, accounting completeness, and
paired task/repetition result. Compare all-trial and successful-trial time/cost,
token/cache counts, cell errors, and checkpoint sizes; show uncertain costs
separately from observed estimates. Do not replace missing usage with zero.

This small selected sample supports descriptive comparisons only. Repeated
attempts are clustered within four tasks, not 16 independent tasks. Any interval
must resample complete task clusters; broad superiority and default-switch claims
are out of scope. Both runtimes remain supported; Starlark stays the default.

This is an adapted local Harbor/Pier study, not an official Frontier score.
Exact task bytes, ordered trial recipe, harness hashes, model catalog, raw
verifiers, events, call ledger and checkpoints are retained with the results.

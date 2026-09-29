# Native chat-polish acceptance — 2026-09-29

`chat-polish.mjs` retains its public entrypoint and now runs
`native-chat-polish.mjs`. Its fixture uses the real native runtime, SDK, engine,
mail admission and scoped history. The HTTP provider supplies bounded replies
and explicit holds; it does not create cells, mail projections or UI events.

The preserved workflows are:

- An actual child sends next-turn mail; the root executes one real cell. Both
  appear in the same recorded activity group, with authored reply preserved and
  exact mail text behind explicit keyboard-accessible disclosure.
- Queued follow-up mail runs a separate turn with zero cells. Its group says
  Agent updates and invents no execution/outcome.
- The Open in REPL action fits its text. User action spacing stays compact at
  530/390/320px, keyboard focus reveals Copy, document width does not overflow,
  and a touch context keeps the action visible with a 44px target.
- More than 6000 characters of actual streamed reasoning retain one group
  identity and readable tail without overflow. A held child-only stream does not
  become root work. Root file/tool operations retain their exact activity group
  through canonical tool settlement.
- Active prose reserves no copy footer. Additional prose and tool completion do
  not falsely finish the response; only the settled turn exposes Copy response,
  which remains in the viewport at Latest.

Two real presentation regressions were fixed. The mixed group omitted its agent
updates label; a focused test failed before the correction and passes after it.
The native REPL button stretched to its detail column; the retained browser
width assertion failed before an explicit start alignment restored its fit.

The initial fixture experiment completed its real mail/cell turn but its cleanup
used the obsolete client.close method; the native stateless client needs no such
method. The first browser attempt also used an ambiguous execution selector;
it now names the step rather than matching both header and step. Neither failure
was reclassified as product evidence.

Final Chromium and Firefox runs pass four workflow groups each, with zero page
errors and CSP violations. Both verify production renderer digest
`3d17e57542f5d46b311e810f39b64aaad2407a76ba0f95c6db4137b68bfd98c8`.
Artifacts: `/tmp/whip-native-chat-polish-final/results.json` and screenshots in
that directory. App type checking and 30 timeline/activity tests pass. This is
actual Chromium/Firefox evidence, not Safari or staged Desktop acceptance.

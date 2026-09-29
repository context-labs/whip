import { useRef, useState } from "react";
import type { DurableCommand, Operations } from "@whip/sdk";
import { useRuntime } from "../context";
import { Action, Section, type InspectorProps } from "./shared";

type Reload = Operations["sessions.reload"]["result"];
export function SessionReload(props: InspectorProps) {
  const runtime = useRuntime();
  const [command, setCommand] = useState<DurableCommand<"sessions.reload">>();
  const [result, setResult] = useState<Reload>();
  const inFlight = useRef(false);
  const [busy, setBusy] = useState(false);
  async function perform(action: () => Promise<void>) {
    if (inFlight.current) return;
    inFlight.current = true;
    setBusy(true);
    try {
      await action();
    } finally {
      inFlight.current = false;
      setBusy(false);
    }
  }
  const root = props.session.id === props.rootId;
  async function check() {
    if (!command) return;
    const found = await command.check();
    if (found.state !== "found" || !("host_revision" in found.evidence))
      throw new Error(
        "The exact reload outcome is not available. Inspect Saved commands in Settings before trying again.",
      );
    setResult(found.evidence);
    if (found.evidence.state === "applied") {
      await command.forget();
      await props.view.refresh();
    }
  }
  return (
    <Section
      title="Reload session defaults"
      description="Capture current host defaults and apply them at the next idle tree boundary. Explicit session and definition choices stay in place; history, model, permissions and running work are preserved."
    >
      {!root && <p>Request a reload from the root agent.</p>}
      <Action
        recoverable
        successLabel="Request accepted"
        disabled={
          busy ||
          !props.connected ||
          !root ||
          (!!command && (!result || result.state === "pending"))
        }
        run={() =>
          perform(async () => {
            const next = runtime.command(props.client, "sessions.reload", {
              session_id: props.session.id,
              edit_id: crypto.randomUUID(),
              expected_revision: props.selected.config_revision,
            });
            setCommand(next);
            setResult(undefined);
            const accepted = await runtime.run(next, "Reload session defaults");
            setResult(accepted);
            if (accepted.state === "applied") await props.view.refresh();
          })
        }
      >
        Reload session defaults
      </Action>
      {result && (
        <p role="status">
          Reload {result.state}
          {result.revision
            ? ` · configuration revision ${result.revision}`
            : ""}
          {result.state === "pending"
            ? " · waiting for an idle tree boundary"
            : ""}
          .
        </p>
      )}
      {command && (
        <Action
          successLabel="Receipt checked"
          disabled={busy || !props.connected}
          run={() => perform(check)}
        >
          Check reload outcome
        </Action>
      )}
      {command && result?.state === "pending" && (
        <Action
          successLabel="Cancellation checked"
          disabled={busy || !props.connected}
          run={() =>
            perform(async () => {
              await props.session.reloads.cancel(command.params.edit_id);
              await check();
            })
          }
        >
          Cancel pending reload
        </Action>
      )}
      <p>
        Pending and uncertain requests remain in Settings → Saved commands,
        where you can check or cancel them after reopening the app. A request
        acknowledgement does not mean the reload has applied.
      </p>
    </Section>
  );
}

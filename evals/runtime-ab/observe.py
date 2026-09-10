#!/usr/bin/env python3
"""Run the real Whip CLI and observe its whole tree in a disposable task container.

This is evaluation instrumentation, not an agent loop. Read-only transactions
observe the daemon's canonical SQLite rows; no benchmark code mutates the store.
The runner supplies a fresh WHIP_HOME and retains the daemon until verification.
"""

import argparse
import datetime
from decimal import Decimal
import errno
import fcntl
import hashlib
import json
import os
from pathlib import Path
import signal
import sqlite3
import stat
import subprocess
import time
import urllib.request


def atomic_json(path, value):
    path = Path(path)
    temporary = path.with_suffix(path.suffix + ".tmp")
    with temporary.open("w") as output:
        json.dump(value, output, indent=2, sort_keys=True)
        output.write("\n")
        output.flush()
        os.fsync(output.fileno())
    temporary.replace(path)


def decode(value):
    if isinstance(value, bytes):
        value = value.decode("utf-8")
    if isinstance(value, str):
        try:
            return json.loads(value)
        except (ValueError, TypeError):
            pass
    return value


def rows(db, query, args=()):
    return [{key: decode(value) for key, value in dict(row).items()}
            for row in db.execute(query, args)]


def export_content_bodies(home, evidence, root_id):
    """Copy scoped immutable bodies once, using the final exported DB as index."""
    home, evidence = Path(home).resolve(), Path(evidence).resolve()
    # session.Open constructs its content store beside runtime-v2/sessions.db.
    source_dir = home / "runtime-v2" / "artifacts" / "sha256"
    if source_dir.resolve() != source_dir:
        raise ValueError("content source directory must not escape the trial home")
    target_dir = evidence / "content" / "sha256"
    if target_dir.resolve() != target_dir:
        raise ValueError("content evidence directory must not escape the evidence root")
    target_dir.mkdir(parents=True, exist_ok=True)
    manifest = {"root_id": root_id, "bodies": [], "errors": []}
    database = evidence / "sessions.db"
    with sqlite3.connect(database.as_uri() + "?mode=ro&immutable=1", uri=True) as db:
        roots = db.execute("SELECT id FROM sessions").fetchall()
        if roots != [(root_id,)]:
            raise ValueError("content export requires the trial's single-root snapshot")
        bodies = db.execute("SELECT DISTINCT r.digest,r.size,o.size FROM content_references r JOIN content_objects o ON o.digest=r.digest JOIN content_grants g ON g.reference_id=r.id WHERE g.root_id=?", (root_id,)).fetchall()
    for digest, size, object_size in bodies:
        if not isinstance(digest, str) or len(digest) != 64 or any(c not in "0123456789abcdef" for c in digest) or not isinstance(size, int) or size < 0 or object_size != size:
            manifest["errors"].append({"reason": "invalid content identity"})
            continue
        temporary = target_dir / (digest + ".tmp")
        try:
            descriptor = os.open(source_dir / digest, os.O_RDONLY | os.O_NOFOLLOW)
            with os.fdopen(descriptor, "rb") as source:
                metadata = os.fstat(source.fileno())
                if not stat.S_ISREG(metadata.st_mode) or metadata.st_size != size:
                    raise ValueError("content source size/type mismatch")
                sha, copied = hashlib.sha256(), 0
                with temporary.open("xb") as target:
                    for chunk in iter(lambda: source.read(1024 * 1024), b""):
                        copied += len(chunk)
                        if copied > size:
                            raise ValueError("content source exceeds its recorded size")
                        sha.update(chunk)
                        target.write(chunk)
                    target.flush()
                    os.fsync(target.fileno())
                if copied != size or sha.hexdigest() != digest:
                    raise ValueError("content source digest/size mismatch")
            temporary.replace(target_dir / digest)
            manifest["bodies"].append({"digest": digest, "bytes": copied})
        except (OSError, ValueError) as error:
            temporary.unlink(missing_ok=True)
            manifest["errors"].append({"digest": digest, "reason": type(error).__name__})
    manifest["bytes"] = sum(body["bytes"] for body in manifest["bodies"])
    atomic_json(evidence / "content-export.json", manifest)
    if manifest["errors"]:
        raise ValueError("content body export incomplete: " + str(len(manifest["errors"])) + " failures")
    return manifest


def snapshot(database, after=0):
    """One consistent read transaction, with the same readiness rules as Whip."""
    with sqlite3.connect(database.as_uri() + "?mode=ro", uri=True, timeout=5) as db:
        db.row_factory = sqlite3.Row
        db.execute("BEGIN")
        roots = rows(db, "SELECT * FROM sessions")
        if len(roots) != 1:
            raise ValueError("each benchmark home must contain exactly one session")
        root = roots[0]
        root_id = root["id"]
        agents = rows(db, "SELECT * FROM agents WHERE root_id=?", (root_id,))
        turns = rows(db, "SELECT rowid AS row_number,* FROM turns WHERE root_id=? ORDER BY rowid", (root_id,))
        calls = rows(db, "SELECT * FROM model_calls WHERE root_id=? ORDER BY rowid", (root_id,))
        inbox = rows(db, "SELECT * FROM inbox WHERE root_id=? AND status IN ('queued','running')", (root_id,))
        mail = rows(db, "SELECT id,recipient_agent_id,delivery,status,available_at FROM agent_messages WHERE root_id=? AND status='pending'", (root_id,))
        latest = {turn["agent_id"]: turn for turn in turns}
        terminal_agents = {a["id"] for a in agents if a["status"] in ("stopped", "deleted", "failed", "cancelled")}
        now = datetime.datetime.now(datetime.timezone.utc).isoformat(timespec="seconds").replace("+00:00", "Z")
        runnable_mail = [m for m in mail if m["delivery"] != "next_turn" and m["recipient_agent_id"] not in terminal_agents
                         and (not m["available_at"] or m["available_at"] <= now)
                         and not (m["recipient_agent_id"] == root_id
                                  and latest.get(root_id, {}).get("status") in ("failed", "cancelled", "interrupted"))]
        active_turns = [t for t in turns if t["status"] == "running"]
        pending_calls = [c for c in calls if c["status"] == "running"]
        events = rows(db, "SELECT * FROM events WHERE root_id=? AND seq>? ORDER BY seq", (root_id, after))
        budgets = rows(db, "SELECT * FROM budgets WHERE root_id=?", (root_id,))
        # Match session.transcriptSource: root history lives in messages;
        # descendants use transcript_messages. Preserve per-agent raw sequence.
        transcripts = rows(db, """SELECT session_id AS root_id,session_id AS agent_id,seq,role,content
            FROM messages WHERE session_id=?
            UNION ALL
            SELECT root_id,agent_id,seq,role,content FROM transcript_messages
            WHERE root_id=? AND agent_id<>?
            ORDER BY agent_id,seq""", (root_id, root_id, root_id))
        tables = {r[0] for r in db.execute("SELECT name FROM sqlite_master WHERE type='table'")}
        checkpoints = rows(db, "SELECT root_id,agent_id,envelope,bytes,updated_at FROM agent_checkpoints WHERE root_id=?", (root_id,)) if "agent_checkpoints" in tables else []
        schedules = rows(db, "SELECT * FROM schedules WHERE session_id=?", (root_id,)) if "schedules" in tables else []
        operations = rows(db, "SELECT * FROM operations WHERE root_id=? AND status IN ('queued','running','waiting')", (root_id,)) if "operations" in tables else []
        permissions = rows(db, "SELECT * FROM permission_requests WHERE root_id=? AND status='pending'", (root_id,)) if "permission_requests" in tables else []
        # Retained future schedules and deliberately deferred mail do not prevent
        # task finality. Record them; the container is removed after verification.
        pending = {"active_turns": len(active_turns), "queued_or_running_inputs": len(inbox),
                   "active_operations": len(operations), "pending_permissions": len(permissions),
                   "runnable_mail": len(runnable_mail), "pending_model_calls": len(pending_calls)}
        return {"root": root, "agents": agents, "turns": turns, "calls": calls,
                "budgets": budgets, "transcripts": transcripts, "checkpoints": checkpoints,
                "schedules": schedules, "pending_mail": mail,
                "active_operations": operations, "pending_permissions": permissions,
                "deferred_or_blocked_mail": len(mail) - len(runnable_mail),
                "pending": pending, "events": events,
                "settled": bool(turns) and not any(pending.values())}


def aggregate(calls):
    totals = {"input_tokens": 0, "output_tokens": 0, "cache_tokens": 0,
              "reported_cost_usd": 0.0, "normalized_cost_usd": 0.0, "ledger_cost_usd": 0.0,
              "reported_cost_calls": 0, "unknown_normalized_cost_calls": 0,
              "unknown_usage_calls": 0, "unknown_cost_calls": 0, "pending_calls": 0,
              "model_calls": len(calls), "peak_input_tokens": 0}
    routes = {}
    for call in calls:
        attempt = call["attempt"]
        route = (attempt["Provider"], attempt["Model"], attempt["Purpose"])
        key = "/".join(route)
        routes[key] = routes.get(key, 0) + 1
        if call["status"] == "running":
            totals["pending_calls"] += 1
        usage = (call.get("result") or {}).get("Usage", {})
        dispatched = (call.get("result") or {}).get("Dispatched", call["status"] != "rejected")
        if dispatched and call["usage_source"] != "reported":
            totals["unknown_usage_calls"] += 1
        input_tokens = usage.get("prompt_tokens", 0)
        output_tokens = usage.get("completion_tokens", 0)
        cached = (usage.get("prompt_tokens_details") or {}).get("cached_tokens", 0)
        totals["input_tokens"] += input_tokens
        totals["output_tokens"] += output_tokens
        totals["cache_tokens"] += cached
        totals["peak_input_tokens"] = max(totals["peak_input_tokens"], input_tokens)
        if usage.get("cost") is not None:
            totals["reported_cost_usd"] += usage["cost"]
            totals["reported_cost_calls"] += 1
        if dispatched and call["cost_source"] not in ("reported", "estimated"):
            totals["unknown_cost_calls"] += 1
        totals["ledger_cost_usd"] += call["cost_micros"] / 1_000_000
        rates = attempt.get("Pricing", {})
        if call["usage_source"] == "reported" and rates.get("prompt") is not None and rates.get("completion") is not None:
            cache_rate = rates.get("input_cache_read")
            if cache_rate is None:
                cache_rate = rates["prompt"]
            totals["normalized_cost_usd"] += float(
                Decimal(str(rates["prompt"])) * (input_tokens - cached)
                + Decimal(str(cache_rate)) * cached
                + Decimal(str(rates["completion"])) * output_tokens)
        elif dispatched:
            totals["unknown_normalized_cost_calls"] += 1
    totals["routes"] = routes
    return totals


def final_accounting_complete(metrics, outcome):
    return bool(metrics and outcome and outcome.get("final_snapshot")
                and not outcome.get("evidence_errors")
                and (outcome.get("frozen_daemon_pid") or outcome.get("daemon_stopped"))
                and outcome.get("pending") is not None and not any(outcome["pending"].values())
                and metrics.get("unknown_cost_calls") == 0 and metrics.get("pending_calls") == 0)


def quiet_start(previous, *, exited, settled, changed, at):
    """New durable activity restarts the finality window, even between polls."""
    if not exited or not settled:
        return None
    return at if previous is None or changed else previous


def settled_outcome(state, cli_exit_code):
    """The CLI observes one root turn; later mailbox turns can still fail."""
    unsuccessful = ("failed", "cancelled", "interrupted")
    root_id = state["root"]["id"]
    root_turns = [turn for turn in state["turns"] if turn["agent_id"] == root_id]
    latest_root = root_turns[-1]["status"] if root_turns else None
    return {"status": "agent_error" if cli_exit_code != 0 or latest_root in unsuccessful else "completed",
            "latest_root_turn_status": latest_root,
            "failed_or_interrupted_turns": sum(turn["status"] in unsuccessful for turn in state["turns"])}


def signal_daemon(home, binary, sig):
    """Signal only the verified daemon in this disposable Linux trial home."""
    with (home / "runtime-v2" / "daemon.lock").open() as lock:
        try:
            fcntl.flock(lock, fcntl.LOCK_SH | fcntl.LOCK_NB)
        except BlockingIOError:
            pass
        else:
            raise RuntimeError("trial daemon no longer owns its lock")
        pid = int(lock.read().strip())
        if pid <= 1:
            raise RuntimeError("invalid trial daemon PID")
        process = Path("/proc") / str(pid)
        start_time = (process / "stat").read_text().rsplit(")", 1)[1].split()[19]
        descriptor = None
        method = "pidfd"
        rosetta = False
        try:
            try:
                descriptor = os.pidfd_open(pid)
            except OSError as error:
                if error.errno != errno.ENOSYS:
                    raise
            executable = Path(binary).resolve()
            interpreter = (process / "exe").resolve()
            if interpreter != executable:
                command = (process / "cmdline").read_bytes().split(b"\0")
                mappings = [line.split(maxsplit=5) for line in (process / "maps").read_text().splitlines()]
                mapped = any(len(parts) == 6 and "x" in parts[1] and parts[5] == str(executable)
                             and int(parts[4]) == executable.stat().st_ino for parts in mappings)
                rosetta = (str(interpreter) == "/mnt/rv/[rosetta]" and mapped
                           and command[:2] == [os.fsencode(str(executable)), b"_daemon"])
                if not rosetta:
                    raise RuntimeError("trial daemon executable differs")
            expected_home = b"WHIP_HOME=" + os.fsencode(str(home))
            if expected_home not in (process / "environ").read_bytes().split(b"\0"):
                raise RuntimeError("trial daemon home differs")
            if descriptor is not None:
                try:
                    signal.pidfd_send_signal(descriptor, sig)
                except OSError as error:
                    if error.errno != errno.ENOSYS:
                        raise
                    os.close(descriptor)
                    descriptor = None
            if descriptor is None:
                # Docker's amd64 emulation may not implement pidfds. The fresh
                # owned container, held lock, executable, home, and unchanged
                # process start time constrain the fallback to this daemon.
                if (process / "stat").read_text().rsplit(")", 1)[1].split()[19] != start_time:
                    raise RuntimeError("trial daemon PID was reused")
                os.kill(pid, sig)
                method = "verified-pid"
        finally:
            if descriptor is not None:
                os.close(descriptor)
    return pid, method + ("-rosetta" if rosetta else "")


def freeze_daemon(home, binary):
    pid, method = signal_daemon(home, binary, signal.SIGSTOP)
    try:
        deadline = time.monotonic() + 2
        while time.monotonic() < deadline:
            state = (Path("/proc") / str(pid) / "stat").read_text().rsplit(")", 1)[1].split()[0]
            if state == "T":
                return pid, method
            time.sleep(0.01)
        raise RuntimeError("trial daemon did not enter stopped state")
    except BaseException:
        signal_daemon(home, binary, signal.SIGCONT)
        raise


def process_sample():
    """Aggregate container process RSS; include shared pages once per process."""
    rss = 0
    cpu = {}
    ticks = os.sysconf("SC_CLK_TCK")
    page = os.sysconf("SC_PAGE_SIZE")
    for path in Path("/proc").glob("[0-9]*/stat"):
        try:
            fields = path.read_text().rsplit(")", 1)[1].split()
            identity = path.parent.name + ":" + fields[19]
            cpu[identity] = (int(fields[11]) + int(fields[12])) / ticks
            rss += int(fields[21]) * page
        except (OSError, ValueError, IndexError):
            continue
    return rss, cpu


def write_config(home, engine, max_output, base_url="https://api.inference.net/v1", native_defaults=False):
    home.mkdir(parents=True, exist_ok=False)
    configuration = {
        "defaultModel": "kimi-k3", "defaultProvider": "inference-net", "defaultEffort": "high",
        "compactModel": "kimi-k3", "compactProvider": "inference-net",
        "providers": {"inference-net": {"name": "Inference.net", "baseUrl": base_url,
                                        "api": "openai-completions", "apiKeyEnv": "INFERENCE_API_KEY"}},
        "models": {"kimi-k3": {"providers": ["inference-net"], "maxOut": max_output, "context": 1048576}},
        "rlm": {"defaultEngine": engine, "maxWorkers": 4, "memoryMiB": 256, "hostRequests": 1024,
                "maxConcurrentHostCalls": 1},
        "browser": {"enabled": False}, "computer": {"enabled": False},
        "mcpImport": {"claude": {"enabled": False}, "codex": {"enabled": False}},
    }
    if native_defaults:
        configuration["rlm"] = {"defaultEngine": engine}
    atomic_json(home / "config.json", configuration)
    return configuration


def discover_catalog(home, evidence, base_url, key):
    request = urllib.request.Request(base_url + "/models", headers={
        "Authorization": "Bearer " + key, "User-Agent": "whip-runtime-benchmark/1.0"})
    with urllib.request.urlopen(request, timeout=30) as response:
        payload = json.load(response)
    models = [m for m in payload["data"] if m["id"] == "kimi-k3"]
    if len(models) != 1:
        raise ValueError("requested kimi-k3 route unavailable")
    model = models[0]
    atomic_json(evidence / "provider-catalog.json", model)
    if "high" not in model.get("reasoning_efforts", []):
        raise ValueError("provider no longer advertises high effort")
    atomic_json(home / "models.json", {"inference-net": {
        "discoveryVersion": 1, "baseUrl": base_url,
        "fetchedAt": datetime.datetime.now(datetime.timezone.utc).isoformat().replace("+00:00", "Z"),
        "models": [{"id": model["id"], "contextLength": model["context_length"],
                    "maxCompletionTokens": model["max_completion_tokens"],
                    "reasoningEfforts": model["reasoning_efforts"], "pricing": model["pricing"]}]}})


def run(args):
    evidence = Path(args.evidence).resolve()
    evidence.mkdir(parents=True, exist_ok=True)
    home = Path(args.home).resolve()
    base_url = "https://api.inference.net/v1"
    if args.fixture:
        from fixture_provider import start
        fixture_server, base_url = start(args.engine)
    config = write_config(home, args.engine, args.max_output, base_url, getattr(args, "native_defaults", False))
    discover_catalog(home, evidence, base_url, os.environ["INFERENCE_API_KEY"])
    atomic_json(evidence / "configuration.json", config)
    env = dict(os.environ, WHIP_HOME=str(home))
    prompt = Path(args.instruction).read_text()
    if args.commit:
        prompt += "\n\nCommit your final changes in this disposable task repository so the evaluator can collect git diff BASE HEAD."
    command = [args.binary, "run", "--format", "json", "--quiet", "--rlm-engine", args.engine,
               "--permission-mode", "automatic", "--max-cost", str(args.max_cost),
               "--max-tokens", str(args.max_tokens), "--max-turns", str(args.max_turns),
               "--timeout", str(args.timeout) + "s", "-m", "kimi-k3", "-p", "inference-net"]
    binary_hash = hashlib.sha256(Path(args.binary).read_bytes()).hexdigest()
    atomic_json(evidence / "identity.json", {"binary_sha256": binary_hash, "engine": args.engine,
                "model": "kimi-k3", "provider": "inference-net", "reasoning_effort": "high",
                "instruction_sha256": hashlib.sha256(prompt.encode()).hexdigest(),
                "command": command, "live_provider": not args.fixture, "provider_endpoint": base_url,
                "finality_policy": "CLI exited plus tree settled with no events for 2 seconds; verified daemon SIGSTOP and settled readback before grading",
                "future_schedule_policy": "record and end; no future wakeups after task finality"})
    started = time.monotonic()
    database = home / "runtime-v2" / "sessions.db"
    last = None
    cursor = 0
    quiet_since = None
    status = "timeout"
    peak_rss = 0
    cpu_seen = {}
    frozen_pid = None
    daemon_signal_method = None
    export_errors = []
    content_export_errors = []
    content_export_complete = False
    final_snapshot = False
    daemon_stopped = False
    with (evidence / "cli.ndjson").open("w") as stdout, (evidence / "cli.stderr").open("w") as stderr, (evidence / "events.ndjson").open("w") as events:
        process = subprocess.Popen(command, stdin=subprocess.PIPE, stdout=stdout, stderr=stderr, env=env, start_new_session=True)
        try:
            process.stdin.write(prompt.encode())
            process.stdin.close()
            while time.monotonic() - started < args.timeout:
                rss, cpu = process_sample()
                peak_rss = max(peak_rss, rss)
                for pid, seconds in cpu.items():
                    cpu_seen[pid] = max(cpu_seen.get(pid, 0), seconds)
                if database.exists():
                    current = None
                    try:
                        current = snapshot(database, cursor)
                    except (sqlite3.OperationalError, ValueError):
                        quiet_since = None
                        if last is None and process.poll() is not None and time.monotonic() - started > 10:
                            raise
                    if current:
                        last = current
                        previous_cursor = cursor
                        for event in last.pop("events"):
                            events.write(json.dumps(event) + "\n")
                            cursor = event["seq"]
                        events.flush()
                        metrics = aggregate(last["calls"])
                        metrics.update({"duration_seconds": time.monotonic() - started,
                                        "engine": args.engine, "pending": last["pending"], "event_cursor": cursor,
                                        "sampled_peak_container_rss_bytes": peak_rss,
                                        "sampled_container_cpu_seconds": sum(cpu_seen.values())})
                        atomic_json(evidence / "metrics.json", metrics)
                        atomic_json(evidence / "state.json", last)
                        quiet_since = quiet_start(quiet_since, exited=process.poll() is not None,
                                                  settled=last["settled"], changed=cursor != previous_cursor,
                                                  at=time.monotonic())
                        if quiet_since is not None:
                            if time.monotonic() - quiet_since >= 2:
                                frozen_pid, daemon_signal_method = freeze_daemon(home, args.binary)
                                try:
                                    frozen = snapshot(database, cursor)
                                    if frozen["settled"] and not frozen["events"]:
                                        status = settled_outcome(frozen, process.returncode)["status"]
                                        break
                                finally:
                                    if status == "timeout":
                                        signal_daemon(home, args.binary, signal.SIGCONT)
                                        frozen_pid = None
                                quiet_since = None
                if process.poll() is not None and last is None and time.monotonic() - started > 10:
                    status = "startup_error"
                    break
                time.sleep(0.25)
        except Exception as error:
            status = "observer_error"
            export_errors.append(type(error).__name__ + ": " + str(error))
        finally:
            agent_duration = time.monotonic() - started
            if process.poll() is None:
                os.killpg(process.pid, signal.SIGTERM)
                try:
                    process.wait(timeout=8)
                except subprocess.TimeoutExpired:
                    os.killpg(process.pid, signal.SIGKILL)
                    process.wait(timeout=5)
            # Preserve task services through grading on a settled run. On timeout
            # stop all daemon-owned children to freeze the attempted solution.
            if status in ("timeout", "observer_error"):
                try:
                    if frozen_pid is not None:
                        signal_daemon(home, args.binary, signal.SIGCONT)
                        frozen_pid = None
                    stopped = subprocess.run([args.binary, "daemon", "stop"], env=env, stdout=stderr, stderr=stderr, timeout=20, check=False)
                    if stopped.returncode:
                        export_errors.append("daemon stop exited " + str(stopped.returncode))
                    else:
                        daemon_stopped = True
                except Exception as error:
                    export_errors.append("daemon stop: " + str(error))
            try:
                if database.exists():
                    last = snapshot(database, cursor)
                    for event in last.pop("events"):
                        events.write(json.dumps(event) + "\n")
                        cursor = event["seq"]
                    events.flush()
                    atomic_json(evidence / "state.json", last)
                    metrics = aggregate(last["calls"])
                    metrics.update({"duration_seconds": time.monotonic() - started, "engine": args.engine,
                                    "pending": last["pending"], "event_cursor": cursor,
                                    "sampled_peak_container_rss_bytes": peak_rss,
                                    "sampled_container_cpu_seconds": sum(cpu_seen.values())})
                    atomic_json(evidence / "metrics.json", metrics)
                    with sqlite3.connect(database.as_uri() + "?mode=ro", uri=True) as source, sqlite3.connect(evidence / "sessions.db") as destination:
                        source.backup(destination)
                    if frozen_pid is None and not daemon_stopped:
                        raise ValueError("content export requires a frozen or stopped trial daemon")
                    final_snapshot = True
                    try:
                        export_content_bodies(home, evidence, last["root"]["id"])
                        content_export_complete = True
                    except (OSError, sqlite3.Error, ValueError, KeyError) as error:
                        content_export_errors.append(str(error))
            except (OSError, sqlite3.Error, ValueError, KeyError) as error:
                export_errors.append("final export: " + str(error))
            turn_outcome = settled_outcome(last, process.returncode) if last else {
                "latest_root_turn_status": None, "failed_or_interrupted_turns": None}
            outcome = {**turn_outcome, "status": status, "cli_exit_code": process.returncode,
                       "duration_seconds": time.monotonic() - started, "event_cursor": cursor,
                       "agent_duration_seconds": agent_duration,
                       "engine": args.engine, "pending": last["pending"] if last else None,
                       "frozen_daemon_pid": frozen_pid, "final_snapshot": final_snapshot,
                       "daemon_stopped": daemon_stopped,
                       "daemon_signal_method": daemon_signal_method,
                       "content_export_complete": content_export_complete,
                       "content_export_errors": content_export_errors,
                       "evidence_errors": export_errors}
            atomic_json(evidence / "outcome.json", outcome)
    return outcome


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--binary", default="/opt/whip/whip")
    parser.add_argument("--engine", choices=["starlark", "quickjs"], required=True)
    parser.add_argument("--home", default="/tmp/whip-eval-home")
    parser.add_argument("--instruction", required=True)
    parser.add_argument("--evidence", default="/logs/agent/whip")
    parser.add_argument("--timeout", type=int, default=600)
    parser.add_argument("--max-cost", type=float, default=2.5)
    parser.add_argument("--max-tokens", type=int, default=120000)
    parser.add_argument("--max-output", type=int, default=8192)
    parser.add_argument("--max-turns", type=int, default=30)
    parser.add_argument("--commit", action="store_true")
    parser.add_argument("--fixture", action="store_true")
    parser.add_argument("--native-defaults", action="store_true")
    print(json.dumps(run(parser.parse_args())))

#!/usr/bin/env python3
"""Run the real Whip CLI and observe its whole tree in a disposable task container.

This is evaluation instrumentation, not an agent loop. Read-only transactions
observe the daemon's canonical SQLite rows; no benchmark code mutates the store.
The runner supplies a fresh WHIPCODE_HOME and retains the daemon until verification.
"""

import argparse
import datetime
import errno
import fcntl
import hashlib
import json
import os
from pathlib import Path
from decimal import Decimal
import signal
import socket
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
    # The native runtime stores immutable bytes beside runtime-v4/state.db.
    source_dir = home / "runtime-v4" / "artifacts" / "sha256"
    if source_dir.resolve() != source_dir:
        raise ValueError("content source directory must not escape the trial home")
    target_dir = evidence / "content" / "sha256"
    if target_dir.resolve() != target_dir:
        raise ValueError("content evidence directory must not escape the evidence root")
    target_dir.mkdir(parents=True, exist_ok=True)
    manifest = {"root_id": root_id, "bodies": [], "errors": []}
    database = evidence / "sessions.db"
    with sqlite3.connect(database.as_uri() + "?mode=ro&immutable=1", uri=True) as db:
        roots = db.execute("SELECT id FROM sessions WHERE parent_id IS NULL").fetchall()
        if roots != [(root_id,)]:
            raise ValueError("content export requires the trial's single-root snapshot")
        bodies = db.execute("SELECT DISTINCT r.digest,o.size,o.size FROM content_references r JOIN content_bodies o ON o.digest=r.digest JOIN sessions s ON s.id=r.owner_session_id WHERE s.tree_id=(SELECT tree_id FROM sessions WHERE id=?)", (root_id,)).fetchall()
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


def snapshot(database):
    """One read transaction of the native ledger, including deleted-child spend."""
    with sqlite3.connect(database.as_uri() + "?mode=ro", uri=True, timeout=5) as db:
        db.row_factory = sqlite3.Row
        db.execute("BEGIN")
        trees = rows(db, "SELECT * FROM session_trees")
        roots = rows(db, "SELECT * FROM sessions WHERE parent_id IS NULL")
        if len(trees) != 1 or len(roots) != 1 or roots[0]["tree_id"] != trees[0]["id"]:
            raise ValueError("each benchmark home must contain exactly one native tree")
        root = roots[0]
        sessions = rows(db, "SELECT * FROM sessions WHERE tree_id=? ORDER BY created_at,id", (root["tree_id"],))
        turns = rows(db, "SELECT rowid AS row_number,* FROM turns ORDER BY rowid")
        # Attempt ancestry is immutable and retained after deletion of a child.
        calls = rows(db, "SELECT a.* FROM model_attempts a JOIN attempt_budget_ancestors b ON b.attempt_id=a.id WHERE b.session_id=? ORDER BY a.rowid", (root["id"],))
        budgets = rows(db, "SELECT * FROM budget_limits ORDER BY session_id,kind")
        count = lambda query, args=(): db.execute(query, args).fetchone()[0]
        # Match store.mailReady: automatic delivery waits for a successful latest
        # prompt turn at every depth. Next-turn and future mail are recorded only.
        runnable_mail = count("""SELECT count(*) FROM mail m
            JOIN mail_revisions r ON r.mail_id=m.id AND r.revision=m.revision
            JOIN sessions s ON s.id=m.recipient_id
            WHERE s.lifecycle='active' AND m.state='pending' AND m.deleted_at IS NULL
            AND r.delivery<>'next_turn' AND r.available_at<=?
            AND COALESCE((SELECT t.state FROM turns t LEFT JOIN inputs i ON i.turn_id=t.id
                WHERE t.session_id=m.recipient_id AND COALESCE(i.kind,'prompt')='prompt'
                ORDER BY t.started_at DESC,t.rowid DESC LIMIT 1),'succeeded')='succeeded'""", (time.time_ns(),))
        pending = {
            "active_turns": count("SELECT count(*) FROM turns WHERE finished_at IS NULL"),
            "queued_inputs": count("SELECT count(*) FROM inputs WHERE turn_id IS NULL AND steered_turn_id IS NULL AND cancelled_at IS NULL"),
            "active_operations": count("SELECT count(*) FROM operations WHERE finished_at IS NULL"),
            "pending_permissions": count("SELECT count(*) FROM permissions WHERE state='pending'"),
            "runnable_mail": runnable_mail,
            "pending_model_calls": count("SELECT count(*) FROM model_attempts WHERE finished_at IS NULL"),
            "armed_goals": count("SELECT count(*) FROM goals WHERE state='armed' AND deleted_at IS NULL"),
        }
        return {"evidence_schema": "native-v4", "tree": trees[0], "root": root,
                "sessions": sessions, "turns": turns, "calls": calls, "budgets": budgets,
                "pending": pending, "settled": bool(turns) and not any(pending.values())}


def aggregate(calls):
    """Native attempt accounting; absent per-field usage and cost stay unknown."""
    totals = {"input_tokens": 0, "output_tokens": 0, "cache_tokens": 0,
              "reported_cost_usd": 0.0, "ledger_cost_usd": 0.0, "reported_cost_calls": 0,
              "unknown_usage_calls": 0, "unknown_cache_calls": 0,
              "unknown_cost_calls": 0, "pending_calls": 0,
              "model_calls": len(calls), "dispatched_calls": 0, "peak_input_tokens": 0}
    ledger_nanos, reported_nanos = 0, 0
    for call in calls:
        if call["state"] in ("reserved", "dispatched"):
            totals["pending_calls"] += 1
        result = call.get("result") or {}
        usage = result.get("usage") or {}
        dispatched = call["dispatched_at"] is not None
        totals["dispatched_calls"] += int(dispatched)
        if dispatched and (usage.get("input") is None or usage.get("output") is None):
            totals["unknown_usage_calls"] += 1
        if dispatched and usage.get("cached_input") is None:
            totals["unknown_cache_calls"] += 1
        for target, source in (("input_tokens", "input"), ("output_tokens", "output"), ("cache_tokens", "cached_input")):
            totals[target] += usage.get(source) or 0
        totals["peak_input_tokens"] = max(totals["peak_input_tokens"], usage.get("input") or 0)
        reported = result.get("reported_cost_nano_usd")
        if reported is not None:
            reported_nanos += reported
            totals["reported_cost_calls"] += 1
        if dispatched and call["cost_nano_usd"] is None:
            totals["unknown_cost_calls"] += 1
        ledger_nanos += call["cost_nano_usd"] or 0
    # Exact integers are the audit authority. Float projections are runner API
    # values only; reports compute money from the copied nanodollar ledger.
    totals["ledger_cost_nano_usd"] = ledger_nanos
    totals["reported_cost_nano_usd"] = reported_nanos
    totals["ledger_cost_usd"] = ledger_nanos / 1_000_000_000
    totals["reported_cost_usd"] = reported_nanos / 1_000_000_000
    return totals


def final_accounting_complete(metrics, outcome):
    return bool(metrics and outcome and outcome.get("final_snapshot")
                and not outcome.get("evidence_errors")
                and (outcome.get("frozen_daemon_pid") or outcome.get("daemon_stopped"))
                and outcome.get("pending") is not None and not any(outcome["pending"].values())
                and metrics.get("unknown_usage_calls") == 0
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
    root_turns = [turn for turn in state["turns"] if turn["session_id"] == root_id]
    latest_root = root_turns[-1]["state"] if root_turns else None
    return {"status": "agent_error" if cli_exit_code != 0 or latest_root in unsuccessful else "completed",
            "latest_root_turn_status": latest_root,
            "failed_or_interrupted_turns": sum(turn["state"] in unsuccessful for turn in state["turns"])}


def stat_fields(process):
    """Fields of /proc/<pid>/stat after the command name (index 0 is the state)."""
    return (Path(process) / "stat").read_text().rsplit(")", 1)[1].split()


def signal_daemon(home, binary, sig, identity=None):
    """Signal only the verified daemon in this disposable Linux trial home."""
    lock_path = home / "runtime-v4" / "runtime.lock"
    with os.fdopen(os.open(lock_path, os.O_RDONLY | os.O_NOFOLLOW)) as lock:
        metadata = os.fstat(lock.fileno())
        if not stat.S_ISREG(metadata.st_mode) or metadata.st_uid != os.getuid() or stat.S_IMODE(metadata.st_mode) != 0o600:
            raise RuntimeError("trial runtime lock is not private and owned")
        try:
            fcntl.flock(lock, fcntl.LOCK_SH | fcntl.LOCK_NB)
        except BlockingIOError:
            pass
        else:
            raise RuntimeError("trial daemon no longer owns its lock")
        if identity is None:
            status = json.loads(subprocess.run([binary, "daemon", "status", "--json"],
                env=dict(os.environ, WHIPCODE_HOME=str(home)), capture_output=True,
                check=True, timeout=5).stdout)
            if status["state"] != "running" or status["directory"] != str(home / "runtime-v4"):
                raise RuntimeError("trial native runtime is not ready")
            pid = status["process"]["pid"]
            if not isinstance(pid, int) or isinstance(pid, bool) or pid <= 1:
                raise RuntimeError("invalid trial runtime PID")
            process = Path("/proc") / str(pid)
            start_time = stat_fields(process)[19]
        else:
            # A stopped process cannot answer RPC; reuse only the exact previously
            # verified process identity, never resolve a new runtime for SIGCONT.
            pid, start_time = identity
            process = Path("/proc") / str(pid)
        if stat_fields(process)[19] != start_time:
            raise RuntimeError("trial daemon PID was reused")
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
            command = (process / "cmdline").read_bytes().split(b"\0")
            expected = [os.fsencode(str(executable)), b"_native-runtime"]
            directory_flag = command.index(b"-directory") if b"-directory" in command else -1
            if command[:2] != expected or directory_flag < 0 or command[directory_flag + 1] != os.fsencode(str(home / "runtime-v4")):
                raise RuntimeError("trial native command/directory differs")
            if interpreter != executable:
                mappings = [line.split(maxsplit=5) for line in (process / "maps").read_text().splitlines()]
                mapped = any(len(parts) == 6 and "x" in parts[1] and parts[5] == str(executable)
                             and int(parts[4]) == executable.stat().st_ino for parts in mappings)
                rosetta = (str(interpreter) == "/mnt/rv/[rosetta]" and mapped
                           and command[:2] == expected)
                if not rosetta:
                    raise RuntimeError("trial daemon executable differs")
            expected_home = b"WHIPCODE_HOME=" + os.fsencode(str(home))
            if expected_home not in (process / "environ").read_bytes().split(b"\0"):
                raise RuntimeError("trial daemon home differs")
            owns_descriptor = False
            for file in (process / "fd").iterdir():
                try:
                    opened = file.stat()
                    owns_descriptor |= (opened.st_dev, opened.st_ino) == (metadata.st_dev, metadata.st_ino)
                except FileNotFoundError:
                    continue
            if not owns_descriptor:
                raise RuntimeError("trial process does not hold its native runtime lock")
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
                if stat_fields(process)[19] != start_time:
                    raise RuntimeError("trial daemon PID was reused")
                os.kill(pid, sig)
                method = "verified-pid"
        finally:
            if descriptor is not None:
                os.close(descriptor)
    return pid, method + ("-rosetta" if rosetta else ""), start_time


def freeze_daemon(home, binary):
    pid, method, start_time = signal_daemon(home, binary, signal.SIGSTOP)
    try:
        deadline = time.monotonic() + 2
        while time.monotonic() < deadline:
            state = stat_fields(Path("/proc") / str(pid))[0]
            if state == "T":
                return pid, method, start_time
            time.sleep(0.01)
        raise RuntimeError("trial daemon did not enter stopped state")
    except BaseException:
        signal_daemon(home, binary, signal.SIGCONT, (pid, start_time))
        raise


def process_sample():
    """Aggregate container process RSS; include shared pages once per process."""
    rss = 0
    cpu = {}
    ticks = os.sysconf("SC_CLK_TCK")
    page = os.sysconf("SC_PAGE_SIZE")
    for path in Path("/proc").glob("[0-9]*/stat"):
        try:
            fields = stat_fields(path.parent)
            identity = path.parent.name + ":" + fields[19]
            cpu[identity] = (int(fields[11]) + int(fields[12])) / ticks
            rss += int(fields[21]) * page
        except (OSError, ValueError, IndexError):
            continue
    return rss, cpu


HOST_VERSION = 20


def write_config(home, engine, max_output, base_url="https://api.inference.net/v1", native_defaults=False, model="kimi-k3"):
    """One explicit fresh host; no retired config/catalog or inferred credentials."""
    if engine not in ("starlark", "quickjs"):
        raise ValueError("unknown native engine")
    home.mkdir(parents=True, exist_ok=False, mode=0o700)
    directory = home / "runtime-v4"
    directory.mkdir(mode=0o700)
    configuration = {
        "version": HOST_VERSION, "engine": engine,
        "default_permission_mode": "automatic",
        "providers": {"inference-net": {"kind": "openai-chat", "base_url": base_url,
            "credential_env": "INFERENCE_API_KEY", "credential_source": "env",
            "models": {model: {"max_output_tokens": max_output,
                               "context_window_tokens": None, "prices": {}}}}},
        "defaults": {"model": {"name": model, "provider": "inference-net", "effort": "high",
                                 "temperature": None, "top_p": None}},
    }
    atomic_json(directory / "host.json", configuration)
    (directory / "host.json").chmod(0o600)
    return configuration


def utc_stamp():
    return datetime.datetime.now(datetime.timezone.utc).isoformat(timespec="seconds").replace("+00:00", "Z")


def fetch_models(base_url, key):
    """The provider's model list; the key travels only in this request header."""
    request = urllib.request.Request(base_url + "/models", headers={
        "Authorization": "Bearer " + key, "User-Agent": "whip-evals/0.1.0"})
    with urllib.request.urlopen(request, timeout=30) as response:
        return json.load(response)["data"]


def configure_catalog(configuration, model):
    """Capture exact route limits and price evidence in native host declarations."""
    configuration = json.loads(json.dumps(configuration))
    selection = configuration["defaults"]["model"]
    if selection["name"] != model["id"] or selection["effort"] not in model.get("reasoning_efforts", []):
        raise ValueError("requested model/effort is not advertised")
    route = configuration["providers"][selection["provider"]]["models"][model["id"]]
    context, output = model["context_length"], model["max_completion_tokens"]
    if not isinstance(context, int) or not isinstance(output, int) or not 1 <= output <= min(context, 1_000_000) or context > 1_000_000_000:
        raise ValueError("invalid advertised model limits")
    route["context_window_tokens"] = context
    route["max_output_tokens"] = min(route["max_output_tokens"] or output, output)
    prices = {}
    for field, source in (("input", "prompt"), ("output", "completion"), ("cached_input", "input_cache_read"),
                          ("reasoning", "reasoning"), ("cached_output", "output_cache_write")):
        value = model.get("pricing", {}).get(source)
        if value is None:
            prices[field] = None
            continue
        nanos_per_million = Decimal(str(value)) * 1_000_000_000_000_000
        if not nanos_per_million.is_finite() or nanos_per_million < 0 or nanos_per_million != nanos_per_million.to_integral_value() or nanos_per_million > 2**63 - 1:
            raise ValueError("invalid advertised model price")
        prices[field] = int(nanos_per_million)
    route["prices"] = prices
    return configuration


def discover_catalog(home, evidence, base_url, key, model_id="kimi-k3"):
    models = [m for m in fetch_models(base_url, key) if m["id"] == model_id]
    if len(models) != 1:
        raise ValueError("requested " + model_id + " route unavailable")
    model = models[0]
    atomic_json(evidence / "provider-catalog.json", model)
    path = home / "runtime-v4" / "host.json"
    configuration = configure_catalog(json.loads(path.read_text()), model)
    atomic_json(path, configuration)
    path.chmod(0o600)
    return configuration


# The headless evaluator is the human authority for its disposable workspace.
# Children receive these exact root grants through ordinary delegation; Full
# Access alone deliberately does not bypass native child authority.
WORKSPACE_GRANTS = tuple("files." + name for name in ("list", "search", "read", "write", "patch")) + tuple(
    "shell." + name for name in ("run", "read", "start", "poll", "tail", "wait", "kill", "list"))


def prepare_native_session(binary, home, engine, env):
    subprocess.run([binary, "daemon", "start"], env=env, capture_output=True, check=True, timeout=30)
    status = json.loads(subprocess.run([binary, "daemon", "status", "--json"],
        env=env, capture_output=True, check=True, timeout=5).stdout)
    if status["state"] != "running" or status["directory"] != str(home / "runtime-v4"):
        raise ValueError("trial native runtime is unavailable")
    with socket.socket(socket.AF_UNIX, socket.SOCK_STREAM) as connection:
        connection.settimeout(10)
        connection.connect(status["socket"])
        with connection.makefile("rwb") as stream:
            next_id = 0

            def rpc(method, params):
                nonlocal next_id
                next_id += 1
                ident = str(next_id)
                stream.write(json.dumps({"jsonrpc": "2.0", "id": ident, "method": method, "params": params}).encode() + b"\n")
                stream.flush()
                raw = stream.readline((8 << 20) + 1)
                if not raw.endswith(b"\n") or len(raw) > 8 << 20:
                    raise ValueError("missing or oversized native response")
                message = json.loads(raw)
                if message.get("id") != ident or message.get("error"):
                    raise ValueError("native trial admission refused: " + method)
                return message["result"]

            initialized = rpc("initialize", {"major": 4})
            if initialized["major"] != 4 or initialized["network_client"] or initialized["runtime_id"] != status["process"]["runtime_id"]:
                raise ValueError("native trial identity differs")
            definition = next(ref for ref in initialized["builtins"] if ref["id"] == "coding")
            created = rpc("trees.create", {"creation_id": "eval-root", "definition": definition,
                "working_directory": str(Path.cwd()), "engine": engine, "permission_mode": "automatic",
                "metadata": {"title": None, "archived": False, "pinned": False},
                "overrides": {"automatic_title": False}})
            if created["deleted"] or not created["root"]:
                raise ValueError("native trial root is unavailable")
            root = created["root"]
            for capability in WORKSPACE_GRANTS:
                grant = rpc("grants.create", {"id": "eval-" + capability.replace(".", "-"),
                    "session_id": root["id"], "capability": capability, "resource": str(Path.cwd())})
                if grant["session_id"] != root["id"] or grant["capability"] != capability or grant["resource"] != str(Path.cwd()):
                    raise ValueError("native trial grant scope differs")
            return root["id"]


def _run(args, fixture_url=None):
    evidence = Path(args.evidence).resolve()
    evidence.mkdir(parents=True, exist_ok=True)
    home = Path(args.home).resolve()
    base_url = fixture_url or "https://api.inference.net/v1"
    contract_path = getattr(args, "contract", None)
    if contract_path:
        contract = json.loads(Path(contract_path).read_text())
        if contract["engine"] != args.engine or any((args.max_cost, args.max_tokens, args.max_turns, args.max_output)):
            raise ValueError("canonical contract cannot use experimental caps or a different engine")
        if contract.get("evidence_schema") != "native-v4":
            raise ValueError("canonical contract belongs to a retired runtime; prepare a new native trial")
        home.mkdir(parents=True, exist_ok=False, mode=0o700)
        directory = home / "runtime-v4"
        directory.mkdir(mode=0o700)
        config = contract["configuration"]
        atomic_json(directory / "host.json", config)
        (directory / "host.json").chmod(0o600)
        base_url = config["providers"]["inference-net"]["base_url"]
        atomic_json(evidence / "provider-catalog.json", contract["catalog_model"])
    else:
        selected_model = getattr(args, "model", "kimi-k3")
        config = write_config(home, args.engine, args.max_output, base_url, getattr(args, "native_defaults", False), selected_model)
        config = discover_catalog(home, evidence, base_url, os.environ["INFERENCE_API_KEY"], selected_model)
    atomic_json(evidence / "configuration.json", config)
    env = dict(os.environ, WHIPCODE_HOME=str(home))
    prompt = Path(args.instruction).read_text()
    if args.commit:
        prompt += "\n\n" + (contract["commit_instruction"] if contract_path else
            "Commit your final changes in this disposable task repository so the evaluator can collect git diff BASE HEAD.")
    model = config["defaults"]["model"]["name"]
    command = [args.binary, "run", "--format", "json", "--quiet", "--rlm-engine", args.engine,
               "--max-cost", str(args.max_cost),
               "--max-tokens", str(args.max_tokens), "--max-turns", str(args.max_turns),
               "--timeout", str(args.timeout) + "s", "-m", model, "-p", "inference-net"]
    binary_hash = hashlib.sha256(Path(args.binary).read_bytes()).hexdigest()
    identity = {"evidence_schema": "native-v4", "binary_sha256": binary_hash, "engine": args.engine,
                "model": model, "provider": "inference-net", "reasoning_effort": "high",
                "instruction_sha256": hashlib.sha256(prompt.encode()).hexdigest(),
                "command": command, "live_provider": not args.fixture, "provider_endpoint": base_url,
                "finality_policy": "CLI exited plus tree settled with no database commits for 2 seconds; verified daemon SIGSTOP and settled readback before grading",
                "future_schedule_policy": "record and end; no future wakeups after task finality",
                "workspace_grants": WORKSPACE_GRANTS}
    atomic_json(evidence / "identity.json", identity)
    started = time.monotonic()
    database = home / "runtime-v4" / "state.db"
    last = None
    quiet_since = None
    status = "timeout"
    peak_rss = 0
    cpu_seen = {}
    frozen_pid = None
    frozen_identity = None
    daemon_signal_method = None
    export_errors = []
    content_export_errors = []
    content_export_complete = False
    final_snapshot = False
    daemon_stopped = False
    # One read-only connection whose PRAGMA data_version moves only when the
    # daemon commits: the full snapshot and its two files are refreshed on
    # change instead of four times a second for the whole trial.
    gate = None
    data_version = None

    def export(current):
        """Publish one snapshot; the database backup at finality is the record."""
        nonlocal last
        last = current
        metrics = aggregate(last["calls"])
        metrics.update({"duration_seconds": time.monotonic() - started, "engine": args.engine,
                        "pending": last["pending"], "observed_database_version": data_version,
                        "sampled_peak_container_rss_bytes": peak_rss,
                        "sampled_container_cpu_seconds": sum(cpu_seen.values())})
        atomic_json(evidence / "metrics.json", metrics)
        atomic_json(evidence / "state.json", last)
        return metrics

    with (evidence / "cli.ndjson").open("w") as stdout, (evidence / "cli.stderr").open("w") as stderr:
        process = None
        try:
            root_id = prepare_native_session(args.binary, home, args.engine, env)
            command.extend(["--resume", root_id])
            identity["command"] = command
            atomic_json(evidence / "identity.json", identity)
            process = subprocess.Popen(command, stdin=subprocess.PIPE, stdout=stdout, stderr=stderr, env=env, start_new_session=True)
            process.stdin.write(prompt.encode())
            process.stdin.close()
            while time.monotonic() - started < args.timeout:
                rss, cpu = process_sample()
                peak_rss = max(peak_rss, rss)
                for pid, seconds in cpu.items():
                    cpu_seen[pid] = max(cpu_seen.get(pid, 0), seconds)
                exited = process.poll() is not None
                if database.exists():
                    changed = False
                    try:
                        if gate is None:
                            gate = sqlite3.connect(database.as_uri() + "?mode=ro", uri=True, timeout=5)
                        version = gate.execute("PRAGMA data_version").fetchone()[0]
                        if last is None or version != data_version:
                            current = snapshot(database)
                            data_version, changed = version, True
                            export(current)
                    except (sqlite3.OperationalError, ValueError):
                        quiet_since = None
                        if last is None and exited and time.monotonic() - started > 10:
                            raise
                        time.sleep(0.25)
                        continue
                    if last is not None:
                        quiet_since = quiet_start(quiet_since, exited=exited, settled=last["settled"],
                                                  changed=changed, at=time.monotonic())
                        if quiet_since is not None and time.monotonic() - quiet_since >= 2:
                            frozen_pid, daemon_signal_method, frozen_start = freeze_daemon(home, args.binary)
                            frozen_identity = (frozen_pid, frozen_start)
                            try:
                                frozen = snapshot(database)
                                if frozen["settled"] and frozen == last:
                                    status = settled_outcome(frozen, process.returncode)["status"]
                                    break
                            finally:
                                if status == "timeout":
                                    signal_daemon(home, args.binary, signal.SIGCONT, frozen_identity)
                                    frozen_pid = None
                            quiet_since = None
                if exited and last is None and time.monotonic() - started > 10:
                    status = "startup_error"
                    break
                time.sleep(0.25)
        except Exception as error:
            status = "observer_error"
            export_errors.append(type(error).__name__ + ": " + str(error))
        finally:
            agent_duration = time.monotonic() - started
            if process is not None and process.poll() is None:
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
                        signal_daemon(home, args.binary, signal.SIGCONT, frozen_identity)
                        frozen_pid = None
                    stopped = subprocess.run([args.binary, "daemon", "stop"], env=env, stdout=stderr, stderr=stderr, timeout=20, check=False)
                    if stopped.returncode:
                        export_errors.append("daemon stop exited " + str(stopped.returncode))
                    else:
                        daemon_stopped = True
                except Exception as error:
                    export_errors.append("daemon stop: " + str(error))
            if gate is not None:
                gate.close()
            try:
                if database.exists():
                    export(snapshot(database))
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
            turn_outcome = settled_outcome(last, process.returncode if process else None) if last else {
                "latest_root_turn_status": None, "failed_or_interrupted_turns": None}
            outcome = {**turn_outcome, "status": status, "cli_exit_code": process.returncode if process else None,
                       "duration_seconds": time.monotonic() - started, "observed_database_version": data_version,
                       "agent_duration_seconds": agent_duration,
                       "engine": args.engine, "pending": last["pending"] if last else None,
                       "frozen_daemon_pid": frozen_pid, "frozen_process_start_time": frozen_identity[1] if frozen_pid else None,
                       "final_snapshot": final_snapshot,
                       "daemon_stopped": daemon_stopped,
                       "daemon_signal_method": daemon_signal_method,
                       "content_export_complete": content_export_complete,
                       "content_export_errors": content_export_errors,
                       "evidence_errors": export_errors}
            atomic_json(evidence / "outcome.json", outcome)
    return outcome


def run(args):
    if not args.fixture:
        return _run(args)
    if __package__:
        from .fixture_provider import start
    else:
        from fixture_provider import start
    server, url = start(args.engine)
    try:
        return _run(args, url)
    finally:
        server.shutdown()
        server.server_close()


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--binary", default="/opt/whip/whip")
    parser.add_argument("--engine", choices=["starlark", "quickjs"], required=True)
    parser.add_argument("--home", default="/tmp/whip-eval-home")
    parser.add_argument("--instruction", required=True)
    parser.add_argument("--evidence", default="/logs/agent/whip")
    parser.add_argument("--timeout", type=int, default=600)
    parser.add_argument("--max-cost", type=float, default=0)
    parser.add_argument("--max-tokens", type=int, default=0)
    parser.add_argument("--max-output", type=int, default=0)
    parser.add_argument("--model", default="kimi-k3", help="no-contract route; canonical trials take the model from the native contract")
    parser.add_argument("--max-turns", type=int, default=0)
    parser.add_argument("--commit", action="store_true")
    parser.add_argument("--fixture", action="store_true", help="doctor integration: the fake provider in fixture_provider.py")
    parser.add_argument("--native-defaults", action="store_true")
    parser.add_argument("--contract", help="frozen canonical config/catalog; no per-trial discovery")
    print(json.dumps(run(parser.parse_args())))


if __name__ == "__main__":
    main()

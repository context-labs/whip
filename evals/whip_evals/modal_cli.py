"""Local CLI attachment to the one deployed Modal campaign controller."""
from concurrent.futures import ThreadPoolExecutor
import json
import os
from pathlib import Path
import tempfile
import time

from .common import EVALS, file_hash, identifier, inside, new_id, read_json, utc_now, write_json
from . import modal_cloud as cloud

TERMINAL_STATUSES = ("completed", "cancelled", "failed")
FETCH_WORKERS = 8
FETCH_TRIES = 3


def submit(args, *, evals=EVALS):
    from .modal_bundle import plan_campaign, prepare_campaign, verify_bundle
    if args.dry_run:
        return plan_campaign(args, evals=evals)
    settings = cloud.load_config()
    if args.jobs is None:
        args.jobs = settings["max_jobs"]
    settings = cloud.validate_settings({**settings, "jobs": args.jobs})
    if not args.allow_model_calls:
        raise ValueError("submit requires --allow-model-calls for a live campaign")
    args.execution_settings = settings
    prepared = prepare_campaign(args, evals=evals)
    identity = read_json(prepared / "bundle.json")
    digest, run_id = identity["sha256"], identity["run_id"]
    verify_bundle(prepared / "bundle.tar", digest)
    manifest = read_json(prepared / "manifest.json")
    state, inputs, _ = cloud.resources()
    request = {"run_id": run_id, "bundle_sha256": digest, "settings": settings,
               "controller_source_sha256": manifest["controller_source_sha256"],
               "planned": len(manifest["schedule"]), "created_at": utc_now()}
    if not state.put(run_id + "/request", request, skip_if_exists=True):
        raise ValueError("run ID already submitted; attach with status, never replay")
    try:
        with inputs.batch_upload() as upload:
            upload.put_file(prepared / "bundle.tar", f"/{run_id}/bundle.tar")
            upload.put_file(prepared / "bundle.json", f"/{run_id}/bundle.json")
            upload.put_file(prepared / "manifest.json", f"/{run_id}/manifest.json")
            upload.put_file(prepared / "inputs.json", f"/{run_id}/schedule.json")
        call = cloud.sdk().Function.from_name(cloud.APP_NAME, "coordinate", environment_name=cloud.ENVIRONMENT).spawn(
            run_id, digest, settings, manifest["controller_source_sha256"])
        receipt = {**request, "call_id": call.object_id, "status": "submitted"}
        state.put(run_id + "/submission", receipt)
        write_json(prepared / "submission.json", receipt, exclusive=True)
        return receipt
    except Exception as error:
        state.put(run_id + "/submission_error", {"error_code": type(error).__name__, "at": utc_now()})
        raise ValueError("submission outcome uncertain; inspect status for this run ID before any new work") from error


def status(run_id):
    run_id = identifier(run_id)
    state, _, _ = cloud.resources()
    request = state.get(run_id + "/request")
    if request is None:
        raise ValueError("unknown cloud run ID")
    value = state.get(run_id + "/status") or {"status": "submitted_or_indeterminate"}
    attempts = [dict(record) for key, record in state.items()
                if isinstance(key, str) and key.startswith(run_id + "/attempt/")]
    return {**value, "run_id": run_id, "planned": request["planned"], "request": request,
            "controller": state.get(run_id + "/controller"), "cancel_requested": bool(state.get(run_id + "/cancel")),
            "fetched": state.get(run_id + "/fetched"),
            "attempts": sorted(attempts, key=lambda t: t["trial_id"])}


def cancel(run_id):
    run_id = identifier(run_id)
    state, _, _ = cloud.resources()
    if state.get(run_id + "/request") is None:
        raise ValueError("unknown cloud run ID")
    state.put(run_id + "/cancel", {"requested_at": utc_now()}, skip_if_exists=True)
    result = status(run_id)
    result.update(status="cancel_requested",
                  detail="the coordinator delivers cancellation to running workers; they export evidence and exit")
    return result


def logs(run_id, trial_id=None, *, tail_bytes=8192):
    """Bounded structured lifecycle logs; raw native logs are retained by fetch."""
    if not 1 <= tail_bytes <= 1024 * 1024:
        raise ValueError("log tail must be between 1 and 1048576 bytes")
    value = status(run_id)
    events = value["attempts"]
    if trial_id:
        events = [event for event in events if event["trial_id"] == identifier(trial_id)]
    text = "\n".join(json.dumps(event, sort_keys=True) for event in events)
    data = text.encode()
    return {"run_id": value["run_id"], "kind": "lifecycle", "text": data[-tail_bytes:].decode(errors="replace"),
            "truncated": len(data) > tail_bytes, "raw_logs": "fetch retains native runner/agent logs"}


def retain_partial(row, directory, attempt):
    """Retain observed paid work without fabricating a final bill or finality."""
    from decimal import Decimal
    from .common import number
    from .observe import aggregate
    from .report import money
    directory = Path(directory)
    samples = []
    for path in (directory / "artifacts").rglob("*.json"):
        if path.name not in ("metrics.interim.json", "metrics.json", "state.json"):
            continue
        try:
            value = read_json(path)
            if path.name == "state.json":
                value = aggregate(value["calls"])
            if number(value.get("ledger_cost_usd")) and value["ledger_cost_usd"] >= 0:
                samples.append((value.get("event_cursor", 0), value.get("model_calls", 0), value, path))
        except (OSError, ValueError, KeyError, TypeError):
            continue
    if samples:
        # Samples are cumulative, not disjoint bills: retain the greatest known
        # lower bound, never sum snapshots.
        _, _, sample, path = max(samples, key=lambda item: (Decimal(str(item[2]["ledger_cost_usd"])), *item[:2]))
        observed = money(sample["ledger_cost_usd"])
        if not row.get("accounting_complete") and Decimal(observed) >= Decimal(row["known_cost_usd"]):
            row["known_cost_usd"] = observed
            for key in ("model_calls", "unknown_cost_calls", "unknown_usage_calls"):
                row[key] = sample.get(key)
            row["partial_observation"] = {"path": path.relative_to(directory).as_posix(),
                "event_cursor": sample.get("event_cursor"), "not_final": True,
                "observed_input_tokens": sample.get("input_tokens"),
                "observed_output_tokens": sample.get("output_tokens"),
                "observed_cache_tokens": sample.get("cache_tokens")}
    started = bool(attempt.get("sandbox_id") or samples or (directory / "started.json").exists())
    if started and not row.get("started"):
        row.update(started=True, execution_status="interrupted_or_indeterminate",
                   termination_source="cloud_worker_incomplete")
    if not row.get("accounting_complete"):
        row["cost_usd"] = None
    # Native cleanup and VM exit are independent facts; report both.
    row["native_cleanup_complete"] = row.get("cleanup_complete")
    row["cleanup_complete"] = attempt.get("cleanup_complete") is True
    return row


def download_file(volume, path, target):
    target.parent.mkdir(parents=True, exist_ok=True)
    for attempt in range(1, FETCH_TRIES + 1):
        try:
            with tempfile.NamedTemporaryFile(prefix="." + target.name + "-", dir=target.parent, delete=False) as stream:
                for chunk in volume.read_file(path):
                    stream.write(chunk)
                stream.flush()
                os.fsync(stream.fileno())
            os.replace(stream.name, target)
            return
        except Exception:
            Path(stream.name).unlink(missing_ok=True)
            if attempt == FETCH_TRIES:
                raise
            time.sleep(2 * attempt)


def collect_files(evidence, run_id, root):
    """Download the run's evidence prefix with a small thread pool; fail loudly."""
    try:
        entries = list(evidence.iterdir(f"/{run_id}", recursive=True))
    except FileNotFoundError:
        return 0
    files = []
    for entry in entries:
        if int(entry.type) != 1:
            continue
        if not entry.path.startswith((run_id + "/", "/" + run_id + "/")):
            raise ValueError("unexpected remote evidence path")
        relative = entry.path.lstrip("/")[len(run_id) + 1:]
        files.append((entry.path, inside(root, relative)))
    with ThreadPoolExecutor(max_workers=FETCH_WORKERS) as pool:
        for _ in pool.map(lambda item: download_file(evidence, *item), files):
            pass
    return len(files)


def prune(run_id, *, force=False):
    """Delete the run's Modal-side inputs, evidence and state; keep `<run>/fetched`."""
    run_id = identifier(run_id)
    state, inputs, evidence = cloud.resources()
    if state.get(run_id + "/request") is None:
        raise ValueError("unknown cloud run ID")
    current = (state.get(run_id + "/status") or {}).get("status")
    if current not in TERMINAL_STATUSES and not force:
        raise ValueError("run is not finished (" + str(current) + "); cancel it or pass --force")
    removed = {"inputs": False, "evidence": False, "state_keys": 0}
    for name, volume in (("inputs", inputs), ("evidence", evidence)):
        try:
            volume.remove_file("/" + run_id, recursive=True)
            removed[name] = True
        except FileNotFoundError:
            pass
    for key in [k for k, _ in state.items() if isinstance(k, str) and k.startswith(run_id + "/") and k != run_id + "/fetched"]:
        state.pop(key)
        removed["state_keys"] += 1
    return {"run_id": run_id, "status": "pruned", **removed}


def fetch(run_id, *, evals=EVALS, keep=False):
    """Snapshot all retained evidence into a new fetch directory, then prune Modal."""
    from .report import build_result, empty_trial, normalize_trial, write_report
    run_id = identifier(run_id)
    state, inputs, evidence = cloud.resources()
    request = state.get(run_id + "/request")
    if request is None:
        raise ValueError("unknown cloud run ID")
    cloud_status = state.get(run_id + "/status") or {}
    manifest = cloud.read_remote(inputs, f"/{run_id}/manifest.json")
    root = Path(evals) / "artifacts" / run_id / "fetches" / new_id()
    root.mkdir(parents=True, exist_ok=False)
    rows = []
    retained = collect_files(evidence, run_id, root)
    for trial in manifest["schedule"]:
        directory = root / "attempts" / trial["id"]
        artifacts = directory / "artifacts"
        record = artifacts / "record.json"
        row = empty_trial(trial)
        marker = directory / "complete.json"
        verified = False
        if marker.exists():
            value = read_json(marker)
            verified = (value.get("run_id"), value.get("trial_id"), value.get("bundle_sha256")) == (run_id, trial["id"], request["bundle_sha256"])
            verified = verified and bool(value.get("files")) and all(
                inside(directory, path).is_file() and file_hash(inside(directory, path)) == digest
                for path, digest in value.get("files", {}).items())
        if record.exists():
            try:
                row = normalize_trial(trial, read_json(record), artifacts)
            except (OSError, ValueError, KeyError, TypeError) as error:
                row.update(execution_status="export_error", error_codes=[type(error).__name__])
        if not verified:
            row.update(evidence_complete=False, accounting_complete=False, cost_usd=None)
            row["error_codes"].append("cloud_snapshot_incomplete")
        attempt = state.get(run_id + "/attempt/" + trial["id"]) or {}
        row = retain_partial(row, directory, attempt)
        prefix = f"attempts/{trial['id']}/artifacts/"
        details = row.get("artifacts", {})
        if details.get("native_result"):
            details["native_result"] = prefix + details["native_result"]
        if details.get("evidence_files"):
            details["evidence_files"] = {prefix + path: digest for path, digest in details["evidence_files"].items()}
        rows.append(row)
    manifest = {**manifest, "artifact_root": root.relative_to(evals).as_posix()}
    result = build_result(manifest, rows, cancelled=cloud_status.get("status") == "cancelled")
    result["cloud"] = {"request": request, "status": cloud_status}
    report = Path(evals) / "reports" / run_id / "fetches" / root.name
    write_report(report, manifest, result)
    outcome = {"run_id": run_id, "status": result["status"], "report": str(report / "report.md"),
               "artifact_root": str(root), "retained_files": retained, "pruned": False}
    if cloud_status.get("status") in TERMINAL_STATUSES:
        state.put(run_id + "/fetched", {"at": utc_now(), "artifact_root": outcome["artifact_root"],
                                        "report": outcome["report"], "status": result["status"]})
        if not keep:
            outcome["pruned"] = prune(run_id)["status"] == "pruned"
    else:
        outcome["note"] = "coordinator not finished; Modal-side data kept"
    return outcome

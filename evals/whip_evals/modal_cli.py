"""Local CLI attachment to the one deployed Modal campaign controller."""
from concurrent.futures import FIRST_COMPLETED, ThreadPoolExecutor, wait
from importlib.metadata import version
from itertools import chain
import json
import os
from pathlib import Path
import tempfile

from .common import EVALS, file_hash, identifier, inside, new_id, read_json, utc_now, write_json
from . import modal_cloud as cloud


def submit(args, *, evals=EVALS):
    from .modal_bundle import plan_campaign, prepare_campaign, verify_bundle
    if args.dry_run:
        return plan_campaign(args, evals=evals)
    settings = cloud.validate_settings(read_json(args.settings))
    settings = {**settings, "jobs": args.jobs}
    cloud.validate_settings(settings)
    if not settings.get("fixture") and not args.allow_model_calls:
        raise ValueError("submit requires --allow-model-calls for a live campaign")
    if bool(settings.get("fixture")) != bool(getattr(args, "fixture", False)):
        raise ValueError("fixture selection must match the qualified settings")
    args.execution_settings = settings
    prepared = prepare_campaign(args, evals=evals)
    identity = read_json(prepared / "bundle.json")
    digest, run_id = identity["sha256"], identity["run_id"]
    verify_bundle(prepared / "bundle.tar", digest)
    manifest = read_json(prepared / "manifest.json")
    native_schedule = prepared / "inputs.json"
    state, inputs, _ = cloud.resources()
    request = {"run_id": run_id, "bundle_sha256": digest, "settings": settings,
               "controller_source_sha256": manifest["controller_source_sha256"],
               "planned": len(manifest["schedule"]), "created_at": utc_now()}
    if not state.put(run_id + "/request", request, skip_if_exists=True):
        raise ValueError("run ID already submitted; attach with status, never replay")
    # No force/overwrite. Interrupted upload remains a reserved, non-executed
    # request; it cannot silently replace an existing immutable bundle.
    try:
        with inputs.batch_upload() as upload:
            upload.put_file(prepared / "bundle.tar", f"/{run_id}/bundle.tar")
            upload.put_file(prepared / "bundle.json", f"/{run_id}/bundle.json")
            upload.put_file(prepared / "manifest.json", f"/{run_id}/manifest.json")
            upload.put_file(native_schedule, f"/{run_id}/schedule.json")
        call = cloud.sdk().Function.from_name(cloud.APP_NAME, "coordinate", environment_name=cloud.ENVIRONMENT).spawn(run_id, digest, settings, manifest["controller_source_sha256"])
        # The detached function independently persists its current call ID before
        # any dispatch, covering CLI death between spawn and this receipt.
        receipt = {**request, "call_id": call.object_id, "status": "submitted"}
        state.put(run_id + "/submission", receipt)
        write_json(prepared / "submission.json", receipt, exclusive=True)
        return receipt
    except Exception as error:
        state.put(run_id + "/submission_error", {"error_code": type(error).__name__, "at": utc_now(),
                                                   "status": "indeterminate", "automatic_retry": False})
        raise ValueError("submission outcome uncertain; inspect status for this run ID before any new work") from error


def status(run_id, *, reconcile=False):
    run_id = identifier(run_id)
    state, _, evidence = cloud.resources()
    request = state.get(run_id + "/request")
    if request is None:
        raise ValueError("unknown cloud run ID")
    value = state.get(run_id + "/status") or {"status": "submitted_or_indeterminate"}
    cancelling = bool(state.get(run_id + "/cancel"))
    attempts = []
    for key, record in state.items():
        if isinstance(key, str) and key.startswith(run_id + "/attempt/"):
            record = dict(record)
            if reconcile:
                sandbox = None
                try:
                    sandbox = cloud.owned_worker(record, request["bundle_sha256"])
                    record["sandbox_id"] = sandbox.object_id
                    record["ownership_proven"] = True
                    exit_code = sandbox.poll()
                    record.update(worker_exit_code=exit_code, cleanup_complete=exit_code is not None)
                    if cancelling and exit_code is None:
                        sandbox.filesystem.write_text("cancel\n", "/tmp/whip-eval-cancel")
                        record["cancel_delivered_at"] = utc_now()
                    # Separate keys avoid overwriting the live single-owner ledger.
                    state.put(run_id + "/reconciliation/" + record["trial_id"], record)
                except Exception as error:
                    record.update(ownership_proven=False, ownership_error=type(error).__name__)
                try:
                    marker = cloud.durable_completion(evidence, run_id, record["trial_id"], request["bundle_sha256"])
                    record.update(durable_result=True, worker_status=marker["status"])
                    if sandbox is not None and record.get("ownership_proven") and sandbox.poll() is None:
                        sandbox.filesystem.write_text("verified\n", "/tmp/whip-eval-exported")
                        record["final_ack_at"] = utc_now()
                        state.put(run_id + "/reconciliation/" + record["trial_id"], record)
                except Exception as error:
                    record.update(durable_result=False, reconcile_error=type(error).__name__)
            # Keep large inventories out of status/logs and credentials out of all output.
            record.pop("completion", None)
            attempts.append(record)
    return {**value, "run_id": run_id, "planned": request["planned"], "request": request,
            "controller": state.get(run_id + "/controller"), "cancel_requested": bool(state.get(run_id + "/cancel")),
            "attempts": sorted(attempts, key=lambda t: t["trial_id"]),
            "reconciliation": "attach/prove; delivers pending cancellation; never redispatches" if reconcile else None}


def cancel(run_id):
    run_id = identifier(run_id)
    state, _, _ = cloud.resources()
    if state.get(run_id + "/request") is None:
        raise ValueError("unknown cloud run ID")
    state.put(run_id + "/cancel", {"requested_at": utc_now()}, skip_if_exists=True)
    result = status(run_id, reconcile=True)
    result.update(status="cancel_requested", cleanup_complete=False,
                  detail="independently signalled proven workers; reconcile again for late creates and cleanup")
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


def retain_partial(row, directory, attempt, reconciliation=None):
    """Retain observed paid work without fabricating a final bill or finality."""
    from decimal import Decimal
    from .common import number
    from .observe import aggregate
    from .report import money
    directory = Path(directory)
    samples = []
    artifacts = directory / "artifacts"
    for path in artifacts.rglob("*.json"):
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
        # lower bound, never sum snapshots. State snapshots have no event cursor.
        _, _, sample, path = max(samples, key=lambda item: (Decimal(str(item[2]["ledger_cost_usd"])), *item[:2]))
        observed = money(sample["ledger_cost_usd"])
        # A complete normalized result remains authoritative. The fallback only
        # replaces its empty/missing-result accounting, never adds snapshots.
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
    # Native cleanup and physical VM exit are independent. A dead coordinator
    # may leave a stale attempt ledger; accept only its exact owned reconciliation.
    proof = reconciliation or {}
    same_attempt = all(attempt.get(key) and proof.get(key) == attempt[key]
                       for key in ("run_id", "trial_id", "bundle_sha256", "owner_id"))
    same_worker = bool(proof.get("sandbox_id")) and (
        not attempt.get("sandbox_id") or proof["sandbox_id"] == attempt["sandbox_id"])
    reconciled_exit = (same_attempt and same_worker and proof.get("ownership_proven") is True
                       and proof.get("cleanup_complete") is True
                       and type(proof.get("worker_exit_code")) is int)
    row["native_cleanup_complete"] = row.get("cleanup_complete")
    row["cleanup_complete"] = attempt.get("cleanup_complete") is True or bool(reconciled_exit)
    return row


def collect_files(evidence, run_id, root):
    """Stream at most eight remote files concurrently; never queue a whole run."""
    # This pinned private hook is also used by the SDK CLI. The public iterator
    # prefetches CPU-count blocks; concurrency=1 streams each file without that fanout.
    download_into = getattr(evidence, "_read_file_into_fileobj", None)
    if version("modal") != "1.5.5" or not callable(download_into):
        raise RuntimeError("collection requires Modal 1.5.5 streaming download API")

    def download(path, target):
        target.parent.mkdir(parents=True, exist_ok=True)
        # Match local atomic_write's exclusive publication without buffering a
        # whole evidence file. Never weaken the shared helper or Volume writes.
        with tempfile.NamedTemporaryFile(prefix="." + target.name + "-", dir=target.parent) as stream:
            download_into(path, stream, concurrency=1)
            stream.flush()
            os.fsync(stream.fileno())
            os.link(stream.name, target)
        directory = os.open(target.parent, os.O_RDONLY)
        try:
            os.fsync(directory)
        finally:
            os.close(directory)
        return 1

    try:
        entries = iter(evidence.iterdir(f"/{run_id}", recursive=True))
        first = next(entries, None)
    except FileNotFoundError:
        return 0  # No evidence prefix yet; never swallow a file download error.
    retained, pending = 0, set()
    with ThreadPoolExecutor(max_workers=8) as pool:
        try:
            for entry in chain(() if first is None else (first,), entries):
                if not entry.path.startswith((run_id + "/", "/" + run_id + "/")):
                    raise ValueError("unexpected remote evidence path")
                if int(entry.type) != 1:
                    continue
                relative = entry.path.lstrip("/")[len(run_id) + 1:]
                target = inside(root, relative)
                pending.add(pool.submit(download, entry.path, target))
                if len(pending) == 8:
                    done, pending = wait(pending, return_when=FIRST_COMPLETED)
                    retained += sum(future.result() for future in done)
            retained += sum(future.result() for future in pending)
        finally:
            for future in pending:
                future.cancel()
    return retained


def fetch(run_id, *, evals=EVALS):
    """Snapshot all retained evidence without clobbering a prior fetch/report."""
    from .report import build_result, empty_trial, normalize_trial, write_report
    run_id = identifier(run_id)
    state, inputs, evidence = cloud.resources()
    request = state.get(run_id + "/request")
    if request is None:
        raise ValueError("unknown cloud run ID")
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
            verified = verified and value.get("integrity_complete") is True and bool(value.get("files")) and all(
                inside(directory, path).is_file() and file_hash(inside(directory, path)) == digest
                for path, digest in value.get("files", {}).items())
        if record.exists():
            try:
                row = normalize_trial(trial, read_json(record), artifacts)
            except (OSError, ValueError, KeyError, TypeError) as error:
                row.update(execution_status="export_error", error_codes=[type(error).__name__])
        if not verified:
            row.update(evidence_complete=False, accounting_complete=False, cost_usd=None)
            if marker.exists() and read_json(marker).get("integrity_complete") is not True:
                row["error_codes"].append("cloud_integrity_failure")

            row["error_codes"].append("cloud_snapshot_incomplete")
        attempt = state.get(run_id + "/attempt/" + trial["id"]) or {}
        reconciliation = state.get(run_id + "/reconciliation/" + trial["id"])
        row = retain_partial(row, directory, attempt, reconciliation)
        # Report paths are relative to this fetched artifact root.
        prefix = f"attempts/{trial['id']}/artifacts/"
        details = row.get("artifacts", {})
        if details.get("native_result"):
            details["native_result"] = prefix + details["native_result"]
        if details.get("evidence_files"):
            details["evidence_files"] = {prefix + path: digest for path, digest in details["evidence_files"].items()}
        rows.append(row)
    manifest = {**manifest, "artifact_root": root.relative_to(evals).as_posix()}
    result = build_result(manifest, rows)
    result["cloud"] = {"request": request, "status": state.get(run_id + "/status"),
                       "infra_cost_usd": None, "infra_cost_status": "not_reconciled"}
    report = Path(evals) / "reports" / run_id / "fetches" / root.name
    write_report(report, manifest, result)
    return {"run_id": run_id, "status": result["status"], "report": str(report / "report.md"),
            "artifact_root": str(root), "retained_files": retained}

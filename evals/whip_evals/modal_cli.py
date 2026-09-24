"""Local CLI attachment to the one deployed Modal campaign controller."""
import os
from pathlib import Path
import shutil
import subprocess
import sys
import time

from .common import EVALS, file_hash, identifier, inside, new_id, read_json, utc_now, write_json
from . import modal_cloud as cloud

TERMINAL_STATUSES = ("completed", "cancelled", "failed")
FETCH_TRIES = 3


def submit(args, *, evals=EVALS):
    from .modal_bundle import plan_campaign, prepare_campaign
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
    manifest = read_json(prepared / "manifest.json")
    state, inputs, _ = cloud.resources()
    request = {"run_id": run_id, "bundle_sha256": digest, "settings": settings,
               "controller_source_sha256": manifest["controller_source_sha256"],
               "planned": len(manifest["schedule"]), "created_at": utc_now()}
    if not state.put(run_id + "/request", request, skip_if_exists=True):
        raise ValueError("run ID already submitted; attach with status, never replay")
    try:
        with inputs.batch_upload() as upload:
            for name in ("bundle.tar", "bundle.json", "manifest.json", "schedule.json"):
                upload.put_file(prepared / name, f"/{run_id}/{name}")
        call = cloud.sdk().Function.from_name(cloud.APP_NAME, "coordinate", environment_name=cloud.ENVIRONMENT).spawn(
            run_id, digest, settings, manifest["controller_source_sha256"])
        receipt = {**request, "call_id": call.object_id, "status": "submitted"}
        write_json(prepared / "submission.json", receipt, exclusive=True)
        return receipt
    except Exception as error:
        raise ValueError("submission outcome uncertain; inspect status for this run ID before any new work") from error


def request_record(state, run_id):
    request = state.get(run_id + "/request")
    if request is None:
        raise ValueError("unknown cloud run ID")
    return request


def status(run_id):
    run_id = identifier(run_id)
    state, _, _ = cloud.resources()
    if state.get(run_id + "/request") is None:
        fetched = state.get(run_id + "/fetched")
        if fetched is None:
            raise ValueError("unknown cloud run ID")
        # A fetched run keeps only this receipt on Modal; the report is local.
        return {"run_id": run_id, "status": "fetched", "fetched": fetched, "attempts": []}
    request = request_record(state, run_id)
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
    request_record(state, run_id)
    state.put(run_id + "/cancel", {"requested_at": utc_now()}, skip_if_exists=True)
    result = status(run_id)
    result.update(status="cancel_requested",
                  detail="the coordinator delivers cancellation to running workers; they export evidence and exit")
    return result


def cloud_fields(row, directory, attempt):
    """VM facts the native row cannot know: whether the worker started and exited."""
    if not row["started"] and (attempt.get("sandbox_id") or (directory / "started.json").exists()):
        row.update(started=True, execution_status="interrupted", termination_source="cloud_worker_incomplete")
    # Native cleanup and VM exit are independent facts; report both.
    row["native_cleanup_complete"] = row["cleanup_complete"]
    row["cleanup_complete"] = attempt.get("cleanup_complete") is True
    return row


def not_found():
    # The SDK raises its own NotFoundError for a missing volume path; it is not
    # a FileNotFoundError subclass.
    return (FileNotFoundError, cloud.sdk().exception.NotFoundError)


def collect_files(evidence, run_id, root):
    """Download the run's evidence prefix with the Modal CLI's parallel transfer.

    Per-file reads through the SDK ran at about 12 files/s (46 min for a
    90-attempt campaign); the CLI moves the same data in a few minutes. Fetch
    verifies every file each worker's marker lists, so no separate listing.
    """
    try:
        next(iter(evidence.iterdir(f"/{run_id}")), None)
    except not_found():
        return 0
    root = Path(root)
    command = [sys.executable, "-m", "modal", "volume", "get", "--env", cloud.ENVIRONMENT, "--force",
               cloud.EVIDENCE_VOLUME, "/" + run_id, str(root)]
    for attempt in range(1, FETCH_TRIES + 1):
        try:
            subprocess.run(command, check=True, stdout=subprocess.DEVNULL, stderr=subprocess.PIPE,
                           env={**os.environ, "MODAL_ENVIRONMENT": cloud.ENVIRONMENT})
            break
        except subprocess.CalledProcessError as error:
            if attempt == FETCH_TRIES:
                raise RuntimeError("modal volume get failed: " + (error.stderr or b"")[-2000:].decode(errors="replace")) from error
            time.sleep(5 * attempt)
    # The CLI writes a directory prefix as <destination>/<prefix name>/...; flatten it.
    nested = root / run_id
    if nested.is_dir():
        for child in nested.iterdir():
            shutil.move(str(child), root / child.name)
        nested.rmdir()
    return sum(1 for path in root.rglob("*") if path.is_file())


def prune(run_id, *, force=False):
    """Delete the run's Modal-side inputs, evidence and state; keep `<run>/fetched`."""
    run_id = identifier(run_id)
    state, inputs, evidence = cloud.resources()
    request_record(state, run_id)
    current = (state.get(run_id + "/status") or {}).get("status")
    if current not in TERMINAL_STATUSES and not force:
        raise ValueError("run is not finished (" + str(current) + "); cancel it or pass --force")
    removed = {"inputs": False, "evidence": False, "state_keys": 0}
    for name, volume in (("inputs", inputs), ("evidence", evidence)):
        try:
            volume.remove_file("/" + run_id, recursive=True)
            removed[name] = True
        except not_found():
            pass
    for key in [k for k in state.keys() if isinstance(k, str) and k.startswith(run_id + "/") and k != run_id + "/fetched"]:
        state.pop(key)
        removed["state_keys"] += 1
    return {"run_id": run_id, "status": "pruned", **removed}


def fetch(run_id, *, evals=EVALS, keep=False):
    """Snapshot all retained evidence into a new fetch directory, then prune Modal."""
    from .report import build_result, normalize_or_error, write_report
    run_id = identifier(run_id)
    state, inputs, evidence = cloud.resources()
    request = request_record(state, run_id)
    cloud_status = state.get(run_id + "/status") or {}
    attempts = {key.rsplit("/", 1)[1]: record for key, record in state.items()
                if isinstance(key, str) and key.startswith(run_id + "/attempt/")}
    manifest = cloud.read_remote(inputs, f"/{run_id}/manifest.json")
    root = Path(evals) / "artifacts" / run_id / "fetches" / new_id()
    root.mkdir(parents=True, exist_ok=False)
    retained = collect_files(evidence, run_id, root)
    rows = []
    for trial in manifest["schedule"]:
        directory = root / "attempts" / trial["id"]
        prefix = f"attempts/{trial['id']}/artifacts"
        record_path = directory / "artifacts" / "record.json"
        raw = read_json(record_path) if record_path.is_file() else {"started": False, "job_path": f"jobs/{trial['id']}",
                                                                      "cleanup": {"complete": False}}
        raw["job_path"] = f"{prefix}/{raw['job_path']}"  # normalize against the fetch root
        row = normalize_or_error(trial, raw, root)
        marker = directory / "complete.json"
        verified = False
        if marker.is_file():
            value = read_json(marker)
            verified = (value.get("run_id"), value.get("trial_id"), value.get("bundle_sha256")) == (run_id, trial["id"], request["bundle_sha256"])
            verified = verified and bool(value.get("files")) and all(
                inside(directory, path).is_file() and file_hash(inside(directory, path)) == digest
                for path, digest in value["files"].items())
        if not verified:
            row.update(evidence_complete=False, accounting_complete=False, cost_usd=None)
            row["error_codes"].append("cloud_snapshot_incomplete")
        rows.append(cloud_fields(row, directory, attempts.get(trial["id"], {})))
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

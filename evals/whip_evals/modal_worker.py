"""One disposable VM, one unchanged native Docker trial. No Modal credentials here."""
import argparse
import os
from pathlib import Path
import signal
import shutil
import tempfile
import threading

from .common import identifier, read_json, tree_files, utc_now, write_json
from .execution import execute_job
from .prepare import environment, pull_images
from .report import normalize_trial


WORK = Path("/work")
EVIDENCE = Path("/evidence")


def worker_paths(run_id, trial_id, *, root=EVIDENCE):
    return Path(root) / identifier(run_id) / "attempts" / identifier(trial_id)


def verify_bundle(archive, digest, destination):
    from .modal_bundle import verify_bundle as verify
    return verify(archive, digest)


def preflight(host, trial, headroom):
    available = {"cpus": host["cpus"], "memory_mb": host["memory_bytes"] // (1024 * 1024),
                 "storage_mb": host["disk_free_mb"]}
    for key, value in available.items():
        if value < trial["resources"][key] + headroom[key]:
            raise ValueError("outer VM cannot fit unchanged native resources and headroom")


def publish_receipts(receipts, target):
    """Copy the runner's closed VM-local receipts once; Volume has no hard links."""
    if not receipts.exists():
        return
    target.mkdir(parents=True, exist_ok=True)
    for source in receipts.iterdir():
        if not source.is_file() or source.is_symlink():
            continue
        fd, temporary = tempfile.mkstemp(prefix=".receipt-", dir=target)
        try:
            with os.fdopen(fd, "wb") as output, source.open("rb") as stream:
                shutil.copyfileobj(stream, output)
                output.flush()
                os.fsync(output.fileno())
            os.replace(temporary, target / source.name)
        finally:
            Path(temporary).unlink(missing_ok=True)


def run_worker(run_id, trial_id, bundle_sha256, *, work=WORK, evidence=EVIDENCE):
    """Never reuse a work directory or a native attempt, even after a crash."""
    run_id, trial_id = identifier(run_id), identifier(trial_id)
    evidence = Path(evidence).resolve(strict=True)
    destination = worker_paths(run_id, trial_id, root=evidence)
    destination.mkdir(parents=True, exist_ok=False)
    write_json(destination / "started.json", {"run_id": run_id, "trial_id": trial_id,
               "bundle_sha256": bundle_sha256, "started_at": utc_now()})
    cancelled = threading.Event()
    stop_watcher = threading.Event()
    def watch_cancel():
        while not stop_watcher.is_set():
            if Path("/tmp/whip-eval-cancel").exists():
                cancelled.set()
            stop_watcher.wait(1)
    watcher = threading.Thread(target=watch_cancel, daemon=True)
    watcher.start()
    previous = {sig: signal.signal(sig, lambda *_: cancelled.set())
                for sig in (signal.SIGTERM, signal.SIGINT)}
    # The native runner needs exclusive-create receipts; keep those on VM-local ext4.
    receipts = Path("/tmp/whip-eval-native") / trial_id
    artifact_root = destination / "artifacts"
    artifact_root.mkdir(exist_ok=False)
    result = {"run_id": run_id, "trial_id": trial_id, "bundle_sha256": bundle_sha256,
              "status": "failed", "accounting_complete": False}
    try:
        verify_bundle(Path("/input") / run_id / "bundle.tar", bundle_sha256, work)
        evals = work / "evals"
        manifest = read_json(evals / "reports" / run_id / "manifest.json")
        trial = next(t for t in manifest["schedule"] if t["id"] == trial_id)
        native = read_json(evals / "artifacts" / run_id / "schedule.json")[trial_id]
        config, envelope = native["config"], native["envelope"]
        config["jobs_dir"] = str(artifact_root / "jobs")
        host = environment(evals)
        write_json(destination / "host.json", host)
        preflight(host, trial, manifest["protocol"]["headroom"])
        if host["os"] != "linux" or host["emulated"] or host["runner_versions"] != manifest["protocol"]["runner_versions"]:
            raise ValueError("worker execution environment does not match frozen protocol")
        # Pull exactly the original OCI images before the existing native clocks.
        from .tasks import load_spec
        lock, _, _ = load_spec(evals)
        selected = [t for t in lock["tasks"] if t["id"] == trial["task_id"]]
        if selected:
            pull_images(selected)
        write_json(destination / "phase.json", {"phase": "native_trial", "at": utc_now()})
        record = execute_job(trial, config, envelope, receipts, cancelled, evals=evals)
        record["job_path"] = str(Path(record["job_path"]).relative_to(artifact_root))
        if record.get("cancelled"):
            record["cancellation_source"] = "user_cancelled"
        write_json(artifact_root / "record.json", record)
        row = normalize_trial(trial, record, artifact_root)
        write_json(artifact_root / "normalized.json", row)
        result.update(status="completed", native_cleanup_complete=record["cleanup"]["complete"],
                      accounting_complete=row["accounting_complete"], row=row)
    except Exception as error:
        # Exception text can contain provider credentials or subprocess commands.
        result["error_code"] = type(error).__name__
    finally:
        stop_watcher.set()
        watcher.join(timeout=2)
        for sig, handler in previous.items():
            signal.signal(sig, handler)
        try:
            publish_receipts(receipts, artifact_root / "trials" / trial_id)
        except OSError as error:
            result["receipt_error"] = type(error).__name__
        result["finished_at"] = utc_now()
        try:
            result["files"] = tree_files(destination)
        except ValueError:
            result["files"] = {}
            result["inventory_error"] = True
        # The marker is written last and the VM exits right after. The controller
        # waits for the exit, then reads the marker; fetch verifies the listed hashes.
        write_json(destination / "complete.json", result)
    return result


def main(argv=None):
    parser = argparse.ArgumentParser()
    parser.add_argument("run_id")
    parser.add_argument("trial_id")
    parser.add_argument("bundle_sha256")
    args = parser.parse_args(argv)
    result = run_worker(args.run_id, args.trial_id, args.bundle_sha256)
    return 0 if result["status"] == "completed" else 2


if __name__ == "__main__":
    raise SystemExit(main())

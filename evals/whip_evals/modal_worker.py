"""One disposable VM, one unchanged native Docker trial. No Modal credentials here."""
import argparse
import os
from pathlib import Path
import signal
import shutil
import tempfile
import subprocess
import threading
import time

from .common import file_hash, identifier, read_json, tree_files, utc_now, write_json
from .execution import execute_job
from .integrity import inventory
from .prepare import environment, pull_images
from .report import empty_trial, normalize_trial


WORK = Path("/work")
EVIDENCE = Path("/evidence")


def worker_paths(run_id, trial_id, *, root=EVIDENCE):
    return Path(root) / identifier(run_id) / "attempts" / identifier(trial_id)


def verify_bundle(archive, digest, destination):
    from .modal_bundle import verify_bundle as verify
    if file_hash(archive) != digest:
        raise ValueError("bundle hash changed after extraction")
    return verify(archive, digest)


def preflight(host, trial, headroom, *, proc=Path("/proc")):
    available = {"cpus": host["cpus"], "memory_mb": host["memory_bytes"] // (1024 * 1024),
                 "storage_mb": host["disk_free_mb"]}
    for key, value in available.items():
        if value < trial["resources"][key] + headroom[key]:
            raise ValueError("outer VM cannot fit unchanged native resources and headroom")
    for name in ("tcp", "tcp6"):
        path = proc / "net" / name
        if not path.exists():
            continue
        for line in path.read_text().splitlines()[1:]:
            fields = line.split()
            if fields[3] == "0A" and int(fields[1].rsplit(":", 1)[1], 16) in (2375, 2376):
                raise ValueError("Docker TCP listener is forbidden")


def run_worker(run_id, trial_id, bundle_sha256, *, work=WORK, evidence=EVIDENCE):
    """Never reuse a work directory or a native attempt, even after a crash."""
    run_id, trial_id = identifier(run_id), identifier(trial_id)
    # Modal exposes the trusted mount root as an alias. Canonicalize only that
    # root; the adapter still rejects symlinks in untrusted evidence descendants.
    evidence = Path(evidence).resolve(strict=True)
    destination = worker_paths(run_id, trial_id, root=evidence)
    # The durable pre-create intent owns exactly one worker. Reserve its namespace
    # once; cloud-owned receipts use atomic replace because Volume has no links.
    destination.mkdir(parents=True, exist_ok=False)
    claim = destination / "started.json"
    write_json(claim, {"run_id": run_id, "trial_id": trial_id,
                      "bundle_sha256": bundle_sha256, "owner_id": os.environ.get("WHIP_EVAL_OWNER_ID"),
                      "started_at": utc_now()})
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
        # Preserve native exclusive writes on ext4; only the wrapper receipts
        # require hard links. Native job and observer logs remain Volume-backed.
        write_json(Path("/tmp/whip-eval-hardlink-proof.json"), {"tested": True}, exclusive=True)
        write_json(destination / "ready.json", {"run_id": run_id, "trial_id": trial_id,
                   "bundle_sha256": bundle_sha256, "native_receipts": "vm-local-ext4",
                   "native_jobs": "durable-volume", "hardlink_proof": True, "at": utc_now()})
        # Verify publication through the independent Volume API before any model
        # can start. ACK is VM-local filesystem, never an inbound Volume update.
        for _ in range(300):
            if cancelled.is_set() or Path("/tmp/whip-eval-admitted").exists():
                break
            time.sleep(1)
        if cancelled.is_set() or not Path("/tmp/whip-eval-admitted").exists():
            raise ValueError("worker admission not confirmed")
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
        # execute_job has closed these files; publish them once without changing
        # its local exclusive-write contract or copying any live SQLite/WAL.
        receipt_target = artifact_root / "trials" / trial_id
        if receipts.exists():
            receipt_target.mkdir(parents=True, exist_ok=True)
            for source in receipts.iterdir():
                if not source.is_file() or source.is_symlink():
                    continue
                fd, temporary = tempfile.mkstemp(prefix=".receipt-", dir=receipt_target)
                try:
                    with os.fdopen(fd, "wb") as output, source.open("rb") as stream:
                        shutil.copyfileobj(stream, output)
                        output.flush()
                        os.fsync(output.fileno())
                    os.replace(temporary, receipt_target / source.name)
                finally:
                    Path(temporary).unlink(missing_ok=True)
        audit = inventory(artifact_root, destination / "integrity.json")
        write_json(destination / "integrity.json", audit)
        result["integrity_complete"] = not audit["errors"] and not audit["skipped_nonregular"] and not audit["exact_key_matches"]
        if audit["exact_key_matches"] or audit["potential_proxy_configs"]:
            result.update(status="security_failure", security_failure=True, accounting_complete=False)
            result["integrity_complete"] = False
        result["finished_at"] = utc_now()
        try:
            result["files"] = tree_files(destination)
        except ValueError:
            result["files"] = {}
            result["integrity_complete"] = False
        # Marker is not proof of durability: the coordinator verifies every hash
        # through the Volume API before acknowledging or reporting completion.
        write_json(destination / "complete.json", result)
    return result


def main(argv=None):
    parser = argparse.ArgumentParser()
    parser.add_argument("run_id")
    parser.add_argument("trial_id")
    parser.add_argument("bundle_sha256")
    args = parser.parse_args(argv)
    result = run_worker(args.run_id, args.trial_id, args.bundle_sha256)
    # Native evidence files are closed. Modal background commits persist them
    # independently of the coordinator. Keep the VM available for explicit flush
    # and hash verification; TTL is the crash backstop, never a task retry.
    acknowledged = Path("/tmp/whip-eval-exported")
    for _ in range(600):
        if acknowledged.exists():
            break
        time.sleep(1)
    return 0 if result["status"] == "completed" else 2


if __name__ == "__main__":
    raise SystemExit(main())

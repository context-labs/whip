"""Narrow Modal control plane for immutable, one-attempt Docker campaigns.

SDK objects are lazy; importing this module never contacts Modal. Deployment is
explicit (`modal deploy -m whip_evals.modal_cloud --env whipcode`).
"""
import hashlib
import json
import os
from pathlib import Path
import re
import shlex
import tempfile
import threading
import time
from uuid import uuid4

from .common import EVALS, atomic_write, file_hash, identifier, inside, read_json, utc_now, value_hash, write_json

ENVIRONMENT = "whipcode"
APP_NAME = "whip-eval"
INPUT_VOLUME = "whip-eval-inputs"
EVIDENCE_VOLUME = "whip-eval-evidence"
STATE_NAME = "whip-eval-state"
PROVIDER_SECRET = "whip-eval-inference"
POLL_SECONDS = 5
EXPORT_SECONDS = 600


def sdk():
    # Some pinned SDK methods (including Sandbox.set_tags) implicitly read this
    # setting even when the App was looked up with an explicit environment.
    os.environ["MODAL_ENVIRONMENT"] = ENVIRONMENT
    import modal
    return modal


def resources():
    modal = sdk()
    return (modal.Dict.from_name(STATE_NAME, environment_name=ENVIRONMENT),
            modal.Volume.from_name(INPUT_VOLUME, environment_name=ENVIRONMENT),
            modal.Volume.from_name(EVIDENCE_VOLUME, environment_name=ENVIRONMENT))


def read_remote(volume, path, *, limit=8 * 1024 * 1024):
    chunks, size = [], 0
    for chunk in volume.read_file(path):
        size += len(chunk)
        if size > limit:
            raise ValueError("remote metadata exceeds limit")
        chunks.append(chunk)
    return json.loads(b"".join(chunks))


def durable_completion(volume, run_id, trial_id, digest):
    """A marker without independently committed matching files is not complete."""
    prefix = f"/{identifier(run_id)}/attempts/{identifier(trial_id)}"
    marker = read_remote(volume, prefix + "/complete.json")
    if (marker.get("run_id"), marker.get("trial_id"), marker.get("bundle_sha256")) != (run_id, trial_id, digest):
        raise ValueError("worker completion identity mismatch")
    if not isinstance(marker.get("files"), dict) or not marker["files"]:
        raise ValueError("worker completion has no evidence inventory")
    for relative, expected in marker["files"].items():
        inside(Path("/verified"), relative)
        checksum = hashlib.sha256()
        for chunk in volume.read_file(prefix + "/" + relative):
            checksum.update(chunk)
        if checksum.hexdigest() != expected:
            raise ValueError("worker evidence is not fully committed")
    return marker


def sandbox_name(run_id, trial_id):
    # Modal names are shorter than user run IDs; identity is also persisted.
    return "whip-eval-" + value_hash({"run": identifier(run_id), "trial": identifier(trial_id)})[:32]


def validate_settings(settings):
    allowed = {"environment", "app", "worker_image_id", "jobs", "shapes", "qualification_status",
               "qualification_receipt", "fixture", "model_capacity"}
    if not isinstance(settings, dict) or set(settings) - allowed:
        raise ValueError("unexpected cloud settings; credentials are forbidden")
    if settings.get("environment") != ENVIRONMENT or settings.get("app") != APP_NAME:
        raise ValueError("cloud execution is restricted to whipcode/whip-eval")
    if not re.fullmatch(r"im-[A-Za-z0-9]+", settings.get("worker_image_id", "")):
        raise ValueError("a qualified immutable Modal worker image ID is required")
    if not isinstance(settings.get("jobs"), int) or not 1 <= settings["jobs"] <= 90:
        raise ValueError("cloud jobs must be between 1 and 90")
    for runner in ("harbor", "pier"):
        shape = settings.get("shapes", {}).get(runner, {})
        for key in ("physical_cpus", "memory_mb", "min_free_disk_mb"):
            value = shape.get(key)
            if not isinstance(value, (int, float)) or isinstance(value, bool) or value <= 0:
                raise ValueError("qualified CPU, RAM and free-disk shapes are required")
    if settings.get("qualification_status") != "passed":
        raise ValueError("worker settings require explicit qualification")
    return settings


def launch_command(run_id, trial_id, digest, owner_id):
    # All interpolated values are strictly validated; no provider secret appears
    # in arguments, shell tracing, images or bundle contents.
    identifier(run_id)
    identifier(trial_id)
    if not re.fullmatch(r"[a-f0-9]{32}", owner_id):
        raise ValueError("invalid attempt owner identity")
    if not re.fullmatch(r"[a-f0-9]{64}", digest):
        raise ValueError("invalid bundle digest")
    claim = json.dumps({"run_id": run_id, "trial_id": trial_id, "bundle_sha256": digest, "owner_id": owner_id})
    return (f"printf '%s\\n' {shlex.quote(claim)} > /tmp/whip-eval-owner.json; "
            f"export WHIP_EVAL_OWNER_ID={owner_id}; dockerd --host=unix:///var/run/docker.sock >/tmp/dockerd.log 2>&1 & "
            "for n in $(seq 1 120); do docker info >/dev/null 2>&1 && break; sleep 1; done; "
            "docker info >/dev/null 2>&1 || exit 70; "
            f"python -c \"import hashlib, pathlib, sys, tarfile; "
            f"p=pathlib.Path('/input/{run_id}/bundle.tar'); "
            f"actual=hashlib.file_digest(p.open('rb'), 'sha256').hexdigest(); "
            f"actual == '{digest}' or sys.exit('bundle hash mismatch'); "
            "pathlib.Path('/work').mkdir(); tarfile.open(p).extractall('/work', filter='data')\" && "
            f"PYTHONPATH=/work/evals exec /.uv/.venv/bin/python -m whip_evals.modal_worker {run_id} {trial_id} {digest}")


def owned_worker(record, digest):
    """Resolve late creates without treating a deterministic name as ownership."""
    if record.get("bundle_sha256") != digest or not re.fullmatch(r"[a-f0-9]{32}", record.get("owner_id", "")):
        raise ValueError("attempt ownership identity missing")
    modal = sdk()
    sandbox = (modal.Sandbox.from_id(record["sandbox_id"]) if record.get("sandbox_id") else
               modal.Sandbox.from_name(APP_NAME, record["sandbox_name"], environment_name=ENVIRONMENT))
    if record.get("sandbox_id") and sandbox.object_id != record["sandbox_id"]:
        raise ValueError("resolved worker identity mismatch")
    expected = {"whip-eval.owner": APP_NAME, "whip-eval.run": record["run_id"],
                "whip-eval.trial": record["trial_id"], "whip-eval.bundle": digest,
                "whip-eval.claim": record["owner_id"]}
    tags = sandbox.get_tags()
    if all(tags.get(k) == v for k, v in expected.items()):
        return sandbox
    # Creation may succeed before the client receives its ID or applies tags.
    # The launch script writes the unpredictable claim to VM-local /tmp BEFORE
    # Docker boot. Shared Volume receipts cannot establish which VM owns it.
    claim = json.loads(sandbox.filesystem.read_text("/tmp/whip-eval-owner.json"))
    fields = ("run_id", "trial_id", "bundle_sha256", "owner_id")
    if not all(claim.get(k) == record.get(k) for k in fields):
        raise ValueError("worker ownership not proven")
    return sandbox


class ModalCampaign:
    """One controller owns dispatch; each worker owns its evidence export."""
    def __init__(self, run_id, digest, settings, state, inputs, evidence):
        self.run_id, self.digest = identifier(run_id), digest
        self.settings = validate_settings(settings)
        self.state, self.inputs, self.evidence = state, inputs, evidence
        self.cancelled = threading.Event()

    def attempt_key(self, trial_id):
        return self.run_id + "/attempt/" + identifier(trial_id)

    def execute(self, trial, cancelled):
        modal = sdk()
        key = self.attempt_key(trial["id"])
        name = sandbox_name(self.run_id, trial["id"])
        record = {"trial_id": trial["id"], "run_id": self.run_id, "sandbox_name": name,
                  "state": "creating", "created_at": utc_now(), "bundle_sha256": self.digest,
                  "sandbox_id": None, "owner_id": uuid4().hex, "cleanup_complete": False}
        # Durable intent precedes creation. A lost create response is uncertainty,
        # never permission to call Sandbox.create again.
        if not self.state.put(key, record, skip_if_exists=True):
            cancelled.set()
            return {"started": False, "cleanup": {"complete": False}, "error_code": "attempt_already_claimed"}
        sandbox = None
        try:
            app = modal.App.lookup(APP_NAME, environment_name=ENVIRONMENT)
            shape = self.settings["shapes"][trial["runner"]]
            sandbox = modal.Sandbox.create("bash", "-c", launch_command(self.run_id, trial["id"], self.digest, record["owner_id"]),
                app=app, name=name, image=modal.Image.from_id(self.settings["worker_image_id"]),
                cpu=(shape["physical_cpus"], shape["physical_cpus"]), memory=(shape["memory_mb"], shape["memory_mb"]),
                timeout=int(trial["outer_watchdog_seconds"]) + 1800,
                experimental_options={"vm_runtime": True},
                volumes={"/input": self.inputs.with_mount_options(read_only=True), "/evidence": self.evidence},
                secrets=[] if self.settings.get("fixture") else [modal.Secret.from_name(PROVIDER_SECRET, environment_name=ENVIRONMENT)])
            record.update(sandbox_id=sandbox.object_id, state="running")
            self.state.put(key, record)
            sandbox.set_tags({"whip-eval.owner": APP_NAME, "whip-eval.run": self.run_id,
                              "whip-eval.trial": trial["id"], "whip-eval.bundle": self.digest,
                              "whip-eval.claim": record["owner_id"]})
            cancel_at = None
            completed = None
            admitted = False
            deadline = time.monotonic() + trial["outer_watchdog_seconds"] + 1500
            while time.monotonic() < deadline:
                if cancelled.is_set() and cancel_at is None:
                    cancel_at = time.monotonic()
                    # Worker owns process-group/native cleanup and final evidence.
                    sandbox.filesystem.write_text("cancel\n", "/tmp/whip-eval-cancel")
                    record["state"] = "cancelling"
                    self.state.put(key, record)
                if not admitted and not cancelled.is_set():
                    try:
                        ready = read_remote(self.evidence, f"/{self.run_id}/attempts/{trial['id']}/ready.json")
                        if (ready.get("run_id"), ready.get("trial_id"), ready.get("bundle_sha256")) != (self.run_id, trial["id"], self.digest):
                            raise ValueError("worker admission identity mismatch")
                        sandbox.filesystem.write_text("admit\n", "/tmp/whip-eval-admitted")
                        record["admitted_at"] = utc_now()
                        self.state.put(key, record)
                        admitted = True
                    except (FileNotFoundError, modal.exception.NotFoundError):
                        pass
                try:
                    completed = durable_completion(self.evidence, self.run_id, trial["id"], self.digest)
                    break
                except (FileNotFoundError, ValueError, modal.exception.NotFoundError):
                    pass
                if sandbox.poll() is not None:
                    # Background commits can become visible just after process exit.
                    if record.get("exited_at") is None:
                        record["exited_at"] = time.monotonic()
                    if time.monotonic() - record["exited_at"] > EXPORT_SECONDS:
                        break
                if cancel_at and time.monotonic() - cancel_at > EXPORT_SECONDS:
                    break
                time.sleep(POLL_SECONDS)
            if completed is not None:
                if completed.get("security_failure") or not completed.get("integrity_complete"):
                    cancelled.set()
                    self.state.put(self.run_id + "/cancel", {"reason": "evidence_integrity_failure", "at": utc_now()}, skip_if_exists=True)
                record.update(state="security_failure" if completed.get("security_failure") else "completed", completion=completed,
                              evidence_complete=completed.get("integrity_complete", False))
                sandbox = owned_worker(record, self.digest)
                exit_code = sandbox.poll()
                if exit_code is None:
                    try:
                        process = sandbox.exec("touch", "/tmp/whip-eval-exported")
                        process.wait()
                        record["export_ack_status"] = "acknowledged"
                    except modal.exception.NotFoundError:
                        # NotFound is not exit proof: re-prove this exact worker.
                        sandbox = owned_worker(record, self.digest)
                        exit_code = sandbox.poll()
                        if type(exit_code) is not int:
                            raise
                        record["export_ack_status"] = "obsolete_after_not_found"
                elif type(exit_code) is int:
                    record["export_ack_status"] = "obsolete_actual_exit"
                else:
                    raise ValueError("worker poll did not return an actual exit code or None")
                if type(exit_code) is int:
                    record["actual_exit_code"] = exit_code
            else:
                record.update(state="interrupted" if cancelled.is_set() else "indeterminate", evidence_complete=False)
        except Exception as error:
            record.update(state="indeterminate", error_code=type(error).__name__)
            cancelled.set()
            if sandbox is None:
                # Reconcile the *same* owned name once; never recreate it.
                try:
                    sandbox = owned_worker(record, self.digest)
                    record["sandbox_id"] = sandbox.object_id
                except Exception:
                    pass
        finally:
            if sandbox is not None:
                try:
                    sandbox = owned_worker(record, self.digest)
                    try:
                        exit_code = sandbox.poll()
                    except Exception as error:
                        record["cleanup_poll_error"] = type(error).__name__
                        exit_code = None  # Unknown still needs termination and a final actual poll.
                    if type(exit_code) is not int:
                        sandbox.terminate()
                        sandbox.wait(raise_on_termination=False)
                        exit_code = sandbox.poll()
                    record["cleanup_complete"] = type(exit_code) is int
                    if record["cleanup_complete"]:
                        record["actual_exit_code"] = exit_code
                except Exception as error:
                    record["cleanup_error"] = type(error).__name__
            record["finished_at"] = utc_now()
            self.state.put(key, record)
        if not record["cleanup_complete"]:
            cancelled.set()
        return {"started": bool(record["sandbox_id"]), "cleanup": {"complete": record["cleanup_complete"]},
                "cloud": record}


def controller_revision():
    return value_hash({name: file_hash(Path(__file__).with_name(name))
                       for name in ("modal_cloud.py", "common.py", "execution.py")})


def coordinate(run_id, digest, settings, expected_controller_sha256):
    modal = sdk()
    state, inputs, evidence = resources()
    run_id = identifier(run_id)
    request = state.get(run_id + "/request") or {}
    revision = controller_revision()
    if (request.get("bundle_sha256") != digest or request.get("settings") != settings
            or request.get("controller_source_sha256") != expected_controller_sha256
            or expected_controller_sha256 != revision):
        result = {"run_id": run_id, "status": "failed", "error_code": "deployment_identity_mismatch",
                  "controller_source_sha256": revision, "expected_controller_sha256": expected_controller_sha256,
                  "bundle_sha256": digest, "worker_image_id": settings["worker_image_id"], "started_trials": 0,
                  "automatic_retry": False, "finished_at": utc_now()}
        state.put(run_id + "/status", result)
        return result
    call_id = modal.current_function_call_id()
    if not state.put(run_id + "/controller", {"call_id": call_id, "started_at": utc_now(), "controller_source_sha256": revision,
            "expected_controller_sha256": expected_controller_sha256, "bundle_sha256": digest,
            "worker_image_id": settings["worker_image_id"]}, skip_if_exists=True):
        return {"run_id": run_id, "status": "already_claimed"}
    controller = ModalCampaign(run_id, digest, settings, state, inputs, evidence)
    state.put(run_id + "/status", {"status": "running", "call_id": call_id, "started_at": utc_now()})
    stop_watcher = threading.Event()
    def watch_cancel():
        while not stop_watcher.wait(2):
            try:
                if state.get(run_id + "/cancel"):
                    controller.cancelled.set()
            except Exception:
                controller.cancelled.set()
    watcher = threading.Thread(target=watch_cancel, daemon=True)
    watcher.start()
    stats = {}
    try:
        manifest = read_remote(inputs, f"/{run_id}/manifest.json")
        schedules = read_remote(inputs, f"/{run_id}/schedule.json")
        trials = [{**t, "outer_watchdog_seconds": schedules[t["id"]]["envelope"]["outer_watchdog_seconds"]}
                  for t in manifest["schedule"]]
        from .execution import run_pool
        capacity = {key: sum(t["resources"][key] for t in trials) for key in ("cpus", "memory_mb", "storage_mb")}
        def collect(trial, result):
            state.put(run_id + "/progress/" + trial["id"], {"id": trial["id"],
                       "finished_at": utc_now(), "started": result.get("started", False),
                       "cleanup_complete": result.get("cleanup", {}).get("complete", False)})
        outcomes = run_pool(trials, capacity, settings["jobs"], controller.execute,
                            cancelled=controller.cancelled, on_result=collect, stats=stats)
        status = "partial" if controller.cancelled.is_set() else "completed"
        result = {"run_id": run_id, "status": status, "finished_at": utc_now(),
                  "planned": len(trials), "recorded": len(outcomes), "concurrency": stats,
                  "infra_cost_usd": None, "infra_cost_status": "not_reconciled"}
    except Exception as error:
        controller.cancelled.set()
        result = {"run_id": run_id, "status": "failed", "error_code": type(error).__name__, "finished_at": utc_now()}
    finally:
        stop_watcher.set()
        watcher.join(timeout=3)
    state.put(run_id + "/status", result)
    return result


# Only the deployed coordinator has Modal authority. A VM gets the provider-only
# secret and file mounts; it never gets a Modal account token.
modal = sdk()
app = modal.App(APP_NAME)
coordinator_image = (modal.Image.debian_slim(python_version="3.12")
    .uv_sync(str(EVALS), frozen=True, uv_version="0.12.13")
    .add_local_python_source("whip_evals"))
coordinator = app.function(image=coordinator_image, timeout=86400, retries=0)(coordinate)

"""Modal control plane: one detached coordinator, one disposable VM per trial.

SDK objects are lazy; importing this module never contacts Modal. Deployment is
explicit (`modal deploy -m whip_evals.modal_cloud --env whipcode`).
"""
import json
import os
from pathlib import Path
import re
import threading
import time

from .common import EVALS, identifier, read_json, utc_now, value_hash
from .modal_bundle import MAX_JOBS, controller_revision

ENVIRONMENT = "whipcode"
APP_NAME = "whip-eval"
INPUT_VOLUME = "whip-eval-inputs"
EVIDENCE_VOLUME = "whip-eval-evidence"
STATE_NAME = "whip-eval-state"
PROVIDER_SECRET = "whip-eval-inference"
CONFIG_PATH = EVALS / "frontier" / "modal.json"
POLL_SECONDS = 5
# Volume commits are asynchronous: a marker written just before VM exit can
# become readable a little after the exit is observed.
MARKER_GRACE_SECONDS = 120


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


def completion_marker(volume, run_id, trial_id, digest):
    """The worker's final marker; fetch verifies the listed file hashes locally."""
    marker = read_remote(volume, f"/{identifier(run_id)}/attempts/{identifier(trial_id)}/complete.json")
    if (marker.get("run_id"), marker.get("trial_id"), marker.get("bundle_sha256")) != (run_id, trial_id, digest):
        raise ValueError("worker completion identity mismatch")
    return marker


def sandbox_name(run_id, trial_id):
    # Modal names are shorter than user run IDs; identity is also persisted.
    return "whip-eval-" + value_hash({"run": identifier(run_id), "trial": identifier(trial_id)})[:32]


def load_config(path=CONFIG_PATH):
    return validate_settings(read_json(path))


def validate_settings(settings):
    """Non-secret worker placement: image, VM shape per runner, concurrency cap."""
    allowed = {"environment", "app", "worker_image_id", "shapes", "max_jobs", "jobs"}
    if not isinstance(settings, dict) or set(settings) - allowed:
        raise ValueError("unexpected cloud settings; credentials are forbidden")
    if settings.get("environment") != ENVIRONMENT or settings.get("app") != APP_NAME:
        raise ValueError("cloud execution is restricted to whipcode/whip-eval")
    if not re.fullmatch(r"im-[A-Za-z0-9]+", settings.get("worker_image_id", "")):
        raise ValueError("an immutable Modal worker image ID is required")
    max_jobs = settings.get("max_jobs", MAX_JOBS)
    if type(max_jobs) is not int or not 1 <= max_jobs <= MAX_JOBS:
        raise ValueError("cloud max_jobs must be between 1 and 90")
    jobs = settings.get("jobs", max_jobs)
    if type(jobs) is not int or not 1 <= jobs <= max_jobs:
        raise ValueError("cloud jobs must be between 1 and max_jobs")
    for runner in ("harbor", "pier"):
        shape = settings.get("shapes", {}).get(runner, {})
        if set(shape) != {"physical_cpus", "memory_mb", "min_free_disk_mb"}:
            raise ValueError("cloud shapes need physical_cpus, memory_mb and min_free_disk_mb per runner")
        for value in shape.values():
            if type(value) not in (int, float) or value <= 0:
                raise ValueError("cloud shape values must be positive numbers")
    return {**settings, "max_jobs": max_jobs, "jobs": jobs}


def launch_command(run_id, trial_id, digest):
    # All interpolated values are strictly validated; no provider secret appears
    # in arguments, shell tracing, images or bundle contents.
    identifier(run_id)
    identifier(trial_id)
    if not re.fullmatch(r"[a-f0-9]{64}", digest):
        raise ValueError("invalid bundle digest")
    return ("export WHIP_EVAL_CLOUD=1; dockerd --host=unix:///var/run/docker.sock >/tmp/dockerd.log 2>&1 & "
            "for n in $(seq 1 120); do docker info >/dev/null 2>&1 && break; sleep 1; done; "
            "docker info >/dev/null 2>&1 || exit 70; "
            f"python -c \"import hashlib, pathlib, sys, tarfile; "
            f"p=pathlib.Path('/input/{run_id}/bundle.tar'); "
            f"actual=hashlib.file_digest(p.open('rb'), 'sha256').hexdigest(); "
            f"actual == '{digest}' or sys.exit('bundle hash mismatch'); "
            "pathlib.Path('/work').mkdir(); tarfile.open(p).extractall('/work', filter='data')\" && "
            f"PYTHONPATH=/work/evals exec /.uv/.venv/bin/python -m whip_evals.modal_worker {run_id} {trial_id} {digest}")


def worker_tags(run_id, trial_id, digest):
    return {"whip-eval.owner": APP_NAME, "whip-eval.run": run_id,
            "whip-eval.trial": trial_id, "whip-eval.bundle": digest}


class ModalCampaign:
    """One controller owns dispatch; each worker owns its evidence export."""
    def __init__(self, run_id, digest, settings, state, inputs, evidence):
        self.run_id, self.digest = identifier(run_id), digest
        self.settings = validate_settings(settings)
        self.state, self.inputs, self.evidence = state, inputs, evidence
        self.cancelled = threading.Event()

    def attempt_key(self, trial_id):
        return self.run_id + "/attempt/" + identifier(trial_id)

    def wait_for_marker(self, trial_id):
        modal = sdk()
        deadline = time.monotonic() + MARKER_GRACE_SECONDS
        while True:
            try:
                return completion_marker(self.evidence, self.run_id, trial_id, self.digest)
            except (FileNotFoundError, ValueError, modal.exception.NotFoundError):
                if time.monotonic() >= deadline:
                    return None
                time.sleep(POLL_SECONDS)

    def execute(self, trial, cancelled):
        """Run one trial VM to exit. Never cancels the campaign; a human does."""
        modal = sdk()
        key = self.attempt_key(trial["id"])
        name = sandbox_name(self.run_id, trial["id"])
        record = {"trial_id": trial["id"], "run_id": self.run_id, "sandbox_name": name,
                  "state": "creating", "created_at": utc_now(), "bundle_sha256": self.digest,
                  "sandbox_id": None, "cleanup_complete": False}
        # Durable intent precedes creation: a paid attempt is never launched twice.
        if not self.state.put(key, record, skip_if_exists=True):
            return {"started": False, "cleanup": {"complete": False}, "error_code": "attempt_already_claimed"}
        sandbox = None
        exit_code = None
        try:
            app = modal.App.lookup(APP_NAME, environment_name=ENVIRONMENT)
            shape = self.settings["shapes"][trial["runner"]]
            sandbox = modal.Sandbox.create("bash", "-c", launch_command(self.run_id, trial["id"], self.digest),
                app=app, name=name, image=modal.Image.from_id(self.settings["worker_image_id"]),
                cpu=(shape["physical_cpus"], shape["physical_cpus"]), memory=(shape["memory_mb"], shape["memory_mb"]),
                timeout=int(trial["outer_watchdog_seconds"]) + 1800,
                experimental_options={"vm_runtime": True},
                volumes={"/input": self.inputs.with_mount_options(read_only=True), "/evidence": self.evidence},
                secrets=[modal.Secret.from_name(PROVIDER_SECRET, environment_name=ENVIRONMENT)])
            record.update(sandbox_id=sandbox.object_id, state="running")
            self.state.put(key, record)
            sandbox.set_tags(worker_tags(self.run_id, trial["id"], self.digest))
            # Modal ends the VM at the sandbox timeout above; poll until it exits.
            while (exit_code := sandbox.poll()) is None:
                if cancelled.is_set() and record["state"] != "cancelling":
                    # The worker owns native cleanup and its final evidence export.
                    sandbox.filesystem.write_text("cancel\n", "/tmp/whip-eval-cancel")
                    record.update(state="cancelling", cancel_sent_at=utc_now())
                    self.state.put(key, record)
                time.sleep(POLL_SECONDS)
            marker = self.wait_for_marker(trial["id"])
            record.update(state="completed" if marker else "exited_without_marker",
                          worker_status=(marker or {}).get("status"))
        except Exception as error:
            record.update(state="controller_error", error_code=type(error).__name__)
        finally:
            if sandbox is not None:
                try:
                    if exit_code is None:  # the controller failed mid-flight: end the VM
                        sandbox.terminate()
                        sandbox.wait(raise_on_termination=False)
                        exit_code = sandbox.poll()
                    record["cleanup_complete"] = type(exit_code) is int
                    record["worker_exit_code"] = exit_code
                except Exception as error:
                    record["cleanup_error"] = type(error).__name__
            record["finished_at"] = utc_now()
            self.state.put(key, record)
        return {"started": bool(record["sandbox_id"]), "cleanup": {"complete": record["cleanup_complete"]},
                "cloud": record}


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
                  "bundle_sha256": digest, "finished_at": utc_now()}
        state.put(run_id + "/status", result)
        return result
    call_id = modal.current_function_call_id()
    if not state.put(run_id + "/controller", {"call_id": call_id, "started_at": utc_now(),
            "controller_source_sha256": revision, "bundle_sha256": digest,
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
                pass  # A transient state read error is not a cancellation.
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
        outcomes = run_pool(trials, capacity, settings["jobs"], controller.execute,
                            cancelled=controller.cancelled, stats=stats)
        result = {"run_id": run_id, "status": "cancelled" if controller.cancelled.is_set() else "completed",
                  "finished_at": utc_now(), "planned": len(trials), "recorded": len(outcomes),
                  "unproven_vm_exits": sum(not o.get("cleanup", {}).get("complete") for o in outcomes.values()),
                  "concurrency": stats}
    except Exception as error:
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

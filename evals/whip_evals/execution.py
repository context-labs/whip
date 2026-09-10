"""One native runner job per subprocess; process and container ownership helpers."""
import concurrent.futures
import json
import math
import os
from pathlib import Path
import random
import signal
import subprocess
import sys
import threading
import time
import tomllib

from .adapter import OBSERVER_GRACE_SECONDS, CLEANUP_TIMEOUT_SECONDS
from .common import read_json, write_json


def schedule(tasks, candidates, repetitions, seed):
    rng = random.Random(seed)
    first = [index % len(candidates) for index in range(len(tasks))]
    rng.shuffle(first)
    first_by_task = {task["id"]: arm for task, arm in zip(tasks, first)}
    result = []
    for repetition in range(1, repetitions + 1):
        order = list(tasks)
        rng.shuffle(order)
        for task in order:
            arms = list(candidates)
            if len(arms) == 2 and (first_by_task[task["id"]] + repetition - 1) % 2:
                arms.reverse()
            for candidate in arms:
                result.append({"id": f"t{len(result) + 1:04d}", "task_id": task["id"],
                    "candidate_id": candidate["id"], "engine": candidate["engine"],
                    "binary_sha256": candidate["binary_sha256"], "repetition": repetition,
                    "runner": task["runner"], "resources": task["resources"]})
    return result


def run_pool(trials, capacity, jobs, worker, *, cancelled=None, on_result=None, stats=None):
    """Central admission; workers never reserve resources while holding others."""
    cancelled = cancelled or threading.Event()
    for trial in trials:
        if any(trial["resources"][key] > capacity[key] for key in capacity):
            raise ValueError("task cannot fit available host capacity: " + trial["task_id"])
    available = dict(capacity)
    pending = list(trials)
    active, results = {}, {}
    queued_at, queue_times = time.monotonic(), {}
    stats = stats if stats is not None else {}
    stats.update(peak_active_trials=0)
    with concurrent.futures.ThreadPoolExecutor(max_workers=jobs) as pool:
        try:
            while pending or active:
                while pending and not cancelled.is_set() and len(active) < jobs:
                    trial = pending[0]
                    if any(trial["resources"][k] > available[k] for k in capacity):
                        break
                    pending.pop(0)
                    for key in available:
                        available[key] -= trial["resources"][key]
                    queue_times[trial["id"]] = time.monotonic() - queued_at
                    active[pool.submit(worker, trial, cancelled)] = trial
                    stats["peak_active_trials"] = max(stats["peak_active_trials"], len(active))
                if not active:
                    break
                done, _ = concurrent.futures.wait(active, timeout=.2, return_when=concurrent.futures.FIRST_COMPLETED)
                for future in done:
                    trial = active.pop(future)
                    try:
                        result = future.result()
                    except Exception as error:
                        result = {"error_code": type(error).__name__, "started": False,
                                  "cleanup": {"complete": False}}
                    results[trial["id"]] = result
                    result.setdefault("phase_seconds", {})["queue"] = queue_times[trial["id"]]
                    if result.get("cleanup", {}).get("complete") is not True:
                        cancelled.set()  # Unproven resources remain reserved.
                    else:
                        for key in available:
                            available[key] += trial["resources"][key]
                    if on_result:
                        on_result(trial, result)
        except BaseException:
            cancelled.set()  # Signal workers before executor shutdown waits for them.
            raise
    for trial in pending:
        results[trial["id"]] = {"started": False, "cancelled": True,
                                "cleanup": {"complete": True}, "error_code": "dispatch_stopped"}
    return results


def job_config(trial, task, binary, contract, task_path, job_dir, *, fixture=False):
    envelope = timing_envelope(task_path, trial["runner"], task["agent_seconds"])
    kwargs = {"engine": trial["engine"], "binary": str(binary), "timeout": task["agent_seconds"],
              "max_cost": 0, "max_tokens": 0, "max_turns": 0, "max_output": 0,
              "native_defaults": True, "commit": trial["runner"] == "pier", "fixture": fixture}
    if contract:
        kwargs["contract"] = str(contract)
    config = {"job_name": trial["id"], "jobs_dir": str(job_dir), "n_attempts": 1,
              "n_concurrent_trials": 1, "retry": {"max_retries": 0},
              "environment": {"type": "docker", "delete": True},
              "agents": [{"import_path": "whip_evals.adapter:" + ("HarborWhip" if trial["runner"] == "harbor" else "PierWhip"),
                          "model_name": "inference-net/kimi-k3",
                          "override_timeout_sec": envelope["agent_runner_seconds"],
                          "override_setup_timeout_sec": AGENT_SETUP_SECONDS, "kwargs": kwargs}],
              "tasks": [{"path": str(task_path)}]}
    # Validate with the pinned native parser without starting a job.
    from importlib import import_module
    model = import_module(trial["runner"] + ".models.job.config").JobConfig
    model.model_validate(config)
    return config, envelope


def stop_process(process):
    if process.poll() is not None:
        return
    try:
        os.killpg(process.pid, signal.SIGINT)
        process.wait(timeout=60)
    except subprocess.TimeoutExpired:
        os.killpg(process.pid, signal.SIGKILL)
        process.wait(timeout=15)
    except ProcessLookupError:
        process.wait(timeout=15)


def execute_job(trial, config, envelope, directory, cancelled, *, evals):
    directory = Path(directory)
    directory.mkdir(parents=True, exist_ok=False)
    path = directory / "runner-config.json"
    write_json(path, config, exclusive=True)
    record = {"started": False, "job_path": str(Path(config["jobs_dir"]) / config["job_name"]),
              "cleanup": {"complete": False}, "phase_seconds": {}}
    started = time.monotonic()
    process = None
    try:
        env = {**os.environ, "PYTHONPATH": str(evals), "DOCKER_DEFAULT_PLATFORM": "linux/amd64"}
        runner = Path(sys.executable).parent / trial["runner"]
        with (directory / "runner.log").open("w") as log:
            process = subprocess.Popen([str(runner), "run", "--config", str(path)], env=env,
                stdout=log, stderr=subprocess.STDOUT, start_new_session=True)
            record["started"] = True
            write_json(directory / "process.json", {"pid": process.pid, "job_path": record["job_path"]}, exclusive=True)
            while process.poll() is None:
                if cancelled.wait(.2):
                    record["cancelled"] = True
                    stop_process(process)
                    break
                if time.monotonic() - started > envelope["outer_watchdog_seconds"]:
                    record["outer_watchdog"] = True
                    stop_process(process)
                    break
            record["runner_exit_code"] = process.returncode
    except Exception as error:
        record["error_code"] = type(error).__name__
    finally:
        process_stopped = True
        if process is not None:
            try:
                stop_process(process)
            except (OSError, subprocess.SubprocessError) as error:
                process_stopped = False
                record["error_code"] = type(error).__name__
        cleanup_start = time.monotonic()
        job = Path(record["job_path"])
        if process is None:
            record["cleanup"] = {"complete": True, "errors": []}
        else:
            record["cleanup"] = cleanup_owned_containers(job, trial["runner"])
        if not process_stopped:
            record["cleanup"]["complete"] = False
            record["cleanup"].setdefault("errors", []).append("runner_process_not_stopped")
        record["phase_seconds"]["cleanup"] = time.monotonic() - cleanup_start
        record["trial_seconds"] = time.monotonic() - started
        write_json(directory / "execution.json", record, exclusive=True)
    return record

# These envelopes describe the pinned Harbor 0.22.0 / Pier 0.3.1 single-step
# runners. Native task verifier limits are read, never overridden.
AGENT_SETUP_SECONDS = 600
RUNNER_GUARD_SECONDS = 60


def native_agent_timeout(path):
    task = tomllib.loads((Path(path) / "task.toml").read_text())
    timeout = task["agent"]["timeout_sec"]
    if not math.isfinite(timeout) or timeout <= 0 or int(timeout) != timeout:
        raise ValueError("native agent timeout must be positive integral seconds")
    return int(timeout)


def timing_envelope(path, runner, timeout):
    task = tomllib.loads((Path(path) / "task.toml").read_text())
    if task.get("steps"):
        raise ValueError("timing envelope supports the selected single-step tasks only")
    build = float(task.get("environment", {}).get("build_timeout_sec", 600))
    verifier = task.get("verifier", {})
    verifier_timeout = float(verifier.get("timeout_sec", 600))
    attempts = 2 if runner == "pier" else 1
    collect = sum(float(hook.get("timeout_sec", 60)) for hook in verifier.get("collect", []))
    # Pier's whole _verify_once (including separate startup) is timed and can
    # retry once. Harbor times separate startup independently before verify().
    separate_build = build if runner == "harbor" and verifier.get("environment_mode") == "separate" else 0
    pre_artifacts = 300 if runner == "pier" and (Path(path) / "pre_artifacts.sh").exists() else 0
    agent = timeout + OBSERVER_GRACE_SECONDS + CLEANUP_TIMEOUT_SECONDS + RUNNER_GUARD_SECONDS
    # Native artifact transfers and shielded environment stops have no global
    # deadlines. Reserve explicit slack, then use the outer watchdog for hangs.
    untimed_allowance = CLEANUP_TIMEOUT_SECONDS * (2 + attempts)
    result = {"environment_build_seconds": build, "agent_setup_seconds": AGENT_SETUP_SECONDS,
              "observer_exec_seconds": timeout + OBSERVER_GRACE_SECONDS,
              "adapter_cleanup_seconds": CLEANUP_TIMEOUT_SECONDS,
              "agent_runner_seconds": agent, "collect_seconds": collect,
              "pre_artifacts_seconds": pre_artifacts,
              "native_verifier_seconds_per_attempt": verifier_timeout,
              "native_verifier_attempts": attempts,
              "native_verifier_retry_delay_seconds": 1 if attempts == 2 else 0,
              "separate_verifier_build_outside_timeout_seconds": separate_build,
              "untimed_transfer_teardown_allowance_seconds": untimed_allowance,
              "runner_guard_seconds": RUNNER_GUARD_SECONDS,
              "watchdog_signal_grace_seconds": 60, "watchdog_kill_grace_seconds": 15,
              "watchdog_owned_container_cleanup_seconds": CLEANUP_TIMEOUT_SECONDS}
    result["outer_watchdog_seconds"] = (build + AGENT_SETUP_SECONDS + agent + collect + pre_artifacts
        + attempts * verifier_timeout + result["native_verifier_retry_delay_seconds"]
        + separate_build + untimed_allowance + RUNNER_GUARD_SECONDS)
    return result


def cleanup_owned_containers(job_dir, runner):
    """After an outer watchdog only, remove verified IDs from this one job.

    Both the runner-created random project identity and the exact host log
    mount must agree. Never remove images, networks, volumes or other projects.
    A failed ownership check/timeout is recorded and dispatch remains halted.
    """
    from importlib import import_module
    from types import SimpleNamespace

    started = time.monotonic()
    report = {"removed_container_ids": [], "errors": [], "timeout_seconds": CLEANUP_TIMEOUT_SECONDS}

    def docker(*args):
        remaining = CLEANUP_TIMEOUT_SECONDS - (time.monotonic() - started)
        if remaining <= 0:
            raise TimeoutError("owned-container cleanup deadline")
        return subprocess.run(["docker", *args], capture_output=True, text=True,
                              check=True, timeout=min(30, remaining)).stdout

    try:
        job_dir = Path(job_dir).resolve()
        configs = list(job_dir.glob("*/config.json"))
        if len(configs) != 1:
            raise ValueError("expected one runner-owned trial config")
        config_path = configs[0]
        config = json.loads(config_path.read_text())
        trial_dir = config_path.parent.resolve()
        if config.get("trial_name") != trial_dir.name or Path(config["trials_dir"]).resolve() != job_dir:
            raise ValueError("runner config does not belong to this job directory")
        # Use the installed, pinned runners' project naming, including their
        # length-limited separate-verifier session name; no prefix matching.
        trial_type = import_module(runner + ".trial.trial").Trial
        sanitize = import_module(runner + ".environments.docker.docker")._sanitize_docker_compose_project_name
        owner = SimpleNamespace(config=SimpleNamespace(trial_name=trial_dir.name))
        verifier_session = trial_type._separate_verifier_session_id(owner, "trial")
        agent_session = trial_dir.name + ("__env" if runner == "harbor" else "")
        projects = {sanitize(agent_session), sanitize(verifier_session)}
        expected_mounts = {(str(trial_dir / "agent"), "/logs/agent"),
                           (str(trial_dir / "verifier"), "/logs/verifier")}
        # Export only identity/mounts; Docker inspect's environment can contain
        # authenticated proxy credentials and must not enter this report.
        inspect_format = ('{"id":{{json .Id}},"project":{{json (index .Config.Labels '
                          '"com.docker.compose.project")}},"mounts":{{json .Mounts}}}')
        for project in sorted(projects):
            ids = docker("ps", "-aq", "--filter", "label=com.docker.compose.project=" + project).split()
            for container_id in ids:
                identity = json.loads(docker("inspect", "--format", inspect_format, container_id))
                mounts = {(str(Path(m.get("Source", "")).resolve()), m.get("Destination"))
                          for m in identity.get("mounts", []) if m.get("Type") == "bind"}
                if identity.get("project") != project or not expected_mounts.intersection(mounts):
                    raise ValueError("container ownership proof failed")
                docker("rm", "-f", identity["id"])
                report["removed_container_ids"].append(identity["id"])
            if docker("ps", "-aq", "--filter", "label=com.docker.compose.project=" + project).strip():
                raise RuntimeError("owned project still has containers")
        report["complete"] = True
    except Exception as error:
        # Do not persist subprocess stderr or commands containing credentials.
        report["errors"].append(type(error).__name__)
        report["complete"] = False
    report["elapsed_seconds"] = time.monotonic() - started
    return report

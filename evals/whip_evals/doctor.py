"""Read-only prerequisites by default; explicit authored integration and cleanup."""
import os
from pathlib import Path
import shutil
import threading

from .common import EVALS, identifier, inside, new_id, read_json, write_json
from .execution import cleanup_owned_containers, execute_job, job_config
from .prepare import build_candidate, environment
from .report import normalize_trial
from .tasks import load_spec


def doctor(*, integration=False, evals=EVALS):
    lock, profiles, protocol = load_spec(evals)
    checks = {name: shutil.which(name) is not None for name in ("git", "go", "docker", "uv")}
    details = {"tasks": len(lock["tasks"]), "profiles": {k: len(v["task_ids"]) for k, v in profiles.items()},
               "provider_key_present": bool(os.environ.get("INFERENCE_API_KEY")), "model_calls": 0}
    try:
        host = environment(evals)
        checks["runner_versions"] = host["runner_versions"] == protocol["runner_versions"]
        checks["linux_docker"] = host["os"] == "linux"
        details["environment"] = host
    except Exception as error:
        checks["docker_environment"] = False
        details["environment_error"] = type(error).__name__
    if integration and all(checks.values()):
        details["integration"] = integration_check(evals)
        checks["integration"] = details["integration"]["passed"]
    return {"status": "ready" if all(checks.values()) else "failed", "checks": checks, **details}


def integration_check(evals=EVALS):
    """Both runners/engines and verifier modes; deterministic provider, never scored."""
    run_id = "doctor-" + new_id()
    root = Path(evals).resolve() / "artifacts" / run_id
    root.mkdir(parents=True, exist_ok=False)
    candidate = build_candidate("fixture", "starlark", evals=evals)
    outcomes = []
    for runner, engine, mode in ((r, e, m) for r in ("harbor", "pier")
                                for e in ("starlark", "quickjs") for m in ("shared", "separate")):
        trial = {"id": f"doctor-{runner}-{engine}-{mode}", "engine": engine, "runner": runner,
                 "task_id": "fixture/qualification", "candidate_id": engine, "repetition": 1,
                 "binary_sha256": candidate["binary_sha256"]}
        path = prepare_fixture(root / "tasks" / trial["id"], mode)
        config, envelope = job_config(trial, {"agent_seconds": 120}, inside(evals, candidate["binary_path"]),
            None, path, root / "jobs", fixture=True)
        record = execute_job(trial, config, envelope, root / trial["id"], threading.Event(), evals=evals)
        record["job_path"] = Path(record["job_path"]).relative_to(root).as_posix()
        row = normalize_trial(trial, record, root)
        passed = (row["success"] is True and row["accounting_complete"] and row["evidence_complete"]
                  and row["diagnostics"]["agent_count"] == 2 and row["model_calls"] >= 4)
        outcomes.append({"runner": runner, "engine": engine, "verifier_mode": mode, "passed": passed})
        if not record["cleanup"]["complete"]:
            break
    result = {"passed": len(outcomes) == 8 and all(row["passed"] for row in outcomes), "trials": outcomes,
              "model_calls_to_external_provider": 0, "excluded_from_scores": True, "artifact_root": str(root)}
    write_json(root / "doctor.json", result, exclusive=True)
    return result


def prepare_fixture(path, mode):
    path = Path(path)
    shutil.copytree(EVALS / "fixtures" / "native", path)
    if mode == "separate":
        task = path / "task.toml"
        task.write_text(task.read_text().replace("[verifier]\n", '[verifier]\nenvironment_mode = "separate"\n'))
        dockerfile = (path / "environment" / "Dockerfile").read_text()
        (path / "tests" / "Dockerfile").write_text(dockerfile + "\nCOPY test.sh /tests/test.sh\n")
    return path


def cleanup(run_id, *, evals=EVALS):
    run_id = identifier(run_id)
    manifest = read_json(Path(evals) / "reports" / run_id / "manifest.json")
    if manifest["run_id"] != run_id:
        raise ValueError("run identity mismatch")
    root = inside(evals, manifest["artifact_root"])
    outcomes = []
    for trial in manifest["schedule"]:
        process_path = root / "trials" / trial["id"] / "process.json"
        if not process_path.exists():
            continue
        process = read_json(process_path)
        try:
            os.kill(process["pid"], 0)
        except ProcessLookupError:
            pass
        else:
            raise ValueError("recorded runner PID is still present; cleanup refuses active or reused PIDs")
        job = root / "jobs" / trial["id"]
        if Path(process["job_path"]).resolve() != job.resolve():
            raise ValueError("process record does not own this trial path")
        outcomes.append({"trial_id": trial["id"], **cleanup_owned_containers(job, trial["runner"])})
    result = {"run_id": run_id, "status": "complete" if all(r["complete"] for r in outcomes) else "failed", "trials": outcomes}
    write_json(root / ("cleanup-" + new_id() + ".json"), result, exclusive=True)
    return result

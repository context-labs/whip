"""Conservative baseline policy and a single-host atomic accepted pointer."""
import fcntl
from pathlib import Path

from .common import (EVALS, file_hash, identifier, inside, read_json, utc_now,
                     value_hash, write_json)
from .report import build_result


def track_path(evals, track):
    parts = track.split("/")
    if len(parts) != 2:
        raise ValueError("baseline track must contain protocol/model identifiers")
    return Path(evals) / "baselines" / "/".join(identifier(part) for part in parts)


def current(evals, track):
    path = track_path(evals, track) / "current.json"
    return read_json(path) if path.exists() else None


def load_accepted(evals, pointer):
    if pointer is None:
        return None
    directory = Path(evals) / "reports" / identifier(pointer["run_id"])
    if file_hash(directory / "result.json") != pointer["result_sha256"]:
        raise ValueError("accepted baseline result changed")
    if file_hash(directory / "manifest.json") != pointer["manifest_sha256"]:
        raise ValueError("accepted baseline manifest changed")
    return read_json(directory / "manifest.json"), read_json(directory / "result.json")


def evaluate(manifest, result, *, previous=None):
    policy = manifest["promotion_policy"]
    canonical = build_result(manifest, result["trials"], finished_at=result["finished_at"], wall_seconds=result.get("wall_seconds"))
    gates = {}
    gates["explicit_promotion_campaign"] = manifest.get("promote") is True
    gates["full_profile"] = manifest["profile"] == "full" and len(manifest["task_ids"]) == 30
    gates["three_repetitions"] = manifest["repetitions"] == policy["repetitions"] == 3
    gates["clean_reproducible_builds"] = all(c.get("dirty") is False and c.get("binary_sha256") and c.get("source_sha256") for c in manifest["candidates"])
    gates["manifest_identity"] = result["manifest_sha256"] == value_hash(manifest)
    gates["summary_consistency"] = result["arms"] == canonical["arms"] and result["comparisons"] == canonical["comparisons"]
    gates["complete_evidence"] = (result["status"] == "complete" and len(result["trials"]) == len(manifest["schedule"])
        and all(r["started"] and r["success"] is not None and r["evidence_complete"]
                and r["accounting_complete"] and r["cleanup_complete"] is True
                and not r["error_codes"] for r in result["trials"]))
    gates["integrity_verified"] = result.get("integrity", {}).get("complete") is True
    candidate = manifest["candidates"][-1]["id"]
    gates["expected_matrix"] = all(
        {(r["task_id"], r["repetition"]) for r in result["trials"] if r["candidate_id"] == c["id"]}
        == {(task, rep) for task in manifest["task_ids"] for rep in range(1, 4)}
        for c in manifest["candidates"])
    if previous is None:
        gates["initial_single_candidate"] = len(manifest["candidates"]) == 1 and manifest.get("baseline_at_launch") is None
    else:
        old_manifest, old_result = previous
        gates["compatible_protocol"] = result["comparison_key"] == old_result["comparison_key"]
        accepted = next(c for c in old_manifest["candidates"] if c["id"] == manifest["baseline_at_launch"]["candidate_id"])
        control = manifest["candidates"][0]
        gates["contemporary_control"] = (len(manifest["candidates"]) == 2
            and control.get("baseline_revision") == (manifest.get("baseline_at_launch") or {}).get("revision")
            and all(control.get(k) == accepted.get(k) for k in ("binary_sha256", "source_sha256", "engine", "configuration")))
        comparison = canonical["comparisons"][0] if canonical["comparisons"] else {}
        interval = comparison.get("delta_interval_95")
        gates["quality_improvement"] = bool(interval and interval[0] > 0)
        control_arm = canonical["arms"][manifest["candidates"][0]["id"]]
        candidate_arm = canonical["arms"][candidate]
        gates["no_suite_regression"] = all(candidate_arm["suites"][suite]["verified_success_rate"] >= value["verified_success_rate"]
                                            for suite, value in control_arm["suites"].items())
        cost = comparison.get("cost_per_pass_ratio")
        latency = comparison.get("common_success_latency_ratio")
        gates["cost_guard"] = cost is not None and cost <= policy["max_cost_ratio"]
        gates["latency_guard"] = (latency is not None and latency <= policy["max_latency_ratio"]
            and comparison.get("common_success_tasks", 0) >= policy["min_common_success_tasks"])
        old_candidate = manifest["baseline_at_launch"]["candidate_id"]
        gates["no_stored_score_regression"] = candidate_arm["verified_success_rate"] >= old_result["arms"][old_candidate]["verified_success_rate"]
    return {"schema_version": 1, "run_id": manifest["run_id"], "candidate_id": candidate,
            "policy_version": policy["version"], "eligible": all(gates.values()),
            "gates": gates, "failed_gates": [name for name, passed in gates.items() if not passed]}


def verify_evidence(evals, manifest, result):
    integrity = result.get("integrity") or {}
    path = inside(evals, integrity["path"])
    if file_hash(path) != integrity["sha256"]:
        raise ValueError("evidence inventory changed")
    inventory = read_json(path)
    if inventory["errors"] or inventory["skipped_nonregular"]:
        raise ValueError("evidence inventory is incomplete")
    root = inside(evals, manifest["artifact_root"])
    for item in inventory["files"]:
        file = inside(root, item["path"])
        if not file.is_file() or file.stat().st_size != item["bytes"] or file_hash(file) != item["sha256"]:
            raise ValueError("retained evidence changed")
    for candidate in manifest["candidates"]:
        if file_hash(inside(evals, candidate["binary_path"])) != candidate["binary_sha256"]:
            raise ValueError("retained baseline build changed")


def publish(evals, manifest, result):
    """Write acceptance even when held; only the pointer establishes acceptance."""
    evals = Path(evals)
    pointer = manifest.get("baseline_at_launch")
    previous = load_accepted(evals, pointer)
    decision = evaluate(manifest, result, previous=previous)
    decision.update(accepted=False, decided_at=utc_now())
    directory = track_path(evals, manifest["track"])
    directory.mkdir(parents=True, exist_ok=True)
    report = evals / "reports" / identifier(manifest["run_id"])
    if decision["eligible"]:
        verify_evidence(evals, manifest, result)
        with (directory / ".baseline.lock").open("a") as lock:
            fcntl.flock(lock, fcntl.LOCK_EX)
            latest = current(evals, manifest["track"])
            if latest != pointer:
                decision["failed_gates"].append("stale_baseline")
                decision["eligible"] = False
            else:
                new_pointer = {"schema_version": 1, "run_id": manifest["run_id"],
                               "candidate_id": decision["candidate_id"], "accepted_at": utc_now(),
                               "result_sha256": file_hash(report / "result.json"),
                               "manifest_sha256": file_hash(report / "manifest.json"),
                               "previous_revision": pointer["revision"] if pointer else None}
                new_pointer["revision"] = value_hash(new_pointer)
                decision.update(accepted=True, baseline_revision=new_pointer["revision"])
                # A crash before pointer publication leaves an unreferenced history
                # entry; readers follow current.json only, never "latest" history.
                write_json(directory / "history" / (new_pointer["revision"] + ".json"),
                           {"pointer": new_pointer, "decision": decision}, exclusive=True)
                write_json(directory / "current.json", new_pointer)
    write_json(report / "acceptance.json", decision, exclusive=True)
    return decision

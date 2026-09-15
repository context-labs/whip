"""Immutable inputs for native Docker trials on Modal; no Docker or model execution."""
import copy
import hashlib
import os
from pathlib import Path, PurePosixPath
import shutil
import stat
import tarfile
import tempfile

from .common import (EVALS, REPO, file_hash, identifier, inside,
                     new_id, read_json, utc_now, value_hash, write_json)

REMOTE_EVALS = Path("/work/evals")
MAX_JOBS = 90
# Deliberately no recursive repository/package copy: newly added files need review.
MODULES = ("__init__.py", "common.py", "adapter.py", "observe.py", "execution.py",
           "report.py", "modal_bundle.py", "prepare.py", "prepare_ripgrep.py", "tasks.py")
CLOUD_MODULES = ("modal_cloud.py", "modal_worker.py")
SPEC_FILES = ("profiles.json", "protocol.json", "references.json", "tasks.lock.json")
# The deployed coordinator must run the controller source the bundle was frozen
# with; modal_cloud.coordinate compares this digest before any dispatch.
CONTROLLER_MODULES = ("modal_cloud.py", "common.py", "execution.py")


def controller_revision(directory=Path(__file__).parent):
    return value_hash({name: file_hash(Path(directory) / name) for name in CONTROLLER_MODULES})


def _name(name):
    if (not isinstance(name, str) or not name or "\\" in name
            or "\x00" in name or name.startswith("/")
            or any(part in ("", ".", "..") for part in name.split("/"))
            or PurePosixPath(name).parts[0] != "evals"):
        raise ValueError("unsafe bundle path")
    return name


def _regular(root, name):
    if root.is_symlink() or any(parent.is_symlink() for parent in root.parents):
        raise ValueError("bundle symlink is not permitted")
    path = root
    for part in PurePosixPath(name).parts:
        path = path / part
        if path.is_symlink():
            raise ValueError("bundle symlink is not permitted")
    if not stat.S_ISREG(path.stat().st_mode):
        raise ValueError("bundle entries must be regular files")
    return path


def create_bundle(payload, archive, *, paths):
    """Write a deterministic uncompressed tar from an explicit file allowlist."""
    payload, archive = Path(payload), Path(archive)
    names = sorted(_name(name) for name in paths)
    if len(names) != len(set(names)) or not names:
        raise ValueError("bundle allowlist must be nonempty and unique")
    inventory = {"schema_version": 1, "files": {}}
    archive.parent.mkdir(parents=True, exist_ok=True)
    fd, temporary = tempfile.mkstemp(prefix=".bundle-", dir=archive.parent)
    os.close(fd)
    try:
        with tarfile.open(temporary, "w", format=tarfile.USTAR_FORMAT) as output:
            for name in names:
                path = _regular(payload, name)
                info = tarfile.TarInfo(name)
                info.size, info.mode = path.stat().st_size, stat.S_IMODE(path.stat().st_mode)
                with path.open("rb") as stream:
                    output.addfile(info, stream)
                inventory["files"][name] = file_hash(path)
        verify_bundle(temporary, inventory)
        os.link(temporary, archive)  # Never replace a previously published bundle.
    finally:
        Path(temporary).unlink(missing_ok=True)
    return inventory


def _members(source):
    """Regular members with safe names, each hashed once; duplicates are rejected."""
    files = {}
    for member in source.getmembers():
        name = _name(member.name)
        if name in files or not member.isfile():
            raise ValueError("unexpected or nonregular bundle member")
        with source.extractfile(member) as stream:
            files[name] = hashlib.file_digest(stream, "sha256").hexdigest()
    return files


def verify_bundle(archive, expected):
    """Verify against a trusted archive SHA-256, or against a {name: sha256} inventory."""
    with tarfile.open(archive, "r:") as source:
        if isinstance(expected, str):
            if len(expected) != 64 or file_hash(archive) != expected:
                raise ValueError("bundle archive hash changed")
            return {"schema_version": 1, "files": _members(source)}
        if expected.get("schema_version") != 1 or not expected.get("files"):
            raise ValueError("invalid bundle inventory")
        if _members(source) != expected["files"]:
            raise ValueError("bundle file inventory differs")
        return expected


def plan_campaign(args, *, evals=EVALS):
    """Reuse native selection/validation, changing only the cloud concurrency cap."""
    from .run import plan_run
    if getattr(args, "promote", False):
        raise ValueError("Modal campaigns cannot promote a baseline")
    if type(args.jobs) is not int or not 1 <= args.jobs <= MAX_JOBS:
        raise ValueError("Modal jobs must be between 1 and 90")
    native = copy.copy(args)
    native.promote = False
    native.against = getattr(args, "against", None)
    native.jobs = min(args.jobs, 32)  # the native planner's own cap; the cloud cap is applied below
    planned = plan_run(native, evals=evals)
    planned.update(jobs=args.jobs, backend="modal-docker", promote=False)
    return planned


def prepare_campaign(args, *, evals=EVALS, repo=REPO):
    """Freeze a campaign without Docker inspection/pulls or paid model requests.

    Builds and pinned task downloads use the native preparation functions. The
    provider catalog request is metadata-only. Returns artifacts/<run>/campaign;
    bundle.tar extracts under /work, with inputs rooted at /work/evals.
    """
    from . import baseline
    from .execution import job_config, schedule
    from .prepare import build_candidate, catalog, configuration, contract
    from .run import cost_estimate
    from .tasks import load_spec, prepare_tasks

    evals, repo = Path(evals).resolve(), Path(repo).resolve()
    planned = plan_campaign(args, evals=evals)
    lock, profiles, native_protocol = load_spec(evals)
    protocol = copy.deepcopy(native_protocol)
    protocol.update(id=native_protocol["id"] + "-modal-docker-v1",
                    track=native_protocol["track"] + "-modal-docker-v1", max_jobs=MAX_JOBS,
                    methodology="Frontier native Docker trials on Modal VM; not a local baseline or leaderboard ranking")
    protocol["execution"] = {"backend": "modal-docker", "worker_root": str(REMOTE_EVALS),
                             "automatic_task_retries": 0, "image_policy": "original-pinned-task-images",
                             "promotion_allowed": False}
    settings = getattr(args, "execution_settings", None)  # validated by submit
    if settings is not None:
        if settings["jobs"] != args.jobs:
            raise ValueError("cloud settings concurrency differs from campaign")
        protocol["execution"].update(copy.deepcopy(settings))
    task_index = {task["id"]: task for task in lock["tasks"]}
    tasks = [task_index[name] for name in planned["task_ids"]]
    run_id = identifier(args.run_id) if args.run_id else new_id()
    artifact_root = evals / "artifacts" / run_id
    report_dir = evals / "reports" / run_id
    # Reserve both namespaces before any expensive preparation, never clobber.
    if artifact_root.exists() or report_dir.exists():
        raise FileExistsError("campaign run ID already exists")
    artifact_root.mkdir(parents=True, exist_ok=False)
    report_dir.mkdir(parents=True, exist_ok=False)
    campaign = artifact_root / "campaign"
    temporary = Path(tempfile.mkdtemp(prefix=".campaign-", dir=artifact_root))
    payload = temporary / "payload"
    staged = payload / "evals"
    paths = []

    def stage(source, relative):
        # Sources are our own repository files; create_bundle hashes every one.
        relative = _name("evals/" + relative)
        source, target = Path(source), payload / _name("evals/" + relative[len("evals/"):])
        if relative in paths:
            if file_hash(target) != file_hash(source):
                raise ValueError("conflicting frozen input paths")
            return
        target.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(source, target)
        target.chmod(stat.S_IMODE(source.stat().st_mode))
        paths.append(relative)

    def stage_json(relative, value):
        write_json(staged / relative, value, exclusive=True)
        paths.append(_name("evals/" + relative))

    try:
        candidates = []
        pointer = planned["baseline_at_launch"]
        previous = baseline.load_accepted(evals, pointer)
        if planned["against"] == "baseline":
            old_manifest = previous[0]
            control = copy.deepcopy(next(c for c in old_manifest["candidates"] if c["id"] == pointer["candidate_id"]))
            control.update(id="control", baseline_revision=pointer["revision"])
            candidates.append(control)
        elif planned["against"]:
            candidates.append(build_candidate("control", planned["engines"][0], repo=repo, evals=evals, ref=planned["against"]))
        shared = build_candidate("candidate", planned["engines"][0], repo=repo, evals=evals, ref=args.ref)
        for engine in planned["engines"]:
            candidate = copy.deepcopy(shared)
            candidate.update(id=engine if len(planned["engines"]) == 2 else "candidate",
                             engine=engine, configuration=configuration(engine, protocol["model"]))
            candidates.append(candidate)
        prepared = prepare_tasks(lock, tasks, evals / "cache" / "tasks")
        model = catalog(protocol)
        contracts = {}
        for candidate in candidates:
            binary = inside(evals, candidate["binary_path"])
            if file_hash(binary) != candidate["binary_sha256"]:
                raise ValueError("captured candidate binary changed")
            if file_hash(binary.with_name("rg-linux-amd64")) != candidate["build"]["ripgrep_sha256"]:
                raise ValueError("captured ripgrep binary changed")
            for name in (binary.name, "rg-linux-amd64", "build.json"):
                source = binary.with_name(name)
                stage(source, source.relative_to(evals).as_posix())
            relative = f"artifacts/{run_id}/contracts/{candidate['id']}.json"
            stage_json(relative, contract(candidate, protocol, model))
            contracts[candidate["id"]] = REMOTE_EVALS / relative
        for value in prepared.values():
            for path in sorted(Path(value["path"]).rglob("*")):
                if path.is_symlink() or not (path.is_dir() or path.is_file()):
                    raise ValueError("nonregular frozen task input")
                if path.is_file():
                    stage(path, path.relative_to(evals).as_posix())
        for name in (*MODULES, *CLOUD_MODULES):
            stage(Path(__file__).with_name(name), "whip_evals/" + name)
        for name in SPEC_FILES:
            stage(evals / "frontier" / name, "frontier/" + name)
        trials = schedule(tasks, candidates, planned["repetitions"], args.seed)
        inputs = {}
        for trial in trials:
            candidate = next(c for c in candidates if c["id"] == trial["candidate_id"])
            task_path = Path(prepared[trial["task_id"]]["path"])
            config, envelope = job_config(trial, task_index[trial["task_id"]],
                REMOTE_EVALS / candidate["binary_path"], contracts[candidate["id"]],
                task_path, REMOTE_EVALS / "artifacts" / run_id / "jobs")
            config["tasks"][0]["path"] = str(REMOTE_EVALS / task_path.relative_to(evals))
            inputs[trial["id"]] = {"config": config, "envelope": envelope}
        comparison = {"protocol_sha256": value_hash(protocol), "task_lock_sha256": value_hash(lock),
            "provider_catalog_sha256": value_hash(model), "environment": protocol["execution"],
            "requested_jobs": args.jobs, "resource_policy": protocol["headroom"],
            "controller_source_sha256": controller_revision(staged / "whip_evals"),
            "measurement_code": {name: file_hash(staged / "whip_evals" / name) for name in (*MODULES, "modal_worker.py")}}
        manifest = {"schema_version": 1, "run_id": run_id, "created_at": utc_now(),
            "profile": planned["profile"], "profile_version": planned["profile_version"],
            "task_ids": planned["task_ids"], "repetitions": planned["repetitions"], "seed": args.seed,
            "track": protocol["track"], "protocol": protocol, "promotion_policy": protocol["promotion"],
            "promote": False, "label": args.label, "candidates": candidates, "schedule": trials,
            "baseline_at_launch": pointer, "host": {"backend": "modal-docker"},
            "capacity": None, "jobs": args.jobs, "cost_estimate": cost_estimate(previous, pointer, planned),
            "external_references": read_json(evals / "frontier" / "references.json"),
            "comparison": comparison, "comparison_key": value_hash(comparison), "catalog": model,
            "controller_source_sha256": comparison["controller_source_sha256"],
            "task_lock_sha256": value_hash(lock),
            "prepared_tasks": {key: value["sha256"] for key, value in prepared.items()},
            "resolved_tasks": {key: {section: value["resolved_native"][section] for section in ("agent", "environment", "verifier")}
                               for key, value in prepared.items()},
            "artifact_root": f"artifacts/{run_id}", "uv_lock_sha256": file_hash(evals / "uv.lock")}
        # The worker reads the manifest and the per-trial schedule from the bundle;
        # submit uploads the same two files beside it for the coordinator.
        stage_json(f"reports/{run_id}/manifest.json", manifest)
        stage_json(f"artifacts/{run_id}/schedule.json", inputs)
        write_json(temporary / "manifest.json", manifest, exclusive=True)
        write_json(temporary / "schedule.json", inputs, exclusive=True)
        inventory = create_bundle(payload, temporary / "bundle.tar", paths=paths)
        inventory.update(sha256=file_hash(temporary / "bundle.tar"), run_id=run_id)
        write_json(temporary / "bundle.json", inventory, exclusive=True)
        temporary.rename(campaign)
        write_json(report_dir / "manifest.json", manifest, exclusive=True)
        return campaign
    except BaseException as error:
        write_json(report_dir / "preparation-error.json", {"status": "preparation_failed",
                   "error_code": type(error).__name__, "created_at": utc_now()}, exclusive=True)
        raise
    finally:
        if temporary.exists():
            shutil.rmtree(temporary)

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
           "report.py", "result_evidence.py", "integrity.py", "modal_bundle.py",
           "prepare.py", "prepare_ripgrep.py", "tasks.py")
CLOUD_MODULES = ("modal_cloud.py", "modal_worker.py")
SPEC_FILES = ("profiles.json", "protocol.json", "references.json", "tasks.lock.json")


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
                mode = stat.S_IMODE(path.stat().st_mode)
                if mode & ~0o777:
                    raise ValueError("special file permissions are not permitted")
                info = tarfile.TarInfo(name)
                info.size, info.mode = path.stat().st_size, mode
                with path.open("rb") as stream:
                    output.addfile(info, stream)
                inventory["files"][name] = {"sha256": file_hash(path), "size": info.size, "mode": mode}
        verify_bundle(temporary, inventory)
        os.link(temporary, archive)  # Never replace a previously published bundle.
    finally:
        Path(temporary).unlink(missing_ok=True)
    return inventory


def _verify(archive, inventory):
    if inventory.get("schema_version") != 1 or not inventory.get("files"):
        raise ValueError("invalid bundle inventory")
    expected = inventory["files"]
    for name, metadata in expected.items():
        _name(name)
        if (set(metadata) != {"sha256", "size", "mode"}
                or type(metadata["size"]) is not int or metadata["size"] < 0
                or type(metadata["mode"]) is not int or not 0 <= metadata["mode"] <= 0o777):
            raise ValueError("invalid bundle file metadata")
    seen = set()
    for member in archive:
        name = _name(member.name)
        if (not member.isfile() or member.issparse() or member.pax_headers
                or name in seen or name not in expected):
            raise ValueError("unexpected or nonregular bundle member")
        metadata = expected[name]
        if member.size != metadata["size"] or member.mode != metadata["mode"]:
            raise ValueError("bundle file metadata changed")
        with archive.extractfile(member) as stream:
            digest = hashlib.file_digest(stream, "sha256").hexdigest()
        if digest != metadata["sha256"]:
            raise ValueError("bundle file hash changed")
        seen.add(name)
    if seen != set(expected):
        raise ValueError("bundle file inventory differs")


def _inventory(source, expected):
    if isinstance(expected, dict):
        return expected
    if not isinstance(expected, str) or len(expected) != 64:
        raise ValueError("expected a trusted bundle SHA-256")
    source.fileobj.seek(0)
    if hashlib.file_digest(source.fileobj, "sha256").hexdigest() != expected:
        raise ValueError("bundle archive hash changed")
    source.fileobj.seek(source.offset)
    files = {}
    for member in source.getmembers():
        name = _name(member.name)
        if (name in files or not member.isfile() or member.issparse()
                or member.pax_headers or not 0 <= member.mode <= 0o777):
            raise ValueError("unexpected or nonregular bundle member")
        with source.extractfile(member) as stream:
            digest = hashlib.file_digest(stream, "sha256").hexdigest()
        files[name] = {"sha256": digest, "size": member.size, "mode": member.mode}
    return {"schema_version": 1, "files": files}


def verify_bundle(archive, expected_sha256):
    """Verify against a trusted archive hash (or an explicit file inventory)."""
    with tarfile.open(archive, "r:") as source:
        inventory = _inventory(source, expected_sha256)
        _verify(source, inventory)
    return inventory


def extract_bundle(archive, destination, expected_sha256):
    """Validate before writing; publish into a new directory, never merge trees."""
    destination = Path(destination)
    if destination.exists() or destination.is_symlink():
        raise FileExistsError(destination)
    destination.parent.mkdir(parents=True, exist_ok=True)
    temporary = Path(tempfile.mkdtemp(prefix=".extract-", dir=destination.parent))
    try:
        # Keep the same descriptor for validation and extraction (no pathname swap).
        with tarfile.open(archive, "r:") as source:
            inventory = _inventory(source, expected_sha256)
            _verify(source, inventory)
            for member in source.getmembers():
                path = temporary / member.name
                path.parent.mkdir(parents=True, exist_ok=True)
                with source.extractfile(member) as incoming, path.open("xb") as outgoing:
                    shutil.copyfileobj(incoming, outgoing)
                path.chmod(member.mode)
                if file_hash(path) != inventory["files"][member.name]["sha256"]:
                    raise ValueError("bundle changed during extraction")
        if destination.exists() or destination.is_symlink():
            raise FileExistsError(destination)
        temporary.rename(destination)
    finally:
        if temporary.exists():
            shutil.rmtree(temporary)
    return destination


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
    fixture = getattr(args, "fixture", False)
    if fixture:
        if native.against:
            raise ValueError("fixture campaigns cannot compare against a baseline/ref")
        repetitions = getattr(args, "fixture_repetitions", 1)
        if type(repetitions) is not int or not 1 <= repetitions <= 45:
            raise ValueError("fixture repetitions must be between 1 and 45")
        authored = ["fixture/harbor-shared", "fixture/pier-separate"]
        selected = getattr(args, "fixture_task_ids", None)
        if selected is None:
            selected = authored
        if (not isinstance(selected, (list, tuple)) or not selected
                or any(task_id not in authored for task_id in selected)
                or len(selected) != len(set(selected))):
            raise ValueError("select a nonempty unique subset of authored fixture task IDs")
        native.profile, native.repetitions = "smoke", repetitions
        native.engines = getattr(args, "engines", None) or "starlark,quickjs"
    native.jobs = min(args.jobs, 32)
    planned = plan_run(native, evals=evals)
    planned.update(jobs=args.jobs, backend="modal-docker", promote=False)
    if fixture:
        trial_count = len(selected) * len(planned["engines"]) * repetitions
        if trial_count > MAX_JOBS:
            raise ValueError("fixture campaigns are limited to 90 total trials")
        planned.update(profile="fixture", profile_version=1, fixture=True,
                       task_ids=list(selected),
                       repetitions=repetitions, trial_count=trial_count,
                       baseline_at_launch=None, excluded_from_scores=True)
    return planned


def prepare_fixture_campaign(args, *, evals=EVALS, repo=REPO):
    """Freeze authored Harbor shared / Pier isolated separate-verifier fixtures."""
    args = copy.copy(args)
    args.fixture = True
    for key, value in {"profile": "smoke", "engines": None, "jobs": 4, "seed": 0,
                       "label": None, "run_id": None, "ref": None, "repetitions": 1}.items():
        if not hasattr(args, key):
            setattr(args, key, value)
    return prepare_campaign(args, evals=evals, repo=repo)


def _prepare_fixtures(tasks, root):
    from importlib import import_module
    import tomllib
    from .doctor import prepare_fixture
    from .tasks import file_inventory

    prepared = {}
    for task in tasks:
        path = prepare_fixture(root / task["id"].split("/")[1], task["verifier_mode"],
                               no_network=task["runner"] == "pier")
        allowed = {"task.toml", "instruction.md", "environment/Dockerfile", "tests/test.sh"}
        if task["verifier_mode"] == "separate":
            allowed.add("tests/Dockerfile")
        actual = {p.relative_to(path).as_posix() for p in path.rglob("*") if not p.is_dir()}
        if actual != allowed:
            raise ValueError("authored fixture contains unexpected files")
        files = {name: (_regular(path, name).read_bytes(), stat.S_IMODE((path / name).stat().st_mode))
                 for name in allowed}
        model = import_module(task["runner"] + ".models.task.config").TaskConfig
        native = model.model_validate(tomllib.loads(files["task.toml"][0].decode()))
        environment = native.environment.model_dump(mode="json")
        verifier = native.verifier.model_dump(mode="json")
        separate = verifier["environment_mode"] == "separate" or verifier["environment"] is not None
        task["resources"] = {key: environment[key] + ((verifier["environment"] or environment)[key] if separate else 0)
                             for key in ("cpus", "memory_mb", "storage_mb")}
        task["agent_seconds"], task["verifier_seconds"] = native.agent.timeout_sec, native.verifier.timeout_sec
        prepared[task["id"]] = {"path": str(path), "sha256": value_hash(file_inventory(files)),
                               "resolved_native": native.model_dump(mode="json")}
    return prepared


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
    from .tasks import load_spec
    from .tasks import file_inventory, prepare_tasks

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
    settings = getattr(args, "execution_settings", None)
    if settings is not None:
        allowed = {"environment", "app", "worker_image_id", "shapes", "jobs",
                   "qualification_status", "qualification_receipt", "fixture"}
        if not isinstance(settings, dict) or set(settings) - allowed:
            raise ValueError("unexpected cloud execution settings")
        for shape in settings.get("shapes", {}).values():
            if set(shape) != {"physical_cpus", "memory_mb", "min_free_disk_mb"}:
                raise ValueError("unexpected cloud shape settings")
        if settings.get("jobs") != args.jobs:
            raise ValueError("cloud settings concurrency differs from campaign")
        protocol["execution"].update(copy.deepcopy(settings))
    task_index = {task["id"]: task for task in lock["tasks"]}
    fixture = planned.get("fixture", False)
    if settings is not None and settings.get("fixture", False) != fixture:
        raise ValueError("cloud settings fixture mode differs from campaign")
    if fixture:
        protocol.update(id=protocol["id"] + "-fixture-v1", track=protocol["track"] + "-fixture-v1",
                        methodology="Authored native qualification fixtures; excluded from benchmark scores")
        protocol["execution"].update(fixture=True, external_provider_calls=0)
        task_index = {name: {"id": name, "runner": name.split("/")[1].split("-")[0],
                            "verifier_mode": name.rsplit("-", 1)[1]} for name in planned["task_ids"]}
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
        relative = _name("evals/" + relative)
        source = Path(source)
        _regular(Path(source.anchor), source.relative_to(source.anchor).as_posix())
        if stat.S_IMODE(source.stat().st_mode) & ~0o777:
            raise ValueError("special file permissions are not permitted")
        target = payload / relative
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
                             engine=engine, configuration=configuration(engine))
            candidates.append(candidate)
        prepared = (_prepare_fixtures(tasks, artifact_root / "fixture-tasks") if fixture
                    else prepare_tasks(lock, tasks, evals / "cache" / "tasks"))
        model = {"fixture": True, "external_provider_calls": 0} if fixture else catalog(protocol)
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
            if (file_hash(staged / candidate["binary_path"]) != candidate["binary_sha256"]
                    or file_hash((staged / candidate["binary_path"]).with_name("rg-linux-amd64"))
                    != candidate["build"]["ripgrep_sha256"]):
                raise ValueError("captured build changed while staging")
            relative = f"artifacts/{run_id}/contracts/{candidate['id']}.json"
            if not fixture:
                stage_json(relative, contract(candidate, protocol, model))
            contracts[candidate["id"]] = None if fixture else REMOTE_EVALS / relative
        for value in prepared.values():
            root = Path(value["path"])
            for path in sorted(root.rglob("*")):
                if path.is_symlink() or not (path.is_dir() or path.is_file()):
                    raise ValueError("nonregular frozen task input")
                if path.is_file():
                    stage(path, path.relative_to(evals).as_posix())
            frozen = staged / root.relative_to(evals)
            captured = {p.relative_to(frozen).as_posix(): (p.read_bytes(), stat.S_IMODE(p.stat().st_mode))
                        for p in frozen.rglob("*") if p.is_file()}
            if value_hash(file_inventory(captured)) != value["sha256"]:
                raise ValueError("frozen task changed while staging")
        for name in MODULES:
            stage(Path(__file__).with_name(name), "whip_evals/" + name)
        if fixture:
            stage(Path(__file__).with_name("fixture_provider.py"), "whip_evals/fixture_provider.py")
        for name in CLOUD_MODULES:
            stage(Path(__file__).with_name(name), "whip_evals/" + name)
        for name in ("pyproject.toml", "uv.lock"):
            stage(evals / name, name)
        for name in SPEC_FILES:
            stage(evals / "frontier" / name, "frontier/" + name)
        trials = schedule(tasks, candidates, planned["repetitions"], args.seed)
        inputs = {}
        for trial in trials:
            candidate = next(c for c in candidates if c["id"] == trial["candidate_id"])
            task_path = Path(prepared[trial["task_id"]]["path"])
            config, envelope = job_config(trial, task_index[trial["task_id"]],
                REMOTE_EVALS / candidate["binary_path"], contracts[candidate["id"]],
                task_path, REMOTE_EVALS / "artifacts" / run_id / "jobs", fixture=fixture)
            config["tasks"][0]["path"] = str(REMOTE_EVALS / task_path.relative_to(evals))
            inputs[trial["id"]] = {"config": config, "envelope": envelope}
        comparison = {"protocol_sha256": value_hash(protocol), "task_lock_sha256": value_hash(lock),
            "provider_catalog_sha256": value_hash(model), "environment": protocol["execution"],
            "requested_jobs": args.jobs, "resource_policy": protocol["headroom"],
            "controller_source_sha256": value_hash({name: file_hash(staged / "whip_evals" / name)
                for name in ("modal_cloud.py", "common.py", "execution.py")}),
            "measurement_code": {name: file_hash(staged / "whip_evals" / name) for name in (*MODULES, "modal_worker.py")}}
        manifest = {"schema_version": 1, "run_id": run_id, "created_at": utc_now(),
            "profile": planned["profile"], "profile_version": planned["profile_version"],
            "task_ids": planned["task_ids"], "repetitions": planned["repetitions"], "seed": args.seed,
            "track": protocol["track"], "protocol": protocol, "promotion_policy": protocol["promotion"],
            "promote": False, "label": args.label, "candidates": candidates, "schedule": trials,
            "baseline_at_launch": pointer, "host": {"backend": "modal-docker", "qualified": False},
            "capacity": None, "jobs": args.jobs, "cost_estimate": cost_estimate(previous, pointer, planned),
            "external_references": read_json(evals / "frontier" / "references.json"),
            "comparison": comparison, "comparison_key": value_hash(comparison), "catalog": model,
            "controller_source_sha256": comparison["controller_source_sha256"],
            "task_lock_sha256": value_hash(lock),
            "prepared_tasks": {key: value["sha256"] for key, value in prepared.items()},
            "resolved_tasks": {key: {section: value["resolved_native"][section] for section in ("agent", "environment", "verifier")}
                               for key, value in prepared.items()},
            "artifact_root": f"artifacts/{run_id}", "uv_lock_sha256": file_hash(evals / "uv.lock")}
        if fixture:
            manifest.update(fixture=True, excluded_from_scores=True, external_provider_calls=0,
                            external_references={}, fixture_tasks=tasks)
        stage_json(f"reports/{run_id}/manifest.json", manifest)
        stage_json(f"artifacts/{run_id}/manifest.json", manifest)
        stage_json(f"artifacts/{run_id}/inputs.json", inputs)
        stage_json(f"artifacts/{run_id}/schedule.json", inputs)
        write_json(temporary / "manifest.json", manifest, exclusive=True)
        write_json(temporary / "inputs.json", inputs, exclusive=True)
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

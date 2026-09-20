"""Command entry point. Only `run` and explicit doctor integration execute trials."""
import argparse
import json
import subprocess
import sys

from .common import EVALS, file_hash, identifier, read_json


def parser():
    result = argparse.ArgumentParser(prog="whip-eval", description="Canonical Whip Frontier evaluations")
    sub = result.add_subparsers(dest="command", required=True)
    run = sub.add_parser("run", help="run fixed native task profiles")
    run.add_argument("profile", choices=["smoke", "medium", "full"])
    run.add_argument("--engines", help="starlark, quickjs, or starlark,quickjs")
    run.add_argument("--ref", help="build this Git ref; default captures current working files")
    run.add_argument("--against", help="baseline or Git ref to replay as control")
    run.add_argument("--repetitions", type=int)
    run.add_argument("--jobs", type=int, default=32, help="maximum active trials; resource admission may reduce this")
    run.add_argument("--seed", type=int, default=20260910)
    run.add_argument("--label", default="")
    run.add_argument("--run-id")
    run.add_argument("--promote", action="store_true", help="three Full repetitions and automatic baseline policy")
    run.add_argument("--dry-run", action="store_true", help="expand workload only; no Docker, builds, network, or model calls")
    modal = sub.add_parser("modal", help="detached Modal campaigns; never promotes the native baseline")
    actions = modal.add_subparsers(dest="modal_action", required=True)
    submit = actions.add_parser("submit", help="freeze and submit one detached campaign")
    submit.add_argument("profile", choices=["smoke", "medium", "full"])
    submit.add_argument("--engines", default="quickjs")
    submit.add_argument("--ref")
    submit.add_argument("--against")
    submit.add_argument("--repetitions", type=int, default=3)
    submit.add_argument("--jobs", type=int, help="maximum VMs at once; default frontier/modal.json max_jobs")
    submit.add_argument("--seed", type=int, default=20260910)
    submit.add_argument("--label", default="")
    submit.add_argument("--run-id")
    submit.add_argument("--allow-model-calls", action="store_true")
    submit.add_argument("--dry-run", action="store_true")
    for name in ("status", "fetch", "cancel", "prune"):
        action = actions.add_parser(name)
        action.add_argument("run_id")
        if name == "fetch":
            action.add_argument("--keep", action="store_true", help="leave Modal-side run data in place after fetching")
        if name == "prune":
            action.add_argument("--force", action="store_true", help="prune even if the coordinator has not finished")
    doctor = sub.add_parser("doctor", help="check local prerequisites; no model calls")
    doctor.add_argument("--integration", action="store_true", help="explicitly execute authored fixture trials with a fake provider")
    doctor.add_argument("--ref", help="build this Git ref for integration; default captures current working files")
    compare = sub.add_parser("compare", help="compare retained reports offline")
    compare.add_argument("control")
    compare.add_argument("candidate")
    compare.add_argument("--control-arm")
    compare.add_argument("--candidate-arm")
    report = sub.add_parser("report", help="regenerate an analysis from retained evidence; never resumes execution")
    report.add_argument("run_id")
    base = sub.add_parser("baseline", help="inspect the accepted baseline")
    base.add_argument("action", choices=["show"])
    cleanup = sub.add_parser("cleanup", help="remove only proven orphan containers for an interrupted run")
    cleanup.add_argument("run_id")
    return result


def main(argv=None):
    args = parser().parse_args(argv)
    try:
        if args.command == "run":
            from .run import run
            value = run(args)
        elif args.command == "modal":
            from . import modal_cli
            if args.modal_action == "submit":
                value = modal_cli.submit(args)
            elif args.modal_action == "status":
                value = modal_cli.status(args.run_id)
            elif args.modal_action == "fetch":
                value = modal_cli.fetch(args.run_id, keep=args.keep)
            elif args.modal_action == "prune":
                value = modal_cli.prune(args.run_id, force=args.force)
            else:
                value = modal_cli.cancel(args.run_id)
        elif args.command == "doctor":
            from .doctor import doctor
            value = doctor(integration=args.integration, ref=args.ref)
        elif args.command == "compare":
            from .report import compare_results
            value = compare_results(read_json(EVALS / "reports" / identifier(args.control) / "result.json"),
                read_json(EVALS / "reports" / identifier(args.candidate) / "result.json"),
                control_id=args.control_arm, candidate_id=args.candidate_arm)
        elif args.command == "report":
            from .run import regenerate
            value = regenerate(args.run_id)
        elif args.command == "baseline":
            from .baseline import current
            from .tasks import load_spec
            value = current(EVALS, load_spec()[2]["track"]) or {"status": "not_initialized"}
        else:
            from .doctor import cleanup
            value = cleanup(args.run_id)
        print(json.dumps(value, indent=2, allow_nan=False))
        return 2 if value.get("status") in ("cancelled", "failed", "incomplete") else 0
    except (OSError, ValueError, KeyError, subprocess.SubprocessError) as error:
        # subprocess output / provider errors can contain secrets; don't echo it.
        detail = str(error) if isinstance(error, ValueError) else type(error).__name__
        print("whip-eval: " + detail, file=sys.stderr)
        return 2
    except Exception as error:
        if args.command != "modal":
            raise
        print("whip-eval: Modal operation failed (" + type(error).__name__ + "); inspect run status", file=sys.stderr)
        return 2


if __name__ == "__main__":
    raise SystemExit(main())

#!/usr/bin/env python3
"""Serial paired Harbor/Pier trials with frozen recipes and a total spend cap."""
import argparse
import hashlib
from importlib.metadata import version
import json
import math
import os
from pathlib import Path
import random
import signal
import subprocess
import sys
import time
import tomllib

from observe import atomic_json, final_accounting_complete
from whip_adapter import OBSERVER_GRACE_SECONDS, CLEANUP_TIMEOUT_SECONDS


def digest(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def task_digest(path):
    value = hashlib.sha256()
    for item in sorted(Path(path).rglob("*")):
        if item.is_file() and "solution" not in item.relative_to(path).parts:
            value.update(str(item.relative_to(path)).encode() + b"\0" + item.read_bytes())
    return value.hexdigest()


def verifier_success(rewards):
    # Pier also exports counts and partial scores in this map. Only the
    # upstream verifier's authoritative reward determines task success.
    return rewards.get("reward") == 1


def paired_schedule(tasks, repetitions, seed, suite="all"):
    if suite not in ("all", "repository"):
        raise ValueError("unknown suite")
    rng = random.Random(seed)
    first_engines = ["starlark" if index % 2 == 0 else "quickjs" for index in range(len(tasks))]
    rng.shuffle(first_engines)
    starts = {task[0]: engine for task, engine in zip(tasks, first_engines)}
    schedule = []
    for repetition in range(repetitions):
        order = list(tasks)
        rng.shuffle(order)
        for task, runner, path, commit in order:
            # Filter after the original full-suite RNG so pair order stays pinned.
            if suite == "repository" and runner != "pier":
                continue
            engines = [starts[task], "quickjs" if starts[task] == "starlark" else "starlark"]
            if repetition % 2:
                engines.reverse()
            for engine in engines:
                schedule.append({"task": task, "runner": runner, "path": str(Path(path).resolve()),
                                 "task_sha256": task_digest(path), "commit": commit,
                                 "engine": engine, "repetition": repetition + 1})
    return schedule


from whip_evals.execution import (AGENT_SETUP_SECONDS, RUNNER_GUARD_SECONDS,
    native_agent_timeout, timing_envelope, cleanup_owned_containers)


def run(args):
    native_limits = getattr(args, "native_limits", False)
    prior_exposure = getattr(args, "prior_exposure", 0.0)
    if not math.isfinite(prior_exposure) or prior_exposure < 0:
        raise ValueError("prior exposure must be finite and nonnegative")
    if native_limits:
        args.trial_cap = args.max_tokens = args.max_turns = args.max_output = 0
    runner_versions = {"harbor": version("harbor"), "pier": version("datacurve-pier")}
    if runner_versions != {"harbor": "0.22.0", "pier": "0.3.1"}:
        raise ValueError("timing/ownership helpers require pinned Harbor 0.22.0 and Pier 0.3.1")
    output = Path(args.output).resolve()
    output.mkdir(parents=True, exist_ok=True)
    binary = Path(args.binary).resolve()
    here = Path(__file__).resolve().parent
    if args.phase == "pilot":
        tasks = [("terminal-pilot", "harbor", str(here / "pilot-terminal"), False),
                 ("repository-pilot", "pier", str(here / "pilot-repository"), True)]
    else:
        tasks = [("regex-log", "harbor", str(Path(args.terminal) / "regex-log"), False),
                 ("openssl-selfsigned-cert", "harbor", str(Path(args.terminal) / "openssl-selfsigned-cert"), False),
                 ("anko-typed-variable-bindings", "pier", str(Path(args.deepswe) / "tasks/anko-typed-variable-bindings"), True),
                 ("httpx-multipart-response-parsing", "pier", str(Path(args.deepswe) / "tasks/httpx-multipart-response-parsing"), True)]
    schedule = paired_schedule(tasks, args.repetitions, args.seed, args.suite)
    task_filter = getattr(args, "task", None)
    if task_filter:
        schedule = [trial for trial in schedule if trial["task"] == task_filter]
        if not schedule:
            raise ValueError("task filter matched no scheduled attempts")
    start_index = getattr(args, "start_index", 0)
    if not 0 <= start_index < len(schedule):
        raise ValueError("start index must identify a scheduled attempt")
    schedule = schedule[start_index:]
    timeouts = {trial["task"]: native_agent_timeout(trial["path"]) if native_limits else args.timeout
                for trial in schedule}
    envelopes = {trial["task"]: timing_envelope(trial["path"], trial["runner"], timeouts[trial["task"]])
                 for trial in schedule}
    recipe = {"phase": args.phase, "suite": args.suite,
              "suite_filter": "original seeded full schedule; repository retains Pier tasks only",
              "timing_envelopes": envelopes, "runner_versions": runner_versions, "model": "kimi-k3", "provider": "https://api.inference.net/v1",
              "reasoning_effort": "high", "max_output": args.max_output, "host_concurrency": None if native_limits else 1,
              "max_tree_tokens": args.max_tokens, "max_root_turn_rounds": args.max_turns,
              "temperature": "provider default (omitted)", "top_p": "provider default (omitted)",
              "binary_sha256": digest(binary), "seed": args.seed, "schedule": schedule,
              "ripgrep_sha256": digest(binary.with_name("rg-linux-amd64")) if binary.with_name("rg-linux-amd64").exists() else None,
              "per_trial_cost_cap_usd": args.trial_cap, "total_cost_cap_usd": args.total_cap,
              "agent_timeout_seconds": timeouts if native_limits else args.timeout, "automatic_task_retries": 0,
              "retry_policy_note": "runner/task retries disabled; native Pier verifier retries once on VerifierTimeoutError; Whip/provider transient retries unchanged",
              "methodology_comparable": False,
              "code_hashes": {name: digest(here / name) for name in ("study.py", "observe.py", "whip_adapter.py")}}
    if native_limits:
        recipe.update(native_limits=True, prior_exposure_usd=prior_exposure,
                      start_index=start_index,
                      runtime_configuration="production defaults; engine selection only",
                      output_limit="provider-advertised maximum (maxOut=0)",
                      authorization_note="Total spending authorization across studies; no per-trial cost cap. Unknown cost reserves all remaining authorization and halts further dispatch.")
    if task_filter:
        recipe["task_filter"] = task_filter
    manifest_path = output / "recipe.json"
    if manifest_path.exists():
        if json.loads(manifest_path.read_text()) != recipe:
            raise ValueError("recipe changed; use a new output directory and retain prior attempts")
    else:
        atomic_json(manifest_path, recipe)
    records_path = output / "trials.json"
    records = json.loads(records_path.read_text()) if records_path.exists() else []
    spent = prior_exposure + sum(r.get("charged_or_reserved_usd", args.trial_cap) for r in records)
    for index, trial in enumerate(schedule):
        if index < len(records):
            if records[index]["status"] == "running" or records[index].get("outer_watchdog"):
                raise RuntimeError("prior trial has an uncertain result; recover its evidence before continuing")
            if native_limits and records[index].get("accounting_complete") is not True:
                raise RuntimeError("prior uncapped trial has unknown accounting; reconcile before continuing")
            continue
        if spent >= args.total_cap or spent + args.trial_cap > args.total_cap + 1e-9:
            print("Total budget ceiling reached; no new trial dispatched.", flush=True)
            break
        name = f"{args.phase}-{start_index+index+1:02d}-{trial['task']}-{trial['engine']}-r{trial['repetition']}"
        adapter = "HarborWhip" if trial["runner"] == "harbor" else "PierWhip"
        config = {"job_name": name, "jobs_dir": str(output / "jobs"), "n_attempts": 1,
                  "n_concurrent_trials": 1, "retry": {"max_retries": 0},
                  "environment": {"type": "docker", "delete": True},
                  "agents": [{"import_path": "whip_adapter:" + adapter,
                              "model_name": "inference-net/kimi-k3", "override_timeout_sec": envelopes[trial["task"]]["agent_runner_seconds"],
                              "override_setup_timeout_sec": AGENT_SETUP_SECONDS,
                              "kwargs": {"engine": trial["engine"], "binary": str(binary),
                                         "timeout": timeouts[trial["task"]], "max_cost": args.trial_cap,
                                         "max_tokens": args.max_tokens, "max_turns": args.max_turns,
                                         "max_output": args.max_output,
                                         "commit": trial["commit"]}}],
                  "tasks": [{"path": trial["path"]}]}
        if native_limits:
            config["agents"][0]["kwargs"]["native_defaults"] = True
        config_path = output / (name + ".json")
        atomic_json(config_path, config)
        reservation = max(0, args.total_cap - spent) if native_limits else args.trial_cap
        record = dict(trial, index=index, name=name, status="running", charged_or_reserved_usd=reservation)
        if native_limits:
            record["reservation_note"] = "Accounting placeholder for unknown uncapped usage, not a Whip budget or measured charge."
        records.append(record)
        atomic_json(records_path, records)
        print(f"Starting {index+1}/{len(schedule)} {name}; spent/reserved ${spent:.4f}", flush=True)
        runner_path = Path(sys.executable).parent / trial["runner"]
        env = dict(os.environ, PYTHONPATH=str(here), DOCKER_DEFAULT_PLATFORM="linux/amd64")
        started = time.monotonic()
        with (output / (name + ".log")).open("w") as log:
            process = subprocess.Popen([str(runner_path), "run", "--config", str(config_path)],
                                       env=env, stdout=log, stderr=subprocess.STDOUT, start_new_session=True)
            try:
                process.wait(timeout=envelopes[trial["task"]]["outer_watchdog_seconds"])
            except subprocess.TimeoutExpired:
                record["outer_watchdog"] = True
                try:
                    os.killpg(process.pid, signal.SIGINT)
                    try:
                        process.wait(timeout=60)
                    except subprocess.TimeoutExpired:
                        os.killpg(process.pid, signal.SIGKILL)
                        process.wait(timeout=15)
                except (ProcessLookupError, subprocess.TimeoutExpired) as error:
                    record["watchdog_runner_stop_error"] = type(error).__name__
                finally:
                    record["watchdog_cleanup"] = cleanup_owned_containers(output / "jobs" / name, trial["runner"])
        leaves = list((output / "jobs" / name).glob("*/result.json"))
        record["runner_exit_code"] = process.returncode
        if len(leaves) != 1:
            record.update(status="missing_evidence", success=False)
        else:
            result = json.loads(leaves[0].read_text())
            context = result.get("agent_result") or {}
            metadata = context.get("metadata") or {}
            metrics = metadata.get("whip")
            outcome = metadata.get("whip_outcome")
            rewards = (result.get("verifier_result") or {}).get("rewards") or {}
            record.update(status="finished", success=verifier_success(rewards),
                          rewards=rewards, exception=result.get("exception_info"),
                          result_path=str(leaves[0]), metrics=metrics)
            accounting_complete = metadata.get("whip_accounting_complete") is True and final_accounting_complete(metrics, outcome)
            if accounting_complete:
                record["charged_or_reserved_usd"] = metrics["ledger_cost_usd"]
            record["accounting_complete"] = accounting_complete
            record["agent_outcome"] = outcome
            record["observer_exit_code"] = metadata.get("whip_observer_exit_code")
            record["evidence_errors"] = metadata.get("whip_evidence_errors", [])
            record["metrics_probes"] = metadata.get("whip_metrics_probes")
            record["adapter_cleanup"] = metadata.get("whip_cleanup")
            record["verifier_observed"] = "reward" in rewards
        record["whole_trial_seconds"] = time.monotonic() - started
        spent += record["charged_or_reserved_usd"]
        atomic_json(records_path, records)
        print(f"Finished {name}: success={record['success']}, cumulative cost/exposure=${spent:.4f}", flush=True)
        if record.get("outer_watchdog"):
            raise RuntimeError("outer watchdog interrupted this trial; inspect retained evidence and owned-container cleanup before continuing")
        if native_limits and record.get("accounting_complete") is not True:
            raise RuntimeError("uncapped trial has unknown accounting; reconcile before continuing")


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--phase", choices=["pilot", "formal"], required=True)
    parser.add_argument("--suite", choices=["all", "repository"], default="all")
    parser.add_argument("--task", help="Retain one named task after generating the seeded paired schedule")
    parser.add_argument("--binary", required=True)
    parser.add_argument("--output", required=True)
    parser.add_argument("--terminal", default="/tmp/whip-runtime-ab-terminal")
    parser.add_argument("--deepswe", default="/tmp/whip-runtime-ab-deepswe")
    parser.add_argument("--repetitions", type=int, default=1)
    parser.add_argument("--seed", type=int, default=20260910)
    parser.add_argument("--trial-cap", type=float, default=2.5)
    parser.add_argument("--total-cap", type=float, default=50)
    parser.add_argument("--timeout", type=int, default=900)
    parser.add_argument("--max-tokens", type=int, default=120000)
    parser.add_argument("--max-turns", type=int, default=30)
    parser.add_argument("--max-output", type=int, default=8192)
    parser.add_argument("--native-limits", action="store_true", help="Use each task's agent timeout, no experimental budgets/round/output caps, and production RLM defaults")
    parser.add_argument("--prior-exposure", type=float, default=0, help="Previously charged/reserved testing dollars within the total authorization")
    parser.add_argument("--start-index", type=int, default=0, help="Resume the original seeded schedule in a separate output directory; never reruns the skipped prefix")
    run(parser.parse_args())

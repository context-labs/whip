#!/usr/bin/env python3
"""Paid connectivity probes in original restricted Pier images; never task scores."""
import argparse
import asyncio
import json
import os
from pathlib import Path
import shlex

from pier.environments.docker.docker import DockerEnvironment
from pier.models.agent.context import AgentContext
from pier.models.task.task import Task
from pier.models.trial.paths import TrialPaths
from whip_adapter import PierWhip


async def run(args):
    output = Path(args.output).resolve()
    output.mkdir(parents=True, exist_ok=False)
    for engine, task_name in (("starlark", "httpx-multipart-response-parsing"),
                              ("quickjs", "anko-typed-variable-bindings")):
        task = Task(Path(args.deepswe) / "tasks" / task_name)
        paths = TrialPaths(output / engine)
        paths.agent_dir.mkdir(parents=True)
        paths.verifier_dir.mkdir()
        paths.artifacts_dir.mkdir()
        agent = PierWhip(logs_dir=paths.agent_dir, model_name="inference-net/kimi-k3",
                         engine=engine, binary=args.binary, timeout=120, max_cost=.10,
                         max_tokens=15000, max_turns=4, max_output=1024)
        environment = DockerEnvironment(environment_dir=task.paths.environment_dir,
            environment_name=task.name, session_id="whip-runtime-preflight-" + engine,
            trial_paths=paths, task_env_config=task.config.environment,
            network_allowlist=agent.network_allowlist(), default_user=task.config.agent.user)
        context = AgentContext()
        try:
            await environment.start(force_build=False)
            await agent.setup(environment)
            probe = '''import json, os, urllib.request
request = urllib.request.Request("https://api.inference.net/v1/models", headers={"Authorization": "Bearer " + os.environ["INFERENCE_API_KEY"], "User-Agent": "whip-runtime-benchmark/1.0"})
with urllib.request.urlopen(request, timeout=30) as response:
    models = json.load(response)
assert any(model["id"] == "kimi-k3" for model in models["data"])
blocked = False
try:
    urllib.request.urlopen("https://example.com", timeout=10)
except Exception as error:
    blocked = "403" in str(error)
assert blocked, "unlisted domain was not denied by proxy"
print(json.dumps({"provider_catalog_reached": True, "unlisted_domain_denied": True}))
'''
            result = await environment.exec(shlex.join(["python3", "-c", probe]),
                env=agent.process_env(environment, {"INFERENCE_API_KEY": os.environ["INFERENCE_API_KEY"]}), timeout_sec=60)
            if result.return_code:
                raise RuntimeError("network preflight failed; no model request dispatched")
            (paths.trial_dir / "network.json").write_text(result.stdout)
            await agent.run("Connectivity probe only. Execute one rlm_exec cell that prints 7, then reply done. Do not inspect the repository or create children.", environment, context)
            metadata = context.metadata or {}
            (paths.trial_dir / "context.json").write_text(context.model_dump_json(indent=2))
            complete = metadata.get("whip_accounting_complete") is True
            amount = metadata.get("whip", {}).get("ledger_cost_usd") if complete else .10
            print(json.dumps({"engine": engine, "accounting_complete": complete,
                              "charged_or_reserved_usd": amount,
                              "outcome": metadata.get("whip_outcome", {}).get("status")}), flush=True)
            if not complete or metadata.get("whip", {}).get("model_calls", 0) < 1:
                raise RuntimeError("live provider preflight lacks complete call evidence")
        finally:
            await environment.stop(delete=True)


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--binary", required=True)
    parser.add_argument("--output", required=True)
    parser.add_argument("--deepswe", default="/tmp/whip-runtime-ab-deepswe")
    asyncio.run(run(parser.parse_args()))

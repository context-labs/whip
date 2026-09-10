"""Whip adapters for the actual Harbor 0.22.0 and Pier 0.3.1 AgentContexts.

Only launch, observation, accounting, and evidence transfer live here. All model
planning, host effects, child sessions and recovery remain in the Whip daemon.
"""

import asyncio
import hashlib
import json
import os
from pathlib import Path
import shlex

from harbor.agents.base import BaseAgent as HarborBaseAgent
from pier.agents.base import BaseAgent as PierBaseAgent
from pier.models.agent.network import NetworkAllowlist
from .observe import final_accounting_complete


OBSERVER_GRACE_SECONDS = 45
CLEANUP_TIMEOUT_SECONDS = 240
PROBE_INTERVAL_SECONDS = 2
PROBE_TIMEOUT_SECONDS = 15


def probe_timed_out(error):
    # Pier 0.3.1 wraps its Docker exec timeout in this exact RuntimeError;
    # Harbor may expose TimeoutError. Other transport errors remain fatal.
    return isinstance(error, TimeoutError) or (
        type(error) is RuntimeError
        and str(error) == f"Command timed out after {PROBE_TIMEOUT_SECONDS} seconds"
    )


def clear_accounting(context):
    context.cost_usd = None
    context.n_input_tokens = context.n_output_tokens = context.n_cache_tokens = None
    if hasattr(context, "peak_context_tokens"):
        context.peak_context_tokens = None


class WhipAdapter:
    def __init__(self, *args, engine="starlark", binary=None, timeout=600,
                 max_cost=0, max_tokens=0, max_turns=0, max_output=0, commit=False, fixture=False,
                 native_defaults=False, contract=None, **kwargs):
        if engine not in ("starlark", "quickjs"):
            raise ValueError("unknown execution engine")
        self.engine = engine
        self.binary = Path(binary or os.environ["WHIP_EVAL_BINARY"]).resolve()
        self.binary_digest = hashlib.sha256(self.binary.read_bytes()).hexdigest()
        self.ripgrep = self.binary.with_name("rg-linux-amd64")
        self.timeout = int(timeout)
        self.max_cost = float(max_cost)
        self.max_tokens = int(max_tokens)
        self.max_turns = int(max_turns)
        self.max_output = int(max_output)
        self.commit = commit is True or str(commit).lower() == "true"
        self.fixture = fixture is True or str(fixture).lower() == "true"
        self.native_defaults = native_defaults is True or str(native_defaults).lower() == "true"
        self.contract = Path(contract).resolve() if contract else None
        if self.contract and any((self.max_cost, self.max_tokens, self.max_turns, self.max_output)):
            raise ValueError("canonical trials cannot use experimental caps")
        super().__init__(*args, **kwargs)
        if self.model_name not in ("kimi-k3", "inference-net/kimi-k3"):
            raise ValueError("this matched study pins inference-net/kimi-k3")

    @staticmethod
    def name():
        return "whip-runtime-ab"

    def version(self):
        return self.binary_digest[:16]

    def network_allowlist(self):
        return NetworkAllowlist(domains=["api.inference.net"])

    @staticmethod
    def process_env(environment, values):
        # Pier injects its authenticated, domain-limited inference proxy only
        # through this API; ordinary environment.exec does not add it.
        configure = getattr(environment, "agent_process_env", None)
        return configure(values) if configure else values

    async def setup(self, environment):
        if self.ripgrep.exists():
            await environment.exec("mkdir -p /usr/local/bin", timeout_sec=15, user="root")
            await environment.upload_file(self.ripgrep, "/usr/local/bin/rg")
            installed = await environment.exec("chmod 755 /usr/local/bin/rg && /usr/local/bin/rg --version", timeout_sec=15, user="root")
            if installed.return_code != 0:
                raise RuntimeError("bundled ripgrep failed startup")
        result = await environment.exec(
            "mkdir -p /opt/whip /logs/agent/whip && "
            "(command -v python3 >/dev/null && command -v curl >/dev/null && command -v rg >/dev/null && command -v git >/dev/null || "
            "(apt-get update -qq && apt-get install -y -qq python3 ca-certificates curl ripgrep git))",
            timeout_sec=300, user="root")
        if result.return_code != 0:
            raise RuntimeError("Whip dependency installation failed: " + (result.stderr or "")[-2000:])
        await environment.upload_file(self.binary, "/opt/whip/whip")
        await environment.upload_file(Path(__file__).with_name("observe.py"), "/opt/whip/observe.py")
        if self.fixture:
            await environment.upload_file(Path(__file__).with_name("fixture_provider.py"), "/opt/whip/fixture_provider.py")
        if getattr(self, "contract", None):
            await environment.upload_file(self.contract, "/opt/whip/contract.json")
        result = await environment.exec("chmod 755 /opt/whip/whip && /opt/whip/whip --version", timeout_sec=30)
        if result.return_code != 0:
            raise RuntimeError("Whip Linux binary failed startup: " + (result.stderr or "")[-2000:])

    def update_context(self, context, metrics):
        complete = metrics["unknown_usage_calls"] == 0 and metrics["pending_calls"] == 0
        context.n_input_tokens = metrics["input_tokens"] if complete else None
        context.n_output_tokens = metrics["output_tokens"] if complete else None
        context.n_cache_tokens = metrics["cache_tokens"] if complete else None
        context.cost_usd = metrics["ledger_cost_usd"] if metrics["unknown_cost_calls"] == 0 and metrics["pending_calls"] == 0 else None
        if hasattr(context, "peak_context_tokens"):
            context.peak_context_tokens = metrics["peak_input_tokens"] if complete else None
        context.metadata = {**(context.metadata or {}), "whip": metrics, "execution_engine": self.engine,
                            "binary_sha256": self.binary_digest,
                            "methodology_comparable": False}

    async def run(self, instruction, environment, context):
        self.logs_dir.mkdir(parents=True, exist_ok=True)
        instruction_path = self.logs_dir / "instruction.txt"
        instruction_path.write_text(instruction)
        await environment.upload_file(instruction_path, "/opt/whip/instruction.txt")
        key = "offline-fixture" if self.fixture else os.environ.get("INFERENCE_API_KEY")
        if not key:
            raise RuntimeError("INFERENCE_API_KEY is required")
        args = ["python3", "/opt/whip/observe.py", "--engine", self.engine,
                "--instruction", "/opt/whip/instruction.txt", "--timeout", str(self.timeout),
                "--max-cost", str(self.max_cost), "--max-tokens", str(self.max_tokens),
                "--max-turns", str(self.max_turns), "--max-output", str(self.max_output)]
        if self.commit:
            args.append("--commit")
        if self.fixture:
            args.append("--fixture")
        if getattr(self, "native_defaults", False):
            args.append("--native-defaults")
        if getattr(self, "contract", None):
            args.extend(["--contract", "/opt/whip/contract.json"])
        operation = asyncio.create_task(environment.exec(
            shlex.join(args), env=self.process_env(environment, {"INFERENCE_API_KEY": key}), timeout_sec=self.timeout + OBSERVER_GRACE_SECONDS))
        observer_exit_code = None
        evidence_errors = []
        downloaded = set()
        clock = asyncio.get_running_loop().time
        started = clock()
        last_sample = started
        probes = {"attempts": 0, "samples": 0, "timeouts": 0,
                  "last_sample_elapsed_seconds": None, "max_staleness_seconds": 0.0}
        cleanup_truncated = False
        try:
            while not operation.done():
                await asyncio.wait({operation}, timeout=PROBE_INTERVAL_SECONDS)
                if operation.done():
                    break
                # Optional intermediate samples never establish finality. Keep
                # the last observation if only this Docker probe times out.
                probes["attempts"] += 1
                try:
                    probe = await environment.exec(
                        "cat /logs/agent/whip/metrics.json 2>/dev/null || true",
                        timeout_sec=PROBE_TIMEOUT_SECONDS)
                except Exception as error:
                    if not probe_timed_out(error):
                        raise
                    probes["timeouts"] += 1
                    probes["last_timeout_elapsed_seconds"] = clock() - started
                    continue
                if (probe.stdout or "").strip():
                    self.update_context(context, json.loads(probe.stdout))
                    now = clock()
                    probes["max_staleness_seconds"] = max(probes["max_staleness_seconds"], now - last_sample)
                    last_sample = now
                    probes["samples"] += 1
                    probes["last_sample_elapsed_seconds"] = now - started
            # A probe timeout must never swallow the observer's own failure.
            result = await operation
            observer_exit_code = result.return_code
            (self.logs_dir / "observer.stdout").write_text(result.stdout or "")
            (self.logs_dir / "observer.stderr").write_text(result.stderr or "")
            if observer_exit_code != 0:
                raise RuntimeError(f"Whip observer exited with code {observer_exit_code}")
        finally:
            probes["staleness_at_observer_exit_seconds"] = clock() - last_sample
            probes["max_staleness_seconds"] = max(probes["max_staleness_seconds"], clock() - last_sample)
            # Cancellation during cleanup must not leave interim usage looking
            # final. Raw partial metrics remain available under metadata.whip.
            clear_accounting(context)
            context.metadata = {**(context.metadata or {}), "whip_accounting_complete": False,
                                "whip_metrics_probes": probes}
            cleanup_started = clock()
            try:
                async with asyncio.timeout(CLEANUP_TIMEOUT_SECONDS):
                    if not operation.done():
                        operation.cancel()
                        await asyncio.gather(operation, return_exceptions=True)
                    if observer_exit_code != 0:
                        # A lost/cancelled exec must not leave planning active
                        # while the runner collects or grades the solution.
                        try:
                            stopped = await environment.exec("/opt/whip/whip daemon stop",
                                env={"WHIP_HOME": "/tmp/whip-eval-home"}, timeout_sec=20)
                            if stopped.return_code != 0:
                                evidence_errors.append("abnormal observer cleanup failed")
                        except Exception as error:
                            evidence_errors.append("abnormal observer cleanup: " + type(error).__name__)
                    # Descendants are evidence, never nested result.json leaves.
                    for name in ("metrics.json", "outcome.json", "state.json", "events.ndjson", "identity.json", "configuration.json", "provider-catalog.json", "cli.ndjson", "cli.stderr", "sessions.db", "content-export.json"):
                        try:
                            await environment.download_file("/logs/agent/whip/" + name, self.logs_dir / name)
                            downloaded.add(name)
                        except Exception as error:
                            evidence_errors.append(name + ": " + type(error).__name__)
                            self.logger.warning("Whip evidence %s unavailable: %s", name, type(error).__name__)
                    if "content-export.json" in downloaded:
                        content = json.loads((self.logs_dir / "content-export.json").read_text())
                        for body in content.get("bodies", []):
                            digest = body.get("digest", "")
                            if len(digest) != 64 or any(c not in "0123456789abcdef" for c in digest):
                                evidence_errors.append("invalid content identity")
                                continue
                            target = self.logs_dir / "content" / "sha256" / digest
                            target.parent.mkdir(parents=True, exist_ok=True)
                            try:
                                await environment.download_file("/logs/agent/whip/content/sha256/" + digest, target)
                                if target.stat().st_size != body["bytes"] or hashlib.sha256(target.read_bytes()).hexdigest() != digest:
                                    raise ValueError("content transfer mismatch")
                            except Exception as error:
                                evidence_errors.append("content transfer: " + type(error).__name__)
            except TimeoutError:
                cleanup_truncated = True
                evidence_errors.append("evidence/daemon cleanup exceeded deadline")
            except asyncio.CancelledError:
                cleanup_truncated = True
                evidence_errors.append("evidence/daemon cleanup cancelled")
                raise
            finally:
                # Parse only completed transfers; a cancelled copy may leave a
                # partial local file. Export errors cannot replace the original
                # observer exception or an external cancellation.
                for name in ("metrics.json", "outcome.json"):
                    if name not in downloaded:
                        continue
                    try:
                        value = json.loads((self.logs_dir / name).read_text())
                        if name == "metrics.json":
                            self.update_context(context, value)
                        else:
                            context.metadata["whip_outcome"] = value
                    except Exception as error:
                        evidence_errors.append(name + " decode: " + type(error).__name__)
                if "metrics.json" not in downloaded:
                    context.metadata["whip_error"] = "no final usage evidence; not zero usage"
                context.metadata.update(
                    execution_engine=self.engine,
                    whip_observer_exit_code=observer_exit_code,
                    whip_evidence_errors=evidence_errors,
                    whip_cleanup={"timeout_seconds": CLEANUP_TIMEOUT_SECONDS,
                                  "elapsed_seconds": clock() - cleanup_started,
                                  "truncated": cleanup_truncated,
                                  "downloaded": sorted(downloaded)})
                complete = observer_exit_code == 0 and not cleanup_truncated and not evidence_errors and final_accounting_complete(
                    context.metadata.get("whip"), context.metadata.get("whip_outcome"))
                context.metadata["whip_accounting_complete"] = complete
                if not complete:
                    clear_accounting(context)


class HarborWhip(WhipAdapter, HarborBaseAgent):
    pass


class PierWhip(WhipAdapter, PierBaseAgent):
    pass

#!/usr/bin/env python3
"""Run the real TUI and its daemon against an isolated, loopback-only provider."""

import argparse
import fcntl
import http.server
import json
import os
from pathlib import Path
import pty
import re
import select
import signal
import sqlite3
import struct
import subprocess
import tempfile
import termios
import threading
import time


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--binary", required=True)
    parser.add_argument("--output", required=True)
    args = parser.parse_args()
    binary = str(Path(args.binary).resolve())
    fixture = Path(tempfile.mkdtemp(prefix="whip-fs-tui-")).resolve()
    profile, home = fixture / "profile", fixture / "user"
    project, sibling = fixture / "project", fixture / "sibling"
    for path in (profile, home, project, sibling):
        path.mkdir()
    (sibling / "seed.txt").write_text("sibling-seed")
    env = {
        "PATH": os.environ["PATH"], "HOME": str(home), "WHIP_HOME": str(profile),
        "WHIPCODE_HOME": str(fixture / "whipcode"), "TERM": "xterm-256color",
        "COLORTERM": "truecolor", "LANG": "en_US.UTF-8", "USER": "fixture",
    }
    calls, tool_results = [], []

    class Provider(http.server.BaseHTTPRequestHandler):
        def log_message(self, *_):
            pass

        def do_GET(self):
            calls.append({"method": "GET", "path": self.path})
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.end_headers()
            self.wfile.write(json.dumps({"object": "list", "data": [{
                "id": "fixture-model", "object": "model", "context_length": 65536,
                "max_completion_tokens": 4096,
            }]}).encode())

        def do_POST(self):
            request = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
            calls.append({"method": "POST", "path": self.path, "model": request.get("model")})
            messages = request.get("messages", [])
            last_user = max((i for i, m in enumerate(messages) if m.get("role") == "user"), default=-1)
            prompt = str(messages[last_user].get("content", "")) if last_user >= 0 else ""
            phase = "RESUME_PASS" if "RESUME_PASS" in prompt else "FIRST_PASS"
            after_user = messages[last_user + 1:]
            results = [m for m in after_user if m.get("role") == "tool"]
            if not request.get("tools"):
                delta, finish = {"content": "Filesystem access acceptance"}, "stop"
            elif results:
                tool_results.append({"phase": phase, "result": results[-1].get("content")})
                delta, finish = {"content": f"FULL_ACCESS_{phase}_COMPLETE"}, "stop"
            else:
                target = str(sibling / (phase.lower() + ".txt"))
                code = (
                    f"listing = files.list(path={json.dumps(str(sibling))})\n"
                    f"seed = files.read(path={json.dumps(str(sibling / 'seed.txt'))})\n"
                    f"files.write(path={json.dumps(target)}, content={json.dumps(phase)})\n"
                    'print("sibling-seed" in seed["output"] and "seed.txt" in listing["output"])'
                )
                delta = {"tool_calls": [{"index": 0, "id": "fs-" + phase.lower(), "type": "function",
                                        "function": {"name": "rlm_exec", "arguments": json.dumps({"code": code})}}]}
                finish = "tool_calls"
            if not request.get("stream", False):
                payload = {"choices": [{"index": 0, "message": {"role": "assistant", **delta}, "finish_reason": finish}],
                           "usage": {"prompt_tokens": 20, "completion_tokens": 10, "total_tokens": 30}}
                self.send_response(200)
                self.send_header("Content-Type", "application/json")
                self.end_headers()
                self.wfile.write(json.dumps(payload).encode())
                return
            self.send_response(200)
            self.send_header("Content-Type", "text/event-stream")
            self.end_headers()
            payload = {"choices": [{"index": 0, "delta": delta, "finish_reason": finish}],
                       "usage": {"prompt_tokens": 20, "completion_tokens": 10, "total_tokens": 30}}
            self.wfile.write(("data: " + json.dumps(payload) + "\n\ndata: [DONE]\n\n").encode())
            self.wfile.flush()

    server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Provider)
    server_thread = threading.Thread(target=server.serve_forever, daemon=True)
    server_thread.start()
    config = {
        "defaultModel": "fixture-model", "defaultProvider": "fixture", "defaultEffort": "off",
        "compactModel": "fixture-model", "compactProvider": "fixture", "maxRetries": 1,
        "theme": "dark", "sidebar": False,
        "providers": {"fixture": {"name": "Local fixture", "baseUrl": f"http://127.0.0.1:{server.server_port}/v1",
                                   "api": "openai-completions", "apiKey": "synthetic-fixture-key"}},
        "models": {"fixture-model": {"providers": ["fixture"], "context": 65536, "maxOut": 4096}},
        "mcpImport": {"claude": {"enabled": False}, "codex": {"enabled": False}},
    }
    (profile / "config.json").write_text(json.dumps(config))
    (profile / "trusted.json").write_text(json.dumps({"paths": {str(project): True}}))
    outcomes = []

    def stop_daemon():
        return subprocess.run([binary, "daemon", "stop"], env=env, cwd=project,
                              capture_output=True, text=True, timeout=15)

    def run_tui(phase, flags):
        master, slave = pty.openpty()
        fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", 42, 145, 0, 0))
        proc = subprocess.Popen([binary, *flags, "up", f"{phase}: inspect the sibling directory and write the acceptance marker."],
                                cwd=project, env=env, stdin=slave, stdout=slave, stderr=slave, start_new_session=True)
        os.close(slave)
        transcript = bytearray()
        marker = f"FULL_ACCESS_{phase}_COMPLETE"
        target = sibling / (phase.lower() + ".txt")
        try:
            deadline = time.monotonic() + 45
            while time.monotonic() < deadline:
                ready, _, _ = select.select([master], [], [], 0.1)
                if ready:
                    try:
                        data = os.read(master, 65536)
                    except OSError:
                        break
                    transcript.extend(data)
                    # Basic terminal query replies allow startup through a real
                    # PTY without depending on a particular terminal emulator.
                    for query, reply in ((b"\x1b[6n", b"\x1b[1;1R"), (b"\x1b[c", b"\x1b[?1;2c"),
                                         (b"\x1b]11;?\x1b\\", b"\x1b]11;rgb:0000/0000/0000\x1b\\")):
                        if query in data:
                            os.write(master, reply)
                text = transcript.decode(errors="replace")
                plain = re.sub(r"\x1b\[[0-?]*[ -/]*[@-~]", "", text)
                if marker in plain and target.exists():
                    assert target.read_text() == phase
                    outcomes.append({"phase": phase, "sibling_file": target.name,
                                     "file_content": phase, "completion_rendered": True,
                                     "full_access_rendered": "Full Access" in plain or "full access" in plain,
                                     "terminal_bytes": len(transcript)})
                    return
                if proc.poll() is not None:
                    break
            raise AssertionError(f"TUI phase {phase} did not finish. Last terminal output:\n{plain[-7000:]}")
        finally:
            (fixture / (phase.lower() + ".terminal")).write_bytes(transcript)
            if proc.poll() is None:
                os.write(master, b"\x03")
                try:
                    proc.wait(timeout=3)
                except subprocess.TimeoutExpired:
                    os.killpg(proc.pid, signal.SIGTERM)
                    try:
                        proc.wait(timeout=3)
                    except subprocess.TimeoutExpired:
                        os.killpg(proc.pid, signal.SIGKILL)
                        proc.wait(timeout=3)
            os.close(master)

    try:
        run_tui("FIRST_PASS", ["--yolo"])
        with sqlite3.connect(profile / "runtime-v2" / "sessions.db") as db:
            rows = db.execute("SELECT id,permission_mode,cwd FROM sessions").fetchall()
        assert len(rows) == 1 and rows[0][1] == "automatic", rows
        root_id = rows[0][0]
        stopped = stop_daemon()
        assert stopped.returncode == 0, stopped.stderr
        run_tui("RESUME_PASS", ["--resume", root_id])
        with sqlite3.connect(profile / "runtime-v2" / "sessions.db") as db:
            resumed = db.execute("SELECT id,permission_mode,cwd FROM sessions").fetchall()
            schema = db.execute("PRAGMA user_version").fetchone()[0]
        assert resumed == rows, (rows, resumed)
        assert schema == 14, schema
        assert len(tool_results) == 2, tool_results
        for result in tool_results:
            decoded = json.loads(result["result"])
            assert decoded.get("output", "").strip() == "True", decoded
        report = {"status": "passed", "fixture_directory": str(fixture), "binary": binary,
                  "schema_version": schema, "session_id": root_id, "permission_mode": resumed[0][1],
                  "runs": outcomes, "provider_requests": calls,
                  "tool_results": [{"phase": r["phase"], "output": "True"} for r in tool_results]}
    finally:
        cleanup = stop_daemon()
        server.shutdown()
        server.server_close()
        server_thread.join(timeout=3)
    assert cleanup.returncode == 0, cleanup.stderr
    report["cleanup"] = "both TUI processes exited; isolated daemon stopped; loopback provider stopped"
    Path(args.output).write_text(json.dumps(report, indent=2) + "\n")
    print(json.dumps(report, indent=2))


if __name__ == "__main__":
    main()

"""Deterministic offline transport fixture. Never used for proficiency scores."""
import json
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from threading import Thread


def qualification_code(engine, child):
    if child:
        command = "sleep 1 && git add proof.txt child.txt large.txt lines.txt page.txt && git commit -qm fixture"
        if engine == "starlark":
            return 'files.write(path="child.txt", content="child")\nshell.run(command=' + json.dumps(command) + ')'
        return 'await files.write({path:"child.txt",content:"child"}); await shell.run({command:' + json.dumps(command) + '});'
    prompt = "fixture-child: fixture-qualification: create and commit child.txt"
    service = "python3 -m http.server 8765 --bind 127.0.0.1"
    if engine == "starlark":
        return ('files.write(path="proof.txt", content="proof")\n'
                'files.write(path="large.txt", content="x" * 70000)\n'
                'files.read(path="large.txt")\n'
                'files.write(path="lines.txt", content="first\\nsecond\\nthird\\n")\n'
                'page = files.read(path="lines.txt", offset=2, limit=1)\n'
                'files.write(path="page.txt", content=page["output"])\n'
                'shell.start(command=' + json.dumps(service) + ')\n'
                'agents.spawn(name="fixture-child", prompt=' + json.dumps(prompt) + ')')
    return ('await files.write({path:"proof.txt",content:"proof"});'
            'await files.write({path:"large.txt",content:"x".repeat(70000)});'
            'await files.read({path:"large.txt"});'
            'await files.write({path:"lines.txt",content:"first\\nsecond\\nthird\\n"});'
            'const page = await files.read({path:"lines.txt",offset:2,limit:1});'
            'await files.write({path:"page.txt",content:page.output});'
            'await shell.start({command:' + json.dumps(service) + '});'
            'await agents.spawn({name:"fixture-child",prompt:' + json.dumps(prompt) + '});')


def start(engine, qualification=False):
    class Handler(BaseHTTPRequestHandler):
        def log_message(self, *args):
            pass

        def do_GET(self):
            payload = {"data": [{"id": "kimi-k3", "context_length": 1048576,
                "max_completion_tokens": 8192, "reasoning_efforts": ["high"],
                "pricing": {"prompt": "0", "completion": "0", "input_cache_read": "0"}}]}
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.end_headers()
            self.wfile.write(json.dumps(payload).encode())

        def do_POST(self):
            request = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
            messages = request.get("messages", [])
            child = any(m.get("role") == "user" and "fixture-child:" in str(m.get("content")) for m in messages)
            has_tool = any(m.get("role") == "tool" for m in messages)
            if request.get("tools") and not has_tool:
                if engine == "starlark":
                    code = ('shell.run(command="sleep 1")\nfiles.write(path="child.txt", content="child")' if child else
                            'child = agents.spawn(name="fixture-child", prompt="fixture-child: create child.txt with exact content child")\nfiles.write(path="proof.txt", content="proof")\nfiles.write(path="large.txt", content="x" * 70000)\nfiles.read(path="large.txt")')
                else:
                    code = ('await shell.run({command:"sleep 1"}); await files.write({path:"child.txt",content:"child"});' if child else
                            'const child = await agents.spawn({name:"fixture-child",prompt:"fixture-child: create child.txt with exact content child"}); await files.write({path:"proof.txt",content:"proof"}); await files.write({path:"large.txt",content:"x".repeat(70000)}); await files.read({path:"large.txt"});')
                if qualification:
                    code = qualification_code(engine, child)
                delta = {"tool_calls": [{"index": 0, "id": "fixture-call", "type": "function", "function": {"name": "rlm_exec", "arguments": json.dumps({"code": code})}}]}
                reason = "tool_calls"
            else:
                delta, reason = {"content": "Fixture complete."}, "stop"
            self.send_response(200)
            self.send_header("Content-Type", "text/event-stream")
            self.end_headers()
            for part in [{"choices": [{"index": 0, "delta": delta}]},
                         {"choices": [{"index": 0, "delta": {}, "finish_reason": reason}],
                          "usage": {"prompt_tokens": 100, "completion_tokens": 30, "cost": 0.0}}]:
                self.wfile.write(("data: " + json.dumps(part) + "\n\n").encode())
            self.wfile.write(b"data: [DONE]\n\n")
            self.wfile.flush()
    server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
    Thread(target=server.serve_forever, daemon=True).start()
    return server, "http://127.0.0.1:%d/v1" % server.server_port

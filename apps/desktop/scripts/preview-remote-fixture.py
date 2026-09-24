#!/usr/bin/env python3
"""Opt-in, stdlib-only remote HTTP fixture. Run only in an owned temporary directory."""
import base64
import hashlib
import http.server
import json
import os
import secrets
import signal
import socket
import sys
import threading
import time
import urllib.parse
from pathlib import Path

root = Path(sys.argv[1]).resolve(strict=True)
marker = "remote-preview-" + secrets.token_hex(16)
lock = threading.Lock()
counts = {"approved": 0, "unapproved": 0}


class Handler(http.server.BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def log_message(self, *_):
        pass

    def do_GET(self):
        with lock:
            counts[self.server.fixture_role] += 1
            (root / "counts.json").write_text(json.dumps(counts))
        try:
            self.respond()
        except (BrokenPipeError, ConnectionResetError, TimeoutError):
            pass  # Expected when the production preview/master is revoked.

    def respond(self):
        url = urllib.parse.urlsplit(self.path)
        if url.path == "/ws" and self.headers.get("Upgrade", "").lower() == "websocket":
            accept = base64.b64encode(hashlib.sha1((self.headers["Sec-WebSocket-Key"] + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11").encode()).digest()).decode()
            self.send_response(101)
            self.send_header("Upgrade", "websocket")
            self.send_header("Connection", "Upgrade")
            self.send_header("Sec-WebSocket-Accept", accept)
            self.end_headers()
            payload = marker.encode()
            self.wfile.write(bytes([0x81, len(payload)]) + payload)
            self.wfile.flush()
            self.connection.settimeout(15)
            self.rfile.read(1)
            self.close_connection = True
            return
        if url.path == "/events":
            self.send_response(200)
            self.send_header("Content-Type", "text/event-stream")
            self.send_header("Cache-Control", "no-store")
            self.end_headers()
            for _ in range(15):
                self.wfile.write(("data: " + marker + "\n\n").encode())
                self.wfile.flush()
                time.sleep(1)
            self.close_connection = True
            return
        status, mime = 200, "text/html; charset=utf-8"
        headers = {"Cache-Control": "no-store", "X-Remote-Preview": marker}
        body = ("<!doctype html><title>Selected remote SSH preview</title><h1 id=remote-marker>" + marker + "</h1>").encode()
        if url.path == "/inspect":
            mime = "application/json"
            body = json.dumps({"marker": marker, "host": self.headers.get("Host"), "authorization": self.headers.get("Authorization"), "proxyAuthorization": self.headers.get("Proxy-Authorization"), "cookie": self.headers.get("Cookie")}).encode()
        elif url.path == "/cookie":
            headers["Set-Cookie"] = "remote_preview=" + marker + "; Path=/; HttpOnly; SameSite=Lax"
        elif url.path == "/website-auth":
            status = 401
            headers["WWW-Authenticate"] = 'Basic realm="remote-website-not-proxy"'
        elif url.path == "/redirect":
            status = 302
            headers["Location"] = urllib.parse.parse_qs(url.query).get("url", ["/"])[0]
        elif url.path == "/worker.js":
            mime = "text/javascript"
            body = b"self.addEventListener('message',async e=>{let result;try{await fetch(e.data);result='allowed'}catch{result='blocked'}e.source.postMessage(result)})"
        self.send_response(status)
        self.send_header("Content-Type", mime)
        self.send_header("Content-Length", str(len(body)))
        for key, value in headers.items():
            self.send_header(key, value)
        self.end_headers()
        self.wfile.write(body)


class IPv6Server(http.server.ThreadingHTTPServer):
    address_family = socket.AF_INET6


servers = []
for host, role, cls in [("127.0.0.1", "approved", http.server.ThreadingHTTPServer), ("::1", "approved", IPv6Server), ("127.0.0.1", "unapproved", http.server.ThreadingHTTPServer)]:
    server = cls((host, 0), Handler)
    server.fixture_role = role
    servers.append(server)
    threading.Thread(target=server.serve_forever, daemon=True).start()
manifest = {"remoteHTTPPort": servers[0].server_port, "remoteIPv6Port": servers[1].server_port, "remoteUnapprovedPort": servers[2].server_port, "remoteHTTPMarker": marker, "fixturePID": os.getpid()}
(root / "http-manifest.json").write_text(json.dumps(manifest))
(root / "counts.json").write_text(json.dumps(counts))
print(json.dumps(manifest), flush=True)
stop = threading.Event()
signal.signal(signal.SIGTERM, lambda *_: stop.set())
signal.signal(signal.SIGINT, lambda *_: stop.set())
stop.wait(3600)
for server in servers:
    server.shutdown()
    server.server_close()

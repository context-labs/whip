"""Resolve canonical cell completion events from immutable exported evidence.

Content tables contain metadata, not bodies. When the external content body was
not exported, a retained tool transcript can reproduce its StreamEvent JSON.
Accept that reconstruction only if ownership, size and SHA-256 all agree with
the canonical event and database. Never count the transcript as another cell.
"""

import hashlib
import json
import sqlite3


# Pinned by the study's source manifest: internal/session/event.go.
FROZEN_EVENT_RETENTION = 10_000


def evidence_file(agent_dir, name):
    return next((path for path in (agent_dir / name, agent_dir / "whip" / name) if path.is_file()), None)


def canonical_events(path):
    seen = {}
    if path is None:
        return
    with path.open() as stream:
        for line in stream:
            event = json.loads(line)
            key = (event.get("root_id"), event.get("seq"))
            if not key[0] or not isinstance(key[1], int):
                raise ValueError("event has no canonical root/sequence identity")
            if key in seen:
                if event != seen[key]:
                    raise ValueError("conflicting duplicate event identity")
                continue
            seen[key] = event
            yield event


def result_payload(text):
    if not isinstance(text, str):
        return {}
    candidates = [text]
    if text.startswith("Error:") and "\n{" in text:
        candidates.append(text[text.index("\n{") + 1:])
    for candidate in candidates:
        try:
            value = json.loads(candidate)
        except ValueError:
            continue
        if isinstance(value, dict) and value.get("format_version") == 2:
            return value
    return {}


def stream_event_bytes(body, result):
    # Frozen protocol.StreamEvent declaration order and encoding/json escaping.
    fields = ("accounting", "usage", "agent_id", "turn_id", "invocation_id", "host_status", "id", "name", "text", "args", "result")
    value = {key: result if key == "result" else body[key] for key in fields if key == "result" or key in body}
    text = json.dumps(value, ensure_ascii=False, separators=(",", ":"))
    for char, replacement in (("&", "\\u0026"), ("<", "\\u003c"), (">", "\\u003e"), ("\u2028", "\\u2028"), ("\u2029", "\\u2029")):
        text = text.replace(char, replacement)
    return text.encode("utf-8")


class ResultResolver:
    def __init__(self, agent_dir, root_id):
        self.agent_dir = agent_dir
        self.database = evidence_file(agent_dir, "sessions.db")
        self.root_id = root_id
        self.db = None
        self.transcripts = {}
        self.event_range = None

    def close(self):
        if self.db is not None:
            self.db.close()

    def resolve(self, event):
        body = event.get("payload_inline") or {}
        if not isinstance(body, dict):
            return None, "event_payload_invalid"
        if event.get("root_id") != self.root_id:
            return None, "event_root_mismatch"
        if isinstance(body.get("result"), str) and not body.get("truncated"):
            return body["result"], "inline"
        content = body.get("content") or {}
        if not isinstance(content, dict):
            return None, "reference_identity_invalid"
        if not content.get("reference_id"):
            return None, "missing_result"
        if self.database is None:
            return None, "database_unavailable"
        try:
            if self.db is None:
                self.db = sqlite3.connect(self.database.resolve().as_uri() + "?mode=ro&immutable=1", uri=True)
            return self._referenced(event, body, content)
        except (sqlite3.Error, ValueError, TypeError, KeyError, UnicodeError):
            return None, "reference_evidence_invalid"

    def _referenced(self, event, body, content):
        size = int(content["size"])
        digest = content["digest"]
        if size <= 0 or not isinstance(digest, str) or len(digest) != 64 or any(c not in "0123456789abcdef" for c in digest):
            return None, "reference_identity_invalid"
        stored = self.db.execute("SELECT kind,payload_inline FROM events WHERE root_id=? AND seq=?", (self.root_id, event["seq"])).fetchone()
        pruned = stored is None and self._known_pruned(event["seq"])
        if stored is None and not pruned:
            return None, "canonical_event_unavailable"
        if stored is not None and (stored[0] != event["kind"] or json.loads(stored[1]) != body):
            return None, "canonical_event_mismatch"
        reference = self.db.execute("SELECT r.digest,r.size,r.media_type,r.source,o.size FROM content_references r JOIN content_objects o ON o.digest=r.digest WHERE r.id=?", (content["reference_id"],)).fetchone()
        if reference != (digest, size, "application/json", "stream.tool.completed", size):
            return None, "reference_metadata_mismatch"
        if content.get("media_type") != "application/json" or content.get("source") != "stream.tool.completed":
            return None, "reference_metadata_mismatch"
        grant = self.db.execute("SELECT 1 FROM content_grants WHERE reference_id=? AND root_id=? AND scope='root' AND agent_id='' AND revoked_at=''", (content["reference_id"], self.root_id)).fetchone()
        if not grant:
            return None, "reference_root_grant_unavailable"
        body_file = evidence_file(self.agent_dir, "content/sha256/" + digest)
        if body_file:
            if body_file.stat().st_size != size:
                return None, "referenced_body_size_mismatch"
            encoded = body_file.read_bytes()
            if hashlib.sha256(encoded).hexdigest() != digest:
                return None, "referenced_body_digest_mismatch"
            complete = json.loads(encoded)
            if not isinstance(complete, dict):
                return None, "referenced_result_invalid"
            if any(complete.get(key) != value for key, value in body.items() if key not in ("content", "truncated")):
                return None, "referenced_stream_identity_mismatch"
            if not isinstance(complete.get("result"), str):
                return None, "referenced_result_invalid"
            return complete["result"], "reference_verified_from_body" + ("_after_pruning" if pruned else "")
        agent_id = body.get("agent_id")
        if not agent_id or not body.get("id"):
            return None, "stream_identity_unavailable"
        if agent_id not in self.transcripts:
            if agent_id == self.root_id:
                rows = self.db.execute("SELECT content FROM messages WHERE session_id=? AND role='tool'", (self.root_id,))
            else:
                rows = self.db.execute("SELECT content FROM transcript_messages WHERE root_id=? AND agent_id=? AND role='tool'", (self.root_id, agent_id))
            candidates = {}
            for row in rows:
                message = json.loads(row[0])
                if isinstance(message, dict) and message.get("role") == "tool" and isinstance(message.get("content"), str):
                    candidates.setdefault(message.get("tool_call_id"), []).append(message["content"])
            self.transcripts[agent_id] = candidates
        candidates = self.transcripts[agent_id].get(body["id"], [])
        for text in candidates:
            encoded = stream_event_bytes(body, text)
            if len(encoded) == size and hashlib.sha256(encoded).hexdigest() == digest:
                return text, "reference_verified_from_transcript" + ("_after_pruning" if pruned else "")
        return None, "referenced_body_unavailable" if not candidates else "referenced_body_digest_mismatch"

    def _known_pruned(self, sequence):
        if self.event_range is None:
            self.event_range = self.db.execute("SELECT MIN(seq),MAX(seq),COUNT(*),COUNT(DISTINCT seq) FROM events WHERE root_id=?", (self.root_id,)).fetchone()
        low, high, count, distinct = self.event_range
        return (isinstance(sequence, int) and not isinstance(sequence, bool)
                and isinstance(low, int) and isinstance(high, int)
                and count == distinct == FROZEN_EVENT_RETENTION
                and high - low + 1 == FROZEN_EVENT_RETENTION
                and 0 < sequence < low)

import copy
import hashlib
import json
from pathlib import Path
import sqlite3
import tempfile
import unittest

from result_evidence import FROZEN_EVENT_RETENTION, ResultResolver, canonical_events, stream_event_bytes
from summarize import evidence


class ReferencedResultTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.agent = Path(self.temp.name) / "agent"
        self.agent.mkdir()
        self.database = self.agent / "sessions.db"
        self.text = json.dumps({"format_version": 2, "execution_engine": "quickjs", "language": "javascript", "has_value": True, "value": "café <&>\u2028", "metrics": {"quickjs_jobs": 3}})
        self.body = {"agent_id": "child", "turn_id": "child:turn:1", "id": "call", "name": "rlm_exec"}
        encoded = stream_event_bytes(self.body, self.text)
        self.digest = hashlib.sha256(encoded).hexdigest()
        self.size = len(encoded)
        self.body.update(content={"reference_id": "ref", "digest": self.digest, "size": str(self.size), "media_type": "application/json", "source": "stream.tool.completed"}, truncated=True)
        self.event = {"root_id": "root", "seq": 1, "kind": "stream.tool.completed", "payload_inline": self.body}
        with sqlite3.connect(self.database) as db:
            for sql in (
                "CREATE TABLE events(root_id,seq,kind,payload_inline)",
                "CREATE TABLE content_objects(digest,size)",
                "CREATE TABLE content_references(id,digest,size,media_type,source)",
                "CREATE TABLE content_grants(reference_id,root_id,agent_id,scope,revoked_at)",
                "CREATE TABLE messages(session_id,role,content)",
                "CREATE TABLE transcript_messages(root_id,agent_id,role,content)",
            ):
                db.execute(sql)
            db.execute("INSERT INTO events VALUES (?,?,?,?)", ("root", 1, self.event["kind"], json.dumps(self.body)))
            db.execute("INSERT INTO content_objects VALUES (?,?)", (self.digest, self.size))
            db.execute("INSERT INTO content_references VALUES (?,?,?,?,?)", ("ref", self.digest, self.size, "application/json", "stream.tool.completed"))
            db.execute("INSERT INTO content_grants VALUES (?,?,?,?,?)", ("ref", "root", "", "root", ""))
            # Reused tool-call IDs alone cannot identify the correct result.
            for text in ("wrong result from another turn", self.text):
                db.execute("INSERT INTO transcript_messages VALUES (?,?,?,?)", ("root", "child", "tool", json.dumps({"role": "tool", "tool_call_id": "call", "content": text})))
        self.resolver = ResultResolver(self.agent, "root")
        self.addCleanup(self.resolver.close)

    def retained_window(self, sequences=range(101, 10_101)):
        with sqlite3.connect(self.database) as db:
            db.execute("DELETE FROM events")
            db.executemany("INSERT INTO events VALUES (?,?,?,?)", (("root", sequence, "stream.text", "{}") for sequence in sequences))

    def fresh_resolve(self, event):
        resolver = ResultResolver(self.agent, "root")
        try:
            return resolver.resolve(event)
        finally:
            resolver.close()

    def test_reconstructs_exact_body_without_modifying_export(self):
        before = self.database.read_bytes()
        self.assertEqual(self.resolver.resolve(self.event), (self.text, "reference_verified_from_transcript"))
        self.assertEqual(self.database.read_bytes(), before)
        self.assertEqual({p.name for p in self.agent.iterdir()}, {"sessions.db"})

    def test_wrong_root_grant_cannot_resolve(self):
        with sqlite3.connect(self.database) as db:
            db.execute("UPDATE content_grants SET root_id='other-root'")
        self.assertEqual(self.resolver.resolve(self.event), (None, "reference_root_grant_unavailable"))

    def test_exact_contiguous_retention_recovers_only_verified_old_body(self):
        self.assertEqual(FROZEN_EVENT_RETENTION, 10_000)
        self.retained_window()
        self.assertEqual(self.resolver.resolve(self.event), (self.text, "reference_verified_from_transcript_after_pruning"))

    def test_missing_event_is_unknown_without_proven_retention_window(self):
        cases = {
            "empty": [],
            "short_window": range(101, 10_100),
            "gap": [*range(101, 500), *range(501, 10_102)],
            "duplicate": [*range(101, 10_100), 101],
        }
        for name, sequences in cases.items():
            with self.subTest(name=name):
                self.retained_window(sequences)
                self.assertEqual(self.fresh_resolve(self.event), (None, "canonical_event_unavailable"))
        self.retained_window()
        after_window = copy.deepcopy(self.event)
        after_window["seq"] = 10_101
        self.assertEqual(self.fresh_resolve(after_window), (None, "canonical_event_unavailable"))

    def test_pruning_does_not_bypass_root_metadata_or_body_checks(self):
        self.retained_window()
        with sqlite3.connect(self.database) as db:
            db.execute("UPDATE content_grants SET root_id='other'")
        self.assertEqual(self.fresh_resolve(self.event), (None, "reference_root_grant_unavailable"))
        with sqlite3.connect(self.database) as db:
            db.execute("UPDATE content_grants SET root_id='root'")
            db.execute("UPDATE content_objects SET size=size+1")
        self.assertEqual(self.fresh_resolve(self.event), (None, "reference_metadata_mismatch"))
        with sqlite3.connect(self.database) as db:
            db.execute("UPDATE content_objects SET size=size-1")
            db.execute("UPDATE transcript_messages SET content=?", (json.dumps({"role": "tool", "tool_call_id": "call", "content": "wrong"}),))
        self.assertEqual(self.fresh_resolve(self.event), (None, "referenced_body_digest_mismatch"))

    def test_retained_event_kind_mismatch_is_not_pruning(self):
        with sqlite3.connect(self.database) as db:
            db.execute("UPDATE events SET kind='stream.tool.started'")
        self.assertEqual(self.resolver.resolve(self.event), (None, "canonical_event_mismatch"))

    def test_exported_body_survives_deleted_transcript(self):
        with sqlite3.connect(self.database) as db:
            db.execute("DELETE FROM transcript_messages")
        directory = self.agent / "content" / "sha256"
        directory.mkdir(parents=True)
        path = directory / self.digest
        path.write_bytes(stream_event_bytes(self.body, self.text))
        self.assertEqual(self.resolver.resolve(self.event), (self.text, "reference_verified_from_body"))
        path.write_bytes(b"corrupt")
        self.assertEqual(self.resolver.resolve(self.event), (None, "referenced_body_size_mismatch"))

    def test_digest_mismatch_does_not_accept_same_tool_call_id(self):
        with sqlite3.connect(self.database) as db:
            db.execute("DELETE FROM transcript_messages")
            db.execute("INSERT INTO transcript_messages VALUES (?,?,?,?)", ("root", "child", "tool", json.dumps({"role": "tool", "tool_call_id": "call", "content": "wrong"})))
        self.assertEqual(self.resolver.resolve(self.event), (None, "referenced_body_digest_mismatch"))

    def test_size_metadata_and_canonical_event_must_match(self):
        event = copy.deepcopy(self.event)
        event["payload_inline"]["content"]["size"] = str(self.size + 1)
        self.assertEqual(self.resolver.resolve(event), (None, "canonical_event_mismatch"))
        with sqlite3.connect(self.database) as db:
            db.execute("UPDATE content_objects SET size=size+1")
        self.assertEqual(self.resolver.resolve(self.event), (None, "reference_metadata_mismatch"))

    def test_missing_database_and_deleted_transcript_remain_unknown(self):
        with sqlite3.connect(self.database) as db:
            db.execute("DELETE FROM transcript_messages")
        self.assertEqual(self.resolver.resolve(self.event), (None, "referenced_body_unavailable"))
        self.resolver.close()
        self.database.unlink()
        resolver = ResultResolver(self.agent, "root")
        self.assertEqual(resolver.resolve(self.event), (None, "database_unavailable"))

    def test_canonical_deduplication_rejects_conflicting_rows(self):
        path = self.agent / "events.ndjson"
        path.write_text(json.dumps(self.event) + "\n" + json.dumps(self.event) + "\n")
        self.assertEqual(list(canonical_events(path)), [self.event])
        other = copy.deepcopy(self.event)
        other["kind"] = "stream.tool.started"
        path.write_text(json.dumps(self.event) + "\n" + json.dumps(other) + "\n")
        with self.assertRaisesRegex(ValueError, "conflicting duplicate"):
            list(canonical_events(path))

    def test_summary_counts_verified_reference_once_and_missing_body_as_unknown(self):
        path = self.agent / "events.ndjson"
        path.write_text(json.dumps(self.event) + "\n" + json.dumps(self.event) + "\n")
        state = {"root": {"id": "root"}, "agents": [], "turns": [], "calls": [], "checkpoints": [], "transcripts": [{"content": {"role": "tool", "content": self.text}}]}
        (self.agent / "state.json").write_text(json.dumps(state))
        record = {"name": "trial", "engine": "quickjs", "status": "finished", "result_path": str(self.agent.parent / "result.json")}
        row = evidence(record)
        self.assertEqual(row["rlm_calls_completed"], 1)
        self.assertEqual(row["cells"], 1)
        self.assertEqual(row["cell_quickjs_jobs"], 3)
        self.assertEqual(row["resolved_referenced_rlm_results"], 1)
        self.database.unlink()
        row = evidence(record)
        self.assertEqual(row["cells"], 0)
        self.assertEqual(row["unparsed_rlm_results"], 1)
        self.assertEqual(row["unresolved_referenced_rlm_results"], 1)


if __name__ == "__main__":
    unittest.main()

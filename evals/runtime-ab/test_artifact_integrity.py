import contextlib
import hashlib
import io
import json
import os
from pathlib import Path
import tarfile
import tempfile
import unittest
from unittest.mock import patch

import artifact_integrity as audit


class ArtifactIntegrityTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name) / "results"
        self.root.mkdir()
        self.output = self.root / "final-audit/integrity.json"
        self.secret = "fixture-credential-7d348f9e"
        self.env = {"INFERENCE_API_KEY": self.secret, "OPENCODE_INFERENCE_API_KEY": ""}

    def inventory(self, **kwargs):
        return audit.inventory(self.root, self.output, environ=self.env, **kwargs)

    def tar(self, name, members):
        with tarfile.open(self.root / name, "w:gz") as archive:
            for member_name, payload in members:
                info = tarfile.TarInfo(member_name)
                info.size = len(payload)
                archive.addfile(info, io.BytesIO(payload))

    def test_stream_hash_and_keys_across_chunk_boundaries(self):
        payload = b"prefix-" + self.secret.encode() + b"-suffix"
        (self.root / "result.bin").write_bytes(payload)
        self.env["OPENCODE_INFERENCE_API_KEY"] = "other-fixture-credential"
        (self.root / "second.log").write_text(self.env["OPENCODE_INFERENCE_API_KEY"])
        with patch.object(audit, "CHUNK_BYTES", 8):
            report = self.inventory()
        row = next(row for row in report["files"] if row["path"] == "result.bin")
        self.assertEqual(row["sha256"], hashlib.sha256(payload).hexdigest())
        self.assertEqual(row["bytes"], len(payload))
        self.assertEqual(report["counts"]["exact_key_matches"], 2)
        self.assertTrue(report["scan_complete_within_declared_scope"])
        self.assertNotIn(self.secret, json.dumps(report))
        self.assertNotIn(self.env["OPENCODE_INFERENCE_API_KEY"], json.dumps(report))

    def test_empty_or_absent_keys_have_explicit_unperformed_scope(self):
        (self.root / "result.txt").write_text("ordinary result")
        report = audit.inventory(self.root, self.output, environ={"INFERENCE_API_KEY": ""})
        self.assertEqual(report["scope"]["exact_key_variables_available"], [])
        self.assertIn("not performed", report["scope"]["exact_key_scan"])
        self.assertEqual(report["counts"]["exact_key_matches"], 0)

    def test_compressed_tar_members_scanned_without_extracting_paths_or_links(self):
        payload = self.secret.encode()
        self.tar("source.tar.gz", [("../../escape.txt", payload),
                                    ("/absolute/" + self.secret + ".txt", b"plain")])
        with tarfile.open(self.root / "links.tar", "w") as archive:
            for name, kind in (("symlink", tarfile.SYMTYPE), ("hardlink", tarfile.LNKTYPE)):
                info = tarfile.TarInfo(name)
                info.type, info.linkname = kind, "/outside/" + self.secret
                archive.addfile(info)
        report = self.inventory()
        self.assertEqual(report["counts"]["tar_entries"], 2)
        self.assertEqual(report["counts"]["skipped_nonregular"], 2)
        self.assertIn("source.tar.gz!../../escape.txt", [row["path"] for row in report["exact_key_matches"]])
        self.assertFalse((self.root.parent / "escape.txt").exists())
        self.assertNotIn(self.secret, json.dumps(report))
        member = next(row for row in report["tar_entries"] if row["path"].endswith("escape.txt"))
        self.assertEqual(member["sha256"], hashlib.sha256(payload).hexdigest())

    def test_filesystem_symlinks_and_fifo_are_not_read(self):
        outside = self.root.parent / "outside"
        outside.mkdir()
        (outside / "key.txt").write_text(self.secret)
        (self.root / "linked-dir").symlink_to(outside, target_is_directory=True)
        (self.root / "linked-file").symlink_to(outside / "key.txt")
        os.mkfifo(self.root / "pipe")
        report = self.inventory()
        self.assertEqual(report["counts"]["files"], 0)
        self.assertEqual(report["counts"]["exact_key_matches"], 0)
        self.assertEqual(report["counts"]["skipped_nonregular"], 3)

    def test_proxy_filename_and_field_heuristics_do_not_emit_values(self):
        value = "http://fixture-user:fixture-proxy-password@proxy.invalid:8080"
        (self.root / "docker-compose-egress-proxy.json").write_text("{}")
        (self.root / "settings.json").write_text(json.dumps({"HTTPS_PROXY": value}))
        (self.root / "program.bin").write_text("HTTPS_PROXY=" + value)
        self.tar("config.tgz", [("settings.env", ("PIER_PROXY_PASSWORD=" + value).encode())])
        report = self.inventory()
        hits = {row["path"]: row["indicators"] for row in report["potential_proxy_configs"]}
        self.assertEqual(hits["docker-compose-egress-proxy.json"], ["filename"])
        self.assertEqual(hits["settings.json"], ["field_name"])
        self.assertEqual(hits["config.tgz!settings.env"], ["field_name"])
        self.assertNotIn("program.bin", hits)
        self.assertNotIn(value, json.dumps(report))

    def test_bad_tar_and_expansion_limit_are_incomplete_not_clean(self):
        (self.root / "broken.tar.gz").write_bytes(self.secret.encode())
        self.tar("large.tgz", [("large.txt", b"x" * 64)])
        report = self.inventory(max_tar_bytes=32)
        self.assertFalse(report["scan_complete_within_declared_scope"])
        self.assertEqual({row["path"] for row in report["errors"]}, {"broken.tar.gz", "large.tgz"})
        self.assertNotIn(self.secret, json.dumps(report))

    def test_nested_archives_are_explicitly_not_expanded(self):
        self.tar("outer.tgz", [("nested.tar.gz", b"nested archive payload")])
        report = self.inventory()
        self.assertEqual(report["nested_archives_not_expanded"], ["outer.tgz!nested.tar.gz"])
        self.assertIn("not recursively", report["scope"]["nested_archives"])

    def test_manifest_is_self_excluded_and_cannot_replace_raw_or_symlink(self):
        original = self.root / "trials.json"
        original.write_text('{"raw":true}')
        report = self.inventory()
        audit.write_manifest(self.output, report)
        second = self.inventory()
        self.assertEqual(report["files"], second["files"])
        audit.write_manifest(self.output, second)
        with self.assertRaises(ValueError):
            audit.write_manifest(original, report)
        self.assertEqual(original.read_text(), '{"raw":true}')
        link = self.root / "manifest-link"
        link.symlink_to(original)
        with self.assertRaises(ValueError):
            audit.write_manifest(link, report)
        self.assertEqual(original.read_text(), '{"raw":true}')

    def test_file_changed_during_read_is_reported(self):
        target = self.root / "changing.txt"
        target.write_text("before")
        scan = audit.scan_stream

        def change_after_read(*args, **kwargs):
            result = scan(*args, **kwargs)
            target.write_text("after with a different size")
            return result

        with patch.object(audit, "scan_stream", side_effect=change_after_read):
            report = self.inventory()
        self.assertFalse(report["scan_complete_within_declared_scope"])
        self.assertIn({"path": "changing.txt", "reason": "file_changed_during_read"}, report["errors"])

    def test_cli_output_and_manifest_never_echo_fixture_credentials(self):
        (self.root / (self.secret + ".txt")).write_text(self.secret)
        stdout = io.StringIO()
        with patch.dict(os.environ, self.env, clear=True), contextlib.redirect_stdout(stdout):
            code = audit.main([str(self.root), "--output", str(self.output)])
        self.assertEqual(code, 1)
        self.assertNotIn(self.secret, stdout.getvalue())
        self.assertNotIn(self.secret, self.output.read_text())
        self.assertIn("[REDACTED].txt", self.output.read_text())


if __name__ == "__main__":
    unittest.main()

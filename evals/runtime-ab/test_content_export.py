import hashlib
import json
from pathlib import Path
import sqlite3
import tempfile
import unittest

from observe import export_content_bodies


class ExportContentTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.home, self.evidence = self.root / "home", self.root / "evidence"
        self.source = self.home / "runtime-v2" / "artifacts" / "sha256"
        self.source.mkdir(parents=True)
        self.evidence.mkdir()
        self.data = b'large stream event body'
        self.digest = hashlib.sha256(self.data).hexdigest()
        self.body = self.source / self.digest
        self.body.write_bytes(self.data)
        with sqlite3.connect(self.evidence / "sessions.db") as db:
            for sql in ("CREATE TABLE sessions(id)", "CREATE TABLE content_objects(digest,size)", "CREATE TABLE content_references(id,digest,size)", "CREATE TABLE content_grants(reference_id,root_id)"):
                db.execute(sql)
            db.execute("INSERT INTO sessions VALUES ('root')")
            db.execute("INSERT INTO content_objects VALUES (?,?)", (self.digest, len(self.data)))
            db.execute("INSERT INTO content_references VALUES ('ref',?,?)", (self.digest, len(self.data)))
            db.execute("INSERT INTO content_grants VALUES ('ref','root')")

    def test_copies_verified_owned_body_once_from_final_snapshot(self):
        database = self.evidence / "sessions.db"
        before = database.read_bytes()
        manifest = export_content_bodies(self.home, self.evidence, "root")
        self.assertEqual(manifest["bodies"], [{"digest": self.digest, "bytes": len(self.data)}])
        self.assertEqual((self.evidence / "content" / "sha256" / self.digest).read_bytes(), self.data)
        self.assertEqual(before, database.read_bytes())
        self.assertEqual(self.body.read_bytes(), self.data)

    def test_other_root_reference_is_excluded(self):
        with sqlite3.connect(self.evidence / "sessions.db") as db:
            db.execute("UPDATE content_grants SET root_id='other'")
        manifest = export_content_bodies(self.home, self.evidence, "root")
        self.assertEqual(manifest["bodies"], [])

    def test_missing_and_wrong_digest_are_explicit_export_errors(self):
        self.body.write_bytes(b'x' * len(self.data))
        with self.assertRaisesRegex(ValueError, "1 failures"):
            export_content_bodies(self.home, self.evidence, "root")
        self.assertFalse((self.evidence / "content" / "sha256" / self.digest).exists())
        self.body.unlink()
        with self.assertRaisesRegex(ValueError, "1 failures"):
            export_content_bodies(self.home, self.evidence, "root")
        self.assertEqual(len(json.loads((self.evidence / "content-export.json").read_text())["errors"]), 1)

    def test_symlinked_source_is_rejected(self):
        self.body.unlink()
        external = self.root / "external"
        external.write_bytes(self.data)
        self.body.symlink_to(external)
        with self.assertRaisesRegex(ValueError, "1 failures"):
            export_content_bodies(self.home, self.evidence, "root")


    def test_symlinked_target_directory_is_rejected_before_creation(self):
        external = self.root / "outside"
        external.mkdir()
        (self.evidence / "content").symlink_to(external, target_is_directory=True)
        with self.assertRaisesRegex(ValueError, "evidence directory"):
            export_content_bodies(self.home, self.evidence, "root")
        self.assertEqual(list(external.iterdir()), [])


if __name__ == "__main__":
    unittest.main()

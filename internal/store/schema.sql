-- A fresh database only. Guards reject writes; they do not orchestrate work.
CREATE TABLE metadata (runtime_id TEXT PRIMARY KEY) STRICT;
CREATE TABLE definition_revisions (
 id TEXT NOT NULL, revision TEXT NOT NULL, document TEXT NOT NULL CHECK(json_valid(document)),
 created_at INTEGER NOT NULL, PRIMARY KEY(id, revision)
) STRICT;
CREATE TRIGGER definition_immutable BEFORE UPDATE ON definition_revisions
 BEGIN SELECT RAISE(ABORT, 'definition revision is immutable'); END;

CREATE TABLE session_trees (
 id TEXT PRIMARY KEY, metadata TEXT NOT NULL CHECK(json_valid(metadata)),
 engine TEXT NOT NULL CHECK(engine IN ('starlark','quickjs')),
 policy TEXT NOT NULL CHECK(json_valid(policy)), revision INTEGER NOT NULL CHECK(revision > 0),
 created_at INTEGER NOT NULL
) STRICT;
CREATE TABLE sessions (
 id TEXT PRIMARY KEY, tree_id TEXT NOT NULL REFERENCES session_trees(id) ON DELETE CASCADE,
 parent_id TEXT, definition_id TEXT NOT NULL, definition_revision TEXT NOT NULL,
 config_revision INTEGER NOT NULL CHECK(config_revision > 0), working_directory TEXT NOT NULL,
 lifecycle TEXT NOT NULL CHECK(lifecycle IN ('active','stopped')), created_at INTEGER NOT NULL,
 UNIQUE(id,tree_id), CHECK(parent_id IS NULL OR parent_id <> id),
 FOREIGN KEY(parent_id,tree_id) REFERENCES sessions(id,tree_id) ON DELETE CASCADE,
 FOREIGN KEY(definition_id,definition_revision) REFERENCES definition_revisions(id,revision),
 FOREIGN KEY(id,config_revision) REFERENCES session_configurations(session_id,revision)
  DEFERRABLE INITIALLY DEFERRED
) STRICT;
CREATE UNIQUE INDEX one_root ON sessions(tree_id) WHERE parent_id IS NULL;
CREATE INDEX session_children ON sessions(parent_id,id);
CREATE TRIGGER parent_exists BEFORE INSERT ON sessions WHEN NEW.parent_id IS NOT NULL
 AND NOT EXISTS(SELECT 1 FROM sessions WHERE id=NEW.parent_id AND tree_id=NEW.tree_id)
 BEGIN SELECT RAISE(ABORT, 'parent must already exist in tree'); END;
CREATE TRIGGER session_identity_immutable BEFORE UPDATE ON sessions
 WHEN NEW.id IS NOT OLD.id OR NEW.tree_id IS NOT OLD.tree_id OR NEW.parent_id IS NOT OLD.parent_id
 OR NEW.definition_id IS NOT OLD.definition_id OR NEW.definition_revision IS NOT OLD.definition_revision
 OR NEW.working_directory IS NOT OLD.working_directory OR NEW.created_at IS NOT OLD.created_at
 BEGIN SELECT RAISE(ABORT, 'session identity is immutable'); END;

CREATE TABLE session_configurations (
 session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
 revision INTEGER NOT NULL CHECK(revision > 0), configuration TEXT NOT NULL CHECK(json_valid(configuration)),
 created_at INTEGER NOT NULL, PRIMARY KEY(session_id,revision)
) STRICT;
CREATE TRIGGER configuration_immutable BEFORE UPDATE ON session_configurations
 BEGIN SELECT RAISE(ABORT, 'configuration revision is immutable'); END;

CREATE TABLE content_bodies (
 digest TEXT PRIMARY KEY CHECK(length(digest)=64 AND digest NOT GLOB '*[^0-9a-f]*'),
 size INTEGER NOT NULL CHECK(size BETWEEN 0 AND 4194304)
) STRICT;
CREATE TRIGGER content_body_immutable BEFORE UPDATE ON content_bodies
 BEGIN SELECT RAISE(ABORT, 'content body is immutable'); END;
CREATE TABLE content_references (
 id TEXT PRIMARY KEY, session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
 digest TEXT NOT NULL REFERENCES content_bodies(digest), media_type TEXT NOT NULL,
 created_at INTEGER NOT NULL
) STRICT;
CREATE INDEX content_owner ON content_references(session_id,id);
CREATE INDEX content_digest ON content_references(digest);
CREATE TRIGGER content_reference_immutable BEFORE UPDATE ON content_references
 BEGIN SELECT RAISE(ABORT, 'content reference is immutable'); END;

CREATE TABLE turns (
 id TEXT PRIMARY KEY, session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
 config_revision INTEGER NOT NULL, state TEXT NOT NULL
 CHECK(state IN ('running','cancelling','succeeded','failed','cancelled','interrupted')),
 failure TEXT, started_at INTEGER NOT NULL, finished_at INTEGER,
 UNIQUE(id,session_id),
 FOREIGN KEY(session_id,config_revision) REFERENCES session_configurations(session_id,revision),
 CHECK((state IN ('running','cancelling')) = (finished_at IS NULL)),
 CHECK(state <> 'succeeded' OR failure IS NULL)
) STRICT;
CREATE UNIQUE INDEX one_active_turn ON turns(session_id) WHERE state IN ('running','cancelling');
CREATE TRIGGER turn_transition BEFORE UPDATE ON turns
 WHEN NEW.id IS NOT OLD.id OR NEW.session_id IS NOT OLD.session_id
 OR NEW.config_revision IS NOT OLD.config_revision OR NEW.started_at IS NOT OLD.started_at
 OR OLD.state NOT IN ('running','cancelling')
 OR (OLD.state='cancelling' AND NEW.state NOT IN ('cancelled','interrupted'))
 OR NEW.state='running'
 BEGIN SELECT RAISE(ABORT, 'invalid turn transition'); END;

CREATE TABLE inputs (
 ordinal INTEGER PRIMARY KEY AUTOINCREMENT, id TEXT NOT NULL UNIQUE,
 session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
 source TEXT NOT NULL CHECK(source IN ('user','agent','schedule')),
 parts TEXT NOT NULL CHECK(json_valid(parts)), turn_id TEXT UNIQUE, cancelled_at INTEGER, created_at INTEGER NOT NULL,
 CHECK(turn_id IS NULL OR cancelled_at IS NULL), UNIQUE(id,turn_id,session_id),
 FOREIGN KEY(turn_id,session_id) REFERENCES turns(id,session_id) ON DELETE CASCADE
) STRICT;
CREATE INDEX queued_inputs ON inputs(session_id,ordinal) WHERE turn_id IS NULL AND cancelled_at IS NULL;
CREATE TRIGGER input_immutable BEFORE UPDATE ON inputs
 WHEN NEW.id IS NOT OLD.id OR NEW.ordinal IS NOT OLD.ordinal OR NEW.session_id IS NOT OLD.session_id
 OR NEW.source IS NOT OLD.source OR NEW.parts IS NOT OLD.parts OR NEW.created_at IS NOT OLD.created_at
 OR OLD.turn_id IS NOT NULL OR OLD.cancelled_at IS NOT NULL
 BEGIN SELECT RAISE(ABORT, 'accepted input is immutable'); END;
CREATE TABLE receipts (
 client_id TEXT NOT NULL, request_id TEXT NOT NULL, digest TEXT NOT NULL,
 input_id TEXT UNIQUE REFERENCES inputs(id), deleted_at INTEGER, created_at INTEGER NOT NULL,
 PRIMARY KEY(client_id,request_id), CHECK((input_id IS NULL) <> (deleted_at IS NULL))
) STRICT;
CREATE TRIGGER receipt_immutable BEFORE UPDATE ON receipts
 WHEN NEW.client_id IS NOT OLD.client_id OR NEW.request_id IS NOT OLD.request_id
 OR NEW.digest IS NOT OLD.digest OR NEW.created_at IS NOT OLD.created_at OR OLD.deleted_at IS NOT NULL
 OR NEW.input_id IS NOT NULL OR NEW.deleted_at IS NULL
 BEGIN SELECT RAISE(ABORT, 'receipt may only become a deletion marker'); END;
CREATE TABLE messages (
 id TEXT PRIMARY KEY, session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
 turn_id TEXT NOT NULL, sequence INTEGER NOT NULL CHECK(sequence > 0),
 role TEXT NOT NULL CHECK(role IN ('system','user','assistant','tool')),
 input_id TEXT, parts TEXT CHECK(parts IS NULL OR json_valid(parts)), created_at INTEGER NOT NULL,
 UNIQUE(session_id,sequence), UNIQUE(input_id), UNIQUE(id,turn_id),
 CHECK((input_id IS NULL) <> (parts IS NULL)), CHECK((role='user') = (input_id IS NOT NULL)),
 FOREIGN KEY(turn_id,session_id) REFERENCES turns(id,session_id) ON DELETE CASCADE,
 FOREIGN KEY(input_id,turn_id,session_id) REFERENCES inputs(id,turn_id,session_id) ON DELETE CASCADE
) STRICT;
CREATE TRIGGER message_immutable BEFORE UPDATE ON messages
 BEGIN SELECT RAISE(ABORT, 'transcript entry is immutable'); END;

CREATE TABLE cells (
 ordinal INTEGER PRIMARY KEY AUTOINCREMENT, id TEXT NOT NULL UNIQUE,
 turn_id TEXT NOT NULL REFERENCES turns(id) ON DELETE CASCADE,
 call_message_id TEXT NOT NULL, call_id TEXT NOT NULL,
 state TEXT NOT NULL CHECK(state IN ('running','succeeded','failed','uncertain')),
 result_message_id TEXT UNIQUE, checkpoint TEXT CHECK(checkpoint IS NULL OR json_valid(checkpoint)),
 created_at INTEGER NOT NULL, finished_at INTEGER,
 UNIQUE(call_message_id,call_id),
 FOREIGN KEY(call_message_id,turn_id) REFERENCES messages(id,turn_id) ON DELETE CASCADE,
 FOREIGN KEY(result_message_id,turn_id) REFERENCES messages(id,turn_id) DEFERRABLE INITIALLY DEFERRED,
 CHECK((state='running') = (finished_at IS NULL)),
 CHECK((state='running') = (result_message_id IS NULL)),
 CHECK(state NOT IN ('running','uncertain') OR checkpoint IS NULL)
) STRICT;
CREATE UNIQUE INDEX one_running_cell ON cells(turn_id) WHERE state='running';
CREATE INDEX cells_by_turn ON cells(turn_id,ordinal);
CREATE INDEX checkpoint_digest ON cells(json_extract(checkpoint,'$.digest')) WHERE checkpoint IS NOT NULL;
CREATE TRIGGER cell_transition BEFORE UPDATE ON cells
 WHEN OLD.state<>'running' OR NEW.state='running'
 OR NEW.id IS NOT OLD.id OR NEW.ordinal IS NOT OLD.ordinal OR NEW.turn_id IS NOT OLD.turn_id
 OR NEW.call_message_id IS NOT OLD.call_message_id OR NEW.call_id IS NOT OLD.call_id
 OR NEW.created_at IS NOT OLD.created_at
 BEGIN SELECT RAISE(ABORT, 'invalid cell transition'); END;

CREATE TABLE model_attempts (
 id TEXT PRIMARY KEY, turn_id TEXT NOT NULL REFERENCES turns(id) ON DELETE CASCADE,
 logical_id TEXT NOT NULL, number INTEGER NOT NULL CHECK(number BETWEEN 1 AND 100),
 request TEXT NOT NULL CHECK(json_valid(request)),
 state TEXT NOT NULL CHECK(state IN ('reserved','dispatched','succeeded','failed','cancelled','uncertain')),
 result TEXT CHECK(result IS NULL OR json_valid(result)),
 cost_nano_usd INTEGER CHECK(cost_nano_usd IS NULL OR cost_nano_usd>=0),
 cost_source TEXT NOT NULL CHECK(cost_source IN ('unknown','provider','prices','not_dispatched')),
 cost_note TEXT,
 message_id TEXT UNIQUE,
 created_at INTEGER NOT NULL, dispatched_at INTEGER, finished_at INTEGER,
 UNIQUE(turn_id,logical_id,number),
 FOREIGN KEY(message_id,turn_id) REFERENCES messages(id,turn_id) DEFERRABLE INITIALLY DEFERRED,
 CHECK((state IN ('reserved','dispatched')) = (finished_at IS NULL)),
 CHECK((result IS NULL) = (finished_at IS NULL)),
 CHECK(result IS NULL OR json_extract(result,'$.state') IS state),
 CHECK((cost_nano_usd IS NULL) = (cost_source='unknown')),
 CHECK(state NOT IN ('dispatched','succeeded','failed','uncertain') OR dispatched_at IS NOT NULL),
 CHECK(state <> 'reserved' OR dispatched_at IS NULL),
 CHECK(state <> 'cancelled' OR dispatched_at IS NULL),
 CHECK(message_id IS NULL OR finished_at IS NOT NULL)
) STRICT;
CREATE INDEX attempts_by_turn ON model_attempts(turn_id,id);
CREATE INDEX attempts_unfinished ON model_attempts(id) WHERE finished_at IS NULL;
CREATE TRIGGER attempt_transition BEFORE UPDATE ON model_attempts
 WHEN NEW.id IS NOT OLD.id OR NEW.turn_id IS NOT OLD.turn_id OR NEW.logical_id IS NOT OLD.logical_id
 OR NEW.number IS NOT OLD.number OR NEW.request IS NOT OLD.request OR NEW.created_at IS NOT OLD.created_at
 OR OLD.state NOT IN ('reserved','dispatched')
 OR (OLD.state='reserved' AND NEW.state NOT IN ('dispatched','cancelled'))
 OR (OLD.state='dispatched' AND (NEW.state IN ('reserved','dispatched') OR NEW.dispatched_at IS NOT OLD.dispatched_at))
 BEGIN SELECT RAISE(ABORT, 'invalid model attempt transition'); END;

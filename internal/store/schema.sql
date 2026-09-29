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
 revision INTEGER NOT NULL CHECK(revision > 0),
 created_at INTEGER NOT NULL
) STRICT;
CREATE TABLE permission_policies (
 tree_id TEXT PRIMARY KEY REFERENCES session_trees(id) ON DELETE CASCADE,
 mode TEXT NOT NULL CHECK(mode IN ('prompt','automatic')),
 revision INTEGER NOT NULL CHECK(revision>0), updated_at INTEGER NOT NULL
) STRICT;
CREATE TRIGGER permission_policy_transition BEFORE UPDATE ON permission_policies
 WHEN NEW.tree_id IS NOT OLD.tree_id OR NEW.mode IS OLD.mode OR NEW.revision<>OLD.revision+1
 BEGIN SELECT RAISE(ABORT, 'permission policy requires a new revision'); END;
-- Receipts outlive their tree; retrying an old edit cannot recreate authority.
CREATE TABLE permission_mode_edits (
 id TEXT PRIMARY KEY, digest TEXT NOT NULL, session_id TEXT NOT NULL,
 tree_id TEXT NOT NULL, expected_revision INTEGER NOT NULL CHECK(expected_revision>0),
 mode TEXT NOT NULL CHECK(mode IN ('prompt','automatic')),
 previous_mode TEXT NOT NULL CHECK(previous_mode IN ('prompt','automatic')),
 revision INTEGER NOT NULL CHECK(revision=expected_revision+(mode<>previous_mode)),
 updated_at INTEGER NOT NULL, created_at INTEGER NOT NULL
) STRICT;
CREATE TRIGGER permission_mode_edit_immutable BEFORE UPDATE ON permission_mode_edits
 BEGIN SELECT RAISE(ABORT, 'permission mode edit is immutable'); END;
CREATE TRIGGER permission_mode_edit_retained BEFORE DELETE ON permission_mode_edits
 BEGIN SELECT RAISE(ABORT, 'permission mode receipt must be retained'); END;
CREATE TABLE sessions (
 id TEXT PRIMARY KEY, tree_id TEXT NOT NULL REFERENCES session_trees(id) ON DELETE CASCADE,
 parent_id TEXT, definition_id TEXT NOT NULL, definition_revision TEXT NOT NULL,
 config_revision INTEGER NOT NULL CHECK(config_revision > 0), working_directory TEXT NOT NULL,
 lifecycle TEXT NOT NULL CHECK(lifecycle IN ('active','stopped')), created_at INTEGER NOT NULL,
 history_revision INTEGER NOT NULL DEFAULT 1 CHECK(history_revision>0),
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
CREATE TRIGGER history_revision_transition BEFORE UPDATE OF history_revision ON sessions
 WHEN NEW.history_revision IS NOT OLD.history_revision AND
 (NEW.history_revision<>OLD.history_revision+1 OR NOT EXISTS(
  SELECT 1 FROM history_edits WHERE session_id=OLD.id AND expected_revision=OLD.history_revision AND revision=NEW.history_revision))
 BEGIN SELECT RAISE(ABORT, 'history revision requires an exact edit'); END;

CREATE TABLE session_configurations (
 session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
 revision INTEGER NOT NULL CHECK(revision > 0), configuration TEXT NOT NULL CHECK(json_valid(configuration)),
 created_at INTEGER NOT NULL, PRIMARY KEY(session_id,revision)
) STRICT;
CREATE TRIGGER configuration_immutable BEFORE UPDATE ON session_configurations
 BEGIN SELECT RAISE(ABORT, 'configuration revision is immutable'); END;

CREATE TABLE content_bodies (
 digest TEXT PRIMARY KEY CHECK(length(digest)=64 AND digest NOT GLOB '*[^0-9a-f]*'),
 size INTEGER NOT NULL CHECK(size BETWEEN 0 AND 67108864)
) STRICT;
CREATE TRIGGER content_body_immutable BEFORE UPDATE ON content_bodies
 BEGIN SELECT RAISE(ABORT, 'content body is immutable'); END;
CREATE TABLE content_references (
 reference_id TEXT NOT NULL, owner_session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
 digest TEXT NOT NULL REFERENCES content_bodies(digest), media_type TEXT NOT NULL,
 created_at INTEGER NOT NULL, PRIMARY KEY(owner_session_id,reference_id)
) STRICT;
CREATE INDEX content_digest ON content_references(digest);
CREATE TRIGGER content_reference_immutable BEFORE UPDATE ON content_references
 BEGIN SELECT RAISE(ABORT, 'content reference is immutable'); END;

CREATE TABLE turns (
 id TEXT PRIMARY KEY, session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
 config_revision INTEGER NOT NULL, history_revision INTEGER NOT NULL CHECK(history_revision>0), state TEXT NOT NULL
 CHECK(state IN ('running','cancelling','succeeded','failed','cancelled','interrupted')),
 failure TEXT, started_at INTEGER NOT NULL, finished_at INTEGER,
 goal_id TEXT, goal_revision INTEGER,
 CHECK((goal_id IS NULL) = (goal_revision IS NULL)),
 CHECK(goal_revision IS NULL OR goal_revision>0),
 FOREIGN KEY(goal_id,session_id) REFERENCES goals(id,session_id),
 UNIQUE(id,session_id),
 FOREIGN KEY(session_id,config_revision) REFERENCES session_configurations(session_id,revision),
 CHECK((state IN ('running','cancelling')) = (finished_at IS NULL)),
 CHECK(state <> 'succeeded' OR failure IS NULL)
) STRICT;
CREATE INDEX goal_turns ON turns(goal_id,id) WHERE goal_id IS NOT NULL;
CREATE INDEX turns_by_session_start ON turns(session_id,started_at DESC);
CREATE UNIQUE INDEX one_active_turn ON turns(session_id) WHERE state IN ('running','cancelling');
CREATE TRIGGER turn_history_snapshot BEFORE INSERT ON turns
 WHEN NOT EXISTS(SELECT 1 FROM sessions WHERE id=NEW.session_id AND history_revision=NEW.history_revision)
 BEGIN SELECT RAISE(ABORT, 'turn must capture current history revision'); END;
CREATE TRIGGER turn_transition BEFORE UPDATE ON turns
 WHEN NEW.id IS NOT OLD.id OR NEW.session_id IS NOT OLD.session_id
 OR NEW.config_revision IS NOT OLD.config_revision OR NEW.started_at IS NOT OLD.started_at
 OR NEW.history_revision IS NOT OLD.history_revision
 OR NEW.goal_id IS NOT OLD.goal_id OR NEW.goal_revision IS NOT OLD.goal_revision
 OR OLD.state NOT IN ('running','cancelling')
 OR (OLD.state='cancelling' AND NEW.state NOT IN ('cancelled','interrupted'))
 OR NEW.state='running'
 BEGIN SELECT RAISE(ABORT, 'invalid turn transition'); END;

CREATE TABLE turn_instruction_manifests (
 turn_id TEXT PRIMARY KEY REFERENCES turns(id) ON DELETE CASCADE,
 manifest TEXT NOT NULL CHECK(json_valid(manifest) AND length(CAST(manifest AS BLOB)) <= 1048576)
) STRICT;
CREATE TRIGGER instruction_manifest_immutable BEFORE UPDATE ON turn_instruction_manifests
 BEGIN SELECT RAISE(ABORT, 'instruction manifest is immutable'); END;

-- A live child reserves one parent-owned completion slot. Source IDs deliberately
-- have no cascading foreign key: pending evidence survives source deletion.
CREATE TABLE completion_slots (
 child_id TEXT PRIMARY KEY, parent_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
 turn_id TEXT, input_id TEXT, message_id TEXT, state TEXT, failure TEXT, mode TEXT,
 finished_at INTEGER, text TEXT, omitted_parts INTEGER,
 CHECK(child_id <> parent_id),
 CHECK((turn_id IS NULL) = (state IS NULL)),
 CHECK((turn_id IS NULL) = (mode IS NULL)),
 CHECK((turn_id IS NULL) = (finished_at IS NULL)),
 CHECK((turn_id IS NULL) = (text IS NULL)),
 CHECK((turn_id IS NULL) = (omitted_parts IS NULL)),
 CHECK(turn_id IS NOT NULL OR (input_id IS NULL AND message_id IS NULL AND failure IS NULL)),
 CHECK(state IS NULL OR state IN ('succeeded','failed','cancelled','interrupted')),
 CHECK(state <> 'succeeded' OR failure IS NULL),
 CHECK(mode IS NULL OR mode IN ('notice','inline','message')),
 CHECK(mode <> 'message' OR state <> 'succeeded'),
 CHECK(text IS NULL OR length(CAST(text AS BLOB)) <= 1048576),
 CHECK(failure IS NULL OR length(CAST(failure AS BLOB)) <= 16384),
 CHECK(omitted_parts IS NULL OR omitted_parts >= 0)
) STRICT;
CREATE INDEX completion_parent ON completion_slots(parent_id,child_id);
CREATE INDEX completion_pending ON completion_slots(child_id) WHERE turn_id IS NOT NULL;
CREATE TRIGGER completion_identity_immutable BEFORE UPDATE ON completion_slots
 WHEN NEW.child_id IS NOT OLD.child_id OR NEW.parent_id IS NOT OLD.parent_id
 BEGIN SELECT RAISE(ABORT, 'completion slot identity is immutable'); END;

-- Execution permission is separate from turn outcome: waiting turns retain
-- their input and history without consuming runnable descendant capacity.
CREATE TABLE turn_permits (
 turn_id TEXT PRIMARY KEY REFERENCES turns(id) ON DELETE CASCADE
) STRICT;
CREATE TRIGGER permit_active BEFORE INSERT ON turn_permits
 WHEN NOT EXISTS(SELECT 1 FROM turns t JOIN sessions s ON s.id=t.session_id
 WHERE t.id=NEW.turn_id AND t.state='running' AND s.lifecycle='active')
 BEGIN SELECT RAISE(ABORT, 'permit requires an active running turn'); END;
CREATE TRIGGER permit_immutable BEFORE UPDATE ON turn_permits
 BEGIN SELECT RAISE(ABORT, 'turn permit identity is immutable'); END;
CREATE TRIGGER terminal_turn_no_permit BEFORE UPDATE ON turns
 WHEN NEW.finished_at IS NOT NULL AND EXISTS(SELECT 1 FROM turn_permits WHERE turn_id=NEW.id)
 BEGIN SELECT RAISE(ABORT, 'terminal turn must release execution permit'); END;

-- Schedule identity/digest survives owner deletion as a retry tombstone. The
-- only scheduling cursor is next_due; admitted work belongs to ordinary inputs.
CREATE TABLE schedules (
 id TEXT PRIMARY KEY, session_id TEXT NOT NULL, initial_digest TEXT NOT NULL,
 first_due TEXT, every_ns INTEGER, next_due TEXT, parts TEXT, failure TEXT,
 cancelled_at INTEGER, created_at INTEGER NOT NULL, deleted_at INTEGER,
 UNIQUE(id,session_id),
 CHECK((first_due IS NULL) = (deleted_at IS NOT NULL)),
 CHECK((parts IS NULL) = (deleted_at IS NOT NULL)),
 CHECK(first_due IS NULL OR length(first_due)=30),
 CHECK(next_due IS NULL OR length(next_due)=30),
 CHECK(every_ns IS NULL OR every_ns>0),
 CHECK(parts IS NULL OR (json_valid(parts) AND length(CAST(parts AS BLOB))<=1048576)),
 CHECK(failure IS NULL OR failure='successor_out_of_range'),
 CHECK(failure IS NULL OR next_due IS NOT NULL),
 CHECK(deleted_at IS NULL OR (every_ns IS NULL AND next_due IS NULL AND failure IS NULL AND cancelled_at IS NULL))
) STRICT;
CREATE INDEX schedule_owner ON schedules(session_id,id);
CREATE INDEX schedules_due ON schedules(next_due,id)
 WHERE next_due IS NOT NULL AND cancelled_at IS NULL AND deleted_at IS NULL AND failure IS NULL;
CREATE TRIGGER schedule_identity_immutable BEFORE UPDATE ON schedules
 WHEN NEW.id IS NOT OLD.id OR NEW.session_id IS NOT OLD.session_id
 OR NEW.initial_digest IS NOT OLD.initial_digest OR NEW.created_at IS NOT OLD.created_at
 OR OLD.deleted_at IS NOT NULL
 OR (NEW.deleted_at IS NULL AND (NEW.first_due IS NOT OLD.first_due OR NEW.every_ns IS NOT OLD.every_ns OR NEW.parts IS NOT OLD.parts))
 BEGIN SELECT RAISE(ABORT, 'schedule identity is immutable'); END;

-- Goal identities survive owner deletion for exact creation retries. Current is
-- the latest ordinal, including terminal records; no separate selection head.
CREATE TABLE goals (
 ordinal INTEGER PRIMARY KEY AUTOINCREMENT, id TEXT NOT NULL UNIQUE,
 session_id TEXT NOT NULL, initial_digest TEXT NOT NULL,
 text TEXT, max_continuations INTEGER NOT NULL CHECK(max_continuations>=0),
 revision INTEGER NOT NULL CHECK(revision>0), state TEXT NOT NULL
 CHECK(state IN ('armed','paused','completed','cancelled','superseded')),
 continuations_used INTEGER NOT NULL CHECK(continuations_used BETWEEN 0 AND max_continuations),
 stop_reason TEXT, created_at INTEGER NOT NULL, deleted_at INTEGER,
 completion_turn_id TEXT, completion_operation_id TEXT REFERENCES operations(id),
 origin_formulation_attempt_id TEXT UNIQUE,
 FOREIGN KEY(completion_turn_id,session_id) REFERENCES turns(id,session_id),
 CHECK((completion_turn_id IS NULL) = (completion_operation_id IS NULL)),
 CHECK(completion_turn_id IS NULL OR state='completed'),
 UNIQUE(id,session_id), CHECK((text IS NULL) = (deleted_at IS NOT NULL)),
 CHECK(text IS NULL OR length(CAST(text AS BLOB))<=1048576),
 CHECK(stop_reason IS NULL OR length(CAST(stop_reason AS BLOB))<=16384)
) STRICT;
CREATE INDEX goal_completion_turn ON goals(completion_turn_id) WHERE completion_turn_id IS NOT NULL;
CREATE INDEX goal_completion_operation ON goals(completion_operation_id) WHERE completion_operation_id IS NOT NULL;
CREATE INDEX goals_owner ON goals(session_id,ordinal DESC);
CREATE UNIQUE INDEX one_open_goal ON goals(session_id)
 WHERE deleted_at IS NULL AND state IN ('armed','paused');
CREATE TRIGGER goal_identity_immutable BEFORE UPDATE ON goals
 WHEN NEW.id IS NOT OLD.id OR NEW.ordinal IS NOT OLD.ordinal OR NEW.session_id IS NOT OLD.session_id
 OR NEW.initial_digest IS NOT OLD.initial_digest OR NEW.created_at IS NOT OLD.created_at
 OR NEW.max_continuations IS NOT OLD.max_continuations OR OLD.deleted_at IS NOT NULL
 OR NEW.origin_formulation_attempt_id IS NOT OLD.origin_formulation_attempt_id
 OR (NEW.deleted_at IS NULL AND (NEW.text IS NOT OLD.text OR NEW.revision<>OLD.revision+1
 OR OLD.state NOT IN ('armed','paused') OR NEW.continuations_used<OLD.continuations_used))
 BEGIN SELECT RAISE(ABORT, 'invalid goal change'); END;

CREATE TABLE inputs (
 ordinal INTEGER PRIMARY KEY AUTOINCREMENT, id TEXT NOT NULL UNIQUE,
 session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
 source TEXT NOT NULL CHECK(source IN ('user','agent','schedule','goal')),
 kind TEXT NOT NULL DEFAULT 'prompt' CHECK(kind IN ('prompt','compact','goal_formulation','automatic_title')),
 parts TEXT NOT NULL CHECK(json_valid(parts)), turn_id TEXT UNIQUE, cancelled_at INTEGER, created_at INTEGER NOT NULL,
 schedule_id TEXT, scheduled_for TEXT, goal_id TEXT, goal_revision INTEGER,
 CHECK((goal_id IS NOT NULL) = (source='goal')),
 CHECK((goal_revision IS NULL) = (goal_id IS NULL)),
 CHECK(goal_revision IS NULL OR (goal_revision>0 AND kind='prompt')),
 FOREIGN KEY(goal_id,session_id) REFERENCES goals(id,session_id),
 CHECK((schedule_id IS NOT NULL) = (source='schedule')),
 CHECK((scheduled_for IS NULL) = (schedule_id IS NULL)),
 CHECK(scheduled_for IS NULL OR (length(scheduled_for)=30 AND kind='prompt')),
 UNIQUE(schedule_id,scheduled_for),
 FOREIGN KEY(schedule_id,session_id) REFERENCES schedules(id,session_id),
 CHECK(kind='prompt' OR (json_type(parts)='array' AND json_array_length(parts)=0)),
 CHECK(turn_id IS NULL OR cancelled_at IS NULL), UNIQUE(id,turn_id,session_id),
 FOREIGN KEY(turn_id,session_id) REFERENCES turns(id,session_id) ON DELETE CASCADE
) STRICT;
CREATE INDEX goal_inputs ON inputs(goal_id,ordinal DESC) WHERE goal_id IS NOT NULL;
CREATE INDEX schedule_inputs ON inputs(schedule_id,ordinal DESC) WHERE schedule_id IS NOT NULL;
CREATE INDEX queued_inputs ON inputs(session_id,ordinal) WHERE turn_id IS NULL AND cancelled_at IS NULL;
CREATE TRIGGER input_immutable BEFORE UPDATE ON inputs
 WHEN NEW.id IS NOT OLD.id OR NEW.ordinal IS NOT OLD.ordinal OR NEW.session_id IS NOT OLD.session_id
 OR NEW.goal_id IS NOT OLD.goal_id OR NEW.goal_revision IS NOT OLD.goal_revision
 OR NEW.schedule_id IS NOT OLD.schedule_id OR NEW.scheduled_for IS NOT OLD.scheduled_for
 OR NEW.source IS NOT OLD.source OR NEW.kind IS NOT OLD.kind OR NEW.parts IS NOT OLD.parts OR NEW.created_at IS NOT OLD.created_at
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
-- Mail identity survives recipient deletion solely as a send-retry tombstone.
CREATE TABLE mail (
 id TEXT PRIMARY KEY, source_kind TEXT NOT NULL CHECK(source_kind IN ('session','state','completion')), source_id TEXT NOT NULL, recipient_id TEXT NOT NULL,
 initial_digest TEXT NOT NULL, revision INTEGER, state TEXT,
 created_at INTEGER NOT NULL, deleted_at INTEGER, UNIQUE(id,recipient_id),
 CHECK((revision IS NULL) = (deleted_at IS NOT NULL)),
 CHECK((state IS NULL) = (deleted_at IS NOT NULL)),
 CHECK(revision IS NULL OR revision BETWEEN 1 AND 128),
 CHECK(state IS NULL OR state IN ('pending','delivered','done')),
 FOREIGN KEY(id,revision) REFERENCES mail_revisions(mail_id,revision) DEFERRABLE INITIALLY DEFERRED
) STRICT;
CREATE INDEX mail_by_recipient ON mail(recipient_id,id) WHERE deleted_at IS NULL;
CREATE INDEX mail_source_rate ON mail(source_kind,source_id,created_at);
CREATE TABLE mail_revisions (
 mail_id TEXT NOT NULL, revision INTEGER NOT NULL CHECK(revision BETWEEN 1 AND 128),
 delivery TEXT NOT NULL CHECK(delivery IN ('queued','steer','next_turn')),
 subject TEXT NOT NULL, body TEXT NOT NULL, available_at INTEGER NOT NULL, created_at INTEGER NOT NULL,
 evidence_ref TEXT, recipient_id TEXT NOT NULL,
 PRIMARY KEY(mail_id,revision),
 FOREIGN KEY(mail_id,recipient_id) REFERENCES mail(id,recipient_id),
 FOREIGN KEY(recipient_id,evidence_ref) REFERENCES content_references(owner_session_id,reference_id) DEFERRABLE INITIALLY DEFERRED
) STRICT;
CREATE INDEX mail_revision_evidence ON mail_revisions(recipient_id,evidence_ref) WHERE evidence_ref IS NOT NULL;
CREATE TRIGGER mail_revision_immutable BEFORE UPDATE ON mail_revisions
 BEGIN SELECT RAISE(ABORT, 'mail revision is immutable'); END;
CREATE TRIGGER mail_identity_immutable BEFORE UPDATE ON mail
 WHEN NEW.id IS NOT OLD.id OR NEW.source_kind IS NOT OLD.source_kind OR NEW.source_id IS NOT OLD.source_id OR NEW.recipient_id IS NOT OLD.recipient_id
 OR NEW.initial_digest IS NOT OLD.initial_digest OR NEW.created_at IS NOT OLD.created_at OR OLD.deleted_at IS NOT NULL
 OR (NEW.deleted_at IS NULL AND NEW.revision NOT IN (OLD.revision,OLD.revision+1))
 BEGIN SELECT RAISE(ABORT, 'invalid mail identity transition'); END;
CREATE TABLE turn_mail_observations (
 turn_id TEXT NOT NULL REFERENCES turns(id) ON DELETE CASCADE,
 mail_id TEXT NOT NULL, revision INTEGER NOT NULL, presented INTEGER NOT NULL CHECK(presented IN (0,1)),
 PRIMARY KEY(turn_id,mail_id,revision),
 FOREIGN KEY(mail_id,revision) REFERENCES mail_revisions(mail_id,revision) ON DELETE CASCADE
) STRICT;
CREATE TRIGGER mail_observation_immutable BEFORE UPDATE ON turn_mail_observations
 WHEN NEW.turn_id IS NOT OLD.turn_id OR NEW.mail_id IS NOT OLD.mail_id OR NEW.revision IS NOT OLD.revision OR NEW.presented<OLD.presented
 BEGIN SELECT RAISE(ABORT, 'invalid mail observation'); END;
CREATE TABLE history_groups (
 id TEXT PRIMARY KEY, session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
 turn_id TEXT UNIQUE, source_session_id TEXT, source_group_id TEXT,
 created_at INTEGER NOT NULL, UNIQUE(id,session_id),
 CHECK((source_session_id IS NULL)=(source_group_id IS NULL)),
 CHECK((turn_id IS NULL)=(source_session_id IS NOT NULL)),
 CHECK(turn_id IS NULL OR id=turn_id),
 FOREIGN KEY(turn_id,session_id) REFERENCES turns(id,session_id) ON DELETE CASCADE
) STRICT;
CREATE TRIGGER history_group_immutable BEFORE UPDATE ON history_groups
 BEGIN SELECT RAISE(ABORT, 'history group is immutable'); END;
CREATE TABLE history_edits (
 id TEXT PRIMARY KEY, session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
 digest TEXT NOT NULL CHECK(length(digest)=64 AND digest NOT GLOB '*[^0-9a-f]*'),
 expected_revision INTEGER NOT NULL CHECK(expected_revision>0),
 revision INTEGER NOT NULL CHECK(revision=expected_revision+1),
 observed_through INTEGER NOT NULL CHECK(observed_through>=0),
 keep_through INTEGER NOT NULL CHECK(keep_through BETWEEN 0 AND observed_through),
 created_at INTEGER NOT NULL,
 UNIQUE(session_id,revision), UNIQUE(id,session_id,revision)
) STRICT;
CREATE TRIGGER history_edit_immutable BEFORE UPDATE ON history_edits
 BEGIN SELECT RAISE(ABORT, 'history edit is immutable'); END;
CREATE TABLE messages (
 id TEXT PRIMARY KEY, session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
 turn_id TEXT, group_id TEXT NOT NULL, sequence INTEGER NOT NULL CHECK(sequence > 0),
 opening_input INTEGER NOT NULL DEFAULT 0 CHECK(opening_input IN (0,1)),
 source_session_id TEXT, source_message_id TEXT, source_sequence INTEGER,
 retired_by TEXT, retired_revision INTEGER,
 role TEXT NOT NULL CHECK(role IN ('system','user','assistant','tool')),
 input_id TEXT, parts TEXT CHECK(parts IS NULL OR json_valid(parts)), created_at INTEGER NOT NULL,
 model_continuation TEXT CHECK(model_continuation IS NULL OR
  (role='assistant' AND json_valid(model_continuation) AND length(CAST(model_continuation AS BLOB))<=1048576)),
 mail_id TEXT, mail_revision INTEGER, mail_presentation TEXT CHECK(mail_presentation IS NULL OR mail_presentation IN ('digest','body')),
 UNIQUE(session_id,sequence), UNIQUE(input_id), UNIQUE(id,turn_id),
 CHECK((input_id IS NOT NULL)+(parts IS NOT NULL)+(mail_id IS NOT NULL)=1),
 CHECK((mail_id IS NULL)=(mail_revision IS NULL)), CHECK((mail_id IS NULL)=(mail_presentation IS NULL)),
 CHECK((source_session_id IS NULL)=(source_message_id IS NULL)),
 CHECK((source_session_id IS NULL)=(source_sequence IS NULL)),
 CHECK(source_sequence IS NULL OR source_sequence>0),
 CHECK((turn_id IS NULL)=(source_session_id IS NOT NULL)),
 CHECK(source_session_id IS NULL OR (parts IS NOT NULL AND input_id IS NULL AND mail_id IS NULL)),
 CHECK(source_session_id IS NOT NULL OR (role='user')=(input_id IS NOT NULL OR mail_id IS NOT NULL)),
 CHECK(source_session_id IS NOT NULL OR opening_input=(input_id IS NOT NULL)),
 CHECK(opening_input=0 OR role='user'),
 CHECK((retired_by IS NULL)=(retired_revision IS NULL)),
 FOREIGN KEY(group_id,session_id) REFERENCES history_groups(id,session_id) ON DELETE CASCADE,
 FOREIGN KEY(retired_by,session_id,retired_revision) REFERENCES history_edits(id,session_id,revision),
 FOREIGN KEY(mail_id,mail_revision) REFERENCES mail_revisions(mail_id,revision) ON DELETE CASCADE,
 FOREIGN KEY(turn_id,session_id) REFERENCES turns(id,session_id) ON DELETE CASCADE,
 FOREIGN KEY(input_id,turn_id,session_id) REFERENCES inputs(id,turn_id,session_id) ON DELETE CASCADE
) STRICT;
CREATE INDEX message_turn_role_sequence ON messages(turn_id,role,sequence DESC);
CREATE INDEX message_group_sequence ON messages(group_id,sequence);
CREATE INDEX active_history_sequence ON messages(session_id,sequence) WHERE retired_revision IS NULL;
CREATE INDEX message_retirement ON messages(retired_by,session_id,retired_revision) WHERE retired_by IS NOT NULL;
CREATE INDEX message_retired_coverage ON messages(session_id,retired_revision,sequence) WHERE retired_revision IS NOT NULL;
CREATE UNIQUE INDEX group_opening_input ON messages(group_id) WHERE opening_input=1;
CREATE TRIGGER message_group_owner BEFORE INSERT ON messages
 WHEN NEW.retired_by IS NOT NULL
 OR NOT EXISTS(SELECT 1 FROM history_groups WHERE id=NEW.group_id AND session_id=NEW.session_id AND turn_id IS NEW.turn_id)
 OR (NEW.turn_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM turns t JOIN sessions s ON s.id=t.session_id
  WHERE t.id=NEW.turn_id AND t.history_revision=s.history_revision))
 BEGIN SELECT RAISE(ABORT, 'message must belong to its history group'); END;
CREATE TRIGGER message_immutable BEFORE UPDATE ON messages
 WHEN OLD.retired_by IS NOT NULL OR NEW.retired_by IS NULL
 OR NEW.id IS NOT OLD.id OR NEW.session_id IS NOT OLD.session_id OR NEW.turn_id IS NOT OLD.turn_id
 OR NEW.group_id IS NOT OLD.group_id OR NEW.sequence IS NOT OLD.sequence OR NEW.role IS NOT OLD.role
 OR NEW.opening_input IS NOT OLD.opening_input OR NEW.source_session_id IS NOT OLD.source_session_id
 OR NEW.source_message_id IS NOT OLD.source_message_id OR NEW.source_sequence IS NOT OLD.source_sequence
 OR NEW.input_id IS NOT OLD.input_id OR NEW.parts IS NOT OLD.parts OR NEW.created_at IS NOT OLD.created_at
 OR NEW.model_continuation IS NOT OLD.model_continuation OR NEW.mail_id IS NOT OLD.mail_id
 OR NEW.mail_revision IS NOT OLD.mail_revision OR NEW.mail_presentation IS NOT OLD.mail_presentation
 OR NOT EXISTS(SELECT 1 FROM history_edits e JOIN sessions s ON s.id=e.session_id WHERE e.id=NEW.retired_by AND e.session_id=NEW.session_id
  AND e.revision=NEW.retired_revision AND s.history_revision=e.revision AND NEW.sequence>e.keep_through AND NEW.sequence<=e.observed_through)
 BEGIN SELECT RAISE(ABORT, 'transcript entry only permits exact retirement'); END;

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

CREATE TABLE operations (
 id TEXT PRIMARY KEY, cell_id TEXT NOT NULL REFERENCES cells(id) ON DELETE CASCADE,
 request_id TEXT NOT NULL, capability TEXT NOT NULL, resource TEXT NOT NULL,
 arguments TEXT NOT NULL CHECK(json_valid(arguments) AND json_type(arguments)='object'),
 state TEXT NOT NULL CHECK(state IN ('waiting','ready','dispatched','succeeded','failed','denied','cancelled','uncertain')),
 grant_id TEXT REFERENCES grants(id) DEFERRABLE INITIALLY DEFERRED,
 permission_revision INTEGER CHECK(permission_revision>0),
 result TEXT CHECK(result IS NULL OR json_valid(result)),
 created_at INTEGER NOT NULL, dispatched_at INTEGER, finished_at INTEGER,
 UNIQUE(cell_id,request_id),
 CHECK((state IN ('waiting','ready','dispatched')) = (finished_at IS NULL)),
 CHECK((result IS NULL) = (finished_at IS NULL)),
 CHECK(result IS NULL OR json_extract(result,'$.state') IS state),
 CHECK((state IN ('dispatched','succeeded','failed','uncertain')) = (dispatched_at IS NOT NULL)),
 CHECK(state <> 'waiting' OR (grant_id IS NULL AND permission_revision IS NULL)),
 CHECK(permission_revision IS NULL OR (grant_id IS NULL AND capability NOT IN ('user.ask','mcp.catalog','mcp.call','mcp.connect'))),
 CHECK(state NOT IN ('ready','dispatched','succeeded','failed','uncertain') OR grant_id IS NOT NULL OR permission_revision IS NOT NULL OR capability IN ('user.ask','mcp.catalog'))
) STRICT;
CREATE INDEX operations_by_cell ON operations(cell_id,id);
CREATE INDEX operations_by_grant ON operations(grant_id,id) WHERE state='ready';
CREATE INDEX operations_unfinished ON operations(id) WHERE finished_at IS NULL;
CREATE TRIGGER operation_transition BEFORE UPDATE ON operations
 WHEN NEW.id IS NOT OLD.id OR NEW.cell_id IS NOT OLD.cell_id OR NEW.request_id IS NOT OLD.request_id
 OR NEW.capability IS NOT OLD.capability OR NEW.resource IS NOT OLD.resource
 OR NEW.arguments IS NOT OLD.arguments OR NEW.created_at IS NOT OLD.created_at
 OR NEW.permission_revision IS NOT OLD.permission_revision
 OR OLD.state NOT IN ('waiting','ready','dispatched')
 OR (OLD.state='waiting' AND NEW.state NOT IN ('ready','denied','cancelled'))
 OR (OLD.state='ready' AND NEW.state NOT IN ('dispatched','denied','cancelled'))
 OR (OLD.state='dispatched' AND NEW.state NOT IN ('succeeded','failed','uncertain'))
 OR (OLD.state<>'waiting' AND NEW.grant_id IS NOT OLD.grant_id)
 OR (OLD.state='waiting' AND NEW.state<>'ready' AND NEW.grant_id IS NOT OLD.grant_id)
 OR (OLD.state='dispatched' AND NEW.dispatched_at IS NOT OLD.dispatched_at)
 BEGIN SELECT RAISE(ABORT, 'invalid operation transition'); END;

CREATE TABLE grants (
 id TEXT PRIMARY KEY, session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
 capability TEXT NOT NULL, resource TEXT NOT NULL,
 operation_id TEXT UNIQUE REFERENCES operations(id) ON DELETE CASCADE,
 issuer_id TEXT REFERENCES grants(id) ON DELETE CASCADE,
 created_at INTEGER NOT NULL, revoked_at INTEGER,
 CHECK(issuer_id IS NULL OR (operation_id IS NULL AND issuer_id<>id))
) STRICT;
CREATE INDEX grants_by_session ON grants(session_id,id);
CREATE INDEX grants_by_issuer ON grants(issuer_id,id);
CREATE INDEX standing_grant_scope ON grants(session_id,capability,resource,id)
 WHERE operation_id IS NULL AND revoked_at IS NULL;
CREATE TRIGGER grant_transition BEFORE UPDATE ON grants
 WHEN NEW.id IS NOT OLD.id OR NEW.session_id IS NOT OLD.session_id
 OR NEW.capability IS NOT OLD.capability OR NEW.resource IS NOT OLD.resource
 OR NEW.operation_id IS NOT OLD.operation_id OR NEW.issuer_id IS NOT OLD.issuer_id OR NEW.created_at IS NOT OLD.created_at
 OR OLD.revoked_at IS NOT NULL OR NEW.revoked_at IS NULL
 BEGIN SELECT RAISE(ABORT, 'grant may only be revoked'); END;

CREATE TABLE permissions (
 operation_id TEXT PRIMARY KEY REFERENCES operations(id) ON DELETE CASCADE,
 state TEXT NOT NULL CHECK(state IN ('pending','approved','denied','cancelled')),
 created_at INTEGER NOT NULL, resolved_at INTEGER,
 CHECK((state='pending') = (resolved_at IS NULL))
) STRICT;
CREATE TRIGGER permission_transition BEFORE UPDATE ON permissions
 WHEN NEW.operation_id IS NOT OLD.operation_id OR NEW.created_at IS NOT OLD.created_at
 OR OLD.state<>'pending' OR NEW.state='pending'
 BEGIN SELECT RAISE(ABORT, 'permission decision is immutable'); END;

-- Intent and answers live only in operations.arguments/result. Closure records
-- why an unanswered human interaction ended, never a resumable waiter.
CREATE TABLE questions (
 operation_id TEXT PRIMARY KEY REFERENCES operations(id) ON DELETE CASCADE,
 created_at INTEGER NOT NULL, deadline INTEGER NOT NULL CHECK(deadline=created_at+300000000),
 close_reason TEXT CHECK(close_reason IN ('cancelled','expired','interrupted'))
) STRICT;
CREATE TRIGGER question_transition BEFORE UPDATE ON questions
 WHEN NEW.operation_id IS NOT OLD.operation_id OR NEW.created_at IS NOT OLD.created_at
 OR NEW.deadline IS NOT OLD.deadline OR OLD.close_reason IS NOT NULL OR NEW.close_reason IS NULL
 BEGIN SELECT RAISE(ABORT, 'question may only close once'); END;

CREATE TABLE model_attempts (
 id TEXT PRIMARY KEY, turn_id TEXT NOT NULL,
 logical_id TEXT NOT NULL, number INTEGER NOT NULL CHECK(number BETWEEN 1 AND 100),
 request TEXT NOT NULL CHECK(json_valid(request)),
 -- Provenance outlives deleted operations so ancestor accounting stays intact.
 operation_id TEXT, batch_index INTEGER CHECK(batch_index IS NULL OR batch_index BETWEEN 0 AND 31),
 state TEXT NOT NULL CHECK(state IN ('reserved','dispatched','succeeded','failed','cancelled','uncertain')),
 result TEXT CHECK(result IS NULL OR json_valid(result)),
 cost_nano_usd INTEGER CHECK(cost_nano_usd IS NULL OR cost_nano_usd>=0),
 cost_source TEXT NOT NULL CHECK(cost_source IN ('unknown','provider','prices','not_dispatched')),
 cost_note TEXT,
 message_id TEXT UNIQUE,
 created_at INTEGER NOT NULL, dispatched_at INTEGER, finished_at INTEGER,
 UNIQUE(turn_id,logical_id,number), UNIQUE(id,turn_id),
 UNIQUE(operation_id,batch_index,number),
 CHECK((operation_id IS NULL)=(batch_index IS NULL)),
 CHECK((json_extract(request,'$.purpose')='model_helper')=(operation_id IS NOT NULL)),
 CHECK((state IN ('reserved','dispatched')) = (finished_at IS NULL)),
 CHECK((result IS NULL) = (finished_at IS NULL)),
 CHECK(result IS NULL OR json_extract(result,'$.state') IS state),
 CHECK((cost_nano_usd IS NULL) = (cost_source='unknown')),
 CHECK(state NOT IN ('dispatched','succeeded','failed','uncertain') OR dispatched_at IS NOT NULL),
 CHECK(state <> 'reserved' OR dispatched_at IS NULL),
 CHECK(state <> 'cancelled' OR dispatched_at IS NULL),
 CHECK(message_id IS NULL OR finished_at IS NOT NULL),
 CHECK(message_id IS NULL OR json_extract(request,'$.purpose') NOT IN ('compaction','model_helper','goal_formulation','automatic_title'))
) STRICT;
CREATE INDEX attempts_by_turn ON model_attempts(turn_id,id);
CREATE INDEX attempts_unfinished ON model_attempts(id) WHERE finished_at IS NULL;
CREATE TRIGGER attempt_transition BEFORE UPDATE ON model_attempts
 WHEN NEW.id IS NOT OLD.id OR NEW.turn_id IS NOT OLD.turn_id OR NEW.logical_id IS NOT OLD.logical_id
 OR NEW.operation_id IS NOT OLD.operation_id OR NEW.batch_index IS NOT OLD.batch_index
 OR NEW.number IS NOT OLD.number OR NEW.request IS NOT OLD.request OR NEW.created_at IS NOT OLD.created_at
 OR OLD.state NOT IN ('reserved','dispatched')
 OR (OLD.state='reserved' AND NEW.state NOT IN ('dispatched','cancelled'))
 OR (OLD.state='dispatched' AND (NEW.state IN ('reserved','dispatched') OR NEW.dispatched_at IS NOT OLD.dispatched_at))
 BEGIN SELECT RAISE(ABORT, 'invalid model attempt transition'); END;

CREATE TABLE compactions (
 id TEXT PRIMARY KEY,
 session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
 turn_id TEXT,
 attempt_id TEXT UNIQUE,
 history_revision INTEGER NOT NULL CHECK(history_revision>0),
 source_session_id TEXT, source_compaction_id TEXT,
 base_id TEXT,
 expected_revision INTEGER NOT NULL CHECK(expected_revision >= 0),
 through_sequence INTEGER NOT NULL CHECK(through_sequence > 0),
 pinned_message_ids TEXT NOT NULL CHECK(json_valid(pinned_message_ids) AND json_type(pinned_message_ids)='array' AND json_array_length(pinned_message_ids) <= 32),
 text TEXT NOT NULL CHECK(length(CAST(text AS BLOB)) BETWEEN 1 AND 65536),
 created_at INTEGER NOT NULL,
 UNIQUE(id,session_id),
 CHECK(base_id IS NULL OR base_id<>id),
 CHECK((source_session_id IS NULL)=(source_compaction_id IS NULL)),
 CHECK((turn_id IS NULL)=(source_session_id IS NOT NULL)),
 CHECK((attempt_id IS NULL)=(source_session_id IS NOT NULL)),
 FOREIGN KEY(turn_id,session_id) REFERENCES turns(id,session_id) ON DELETE CASCADE,
 FOREIGN KEY(attempt_id,turn_id) REFERENCES model_attempts(id,turn_id) ON DELETE CASCADE,
 FOREIGN KEY(base_id,session_id) REFERENCES compactions(id,session_id) DEFERRABLE INITIALLY DEFERRED
) STRICT;
CREATE INDEX compactions_by_session ON compactions(session_id,id);
CREATE TRIGGER compaction_immutable BEFORE UPDATE ON compactions
 BEGIN SELECT RAISE(ABORT,'compaction is immutable'); END;
-- Fork receipts intentionally have no foreign keys: deleting either tree cannot
-- erase admission identity or recreate a deleted destination on retry.
CREATE TABLE forks (
 id TEXT PRIMARY KEY,
 digest TEXT NOT NULL CHECK(length(digest)=64),
 source_session_id TEXT NOT NULL,
 history_revision INTEGER NOT NULL CHECK(history_revision>0),
 config_revision INTEGER NOT NULL CHECK(config_revision>0),
 observed_through INTEGER NOT NULL CHECK(observed_through>=0),
 keep_through INTEGER NOT NULL CHECK(keep_through>=0 AND keep_through<=observed_through),
 title TEXT CHECK(title IS NULL OR length(CAST(title AS BLOB)) BETWEEN 1 AND 1024),
 tree_id TEXT NOT NULL UNIQUE,
 root_id TEXT NOT NULL UNIQUE,
 created_at INTEGER NOT NULL
) STRICT;
CREATE TRIGGER fork_immutable BEFORE UPDATE ON forks
 BEGIN SELECT RAISE(ABORT,'fork receipt is immutable'); END;
CREATE TRIGGER fork_retained BEFORE DELETE ON forks
 BEGIN SELECT RAISE(ABORT,'fork receipt is retained'); END;
-- Human workspace actions own no fabricated turn/cell. Retained receipts and
-- pins must be explicitly released before session deletion.
CREATE TABLE workspace_snapshots (
 id TEXT PRIMARY KEY,
 session_id TEXT NOT NULL,
 capture_id TEXT NOT NULL UNIQUE REFERENCES workspace_actions(id) DEFERRABLE INITIALLY DEFERRED,
 binding TEXT NOT NULL CHECK(json_valid(binding) AND json_type(binding)='object'),
 object_id TEXT CHECK(object_id IS NULL OR (length(object_id) IN(40,64) AND object_id NOT GLOB '*[^0-9a-f]*')),
 created_at INTEGER NOT NULL,
 released_at INTEGER,
 UNIQUE(id,session_id)
) STRICT;
CREATE INDEX workspace_pins ON workspace_snapshots(session_id,id) WHERE released_at IS NULL;
CREATE TABLE workspace_actions (
 id TEXT PRIMARY KEY,
 snapshot_id TEXT NOT NULL,
 session_id TEXT NOT NULL,
 kind TEXT NOT NULL CHECK(kind IN('capture','restore','release')),
 digest TEXT NOT NULL CHECK(length(digest)=64),
 state TEXT NOT NULL CHECK(state IN('claimed','succeeded','uncertain')),
 failure TEXT CHECK(failure IS NULL OR length(CAST(failure AS BLOB)) BETWEEN 1 AND 1024),
 created_at INTEGER NOT NULL,
 finished_at INTEGER,
 CHECK((state='claimed')=(finished_at IS NULL)),
 CHECK((state='uncertain')=(failure IS NOT NULL)),
 FOREIGN KEY(snapshot_id,session_id) REFERENCES workspace_snapshots(id,session_id)
) STRICT;
CREATE UNIQUE INDEX workspace_owner_claim ON workspace_actions(session_id) WHERE state='claimed';
CREATE INDEX workspace_action_owner ON workspace_actions(session_id,id);
CREATE TRIGGER workspace_action_transition BEFORE UPDATE ON workspace_actions
 WHEN NEW.id IS NOT OLD.id OR NEW.snapshot_id IS NOT OLD.snapshot_id OR NEW.session_id IS NOT OLD.session_id
 OR NEW.kind IS NOT OLD.kind OR NEW.digest IS NOT OLD.digest OR NEW.created_at IS NOT OLD.created_at
 OR OLD.state<>'claimed' OR NEW.state='claimed'
 BEGIN SELECT RAISE(ABORT,'invalid workspace action transition'); END;
CREATE TRIGGER workspace_snapshot_transition BEFORE UPDATE ON workspace_snapshots
 WHEN NEW.id IS NOT OLD.id OR NEW.session_id IS NOT OLD.session_id OR NEW.capture_id IS NOT OLD.capture_id
 OR NEW.binding IS NOT OLD.binding OR NEW.created_at IS NOT OLD.created_at OR OLD.released_at IS NOT NULL
 OR (NEW.object_id IS NOT OLD.object_id AND (OLD.object_id IS NOT NULL OR NEW.object_id IS NULL))
 OR (NEW.object_id IS NOT OLD.object_id AND NEW.released_at IS NOT OLD.released_at)
 BEGIN SELECT RAISE(ABORT,'invalid workspace snapshot transition'); END;
CREATE TRIGGER workspace_snapshot_retained BEFORE DELETE ON workspace_snapshots
 BEGIN SELECT RAISE(ABORT,'workspace snapshot receipt is retained'); END;
CREATE TRIGGER workspace_action_retained BEFORE DELETE ON workspace_actions
 BEGIN SELECT RAISE(ABORT,'workspace action receipt is retained'); END;
CREATE TABLE context_heads (
 session_id TEXT PRIMARY KEY REFERENCES sessions(id) ON DELETE CASCADE,
 revision INTEGER NOT NULL CHECK(revision > 0),
 compaction_id TEXT,
 FOREIGN KEY(compaction_id,session_id) REFERENCES compactions(id,session_id) DEFERRABLE INITIALLY DEFERRED
) STRICT;

-- Reusable capacity derives usage from its owning rows and their lifecycle.
CREATE TABLE resource_limits (
 session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
 kind TEXT NOT NULL CHECK(kind IN ('depth','descendants','queued_inputs','active_operations','subscriptions','runnable_descendants','schedules')),
 revision INTEGER NOT NULL CHECK(revision>0),
 limit_value INTEGER CHECK(limit_value IS NULL OR limit_value>=0),
 PRIMARY KEY(session_id,kind)
) STRICT;

-- Limits are mutable policy. Accounting derives from attempts and logical writes,
-- including evidence whose original session or source record has been deleted.
CREATE TABLE budget_limits (
 session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
 kind TEXT NOT NULL CHECK(kind IN ('model_calls','model_tokens','model_cost_nano_usd','model_elapsed_millis','logical_writes','logical_write_bytes')),
 revision INTEGER NOT NULL CHECK(revision>0),
 limit_value INTEGER CHECK(limit_value IS NULL OR limit_value>=0),
 PRIMARY KEY(session_id,kind)
) STRICT;
CREATE TABLE attempt_budget_ancestors (
 attempt_id TEXT NOT NULL REFERENCES model_attempts(id) ON DELETE CASCADE,
 session_id TEXT NOT NULL,
 PRIMARY KEY(attempt_id,session_id)
) STRICT;
CREATE INDEX budget_attempts_by_session ON attempt_budget_ancestors(session_id,attempt_id);
CREATE TRIGGER attempt_budget_ancestors_immutable BEFORE UPDATE ON attempt_budget_ancestors
 BEGIN SELECT RAISE(ABORT, 'attempt budget ancestry is immutable'); END;

-- Logical-write charges outlive their source session and records. Only deleting
-- the entire tree discards this permanent accounting evidence.
CREATE TABLE logical_writes (
 id TEXT PRIMARY KEY,
 tree_id TEXT NOT NULL REFERENCES session_trees(id) ON DELETE CASCADE,
 author_id TEXT NOT NULL,
 source_kind TEXT NOT NULL CHECK(source_kind IN ('mail','input','content','state','subscription','schedule','goal')),
 source_id TEXT NOT NULL, source_revision INTEGER NOT NULL CHECK(source_revision>=0),
 bytes INTEGER NOT NULL CHECK(bytes>=0), created_at INTEGER NOT NULL,
 UNIQUE(source_kind,source_id,source_revision)
) STRICT;
CREATE INDEX logical_writes_by_tree ON logical_writes(tree_id,id);
CREATE TRIGGER logical_write_immutable BEFORE UPDATE ON logical_writes
 BEGIN SELECT RAISE(ABORT,'logical write is immutable'); END;
CREATE TABLE logical_write_ancestors (
 write_id TEXT NOT NULL REFERENCES logical_writes(id) ON DELETE CASCADE,
 session_id TEXT NOT NULL,
 PRIMARY KEY(write_id,session_id)
) STRICT;
CREATE INDEX logical_writes_by_ancestor ON logical_write_ancestors(session_id,write_id);
CREATE TRIGGER logical_write_ancestor_immutable BEFORE UPDATE ON logical_write_ancestors
 BEGIN SELECT RAISE(ABORT,'logical write ancestry is immutable'); END;

-- Explicit application state is immutable JSON content, separate from VM images.
-- Shared values outlive their author; private values follow their owning session.
CREATE TABLE state_versions (
 id TEXT PRIMARY KEY, tree_id TEXT NOT NULL REFERENCES session_trees(id) ON DELETE CASCADE,
 session_id TEXT, key TEXT NOT NULL, revision INTEGER NOT NULL CHECK(revision>0),
 author_id TEXT NOT NULL, digest TEXT NOT NULL REFERENCES content_bodies(digest), created_at INTEGER NOT NULL,
 FOREIGN KEY(session_id,tree_id) REFERENCES sessions(id,tree_id) ON DELETE CASCADE
) STRICT;
CREATE UNIQUE INDEX private_state_versions ON state_versions(session_id,key,revision) WHERE session_id IS NOT NULL;
CREATE UNIQUE INDEX shared_state_versions ON state_versions(tree_id,key,revision) WHERE session_id IS NULL;
CREATE INDEX state_body_references ON state_versions(digest);
CREATE TRIGGER state_version_immutable BEFORE UPDATE ON state_versions
 BEGIN SELECT RAISE(ABORT, 'state version is immutable'); END;

CREATE TABLE state_subscriptions (
 id TEXT PRIMARY KEY, tree_id TEXT NOT NULL REFERENCES session_trees(id) ON DELETE CASCADE,
 session_id TEXT NOT NULL, key TEXT NOT NULL, delivery TEXT NOT NULL CHECK(delivery IN ('queued','steer','next_turn')),
 initial_digest TEXT NOT NULL, cursor INTEGER NOT NULL CHECK(cursor>=0),
 created_at INTEGER NOT NULL, cancelled_at INTEGER,
 FOREIGN KEY(session_id,tree_id) REFERENCES sessions(id,tree_id) ON DELETE CASCADE
) STRICT;
CREATE UNIQUE INDEX active_state_subscriptions ON state_subscriptions(session_id,key) WHERE cancelled_at IS NULL;
CREATE INDEX subscribed_state_keys ON state_subscriptions(tree_id,key) WHERE cancelled_at IS NULL;
CREATE TRIGGER state_subscription_transition BEFORE UPDATE ON state_subscriptions
 WHEN NEW.id IS NOT OLD.id OR NEW.tree_id IS NOT OLD.tree_id OR NEW.session_id IS NOT OLD.session_id
 OR NEW.key IS NOT OLD.key OR NEW.delivery IS NOT OLD.delivery OR NEW.initial_digest IS NOT OLD.initial_digest
 OR NEW.created_at IS NOT OLD.created_at OR NEW.cursor<OLD.cursor OR OLD.cancelled_at IS NOT NULL
 BEGIN SELECT RAISE(ABORT,'invalid state subscription transition'); END;

-- Typed maintenance input payload, separate from prompt parts and goal work.
CREATE TABLE goal_formulation_inputs (
 input_id TEXT PRIMARY KEY REFERENCES inputs(id) ON DELETE CASCADE,
 snapshot TEXT NOT NULL CHECK(json_valid(snapshot) AND length(CAST(snapshot AS BLOB))<=4096)
) STRICT;
CREATE TRIGGER goal_formulation_input_immutable BEFORE UPDATE ON goal_formulation_inputs
 BEGIN SELECT RAISE(ABORT, 'formulation input is immutable'); END;
-- Candidates follow permanent attempt accounting, not deleted turns or inputs.
CREATE TABLE goal_formulations (
 attempt_id TEXT PRIMARY KEY REFERENCES model_attempts(id) ON DELETE CASCADE,
 session_id TEXT NOT NULL, turn_id TEXT NOT NULL,
 snapshot TEXT NOT NULL CHECK(json_valid(snapshot) AND length(CAST(snapshot AS BLOB))<=4096),
 text TEXT NOT NULL CHECK(length(CAST(text AS BLOB)) BETWEEN 1 AND 1048576),
 rejection TEXT, created_at INTEGER NOT NULL
) STRICT;
CREATE INDEX goal_formulations_owner ON goal_formulations(session_id,attempt_id);
CREATE INDEX goal_formulations_turn ON goal_formulations(turn_id);
CREATE TRIGGER goal_formulation_immutable BEFORE UPDATE ON goal_formulations
 BEGIN SELECT RAISE(ABORT, 'formulation candidate is immutable'); END;

CREATE TABLE automatic_title_decisions (
 tree_id TEXT PRIMARY KEY REFERENCES session_trees(id) ON DELETE CASCADE,
 session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
 config_revision INTEGER NOT NULL,
 expected_revision INTEGER NOT NULL CHECK(expected_revision>0),
 eligible INTEGER NOT NULL CHECK(eligible IN (0,1)),
 snapshot TEXT NOT NULL CHECK(json_valid(snapshot) AND length(CAST(snapshot AS BLOB))<=4096),
 created_at INTEGER NOT NULL,
 FOREIGN KEY(session_id,config_revision) REFERENCES session_configurations(session_id,revision)
) STRICT;
CREATE TRIGGER automatic_title_decision_immutable BEFORE UPDATE ON automatic_title_decisions
 BEGIN SELECT RAISE(ABORT, 'automatic title decision is immutable'); END;
CREATE INDEX automatic_title_pending ON automatic_title_decisions(eligible,created_at,session_id);
CREATE TABLE automatic_title_results (
 attempt_id TEXT PRIMARY KEY REFERENCES model_attempts(id) ON DELETE CASCADE,
 tree_id TEXT NOT NULL REFERENCES automatic_title_decisions(tree_id) ON DELETE CASCADE,
 text TEXT NOT NULL CHECK(length(CAST(text AS BLOB)) BETWEEN 1 AND 320),
 applied INTEGER NOT NULL CHECK(applied IN (0,1)), created_at INTEGER NOT NULL
) STRICT;
CREATE TRIGGER automatic_title_result_immutable BEFORE UPDATE ON automatic_title_results
 BEGIN SELECT RAISE(ABORT, 'automatic title result is immutable'); END;

-- Creation receipts retain caller delivery identity after destination deletion.
CREATE TABLE tree_creations (
 id TEXT PRIMARY KEY, digest TEXT NOT NULL,
 tree_id TEXT NOT NULL UNIQUE, root_id TEXT NOT NULL UNIQUE, created_at INTEGER NOT NULL
) STRICT;
CREATE TRIGGER tree_creation_immutable BEFORE UPDATE ON tree_creations BEGIN SELECT RAISE(ABORT,'immutable tree creation'); END;
CREATE TRIGGER tree_creation_retained BEFORE DELETE ON tree_creations BEGIN SELECT RAISE(ABORT,'retained tree creation'); END;

CREATE TABLE tree_catalog (
 singleton INTEGER PRIMARY KEY CHECK(singleton=1),
 revision INTEGER NOT NULL CHECK(revision>0)
) STRICT;
INSERT INTO tree_catalog VALUES (1,1);
CREATE TRIGGER tree_catalog_revision BEFORE UPDATE ON tree_catalog
 WHEN NEW.singleton<>OLD.singleton OR NEW.revision<>OLD.revision+1
 BEGIN SELECT RAISE(ABORT,'invalid catalog revision'); END;
CREATE TRIGGER tree_catalog_retained BEFORE DELETE ON tree_catalog BEGIN SELECT RAISE(ABORT,'retained tree catalog'); END;

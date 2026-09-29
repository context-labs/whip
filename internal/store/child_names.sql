CREATE TABLE child_names (
 session_id TEXT PRIMARY KEY REFERENCES sessions(id) ON DELETE CASCADE,
 name TEXT NOT NULL CHECK(length(CAST(name AS BLOB)) BETWEEN 1 AND 128)
) STRICT;
CREATE TRIGGER child_name_immutable BEFORE UPDATE ON child_names
 BEGIN SELECT RAISE(ABORT, 'child name is immutable'); END;
CREATE TRIGGER child_name_owner BEFORE INSERT ON child_names
 WHEN NOT EXISTS(SELECT 1 FROM sessions WHERE id=NEW.session_id AND parent_id IS NOT NULL)
 BEGIN SELECT RAISE(ABORT, 'child name requires a child'); END;

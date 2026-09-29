-- A row records ongoing inheritance from the direct parent. policy_revision is
-- the admission observation; operation revisions determine dispatch authority.
CREATE TABLE child_permission_policies (
 session_id TEXT PRIMARY KEY REFERENCES sessions(id) ON DELETE CASCADE,
 policy_revision INTEGER NOT NULL CHECK(policy_revision>0)
) STRICT;
CREATE TRIGGER child_permission_policy_immutable BEFORE UPDATE ON child_permission_policies
 BEGIN SELECT RAISE(ABORT, 'child permission delegation is immutable'); END;
CREATE TRIGGER child_permission_policy_owner BEFORE INSERT ON child_permission_policies
 WHEN NOT EXISTS(SELECT 1 FROM sessions WHERE id=NEW.session_id AND parent_id IS NOT NULL)
 BEGIN SELECT RAISE(ABORT, 'permission delegation requires a child'); END;

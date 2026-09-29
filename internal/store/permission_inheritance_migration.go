package store

import (
	"context"
	"database/sql"
)

// restoreChildPermissionInheritance recovers only a provable default spawn.
// Older direct-client receipts retain a digest but not the grant selection, so
// they cannot distinguish default inheritance from an explicit restriction.
// This never changes an operation outcome or admits an input.
func restoreChildPermissionInheritance(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO child_permission_policies(session_id,policy_revision)
 SELECT child.id,policy.revision
 FROM operations o
 JOIN cells c ON c.id=o.cell_id
 JOIN turns t ON t.id=c.turn_id
 JOIN receipts r ON r.client_id='operation' AND r.request_id=o.id
 JOIN inputs i ON i.id=r.input_id
 JOIN sessions child ON child.id=i.session_id AND child.parent_id=t.session_id
 JOIN sessions parent ON parent.id=child.parent_id AND parent.tree_id=child.tree_id
 JOIN session_configurations child_config ON child_config.session_id=child.id AND child_config.revision=child.config_revision
 JOIN session_configurations parent_config ON parent_config.session_id=parent.id AND parent_config.revision=parent.config_revision
 JOIN permission_policies policy ON policy.tree_id=child.tree_id
 WHERE o.capability='agents.spawn' AND o.state='succeeded'
 AND json_extract(o.arguments,'$.parent_id')=parent.id
 AND COALESCE(json_type(o.arguments,'$.grant_ids'),'null')='null'
 AND child_config.working_directory=parent_config.working_directory
 AND COALESCE(NULLIF(json_extract(o.arguments,'$.working_directory'),''),parent_config.working_directory)=child_config.working_directory
 AND NOT EXISTS(SELECT 1 FROM child_permission_policies inherited WHERE inherited.session_id=child.id)`)
	return err
}

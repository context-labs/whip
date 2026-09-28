package session

import (
	"context"
	"database/sql"
	"fmt"
)

// normalizeLegacyStamps runs inside the v20 upgrade transaction. Before v19,
// writers stored UTC RFC3339 seconds; mixing those with fixed-width fractions
// reverses same-second SQL ordering. Do not round existing nanoseconds or fill
// empty optional timestamps.
func normalizeLegacyStamps(ctx context.Context, conn *sql.Conn) error {
	if _, err := conn.ExecContext(ctx, `
UPDATE sessions SET created_at=substr(created_at,1,19)||'.000000000Z'
 WHERE length(created_at)=20 AND substr(created_at,20,1)='Z';
UPDATE sessions SET updated_at=substr(updated_at,1,19)||'.000000000Z'
 WHERE length(updated_at)=20 AND substr(updated_at,20,1)='Z';
UPDATE definitions SET created_at=substr(created_at,1,19)||'.000000000Z'
 WHERE length(created_at)=20 AND substr(created_at,20,1)='Z';
UPDATE snapshots SET created_at=substr(created_at,1,19)||'.000000000Z'
 WHERE length(created_at)=20 AND substr(created_at,20,1)='Z';
UPDATE schedules SET anchor=substr(anchor,1,19)||'.000000000Z'
 WHERE length(anchor)=20 AND substr(anchor,20,1)='Z';
UPDATE schedules SET last_fire=substr(last_fire,1,19)||'.000000000Z'
 WHERE length(last_fire)=20 AND substr(last_fire,20,1)='Z';
UPDATE schedules SET created_at=substr(created_at,1,19)||'.000000000Z'
 WHERE length(created_at)=20 AND substr(created_at,20,1)='Z';
UPDATE compactions SET created_at=substr(created_at,1,19)||'.000000000Z'
 WHERE length(created_at)=20 AND substr(created_at,20,1)='Z';
UPDATE agents SET created_at=substr(created_at,1,19)||'.000000000Z'
 WHERE length(created_at)=20 AND substr(created_at,20,1)='Z';
UPDATE agents SET updated_at=substr(updated_at,1,19)||'.000000000Z'
 WHERE length(updated_at)=20 AND substr(updated_at,20,1)='Z';
UPDATE turns SET created_at=substr(created_at,1,19)||'.000000000Z'
 WHERE length(created_at)=20 AND substr(created_at,20,1)='Z';
UPDATE turns SET updated_at=substr(updated_at,1,19)||'.000000000Z'
 WHERE length(updated_at)=20 AND substr(updated_at,20,1)='Z';
UPDATE content_objects SET created_at=substr(created_at,1,19)||'.000000000Z'
 WHERE length(created_at)=20 AND substr(created_at,20,1)='Z';
UPDATE content_references SET created_at=substr(created_at,1,19)||'.000000000Z'
 WHERE length(created_at)=20 AND substr(created_at,20,1)='Z';
UPDATE content_grants SET created_at=substr(created_at,1,19)||'.000000000Z'
 WHERE length(created_at)=20 AND substr(created_at,20,1)='Z';
UPDATE content_grants SET revoked_at=substr(revoked_at,1,19)||'.000000000Z'
 WHERE length(revoked_at)=20 AND substr(revoked_at,20,1)='Z';
UPDATE agent_messages SET available_at=substr(available_at,1,19)||'.000000000Z'
 WHERE length(available_at)=20 AND substr(available_at,20,1)='Z';
UPDATE agent_messages SET created_at=substr(created_at,1,19)||'.000000000Z'
 WHERE length(created_at)=20 AND substr(created_at,20,1)='Z';
UPDATE agent_messages SET delivered_at=substr(delivered_at,1,19)||'.000000000Z'
 WHERE length(delivered_at)=20 AND substr(delivered_at,20,1)='Z';
UPDATE agent_messages SET done_at=substr(done_at,1,19)||'.000000000Z'
 WHERE length(done_at)=20 AND substr(done_at,20,1)='Z';
UPDATE commands SET created_at=substr(created_at,1,19)||'.000000000Z'
 WHERE length(created_at)=20 AND substr(created_at,20,1)='Z';
UPDATE commands SET updated_at=substr(updated_at,1,19)||'.000000000Z'
 WHERE length(updated_at)=20 AND substr(updated_at,20,1)='Z';
UPDATE events SET created_at=substr(created_at,1,19)||'.000000000Z'
 WHERE length(created_at)=20 AND substr(created_at,20,1)='Z';
UPDATE inbox SET created_at=substr(created_at,1,19)||'.000000000Z'
 WHERE length(created_at)=20 AND substr(created_at,20,1)='Z';
UPDATE agent_state SET updated_at=substr(updated_at,1,19)||'.000000000Z'
 WHERE length(updated_at)=20 AND substr(updated_at,20,1)='Z';
UPDATE agent_scratch SET updated_at=substr(updated_at,1,19)||'.000000000Z'
 WHERE length(updated_at)=20 AND substr(updated_at,20,1)='Z';
UPDATE capabilities SET created_at=substr(created_at,1,19)||'.000000000Z'
 WHERE length(created_at)=20 AND substr(created_at,20,1)='Z';
UPDATE capabilities SET updated_at=substr(updated_at,1,19)||'.000000000Z'
 WHERE length(updated_at)=20 AND substr(updated_at,20,1)='Z';
UPDATE budgets SET updated_at=substr(updated_at,1,19)||'.000000000Z'
 WHERE length(updated_at)=20 AND substr(updated_at,20,1)='Z';
UPDATE operations SET created_at=substr(created_at,1,19)||'.000000000Z'
 WHERE length(created_at)=20 AND substr(created_at,20,1)='Z';
UPDATE operations SET updated_at=substr(updated_at,1,19)||'.000000000Z'
 WHERE length(updated_at)=20 AND substr(updated_at,20,1)='Z';
UPDATE model_calls SET created_at=substr(created_at,1,19)||'.000000000Z'
 WHERE length(created_at)=20 AND substr(created_at,20,1)='Z';
UPDATE model_calls SET updated_at=substr(updated_at,1,19)||'.000000000Z'
 WHERE length(updated_at)=20 AND substr(updated_at,20,1)='Z';
UPDATE leases SET created_at=substr(created_at,1,19)||'.000000000Z'
 WHERE length(created_at)=20 AND substr(created_at,20,1)='Z';
UPDATE leases SET updated_at=substr(updated_at,1,19)||'.000000000Z'
 WHERE length(updated_at)=20 AND substr(updated_at,20,1)='Z';
UPDATE agent_checkpoints SET updated_at=substr(updated_at,1,19)||'.000000000Z'
 WHERE length(updated_at)=20 AND substr(updated_at,20,1)='Z';
UPDATE blackboard SET updated_at=substr(updated_at,1,19)||'.000000000Z'
 WHERE length(updated_at)=20 AND substr(updated_at,20,1)='Z';
UPDATE blackboard_history SET created_at=substr(created_at,1,19)||'.000000000Z'
 WHERE length(created_at)=20 AND substr(created_at,20,1)='Z';
UPDATE subscriptions SET created_at=substr(created_at,1,19)||'.000000000Z'
 WHERE length(created_at)=20 AND substr(created_at,20,1)='Z';
UPDATE subscriptions SET updated_at=substr(updated_at,1,19)||'.000000000Z'
 WHERE length(updated_at)=20 AND substr(updated_at,20,1)='Z';
UPDATE permission_requests SET created_at=substr(created_at,1,19)||'.000000000Z'
 WHERE length(created_at)=20 AND substr(created_at,20,1)='Z';
UPDATE permission_requests SET updated_at=substr(updated_at,1,19)||'.000000000Z'
 WHERE length(updated_at)=20 AND substr(updated_at,20,1)='Z';
UPDATE permission_rules SET created_at=substr(created_at,1,19)||'.000000000Z'
 WHERE length(created_at)=20 AND substr(created_at,20,1)='Z';
UPDATE usage_charges SET created_at=substr(created_at,1,19)||'.000000000Z'
 WHERE length(created_at)=20 AND substr(created_at,20,1)='Z';
UPDATE content_orphans SET first_seen_at=substr(first_seen_at,1,19)||'.000000000Z'
 WHERE length(first_seen_at)=20 AND substr(first_seen_at,20,1)='Z';
UPDATE content_orphans SET last_seen_at=substr(last_seen_at,1,19)||'.000000000Z'
 WHERE length(last_seen_at)=20 AND substr(last_seen_at,20,1)='Z';
UPDATE daemon_state SET updated_at=substr(updated_at,1,19)||'.000000000Z'
 WHERE length(updated_at)=20 AND substr(updated_at,20,1)='Z';
`); err != nil {
		return fmt.Errorf("normalize legacy timestamps: %w", err)
	}
	return nil
}

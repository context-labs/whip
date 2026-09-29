package store

import (
	"context"
	"database/sql"

	"github.com/context-labs/whip/internal/session"
)

// TreeCatalog is the invalidation head for all tree metadata, including trees
// outside a client's loaded pages. It is not a transcript or worker revision.
func (s *Store) TreeCatalog(ctx context.Context) (revision session.Revision, err error) {
	err = s.db.QueryRowContext(ctx, "SELECT revision FROM tree_catalog WHERE singleton=1").Scan(&revision)
	return
}

func bumpTreeCatalog(ctx context.Context, tx *sql.Tx) error {
	result, err := tx.ExecContext(ctx, "UPDATE tree_catalog SET revision=revision+1 WHERE singleton=1 AND revision<9223372036854775807")
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return ErrConflict
	}
	return nil
}

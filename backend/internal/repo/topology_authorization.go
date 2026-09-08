package repo

import (
	"context"
	"database/sql"
)

func InvalidateTopologyAuthorization(ctx context.Context, tx *sql.Tx) error {
	if _, err := lockTopologyMaterial(ctx, tx); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE topology_releases SET status='manual_required',error_code='authorization_changed',finished_at=now() WHERE status IN('preparing','ready','applying','rolling_back')`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE nodes SET config_generation=config_generation+1 WHERE type='managed'`); err != nil {
		return err
	}
	return bumpTopologyMaterial(ctx, tx)
}

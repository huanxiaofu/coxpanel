package repo

import (
	"context"
)

func (r *TopologyRepo) PruneReleaseHistory(ctx context.Context) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `LOCK TABLE topology_releases IN SHARE ROW EXCLUSIVE MODE`); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `WITH ranked AS (
		SELECT n.release_id,n.node_id,row_number() OVER(PARTITION BY n.node_id ORDER BY r.created_at DESC,r.id DESC) AS position
		FROM topology_release_nodes n JOIN topology_releases r ON r.id=n.release_id
	) SELECT r.id FROM topology_releases r
	WHERE r.status IN ('succeeded','failed','rolled_back')
	AND r.created_at<now()-interval '30 days' AND r.finished_at<now()-interval '30 days'
	AND NOT EXISTS(SELECT 1 FROM topology_deployments d WHERE d.release_id=r.id)
	AND NOT EXISTS(SELECT 1 FROM ranked n WHERE n.release_id=r.id AND n.position<=10)
	AND NOT EXISTS(SELECT 1 FROM topology_release_nodes candidate
		JOIN topology_release_nodes dependency ON dependency.node_id=candidate.node_id
		JOIN topology_releases active ON active.id=dependency.release_id
		WHERE candidate.release_id=r.id AND active.status IN('preparing','ready','applying','rolling_back','manual_required'))`)
	if err != nil {
		return err
	}
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if len(ids) > 0 {
		if _, err = tx.ExecContext(ctx, `DELETE FROM topology_release_nodes WHERE release_id=ANY($1::bigint[])`, ids); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM topology_releases WHERE id=ANY($1::bigint[])`, ids); err != nil {
			return err
		}
	}
	return tx.Commit()
}

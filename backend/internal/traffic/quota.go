package traffic

import (
	"context"
	"time"

	"github.com/coxpanel/backend/internal/repo"
)

func (service *Service) SetQuota(ctx context.Context, userID, actorID, expectedRevision, limit int64, expireAt *time.Time) error {
	if expectedRevision < 1 || limit < 0 {
		return ErrInvalid
	}
	tx, err := service.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `SELECT revision FROM topology_material_state WHERE id=TRUE FOR UPDATE`); err != nil {
		return err
	}
	var oldLimit, revision int64
	if err = tx.QueryRowContext(ctx, `SELECT traffic_limit_bytes,quota_revision FROM users WHERE id=$1 FOR UPDATE`, userID).Scan(&oldLimit, &revision); err != nil {
		return err
	}
	if revision != expectedRevision {
		return ErrConflict
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO traffic_accounts(user_id,period_start) VALUES($1,now()) ON CONFLICT DO NOTHING`, userID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO traffic_account_events(user_id,quota_epoch,kind,old_limit_bytes,new_limit_bytes,snapshot_up_bytes,snapshot_down_bytes,actor_id) SELECT user_id,quota_epoch,'limit_changed',$2,$3,used_up_bytes,used_down_bytes,$4 FROM traffic_accounts WHERE user_id=$1`, userID, oldLimit, limit, actorID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE users SET traffic_limit_bytes=$2,expire_at=$3,quota_revision=quota_revision+1 WHERE id=$1`, userID, limit, expireAt); err != nil {
		return err
	}
	if err = repo.InvalidateTopologyAuthorization(ctx, tx); err != nil {
		return err
	}
	return tx.Commit()
}

func (service *Service) Reset(ctx context.Context, userID, actorID, expectedEpoch int64, reason string) error {
	if expectedEpoch < 1 || len(reason) < 1 || len(reason) > 500 {
		return ErrInvalid
	}
	tx, err := service.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `INSERT INTO traffic_accounts(user_id,period_start) VALUES($1,now()) ON CONFLICT DO NOTHING`, userID); err != nil {
		return err
	}
	var epoch int64
	if err = tx.QueryRowContext(ctx, `SELECT quota_epoch FROM traffic_accounts WHERE user_id=$1 FOR UPDATE`, userID).Scan(&epoch); err != nil {
		return err
	}
	if epoch != expectedEpoch {
		return ErrConflict
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO traffic_account_events(user_id,quota_epoch,kind,old_limit_bytes,new_limit_bytes,snapshot_up_bytes,snapshot_down_bytes,actor_id) SELECT a.user_id,a.quota_epoch,'reset',u.traffic_limit_bytes,u.traffic_limit_bytes,a.used_up_bytes,a.used_down_bytes,$2 FROM traffic_accounts a JOIN users u ON u.id=a.user_id WHERE a.user_id=$1`, userID, actorID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE traffic_accounts SET quota_epoch=quota_epoch+1,period_start=now(),used_up_bytes=0,used_down_bytes=0,updated_at=now() WHERE user_id=$1`, userID); err != nil {
		return err
	}
	return tx.Commit()
}

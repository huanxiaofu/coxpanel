package mail

import "context"

func (service *Service) EnsureDefaultRules(ctx context.Context) error {
	tx, err := service.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(7144,2)`); err != nil {
		return err
	}
	for _, rule := range []struct{ Name, Kind, Thresholds string }{{"用量默认提醒", "traffic_limit", "[80,90,100]"}, {"到期默认提醒", "expiration", "[7,3,1,0]"}} {
		if _, err = tx.ExecContext(ctx, `INSERT INTO alert_rules(name,kind,scope_type,thresholds,created_by) SELECT $1,$2,'all',$3::jsonb,id FROM users WHERE role='owner' AND NOT EXISTS(SELECT 1 FROM alert_rules WHERE kind=$2 AND scope_type='all') ORDER BY id LIMIT 1`, rule.Name, rule.Kind, rule.Thresholds); err != nil {
			return err
		}
	}
	return tx.Commit()
}

package repo

import (
	"context"
	"encoding/json"
)

func (r *SubscriptionRepo) GroupDefaults(ctx context.Context, groupID int64) (json.RawMessage, error) {
	var raw []byte
	err := r.db.QueryRowContext(ctx, `SELECT subscription_defaults FROM node_groups WHERE id=$1`, groupID).Scan(&raw)
	if isP2CompatibilityError(err) {
		return nil, nil
	}
	return json.RawMessage(raw), err
}

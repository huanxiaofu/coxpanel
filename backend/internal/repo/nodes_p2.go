package repo

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/coxpanel/backend/internal/models"
)

func (r *NodeRepo) UpdateAgentCapabilities(ctx context.Context, nodeID int64, capabilities []string, generation int64) error {
	if len(capabilities) > 32 || generation < 0 {
		return errors.New("invalid_capabilities")
	}
	for _, capability := range capabilities {
		if len(capability) > 64 {
			return errors.New("invalid_capabilities")
		}
	}
	if capabilities == nil {
		capabilities = []string{}
	}
	raw, _ := json.Marshal(capabilities)
	_, err := r.db.ExecContext(ctx, `UPDATE nodes SET agent_capabilities=$2 WHERE id=$1`, nodeID, raw)
	return err
}

func (r *NodeRepo) InboundRevisions(ctx context.Context, nodeID int64) (map[int64]models.Inbound, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id,egress_mode,revision FROM inbounds WHERE node_id=$1`, nodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[int64]models.Inbound{}
	for rows.Next() {
		var inbound models.Inbound
		if err = rows.Scan(&inbound.ID, &inbound.EgressMode, &inbound.Revision); err != nil {
			return nil, err
		}
		result[inbound.ID] = inbound
	}
	return result, rows.Err()
}

func (r *NodeRepo) UpdateInbound(ctx context.Context, inbound models.Inbound, expected int64) error {
	if expected < 1 || inbound.NodeID <= 0 || inbound.ID <= 0 {
		return ErrTopologyReleaseConflict
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = lockTopologyMaterial(ctx, tx); err != nil {
		return err
	}
	var revision int64
	if err = tx.QueryRowContext(ctx, `SELECT revision FROM inbounds WHERE id=$1 AND node_id=$2 FOR UPDATE`, inbound.ID, inbound.NodeID).Scan(&revision); err != nil {
		return err
	}
	if revision != expected {
		return ErrTopologyPreviewStale
	}
	if err = rejectActiveNodeEdit(ctx, tx, inbound.NodeID); err != nil {
		return err
	}
	var referenced bool
	err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM topology_deployments WHERE node_id=$1 OR topology @> jsonb_build_object('edges',jsonb_build_array(jsonb_build_object('toInboundId',$2::bigint))))`, inbound.NodeID, inbound.ID).Scan(&referenced)
	if err != nil {
		return err
	}
	if referenced {
		return ErrTopologyReleaseConflict
	}
	result, err := tx.ExecContext(ctx, `UPDATE inbounds SET name=$3,protocol=$4,role=$5,listen_addr=$6,listen_port=$7,config=$8,egress_mode=$9,revision=revision+1,updated_at=now() WHERE id=$1 AND node_id=$2`, inbound.ID, inbound.NodeID, inbound.Name, inbound.Protocol, inbound.Role, inbound.ListenAddr, inbound.ListenPort, inbound.Config, inbound.EgressMode)
	if err != nil {
		return err
	}
	count, _ := result.RowsAffected()
	if count != 1 {
		return sql.ErrNoRows
	}
	if _, err = tx.ExecContext(ctx, `UPDATE topology_material_state SET revision=revision+1,graph_revision=graph_revision+1 WHERE id=TRUE`); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE nodes SET config_generation=config_generation+1 WHERE id=$1`, inbound.NodeID); err != nil {
		return err
	}
	return tx.Commit()
}

func rejectActiveNodeEdit(ctx context.Context, tx *sql.Tx, nodeID int64) error {
	var exists bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM topology_release_nodes n JOIN topology_releases r ON r.id=n.release_id WHERE n.node_id=$1 AND r.status IN('preparing','ready','applying','rolling_back','manual_required'))`, nodeID).Scan(&exists)
	if err != nil {
		return err
	}
	if exists {
		return ErrTopologyReleaseConflict
	}
	return nil
}

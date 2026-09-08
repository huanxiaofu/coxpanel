package repo

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"github.com/coxpanel/backend/internal/generator"
)

type TemplateReferenceError struct{ Count int }

func (err *TemplateReferenceError) Error() string { return "template references are incompatible" }
func (err *TemplateReferenceError) Unwrap() error { return ErrTemplateInUse }

func lockSubscriptionTemplates(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(7212,1)`)
	return err
}

func templateDefinitionTx(ctx context.Context, tx *sql.Tx, templateID *int64, version *int) (json.RawMessage, error) {
	if templateID == nil {
		return nil, nil
	}
	var raw []byte
	err := tx.QueryRowContext(ctx, `SELECT v.definition FROM templates t JOIN template_versions v ON v.template_id=t.id AND v.version=COALESCE($2,t.published_version) WHERE t.id=$1`, *templateID, version).Scan(&raw)
	return json.RawMessage(raw), err
}

func checkGroupReference(definition json.RawMessage, group *string) error {
	if group == nil || *group == "" {
		return nil
	}
	if generator.ValidateClientGroup(definition, *group) != nil {
		return ErrTemplateInUse
	}
	return nil
}

func validateBoundOverridesTx(ctx context.Context, tx *sql.Tx, subID int64, format string, definition json.RawMessage) error {
	rows, err := tx.QueryContext(ctx, `SELECT params,proxy_group FROM subscription_node_overrides WHERE subscription_id=$1 UNION ALL SELECT params,proxy_group FROM subscription_inbound_overrides WHERE subscription_id=$1 UNION ALL SELECT g.subscription_defaults->'params',g.subscription_defaults->>'proxyGroup' FROM subscriptions s JOIN node_groups g ON g.id=s.node_group_id WHERE s.id=$1`, subID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var raw []byte
		var group *string
		if err := rows.Scan(&raw, &group); err != nil {
			return err
		}
		if err := checkGroupReference(definition, group); err != nil {
			return err
		}
		var params map[string]any
		_ = json.Unmarshal(raw, &params)
		if err := generator.ValidateOverrideParams(format, "", params); err != nil {
			return ErrTemplateInUse
		}
	}
	return rows.Err()
}

func validateOverrideTargetTx(ctx context.Context, tx *sql.Tx, subID, nodeID, inboundID int64, patch OverridePatch) error {
	var format string
	var templateID *int64
	var version *int
	if err := tx.QueryRowContext(ctx, `SELECT format,template_id,template_version FROM subscriptions WHERE id=$1`, subID).Scan(&format, &templateID, &version); err != nil {
		return err
	}
	definition, err := templateDefinitionTx(ctx, tx, templateID, version)
	if err != nil {
		return err
	}
	if err := checkGroupReference(definition, patch.ProxyGroup); err != nil {
		return err
	}
	var params map[string]any
	_ = json.Unmarshal(patch.Params, &params)
	var nodeType string
	if err := tx.QueryRowContext(ctx, `SELECT type FROM nodes WHERE id=$1`, nodeID).Scan(&nodeType); err != nil {
		return err
	}
	if nodeType == "external" {
		return generator.ValidateOverrideParams(format, "external", params)
	}
	rows, err := tx.QueryContext(ctx, `SELECT protocol,config FROM inbounds WHERE node_id=$1 AND role='entry' AND ($2::bigint=0 OR id=$2)`, nodeID, inboundID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var protocol string
		var raw []byte
		if err := rows.Scan(&protocol, &raw); err != nil {
			return err
		}
		if err := generator.ValidateOverrideParams(format, protocol, params); err != nil {
			return err
		}
		if flow, ok := params["flow"].(string); ok && flow != "" {
			var config map[string]any
			_ = json.Unmarshal(raw, &config)
			if value, exists := config["flow"]; exists && value != flow {
				return generator.ErrOverrideFieldForbidden
			}
		}
	}
	return rows.Err()
}

func (r *TemplateRepo) SaveGroupDefaults(ctx context.Context, id, revision int64, raw json.RawMessage) (int64, error) {
	var defaults generator.ClientOverride
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&defaults) != nil {
		return 0, ErrInvalidOverride
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return 0, ErrInvalidOverride
	}
	if err := generator.ValidateOverride("mihomo", "", defaults.DisplayName, defaults.SortOrder, defaults.Icon, defaults.ProxyGroup, defaults.Params); err != nil {
		return 0, err
	}
	if defaults.Params["insecure"] == true {
		return 0, generator.ErrOverrideFieldForbidden
	}
	if _, exists := defaults.Params["obfsPassword"]; exists {
		return 0, generator.ErrOverrideFieldForbidden
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if err := lockSubscriptionTemplates(ctx, tx); err != nil {
		return 0, err
	}
	var current int64
	if err := tx.QueryRowContext(ctx, `SELECT revision FROM node_groups WHERE id=$1 FOR UPDATE`, id).Scan(&current); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, ErrNodeGroupNotFound
		}
		return 0, err
	}
	if revision != current {
		return 0, ErrRevisionConflict
	}
	if defaults.ProxyGroup != nil && *defaults.ProxyGroup != "" {
		rows, err := tx.QueryContext(ctx, `SELECT v.definition FROM subscriptions s LEFT JOIN templates t ON t.id=s.template_id LEFT JOIN template_versions v ON v.template_id=t.id AND v.version=COALESCE(s.template_version,t.published_version) WHERE s.node_group_id=$1`, id)
		if err != nil {
			return 0, err
		}
		for rows.Next() {
			var definition []byte
			if err := rows.Scan(&definition); err != nil {
				rows.Close()
				return 0, err
			}
			if err := checkGroupReference(definition, defaults.ProxyGroup); err != nil {
				rows.Close()
				return 0, err
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return 0, err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE node_groups SET subscription_defaults=$2,revision=revision+1 WHERE id=$1`, id, raw); err != nil {
		return 0, err
	}
	return current + 1, tx.Commit()
}

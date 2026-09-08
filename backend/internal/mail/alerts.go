package mail

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math/big"
	"sort"
	"strconv"
	"time"
)

type Rule struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	Enabled    bool   `json:"enabled"`
	ScopeType  string `json:"scopeType"`
	ScopeID    *int64 `json:"scopeId"`
	Thresholds []int  `json:"thresholds"`
	Channel    string `json:"channel"`
	Revision   int64  `json:"revision"`
}

func (rule Rule) Validate() error {
	if rule.Channel != "email" {
		return ErrChannelUnsupported
	}
	if rule.Name == "" || len(rule.Name) > 128 || (rule.Kind != "traffic_limit" && rule.Kind != "expiration") || (rule.ScopeType != "all" && rule.ScopeType != "group" && rule.ScopeType != "user") || ((rule.ScopeType == "all") != (rule.ScopeID == nil)) || (rule.ScopeID != nil && *rule.ScopeID <= 0) {
		return ErrInvalid
	}
	maximum, minimum, limit := 100, 1, 5
	if rule.Kind == "expiration" {
		maximum, minimum, limit = 90, 0, 8
	}
	if len(rule.Thresholds) == 0 || len(rule.Thresholds) > limit {
		return ErrInvalid
	}
	seen := map[int]bool{}
	for _, threshold := range rule.Thresholds {
		if threshold < minimum || threshold > maximum || seen[threshold] {
			return ErrInvalid
		}
		seen[threshold] = true
	}
	return nil
}
func Reached(up, down, limit int64, threshold int) bool {
	if up < 0 || down < 0 || limit <= 0 || threshold < 1 {
		return false
	}
	used := new(big.Int).Add(big.NewInt(up), big.NewInt(down))
	used.Mul(used, big.NewInt(100))
	return used.Cmp(new(big.Int).Mul(big.NewInt(limit), big.NewInt(int64(threshold)))) >= 0
}
func EventKey(kind string, userID, epoch, limit int64, expires *time.Time, threshold int) string {
	if kind == "traffic_limit" {
		return fmt.Sprintf("traffic:%d:%d:%d:%d:email", userID, epoch, limit, threshold)
	}
	return fmt.Sprintf("expiration:%d:%s:%d:email", userID, expires.UTC().Format(time.RFC3339Nano), threshold)
}

func (service *Service) ListRules(ctx context.Context) ([]Rule, error) {
	if err := service.EnsureDefaultRules(ctx); err != nil {
		return nil, err
	}
	rows, err := service.DB.QueryContext(ctx, `SELECT id,name,kind,enabled,scope_type,scope_id,thresholds,channel,revision FROM alert_rules ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Rule{}
	for rows.Next() {
		var rule Rule
		var thresholds []byte
		if err = rows.Scan(&rule.ID, &rule.Name, &rule.Kind, &rule.Enabled, &rule.ScopeType, &rule.ScopeID, &thresholds, &rule.Channel, &rule.Revision); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(thresholds, &rule.Thresholds); err != nil {
			return nil, err
		}
		result = append(result, rule)
	}
	return result, rows.Err()
}
func (service *Service) SaveRule(ctx context.Context, actorID int64, rule Rule, expected int64) (int64, error) {
	if err := rule.Validate(); err != nil {
		return 0, err
	}
	tx, err := service.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if rule.ScopeID != nil {
		table := "users"
		if rule.ScopeType == "group" {
			table = "node_groups"
		}
		var exists bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM `+table+` WHERE id=$1)`, *rule.ScopeID).Scan(&exists); err != nil {
			return 0, err
		}
		if !exists {
			return 0, ErrInvalid
		}
	}
	thresholds := append([]int{}, rule.Thresholds...)
	sort.Ints(thresholds)
	if rule.Kind == "expiration" {
		sort.Sort(sort.Reverse(sort.IntSlice(thresholds)))
	}
	raw, _ := json.Marshal(thresholds)
	if rule.ID == 0 {
		err = tx.QueryRowContext(ctx, `INSERT INTO alert_rules(name,kind,enabled,scope_type,scope_id,thresholds,channel,created_by) VALUES($1,$2,$3,$4,$5,$6,'email',$7) RETURNING id`, rule.Name, rule.Kind, rule.Enabled, rule.ScopeType, rule.ScopeID, raw, actorID).Scan(&rule.ID)
	} else {
		var result sql.Result
		result, err = tx.ExecContext(ctx, `UPDATE alert_rules SET name=$2,kind=$3,enabled=$4,scope_type=$5,scope_id=$6,thresholds=$7,revision=revision+1,updated_at=now() WHERE id=$1 AND revision=$8`, rule.ID, rule.Name, rule.Kind, rule.Enabled, rule.ScopeType, rule.ScopeID, raw, expected)
		if err == nil {
			count, _ := result.RowsAffected()
			if count != 1 {
				err = ErrConflict
			}
		}
	}
	if err != nil {
		return 0, err
	}
	return rule.ID, tx.Commit()
}

type Preference struct {
	TrafficEnabled    bool  `json:"trafficEnabled"`
	ExpirationEnabled bool  `json:"expirationEnabled"`
	Revision          int64 `json:"revision"`
}

func (service *Service) Preferences(ctx context.Context, userID int64) (Preference, error) {
	result := Preference{true, true, 1}
	err := service.DB.QueryRowContext(ctx, `SELECT traffic_enabled,expiration_enabled,revision FROM notification_preferences WHERE user_id=$1`, userID).Scan(&result.TrafficEnabled, &result.ExpirationEnabled, &result.Revision)
	if err == sql.ErrNoRows {
		err = nil
	}
	return result, err
}
func (service *Service) SavePreferences(ctx context.Context, userID, expected int64, preferences Preference) error {
	tx, err := service.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `INSERT INTO notification_preferences(user_id) VALUES($1) ON CONFLICT DO NOTHING`, userID); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE notification_preferences SET traffic_enabled=$2,expiration_enabled=$3,revision=revision+1,updated_at=now() WHERE user_id=$1 AND revision=$4`, userID, preferences.TrafficEnabled, preferences.ExpirationEnabled, expected)
	if err != nil {
		return err
	}
	count, _ := result.RowsAffected()
	if count != 1 {
		return ErrConflict
	}
	return tx.Commit()
}

func (service *Service) ScanAlerts(ctx context.Context) error {
	if err := service.EnsureDefaultRules(ctx); err != nil {
		return err
	}
	connection, err := service.DB.Conn(ctx)
	if err != nil {
		return err
	}
	defer connection.Close()
	var locked bool
	if err = connection.QueryRowContext(ctx, `SELECT pg_try_advisory_lock(7144,1)`).Scan(&locked); err != nil {
		return err
	}
	if !locked {
		return nil
	}
	defer connection.ExecContext(context.WithoutCancel(ctx), `SELECT pg_advisory_unlock(7144,1)`)
	for cursor := int64(0); ; {
		rows, queryErr := connection.QueryContext(ctx, `SELECT id FROM users WHERE is_active AND id>$1 ORDER BY id LIMIT 100`, cursor)
		if queryErr != nil {
			return queryErr
		}
		ids := []int64{}
		for rows.Next() {
			var id int64
			if queryErr = rows.Scan(&id); queryErr != nil {
				rows.Close()
				return queryErr
			}
			ids = append(ids, id)
		}
		queryErr = rows.Err()
		rows.Close()
		if queryErr != nil {
			return queryErr
		}
		if len(ids) == 0 {
			return nil
		}
		for _, id := range ids {
			if err = service.evaluateUser(ctx, id); err != nil {
				return err
			}
			cursor = id
		}
	}
}

func (service *Service) evaluateUser(ctx context.Context, userID int64) error {
	tx, err := service.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var email string
	var verified, expires *time.Time
	var emailRevision, limit, epoch, up, down int64
	var trafficEnabled, expirationEnabled bool
	err = tx.QueryRowContext(ctx, `SELECT COALESCE(u.email,''),u.email_verified_at,u.email_revision,u.traffic_limit_bytes,u.expire_at,COALESCE(a.quota_epoch,1),COALESCE(a.used_up_bytes,0),COALESCE(a.used_down_bytes,0),COALESCE(p.traffic_enabled,true),COALESCE(p.expiration_enabled,true) FROM users u LEFT JOIN traffic_accounts a ON a.user_id=u.id LEFT JOIN notification_preferences p ON p.user_id=u.id WHERE u.id=$1 AND u.is_active FOR UPDATE OF u`, userID).Scan(&email, &verified, &emailRevision, &limit, &expires, &epoch, &up, &down, &trafficEnabled, &expirationEnabled)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	for _, kind := range []string{"traffic_limit", "expiration"} {
		if (kind == "traffic_limit" && limit == 0) || (kind == "expiration" && expires == nil) {
			continue
		}
		var ruleID int64
		var raw []byte
		err = tx.QueryRowContext(ctx, `SELECT id,thresholds FROM alert_rules WHERE enabled AND kind=$1 AND (scope_type='all' OR (scope_type='user' AND scope_id=$2) OR (scope_type='group' AND scope_id IN (SELECT group_id FROM user_node_groups WHERE user_id=$2))) ORDER BY CASE scope_type WHEN 'user' THEN 0 WHEN 'group' THEN 1 ELSE 2 END,id LIMIT 1`, kind, userID).Scan(&ruleID, &raw)
		if err == sql.ErrNoRows {
			continue
		}
		if err != nil {
			return err
		}
		var thresholds []int
		if err = json.Unmarshal(raw, &thresholds); err != nil {
			return err
		}
		sort.Ints(thresholds)
		if kind == "traffic_limit" {
			sort.Sort(sort.Reverse(sort.IntSlice(thresholds)))
		}
		urgentFound := false
		for _, threshold := range thresholds {
			crossed := false
			if kind == "traffic_limit" {
				crossed = Reached(up, down, limit, threshold)
			} else {
				crossed = !time.Now().Before(expires.Add(-time.Duration(threshold) * 24 * time.Hour))
			}
			if !crossed {
				continue
			}
			state := "queued"
			if urgentFound {
				state = "suppressed"
			}
			urgentFound = true
			if (kind == "traffic_limit" && !trafficEnabled) || (kind == "expiration" && !expirationEnabled) {
				state = "suppressed"
			}
			if state == "queued" && (verified == nil || !ValidEmail(email) || !service.Configured() || service.aead == nil) {
				state = "blocked_recipient"
			}
			key := EventKey(kind, userID, epoch, limit, expires, threshold)
			snapshot := map[string]any{"threshold": threshold, "quotaEpoch": epoch, "limitBytes": strconv.FormatInt(limit, 10), "usedBytes": new(big.Int).Add(big.NewInt(up), big.NewInt(down)).String(), "expireAt": expires, "asOf": time.Now().UTC(), "quality": "known_usage_may_be_incomplete"}
			payload := Payload{Subject: "CoxPanel 用量提醒", Body: fmt.Sprintf("已知用量达到配额 %d%%。采集可能不完整，请登录面板查看截至时间和详细用量。", threshold)}
			if kind == "expiration" {
				payload = Payload{Subject: "CoxPanel 到期提醒", Body: "账号到期时间（UTC）：" + expires.UTC().Format(time.RFC3339) + "。请登录面板查看详情。"}
			}
			if state == "queued" {
				var id int64
				var oldState string
				queryErr := tx.QueryRowContext(ctx, `SELECT id,state FROM notification_records WHERE dedupe_key=$1 FOR UPDATE`, key).Scan(&id, &oldState)
				if queryErr != nil && queryErr != sql.ErrNoRows {
					return queryErr
				}
				if oldState == "blocked_recipient" {
					recipientCipher, sealErr := service.seal([]byte(email), fmt.Sprintf("notification_records:%d:recipient", id))
					if sealErr != nil {
						return sealErr
					}
					body, _ := json.Marshal(payload)
					payloadCipher, sealErr := service.seal(body, fmt.Sprintf("notification_records:%d:payload", id))
					if sealErr != nil {
						return sealErr
					}
					if _, err = tx.ExecContext(ctx, `UPDATE notification_records SET state='queued',recipient_ciphertext=$2,payload_ciphertext=$3,email_revision=$4,next_attempt_at=now() WHERE id=$1`, id, recipientCipher, payloadCipher, emailRevision); err != nil {
						return err
					}
					continue
				}
			}
			if _, err = service.Enqueue(ctx, tx, Event{UserID: &userID, RuleID: &ruleID, Kind: kind, Key: key, Recipient: email, State: state, EmailRevision: &emailRevision, Snapshot: snapshot, Payload: payload}); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

func (service *Service) Eligible(ctx context.Context, tx *sql.Tx, id int64) (bool, error) {
	var kind string
	var userID, ruleID, revision sql.NullInt64
	var raw []byte
	if err := tx.QueryRowContext(ctx, `SELECT kind,user_id,rule_id,email_revision,event_snapshot FROM notification_records WHERE id=$1`, id).Scan(&kind, &userID, &ruleID, &revision, &raw); err != nil {
		return false, err
	}
	var snapshot map[string]json.RawMessage
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		return false, err
	}
	if kind == "invitation" {
		var inviteID int64
		_ = json.Unmarshal(snapshot["inviteId"], &inviteID)
		var valid bool
		err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM invite_codes WHERE id=$1 AND used_count<max_uses AND (expires_at IS NULL OR expires_at>now()))`, inviteID).Scan(&valid)
		return valid, err
	}
	if kind == "email_verification" {
		var challengeID string
		_ = json.Unmarshal(snapshot["challengeId"], &challengeID)
		var valid bool
		err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM email_verifications e LEFT JOIN users u ON u.id=e.user_id WHERE e.id=$1 AND e.state='pending' AND e.expires_at>now() AND (e.user_id IS NULL OR (u.is_active AND u.email_revision=e.email_revision)))`, challengeID).Scan(&valid)
		return valid, err
	}
	if !userID.Valid {
		return false, nil
	}
	var active, verified bool
	var emailRevision, limit, epoch, up, down int64
	var expires *time.Time
	var trafficEnabled, expirationEnabled bool
	err := tx.QueryRowContext(ctx, `SELECT u.is_active,u.email_verified_at IS NOT NULL,u.email_revision,u.traffic_limit_bytes,u.expire_at,COALESCE(a.quota_epoch,1),COALESCE(a.used_up_bytes,0),COALESCE(a.used_down_bytes,0),COALESCE(p.traffic_enabled,true),COALESCE(p.expiration_enabled,true) FROM users u LEFT JOIN traffic_accounts a ON a.user_id=u.id LEFT JOIN notification_preferences p ON p.user_id=u.id WHERE u.id=$1`, userID.Int64).Scan(&active, &verified, &emailRevision, &limit, &expires, &epoch, &up, &down, &trafficEnabled, &expirationEnabled)
	if err != nil {
		return false, err
	}
	if !active || emailRevision != revision.Int64 {
		return false, nil
	}
	if kind == "smtp_test" {
		return true, nil
	}
	if !verified {
		return false, nil
	}
	var enabled bool
	if err = tx.QueryRowContext(ctx, `SELECT enabled AND kind=$2 AND (scope_type='all' OR (scope_type='user' AND scope_id=$3) OR (scope_type='group' AND scope_id IN(SELECT group_id FROM user_node_groups WHERE user_id=$3))) AND thresholds @> jsonb_build_array(($4::jsonb->>'threshold')::int) FROM alert_rules WHERE id=$1`, ruleID.Int64, kind, userID.Int64, raw).Scan(&enabled); err != nil {
		return false, err
	}
	if !enabled {
		return false, nil
	}
	var threshold int
	var oldEpoch int64
	var oldLimit string
	var oldExpires *time.Time
	_ = json.Unmarshal(snapshot["threshold"], &threshold)
	_ = json.Unmarshal(snapshot["quotaEpoch"], &oldEpoch)
	_ = json.Unmarshal(snapshot["limitBytes"], &oldLimit)
	_ = json.Unmarshal(snapshot["expireAt"], &oldExpires)
	if kind == "traffic_limit" {
		var currentUrgent int
		if err = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(value::int),0) FROM alert_rules, jsonb_array_elements_text(thresholds) value WHERE id=$1 AND ($2::numeric+$3::numeric)*100 >= $4::numeric*value::int`, ruleID.Int64, up, down, limit).Scan(&currentUrgent); err != nil {
			return false, err
		}
		if currentUrgent != threshold {
			return false, nil
		}
		return trafficEnabled && epoch == oldEpoch && strconv.FormatInt(limit, 10) == oldLimit && Reached(up, down, limit, threshold), nil
	}
	if expires != nil {
		var currentUrgent int
		if err = tx.QueryRowContext(ctx, `SELECT COALESCE(MIN(value::int),-1) FROM alert_rules,jsonb_array_elements_text(thresholds) value WHERE id=$1 AND now()>=$2::timestamptz-value::int*interval '24 hours'`, ruleID.Int64, expires).Scan(&currentUrgent); err != nil {
			return false, err
		}
		if currentUrgent != threshold {
			return false, nil
		}
	}
	return expirationEnabled && expires != nil && oldExpires != nil && expires.Equal(*oldExpires) && !time.Now().Before(expires.Add(-time.Duration(threshold)*24*time.Hour)), nil
}

func (service *Service) RunAlerts(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			return
		}
		_ = service.ScanAlerts(ctx)
		_ = service.Cleanup(ctx)
		next := time.Now().UTC().Truncate(5 * time.Minute).Add(5 * time.Minute)
		timer := time.NewTimer(time.Until(next))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}
func (service *Service) Cleanup(ctx context.Context) error {
	for _, statement := range []string{`DELETE FROM email_verifications WHERE expires_at<now()-interval '7 days' AND (ticket_expires_at IS NULL OR ticket_expires_at<now()-interval '7 days')`, `UPDATE notification_records SET recipient_ciphertext=''::bytea,payload_ciphertext=''::bytea WHERE (state='sent' AND sent_at<now()-interval '24 hours') OR (state IN ('dead','suppressed','blocked_recipient') AND next_attempt_at<now()-interval '24 hours')`, `DELETE FROM notification_attempts WHERE created_at<now()-interval '90 days'`} {
		if _, err := service.DB.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	_, err := service.DB.ExecContext(ctx, `UPDATE notification_records SET event_snapshot='{"tombstone":true}'::jsonb,recipient_ciphertext=''::bytea,payload_ciphertext=''::bytea WHERE created_at<now()-interval '90 days' AND state IN('sent','dead','suppressed','blocked_recipient') AND event_snapshot<>'{"tombstone":true}'::jsonb`)
	if err != nil {
		return err
	}
	return nil
}

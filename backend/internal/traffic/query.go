package traffic

import (
	"context"
	"database/sql"
	"fmt"
	"math/big"
	"net/url"
	"strconv"
	"time"
)

type Query struct {
	From, To          time.Time
	Grain, GroupBy    string
	NodeID, InboundID int64
}
type Point struct {
	At        time.Time `json:"at"`
	UpBytes   string    `json:"upBytes"`
	DownBytes string    `json:"downBytes"`
	Complete  bool      `json:"complete"`
}
type Series struct {
	Key    string  `json:"key"`
	Points []Point `json:"points"`
}
type Summary struct {
	UpBytes    string `json:"upBytes"`
	DownBytes  string `json:"downBytes"`
	TotalBytes string `json:"totalBytes"`
}
type Quality struct {
	Status         string     `json:"status"`
	AvailableFrom  *time.Time `json:"availableFrom"`
	LastReceivedAt *time.Time `json:"lastReceivedAt"`
	Estimated      bool       `json:"estimated"`
}
type Quota struct {
	Epoch       int64      `json:"epoch"`
	LimitBytes  string     `json:"limitBytes"`
	UsedBytes   string     `json:"usedBytes"`
	Unlimited   bool       `json:"unlimited"`
	Revision    int64      `json:"revision"`
	PeriodStart *time.Time `json:"periodStart"`
	ExpireAt    *time.Time `json:"expireAt"`
}
type Result struct {
	From     time.Time `json:"from"`
	To       time.Time `json:"to"`
	Grain    string    `json:"grain"`
	Timezone string    `json:"timezone"`
	Summary  Summary   `json:"summary"`
	Series   []Series  `json:"series"`
	Quality  Quality   `json:"quality"`
	Quota    *Quota    `json:"quota,omitempty"`
}

func ParseQuery(values url.Values, now time.Time, node bool) (Query, error) {
	query := Query{From: now.UTC().Truncate(time.Hour).Add(-24 * time.Hour), To: now.UTC(), Grain: values.Get("grain"), GroupBy: values.Get("groupBy")}
	var err error
	if value := values.Get("from"); value != "" {
		query.From, err = time.Parse(time.RFC3339, value)
		if err != nil {
			return query, ErrInvalid
		}
	}
	if value := values.Get("to"); value != "" {
		query.To, err = time.Parse(time.RFC3339, value)
		if err != nil {
			return query, ErrInvalid
		}
	}
	query.From = query.From.UTC()
	query.To = query.To.UTC()
	span := query.To.Sub(query.From)
	if span <= 0 || query.To.After(now.Add(time.Minute)) || query.From.Before(now.Add(-730*24*time.Hour)) {
		return query, ErrInvalid
	}
	if query.Grain == "" || query.Grain == "auto" {
		query.Grain = "5m"
		if span > 2*24*time.Hour {
			query.Grain = "1h"
		}
		if span > 30*24*time.Hour {
			query.Grain = "1d"
		}
	}
	retention := map[string]time.Duration{"5m": 30 * 24 * time.Hour, "1h": 180 * 24 * time.Hour, "1d": 730 * 24 * time.Hour}
	duration, ok := retention[query.Grain]
	if !ok || query.From.Before(now.Add(-duration)) {
		return query, ErrInvalid
	}
	if query.GroupBy == "" {
		query.GroupBy = "none"
	}
	allowed := "node"
	if node {
		allowed = "inbound"
	}
	if query.GroupBy != "none" && query.GroupBy != allowed {
		return query, ErrInvalid
	}
	for name, target := range map[string]*int64{"nodeId": &query.NodeID, "inboundId": &query.InboundID} {
		if value := values.Get(name); value != "" {
			*target, err = strconv.ParseInt(value, 10, 64)
			if err != nil || *target <= 0 {
				return query, ErrInvalid
			}
		}
	}
	if node && (query.GroupBy == "inbound" || query.InboundID > 0) && query.Grain != "5m" {
		return query, ErrInvalid
	}
	if !node && query.InboundID > 0 {
		return query, ErrInvalid
	}
	return query, nil
}

func (service *Service) Query(ctx context.Context, subject int64, node bool, query Query) (Result, error) {
	result := Result{From: query.From, To: query.To, Grain: query.Grain, Timezone: "UTC", Series: []Series{}, Summary: Summary{"0", "0", "0"}, Quality: Quality{Status: "unavailable"}}
	table := "traffic_records"
	filter := "user_id=$1"
	key := "'total'"
	if node {
		table = "traffic_node_records"
		filter = "node_id=$1"
	}
	if query.Grain != "5m" {
		table = "traffic_user_aggregates"
		if node {
			table = "traffic_node_aggregates"
		}
		filter += " AND grain=$4"
	}
	args := []any{subject, query.From, query.To}
	if query.Grain != "5m" {
		args = append(args, query.Grain)
	}
	if query.GroupBy == "node" {
		key = "node_id::text"
	}
	if query.GroupBy == "inbound" {
		key = "inbound_id::text"
	}
	if !node && query.NodeID > 0 {
		args = append(args, query.NodeID)
		filter += fmt.Sprintf(" AND node_id=$%d", len(args))
	}
	if node && query.InboundID > 0 {
		args = append(args, query.InboundID)
		filter += fmt.Sprintf(" AND inbound_id=$%d", len(args))
	}
	rows, err := service.DB.QueryContext(ctx, `SELECT `+key+`,period_start,SUM(up_bytes)::text,SUM(down_bytes)::text,bool_or(estimated) FROM `+table+` WHERE `+filter+` AND period_start >= $2 AND period_start < $3 GROUP BY 1,2 ORDER BY 1,2`, args...)
	if err != nil {
		return result, err
	}
	up, down := new(big.Int), new(big.Int)
	indices := map[string]int{}
	for rows.Next() {
		var seriesKey string
		var point Point
		var estimated bool
		if err = rows.Scan(&seriesKey, &point.At, &point.UpBytes, &point.DownBytes, &estimated); err != nil {
			rows.Close()
			return result, err
		}
		point.Complete = !estimated
		result.Quality.Estimated = result.Quality.Estimated || estimated
		index, exists := indices[seriesKey]
		if !exists {
			index = len(result.Series)
			indices[seriesKey] = index
			result.Series = append(result.Series, Series{Key: seriesKey, Points: []Point{}})
		}
		result.Series[index].Points = append(result.Series[index].Points, point)
		value, _ := new(big.Int).SetString(point.UpBytes, 10)
		up.Add(up, value)
		value, _ = new(big.Int).SetString(point.DownBytes, 10)
		down.Add(down, value)
		if result.Quality.AvailableFrom == nil || point.At.Before(*result.Quality.AvailableFrom) {
			at := point.At
			result.Quality.AvailableFrom = &at
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	result.Summary = Summary{up.String(), down.String(), new(big.Int).Add(up, down).String()}
	var last sql.NullTime
	var gap bool
	freshFilter := "node_id=$1"
	if !node {
		freshFilter = "node_id IN (SELECT DISTINCT node_id FROM traffic_records WHERE user_id=$1 UNION SELECT DISTINCT node_id FROM traffic_user_aggregates WHERE user_id=$1)"
	}
	if err = service.DB.QueryRowContext(ctx, `SELECT MAX(last_received_at),COALESCE(bool_or(status<>'collecting' OR last_received_at<now()-interval '3 minutes'),true) FROM traffic_node_state WHERE `+freshFilter, subject).Scan(&last, &gap); err != nil {
		return result, err
	}
	if last.Valid {
		result.Quality.LastReceivedAt = &last.Time
	}
	if len(result.Series) > 0 {
		result.Quality.Status = "partial"
		if !gap && !result.Quality.Estimated {
			result.Quality.Status = "complete"
		}
	}
	if !node {
		quota := &Quota{}
		err = service.DB.QueryRowContext(ctx, `SELECT COALESCE(a.quota_epoch,1),u.traffic_limit_bytes::text,(COALESCE(a.used_up_bytes,0)::numeric+COALESCE(a.used_down_bytes,0))::text,u.traffic_limit_bytes=0,u.quota_revision,a.period_start,u.expire_at FROM users u LEFT JOIN traffic_accounts a ON a.user_id=u.id WHERE u.id=$1`, subject).Scan(&quota.Epoch, &quota.LimitBytes, &quota.UsedBytes, &quota.Unlimited, &quota.Revision, &quota.PeriodStart, &quota.ExpireAt)
		if err != nil {
			return result, err
		}
		result.Quota = quota
	}
	return result, nil
}

func (service *Service) Rollup(ctx context.Context) error {
	for count := 0; count < 100; count++ {
		tx, err := service.DB.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		var grain string
		var start time.Time
		var revision int64
		err = tx.QueryRowContext(ctx, `SELECT grain,period_start,revision FROM traffic_rollup_jobs ORDER BY period_start,grain LIMIT 1 FOR UPDATE SKIP LOCKED`).Scan(&grain, &start, &revision)
		if err == sql.ErrNoRows {
			tx.Rollback()
			return nil
		}
		if err != nil {
			tx.Rollback()
			return err
		}
		duration := time.Hour
		if grain == "1d" {
			duration = 24 * time.Hour
		}
		end := start.Add(duration)
		_, err = tx.ExecContext(ctx, `INSERT INTO traffic_user_aggregates(user_id,node_id,grain,period_start,up_bytes,down_bytes,estimated) SELECT user_id,node_id,$1,$2,SUM(up_bytes),SUM(down_bytes),bool_or(estimated) FROM traffic_records WHERE period_start >= $2 AND period_start < $3 GROUP BY user_id,node_id ON CONFLICT(user_id,node_id,grain,period_start) DO UPDATE SET up_bytes=EXCLUDED.up_bytes,down_bytes=EXCLUDED.down_bytes,estimated=EXCLUDED.estimated,updated_at=now()`, grain, start, end)
		if err == nil {
			_, err = tx.ExecContext(ctx, `INSERT INTO traffic_node_aggregates(node_id,grain,period_start,up_bytes,down_bytes,estimated) SELECT node_id,$1,$2,SUM(up_bytes),SUM(down_bytes),bool_or(estimated) FROM traffic_node_records WHERE period_start >= $2 AND period_start < $3 GROUP BY node_id ON CONFLICT(node_id,grain,period_start) DO UPDATE SET up_bytes=EXCLUDED.up_bytes,down_bytes=EXCLUDED.down_bytes,estimated=EXCLUDED.estimated,updated_at=now()`, grain, start, end)
		}
		if err == nil {
			_, err = tx.ExecContext(ctx, `DELETE FROM traffic_rollup_jobs WHERE grain=$1 AND period_start=$2 AND revision=$3`, grain, start, revision)
		}
		if err != nil {
			tx.Rollback()
			return err
		}
		if err = tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

func (service *Service) Cleanup(ctx context.Context) error {
	for _, statement := range []string{
		`DELETE FROM traffic_reports WHERE received_at<now()-interval '7 days'`,
		`DELETE FROM traffic_cursors c WHERE last_sample_at<now()-interval '7 days' AND NOT EXISTS(SELECT 1 FROM traffic_node_state s WHERE s.node_id=c.node_id AND s.active_epoch_id=c.epoch_id)`,
		`DELETE FROM traffic_records WHERE period_start<now()-interval '30 days' AND NOT EXISTS(SELECT 1 FROM traffic_rollup_jobs j WHERE j.period_start=date_trunc('hour',traffic_records.period_start) OR j.period_start=date_trunc('day',traffic_records.period_start))`,
		`DELETE FROM traffic_node_records WHERE period_start<now()-interval '30 days' AND NOT EXISTS(SELECT 1 FROM traffic_rollup_jobs j WHERE j.period_start=date_trunc('hour',traffic_node_records.period_start) OR j.period_start=date_trunc('day',traffic_node_records.period_start))`,
		`DELETE FROM traffic_user_aggregates WHERE (grain='1h' AND period_start<now()-interval '180 days') OR (grain='1d' AND period_start<now()-interval '730 days')`,
		`DELETE FROM traffic_node_aggregates WHERE (grain='1h' AND period_start<now()-interval '180 days') OR (grain='1d' AND period_start<now()-interval '730 days')`,
		`UPDATE traffic_node_state SET status='stale' WHERE last_received_at<now()-interval '3 minutes' AND status='collecting'`,
	} {
		if _, err := service.DB.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}

func (service *Service) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		_ = service.Rollup(ctx)
		_ = service.Cleanup(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

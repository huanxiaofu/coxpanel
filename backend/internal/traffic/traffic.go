package traffic

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"regexp"
	"sort"
	"time"
)

var ErrInvalid = errors.New("traffic_invalid")
var ErrConflict = errors.New("traffic_sequence_conflict")
var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

type Sample struct {
	InboundID int64 `json:"inboundId"`
	UserID    int64 `json:"userId,omitempty"`
	UpBytes   int64 `json:"upBytes,string"`
	DownBytes int64 `json:"downBytes,string"`
}

type Report struct {
	SchemaVersion string    `json:"schemaVersion"`
	NodeID        int64     `json:"nodeId"`
	EpochID       string    `json:"epochId"`
	Sequence      int64     `json:"sequence"`
	PeriodStart   time.Time `json:"periodStart"`
	PeriodEnd     time.Time `json:"periodEnd"`
	Complete      bool      `json:"complete"`
	Samples       []Sample  `json:"samples"`
}

type Receipt struct {
	Accepted         bool      `json:"accepted"`
	Duplicate        bool      `json:"duplicate"`
	AcceptedSequence int64     `json:"acceptedSequence"`
	ServerTime       time.Time `json:"serverTime"`
}

type Bucket struct {
	At   time.Time
	Up   int64
	Down int64
}

func Add(left, right int64) (int64, error) {
	if left < 0 || right < 0 || left > math.MaxInt64-right {
		return 0, ErrInvalid
	}
	return left + right, nil
}

func Split(start, end time.Time, up, down int64) ([]Bucket, error) {
	if !end.After(start) || end.Sub(start) > 24*time.Hour || up < 0 || down < 0 {
		return nil, ErrInvalid
	}
	var result []Bucket
	remainingUp, remainingDown := up, down
	total := end.Sub(start).Nanoseconds()
	for cursor := start; cursor.Before(end); {
		boundary := cursor.UTC().Truncate(5 * time.Minute).Add(5 * time.Minute)
		if boundary.After(end) {
			boundary = end
		}
		part := boundary.Sub(cursor).Nanoseconds()
		portion := func(value int64) int64 {
			number := new(big.Int).Mul(big.NewInt(value), big.NewInt(part))
			return number.Quo(number, big.NewInt(total)).Int64()
		}
		bucket := Bucket{At: cursor.UTC().Truncate(5 * time.Minute), Up: portion(up), Down: portion(down)}
		if boundary.Equal(end) {
			bucket.Up = remainingUp
			bucket.Down = remainingDown
		}
		remainingUp -= bucket.Up
		remainingDown -= bucket.Down
		result = append(result, bucket)
		cursor = boundary
	}
	return result, nil
}

func (report *Report) Validate(nodeID int64, now time.Time) error {
	if report.SchemaVersion != "traffic/v2" || report.NodeID != nodeID || nodeID <= 0 || !uuidPattern.MatchString(report.EpochID) || report.Sequence <= 0 || len(report.Samples) > 4000 || !report.PeriodEnd.After(report.PeriodStart) || report.PeriodEnd.Sub(report.PeriodStart) > 24*time.Hour || report.PeriodEnd.After(now.Add(time.Minute)) || report.PeriodStart.Before(now.Add(-7*24*time.Hour)) {
		return ErrInvalid
	}
	seen := map[string]bool{}
	for _, sample := range report.Samples {
		key := fmt.Sprintf("%d:%d", sample.InboundID, sample.UserID)
		if sample.InboundID <= 0 || sample.UserID < 0 || sample.UpBytes < 0 || sample.DownBytes < 0 || seen[key] {
			return ErrInvalid
		}
		seen[key] = true
	}
	sort.Slice(report.Samples, func(left, right int) bool {
		if report.Samples[left].InboundID != report.Samples[right].InboundID {
			return report.Samples[left].InboundID < report.Samples[right].InboundID
		}
		return report.Samples[left].UserID < report.Samples[right].UserID
	})
	return nil
}

type Service struct{ DB *sql.DB }

func (service *Service) Ingest(ctx context.Context, nodeID int64, report Report) (Receipt, error) {
	now := time.Now().UTC()
	report.Samples = append([]Sample(nil), report.Samples...)
	receipt := Receipt{ServerTime: now, AcceptedSequence: report.Sequence}
	if err := report.Validate(nodeID, now); err != nil {
		return receipt, err
	}
	body, _ := json.Marshal(report)
	sum := sha256.Sum256(body)
	hash := hex.EncodeToString(sum[:])
	tx, err := service.DB.BeginTx(ctx, nil)
	if err != nil {
		return receipt, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(7133,$1::int)`, nodeID); err != nil {
		return receipt, err
	}
	var existing string
	err = tx.QueryRowContext(ctx, `SELECT payload_hash FROM traffic_reports WHERE node_id=$1 AND epoch_id=$2 AND sequence=$3`, nodeID, report.EpochID, report.Sequence).Scan(&existing)
	if err == nil {
		if existing != hash {
			return receipt, ErrConflict
		}
		receipt.Accepted = true
		receipt.Duplicate = true
		return receipt, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return receipt, err
	}
	var epoch string
	var lastSequence int64
	var lastReceived sql.NullTime
	err = tx.QueryRowContext(ctx, `SELECT active_epoch_id::text,last_sequence,last_received_at FROM traffic_node_state WHERE node_id=$1 FOR UPDATE`, nodeID).Scan(&epoch, &lastSequence, &lastReceived)
	first := errors.Is(err, sql.ErrNoRows)
	if err != nil && !first {
		return receipt, err
	}
	newEpoch := epoch != report.EpochID
	if !newEpoch && report.Sequence <= lastSequence {
		return receipt, ErrConflict
	}
	if newEpoch && !first {
		var retired bool
		err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM traffic_reports WHERE node_id=$1 AND epoch_id=$2)`, nodeID, report.EpochID).Scan(&retired)
		if err != nil {
			return receipt, err
		}
		if retired || report.Sequence != 1 {
			return receipt, ErrConflict
		}
	}
	if first && report.Sequence != 1 {
		return receipt, ErrConflict
	}
	gap := !report.Complete || first || newEpoch || report.Sequence != lastSequence+1
	if _, err = tx.ExecContext(ctx, `INSERT INTO traffic_reports(node_id,epoch_id,sequence,payload_hash,period_start,period_end) VALUES($1,$2,$3,$4,$5,$6)`, nodeID, report.EpochID, report.Sequence, hash, report.PeriodStart, report.PeriodEnd); err != nil {
		return receipt, err
	}
	for _, sample := range report.Samples {
		var role string
		if err = tx.QueryRowContext(ctx, `SELECT role FROM inbounds WHERE id=$1 AND node_id=$2`, sample.InboundID, nodeID).Scan(&role); err != nil {
			return receipt, ErrInvalid
		}
		if sample.UserID > 0 {
			var allowed bool
			err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM user_credentials WHERE user_id=$1 AND inbound_id=$2)`, sample.UserID, sample.InboundID).Scan(&allowed)
			if err != nil {
				return receipt, err
			}
			if !allowed || role != "entry" {
				return receipt, ErrInvalid
			}
		}
		key := fmt.Sprintf("inbound:%d", sample.InboundID)
		if sample.UserID > 0 {
			key = fmt.Sprintf("user:%d:inbound:%d", sample.UserID, sample.InboundID)
		}
		var oldUp, oldDown int64
		var sampled time.Time
		err = tx.QueryRowContext(ctx, `SELECT last_up_bytes,last_down_bytes,last_sample_at FROM traffic_cursors WHERE node_id=$1 AND epoch_id=$2 AND series_key=$3`, nodeID, report.EpochID, key).Scan(&oldUp, &oldDown, &sampled)
		baseline := errors.Is(err, sql.ErrNoRows)
		if err != nil && !baseline {
			return receipt, err
		}
		if !baseline && !report.PeriodEnd.After(sampled) {
			return receipt, ErrConflict
		}
		if !baseline && (sample.UpBytes < oldUp || sample.DownBytes < oldDown) {
			return receipt, ErrConflict
		}
		if !baseline {
			up, down := sample.UpBytes-oldUp, sample.DownBytes-oldDown
			buckets, splitErr := Split(sampled, report.PeriodEnd, up, down)
			if splitErr != nil {
				return receipt, splitErr
			}
			estimated := gap || !sampled.Equal(report.PeriodStart) || len(buckets) > 1
			for _, bucket := range buckets {
				if err = writeBucket(ctx, tx, nodeID, sample, bucket, estimated); err != nil {
					return receipt, err
				}
			}
			if sample.UserID > 0 {
				if _, err = tx.ExecContext(ctx, `INSERT INTO traffic_accounts(user_id,period_start) VALUES($1,$2) ON CONFLICT DO NOTHING`, sample.UserID, now); err != nil {
					return receipt, err
				}
				var usedUp, usedDown, lifeUp, lifeDown int64
				if err = tx.QueryRowContext(ctx, `SELECT used_up_bytes,used_down_bytes,lifetime_up_bytes,lifetime_down_bytes FROM traffic_accounts WHERE user_id=$1 FOR UPDATE`, sample.UserID).Scan(&usedUp, &usedDown, &lifeUp, &lifeDown); err != nil {
					return receipt, err
				}
				for _, pair := range [][2]int64{{usedUp, up}, {usedDown, down}, {lifeUp, up}, {lifeDown, down}} {
					if _, err = Add(pair[0], pair[1]); err != nil {
						return receipt, err
					}
				}
				if _, err = tx.ExecContext(ctx, `UPDATE traffic_accounts SET used_up_bytes=used_up_bytes+$2,used_down_bytes=used_down_bytes+$3,lifetime_up_bytes=lifetime_up_bytes+$2,lifetime_down_bytes=lifetime_down_bytes+$3,updated_at=now() WHERE user_id=$1`, sample.UserID, up, down); err != nil {
					return receipt, err
				}
			}
		} else {
			gap = true
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO traffic_cursors(node_id,epoch_id,series_key,last_sequence,last_up_bytes,last_down_bytes,last_sample_at) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(node_id,epoch_id,series_key) DO UPDATE SET last_sequence=EXCLUDED.last_sequence,last_up_bytes=EXCLUDED.last_up_bytes,last_down_bytes=EXCLUDED.last_down_bytes,last_sample_at=EXCLUDED.last_sample_at`, nodeID, report.EpochID, key, report.Sequence, sample.UpBytes, sample.DownBytes, report.PeriodEnd); err != nil {
			return receipt, err
		}
	}
	status := "collecting"
	if gap {
		status = "gap"
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO traffic_node_state(node_id,active_epoch_id,last_sequence,last_received_at,last_complete_at,coverage_start,status) VALUES($1,$2,$3,$4,CASE WHEN $5='collecting' THEN $6::timestamptz END,$6,$5) ON CONFLICT(node_id) DO UPDATE SET active_epoch_id=EXCLUDED.active_epoch_id,last_sequence=EXCLUDED.last_sequence,last_received_at=EXCLUDED.last_received_at,last_complete_at=COALESCE(EXCLUDED.last_complete_at,traffic_node_state.last_complete_at),status=EXCLUDED.status`, nodeID, report.EpochID, report.Sequence, now, status, report.PeriodEnd); err != nil {
		return receipt, err
	}
	if err = tx.Commit(); err != nil {
		return receipt, err
	}
	receipt.Accepted = true
	return receipt, nil
}

func writeBucket(ctx context.Context, tx *sql.Tx, nodeID int64, sample Sample, bucket Bucket, estimated bool) error {
	var err error
	if sample.UserID > 0 {
		_, err = tx.ExecContext(ctx, `INSERT INTO traffic_records(user_id,node_id,inbound_id,period_start,up_bytes,down_bytes,estimated) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(user_id,node_id,inbound_id,period_start) DO UPDATE SET up_bytes=traffic_records.up_bytes+EXCLUDED.up_bytes,down_bytes=traffic_records.down_bytes+EXCLUDED.down_bytes,estimated=traffic_records.estimated OR EXCLUDED.estimated,updated_at=now()`, sample.UserID, nodeID, sample.InboundID, bucket.At, bucket.Up, bucket.Down, estimated)
	} else {
		_, err = tx.ExecContext(ctx, `INSERT INTO traffic_node_records(node_id,inbound_id,period_start,up_bytes,down_bytes,estimated) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(node_id,inbound_id,period_start) DO UPDATE SET up_bytes=traffic_node_records.up_bytes+EXCLUDED.up_bytes,down_bytes=traffic_node_records.down_bytes+EXCLUDED.down_bytes,estimated=traffic_node_records.estimated OR EXCLUDED.estimated,updated_at=now()`, nodeID, sample.InboundID, bucket.At, bucket.Up, bucket.Down, estimated)
	}
	if err != nil {
		return err
	}
	for _, grain := range []string{"1h", "1d"} {
		duration := time.Hour
		if grain == "1d" {
			duration = 24 * time.Hour
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO traffic_rollup_jobs(grain,period_start) VALUES($1,$2) ON CONFLICT(grain,period_start) DO UPDATE SET revision=traffic_rollup_jobs.revision+1,updated_at=now()`, grain, bucket.At.Truncate(duration)); err != nil {
			return err
		}
	}
	return nil
}

package acceptance

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/coxpanel/backend/internal/db"
	"github.com/coxpanel/backend/internal/mail"
	"github.com/coxpanel/backend/internal/traffic"
)

func TestP2TrafficIdempotencyRollupAndReset(t *testing.T) {
	database := openAcceptanceDB(t)
	ctx := context.Background()
	if err := db.Migrate(ctx, database.db); err != nil {
		t.Fatal(err)
	}
	if _, err := database.db.ExecContext(ctx, `INSERT INTO users(id,username,password_hash,role,is_active) VALUES(1,'p2-owner','synthetic','owner',true);INSERT INTO nodes(id,name,type) VALUES(1,'p2-node','managed');INSERT INTO inbounds(id,node_id,name,protocol,role,listen_port) VALUES(1,1,'entry','shadowsocks','entry',10001);INSERT INTO user_credentials(user_id,inbound_id,credential) VALUES(1,1,'synthetic')`); err != nil {
		t.Fatal(err)
	}
	service := &traffic.Service{DB: database.db}
	start := time.Now().UTC().Truncate(5 * time.Minute).Add(-10 * time.Minute)
	report := traffic.Report{SchemaVersion: "traffic/v2", NodeID: 1, EpochID: "11111111-1111-4111-8111-111111111111", Sequence: 1, PeriodStart: start, PeriodEnd: start.Add(time.Minute), Complete: true, Samples: []traffic.Sample{{InboundID: 1, UserID: 1, UpBytes: 100, DownBytes: 200}, {InboundID: 1, UpBytes: 500, DownBytes: 900}}}
	if _, err := service.Ingest(ctx, 1, report); err != nil {
		t.Fatal(err)
	}
	report.Sequence = 2
	report.PeriodStart = report.PeriodEnd
	report.PeriodEnd = report.PeriodEnd.Add(time.Minute)
	report.Samples[0].UpBytes += 10
	report.Samples[0].DownBytes += 20
	report.Samples[1].UpBytes += 40
	report.Samples[1].DownBytes += 80
	if _, err := service.Ingest(ctx, 1, report); err != nil {
		t.Fatal(err)
	}
	receipt, err := service.Ingest(ctx, 1, report)
	if err != nil || !receipt.Duplicate {
		t.Fatal("duplicate report was not acknowledged idempotently")
	}
	report.Samples[0].UpBytes++
	if _, err = service.Ingest(ctx, 1, report); !errors.Is(err, traffic.ErrConflict) {
		t.Fatal("same sequence changed payload accepted")
	}
	if err = service.Rollup(ctx); err != nil {
		t.Fatal(err)
	}
	if err = service.Rollup(ctx); err != nil {
		t.Fatal(err)
	}
	var used, nodeTotal int64
	if err = database.db.QueryRow(`SELECT used_up_bytes+used_down_bytes FROM traffic_accounts WHERE user_id=1`).Scan(&used); err != nil {
		t.Fatal(err)
	}
	if used != 30 {
		t.Fatalf("account=%d want30", used)
	}
	if err = database.db.QueryRow(`SELECT SUM(up_bytes+down_bytes) FROM traffic_node_aggregates WHERE grain='1h'`).Scan(&nodeTotal); err != nil {
		t.Fatal(err)
	}
	if nodeTotal != 120 {
		t.Fatalf("node=%d want120, separate from user", nodeTotal)
	}
	if err = service.Reset(ctx, 1, 1, 1, "synthetic regression"); err != nil {
		t.Fatal(err)
	}
	if err = service.Reset(ctx, 1, 1, 1, "duplicate"); !errors.Is(err, traffic.ErrConflict) {
		t.Fatal("old quota epoch accepted")
	}
	var lifetime int64
	if err = database.db.QueryRow(`SELECT lifetime_up_bytes+lifetime_down_bytes FROM traffic_accounts WHERE user_id=1`).Scan(&lifetime); err != nil || lifetime != 30 {
		t.Fatal("reset altered lifetime")
	}
}

type fakeMailSender struct {
	Count   int
	Payload mail.Payload
}

func (sender *fakeMailSender) Send(_ context.Context, _ string, _ string, payload mail.Payload) error {
	sender.Count++
	sender.Payload = payload
	return nil
}

func TestP2VerificationTicketInviteAtomicity(t *testing.T) {
	database := openAcceptanceDB(t)
	prepareAcceptanceSchema(t, database)
	ctx := context.Background()
	_, err := database.db.ExecContext(ctx, `INSERT INTO users(username,password_hash,email,role,is_active) VALUES('mail-owner','synthetic','owner@example.test','owner',true);INSERT INTO node_groups(name) VALUES('group');INSERT INTO invite_codes(code,max_uses,node_group_id,created_by) VALUES('SYNTHETIC-INVITE',2,1,1)`)
	if err != nil {
		t.Fatal(err)
	}
	service, err := mail.New(database.db, mail.Config{Host: "mail.example.test", Port: 587, User: "synthetic", Password: "synthetic-only", From: "sender@example.test", BaseURL: "https://panel.example.test", RequireVerification: true}, strings.Repeat("a3", 32))
	if err != nil {
		t.Fatal(err)
	}
	sender := &fakeMailSender{}
	service.Sender = sender
	challenge, err := service.Challenge(ctx, "new@example.test", "SYNTHETIC-INVITE", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = service.DeliverOne(ctx); err != nil {
		t.Fatal(err)
	}
	link := sender.Payload.Body[strings.LastIndex(sender.Payload.Body, "\n")+1:]
	parsed, err := url.Parse(link)
	if err != nil {
		t.Fatal("synthetic verification link invalid")
	}
	parameters, err := url.ParseQuery(parsed.Fragment)
	if err != nil {
		t.Fatal("synthetic fragment invalid")
	}
	confirmation, err := service.Confirm(ctx, challenge, parameters.Get("token"))
	if err != nil || confirmation.RegistrationTicket == "" {
		t.Fatal("verification failed")
	}
	if _, err = service.Confirm(ctx, challenge, parameters.Get("token")); err == nil {
		t.Fatal("token replay accepted")
	}
	if _, err = service.Register(ctx, "new-user", "synthetic", "different@example.test", "SYNTHETIC-INVITE", confirmation.RegistrationTicket); err == nil {
		t.Fatal("ticket email substitution accepted")
	}
	if _, err = service.Register(ctx, "mail-owner", "synthetic", "new@example.test", "SYNTHETIC-INVITE", confirmation.RegistrationTicket); err == nil {
		t.Fatal("duplicate account accepted")
	}
	var uses int
	if err = database.db.QueryRow(`SELECT used_count FROM invite_codes WHERE code='SYNTHETIC-INVITE'`).Scan(&uses); err != nil || uses != 0 {
		t.Fatal("failed transaction consumed invite")
	}
	user, err := service.Register(ctx, "new-user", "synthetic", "new@example.test", "SYNTHETIC-INVITE", confirmation.RegistrationTicket)
	if err != nil || user == nil {
		t.Fatal("valid ticket register failed")
	}
	if _, err = service.Register(ctx, "replay-user", "synthetic", "new@example.test", "SYNTHETIC-INVITE", confirmation.RegistrationTicket); err == nil {
		t.Fatal("registration ticket replay accepted")
	}
	if err = database.db.QueryRow(`SELECT used_count FROM invite_codes WHERE code='SYNTHETIC-INVITE'`).Scan(&uses); err != nil || uses != 1 {
		t.Fatal("invite consumption not atomic")
	}
	var verified bool
	if err = database.db.QueryRow(`SELECT email_verified_at IS NOT NULL FROM users WHERE id=$1`, user.ID).Scan(&verified); err != nil || !verified {
		t.Fatal("new account verification missing")
	}
	var count int
	beforeErr := database.db.QueryRow(`SELECT count(*) FROM notification_records`).Scan(&count)
	if beforeErr != nil {
		t.Fatal(beforeErr)
	}
	invalidChallenge, err := service.Challenge(ctx, "absent@example.test", "INVALID-INVITE", nil)
	if err != nil || len(invalidChallenge) != len(challenge) {
		t.Fatal("public invalid challenge shape differs")
	}
	var after int
	if err = database.db.QueryRow(`SELECT count(*) FROM notification_records`).Scan(&after); err != nil || after != count {
		t.Fatal("invalid invite caused mail")
	}
}

func TestP2AlertSuppressionAndTransactionalMail(t *testing.T) {
	database := openAcceptanceDB(t)
	ctx := context.Background()
	if err := db.Migrate(ctx, database.db); err != nil {
		t.Fatal(err)
	}
	if _, err := database.db.ExecContext(ctx, `INSERT INTO users(id,username,password_hash,email,role,is_active,email_verified_at,traffic_limit_bytes) VALUES(1,'mail-owner','synthetic','owner@example.test','owner',true,now(),100);INSERT INTO traffic_accounts(user_id,period_start,used_up_bytes,used_down_bytes) VALUES(1,now(),70,31)`); err != nil {
		t.Fatal(err)
	}
	service, err := mail.New(database.db, mail.Config{Host: "mail.example.test", Port: 587, User: "synthetic", Password: "synthetic-only", From: "sender@example.test", BaseURL: "https://panel.example.test"}, strings.Repeat("a2", 32))
	if err != nil {
		t.Fatal(err)
	}
	sender := &fakeMailSender{}
	service.Sender = sender
	if _, err = service.SaveRule(ctx, 1, mail.Rule{Name: "traffic", Kind: "traffic_limit", Enabled: true, ScopeType: "all", Thresholds: []int{80, 90, 100}, Channel: "email"}, 0); err != nil {
		t.Fatal(err)
	}
	if err = service.ScanAlerts(ctx); err != nil {
		t.Fatal(err)
	}
	if err = service.ScanAlerts(ctx); err != nil {
		t.Fatal(err)
	}
	var queued, suppressed int
	if err = database.db.QueryRow(`SELECT COUNT(*) FILTER(WHERE state='queued'),COUNT(*) FILTER(WHERE state='suppressed') FROM notification_records`).Scan(&queued, &suppressed); err != nil {
		t.Fatal(err)
	}
	if queued != 1 || suppressed != 2 {
		t.Fatalf("queued/suppressed=%d/%d", queued, suppressed)
	}
	if err = service.DeliverOne(ctx); err != nil {
		t.Fatal(err)
	}
	if sender.Count != 1 {
		t.Fatal("delivery count incorrect")
	}
	if err = service.ScanAlerts(ctx); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = database.db.QueryRow(`SELECT COUNT(*) FROM notification_records`).Scan(&count); err != nil || count != 3 {
		t.Fatal("logical event duplicated")
	}
	var snapshot []byte
	if err = database.db.QueryRow(`SELECT event_snapshot FROM notification_records WHERE state='sent'`).Scan(&snapshot); err != nil {
		t.Fatal(err)
	}
	var event map[string]any
	if json.Unmarshal(snapshot, &event) != nil || event["threshold"] != float64(100) {
		t.Fatal("wrong urgent event")
	}
	preferences := mail.Preference{TrafficEnabled: false, ExpirationEnabled: true}
	if err = service.SavePreferences(ctx, 1, 1, preferences); err != nil {
		t.Fatal(err)
	}
	if err = service.SavePreferences(ctx, 1, 1, preferences); !errors.Is(err, mail.ErrConflict) {
		t.Fatal("preference stale revision accepted")
	}
}

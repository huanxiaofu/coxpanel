package mail

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/coxpanel/backend/internal/models"
)

func (service *Service) Challenge(ctx context.Context, email, invite string, userID *int64) (string, error) {
	id, err := randomUUID()
	if err != nil {
		return "", err
	}
	if !service.Configured() || service.aead == nil {
		return id, nil
	}
	email = strings.ToLower(strings.TrimSpace(email))
	if !ValidEmail(email) {
		return id, nil
	}
	tx, err := service.DB.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	purpose := "register"
	var revision *int64
	var inviteHash []byte
	if userID == nil {
		var valid bool
		err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM invite_codes WHERE code=$1 AND used_count<max_uses AND (expires_at IS NULL OR expires_at>now()) AND node_group_id IS NOT NULL) AND NOT EXISTS(SELECT 1 FROM users WHERE lower(email)=$2)`, invite, email).Scan(&valid)
		if err != nil {
			return "", err
		}
		if !valid {
			return id, nil
		}
		inviteHash = service.Lookup("invite", invite)
	} else {
		purpose = "verify_existing"
		var current string
		var value int64
		err = tx.QueryRowContext(ctx, `SELECT COALESCE(email,''),email_revision FROM users WHERE id=$1 AND is_active`, *userID).Scan(&current, &value)
		if err != nil {
			return "", err
		}
		if !strings.EqualFold(current, email) {
			return "", ErrInvalid
		}
		revision = &value
	}
	lookup := service.Lookup("email", email)
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, fmt.Sprintf("verify:%x", lookup)); err != nil {
		return "", err
	}
	var count int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM email_verifications WHERE email_lookup_hash=$1 AND created_at>now()-interval '1 hour'`, lookup).Scan(&count); err != nil {
		return "", err
	}
	if count >= 3 {
		return id, nil
	}
	token, err := randomToken()
	if err != nil {
		return "", err
	}
	encrypted, err := service.seal([]byte(email), "email_verifications:"+id+":email")
	if err != nil {
		return "", err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO email_verifications(id,purpose,user_id,email_ciphertext,email_lookup_hash,invite_lookup_hash,token_hash,email_revision,state,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,'pending',now()+interval '30 minutes')`, id, purpose, userID, encrypted, lookup, inviteHash, digest(token), revision); err != nil {
		return "", err
	}
	link := strings.TrimRight(service.Config.BaseURL, "/") + "/verify-email#challengeId=" + url.QueryEscape(id) + "&token=" + url.QueryEscape(token)
	_, err = service.Enqueue(ctx, tx, Event{UserID: userID, Kind: "email_verification", Key: "verification:" + id, Recipient: email, EmailRevision: revision, Snapshot: map[string]any{"challengeId": id, "purpose": purpose}, Payload: Payload{Subject: "CoxPanel 邮箱验证", Body: "请在30分钟内打开以下页面并点击确认。打开页面不会自动消费验证链接。\n" + link}})
	if err != nil {
		return "", err
	}
	return id, tx.Commit()
}

type Confirmation struct {
	Status             string `json:"status"`
	RegistrationTicket string `json:"registrationTicket,omitempty"`
}

func (service *Service) Confirm(ctx context.Context, id, token string) (Confirmation, error) {
	result := Confirmation{}
	if len(id) != 36 || len(token) != 43 {
		return result, ErrInvalid
	}
	tx, err := service.DB.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	var purpose, state string
	var userID, revision sql.NullInt64
	var expires time.Time
	var cipher []byte
	err = tx.QueryRowContext(ctx, `SELECT purpose,state,user_id,email_revision,expires_at,email_ciphertext FROM email_verifications WHERE id=$1 AND token_hash=$2 FOR UPDATE`, id, digest(token)).Scan(&purpose, &state, &userID, &revision, &expires, &cipher)
	if err != nil {
		return result, ErrInvalid
	}
	if state != "pending" || !time.Now().Before(expires) {
		return result, ErrInvalid
	}
	if purpose == "verify_existing" {
		email, openErr := service.open(cipher, "email_verifications:"+id+":email")
		if openErr != nil {
			return result, openErr
		}
		update, updateErr := tx.ExecContext(ctx, `UPDATE users SET email_verified_at=now() WHERE id=$1 AND email_revision=$2 AND lower(email)=$3 AND is_active`, userID.Int64, revision.Int64, strings.ToLower(string(email)))
		if updateErr != nil {
			return result, updateErr
		}
		count, _ := update.RowsAffected()
		if count != 1 {
			return result, ErrInvalid
		}
		result.Status = "existing_email_verified"
		if _, err = tx.ExecContext(ctx, `UPDATE email_verifications SET state='consumed',verified_at=now(),consumed_at=now() WHERE id=$1`, id); err != nil {
			return result, err
		}
	} else {
		ticket, ticketErr := randomToken()
		if ticketErr != nil {
			return result, ticketErr
		}
		result.Status = "verified"
		result.RegistrationTicket = ticket
		if _, err = tx.ExecContext(ctx, `UPDATE email_verifications SET state='verified',verified_at=now(),ticket_hash=$2,ticket_expires_at=now()+interval '10 minutes' WHERE id=$1`, id, digest(ticket)); err != nil {
			return result, err
		}
	}
	return result, tx.Commit()
}

func (service *Service) Register(ctx context.Context, username, passwordHash, email, invite, ticket string) (*models.User, error) {
	if len(ticket) != 43 {
		return nil, ErrInvalid
	}
	tx, err := service.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	email = strings.ToLower(strings.TrimSpace(email))
	var challengeID string
	err = tx.QueryRowContext(ctx, `SELECT id::text FROM email_verifications WHERE ticket_hash=$1 AND purpose='register' AND state='verified' AND ticket_expires_at>now() AND email_lookup_hash=$2 AND invite_lookup_hash=$3 FOR UPDATE`, digest(ticket), service.Lookup("email", email), service.Lookup("invite", invite)).Scan(&challengeID)
	if err != nil {
		return nil, ErrInvalid
	}
	var inviteID, groupID int64
	err = tx.QueryRowContext(ctx, `SELECT id,node_group_id FROM invite_codes WHERE code=$1 AND used_count<max_uses AND (expires_at IS NULL OR expires_at>now()) AND node_group_id IS NOT NULL FOR UPDATE`, invite).Scan(&inviteID, &groupID)
	if err != nil {
		return nil, ErrInvalid
	}
	user := &models.User{Username: username, Email: email, Role: "user", Active: true}
	if err = tx.QueryRowContext(ctx, `INSERT INTO users(username,password_hash,email,role,is_active,email_verified_at) VALUES($1,$2,$3,'user',true,now()) RETURNING id,created_at`, username, passwordHash, email).Scan(&user.ID, &user.CreatedAt); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO user_node_groups(user_id,group_id) VALUES($1,$2)`, user.ID, groupID); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE invite_codes SET used_count=used_count+1 WHERE id=$1`, inviteID); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE email_verifications SET state='consumed',consumed_at=now(),user_id=$2 WHERE id=$1`, challengeID, user.ID); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return user, nil
}

func (service *Service) SendInvite(ctx context.Context, actorID, inviteID int64, owner bool, email, requestID string) (int64, error) {
	if !ValidEmail(email) || len(requestID) < 8 || len(requestID) > 128 || strings.ContainsAny(requestID, "\r\n\x00") {
		return 0, ErrInvalid
	}
	if !service.Configured() {
		return 0, ErrUnavailable
	}
	tx, err := service.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var code string
	err = tx.QueryRowContext(ctx, `SELECT code FROM invite_codes WHERE id=$1 AND (created_by=$2 OR $3) AND used_count<max_uses AND (expires_at IS NULL OR expires_at>now()) AND node_group_id IS NOT NULL`, inviteID, actorID, owner).Scan(&code)
	if err != nil {
		return 0, sql.ErrNoRows
	}
	key := fmt.Sprintf("invite:%d:%s", actorID, requestID)
	var existing int64
	var existingInvite string
	err = tx.QueryRowContext(ctx, `SELECT id,event_snapshot->>'inviteId' FROM notification_records WHERE dedupe_key=$1`, key).Scan(&existing, &existingInvite)
	if err == nil {
		if existingInvite != fmt.Sprint(inviteID) {
			return 0, ErrConflict
		}
		return existing, nil
	}
	if err != sql.ErrNoRows {
		return 0, err
	}
	if err = service.rate(ctx, tx, actorID, "invitation", 20); err != nil {
		return 0, err
	}
	id, err := service.Enqueue(ctx, tx, Event{Kind: "invitation", Key: key, Recipient: email, Snapshot: map[string]any{"actorId": actorID, "inviteId": inviteID}, Payload: Payload{Subject: "CoxPanel 邀请", Body: "你已获邀注册 CoxPanel。\n注册地址：" + strings.TrimRight(service.Config.BaseURL, "/") + "/register\n邀请码：" + code}})
	if err != nil {
		return 0, err
	}
	return id, tx.Commit()
}
func (service *Service) rate(ctx context.Context, tx *sql.Tx, actorID int64, kind string, limit int) error {
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, fmt.Sprintf("mail:%s:%d", kind, actorID)); err != nil {
		return err
	}
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM notification_records WHERE kind=$1 AND event_snapshot->>'actorId'=$2 AND created_at>now()-interval '1 hour'`, kind, fmt.Sprint(actorID)).Scan(&count); err != nil {
		return err
	}
	if count >= limit {
		return ErrRateLimited
	}
	return nil
}
func (service *Service) Test(ctx context.Context, userID int64) (int64, error) {
	if !service.Configured() {
		return 0, ErrUnavailable
	}
	tx, err := service.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var email string
	var revision int64
	if err = tx.QueryRowContext(ctx, `SELECT email,email_revision FROM users WHERE id=$1 AND role='owner' AND is_active`, userID).Scan(&email, &revision); err != nil {
		return 0, ErrInvalid
	}
	if err = service.rate(ctx, tx, userID, "smtp_test", 3); err != nil {
		return 0, err
	}
	requestID, err := randomUUID()
	if err != nil {
		return 0, err
	}
	id, err := service.Enqueue(ctx, tx, Event{UserID: &userID, Kind: "smtp_test", Key: "smtp-test:" + requestID, Recipient: email, EmailRevision: &revision, Snapshot: map[string]any{"actorId": userID}, Payload: Payload{Subject: "CoxPanel SMTP 测试", Body: "此邮件用于验证邮件工作链路。邮局接受投递不代表进入收件箱。"}})
	if err != nil {
		return 0, err
	}
	return id, tx.Commit()
}

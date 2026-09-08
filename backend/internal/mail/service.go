package mail

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	netmail "net/mail"
	"net/smtp"
	"net/url"
	"strings"
	"time"
)

var ErrUnavailable = errors.New("mail_unavailable")
var ErrInvalid = errors.New("mail_invalid")
var ErrConflict = errors.New("revision_conflict")
var ErrRateLimited = errors.New("mail_rate_limited")
var ErrChannelUnsupported = errors.New("channel_unsupported")

type Config struct {
	Host                string
	Port                int
	User                string
	Password            string
	From                string
	BaseURL             string
	RequireVerification bool
}
type Payload struct {
	Subject string `json:"subject"`
	Body    string `json:"body"`
}
type Sender interface {
	Send(context.Context, string, string, Payload) error
}
type Service struct {
	DB     *sql.DB
	Config Config
	aead   cipher.AEAD
	key    []byte
	Sender Sender
}

func New(db *sql.DB, config Config, encodedKey string) (*Service, error) {
	service := &Service{DB: db, Config: config}
	if config.Host != "" {
		base, err := url.Parse(config.BaseURL)
		if err != nil || base.Host == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" || (base.Scheme != "https" && !(base.Scheme == "http" && net.ParseIP(base.Hostname()) != nil && net.ParseIP(base.Hostname()).IsLoopback())) {
			return nil, ErrUnavailable
		}
	}
	if encodedKey != "" {
		key, err := hex.DecodeString(encodedKey)
		if err != nil || len(key) != 32 {
			return nil, ErrUnavailable
		}
		block, err := aes.NewCipher(key)
		if err != nil {
			return nil, ErrUnavailable
		}
		service.aead, err = cipher.NewGCM(block)
		if err != nil {
			return nil, ErrUnavailable
		}
		service.key = key
	}
	if config.RequireVerification && (!service.Configured() || service.aead == nil) {
		return nil, ErrUnavailable
	}
	service.Sender = service
	return service, nil
}
func (service *Service) Configured() bool {
	return service != nil && service.Config.Host != "" && service.Config.Port == 587 && service.Config.User != "" && service.Config.Password != "" && ValidEmail(service.Config.From)
}
func ValidEmail(value string) bool {
	address, err := netmail.ParseAddress(value)
	return err == nil && address.Address == value && len(value) <= 254 && !strings.ContainsAny(value, "\r\n\x00")
}
func Mask(value string) string {
	parts := strings.Split(value, "@")
	if len(parts) != 2 {
		return "未配置"
	}
	return "***@" + parts[1]
}
func randomToken() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}
func randomUUID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	value[6] = (value[6] & 15) | 64
	value[8] = (value[8] & 63) | 128
	return fmt.Sprintf("%x-%x-%x-%x-%x", value[:4], value[4:6], value[6:8], value[8:10], value[10:]), nil
}
func digest(value string) []byte { sum := sha256.Sum256([]byte(value)); return sum[:] }
func (service *Service) Lookup(purpose, value string) []byte {
	mac := hmac.New(sha256.New, service.key)
	mac.Write([]byte(purpose + "\x00" + value))
	return mac.Sum(nil)
}
func (service *Service) seal(value []byte, aad string) ([]byte, error) {
	if service.aead == nil {
		return nil, ErrUnavailable
	}
	nonce := make([]byte, service.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return append([]byte("v1:default:"), service.aead.Seal(nonce, nonce, value, []byte(aad))...), nil
}
func (service *Service) open(value []byte, aad string) ([]byte, error) {
	prefix := []byte("v1:default:")
	if service.aead == nil || len(value) < len(prefix)+service.aead.NonceSize() || string(value[:len(prefix)]) != string(prefix) {
		return nil, ErrUnavailable
	}
	value = value[len(prefix):]
	return service.aead.Open(nil, value[:service.aead.NonceSize()], value[service.aead.NonceSize():], []byte(aad))
}

type Event struct {
	UserID                      *int64
	RuleID                      *int64
	Kind, Key, Recipient, State string
	EmailRevision               *int64
	Snapshot                    map[string]any
	Payload                     Payload
}

func (service *Service) Seal(ctx context.Context, aad string, value []byte) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return service.seal(value, aad)
}

func (service *Service) Open(ctx context.Context, aad string, value []byte) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return service.open(value, aad)
}

func (service *Service) Enqueue(ctx context.Context, tx *sql.Tx, event Event) (int64, error) {
	var existing int64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM notification_records WHERE dedupe_key=$1`, event.Key).Scan(&existing); err == nil {
		return existing, nil
	} else if err != sql.ErrNoRows {
		return 0, err
	}
	var id int64
	if err := tx.QueryRowContext(ctx, `SELECT nextval('notification_records_id_seq')`).Scan(&id); err != nil {
		return 0, err
	}
	if event.State == "" {
		event.State = "queued"
	}
	recipient, payload := []byte{}, []byte{}
	if event.State == "queued" || event.State == "retry" {
		var err error
		recipient, err = service.seal([]byte(event.Recipient), fmt.Sprintf("notification_records:%d:recipient", id))
		if err != nil {
			return 0, err
		}
		body, _ := json.Marshal(event.Payload)
		payload, err = service.seal(body, fmt.Sprintf("notification_records:%d:payload", id))
		if err != nil {
			return 0, err
		}
	}
	if event.Snapshot == nil {
		event.Snapshot = map[string]any{}
	}
	event.Snapshot["recipientMasked"] = Mask(event.Recipient)
	snapshot, err := json.Marshal(event.Snapshot)
	if err != nil {
		return 0, err
	}
	messageID := fmt.Sprintf("<coxpanel-%d-%x@notifications.invalid>", id, digest(event.Key)[:8])
	err = tx.QueryRowContext(ctx, `INSERT INTO notification_records(id,user_id,rule_id,kind,dedupe_key,event_snapshot,recipient_ciphertext,payload_ciphertext,email_revision,state,next_attempt_at,message_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,now(),$11) ON CONFLICT(dedupe_key) DO UPDATE SET dedupe_key=EXCLUDED.dedupe_key RETURNING id`, id, event.UserID, event.RuleID, event.Kind, event.Key, snapshot, recipient, payload, event.EmailRevision, event.State, messageID).Scan(&existing)
	return existing, err
}

func (service *Service) Send(ctx context.Context, recipient, messageID string, payload Payload) error {
	if !service.Configured() || !ValidEmail(recipient) || strings.ContainsAny(payload.Subject, "\r\n\x00") {
		return ErrUnavailable
	}
	dialer := &net.Dialer{Timeout: 15 * time.Second}
	connection, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(service.Config.Host, "587"))
	if err != nil {
		return errors.New("smtp_connect")
	}
	defer connection.Close()
	deadline := time.Now().Add(30 * time.Second)
	if value, ok := ctx.Deadline(); ok && value.Before(deadline) {
		deadline = value
	}
	_ = connection.SetDeadline(deadline)
	stop := context.AfterFunc(ctx, func() { connection.Close() })
	defer stop()
	client, err := smtp.NewClient(connection, service.Config.Host)
	if err != nil {
		return errors.New("smtp_greeting")
	}
	defer client.Close()
	if ok, _ := client.Extension("STARTTLS"); !ok {
		return errors.New("smtp_starttls_required")
	}
	if err = client.StartTLS(&tls.Config{ServerName: service.Config.Host, MinVersion: tls.VersionTLS12}); err != nil {
		return errors.New("smtp_tls")
	}
	if err = client.Auth(smtp.PlainAuth("", service.Config.User, service.Config.Password, service.Config.Host)); err != nil {
		return errors.New("smtp_auth")
	}
	if err = client.Mail(service.Config.From); err != nil {
		return errors.New("smtp_sender")
	}
	if err = client.Rcpt(recipient); err != nil {
		return errors.New("smtp_recipient")
	}
	writer, err := client.Data()
	if err != nil {
		return errors.New("smtp_data")
	}
	message := "From: " + service.Config.From + "\r\nTo: " + recipient + "\r\nMessage-ID: " + messageID + "\r\nDate: " + time.Now().UTC().Format(time.RFC1123Z) + "\r\nSubject: =?UTF-8?B?" + base64.StdEncoding.EncodeToString([]byte(payload.Subject)) + "?=\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\nContent-Transfer-Encoding: base64\r\n\r\n"
	body := base64.StdEncoding.EncodeToString([]byte(payload.Body))
	for len(body) > 76 {
		message += body[:76] + "\r\n"
		body = body[76:]
	}
	message += body + "\r\n"
	if _, err = writer.Write([]byte(message)); err != nil {
		return errors.New("smtp_write")
	}
	if err = writer.Close(); err != nil {
		return errors.New("smtp_acceptance_unknown")
	}
	_ = client.Quit()
	return nil
}

func (service *Service) DeliverOne(ctx context.Context) error {
	if service.aead == nil || !service.Configured() {
		return ErrUnavailable
	}
	tx, err := service.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var id int64
	var recipientCipher, payloadCipher []byte
	var messageID string
	var attempts int
	err = tx.QueryRowContext(ctx, `SELECT id,recipient_ciphertext,payload_ciphertext,message_id,attempts FROM notification_records WHERE ((state IN ('queued','retry') AND next_attempt_at<=now()) OR (state='sending' AND lease_until<now())) ORDER BY next_attempt_at,id LIMIT 1 FOR UPDATE SKIP LOCKED`).Scan(&id, &recipientCipher, &payloadCipher, &messageID, &attempts)
	if err != nil {
		return err
	}
	valid, err := service.Eligible(ctx, tx, id)
	if err != nil {
		return err
	}
	if !valid {
		_, err = tx.ExecContext(ctx, `UPDATE notification_records SET state='suppressed',recipient_ciphertext=''::bytea,payload_ciphertext=''::bytea,lease_until=NULL WHERE id=$1`, id)
		if err != nil {
			return err
		}
		return tx.Commit()
	}
	attempts++
	if _, err = tx.ExecContext(ctx, `UPDATE notification_records SET state='sending',attempts=$2,lease_until=now()+interval '60 seconds' WHERE id=$1`, id, attempts); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	started := time.Now()
	recipient, err := service.open(recipientCipher, fmt.Sprintf("notification_records:%d:recipient", id))
	var payload Payload
	if err == nil {
		var body []byte
		body, err = service.open(payloadCipher, fmt.Sprintf("notification_records:%d:payload", id))
		if err == nil {
			err = json.Unmarshal(body, &payload)
		}
	}
	if err == nil {
		err = service.Sender.Send(ctx, string(recipient), messageID, payload)
	}
	state, errorCode := "sent", ""
	if err != nil {
		state = "retry"
		errorCode = "smtp_delivery_failed"
		if attempts >= 5 {
			state = "dead"
		}
	}
	delay := time.Minute * time.Duration(1<<min(attempts, 10))
	finish, finishErr := service.DB.BeginTx(ctx, nil)
	if finishErr != nil {
		return finishErr
	}
	defer finish.Rollback()
	if _, finishErr = finish.ExecContext(ctx, `INSERT INTO notification_attempts(notification_id,attempt_no,result,duration_ms,error_code) VALUES($1,$2,$3,$4,NULLIF($5,'')) ON CONFLICT DO NOTHING`, id, attempts, state, int(time.Since(started).Milliseconds()), errorCode); finishErr != nil {
		return finishErr
	}
	_, finishErr = finish.ExecContext(ctx, `UPDATE notification_records SET state=$2,last_error_code=NULLIF($3,''),next_attempt_at=$4,lease_until=NULL,sent_at=CASE WHEN $2='sent' THEN now() ELSE sent_at END,provider_status=CASE WHEN $2='sent' THEN 'accepted_by_mail_server' ELSE NULL END WHERE id=$1 AND attempts=$5 AND state='sending'`, id, state, errorCode, time.Now().Add(delay), attempts)
	if finishErr != nil {
		return finishErr
	}
	return finish.Commit()
}

func (service *Service) Run(ctx context.Context) {
	if service.aead == nil || !service.Configured() {
		return
	}
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		for count := 0; count < 20; count++ {
			if ctx.Err() != nil {
				return
			}
			if err := service.DeliverOne(ctx); err != nil {
				break
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

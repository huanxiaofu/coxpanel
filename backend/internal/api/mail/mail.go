package mail

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/coxpanel/backend/internal/api/middleware"
	mailer "github.com/coxpanel/backend/internal/mail"
	"github.com/go-chi/chi/v5"
)

type rateEntry struct {
	Start time.Time
	Count int
}
type Handler struct {
	Service *mailer.Service
	mutex   sync.Mutex
	rates   map[string]rateEntry
}

func fail(w http.ResponseWriter, err error) {
	status, code := 500, "internal"
	switch {
	case errors.Is(err, mailer.ErrInvalid):
		status, code = 422, "mail_invalid"
	case errors.Is(err, mailer.ErrConflict):
		status, code = 409, "revision_conflict"
	case errors.Is(err, mailer.ErrUnavailable):
		status, code = 503, "mail_unavailable"
	case errors.Is(err, mailer.ErrRateLimited):
		status, code = 429, "rate_limited"
		w.Header().Set("Retry-After", "3600")
	case errors.Is(err, mailer.ErrChannelUnsupported):
		status, code = 422, "channel_unsupported"
	case errors.Is(err, sql.ErrNoRows):
		status, code = 404, "not_found"
	}
	middleware.Err(w, status, code, "邮件操作未完成，请检查状态和输入")
}
func decode(w http.ResponseWriter, r *http.Request, target any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 65536))
	decoder.DisallowUnknownFields()
	if decoder.Decode(target) != nil {
		fail(w, mailer.ErrInvalid)
		return false
	}
	if decoder.Decode(new(any)) != io.EOF {
		fail(w, mailer.ErrInvalid)
		return false
	}
	return true
}
func id(r *http.Request) int64 {
	value, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	return value
}
func (h *Handler) limit(w http.ResponseWriter, r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	h.mutex.Lock()
	defer h.mutex.Unlock()
	if h.rates == nil {
		h.rates = map[string]rateEntry{}
	}
	now := time.Now()
	if len(h.rates) > 10000 {
		for key, value := range h.rates {
			if now.Sub(value.Start) > time.Hour {
				delete(h.rates, key)
			}
		}
		if len(h.rates) > 10000 {
			fail(w, mailer.ErrRateLimited)
			return false
		}
	}
	entry := h.rates[host]
	if now.Sub(entry.Start) >= time.Hour {
		entry = rateEntry{Start: now}
	}
	entry.Count++
	h.rates[host] = entry
	if entry.Count > 30 {
		fail(w, mailer.ErrRateLimited)
		return false
	}
	return true
}
func (h *Handler) Status(w http.ResponseWriter, r *http.Request) {
	middleware.JSON(w, 200, map[string]any{"emailVerificationRequired": h.Service.Config.RequireVerification, "emailVerificationAvailable": h.Service.Configured()})
}
func (h *Handler) Challenge(w http.ResponseWriter, r *http.Request) {
	if !h.limit(w, r) {
		return
	}
	var request struct {
		Email      string `json:"email"`
		InviteCode string `json:"inviteCode"`
	}
	if !decode(w, r, &request) {
		return
	}
	challenge, err := h.Service.Challenge(r.Context(), request.Email, request.InviteCode, nil)
	if err != nil {
		fail(w, err)
		return
	}
	middleware.JSON(w, 202, map[string]any{"challengeId": challenge})
}
func (h *Handler) Confirm(w http.ResponseWriter, r *http.Request) {
	if !h.limit(w, r) {
		return
	}
	var request struct {
		ChallengeID string `json:"challengeId"`
		Token       string `json:"token"`
	}
	if !decode(w, r, &request) {
		return
	}
	confirmation, err := h.Service.Confirm(r.Context(), request.ChallengeID, request.Token)
	if err != nil {
		fail(w, err)
		return
	}
	middleware.JSON(w, 200, confirmation)
}
func (h *Handler) Existing(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFrom(r.Context())
	challenge, err := h.Service.Challenge(r.Context(), user.Email, "", &user.ID)
	if err != nil {
		fail(w, err)
		return
	}
	middleware.JSON(w, 202, map[string]any{"challengeId": challenge})
}
func (h *Handler) SMTP(w http.ResponseWriter, r *http.Request) {
	config := h.Service.Config
	var lastTest any
	var state, code string
	var created time.Time
	err := h.Service.DB.QueryRowContext(r.Context(), `SELECT state,COALESCE(last_error_code,''),created_at FROM notification_records WHERE kind='smtp_test' ORDER BY id DESC LIMIT 1`).Scan(&state, &code, &created)
	if err == nil {
		lastTest = map[string]any{"state": state, "errorCode": code, "createdAt": created}
	}
	middleware.JSON(w, 200, map[string]any{"host": config.Host, "port": config.Port, "security": "STARTTLS", "fromMasked": mailer.Mask(config.From), "configured": h.Service.Configured(), "passwordConfigured": config.Password != "", "lastTest": lastTest, "emailVerificationRequired": config.RequireVerification})
}
func (h *Handler) Test(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	if r.ContentLength > 0 {
		if !decode(w, r, &body) {
			return
		}
		if len(body) > 0 {
			fail(w, mailer.ErrInvalid)
			return
		}
	}
	notificationID, err := h.Service.Test(r.Context(), middleware.UserFrom(r.Context()).ID)
	if err != nil {
		fail(w, err)
		return
	}
	middleware.JSON(w, 202, map[string]any{"notificationId": notificationID, "state": "queued"})
}
func (h *Handler) Invite(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Email     string `json:"email"`
		RequestID string `json:"requestId"`
	}
	if !decode(w, r, &request) {
		return
	}
	user := middleware.UserFrom(r.Context())
	notificationID, err := h.Service.SendInvite(r.Context(), user.ID, id(r), user.Role == "owner", request.Email, request.RequestID)
	if err != nil {
		fail(w, err)
		return
	}
	middleware.JSON(w, 202, map[string]any{"notificationId": notificationID})
}
func (h *Handler) Rules(w http.ResponseWriter, r *http.Request) {
	page, err := middleware.ParsePage(r)
	if err != nil {
		fail(w, mailer.ErrInvalid)
		return
	}
	rules, err := h.Service.ListRules(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	middleware.JSON(w, 200, middleware.PageItems(rules, page, func(rule mailer.Rule) int64 { return rule.ID }, false))
}
func (h *Handler) Rule(w http.ResponseWriter, r *http.Request) {
	rules, err := h.Service.ListRules(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	for _, rule := range rules {
		if rule.ID == id(r) {
			middleware.JSON(w, 200, rule)
			return
		}
	}
	fail(w, sql.ErrNoRows)
}
func (h *Handler) SaveRule(w http.ResponseWriter, r *http.Request) {
	var request struct {
		mailer.Rule
		ExpectedRevision int64 `json:"expectedRevision"`
	}
	if !decode(w, r, &request) {
		return
	}
	request.Rule.ID = id(r)
	ruleID, err := h.Service.SaveRule(r.Context(), middleware.UserFrom(r.Context()).ID, request.Rule, request.ExpectedRevision)
	if err != nil {
		fail(w, err)
		return
	}
	status := 200
	if request.Rule.ID == 0 {
		status = 201
	}
	middleware.JSON(w, status, map[string]any{"id": ruleID})
}
func (h *Handler) DeleteRule(w http.ResponseWriter, r *http.Request) {
	result, err := h.Service.DB.ExecContext(r.Context(), `UPDATE alert_rules SET enabled=false,revision=revision+1,updated_at=now() WHERE id=$1`, id(r))
	if err != nil {
		fail(w, err)
		return
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		fail(w, sql.ErrNoRows)
		return
	}
	middleware.JSON(w, 200, map[string]bool{"disabled": true})
}
func (h *Handler) Preferences(w http.ResponseWriter, r *http.Request) {
	preference, err := h.Service.Preferences(r.Context(), middleware.UserFrom(r.Context()).ID)
	if err != nil {
		fail(w, err)
		return
	}
	middleware.JSON(w, 200, preference)
}
func (h *Handler) SavePreferences(w http.ResponseWriter, r *http.Request) {
	var request struct {
		TrafficEnabled    bool  `json:"trafficEnabled"`
		ExpirationEnabled bool  `json:"expirationEnabled"`
		ExpectedRevision  int64 `json:"expectedRevision"`
	}
	if !decode(w, r, &request) {
		return
	}
	err := h.Service.SavePreferences(r.Context(), middleware.UserFrom(r.Context()).ID, request.ExpectedRevision, mailer.Preference{TrafficEnabled: request.TrafficEnabled, ExpirationEnabled: request.ExpirationEnabled})
	if err != nil {
		fail(w, err)
		return
	}
	middleware.JSON(w, 200, map[string]any{"revision": request.ExpectedRevision + 1})
}
func (h *Handler) Mine(w http.ResponseWriter, r *http.Request)          { h.notifications(w, r, true) }
func (h *Handler) Notifications(w http.ResponseWriter, r *http.Request) { h.notifications(w, r, false) }
func (h *Handler) notifications(w http.ResponseWriter, r *http.Request, mine bool) {
	page, err := middleware.ParsePage(r)
	if err != nil {
		fail(w, mailer.ErrInvalid)
		return
	}
	userID := int64(0)
	if mine {
		userID = middleware.UserFrom(r.Context()).ID
	}
	var from, to *time.Time
	for key, target := range map[string]**time.Time{"from": &from, "to": &to} {
		if value := r.URL.Query().Get(key); value != "" {
			parsed, err := time.Parse(time.RFC3339, value)
			if err != nil {
				fail(w, mailer.ErrInvalid)
				return
			}
			*target = &parsed
		}
	}
	rows, err := h.Service.DB.QueryContext(r.Context(), `SELECT id,kind,state,event_snapshot,attempts,last_error_code,created_at,sent_at FROM notification_records WHERE ($1::bigint=0 OR user_id=$1) AND ($2='' OR state=$2) AND ($3='' OR kind=$3) AND ($4::timestamptz IS NULL OR created_at >= $4) AND ($5::timestamptz IS NULL OR created_at < $5) AND created_at>now()-interval '90 days' AND ($6::bigint=0 OR id<$6) ORDER BY id DESC LIMIT $7`, userID, r.URL.Query().Get("state"), r.URL.Query().Get("kind"), from, to, page.Cursor, page.Limit+1)
	if err != nil {
		fail(w, err)
		return
	}
	defer rows.Close()
	result := []map[string]any{}
	for rows.Next() {
		var notificationID int64
		var kind, state string
		var snapshot json.RawMessage
		var attempts int
		var errorCode *string
		var created time.Time
		var sent *time.Time
		if err = rows.Scan(&notificationID, &kind, &state, &snapshot, &attempts, &errorCode, &created, &sent); err != nil {
			fail(w, err)
			return
		}
		result = append(result, map[string]any{"id": notificationID, "kind": kind, "state": state, "snapshot": snapshot, "attempts": attempts, "errorCode": errorCode, "createdAt": created, "sentAt": sent})
	}
	if err = rows.Err(); err != nil {
		fail(w, err)
		return
	}
	middleware.JSON(w, 200, middleware.PageItems(result, page, func(item map[string]any) int64 { return item["id"].(int64) }, true))
}
func (h *Handler) Retry(w http.ResponseWriter, r *http.Request) {
	tx, err := h.Service.DB.BeginTx(r.Context(), nil)
	if err != nil {
		fail(w, err)
		return
	}
	defer tx.Rollback()
	var state string
	var bytes int
	if err = tx.QueryRowContext(r.Context(), `SELECT state,octet_length(payload_ciphertext) FROM notification_records WHERE id=$1 FOR UPDATE`, id(r)).Scan(&state, &bytes); err != nil {
		fail(w, err)
		return
	}
	valid, err := h.Service.Eligible(r.Context(), tx, id(r))
	if err != nil {
		fail(w, err)
		return
	}
	if state != "dead" || !valid || bytes == 0 {
		fail(w, mailer.ErrConflict)
		return
	}
	if _, err = tx.ExecContext(r.Context(), `UPDATE notification_records SET state='retry',next_attempt_at=now(),last_error_code=NULL WHERE id=$1`, id(r)); err != nil {
		fail(w, err)
		return
	}
	if err = tx.Commit(); err != nil {
		fail(w, err)
		return
	}
	middleware.JSON(w, 202, map[string]any{"notificationId": id(r), "state": "retry"})
}

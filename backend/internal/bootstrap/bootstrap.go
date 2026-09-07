// Package bootstrap contains the explicit first-owner provisioning flow.
package bootstrap

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"unicode"

	"github.com/coxpanel/backend/internal/auth"
	"github.com/coxpanel/backend/internal/repo"
)

var (
	ErrInvalidInput = errors.New("invalid bootstrap input")
	ErrOwnerExists  = errors.New("owner already exists")
)

// Options is supplied by the one-shot bootstrap command. Password is held
// only in memory long enough to hash it and is never logged or persisted in
// plaintext.
type Options struct {
	Username string
	Password string
	Email    string
}

// Run provisions the first owner atomically. A second caller receives
// ErrOwnerExists after the repository's transaction-level bootstrap lock.
func Run(ctx context.Context, database *sql.DB, options Options) error {
	if database == nil || !validUsername(options.Username) || !strongPassword(options.Password) || !validEmail(options.Email) {
		return ErrInvalidInput
	}
	hash, err := auth.HashPassword(options.Password)
	if err != nil {
		return err
	}
	created, err := repo.NewUserRepo(database).BootstrapOwner(ctx, options.Username, hash, options.Email)
	if err != nil {
		return err
	}
	if !created {
		return ErrOwnerExists
	}
	return nil
}

func validUsername(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 128 || strings.ContainsAny(value, "\r\n\x00") {
		return false
	}
	return true
}

func validEmail(value string) bool {
	return value == "" || (len(value) <= 320 && !strings.ContainsAny(value, "\r\n\x00"))
}

func strongPassword(value string) bool {
	if len([]rune(value)) < 12 || len([]byte(value)) > 1024 || strings.ContainsAny(value, "\r\n\x00") {
		return false
	}
	var upper, lower, digit, other bool
	for _, character := range value {
		switch {
		case unicode.IsUpper(character):
			upper = true
		case unicode.IsLower(character):
			lower = true
		case unicode.IsDigit(character):
			digit = true
		default:
			other = true
		}
	}
	return upper && lower && digit && other
}

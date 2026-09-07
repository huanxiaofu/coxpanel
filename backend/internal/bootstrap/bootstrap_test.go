package bootstrap

import (
	"context"
	"errors"
	"testing"
)

func TestRunRejectsInvalidBootstrapInputBeforeDatabaseUse(t *testing.T) {
	tests := []struct {
		name    string
		options Options
	}{
		{name: "missing username", options: Options{Password: "Strong-password-1!", Email: "owner@example.invalid"}},
		{name: "weak password", options: Options{Username: "owner", Password: "short", Email: "owner@example.invalid"}},
		{name: "missing email value is allowed", options: Options{Username: "owner", Password: "Strong-password-1!"}},
		{name: "newline username", options: Options{Username: "owner\n", Password: "Strong-password-1!", Email: "owner@example.invalid"}},
		{name: "newline email", options: Options{Username: "owner", Password: "Strong-password-1!", Email: "owner\n@example.invalid"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := Run(context.Background(), nil, test.options)
			if !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("error = %v, want ErrInvalidInput", err)
			}
		})
	}
}

func TestStrongPasswordRequiresMixedCharacterClasses(t *testing.T) {
	for _, password := range []string{
		"alllowercase-123!",
		"ALLUPPERCASE-123!",
		"No-digits-here!",
		"NoSymbols1234",
	} {
		if strongPassword(password) {
			t.Fatalf("strongPassword(%q) = true, want false", password)
		}
	}
	if !strongPassword("Strong-password-1!") {
		t.Fatal("mixed-character password was rejected")
	}
}

package main

import (
	"errors"
	"strings"
	"testing"
)

func TestBootstrapPasswordSupportsOneRuntimeSourceWithoutExposingIt(t *testing.T) {
	const secret = "Strong-password-1!"
	getenv := func(name string) string {
		if name == "COXPANEL_BOOTSTRAP_PASSWORD_FILE" {
			return "/work/test-only/bootstrap-password"
		}
		return ""
	}
	password, err := bootstrapPassword(getenv, func(path string) ([]byte, error) {
		if path != "/work/test-only/bootstrap-password" {
			t.Fatalf("unexpected password path %q", path)
		}
		return []byte(secret + "\n"), nil
	})
	if err != nil || password != secret {
		t.Fatalf("password = %q, error = %v, want runtime file value", password, err)
	}
}

func TestBootstrapPasswordRejectsAmbiguousOrMissingSource(t *testing.T) {
	for name, values := range map[string]string{
		"missing": "",
		"both":    "both",
	} {
		t.Run(name, func(t *testing.T) {
			getenv := func(key string) string {
				switch key {
				case "COXPANEL_BOOTSTRAP_PASSWORD":
					if name == "both" {
						return values
					}
				case "COXPANEL_BOOTSTRAP_PASSWORD_FILE":
					if name == "both" {
						return "/work/test-only/password"
					}
				}
				return ""
			}
			_, err := bootstrapPassword(getenv, func(string) ([]byte, error) {
				return nil, errors.New("must not read a file")
			})
			if err == nil || strings.Contains(err.Error(), values) && values != "" {
				t.Fatalf("error = %v, expected source validation without secret disclosure", err)
			}
		})
	}
}

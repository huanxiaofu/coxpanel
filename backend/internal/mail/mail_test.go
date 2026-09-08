package mail

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
	"time"
)

func TestEncryptedEnvelopeBindsPurposeAndRecord(t *testing.T) {
	service, err := New(nil, Config{}, strings.Repeat("a1", 32))
	if err != nil {
		t.Fatal(err)
	}
	encrypted, err := service.seal([]byte("synthetic-test-value"), "notification_records:1:payload")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encrypted), "synthetic-test-value") {
		t.Fatal("plaintext stored")
	}
	if _, err = service.open(encrypted, "notification_records:2:payload"); err == nil {
		t.Fatal("record substitution accepted")
	}
	if _, err = service.open(encrypted, "notification_records:1:recipient"); err == nil {
		t.Fatal("purpose substitution accepted")
	}
	plain, err := service.open(encrypted, "notification_records:1:payload")
	if err != nil || string(plain) != "synthetic-test-value" {
		t.Fatal("envelope roundtrip failed")
	}
}
func TestMailFailsClosedWithoutEncryption(t *testing.T) {
	service, err := New(nil, Config{}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.seal([]byte("synthetic"), "aad"); err != ErrUnavailable {
		t.Fatal("missing encryption accepted")
	}
	if err = service.DeliverOne(context.Background()); err != ErrUnavailable {
		t.Fatal("worker started without encryption")
	}
}
func TestThresholdArithmeticAndStableDedupe(t *testing.T) {
	if !Reached(math.MaxInt64, math.MaxInt64, math.MaxInt64, 100) {
		t.Fatal("overflow corrupted comparison")
	}
	if Reached(79, 0, 100, 80) || Reached(99, 0, 0, 80) {
		t.Fatal("incorrect threshold")
	}
	at := time.Date(2026, 9, 8, 1, 2, 3, 0, time.FixedZone("other", 3600))
	if got := EventKey("expiration", 4, 0, 0, &at, 7); got != "expiration:4:2026-09-08T00:02:03Z:7:email" {
		t.Fatal("dedupe key must use UTC")
	}
	if got := EventKey("traffic_limit", 4, 2, 100, nil, 90); got != "traffic:4:2:100:90:email" {
		t.Fatal("traffic dedupe changed")
	}
}
func TestRuleLimitsAndEmailHeaderSafety(t *testing.T) {
	rule := Rule{Name: "test", Kind: "traffic_limit", ScopeType: "all", Thresholds: []int{80, 90, 100}, Channel: "email"}
	if rule.Validate() != nil {
		t.Fatal("default rule rejected")
	}
	rule.Channel = "webhook"
	if !errors.Is(rule.Validate(), ErrChannelUnsupported) {
		t.Fatal("unsupported channel must return explicit contract error")
	}
	rule.Channel = "email"
	rule.Thresholds = []int{80, 80}
	if rule.Validate() == nil {
		t.Fatal("duplicate thresholds accepted")
	}
	if ValidEmail("person@example.test\r\nBcc: other@example.test") {
		t.Fatal("header injection accepted")
	}
}

package traffic

import (
	"math"
	"net/url"
	"testing"
	"time"
)

func TestSplitPreservesBytesAndRejectsOverflow(t *testing.T) {
	start := time.Date(2026, 9, 8, 0, 4, 59, 0, time.UTC)
	buckets, err := Split(start, start.Add(2*time.Second), math.MaxInt64, 101)
	if err != nil || len(buckets) != 2 {
		t.Fatal("expected two UTC buckets")
	}
	up, err := Add(buckets[0].Up, buckets[1].Up)
	if err != nil || up != math.MaxInt64 || buckets[0].Down+buckets[1].Down != 101 {
		t.Fatal("split lost bytes")
	}
	if _, err = Add(math.MaxInt64, 1); err == nil {
		t.Fatal("overflow accepted")
	}
}
func TestReportRejectsDuplicatesAndFutureSamples(t *testing.T) {
	now := time.Now().UTC()
	report := Report{SchemaVersion: "traffic/v2", NodeID: 1, EpochID: "11111111-1111-4111-8111-111111111111", Sequence: 1, PeriodStart: now.Add(-time.Minute), PeriodEnd: now, Samples: []Sample{{InboundID: 1, UpBytes: 2}, {InboundID: 1, UpBytes: 2}}}
	if report.Validate(1, now) == nil {
		t.Fatal("duplicate series accepted")
	}
	report.Samples = report.Samples[:1]
	report.PeriodEnd = now.Add(2 * time.Minute)
	if report.Validate(1, now) == nil {
		t.Fatal("future accepted")
	}
}
func TestQueryRetentionAndInboundGrain(t *testing.T) {
	now := time.Now().UTC()
	if _, err := ParseQuery(url.Values{"grain": {"1h"}, "groupBy": {"inbound"}}, now, true); err == nil {
		t.Fatal("inbound hourly accepted")
	}
	if _, err := ParseQuery(url.Values{"grain": {"5m"}, "from": {now.Add(-31 * 24 * time.Hour).Format(time.RFC3339)}}, now, false); err == nil {
		t.Fatal("expired grain accepted")
	}
	query, err := ParseQuery(url.Values{}, now, false)
	if err != nil || query.Grain != "5m" {
		t.Fatal("auto grain failed")
	}
}

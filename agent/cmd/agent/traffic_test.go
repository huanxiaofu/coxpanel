package main

import (
	"testing"
)

func TestTrafficStatsKeepNodeAndUserLinesSeparate(t *testing.T) {
	samples, err := statsSamples(map[string]int64{"inbound>>>in-12-shadowsocks>>>traffic>>>uplink": 90, "user>>>u-7-in-12>>>traffic>>>uplink": 10, "user>>>u-7-in-12>>>traffic>>>downlink": 20, "user>>>unmapped-name>>>traffic>>>uplink": 99})
	if err != nil || len(samples) != 2 {
		t.Fatal("stats mapping failed")
	}
	if samples[0].UserID != 0 || samples[0].UpBytes != 90 || samples[1].UserID != 7 || samples[1].DownBytes != 20 {
		t.Fatal("user traffic double counted or identity mapping lost")
	}
}
func TestTrafficProtobufRejectsTruncation(t *testing.T) {
	for _, body := range [][]byte{{10, 127}, {128}, {15}} {
		if _, err := protobufFields(body); err == nil {
			t.Fatal("invalid frame accepted")
		}
	}
	fields, err := protobufFields([]byte{10, 3, 'a', 'b', 'c', 16, 7})
	if err != nil || len(fields) != 2 || string(fields[0].Data) != "abc" || fields[1].Value != 7 {
		t.Fatal("protobuf decoding failed")
	}
}

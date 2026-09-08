package config

import (
	"testing"
)

func TestTrafficStatsRejectsNonLoopbackListener(t *testing.T) {
	for _, address := range []string{"0.0.0.0:10085", "example.test:10085", "127.0.0.1"} {
		if _, err := Render(NodeConfig{NodeID: 1, TrafficStatsListen: address}); err == nil {
			t.Fatal("unsafe stats listener accepted")
		}
	}
	rendered, err := Render(NodeConfig{NodeID: 1, TrafficStatsListen: "127.0.0.1:10085"})
	if err != nil || rendered == nil {
		t.Fatal("loopback stats listener rejected")
	}
}

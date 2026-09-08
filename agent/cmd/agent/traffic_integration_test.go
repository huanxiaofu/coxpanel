package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/coxpanel/shared/config"
)

func TestP2RealTrafficCollectorReadsCore(t *testing.T) {
	core := os.Getenv("P2_TEST_STATS_SINGBOX")
	if core == "" {
		t.Skip("P2_TEST_STATS_SINGBOX is unset")
	}
	statsPort, serverPort, clientPort := freeTCPPort(t), freeTCPPort(t), freeTCPPort(t)
	statsAddress := fmt.Sprintf("127.0.0.1:%d", statsPort)
	node := config.NodeConfig{NodeID: 41, SchemaVersion: config.SchemaVersion, TrafficStatsListen: statsAddress, Inbounds: []config.Inbound{{ID: 401, Protocol: "shadowsocks", Role: "entry", Listen: "127.0.0.1", Port: serverPort, Params: map[string]string{"method": "2022-blake3-aes-128-gcm", "password": "QUFBQUFBQUFBQUFBQUFBQQ=="}}}, Credentials: []config.UserCredential{{UserID: 7, InboundID: 401, Protocol: "shadowsocks", Password: "QkJCQkJCQkJCQkJCQkJCQg=="}}, Outbound: "direct"}
	rendered, err := config.Render(node)
	if err != nil {
		t.Fatal(err)
	}
	start := func(name string, content []byte) {
		path := filepath.Join(t.TempDir(), name+".json")
		if os.WriteFile(path, content, 0600) != nil {
			t.Fatal("fixture write failed")
		}
		command := exec.Command(core, "run", "-c", path)
		command.Stdout = io.Discard
		command.Stderr = io.Discard
		if command.Start() != nil {
			t.Fatal("fixture core start failed")
		}
		t.Cleanup(func() { command.Process.Kill(); command.Wait() })
	}
	start("server", rendered.Content)
	client := map[string]any{"inbounds": []any{map[string]any{"type": "mixed", "listen": "127.0.0.1", "listen_port": clientPort}}, "outbounds": []any{map[string]any{"type": "shadowsocks", "tag": "proxy", "server": "127.0.0.1", "server_port": serverPort, "method": "2022-blake3-aes-128-gcm", "password": "QUFBQUFBQUFBQUFBQUFBQQ==:QkJCQkJCQkJCQkJCQkJCQg=="}}, "route": map[string]any{"final": "proxy"}}
	content, _ := json.Marshal(client)
	start("client", content)
	for _, port := range []int{serverPort, clientPort, statsPort} {
		ready := false
		for attempt := 0; attempt < 80; attempt++ {
			connection, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 100*time.Millisecond)
			if err == nil {
				connection.Close()
				ready = true
				break
			}
			time.Sleep(25 * time.Millisecond)
		}
		if !ready {
			t.Fatal("test core listener unavailable")
		}
	}
	if _, _, err := readStats(statsAddress); err != nil {
		t.Fatal("real stats query failed")
	}
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("p2-known-response-payload")) }))
	defer target.Close()
	proxyURL, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", clientPort))
	transport := &http.Transport{Proxy: http.ProxyURL(proxyURL), DisableKeepAlives: true}
	defer transport.CloseIdleConnections()
	response, err := (&http.Client{Transport: transport, Timeout: 5 * time.Second}).Get(target.URL)
	if err != nil {
		t.Fatal("real stats fixture handshake failed")
	}
	_, _ = io.Copy(io.Discard, response.Body)
	response.Body.Close()
	counters, _, err := readStats(statsAddress)
	if err != nil {
		t.Fatal("real counters unavailable")
	}
	samples, err := statsSamples(counters)
	if err != nil {
		t.Fatal(err)
	}
	var userBytes, nodeBytes int64
	for _, sample := range samples {
		if sample.InboundID == 401 {
			if sample.UserID == 7 {
				userBytes += sample.UpBytes + sample.DownBytes
			} else if sample.UserID == 0 {
				nodeBytes += sample.UpBytes + sample.DownBytes
			}
		}
	}
	if userBytes == 0 || nodeBytes == 0 {
		t.Fatal("real user and node counters must both increase")
	}
}

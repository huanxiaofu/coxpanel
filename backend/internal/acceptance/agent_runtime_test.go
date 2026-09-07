package acceptance

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
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
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/coxpanel/shared/config"
	"github.com/coxpanel/shared/contract"
)

const acceptanceAgentBinaryPath = "/work/p1-agent-runtime/coxpanel-agent"

func TestPanelAgentRuntimeRevocationAndOfflineState(t *testing.T) {
	agentBinary := acceptanceAgentRuntimeBinary(t)
	harness := newPanelHarness(t)
	owner := harness.bootstrapOwner()
	groupID := harness.createGroup(owner, "p1-agent-runtime-group")
	serverPSK := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0x61}, 16))
	ss2022Port := reserveAcceptanceTCPPort(t)
	node := harness.createNode(owner, "p1-agent-runtime-node", "127.0.0.1")
	harness.createInbound(owner, node.ID, "p1-agent-runtime-ss2022", "shadowsocks", "entry", "127.0.0.1", ss2022Port, json.RawMessage(fmt.Sprintf(`{"method":"2022-blake3-aes-128-gcm","password":%q}`, serverPSK)))
	harness.setGroupNodes(owner, groupID, []int64{node.ID})
	user := harness.register("p1-agent-runtime-user", "Agent-runtime-password-6!", "p1-agent-runtime@example.invalid", harness.createInvite(owner, groupID))
	subscription := harness.createSubscription(user, groupID, "p1-agent-runtime-subscription")
	subscriptionBody := harness.subscriptionBody(subscription.Token, "")
	if panelMihomoCredential(t, subscriptionBody, "shadowsocks") == "" {
		t.Fatal("anonymous subscription omitted the panel-issued SS2022 credential")
	}

	harness.saveTopology(owner, node.ID, []map[string]int64{})
	preview := harness.previewTopology(owner, node.ID)
	harness.deployTopology(owner, node.ID, preview.Version)
	initialDocument := harness.agentConfig(node.ID, node.AgentCredential)
	if err := runAcceptanceSingboxCheck(t, initialDocument.Singbox); err != nil {
		t.Fatalf("initial panel document failed pinned sing-box check: %v", err)
	}
	assertAgentRuntimeDocumentListeners(t, initialDocument.Singbox, ss2022Port, 1)

	marker := acceptanceResponseMarker(t)
	var markerRequests atomic.Int64
	target := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/p1-ready":
			response.WriteHeader(http.StatusNoContent)
		case "/p1-marker":
			markerRequests.Add(1)
			response.WriteHeader(http.StatusOK)
			_, _ = response.Write([]byte(marker))
		default:
			http.NotFound(response, request)
		}
	}))
	t.Cleanup(target.Close)

	panelProxy := newAgentRuntimePanelProxy(t, harness.server.URL)
	configPath := filepath.Join(t.TempDir(), "config.json")
	agent := startAgentRuntimeProcess(t, agentBinary, panelProxy.URL(), node, configPath)
	initialFetched := waitForAgentRuntimeConfig(t, panelProxy, initialDocument.Version)
	initialState := waitForAgentRuntimeState(t, configPath, initialDocument.Version, initialFetched.Singbox, node.ID)
	assertAgentRuntimeDocumentListeners(t, initialState.Singbox, ss2022Port, 1)
	waitForAgentRuntimeHeartbeat(t, panelProxy, node.ID, initialDocument.Version)
	waitForAgentRuntimeNodeOnline(t, harness, owner, node.ID)

	clientConfig := acceptanceMihomoConfig(t, subscriptionBody, "shadowsocks", reserveAcceptanceTCPPort(t), nil)
	client := startAcceptanceMihomo(t, clientConfig)
	waitForAcceptanceProxyReadiness(t, client.proxy, target.URL+"/p1-ready")
	status, body, err := acceptanceProxyRequest(client.proxy, http.MethodGet, target.URL+"/p1-marker")
	if err != nil || status != http.StatusOK || string(body) != marker {
		t.Fatalf("real agent SS2022 marker exchange failed with status %d", status)
	}
	positiveMarkerRequests := markerRequests.Load()

	stopAgentRuntimeProcess(t, agent)
	waitForAcceptanceTCPClosed(t, ss2022Port)
	panelProxy.SetOffline(true)
	offlineAgent := startAgentRuntimeProcess(t, agentBinary, panelProxy.URL(), node, configPath)
	waitForAgentRuntimeState(t, configPath, initialDocument.Version, initialFetched.Singbox, node.ID)
	waitForAgentRuntimeTCPListener(t, offlineAgent, ss2022Port)
	waitForAcceptanceProxyReadiness(t, client.proxy, target.URL+"/p1-ready")
	status, body, err = acceptanceProxyRequest(client.proxy, http.MethodGet, target.URL+"/p1-marker")
	if err != nil || status != http.StatusOK || string(body) != marker {
		t.Fatalf("offline agent recovery did not preserve the prior SS2022 marker exchange, status=%d", status)
	}
	positiveMarkerRequests = markerRequests.Load()
	if positiveMarkerRequests != 2 {
		t.Fatalf("offline recovery marker count = %d, want two successful marker exchanges", positiveMarkerRequests)
	}

	panelProxy.SetOffline(false)
	waitForAgentRuntimeHeartbeat(t, panelProxy, node.ID, initialDocument.Version)
	waitForAgentRuntimeNodeOnline(t, harness, owner, node.ID)

	harness.setUserGroups(owner, user.ID, []int64{})
	harness.expectError(http.MethodGet, "/sub/"+subscription.Token, "", nil, http.StatusForbidden, "group_not_authorized")
	finalStatus, finalDocument := harness.fetchAgentConfig(node.ID, node.AgentCredential)
	if finalStatus != http.StatusOK {
		t.Fatalf("final-user revocation agent document status = %d, want %d", finalStatus, http.StatusOK)
	}
	if finalDocument.Version == initialDocument.Version || finalDocument.NodeID != node.ID || !finalDocument.Explicit {
		t.Fatal("final-user revocation did not produce a new explicit agent document")
	}
	assertAgentRuntimeDocumentListeners(t, finalDocument.Singbox, ss2022Port, 0)
	if err := runAcceptanceSingboxCheck(t, finalDocument.Singbox); err != nil {
		t.Fatalf("final fetched deny-all document failed pinned sing-box check: %v", err)
	}
	finalFetched := waitForAgentRuntimeConfig(t, panelProxy, finalDocument.Version)
	waitForAgentRuntimeHeartbeat(t, panelProxy, node.ID, finalDocument.Version)
	waitForAgentRuntimeNodeOnline(t, harness, owner, node.ID)
	finalState := waitForAgentRuntimeState(t, configPath, finalDocument.Version, finalFetched.Singbox, node.ID)
	assertAgentRuntimeDocumentListeners(t, finalState.Singbox, ss2022Port, 0)
	waitForAcceptanceTCPClosed(t, ss2022Port)
	assertAgentRuntimeOldClientDenied(t, client, target.URL+"/p1-marker", marker, &markerRequests, positiveMarkerRequests)

	stopAgentRuntimeProcess(t, offlineAgent)
	waitForAcceptanceTCPClosed(t, ss2022Port)
	panelProxy.SetOffline(true)
	harness.server.CloseClientConnections()
	harness.server.Close()
	finalOfflineAgent := startAgentRuntimeProcess(t, agentBinary, panelProxy.URL(), node, configPath)
	waitForAgentRuntimeState(t, configPath, finalDocument.Version, finalFetched.Singbox, node.ID)
	waitForAgentRuntimeProcessAlive(t, finalOfflineAgent)
	assertAgentRuntimeDocumentListeners(t, finalFetched.Singbox, ss2022Port, 0)
	assertAgentRuntimeOldClientDenied(t, client, target.URL+"/p1-marker", marker, &markerRequests, positiveMarkerRequests)
	stopAgentRuntimeProcess(t, finalOfflineAgent)
}

type agentRuntimeState struct {
	SchemaVersion string `json:"schemaVersion"`
	Version       string `json:"version"`
	SHA256        string `json:"sha256"`
	NodeID        int64  `json:"nodeId"`
	Singbox       []byte `json:"singbox"`
}

func acceptanceAgentRuntimeBinary(t *testing.T) string {
	t.Helper()
	path := os.Getenv("P1_TEST_AGENT_BINARY")
	if path == "" {
		path = acceptanceAgentBinaryPath
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&0111 == 0 {
		t.Skip("built real agent executable is unavailable; set P1_TEST_AGENT_BINARY")
	}
	return path
}

type agentRuntimeProcess struct {
	command *exec.Cmd
	done    chan error
	once    sync.Once
}

func startAgentRuntimeProcess(t *testing.T, binary, panelURL string, node idResponse, configPath string) *agentRuntimeProcess {
	t.Helper()
	command := exec.Command(binary, "-panel", panelURL, "-node", strconv.FormatInt(node.ID, 10), "-config", configPath, "-singbox", acceptanceSingboxPath, "-interval", "100ms")
	command.Env = acceptanceAgentRuntimeEnvironment(node.AgentCredential)
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	if err := command.Start(); err != nil {
		t.Fatalf("start compiled agent: %v", err)
	}
	process := &agentRuntimeProcess{command: command, done: make(chan error, 1)}
	go func() { process.done <- command.Wait() }()
	t.Cleanup(func() { stopAgentRuntimeProcess(t, process) })
	return process
}

func acceptanceAgentRuntimeEnvironment(credential string) []string {
	environment := make([]string, 0, len(os.Environ())+1)
	for _, value := range os.Environ() {
		if strings.HasPrefix(value, "COXPANEL_AGENT_CREDENTIAL=") {
			continue
		}
		environment = append(environment, value)
	}
	return append(environment, "COXPANEL_AGENT_CREDENTIAL="+credential)
}

func stopAgentRuntimeProcess(t *testing.T, process *agentRuntimeProcess) {
	t.Helper()
	if process == nil || process.command == nil {
		return
	}
	process.once.Do(func() {
		if process.command.Process != nil {
			_ = process.command.Process.Signal(syscall.SIGTERM)
		}
		select {
		case <-process.done:
		case <-time.After(5 * time.Second):
			if process.command.Process != nil {
				_ = process.command.Process.Kill()
			}
			select {
			case <-process.done:
			case <-time.After(2 * time.Second):
				t.Error("compiled agent did not stop")
			}
		}
	})
}

func agentRuntimeProcessAlive(process *agentRuntimeProcess) bool {
	if process == nil {
		return false
	}
	select {
	case <-process.done:
		return false
	default:
		return process.command != nil && process.command.Process != nil
	}
}

func waitForAgentRuntimeProcessAlive(t *testing.T, process *agentRuntimeProcess) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if agentRuntimeProcessAlive(process) {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("compiled agent exited during offline recovery")
}

func waitForAgentRuntimeTCPListener(t *testing.T, process *agentRuntimeProcess, port int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	address := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	var lastError error
	for time.Now().Before(deadline) {
		if !agentRuntimeProcessAlive(process) {
			t.Fatalf("compiled agent exited before recovering listener %s", address)
		}
		connection, err := net.DialTimeout("tcp", address, 100*time.Millisecond)
		if err == nil {
			_ = connection.Close()
			return
		}
		lastError = err
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("compiled agent did not recover listener %s: %v", address, lastError)
}

func waitForAgentRuntimeState(t *testing.T, configPath, expectedVersion string, expectedSingbox []byte, nodeID int64) agentRuntimeState {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	statePath := configPath + ".state"
	for time.Now().Before(deadline) {
		contents, err := os.ReadFile(statePath)
		if err == nil {
			var state agentRuntimeState
			if json.Unmarshal(contents, &state) == nil && state.SchemaVersion == config.SchemaVersion && state.NodeID == nodeID && state.Version == expectedVersion && state.SHA256 == expectedVersion && bytes.Equal(state.Singbox, expectedSingbox) {
				info, statErr := os.Stat(statePath)
				if statErr != nil || info.Mode().Perm() != 0600 {
					t.Fatalf("agent durable state mode = %s, want 0600", info.Mode().Perm())
				}
				digest := sha256.Sum256(state.Singbox)
				if hex.EncodeToString(digest[:]) != expectedVersion || config.ValidateRendered(state.Singbox) != nil {
					t.Fatal("agent durable state hash or rendered document validation failed")
				}
				return state
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("agent durable state did not reach expected version")
	return agentRuntimeState{}
}

func assertAgentRuntimeDocumentListeners(t *testing.T, content []byte, expectedPort, expectedCount int) {
	t.Helper()
	var document struct {
		Inbounds []struct {
			Type       string `json:"type"`
			Listen     string `json:"listen"`
			ListenPort int    `json:"listen_port"`
		} `json:"inbounds"`
	}
	if err := json.Unmarshal(content, &document); err != nil {
		t.Fatalf("agent durable sing-box document is not JSON: %v", err)
	}
	if len(document.Inbounds) != expectedCount {
		t.Fatalf("agent durable listener count = %d, want %d", len(document.Inbounds), expectedCount)
	}
	if expectedCount == 1 && (document.Inbounds[0].Type != "shadowsocks" || document.Inbounds[0].Listen != "127.0.0.1" || document.Inbounds[0].ListenPort != expectedPort) {
		t.Fatalf("agent durable SS2022 listener did not retain the fetched panel port or bind")
	}
}

func assertAgentRuntimeOldClientDenied(t *testing.T, client *acceptanceMihomoProcess, requestURL, marker string, markerRequests *atomic.Int64, expectedMarkerRequests int64) {
	t.Helper()
	status, body, err := acceptanceProxyRequest(client.proxy, http.MethodGet, requestURL)
	if err == nil && status == http.StatusOK && string(body) == marker {
		t.Fatal("revoked SS2022 client still reached the target marker")
	}
	if markerRequests.Load() != expectedMarkerRequests {
		t.Fatalf("revoked SS2022 request changed target marker count to %d, want %d", markerRequests.Load(), expectedMarkerRequests)
	}
}

type agentRuntimeHeartbeatObservation struct {
	heartbeat      contract.Heartbeat
	authenticated  bool
	responseStatus int
}

type agentRuntimeConfigObservation struct {
	document agentConfigResponse
	status   int
}

type agentRuntimePanelProxy struct {
	targetURL  *url.URL
	client     *http.Client
	server     *httptest.Server
	offline    atomic.Bool
	heartbeats chan agentRuntimeHeartbeatObservation
	configs    chan agentRuntimeConfigObservation
}

func newAgentRuntimePanelProxy(t *testing.T, target string) *agentRuntimePanelProxy {
	t.Helper()
	parsed, err := url.Parse(target)
	if err != nil {
		t.Fatalf("parse panel URL: %v", err)
	}
	proxy := &agentRuntimePanelProxy{
		targetURL:  parsed,
		client:     &http.Client{Timeout: 5 * time.Second},
		heartbeats: make(chan agentRuntimeHeartbeatObservation, 32),
		configs:    make(chan agentRuntimeConfigObservation, 32),
	}
	proxy.server = httptest.NewServer(http.HandlerFunc(proxy.serveHTTP))
	t.Cleanup(proxy.server.Close)
	return proxy
}

func (proxy *agentRuntimePanelProxy) URL() string { return proxy.server.URL }

func (proxy *agentRuntimePanelProxy) SetOffline(offline bool) { proxy.offline.Store(offline) }

func (proxy *agentRuntimePanelProxy) serveHTTP(response http.ResponseWriter, request *http.Request) {
	if proxy.offline.Load() {
		http.Error(response, "panel unavailable", http.StatusServiceUnavailable)
		return
	}

	isHeartbeat := request.URL.Path == "/api/agent/heartbeat"
	var requestBody []byte
	var heartbeat contract.Heartbeat
	if isHeartbeat {
		requestBody, _ = io.ReadAll(io.LimitReader(request.Body, 1<<20))
		_ = request.Body.Close()
		request.Body = io.NopCloser(bytes.NewReader(requestBody))
		_ = json.Unmarshal(requestBody, &heartbeat)
	}

	destination := *proxy.targetURL
	destination.Path = request.URL.Path
	destination.RawPath = request.URL.RawPath
	destination.RawQuery = request.URL.RawQuery
	forwarded, err := http.NewRequestWithContext(request.Context(), request.Method, destination.String(), request.Body)
	if err != nil {
		http.Error(response, "panel forwarding unavailable", http.StatusBadGateway)
		return
	}
	forwarded.Header = request.Header.Clone()
	forwarded.Host = proxy.targetURL.Host
	if isHeartbeat {
		forwarded.ContentLength = int64(len(requestBody))
	}
	upstream, err := proxy.client.Do(forwarded)
	if err != nil {
		http.Error(response, "panel forwarding unavailable", http.StatusBadGateway)
		return
	}
	defer upstream.Body.Close()
	body, err := io.ReadAll(io.LimitReader(upstream.Body, 8<<20))
	if err != nil {
		http.Error(response, "panel response unavailable", http.StatusBadGateway)
		return
	}
	for key, values := range upstream.Header {
		for _, value := range values {
			response.Header().Add(key, value)
		}
	}
	response.WriteHeader(upstream.StatusCode)
	_, _ = response.Write(body)

	if isHeartbeat {
		observation := agentRuntimeHeartbeatObservation{
			heartbeat:      heartbeat,
			authenticated:  strings.TrimSpace(request.Header.Get(contract.AgentCredentialHeader)) != "",
			responseStatus: upstream.StatusCode,
		}
		select {
		case proxy.heartbeats <- observation:
		default:
		}
	}
	if request.URL.Path == "/api/agent/config" && upstream.StatusCode == http.StatusOK {
		var document agentConfigResponse
		if json.Unmarshal(body, &document) == nil {
			select {
			case proxy.configs <- agentRuntimeConfigObservation{document: document, status: upstream.StatusCode}:
			default:
			}
		}
	}
}

func waitForAgentRuntimeConfig(t *testing.T, proxy *agentRuntimePanelProxy, expectedVersion string) agentConfigResponse {
	t.Helper()
	deadline := time.NewTimer(15 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case observation := <-proxy.configs:
			if observation.status == http.StatusOK && observation.document.Version == expectedVersion {
				return observation.document
			}
		case <-deadline.C:
			t.Fatalf("compiled agent did not fetch expected config version")
			return agentConfigResponse{}
		}
	}
}

func waitForAgentRuntimeHeartbeat(t *testing.T, proxy *agentRuntimePanelProxy, nodeID int64, expectedVersion string) contract.Heartbeat {
	t.Helper()
	deadline := time.NewTimer(15 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case observation := <-proxy.heartbeats:
			if observation.authenticated && observation.responseStatus == http.StatusOK && observation.heartbeat.NodeID == nodeID && observation.heartbeat.Version == expectedVersion {
				return observation.heartbeat
			}
		case <-deadline.C:
			t.Fatalf("compiled agent did not produce an authenticated heartbeat for expected version")
			return contract.Heartbeat{}
		}
	}
}

func waitForAgentRuntimeNodeOnline(t *testing.T, harness *panelHarness, owner principal, nodeID int64) {
	t.Helper()
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	for {
		var node struct {
			ID         int64      `json:"id"`
			Status     string     `json:"status"`
			LastSeenAt *time.Time `json:"lastSeenAt"`
		}
		body, status := harness.doRawStatus(http.MethodGet, fmt.Sprintf("/api/nodes/%d/", nodeID), owner.Token, nil)
		if status == http.StatusOK && json.Unmarshal(body, &node) == nil && node.ID == nodeID && node.Status == "online" && node.LastSeenAt != nil {
			return
		}
		select {
		case <-deadline.C:
			t.Fatalf("authenticated agent heartbeat did not mark node online")
		default:
			time.Sleep(50 * time.Millisecond)
		}
	}
}

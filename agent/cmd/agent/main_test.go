package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/coxpanel/shared/config"
	"github.com/coxpanel/shared/contract"
)

func TestFetchConfigUsesAgentHeadersAndDoesNotSendBearer(t *testing.T) {
	content := renderedTestContent(t, 7)
	payload := configDocumentJSON(t, 7, content, true)
	var gotNodeID, gotCredential, gotAuthorization string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotNodeID = r.Header.Get(contract.AgentNodeIDHeader)
		gotCredential = r.Header.Get(contract.AgentCredentialHeader)
		gotAuthorization = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(payload)
	}))
	defer server.Close()

	oldPanel, oldNode, oldKey := *panelURL, *nodeID, *apiKey
	defer func() { *panelURL, *nodeID, *apiKey = oldPanel, oldNode, oldKey }()
	*panelURL, *nodeID, *apiKey = server.URL, 7, "synthetic-agent-secret"

	if _, err := fetchConfig(); err != nil {
		t.Fatalf("fetchConfig() error = %v", err)
	}
	if gotNodeID != "7" || gotCredential != "synthetic-agent-secret" {
		t.Fatalf("agent headers = node %q credential %q", gotNodeID, gotCredential)
	}
	if gotAuthorization != "" {
		t.Fatalf("Authorization header = %q, want absent", gotAuthorization)
	}
}

func TestApplyConfigRequiresExplicitDocumentAndCore(t *testing.T) {
	content := renderedTestContent(t, 8)
	oldNode, oldCore, oldPath, oldApplied := *nodeID, *sbPath, *cfgPath, lastApplied
	defer func() { *nodeID, *sbPath, *cfgPath, lastApplied = oldNode, oldCore, oldPath, oldApplied }()
	*nodeID = 8
	lastApplied = ""

	tests := []struct {
		name     string
		explicit bool
		corePath func(string) string
		check    string
		wantErr  string
	}{
		{
			name:     "requires explicit confirmation",
			explicit: false,
			corePath: func(dir string) string { return helperCore(t, dir) },
			check:    "ok",
			wantErr:  "config validation failed",
		},
		{
			name:     "requires core binary",
			explicit: true,
			corePath: func(dir string) string { return filepath.Join(dir, "missing-sing-box") },
			check:    "ok",
			wantErr:  "sing-box core unavailable",
		},
		{
			name:     "rejects core check failure",
			explicit: true,
			corePath: func(dir string) string { return helperCore(t, dir) },
			check:    "fail",
			wantErr:  "sing-box check failed",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			testDir := t.TempDir()
			*sbPath = test.corePath(testDir)
			*cfgPath = filepath.Join(testDir, "config.json")
			t.Setenv("COXPANEL_HELPER_CHECK", test.check)
			document := decodeSharedConfig(t, configDocumentJSON(t, 8, content, test.explicit))

			err := applyConfig(&document)
			if err == nil || err.Error() != test.wantErr {
				t.Fatalf("applyConfig() error = %v, want %q", err, test.wantErr)
			}
			if _, err := os.Stat(*cfgPath); !os.IsNotExist(err) {
				t.Fatalf("config file exists after rejected apply: stat error = %v", err)
			}
		})
	}
}

func TestApplyConfigPersistsVersionOnlyAfterCoreReadiness(t *testing.T) {
	content := renderedTestContent(t, 11)
	document := decodeSharedConfig(t, configDocumentJSON(t, 11, content, true))
	testDir := t.TempDir()
	events := filepath.Join(testDir, "events")
	core := helperCore(t, testDir)
	oldNode, oldCore, oldPath, oldApplied := *nodeID, *sbPath, *cfgPath, lastApplied
	defer func() {
		stopOwnedCore()
		*nodeID, *sbPath, *cfgPath, lastApplied = oldNode, oldCore, oldPath, oldApplied
	}()
	*nodeID, *sbPath, *cfgPath = 11, core, filepath.Join(testDir, "config.json")
	useSyntheticReadiness(t)
	lastApplied = ""
	t.Setenv("COXPANEL_HELPER_EVENTS", events)
	t.Setenv("COXPANEL_HELPER_CHECK", "ok")
	t.Setenv("COXPANEL_HELPER_RUN", "stay")
	t.Setenv("COXPANEL_HELPER_ACTIVE_CONFIG", *cfgPath)
	t.Setenv("COXPANEL_HELPER_VERSION_PATH", versionPath())
	t.Setenv("COXPANEL_HELPER_ASSERT_NO_VERSION", "1")

	if err := applyConfig(&document); err != nil {
		t.Fatalf("applyConfig() error = %v", err)
	}
	eventText, err := os.ReadFile(events)
	if err != nil {
		t.Fatalf("read helper events: %v", err)
	}
	if strings.Contains(string(eventText), "VERSION-PRESENT") {
		t.Fatalf("version sidecar was visible before core readiness: %q", eventText)
	}
	if lastApplied != document.Version {
		t.Fatalf("lastApplied = %q, want %q", lastApplied, document.Version)
	}
}

func TestReadLastGoodRequiresVersionAndNodeBinding(t *testing.T) {
	content := renderedTestContent(t, 12)
	testDir := t.TempDir()
	oldNode, oldPath := *nodeID, *cfgPath
	defer func() { *nodeID, *cfgPath = oldNode, oldPath }()
	*nodeID, *cfgPath = 12, filepath.Join(testDir, "config.json")
	if err := os.WriteFile(*cfgPath, content, 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.WriteFile(versionPath(), []byte(config.Hash(content)+"\n"), 0600); err != nil {
		t.Fatalf("write version sidecar: %v", err)
	}

	if _, _, err := readLastGood(); err == nil {
		t.Fatalf("readLastGood() error = nil without node sidecar")
	}
	if err := os.WriteFile(nodePath(), []byte("13\n"), 0600); err != nil {
		t.Fatalf("write mismatched node sidecar: %v", err)
	}
	if _, _, err := readLastGood(); err == nil {
		t.Fatalf("readLastGood() error = nil for mismatched node sidecar")
	}
}

func TestReadLastGoodSurvivesInterruptedCanonicalConfigRename(t *testing.T) {
	oldContent := renderedTestContent(t, 13)
	newContent := renderedTestContentWithNodeName(t, 13, "interrupted")
	testDir := t.TempDir()
	oldNode, oldPath := *nodeID, *cfgPath
	defer func() { *nodeID, *cfgPath = oldNode, oldPath }()
	*nodeID, *cfgPath = 13, filepath.Join(testDir, "config.json")
	oldVersion := config.Hash(oldContent)
	if err := writeDurableState(oldContent, oldVersion); err != nil {
		t.Fatalf("write durable state: %v", err)
	}
	if err := atomicWrite(*cfgPath, oldContent, 0600); err != nil {
		t.Fatalf("write canonical config: %v", err)
	}
	if err := atomicWrite(versionPath(), []byte(oldVersion+"\n"), 0600); err != nil {
		t.Fatalf("write version sidecar: %v", err)
	}
	if err := atomicWrite(nodePath(), []byte("13\n"), 0600); err != nil {
		t.Fatalf("write node sidecar: %v", err)
	}

	if err := atomicWrite(*cfgPath, newContent, 0600); err != nil {
		t.Fatalf("simulate interrupted config rename: %v", err)
	}
	got, version, err := readLastGood()
	if err != nil {
		t.Fatalf("readLastGood() after interrupted rename: %v", err)
	}
	if string(got) != string(oldContent) || version != oldVersion {
		t.Fatalf("recovered state = %q/%q, want old state %q/%q", got, version, oldContent, oldVersion)
	}

	wrongNodeState := durableConfigState{
		SchemaVersion: config.SchemaVersion,
		Version:       oldVersion,
		SHA256:        oldVersion,
		NodeID:        99,
		Singbox:       oldContent,
	}
	encoded, err := json.Marshal(wrongNodeState)
	if err != nil {
		t.Fatalf("marshal wrong-node state: %v", err)
	}
	if err := atomicWrite(durableStatePath(), encoded, 0600); err != nil {
		t.Fatalf("write wrong-node state: %v", err)
	}
	if _, _, err := readLastGood(); err == nil {
		t.Fatal("readLastGood() error = nil for wrong-node durable state")
	}
}

func TestApplyConfigRollsBackAfterOwnedCoreStartupFailure(t *testing.T) {
	content := renderedTestContent(t, 9)
	oldDocument := decodeSharedConfig(t, configDocumentJSON(t, 9, content, true))
	newContent := renderedTestContentWithSS2022(t, 9)
	if bytes.Equal(content, newContent) {
		t.Fatal("rollback fixtures have identical rendered content")
	}
	newDocument := decodeSharedConfig(t, configDocumentJSON(t, 9, newContent, true))
	if oldDocument.Version == newDocument.Version {
		t.Fatalf("rollback fixture versions are identical: %q", oldDocument.Version)
	}
	testDir := t.TempDir()
	events := filepath.Join(testDir, "events")
	core := helperCore(t, testDir)
	oldNode, oldCore, oldPath, oldApplied := *nodeID, *sbPath, *cfgPath, lastApplied
	defer func() {
		stopOwnedCore()
		*nodeID, *sbPath, *cfgPath, lastApplied = oldNode, oldCore, oldPath, oldApplied
	}()
	*nodeID, *sbPath, *cfgPath = 9, core, filepath.Join(testDir, "config.json")
	useSyntheticReadiness(t)
	lastApplied = ""
	t.Setenv("COXPANEL_HELPER_EVENTS", events)
	t.Setenv("COXPANEL_HELPER_CHECK", "ok")
	t.Setenv("COXPANEL_HELPER_RUN", "stay")

	if err := applyConfig(&oldDocument); err != nil {
		t.Fatalf("initial applyConfig() error = %v", err)
	}
	lastApplied = oldDocument.Version
	t.Setenv("COXPANEL_HELPER_RUN", "early-exit-once")
	if err := applyConfig(&newDocument); err == nil {
		t.Fatal("failed startup applyConfig() error = nil")
	}
	got, err := os.ReadFile(*cfgPath)
	if err != nil {
		t.Fatalf("read restored config: %v", err)
	}
	if string(got) != string(content) {
		t.Fatalf("restored config = %s, want prior rendered content", got)
	}
	if lastApplied != oldDocument.Version {
		t.Fatalf("lastApplied = %q, want %q", lastApplied, oldDocument.Version)
	}
	if !ownedCoreRunning() {
		t.Fatal("owned core is not running after rollback")
	}
	versionBytes, err := os.ReadFile(versionPath())
	if err != nil {
		t.Fatalf("read restored version sidecar: %v", err)
	}
	if strings.TrimSpace(string(versionBytes)) != oldDocument.Version {
		t.Fatalf("restored version sidecar = %q, want %q", strings.TrimSpace(string(versionBytes)), oldDocument.Version)
	}
	eventText, err := os.ReadFile(events)
	if err != nil {
		t.Fatalf("read helper events: %v", err)
	}
	if !strings.Contains(string(eventText), "TERM") || strings.Count(string(eventText), "START") < 3 {
		t.Fatalf("helper lifecycle events = %q, want stop and rollback restart", eventText)
	}
}

func TestApplyConfigRollsBackWhenCoreExitsAfterInitialStartupObservation(t *testing.T) {
	oldContent := renderedTestContent(t, 14)
	oldDocument := decodeSharedConfig(t, configDocumentJSON(t, 14, oldContent, true))
	newContent := renderedTestContentWithNodeName(t, 14, "replacement")
	newDocument := decodeSharedConfig(t, configDocumentJSON(t, 14, newContent, true))
	testDir := t.TempDir()
	events := filepath.Join(testDir, "events")
	core := helperCore(t, testDir)
	oldNode, oldCore, oldPath, oldApplied := *nodeID, *sbPath, *cfgPath, lastApplied
	defer func() {
		stopOwnedCore()
		*nodeID, *sbPath, *cfgPath, lastApplied = oldNode, oldCore, oldPath, oldApplied
	}()
	*nodeID, *sbPath, *cfgPath = 14, core, filepath.Join(testDir, "config.json")
	useSyntheticReadiness(t)
	lastApplied = ""
	t.Setenv("COXPANEL_HELPER_EVENTS", events)
	t.Setenv("COXPANEL_HELPER_CHECK", "ok")
	t.Setenv("COXPANEL_HELPER_RUN", "stay")
	if err := applyConfig(&oldDocument); err != nil {
		t.Fatalf("initial applyConfig() error = %v", err)
	}
	t.Setenv("COXPANEL_HELPER_RUN", "delayed-exit-once")
	if err := applyConfig(&newDocument); err == nil {
		t.Fatal("delayed core exit applyConfig() error = nil")
	}
	got, err := os.ReadFile(*cfgPath)
	if err != nil {
		t.Fatalf("read restored config: %v", err)
	}
	if string(got) != string(oldContent) {
		t.Fatalf("restored config = %s, want prior rendered content", got)
	}
	if lastApplied != oldDocument.Version {
		t.Fatalf("lastApplied = %q, want %q", lastApplied, oldDocument.Version)
	}
	if !ownedCoreRunning() {
		t.Fatal("owned core is not running after delayed-exit rollback")
	}
	versionBytes, err := os.ReadFile(versionPath())
	if err != nil {
		t.Fatalf("read restored version sidecar: %v", err)
	}
	if strings.TrimSpace(string(versionBytes)) != oldDocument.Version {
		t.Fatalf("restored version sidecar = %q, want %q", strings.TrimSpace(string(versionBytes)), oldDocument.Version)
	}
}

func TestApplyConfigRejectsRunningButUnreadyCore(t *testing.T) {
	content := renderedTestContentWithSS2022Port(t, 15, freeTCPPort(t))
	document := decodeSharedConfig(t, configDocumentJSON(t, 15, content, true))
	testDir := t.TempDir()
	events := filepath.Join(testDir, "events")
	core := helperCore(t, testDir)
	oldNode, oldCore, oldPath, oldApplied := *nodeID, *sbPath, *cfgPath, lastApplied
	defer func() {
		stopOwnedCore()
		*nodeID, *sbPath, *cfgPath, lastApplied = oldNode, oldCore, oldPath, oldApplied
	}()
	*nodeID, *sbPath, *cfgPath = 15, core, filepath.Join(testDir, "config.json")
	lastApplied = ""
	t.Setenv("COXPANEL_HELPER_EVENTS", events)
	t.Setenv("COXPANEL_HELPER_CHECK", "ok")
	t.Setenv("COXPANEL_HELPER_RUN", "unready")

	if err := applyConfig(&document); err == nil {
		t.Fatal("applyConfig() error = nil for running but unready core")
	}
	eventText, err := os.ReadFile(events)
	if err != nil {
		t.Fatalf("read helper events: %v", err)
	}
	if !strings.Contains(string(eventText), "START") {
		t.Fatalf("helper events = %q, want started unready process", eventText)
	}
	for _, path := range []string{*cfgPath, durableStatePath(), versionPath(), nodePath()} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("durable path %s exists after readiness rejection: stat error = %v", path, err)
		}
	}
	if ownedCoreRunning() {
		t.Fatal("unready core remains owned after rejection")
	}
}

func TestStartOwnedCoreWithRealSingBox(t *testing.T) {
	core := os.Getenv("COXPANEL_REAL_SINGBOX")
	if core == "" {
		core = "/workspace/tmp/sing-box-audit/sing-box-1.13.21-linux-amd64/sing-box"
	}
	if _, err := os.Stat(core); err != nil {
		t.Skipf("real sing-box unavailable: %v", err)
	}
	port := freeTCPPort(t)
	rendered, err := config.Render(config.NodeConfig{
		SchemaVersion: config.SchemaVersion,
		NodeID:        17,
		NodeName:      "real-core",
		Inbounds: []config.Inbound{{
			ID:       1,
			Protocol: "shadowsocks",
			Role:     "relay",
			Listen:   "127.0.0.1",
			Port:     port,
			Params: map[string]string{
				"method":   "2022-blake3-aes-128-gcm",
				"password": "AAAAAAAAAAAAAAAAAAAAAA==",
			},
		}},
		Edges:    []config.Edge{},
		Outbound: "direct",
	})
	if err != nil {
		t.Fatalf("render real-core fixture: %v", err)
	}
	testDir := t.TempDir()
	configPath := filepath.Join(testDir, "config.json")
	if err := os.WriteFile(configPath, rendered.Content, 0600); err != nil {
		t.Fatalf("write real-core config: %v", err)
	}
	oldCore, oldPath, oldProbe := *sbPath, *cfgPath, readinessProbe
	defer func() { *sbPath, *cfgPath, readinessProbe = oldCore, oldPath, oldProbe }()
	*sbPath, *cfgPath, readinessProbe = core, configPath, defaultCoreReadinessProbe

	if err := checkCoreBytes(rendered.Content); err != nil {
		t.Fatalf("real sing-box check: %v", err)
	}
	process, err := startOwnedCore(configPath)
	if err != nil {
		t.Fatalf("real sing-box readiness: %v", err)
	}
	defer func() {
		if err := stopCore(process); err != nil {
			t.Errorf("stop real sing-box: %v", err)
		}
	}()
	if !coreListenersReady([]coreListener{{network: "tcp", addresses: []string{net.JoinHostPort("127.0.0.1", fmt.Sprintf("%d", port))}}}) {
		t.Fatal("real sing-box listener is not ready after startOwnedCore")
	}
}

func TestStartOwnedCoreAcceptsExplicitNoListenerConfiguration(t *testing.T) {
	testDir := t.TempDir()
	content := renderedTestContent(t, 20)
	configPath := filepath.Join(testDir, "config.json")
	if err := os.WriteFile(configPath, content, 0600); err != nil {
		t.Fatalf("write no-listener config: %v", err)
	}
	core := helperCore(t, testDir)
	oldCore, oldPath, oldProbe := *sbPath, *cfgPath, readinessProbe
	defer func() { *sbPath, *cfgPath, readinessProbe = oldCore, oldPath, oldProbe }()
	*sbPath, *cfgPath, readinessProbe = core, configPath, defaultCoreReadinessProbe
	t.Setenv("COXPANEL_HELPER_CHECK", "ok")
	t.Setenv("COXPANEL_HELPER_RUN", "stay")

	process, err := startOwnedCore(configPath)
	if err != nil {
		t.Fatalf("startOwnedCore() no-listener error = %v", err)
	}
	if err := adoptOwnedCore(process); err != nil {
		t.Fatalf("adoptOwnedCore() no-listener error = %v", err)
	}
	defer func() { _ = stopCore(process) }()
	if !ownedCoreRunning() {
		t.Fatal("no-listener core was not running after stability readiness")
	}
}

func TestRunRestartsLastGoodConfigWhenPanelIsOffline(t *testing.T) {
	content := renderedTestContent(t, 10)
	document := decodeSharedConfig(t, configDocumentJSON(t, 10, content, true))
	testDir := t.TempDir()
	events := filepath.Join(testDir, "events")
	core := helperCore(t, testDir)
	oldPanel, oldNode, oldCore, oldPath, oldApplied := *panelURL, *nodeID, *sbPath, *cfgPath, lastApplied
	defer func() {
		stopOwnedCore()
		*panelURL, *nodeID, *sbPath, *cfgPath, lastApplied = oldPanel, oldNode, oldCore, oldPath, oldApplied
	}()
	*panelURL, *nodeID, *sbPath, *cfgPath = "http://127.0.0.1:1", 10, core, filepath.Join(testDir, "config.json")
	useSyntheticReadiness(t)
	lastApplied = ""
	t.Setenv("COXPANEL_HELPER_EVENTS", events)
	t.Setenv("COXPANEL_HELPER_CHECK", "ok")
	t.Setenv("COXPANEL_HELPER_RUN", "stay")
	if err := applyConfig(&document); err != nil {
		t.Fatalf("initial applyConfig() error = %v", err)
	}
	stopOwnedCore()
	run()
	eventText, err := os.ReadFile(events)
	if err != nil {
		t.Fatalf("read helper events: %v", err)
	}
	if strings.Count(string(eventText), "START") < 2 {
		t.Fatalf("helper starts = %q, want offline recovery start", eventText)
	}
	if !ownedCoreRunning() {
		t.Fatal("owned core is not running after offline recovery")
	}
	versionBytes, err := os.ReadFile(versionPath())
	if err != nil {
		t.Fatalf("read offline recovery version sidecar: %v", err)
	}
	if strings.TrimSpace(string(versionBytes)) != document.Version {
		t.Fatalf("offline recovery version sidecar = %q, want %q", strings.TrimSpace(string(versionBytes)), document.Version)
	}
}

func TestRunDoesNotAdvertiseStoredVersionWhenRecoveryFails(t *testing.T) {
	content := renderedTestContent(t, 16)
	document := decodeSharedConfig(t, configDocumentJSON(t, 16, content, true))
	testDir := t.TempDir()
	core := helperCore(t, testDir)
	var heartbeat contract.Heartbeat
	var heartbeatReceived bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		if r.Method == http.MethodPost {
			if err := json.NewDecoder(r.Body).Decode(&heartbeat); err != nil {
				t.Errorf("decode heartbeat: %v", err)
			}
			heartbeatReceived = true
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()
	oldPanel, oldNode, oldCore, oldPath, oldApplied := *panelURL, *nodeID, *sbPath, *cfgPath, lastApplied
	defer func() {
		stopOwnedCore()
		*panelURL, *nodeID, *sbPath, *cfgPath, lastApplied = oldPanel, oldNode, oldCore, oldPath, oldApplied
	}()
	*panelURL, *nodeID, *sbPath, *cfgPath = server.URL, 16, core, filepath.Join(testDir, "config.json")
	if err := writeDurableState(content, document.Version); err != nil {
		t.Fatalf("write durable state: %v", err)
	}
	if err := atomicWrite(*cfgPath, content, 0600); err != nil {
		t.Fatalf("write canonical config: %v", err)
	}
	lastApplied = loadLastGoodVersion()
	if lastApplied != document.Version {
		t.Fatalf("loaded lastApplied = %q, want %q", lastApplied, document.Version)
	}
	t.Setenv("COXPANEL_HELPER_CHECK", "ok")
	t.Setenv("COXPANEL_HELPER_RUN", "early-exit")

	run()
	if !heartbeatReceived {
		t.Fatal("heartbeat was not sent")
	}
	if heartbeat.Version != "" {
		t.Fatalf("heartbeat version = %q, want empty without running core", heartbeat.Version)
	}
	if ownedCoreRunning() {
		t.Fatal("failed recovery core remains owned")
	}
}

func TestHelperProcess(t *testing.T) {
	if !hasArg("check") && !hasArg("run") {
		return
	}
	appendHelperEvent("START-" + strings.Join(os.Args, " "))
	if hasArg("check") {
		if os.Getenv("COXPANEL_HELPER_CHECK") == "fail" {
			os.Exit(19)
		}
		return
	}
	if os.Getenv("COXPANEL_HELPER_ASSERT_NO_VERSION") == "1" {
		versionPath := os.Getenv("COXPANEL_HELPER_VERSION_PATH")
		if versionPath == "" {
			if configPath := os.Getenv("COXPANEL_HELPER_ACTIVE_CONFIG"); configPath != "" {
				versionPath = configPath + ".version"
			}
		}
		if versionPath != "" {
			if _, err := os.Stat(versionPath); err == nil {
				appendHelperEvent("VERSION-PRESENT")
			}
		}
	}
	if os.Getenv("COXPANEL_HELPER_RUN") == "early-exit" || (os.Getenv("COXPANEL_HELPER_RUN") == "early-exit-once" && createOnceMarker()) {
		os.Exit(17)
	}
	if os.Getenv("COXPANEL_HELPER_RUN") == "delayed-exit" || (os.Getenv("COXPANEL_HELPER_RUN") == "delayed-exit-once" && createOnceMarker()) {
		time.Sleep(corePostReadyObserve + 100*time.Millisecond)
		os.Exit(17)
	}
	term := make(chan os.Signal, 1)
	signal.Notify(term, syscall.SIGTERM, syscall.SIGINT)
	<-term
	appendHelperEvent("TERM")
}

func createOnceMarker() bool {
	marker := os.Getenv("COXPANEL_HELPER_EVENTS") + ".once"
	if _, err := os.Stat(marker); err == nil {
		return false
	}
	return os.WriteFile(marker, []byte("used"), 0600) == nil
}

func configDocumentJSON(t *testing.T, nodeID int64, content []byte, explicit bool) []byte {
	t.Helper()
	hash := config.Hash(content)
	type configDocumentMetadata struct {
		SchemaVersion string            `json:"schemaVersion"`
		Version       string            `json:"version"`
		SHA256        string            `json:"sha256"`
		NodeID        int64             `json:"nodeId"`
		Explicit      bool              `json:"explicit"`
		Topology      config.NodeConfig `json:"topology,omitempty"`
	}
	metadata, err := json.Marshal(configDocumentMetadata{
		SchemaVersion: config.SchemaVersion,
		Version:       hash,
		SHA256:        hash,
		NodeID:        nodeID,
		Explicit:      explicit,
		Topology:      config.NodeConfig{SchemaVersion: config.SchemaVersion, NodeID: nodeID},
	})
	if err != nil {
		t.Fatal(err)
	}
	payload := make([]byte, 0, len(metadata)+len(content)+len(",\"singbox\":"))
	payload = append(payload, metadata[:len(metadata)-1]...)
	payload = append(payload, []byte(",\"singbox\":")...)
	payload = append(payload, content...)
	payload = append(payload, '}')
	return payload
}

func renderedTestContentWithSS2022(t *testing.T, nodeID int64) []byte {
	return renderedTestContentWithSS2022Port(t, nodeID, 8388)
}

func renderedTestContentWithSS2022Port(t *testing.T, nodeID int64, port int) []byte {
	t.Helper()
	rendered, err := config.Render(config.NodeConfig{
		SchemaVersion: config.SchemaVersion,
		NodeID:        nodeID,
		NodeName:      "new",
		Inbounds: []config.Inbound{{
			ID:       1,
			Protocol: "shadowsocks",
			Role:     "entry",
			Listen:   "127.0.0.1",
			Port:     port,
			Params: map[string]string{
				"method":   "2022-blake3-aes-128-gcm",
				"password": "AAAAAAAAAAAAAAAAAAAAAA==",
			},
		}},
		Credentials: []config.UserCredential{{
			UserID: 1, InboundID: 1, Name: "synthetic-user", Protocol: "shadowsocks", Password: "BBBBBBBBBBBBBBBBBBBBBB==",
		}},
		Edges:    []config.Edge{},
		Outbound: "direct",
	})
	if err != nil {
		t.Fatal(err)
	}
	return rendered.Content
}

func freeTCPPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve test port: %v", err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}

func decodeSharedConfig(t *testing.T, payload []byte) sharedConfig {
	t.Helper()
	var document sharedConfig
	if err := json.Unmarshal(payload, &document); err != nil {
		t.Fatal(err)
	}
	return document
}

func renderedTestContent(t *testing.T, nodeID int64) []byte {
	return renderedTestContentWithNodeName(t, nodeID, "synthetic")
}

func renderedTestContentWithNodeName(t *testing.T, nodeID int64, name string) []byte {
	t.Helper()
	rendered, err := config.Render(config.NodeConfig{SchemaVersion: config.SchemaVersion, NodeID: nodeID, NodeName: name})
	if err != nil {
		t.Fatal(err)
	}
	return rendered.Content
}

func helperCore(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "sing-box-helper")
	contents := "#!/bin/sh\nexec " + os.Args[0] + " -test.run=TestHelperProcess -- \"$@\"\n"
	if err := os.WriteFile(path, []byte(contents), 0755); err != nil {
		t.Fatal(err)
	}
	return path
}

func useSyntheticReadiness(t *testing.T) {
	t.Helper()
	previous := readinessProbe
	readinessProbe = func(process *runningCore, _ []coreListener) error {
		timer := time.NewTimer(coreReadinessObserve)
		defer timer.Stop()
		select {
		case <-process.done:
			return errors.New("synthetic core exited before readiness")
		case <-timer.C:
			if processExited(process) {
				return errors.New("synthetic core exited during readiness")
			}
			return nil
		}
	}
	t.Cleanup(func() { readinessProbe = previous })
}

func appendHelperEvent(event string) {
	path := os.Getenv("COXPANEL_HELPER_EVENTS")
	if path == "" {
		return
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return
	}
	defer file.Close()
	_, _ = file.WriteString(event + "\n")
}

func hasArg(want string) bool {
	for _, arg := range os.Args {
		if arg == want {
			return true
		}
	}
	return false
}

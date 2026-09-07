package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/coxpanel/shared/config"
	"github.com/coxpanel/shared/contract"
)

var (
	panelURL = flag.String("panel", "", "面板地址")
	nodeID   = flag.Int64("node", 0, "节点 ID")
	apiKey   = flag.String("key", "", "兼容旧部署；优先使用 COXPANEL_AGENT_CREDENTIAL")
	sbPath   = flag.String("singbox", "/usr/local/bin/sing-box", "sing-box 二进制路径")
	cfgPath  = flag.String("config", "/etc/coxpanel/config.json", "配置文件路径")
	interval = flag.Duration("interval", 30*time.Second, "心跳/轮询间隔")

	agentHTTPClient      = &http.Client{Timeout: 5 * time.Second}
	coreCheckTimeout     = 5 * time.Second
	coreReadinessObserve = 200 * time.Millisecond
	coreReadinessTimeout = 2 * time.Second
	coreReadinessPoll    = 25 * time.Millisecond
	corePostReadyObserve = 750 * time.Millisecond
	coreShutdownTimeout  = 2 * time.Second
	maxConfigResponseLen = int64(4 << 20)
)

type sharedConfig = contract.ConfigDocument

type runningCore struct {
	cmd  *exec.Cmd
	done chan error
}

type coreReadinessProbe func(*runningCore, []coreListener) error

var (
	lastApplied    string
	coreMu         sync.Mutex
	ownedCore      *runningCore
	readinessProbe coreReadinessProbe = defaultCoreReadinessProbe
)

var errStoredNodeMismatch = errors.New("stored config node mismatch")

func main() {
	flag.Parse()
	if *panelURL == "" || *nodeID <= 0 {
		log.Fatal("必须指定 -panel 和 -node")
	}

	lastApplied = loadLastGoodVersion()
	log.Printf("Coxpanel agent 启动: node=%d", *nodeID)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	defer func() { _ = stopOwnedCore() }()

	ticker := time.NewTicker(*interval)
	defer ticker.Stop()
	run()
	for {
		select {
		case <-ticker.C:
			run()
		case <-ctx.Done():
			return
		}
	}
}

func run() {
	cfg, err := fetchConfig()
	if err != nil {
		if ensureErr := ensureLastGoodRunning(); ensureErr != nil {
			log.Printf("本地配置恢复失败")
		}
		sendHeartbeat(appliedHeartbeatVersion())
		return
	}

	if cfg.Version == lastApplied && ownedCoreRunning() {
		sendHeartbeat(appliedHeartbeatVersion())
		return
	}
	if cfg.Version == lastApplied {
		if err := ensureLastGoodRunning(); err != nil {
			log.Printf("本地配置恢复失败")
			sendHeartbeat(appliedHeartbeatVersion())
			return
		}
		sendHeartbeat(appliedHeartbeatVersion())
		return
	}

	if err := applyConfig(&cfg); err != nil {
		log.Printf("配置应用失败")
	}
	sendHeartbeat(appliedHeartbeatVersion())
}

func fetchConfig() (sharedConfig, error) {
	url := strings.TrimRight(*panelURL, "/") + "/api/agent/config"
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return sharedConfig{}, errors.New("panel request failed")
	}
	req.Header.Set(contract.AgentNodeIDHeader, fmt.Sprintf("%d", *nodeID))
	req.Header.Set(contract.AgentCredentialHeader, agentCredential())
	resp, err := agentHTTPClient.Do(req)
	if err != nil {
		return sharedConfig{}, errors.New("panel request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return sharedConfig{}, fmt.Errorf("panel returned HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxConfigResponseLen+1))
	if err != nil || int64(len(body)) > maxConfigResponseLen {
		return sharedConfig{}, errors.New("panel response unavailable")
	}
	var cfg sharedConfig
	if err := json.Unmarshal(body, &cfg); err != nil {
		return sharedConfig{}, errors.New("panel response invalid")
	}
	if err := cfg.Validate(true); err != nil {
		return sharedConfig{}, errors.New("panel config invalid")
	}
	if cfg.NodeID != *nodeID {
		return sharedConfig{}, errors.New("panel config node mismatch")
	}
	return cfg, nil
}

func applyConfig(cfg *sharedConfig) error {
	if cfg == nil {
		return errors.New("config missing")
	}
	if err := cfg.Validate(true); err != nil {
		return errors.New("config validation failed")
	}
	if cfg.NodeID != *nodeID {
		return errors.New("config node mismatch")
	}
	if err := validateCoreBinary(); err != nil {
		return err
	}

	oldContent, oldVersion, oldErr := readLastGood()
	hasOld := oldErr == nil
	if oldErr != nil && !errors.Is(oldErr, os.ErrNotExist) {
		return errors.New("last-good config unavailable")
	}
	if err := checkCoreBytes(cfg.Singbox); err != nil {
		return err
	}
	candidatePath, err := createCandidatePath()
	if err != nil {
		return errors.New("candidate config unavailable")
	}
	defer os.Remove(candidatePath)
	if err := atomicWrite(candidatePath, cfg.Singbox, 0600); err != nil {
		return errors.New("candidate config persistence failed")
	}
	if err := stopOwnedCore(); err != nil {
		return errors.New("owned core could not stop")
	}
	process, err := startOwnedCore(candidatePath)
	if err != nil {
		_ = restorePrevious(oldContent, oldVersion, hasOld)
		_ = restartPrevious(oldVersion, hasOld)
		return errors.New("core startup failed; previous config restored")
	}
	rollback := func() error {
		if stopErr := stopCore(process); stopErr != nil {
			return stopErr
		}
		if restoreErr := restorePrevious(oldContent, oldVersion, hasOld); restoreErr != nil {
			return restoreErr
		}
		return restartPrevious(oldVersion, hasOld)
	}
	if err := adoptOwnedCore(process); err != nil {
		_ = rollback()
		return errors.New("core exited before apply completed")
	}
	if err := writeDurableState(cfg.Singbox, cfg.Version); err != nil {
		_ = rollback()
		return errors.New("config state persistence failed")
	}
	if err := atomicWrite(*cfgPath, cfg.Singbox, 0600); err != nil {
		_ = rollback()
		return errors.New("config persistence failed")
	}
	if err := atomicWrite(versionPath(), []byte(cfg.Version+"\n"), 0600); err != nil {
		_ = rollback()
		return errors.New("config version persistence failed")
	}
	if err := atomicWrite(nodePath(), []byte(fmt.Sprintf("%d\n", *nodeID)), 0600); err != nil {
		_ = rollback()
		return errors.New("config node persistence failed")
	}
	lastApplied = cfg.Version
	return nil
}

func ensureLastGoodRunning() error {
	if ownedCoreRunning() {
		return nil
	}
	if err := validateCoreBinary(); err != nil {
		return err
	}
	content, version, err := readLastGood()
	if err != nil {
		return errors.New("last-good config unavailable")
	}
	if err := checkCoreBytes(content); err != nil {
		return err
	}
	if err := atomicWrite(*cfgPath, content, 0600); err != nil {
		return errors.New("last-good config materialization failed")
	}
	process, err := startOwnedCore(*cfgPath)
	if err != nil {
		return errors.New("last-good core startup failed")
	}
	if err := adoptOwnedCore(process); err != nil {
		return errors.New("last-good core exited during startup")
	}
	lastApplied = version
	return nil
}

func validateCoreBinary() error {
	info, err := os.Stat(*sbPath)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&0111 == 0 {
		return errors.New("sing-box core unavailable")
	}
	return nil
}

func checkCoreBytes(content []byte) error {
	dir := filepath.Dir(*cfgPath)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return errors.New("config directory unavailable")
	}
	tmp, err := os.CreateTemp(dir, ".sing-box-check-*")
	if err != nil {
		return errors.New("check file unavailable")
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0600); err != nil {
		_ = tmp.Close()
		return errors.New("check file unavailable")
	}
	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close()
		return errors.New("check file unavailable")
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return errors.New("check file unavailable")
	}
	if err := tmp.Close(); err != nil {
		return errors.New("check file unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), coreCheckTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, *sbPath, "check", "-c", tmpPath)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Run(); ctx.Err() != nil {
		return errors.New("sing-box check timed out")
	} else if err != nil {
		return errors.New("sing-box check failed")
	}
	return nil
}

func startOwnedCore(configPath string) (*runningCore, error) {
	listeners, err := expectedCoreListeners(configPath)
	if err != nil {
		return nil, errors.New("owned core readiness config unavailable")
	}
	if err := ensureCoreListenersAvailable(listeners); err != nil {
		return nil, err
	}
	cmd := exec.Command(*sbPath, "run", "-c", configPath)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return nil, errors.New("owned core could not start")
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	process := &runningCore{cmd: cmd, done: done}
	if err := readinessProbe(process, listeners); err != nil {
		_ = stopCore(process)
		return nil, err
	}
	if err := waitForCoreStability(process, corePostReadyObserve); err != nil {
		_ = stopCore(process)
		return nil, err
	}
	return process, nil
}

func defaultCoreReadinessProbe(process *runningCore, listeners []coreListener) error {
	if len(listeners) == 0 {
		return waitForCoreStability(process, coreReadinessObserve)
	}
	return waitForCoreListeners(process, listeners)
}

func waitForCoreStability(process *runningCore, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-process.done:
		return errors.New("owned core exited during post-readiness observation")
	case <-timer.C:
		if processExited(process) {
			return errors.New("owned core exited during post-readiness observation")
		}
		return nil
	}
}

type coreListener struct {
	network   string
	addresses []string
}

func expectedCoreListeners(configPath string) ([]coreListener, error) {
	content, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}
	var parsed struct {
		Inbounds []struct {
			Type       string `json:"type"`
			Listen     string `json:"listen"`
			ListenPort int    `json:"listen_port"`
		} `json:"inbounds"`
	}
	if err := json.Unmarshal(content, &parsed); err != nil {
		return nil, err
	}
	listeners := make([]coreListener, 0, len(parsed.Inbounds))
	for _, inbound := range parsed.Inbounds {
		if inbound.ListenPort < 1 || inbound.ListenPort > 65535 {
			return nil, errors.New("core listener port is invalid")
		}
		host := strings.TrimSpace(inbound.Listen)
		if host == "" {
			host = "::"
		}
		network := "tcp"
		if inbound.Type == "hysteria2" {
			network = "udp"
		}
		listeners = append(listeners, coreListener{network: network, addresses: listenerAddresses(host, inbound.ListenPort)})
	}
	return listeners, nil
}

func listenerAddresses(host string, port int) []string {
	host = strings.Trim(host, "[]")
	hosts := []string{host}
	switch host {
	case "", "*", "0.0.0.0":
		hosts = []string{"127.0.0.1", "::1"}
	case "::":
		hosts = []string{"::1", "127.0.0.1"}
	}
	addresses := make([]string, 0, len(hosts))
	for _, candidate := range hosts {
		addresses = append(addresses, net.JoinHostPort(candidate, fmt.Sprintf("%d", port)))
	}
	return addresses
}

func ensureCoreListenersAvailable(listeners []coreListener) error {
	for _, listener := range listeners {
		for _, address := range listener.addresses {
			if listener.network == "udp" {
				udpAddress, err := net.ResolveUDPAddr("udp", address)
				if err != nil {
					continue
				}
				probe, err := net.ListenUDP("udp", udpAddress)
				if err == nil {
					_ = probe.Close()
					continue
				}
				return errors.New("expected core listener is already occupied")
			}
			connection, err := net.DialTimeout("tcp", address, coreReadinessPoll)
			if err == nil {
				_ = connection.Close()
				return errors.New("expected core listener is already occupied")
			}
		}
	}
	return nil
}

func waitForCoreListeners(process *runningCore, listeners []coreListener) error {
	deadline := time.NewTimer(coreReadinessTimeout)
	defer deadline.Stop()
	poll := time.NewTicker(coreReadinessPoll)
	defer poll.Stop()
	for {
		if processExited(process) {
			return errors.New("owned core exited before listener readiness")
		}
		if coreListenersReady(listeners) {
			stable := time.NewTimer(coreReadinessObserve)
			select {
			case <-stable.C:
				if processExited(process) {
					return errors.New("owned core exited during listener readiness")
				}
				if coreListenersReady(listeners) {
					return nil
				}
			case <-process.done:
				if !stable.Stop() {
					<-stable.C
				}
				return errors.New("owned core exited during listener readiness")
			case <-deadline.C:
				if !stable.Stop() {
					<-stable.C
				}
				return errors.New("owned core listener readiness timed out")
			}
			continue
		}
		select {
		case <-poll.C:
		case <-deadline.C:
			return errors.New("owned core listener readiness timed out")
		case <-process.done:
			return errors.New("owned core exited before listener readiness")
		}
	}
}

func coreListenersReady(listeners []coreListener) bool {
	for _, listener := range listeners {
		if !coreListenerReady(listener) {
			return false
		}
	}
	return true
}

func coreListenerReady(listener coreListener) bool {
	for _, address := range listener.addresses {
		if listener.network == "udp" {
			udpAddress, err := net.ResolveUDPAddr("udp", address)
			if err != nil {
				continue
			}
			probe, err := net.ListenUDP("udp", udpAddress)
			if err == nil {
				_ = probe.Close()
				continue
			}
			if errors.Is(err, syscall.EADDRINUSE) {
				return true
			}
			continue
		}
		connection, err := net.DialTimeout("tcp", address, coreReadinessPoll)
		if err == nil {
			_ = connection.Close()
			return true
		}
	}
	return false
}

func processExited(process *runningCore) bool {
	if process == nil {
		return true
	}
	select {
	case <-process.done:
		return true
	default:
		return false
	}
}

func stopOwnedCore() error {
	coreMu.Lock()
	process := ownedCore
	coreMu.Unlock()
	if process == nil {
		return nil
	}
	err := stopCore(process)
	if err == nil || processExited(process) {
		coreMu.Lock()
		if ownedCore == process {
			ownedCore = nil
		}
		coreMu.Unlock()
	}
	return err
}

func stopCore(process *runningCore) error {
	if process == nil {
		return nil
	}
	select {
	case <-process.done:
		clearOwnedCore(process)
		return nil
	default:
	}
	if err := process.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		if processExited(process) {
			clearOwnedCore(process)
			return nil
		}
		return errors.New("owned core signal failed")
	}
	timer := time.NewTimer(coreShutdownTimeout)
	defer timer.Stop()
	select {
	case <-process.done:
		clearOwnedCore(process)
		return nil
	case <-timer.C:
		_ = process.cmd.Process.Kill()
		<-process.done
		clearOwnedCore(process)
		return errors.New("owned core shutdown timed out")
	}
}

func clearOwnedCore(process *runningCore) {
	coreMu.Lock()
	if ownedCore == process {
		ownedCore = nil
	}
	coreMu.Unlock()
}

func adoptOwnedCore(process *runningCore) error {
	if process == nil {
		return errors.New("owned core missing")
	}
	select {
	case <-process.done:
		return errors.New("owned core is not running")
	default:
	}
	coreMu.Lock()
	ownedCore = process
	coreMu.Unlock()
	return nil
}

func ownedCoreRunning() bool {
	coreMu.Lock()
	defer coreMu.Unlock()
	if ownedCore == nil {
		return false
	}
	select {
	case <-ownedCore.done:
		ownedCore = nil
		return false
	default:
		return true
	}
}

func restartPrevious(version string, hasPrevious bool) error {
	if !hasPrevious {
		return nil
	}
	process, err := startOwnedCore(*cfgPath)
	if err != nil {
		return err
	}
	if err := adoptOwnedCore(process); err != nil {
		return err
	}
	lastApplied = version
	return nil
}

func restorePrevious(content []byte, version string, hasPrevious bool) error {
	if hasPrevious {
		if err := writeDurableState(content, version); err != nil {
			return err
		}
		if err := atomicWrite(*cfgPath, content, 0600); err != nil {
			return err
		}
		if err := atomicWrite(versionPath(), []byte(version+"\n"), 0600); err != nil {
			return err
		}
		return atomicWrite(nodePath(), []byte(fmt.Sprintf("%d\n", *nodeID)), 0600)
	}
	if err := removeDurable(*cfgPath); err != nil {
		return err
	}
	if err := removeDurable(durableStatePath()); err != nil {
		return err
	}
	if err := removeDurable(versionPath()); err != nil {
		return err
	}
	return removeDurable(nodePath())
}

func readLastGood() ([]byte, string, error) {
	stateBytes, stateErr := os.ReadFile(durableStatePath())
	if stateErr == nil {
		var state durableConfigState
		if err := json.Unmarshal(stateBytes, &state); err != nil {
			return nil, "", errors.New("stored config state invalid")
		}
		return validateDurableState(state)
	}
	if !errors.Is(stateErr, os.ErrNotExist) {
		return nil, "", stateErr
	}
	content, err := os.ReadFile(*cfgPath)
	if err != nil {
		return nil, "", err
	}
	if err := config.ValidateRendered(content); err != nil {
		return nil, "", errors.New("stored config invalid")
	}
	versionBytes, err := os.ReadFile(versionPath())
	if err != nil {
		return nil, "", err
	}
	version := strings.TrimSpace(string(versionBytes))
	hash := config.Hash(content)
	if !strings.EqualFold(version, hash) {
		return nil, "", errors.New("stored config version mismatch")
	}
	nodeBytes, err := os.ReadFile(nodePath())
	if err != nil {
		return nil, "", err
	}
	storedNode := strings.TrimSpace(string(nodeBytes))
	if storedNode != fmt.Sprintf("%d", *nodeID) {
		return nil, "", errStoredNodeMismatch
	}
	return content, version, nil
}

type durableConfigState struct {
	SchemaVersion string `json:"schemaVersion"`
	Version       string `json:"version"`
	SHA256        string `json:"sha256"`
	NodeID        int64  `json:"nodeId"`
	Singbox       []byte `json:"singbox"`
}

func durableStatePath() string { return *cfgPath + ".state" }

func writeDurableState(content []byte, version string) error {
	hash := config.Hash(content)
	if !strings.EqualFold(version, hash) {
		return errors.New("durable config version mismatch")
	}
	state := durableConfigState{
		SchemaVersion: config.SchemaVersion,
		Version:       version,
		SHA256:        hash,
		NodeID:        *nodeID,
		Singbox:       append(json.RawMessage(nil), content...),
	}
	encoded, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return atomicWrite(durableStatePath(), encoded, 0600)
}

func validateDurableState(state durableConfigState) ([]byte, string, error) {
	if state.SchemaVersion != config.SchemaVersion || state.NodeID != *nodeID {
		return nil, "", errStoredNodeMismatch
	}
	if !json.Valid(state.Singbox) || config.ValidateRendered(state.Singbox) != nil {
		return nil, "", errors.New("stored config invalid")
	}
	hash := config.Hash(state.Singbox)
	if !strings.EqualFold(state.Version, hash) || !strings.EqualFold(state.SHA256, hash) {
		return nil, "", errors.New("stored config version mismatch")
	}
	return append([]byte(nil), state.Singbox...), state.Version, nil
}

func atomicWrite(path string, content []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	return syncDirectory(dir)
}

func removeDurable(path string) error {
	err := os.Remove(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return syncDirectory(filepath.Dir(path))
}

func syncDirectory(dir string) error {
	file, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer file.Close()
	return file.Sync()
}

func versionPath() string { return *cfgPath + ".version" }

func nodePath() string { return *cfgPath + ".node" }

func createCandidatePath() (string, error) {
	dir := filepath.Dir(*cfgPath)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return "", err
	}
	file, err := os.CreateTemp(dir, "."+filepath.Base(*cfgPath)+".candidate-*")
	if err != nil {
		return "", err
	}
	path := file.Name()
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	if err := os.Remove(path); err != nil {
		return "", err
	}
	return path, nil
}

func loadLastGoodVersion() string {
	_, version, err := readLastGood()
	if err != nil {
		return ""
	}
	return version
}

func agentCredential() string {
	if value, ok := os.LookupEnv("COXPANEL_AGENT_CREDENTIAL"); ok {
		return value
	}
	return *apiKey
}

func appliedHeartbeatVersion() string {
	if !ownedCoreRunning() {
		return ""
	}
	return lastApplied
}

func sendHeartbeat(version string) {
	hb := contract.Heartbeat{NodeID: *nodeID, Version: version, At: time.Now()}
	body, err := json.Marshal(hb)
	if err != nil {
		return
	}
	url := strings.TrimRight(*panelURL, "/") + "/api/agent/heartbeat"
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set(contract.AgentNodeIDHeader, fmt.Sprintf("%d", *nodeID))
	req.Header.Set(contract.AgentCredentialHeader, agentCredential())
	req.Header.Set("Content-Type", "application/json")
	resp, err := agentHTTPClient.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		log.Printf("心跳非 200: %d", resp.StatusCode)
	}
}

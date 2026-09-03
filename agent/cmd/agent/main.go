// Coxpanel 节点 Agent 入口。
// 职责：心跳上报、拉取配置、校验并原子应用 sing-box 配置。
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/coxpanel/shared/contract"
)

var (
	panelURL = flag.String("panel", "", "面板地址，如 http://panel:8080")
	nodeID   = flag.Int64("node", 0, "节点 ID")
	apiKey   = flag.String("key", "", "节点 API key")
	sbPath   = flag.String("singbox", "/usr/local/bin/sing-box", "sing-box 二进制路径")
	cfgPath  = flag.String("config", "/etc/coxpanel/config.json", "配置文件路径")
	interval = flag.Duration("interval", 30*time.Second, "心跳/轮询间隔")
)

func main() {
	flag.Parse()
	if *panelURL == "" || *nodeID == 0 {
		log.Fatal("必须指定 -panel 和 -node")
	}
	log.Printf("Coxpanel agent 启动: node=%d panel=%s", *nodeID, *panelURL)

	ticker := time.NewTicker(*interval)
	defer ticker.Stop()
	run() // 启动立即执行一次
	for range ticker.C {
		run()
	}
}

func run() {
	// 1. 拉取最新配置
	cfg, err := fetchConfig()
	if err != nil {
		log.Printf("拉取配置失败: %v", err)
		sendHeartbeat("")
		return
	}
	// 2. 与本地生效版本比对
	if cfg.Version == lastApplied {
		sendHeartbeat(cfg.Version)
		return
	}
	// 3. 应用配置（sing-box check → 原子写 → 通知重载）
	if err := applyConfig(cfg); err != nil {
		log.Printf("应用配置失败: %v", err)
		sendHeartbeat(lastApplied)
		return
	}
	lastApplied = cfg.Version
	log.Printf("配置已更新: %s", cfg.Version)
	sendHeartbeat(cfg.Version)
}

var lastApplied string

// fetchConfig 从面板拉取节点配置。
func fetchConfig() (*sharedConfig, error) {
	req, _ := http.NewRequest("GET", *panelURL+"/api/agent/config", nil)
	req.Header.Set("Authorization", "Bearer "+*apiKey)
	req.Header.Set("X-Node-Id", fmt.Sprintf("%d", *nodeID))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}
	var cfg sharedConfig
	if err := json.NewDecoder(resp.Body).Decode(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// sharedConfig 与 shared/config.NodeConfig 对齐（P1 简化，直接复用 JSON 结构）。
type sharedConfig = contract.ConfigPush

// applyConfig 校验并原子应用配置。
func applyConfig(cfg *sharedConfig) error {
	// 面板推送的是完整配置内容（P1 简化：把配置内容写盘）
	// 实际完整实现：GET cfg.URL 拉取完整 NodeConfig → 生成 sing-box JSON
	// P1：直接写配置内容
	tmp := *cfgPath + ".tmp"
	if err := os.WriteFile(tmp, []byte(cfg.URL), 0600); err != nil {
		return fmt.Errorf("写临时文件: %w", err)
	}
	// sing-box check（若 sing-box 可用）
	if _, err := os.Stat(*sbPath); err == nil {
		cmd := exec.Command(*sbPath, "check", "-c", tmp)
		if out, err := cmd.CombinedOutput(); err != nil {
			os.Remove(tmp)
			return fmt.Errorf("sing-box check 失败: %v: %s", err, string(out))
		}
	}
	// 原子 rename
	if err := os.Rename(tmp, *cfgPath); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("rename: %w", err)
	}
	// 通知 sing-box 重载（SIGHUP）
	reloadSingbox()
	return nil
}

// reloadSingbox 向 sing-box 进程发 SIGHUP 热载。
func reloadSingbox() {
	pidFile := filepath.Dir(*cfgPath) + "/sing-box.pid"
	data, err := os.ReadFile(pidFile)
	if err != nil {
		return // 无 pid 文件，跳过（首次由容器编排启动）
	}
	var pid int
	fmt.Sscanf(string(data), "%d", &pid)
	if pid > 0 {
		if p, err := os.FindProcess(pid); err == nil {
			p.Signal(syscall.SIGHUP)
		}
	}
}

// sendHeartbeat 上报心跳。
func sendHeartbeat(version string) {
	hb := contract.Heartbeat{
		NodeID:  *nodeID,
		Version: version,
		At:      time.Now(),
	}
	body, _ := json.Marshal(hb)
	req, _ := http.NewRequest("POST", *panelURL+"/api/agent/heartbeat", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+*apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("心跳失败: %v", err)
		return
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		log.Printf("心跳非 200: %d", resp.StatusCode)
	}
}

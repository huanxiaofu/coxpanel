// Package contract 定义 agent ↔ panel 通信消息格式。
package contract

import "time"

// Heartbeat agent 定时上报的心跳消息。
type Heartbeat struct {
	NodeID      int64     `json:"nodeId"`
	Version     string    `json:"version"` // agent 当前生效的配置版本
	CoreVersion string    `json:"coreVersion"` // sing-box 版本
	CPU         float64   `json:"cpu"`     // 百分比
	Mem         float64   `json:"mem"`     // 百分比
	OnlineUsers int       `json:"onlineUsers"`
	Uptime      int64     `json:"uptime"`  // 秒
	At          time.Time `json:"at"`
}

// TrafficReport 流量上报（用户级，按入站）。
type TrafficReport struct {
	NodeID   int64          `json:"nodeId"`
	Period   time.Time      `json:"period"` // 聚合周期起点
	Users    []UserTraffic  `json:"users"`
	At       time.Time      `json:"at"`
}

// UserTraffic 单个用户在一个入站的流量。
type UserTraffic struct {
	UserUUID string `json:"userUuid"` // 用户标识（协议层 uuid/password 映射）
	InboundID int64 `json:"inboundId"`
	UpBytes   int64 `json:"upBytes"`
	DownBytes int64 `json:"downBytes"`
}

// ConfigPush panel → agent 的配置推送消息（WS）。
type ConfigPush struct {
	Version string `json:"version"`
	URL     string `json:"url"` // agent 拉取完整配置的地址（带签名）
}

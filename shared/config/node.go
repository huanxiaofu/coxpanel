// Package config 定义 panel 下发节点的完整配置契约。
// backend 生成、agent 解析，两端共用一套结构。
package config

import "time"

// NodeConfig 是面板下发给一个节点（一台机器）的完整配置。
type NodeConfig struct {
	Version   string    `json:"version"`   // 配置版本号，agent 用于比对
	UpdatedAt time.Time `json:"updatedAt"` // 生成时间

	NodeID   int64      `json:"nodeId"`
	NodeName string     `json:"nodeName"`
	Inbounds []Inbound  `json:"inbounds"`
	Edges    []Edge     `json:"edges"`    // 中转/落地连线
	Outbound string     `json:"outbound"` // 默认出站 tag（direct 或指定）
}

// Inbound 一个入站。
type Inbound struct {
	ID        int64             `json:"id"`
	Name      string            `json:"name"`
	Protocol  string            `json:"protocol"` // vless-reality / shadowsocks / hysteria2
	Role      string            `json:"role"`     // entry / landing / relay
	Listen    string            `json:"listen"`
	Port      int               `json:"port"`
	Params    map[string]string `json:"params"` // 协议参数（uuid/password/privateKey/sni/...）
	MinClientVer string         `json:"minClientVer,omitempty"` // Reality 专用
}

// Edge 一条拓扑连线（from 本机入站 → 远端落地入站）。
type Edge struct {
	ID            int64  `json:"id"`
	FromInboundID int64  `json:"fromInboundId"`
	ToNodeID      int64  `json:"toNodeId"`
	ToInboundID   int64  `json:"toInboundId"`
	ToServer      string `json:"toServer"`   // 落地服务器地址
	ToPort        int    `json:"toPort"`     // 落地端口
	ToProtocol    string `json:"toProtocol"` // 落地协议（shadowsocks / hysteria2）
	ToParams      map[string]string `json:"toParams"` // 落地协议参数
}

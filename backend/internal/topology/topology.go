package topology

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/coxpanel/backend/internal/models"
	sharedconfig "github.com/coxpanel/shared/config"
)

type SingboxConfig struct {
	sharedconfig.SingBoxConfig
}

type LogConfig = sharedconfig.LogConfig
type SBInbound = sharedconfig.SBInbound
type SBVLESSUser = sharedconfig.SBUser
type SBInboundTLS = sharedconfig.SBInboundTLS
type SBReality = sharedconfig.SBReality
type SBHandshake = sharedconfig.SBHandshake
type SBHysteriaObfs = sharedconfig.SBHysteriaObfs
type SBOutbound = sharedconfig.SBOutbound
type SBOutboundTLS = sharedconfig.SBOutboundTLS
type SBOutboundReality = sharedconfig.SBOutboundReality
type SBRoute = sharedconfig.SBRoute
type SBRule = sharedconfig.SBRule

type Edge struct {
	FromInboundID int64
	ToNodeID      int64
	ToInboundID   int64
	ToServer      string
	ToPort        int
	ToProtocol    string
	ToParams      map[string]string
}

func Build(node *models.Node, inbounds []models.Inbound, edges []Edge) (*SingboxConfig, error) {
	if node == nil {
		return nil, fmt.Errorf("node is nil")
	}
	config := sharedconfig.NodeConfig{
		SchemaVersion: sharedconfig.SchemaVersion,
		NodeID:        node.ID,
		NodeName:      node.Name,
		Inbounds:      make([]sharedconfig.Inbound, 0, len(inbounds)),
		Edges:         make([]sharedconfig.Edge, 0, len(edges)),
		Outbound:      "direct",
	}
	for _, inbound := range inbounds {
		params := make(map[string]string)
		if len(inbound.Config) > 0 && string(inbound.Config) != "null" {
			if err := json.Unmarshal(inbound.Config, &params); err != nil {
				return nil, fmt.Errorf("inbound %d parameters: %w", inbound.ID, err)
			}
		}
		config.Inbounds = append(config.Inbounds, sharedconfig.Inbound{
			ID: inbound.ID, Name: inbound.Name, Protocol: inbound.Protocol, Role: inbound.Role,
			Listen: inbound.ListenAddr, Port: inbound.ListenPort, Params: params,
		})
	}
	for index, edge := range edges {
		config.Edges = append(config.Edges, sharedconfig.Edge{
			ID: int64(index + 1), FromInboundID: edge.FromInboundID, ToNodeID: edge.ToNodeID,
			ToInboundID: edge.ToInboundID, ToServer: edge.ToServer, ToPort: edge.ToPort,
			ToProtocol: edge.ToProtocol, ToParams: edge.ToParams,
		})
	}
	rendered, err := sharedconfig.RenderRouting(config)
	if err != nil {
		return nil, err
	}
	return &SingboxConfig{SingBoxConfig: decodeSingBoxConfig(rendered.Content)}, nil
}

func decodeSingBoxConfig(content []byte) sharedconfig.SingBoxConfig {
	var decoded sharedconfig.SingBoxConfig
	_ = json.Unmarshal(content, &decoded)
	return decoded
}

func (c *SingboxConfig) Marshal() ([]byte, error) {
	if c == nil {
		return nil, fmt.Errorf("config is nil")
	}
	return json.MarshalIndent(c.SingBoxConfig, "", "  ")
}

func SortInbounds(inbounds []SBInbound) {
	sort.Slice(inbounds, func(i, j int) bool {
		return inbounds[i].ListenPort < inbounds[j].ListenPort
	})
}

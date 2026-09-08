package contract

import "encoding/json"

func (document TopologyDeploymentDocument) JSONBytes() ([]byte, error) {
	metadata, err := json.Marshal(map[string]any{"schemaVersion": document.SchemaVersion, "version": document.Version, "sha256": document.SHA256, "nodeId": document.NodeID, "explicit": document.Explicit, "topology": document.Topology, "releaseId": document.ReleaseID, "generation": document.Generation, "phase": document.Phase, "routingVersion": document.RoutingVersion, "token": document.Token})
	if err != nil {
		return nil, err
	}
	body := append(metadata[:len(metadata)-1], []byte(",\"singbox\":")...)
	body = append(body, document.Singbox...)
	return append(body, '}'), nil
}

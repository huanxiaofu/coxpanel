package contract

// HasTopologyChainV2 reports whether an authenticated heartbeat advertises
// the P2 deployment capability.
func (h Heartbeat) HasTopologyChainV2() bool {
	for _, capability := range h.Capabilities {
		if capability == AgentCapabilityTopologyChainV2 {
			return true
		}
	}
	return false
}

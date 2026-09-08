package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/coxpanel/shared/contract"
)

type deploymentState struct {
	NodeID         int64  `json:"nodeId"`
	ReleaseID      int64  `json:"releaseId"`
	Generation     int64  `json:"generation"`
	RuntimeVersion string `json:"runtimeVersion"`
	Phase          string `json:"phase"`
}

func fetchDeploymentConfig(pending contract.PendingDeployment) (contract.TopologyDeploymentDocument, error) {
	var document contract.TopologyDeploymentDocument
	if pending.Validate() != nil {
		return document, errors.New("invalid deployment request")
	}
	url := fmt.Sprintf("%s/api/agent/config?phase=%s&releaseId=%d", strings.TrimRight(*panelURL, "/"), pending.Phase, pending.ReleaseID)
	request, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return document, errors.New("deployment request failed")
	}
	request.Header.Set(contract.AgentNodeIDHeader, fmt.Sprint(*nodeID))
	request.Header.Set(contract.AgentCredentialHeader, agentCredential())
	response, err := agentHTTPClient.Do(request)
	if err != nil {
		return document, errors.New("deployment unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return document, errors.New("deployment unavailable")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxConfigResponseLen+1))
	if err != nil || int64(len(body)) > maxConfigResponseLen {
		return document, errors.New("deployment too large")
	}
	if json.Unmarshal(body, &document) != nil || document.Validate(true) != nil || document.NodeID != *nodeID || document.ReleaseID != pending.ReleaseID || document.Generation != pending.Generation || document.Phase != pending.Phase {
		return document, errors.New("deployment binding invalid")
	}
	return document, nil
}

func readDeploymentState() deploymentState {
	var state deploymentState
	content, err := os.ReadFile(*cfgPath + ".deployment.json")
	if err != nil || len(content) > 4096 || json.Unmarshal(content, &state) != nil || state.NodeID != *nodeID {
		return deploymentState{}
	}
	return state
}

func handlePendingDeployment(pending contract.PendingDeployment) {
	state := readDeploymentState()
	if pending.Generation < state.Generation {
		return
	}
	document, err := fetchDeploymentConfig(pending)
	if err != nil {
		return
	}
	ack := contract.DeploymentAck{NodeID: *nodeID, ReleaseID: document.ReleaseID, Generation: document.Generation, Phase: document.Phase, RuntimeVersion: document.Version, Token: document.Token}
	if pending.Generation == state.Generation && state.ReleaseID != 0 && state.ReleaseID != pending.ReleaseID {
		return
	}
	if pending.Phase == contract.DeploymentPhasePrepare {
		if state.Generation == pending.Generation && state.Phase != contract.DeploymentPhasePrepare {
			return
		}
		err = validateCoreBinary()
		if err == nil {
			err = checkCoreBytes(document.Singbox)
		}
		if err == nil {
			err = atomicWrite(*cfgPath+".prepared.json", document.Singbox, 0600)
		}
		ack.Status = contract.AckStatusPrepared
	} else {
		if document.Version != lastApplied || !ownedCoreRunning() {
			err = applyConfig(&document.ConfigDocument)
		}
		ack.Status = contract.AckStatusApplied
		if pending.Phase == contract.DeploymentPhaseRollback {
			ack.Status = contract.AckStatusRolledBack
		}
	}
	if err == nil {
		state = deploymentState{NodeID: *nodeID, ReleaseID: document.ReleaseID, Generation: document.Generation, RuntimeVersion: document.Version, Phase: document.Phase}
		content, _ := json.Marshal(state)
		err = atomicWrite(*cfgPath+".deployment.json", content, 0600)
	}
	if err != nil {
		ack.Status = contract.AckStatusFailed
		ack.ErrorCode = "core_" + pending.Phase + "_failed"
	}
	content, _ := json.Marshal(ack)
	request, err := http.NewRequest(http.MethodPost, strings.TrimRight(*panelURL, "/")+"/api/agent/deployment-acks", bytes.NewReader(content))
	if err != nil {
		return
	}
	request.Header.Set(contract.AgentNodeIDHeader, fmt.Sprint(*nodeID))
	request.Header.Set(contract.AgentCredentialHeader, agentCredential())
	request.Header.Set("Content-Type", "application/json")
	response, err := agentHTTPClient.Do(request)
	if err == nil {
		response.Body.Close()
	}
}

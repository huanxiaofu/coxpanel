package acceptance

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	acceptanceMihomoPath  = "/work/tools/mihomo"
	acceptanceSingboxPath = "/workspace/tmp/sing-box-audit/sing-box-1.13.21-linux-amd64/sing-box"
)

func TestPanelDatabaseAPIAndProtocolAcceptance(t *testing.T) {
	harness := newPanelHarness(t)
	owner := harness.bootstrapOwner()
	groupID := harness.createGroup(owner, "p1-panel-protocol-group")

	tlsMaterial := newAcceptanceTLSMaterial(t)
	realityKeys := generateAcceptanceRealityKeys(t)
	serverPSK := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0x11}, 16))
	realityPort := reserveAcceptanceTCPPort(t)
	ss2022Port := reserveAcceptanceTCPPort(t)
	hysteriaPort := reserveAcceptanceUDPPort(t)
	node := harness.createNode(owner, "p1-panel-protocol-node", "127.0.0.1")
	realityInbound := harness.createInbound(owner, node.ID, "reality-entry", "vless-reality", "entry", "127.0.0.1", realityPort, json.RawMessage(fmt.Sprintf(`{"sni":"example.com","publicKey":%q,"privateKey":%q,"shortId":"0123456789abcdef","target":%q}`, realityKeys.publicKey, realityKeys.privateKey, tlsMaterial.address)))
	ssInbound := harness.createInbound(owner, node.ID, "ss2022-entry", "shadowsocks", "entry", "127.0.0.1", ss2022Port, json.RawMessage(fmt.Sprintf(`{"method":"2022-blake3-aes-128-gcm","password":%q}`, serverPSK)))
	hy2Inbound := harness.createInbound(owner, node.ID, "hysteria2-entry", "hysteria2", "entry", "127.0.0.1", hysteriaPort, json.RawMessage(fmt.Sprintf(`{"sni":"example.com","certificatePath":%q,"keyPath":%q,"insecure":true}`, tlsMaterial.certificatePath, tlsMaterial.keyPath)))
	if realityInbound == ssInbound || realityInbound == hy2Inbound || ssInbound == hy2Inbound {
		t.Fatal("protocol inbounds did not receive distinct database ids")
	}
	harness.setGroupNodes(owner, groupID, []int64{node.ID})

	userOne := harness.register("p1-panel-user-one", "User-one-password-1!", "p1-user-one@example.invalid", harness.createInvite(owner, groupID))
	userTwo := harness.register("p1-panel-user-two", "User-two-password-2!", "p1-user-two@example.invalid", harness.createInvite(owner, groupID))

	groupsBody := harness.doRaw(http.MethodGet, "/api/groups", owner.Token, nil, http.StatusOK)
	var groups []struct {
		ID      int64   `json:"id"`
		NodeIDs []int64 `json:"nodeIds"`
	}
	if err := json.Unmarshal(groupsBody, &groups); err != nil || len(groups) == 0 {
		t.Fatalf("administrator group list was not a non-empty array")
	}
	usersBody := harness.doRaw(http.MethodGet, "/api/users", owner.Token, nil, http.StatusOK)
	if bytes.Contains(usersBody, []byte("passwordHash")) || bytes.Contains(usersBody, []byte("token")) {
		t.Fatal("administrator user list exposed password or token fields")
	}
	harness.expectError(http.MethodGet, "/api/groups", userOne.Token, nil, http.StatusForbidden, "forbidden")

	userOneSubscription := harness.createSubscription(userOne, groupID, "p1-user-one-panel-subscription")
	userTwoSubscription := harness.createSubscription(userTwo, groupID, "p1-user-two-panel-subscription")
	userOneBody := harness.subscriptionBody(userOneSubscription.Token, "")
	userTwoBody := harness.subscriptionBody(userTwoSubscription.Token, "")
	assertPanelCredentialPairsDiffer(t, userOneBody, userTwoBody)
	harness.expectError(http.MethodGet, "/sub/"+userOneSubscription.Token, userTwo.Token, nil, http.StatusForbidden, "forbidden")
	harness.expectError(http.MethodPut, fmt.Sprintf("/api/my/subscriptions/%d/overrides/%d", userOneSubscription.ID, node.ID), userTwo.Token, map[string]any{"displayName": "cross-user", "params": map[string]any{"server": "127.0.0.1"}}, http.StatusNotFound, "not_found")

	if status := harness.agentConfigStatus(node.ID, node.AgentCredential); status != http.StatusNotFound {
		t.Fatalf("agent config before explicit deployment status = %d, want %d", status, http.StatusNotFound)
	}
	harness.saveTopology(owner, node.ID, []map[string]int64{})
	firstPreview := harness.previewTopology(owner, node.ID)
	for _, secret := range []string{serverPSK, realityKeys.privateKey, realityKeys.publicKey, tlsMaterial.certificatePath, tlsMaterial.keyPath} {
		if secret != "" && bytes.Contains(firstPreview.Config, []byte(secret)) {
			t.Fatalf("redacted topology preview leaked test-owned material")
		}
	}
	harness.saveTopology(owner, node.ID, []map[string]int64{})
	harness.expectError(http.MethodPost, fmt.Sprintf("/api/topology/%d/deploy", node.ID), owner.Token, map[string]string{"version": firstPreview.Version}, http.StatusConflict, "stale_preview")
	secondPreview := harness.previewTopology(owner, node.ID)
	harness.deployTopology(owner, node.ID, secondPreview.Version)
	deployed := harness.agentConfig(node.ID, node.AgentCredential)
	if deployed.Version == "" || !deployed.Explicit || deployed.NodeID != node.ID {
		t.Fatal("agent did not receive the explicitly deployed panel document")
	}
	if err := runAcceptanceSingboxCheck(t, deployed.Singbox); err != nil {
		t.Fatalf("panel-rendered sing-box configuration was rejected: %v", err)
	}

	marker := acceptanceResponseMarker(t)
	target := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/p1-ready":
			response.WriteHeader(http.StatusNoContent)
		case "/p1-marker":
			response.WriteHeader(http.StatusOK)
			_, _ = response.Write([]byte(marker))
		default:
			http.NotFound(response, request)
		}
	}))
	t.Cleanup(target.Close)
	startAcceptanceSingbox(t, deployed.Singbox)
	for _, protocol := range []string{"vless-reality", "shadowsocks", "hysteria2"} {
		runPanelProtocolExchange(t, userOneBody, protocol, target.URL, marker, serverPSK)
	}

	previousVersion := deployed.Version
	harness.saveTopology(owner, node.ID, []map[string]int64{})
	nextPreview := harness.previewTopology(owner, node.ID)
	if _, err := harness.db.ExecContext(context.Background(), `
		CREATE FUNCTION p1_acceptance_reject_deployment() RETURNS trigger
		LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'p1 acceptance deployment rejection'; END; $$`); err != nil {
		t.Fatalf("create deployment rejection function: %v", err)
	}
	if _, err := harness.db.ExecContext(context.Background(), `
		CREATE TRIGGER p1_acceptance_reject_deployment
		BEFORE INSERT OR UPDATE ON topology_deployments
		FOR EACH ROW EXECUTE FUNCTION p1_acceptance_reject_deployment()`); err != nil {
		t.Fatalf("create deployment rejection trigger: %v", err)
	}
	harness.expectError(http.MethodPost, fmt.Sprintf("/api/topology/%d/deploy", node.ID), owner.Token, map[string]string{"version": nextPreview.Version}, http.StatusInternalServerError, "internal")
	unchanged := harness.agentConfig(node.ID, node.AgentCredential)
	if unchanged.Version != previousVersion {
		t.Fatal("failed deployment replaced the previous last-good agent snapshot")
	}

	harness.setUserGroups(owner, userTwo.ID, []int64{})
	harness.expectError(http.MethodGet, "/sub/"+userTwoSubscription.Token, "", nil, http.StatusForbidden, "group_not_authorized")
	refreshed := harness.agentConfig(node.ID, node.AgentCredential)
	if bytes.Contains(refreshed.Singbox, []byte(userTwo.Username)) || !bytes.Contains(refreshed.Singbox, []byte(userOne.Username)) {
		t.Fatal("agent credential roster did not remove revoked group identity")
	}
	if _, err := harness.db.ExecContext(context.Background(), `UPDATE users SET is_active=FALSE WHERE id=$1`, userOne.ID); err != nil {
		t.Fatalf("disable test user: %v", err)
	}
	harness.expectError(http.MethodGet, "/sub/"+userOneSubscription.Token, "", nil, http.StatusNotFound, "not_found")
}

func TestPanelFinalUserRevocationAppliesFetchedDenyAllDocument(t *testing.T) {
	harness := newPanelHarness(t)
	owner := harness.bootstrapOwner()
	groupID := harness.createGroup(owner, "p1-panel-final-revocation-group")
	serverPSK := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0x51}, 16))
	ss2022Port := reserveAcceptanceTCPPort(t)
	node := harness.createNode(owner, "p1-panel-final-revocation-node", "127.0.0.1")
	harness.createInbound(owner, node.ID, "ss2022-final-user", "shadowsocks", "entry", "127.0.0.1", ss2022Port, json.RawMessage(fmt.Sprintf(`{"method":"2022-blake3-aes-128-gcm","password":%q}`, serverPSK)))
	harness.setGroupNodes(owner, groupID, []int64{node.ID})
	user := harness.register("p1-panel-final-user", "Final-user-password-4!", "p1-final-user@example.invalid", harness.createInvite(owner, groupID))
	subscription := harness.createSubscription(user, groupID, "p1-final-user-subscription")
	subscriptionBody := harness.subscriptionBody(subscription.Token, "")

	harness.saveTopology(owner, node.ID, []map[string]int64{})
	preview := harness.previewTopology(owner, node.ID)
	harness.deployTopology(owner, node.ID, preview.Version)
	deployed := harness.agentConfig(node.ID, node.AgentCredential)
	if err := runAcceptanceSingboxCheck(t, deployed.Singbox); err != nil {
		t.Fatalf("initial panel-rendered sing-box configuration was rejected: %v", err)
	}

	marker := acceptanceResponseMarker(t)
	var targetRequests atomic.Int64
	target := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/p1-ready":
			targetRequests.Add(1)
			response.WriteHeader(http.StatusNoContent)
		case "/p1-marker":
			targetRequests.Add(1)
			response.WriteHeader(http.StatusOK)
			_, _ = response.Write([]byte(marker))
		default:
			http.NotFound(response, request)
		}
	}))
	t.Cleanup(target.Close)
	core := startAcceptanceSingbox(t, deployed.Singbox)
	oldUserConfig := acceptanceMihomoConfig(t, subscriptionBody, "shadowsocks", reserveAcceptanceTCPPort(t), nil)
	oldUserClient := startAcceptanceMihomo(t, oldUserConfig)
	waitForAcceptanceProxyReadiness(t, oldUserClient.proxy, target.URL+"/p1-ready")
	status, body, err := acceptanceProxyRequest(oldUserClient.proxy, http.MethodGet, target.URL+"/p1-marker")
	if err != nil || status != http.StatusOK || string(body) != marker {
		t.Fatalf("old user could not reach the target before revocation, status=%d", status)
	}
	requestsBeforeRevocation := targetRequests.Load()

	harness.setUserGroups(owner, user.ID, []int64{})
	harness.expectError(http.MethodGet, "/sub/"+subscription.Token, "", nil, http.StatusForbidden, "group_not_authorized")
	finalStatus, finalDocument := harness.fetchAgentConfig(node.ID, node.AgentCredential)
	if finalStatus != http.StatusOK {
		t.Fatalf("agent config after final-user revocation status = %d, want %d deny-all/disabled document", finalStatus, http.StatusOK)
	}
	if finalDocument.NodeID != node.ID || finalDocument.Version == "" || len(finalDocument.Singbox) == 0 || !finalDocument.Explicit {
		t.Fatal("final-user revocation returned an incomplete agent document")
	}
	if finalDocument.Version == deployed.Version || bytes.Contains(finalDocument.Singbox, []byte(user.Username)) {
		t.Fatal("final-user revocation did not produce a changed agent document without the revoked identity")
	}
	oldCredential := panelMihomoCredential(t, subscriptionBody, "shadowsocks")
	oldUserCredential := strings.TrimPrefix(oldCredential, serverPSK+":")
	if oldUserCredential == "" || oldUserCredential == oldCredential {
		t.Fatal("panel-issued SS2022 subscription omitted its independent user credential")
	}
	if bytes.Contains(finalDocument.Singbox, []byte(oldUserCredential)) {
		t.Fatal("final-user revocation left the old protocol credential in the agent document")
	}
	assertAcceptanceDenyAllDocument(t, finalDocument)
	if err := runAcceptanceSingboxCheck(t, finalDocument.Singbox); err != nil {
		t.Fatalf("fetched final-user deny-all agent configuration was rejected: %v", err)
	}

	stopAcceptanceProcess(core)
	waitForAcceptanceTCPClosed(t, ss2022Port)
	finalCore := startAcceptanceSingbox(t, finalDocument.Singbox)
	status, _, err = acceptanceProxyRequest(oldUserClient.proxy, http.MethodGet, target.URL+"/p1-marker")
	if err == nil && status == http.StatusOK {
		t.Fatal("revoked final user still reached the target after applying the fetched agent document")
	}
	if !acceptanceProcessAlive(finalCore) {
		t.Fatal("fetched final-user deny-all agent document did not keep the core running")
	}
	if targetRequests.Load() != requestsBeforeRevocation {
		t.Fatal("target observed egress from the revoked final user after agent document application")
	}
}

func assertAcceptanceDenyAllDocument(t *testing.T, document agentConfigResponse) {
	t.Helper()
	var topology struct {
		Inbounds    []json.RawMessage `json:"inbounds"`
		Credentials []json.RawMessage `json:"credentials"`
	}
	if err := json.Unmarshal(document.Topology, &topology); err != nil {
		t.Fatalf("final-user revocation topology is not JSON: %v", err)
	}
	if topology.Inbounds == nil || len(topology.Inbounds) != 0 {
		t.Fatal("final-user revocation retained an entry inbound in the fetched topology")
	}
	if len(topology.Credentials) != 0 {
		t.Fatal("final-user revocation retained user credentials in the fetched topology")
	}
	var singbox struct {
		Inbounds []json.RawMessage `json:"inbounds"`
	}
	if err := json.Unmarshal(document.Singbox, &singbox); err != nil {
		t.Fatalf("final-user revocation sing-box document is not JSON: %v", err)
	}
	if singbox.Inbounds == nil || len(singbox.Inbounds) != 0 {
		t.Fatal("final-user revocation retained a sing-box inbound listener")
	}
	for _, reserved := range []string{
		"routing-placeholder",
		"00000000-0000-4000-8000-000000000000",
		"redacted",
	} {
		if bytes.Contains(document.Singbox, []byte(reserved)) {
			t.Fatalf("final-user revocation used a predictable reserved credential")
		}
	}
}

func TestPanelTwoHopRouteAndLandingFailureAcceptance(t *testing.T) {
	harness := newPanelHarness(t)
	owner := harness.bootstrapOwner()
	groupID := harness.createGroup(owner, "p1-panel-two-hop-group")
	serverPSK := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0x21}, 16))
	landingPSK := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0x31}, 16))
	sourcePort := reserveAcceptanceTCPPort(t)
	landingPort := reserveAcceptanceTCPPort(t)
	source := harness.createNode(owner, "p1-entry-node", "127.0.0.1")
	landing := harness.createNode(owner, "p1-landing-node", "127.0.0.1")
	sourceInbound := harness.createInbound(owner, source.ID, "entry", "shadowsocks", "entry", "127.0.0.1", sourcePort, json.RawMessage(fmt.Sprintf(`{"method":"2022-blake3-aes-128-gcm","password":%q}`, serverPSK)))
	landingInbound := harness.createInbound(owner, landing.ID, "landing", "shadowsocks", "landing", "127.0.0.1", landingPort, json.RawMessage(fmt.Sprintf(`{"method":"2022-blake3-aes-128-gcm","password":%q}`, landingPSK)))
	harness.setGroupNodes(owner, groupID, []int64{source.ID})
	user := harness.register("p1-panel-two-hop-user", "Two-hop-password-3!", "p1-two-hop@example.invalid", harness.createInvite(owner, groupID))
	subscription := harness.createSubscription(user, groupID, "p1-two-hop-subscription")
	subscriptionBody := harness.subscriptionBody(subscription.Token, "")

	harness.saveTopology(owner, landing.ID, []map[string]int64{})
	landingPreview := harness.previewTopology(owner, landing.ID)
	harness.deployTopology(owner, landing.ID, landingPreview.Version)
	harness.saveTopology(owner, source.ID, []map[string]int64{{
		"fromInboundId": sourceInbound,
		"toNodeId":      landing.ID,
		"toInboundId":   landingInbound,
	}})
	sourcePreview := harness.previewTopology(owner, source.ID)
	harness.deployTopology(owner, source.ID, sourcePreview.Version)
	landingDocument := harness.agentConfig(landing.ID, landing.AgentCredential)
	sourceDocument := harness.agentConfig(source.ID, source.AgentCredential)
	if err := runAcceptanceSingboxCheck(t, landingDocument.Singbox); err != nil {
		t.Fatalf("landing panel-rendered sing-box configuration was rejected: %v", err)
	}
	if err := runAcceptanceSingboxCheck(t, sourceDocument.Singbox); err != nil {
		t.Fatalf("entry panel-rendered sing-box configuration was rejected: %v", err)
	}

	marker := acceptanceResponseMarker(t)
	var targetRequests atomic.Int64
	target := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/p1-ready":
			targetRequests.Add(1)
			response.WriteHeader(http.StatusNoContent)
		case "/p1-marker":
			targetRequests.Add(1)
			response.WriteHeader(http.StatusOK)
			_, _ = response.Write([]byte(marker))
		default:
			http.NotFound(response, request)
		}
	}))
	t.Cleanup(target.Close)
	landingProcess := startAcceptanceSingbox(t, landingDocument.Singbox)
	startAcceptanceSingbox(t, sourceDocument.Singbox)

	goodConfig := acceptanceMihomoConfig(t, subscriptionBody, "shadowsocks", reserveAcceptanceTCPPort(t), nil)
	goodClient := startAcceptanceMihomo(t, goodConfig)
	waitForAcceptanceProxyReadiness(t, goodClient.proxy, target.URL+"/p1-ready")
	positiveStatus, positiveBody, positiveErr := acceptanceProxyRequest(goodClient.proxy, http.MethodGet, target.URL+"/p1-marker")
	if positiveErr != nil || positiveStatus != http.StatusOK || string(positiveBody) != marker {
		t.Fatalf("two-hop positive exchange failed with status %d", positiveStatus)
	}
	positiveTargetRequests := targetRequests.Load()
	stopAcceptanceProcess(goodClient.process)

	stopAcceptanceProcess(landingProcess)
	waitForAcceptanceTCPClosed(t, landingPort)
	negativeConfig := acceptanceMihomoConfig(t, subscriptionBody, "shadowsocks", reserveAcceptanceTCPPort(t), nil)
	negativeClient := startAcceptanceMihomo(t, negativeConfig)
	negativeStatus, _, negativeErr := acceptanceProxyRequest(negativeClient.proxy, http.MethodGet, target.URL+"/p1-marker")
	if negativeErr == nil && negativeStatus == http.StatusOK {
		t.Fatal("two-hop request reached the target after the landing hop stopped")
	}
	if targetRequests.Load() != positiveTargetRequests {
		t.Fatal("target observed egress after the landing hop stopped")
	}
	t.Logf("two-hop target requests before landing stop=%d after failed request=%d", positiveTargetRequests, targetRequests.Load())
}

func TestPanelNegativeAuthorizationValidationAndAgentCredentialAcceptance(t *testing.T) {
	harness := newPanelHarness(t)
	owner := harness.bootstrapOwner()
	groupID := harness.createGroup(owner, "p1-panel-negative-group")
	node := harness.createNode(owner, "p1-panel-negative-node", "127.0.0.1")

	for name, request := range map[string]map[string]any{
		"unsupported protocol": {
			"name": "unsupported", "protocol": "not-supported", "role": "entry", "listenAddr": "127.0.0.1", "listenPort": 18388,
			"config": map[string]any{},
		},
		"port zero": {
			"name": "zero-port", "protocol": "shadowsocks", "role": "entry", "listenAddr": "127.0.0.1", "listenPort": 0,
			"config": map[string]any{"method": "2022-blake3-aes-128-gcm", "password": "synthetic-server-psk"},
		},
		"port above range": {
			"name": "high-port", "protocol": "shadowsocks", "role": "entry", "listenAddr": "127.0.0.1", "listenPort": 65536,
			"config": map[string]any{"method": "2022-blake3-aes-128-gcm", "password": "synthetic-server-psk"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			harness.expectError(http.MethodPost, fmt.Sprintf("/api/nodes/%d/inbounds", node.ID), owner.Token, request, http.StatusBadRequest, "bad_request")
		})
	}

	inboundsBody := harness.doRaw(http.MethodGet, fmt.Sprintf("/api/nodes/%d/inbounds", node.ID), owner.Token, nil, http.StatusOK)
	var inbounds []json.RawMessage
	if err := json.Unmarshal(inboundsBody, &inbounds); err != nil {
		t.Fatalf("empty inbound list is not JSON: %v", err)
	}
	if inbounds == nil || len(inbounds) != 0 {
		t.Fatalf("empty inbound list = %d entries, want a non-nil empty array", len(inbounds))
	}

	user := harness.register("p1-panel-negative-user", "Negative-user-password-5!", "p1-negative@example.invalid", harness.createInvite(owner, groupID))
	harness.expectError(http.MethodGet, "/api/nodes", user.Token, nil, http.StatusForbidden, "forbidden")
	harness.expectError(http.MethodGet, "/api/groups", user.Token, nil, http.StatusForbidden, "forbidden")
	if status := harness.agentConfigStatus(node.ID, "synthetic-wrong-agent-credential"); status != http.StatusUnauthorized {
		t.Fatalf("wrong agent credential status = %d, want %d", status, http.StatusUnauthorized)
	}
}

type acceptanceTLSMaterial struct {
	address         string
	certificatePath string
	keyPath         string
}

type acceptanceRealityKeys struct {
	privateKey string
	publicKey  string
}

type acceptanceMihomoDocument struct {
	Proxies []map[string]any        `yaml:"proxies"`
	Groups  []acceptanceMihomoGroup `yaml:"proxy-groups"`
	Rules   []string                `yaml:"rules"`
}

type acceptanceMihomoGroup struct {
	Name    string   `yaml:"name"`
	Type    string   `yaml:"type"`
	Proxies []string `yaml:"proxies"`
}

type acceptanceMihomoProcess struct {
	process *exec.Cmd
	proxy   *http.Client
}

func newAcceptanceTLSMaterial(t *testing.T) acceptanceTLSMaterial {
	t.Helper()
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate TLS test key: %v", err)
	}
	serial := big.NewInt(time.Now().UnixNano())
	now := time.Now()
	certificateTemplate := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "example.com"},
		DNSNames:     []string{"example.com"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		NotBefore:    now.Add(-time.Minute),
		NotAfter:     now.Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	certificate, err := x509.CreateCertificate(rand.Reader, certificateTemplate, certificateTemplate, &privateKey.PublicKey, privateKey)
	if err != nil {
		t.Fatalf("create TLS test certificate: %v", err)
	}
	keyBytes, err := x509.MarshalECPrivateKey(privateKey)
	if err != nil {
		t.Fatalf("marshal TLS test key: %v", err)
	}
	workDir := t.TempDir()
	certificatePath := filepath.Join(workDir, "test-certificate.pem")
	keyPath := filepath.Join(workDir, "test-key.pem")
	if err := os.WriteFile(certificatePath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate}), 0600); err != nil {
		t.Fatalf("write TLS test certificate: %v", err)
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes}), 0600); err != nil {
		t.Fatalf("write TLS test key: %v", err)
	}
	certificatePair, err := tls.X509KeyPair(mustReadAcceptanceFile(t, certificatePath), mustReadAcceptanceFile(t, keyPath))
	if err != nil {
		t.Fatalf("load TLS test certificate: %v", err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen for Reality handshake target: %v", err)
	}
	tlsListener := tls.NewListener(listener, &tls.Config{Certificates: []tls.Certificate{certificatePair}})
	server := &http.Server{Handler: http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.WriteHeader(http.StatusNoContent)
	})}
	go func() { _ = server.Serve(tlsListener) }()
	t.Cleanup(func() {
		shutdownContext, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownContext)
	})
	return acceptanceTLSMaterial{address: listener.Addr().String(), certificatePath: certificatePath, keyPath: keyPath}
}

func mustReadAcceptanceFile(t *testing.T, path string) []byte {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read test fixture: %v", err)
	}
	return contents
}

func generateAcceptanceRealityKeys(t *testing.T) acceptanceRealityKeys {
	t.Helper()
	if _, err := os.Stat(acceptanceSingboxPath); err != nil {
		t.Fatalf("pinned sing-box binary is unavailable")
	}
	command := exec.Command(acceptanceSingboxPath, "generate", "reality-keypair")
	output, err := command.Output()
	if err != nil {
		t.Fatalf("pinned sing-box Reality key generation failed")
	}
	keys := acceptanceRealityKeys{}
	for _, line := range strings.Split(string(output), "\n") {
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		switch strings.TrimSpace(parts[0]) {
		case "PrivateKey":
			keys.privateKey = strings.TrimSpace(parts[1])
		case "PublicKey":
			keys.publicKey = strings.TrimSpace(parts[1])
		}
	}
	if keys.privateKey == "" || keys.publicKey == "" {
		t.Fatalf("pinned sing-box Reality key generation omitted key material")
	}
	return keys
}

func (h *panelHarness) subscriptionBody(token, authorization string) []byte {
	h.t.Helper()
	request, err := http.NewRequest(http.MethodGet, h.server.URL+"/sub/"+token, nil)
	if err != nil {
		h.t.Fatalf("create subscription request: %v", err)
	}
	if authorization != "" {
		request.Header.Set("Authorization", "Bearer "+authorization)
	}
	response, err := h.client.Do(request)
	if err != nil {
		h.t.Fatalf("subscription request failed: %v", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 8<<20))
	if err != nil {
		h.t.Fatalf("read subscription response: %v", err)
	}
	if response.StatusCode != http.StatusOK {
		h.t.Fatalf("subscription status = %d, want %d", response.StatusCode, http.StatusOK)
	}
	return body
}

func (h *panelHarness) agentConfigStatus(nodeID int64, credential string) int {
	h.t.Helper()
	request, err := http.NewRequest(http.MethodGet, h.server.URL+"/api/agent/config", nil)
	if err != nil {
		h.t.Fatalf("create agent config status request: %v", err)
	}
	request.Header.Set("X-Node-Id", strconv.FormatInt(nodeID, 10))
	request.Header.Set("X-Agent-Credential", credential)
	response, err := h.client.Do(request)
	if err != nil {
		h.t.Fatalf("agent config status request failed: %v", err)
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 8<<20))
	return response.StatusCode
}

func assertPanelCredentialPairsDiffer(t *testing.T, first, second []byte) {
	t.Helper()
	for _, protocol := range []string{"vless-reality", "shadowsocks", "hysteria2"} {
		firstCredential := panelMihomoCredential(t, first, protocol)
		secondCredential := panelMihomoCredential(t, second, protocol)
		if firstCredential == "" || secondCredential == "" || firstCredential == secondCredential {
			t.Fatalf("panel-issued %s credentials were not distinct", protocol)
		}
	}
}

func panelMihomoCredential(t *testing.T, body []byte, protocol string) string {
	t.Helper()
	var document acceptanceMihomoDocument
	if err := yaml.Unmarshal(body, &document); err != nil {
		t.Fatalf("panel subscription YAML is invalid: %v", err)
	}
	for _, proxy := range document.Proxies {
		if acceptanceProxyProtocol(proxy) != protocol {
			continue
		}
		switch protocol {
		case "vless-reality":
			value, _ := proxy["uuid"].(string)
			return value
		case "shadowsocks", "hysteria2":
			value, _ := proxy["password"].(string)
			return value
		}
	}
	return ""
}

func acceptanceProxyProtocol(proxy map[string]any) string {
	proxyType, _ := proxy["type"].(string)
	switch proxyType {
	case "vless":
		if _, ok := proxy["reality-opts"]; ok {
			return "vless-reality"
		}
	case "ss":
		return "shadowsocks"
	case "hysteria2":
		return "hysteria2"
	}
	return ""
}

func acceptanceMihomoConfig(t *testing.T, body []byte, protocol string, mixedPort int, mutate func(map[string]any)) []byte {
	t.Helper()
	var document acceptanceMihomoDocument
	if err := yaml.Unmarshal(body, &document); err != nil {
		t.Fatalf("decode panel Mihomo YAML: %v", err)
	}
	var selected map[string]any
	for _, proxy := range document.Proxies {
		if acceptanceProxyProtocol(proxy) == protocol {
			selected = make(map[string]any, len(proxy))
			for key, value := range proxy {
				selected[key] = value
			}
			break
		}
	}
	if selected == nil {
		t.Fatalf("panel subscription omitted %s proxy", protocol)
	}
	if mutate != nil {
		mutate(selected)
	}
	name, ok := selected["name"].(string)
	if !ok || name == "" {
		t.Fatalf("panel %s proxy omitted name", protocol)
	}
	document.Proxies = []map[string]any{selected}
	if len(document.Groups) == 0 {
		t.Fatalf("panel subscription omitted proxy group")
	}
	document.Groups[0].Proxies = []string{name}
	output, err := yaml.Marshal(document)
	if err != nil {
		t.Fatalf("marshal filtered panel Mihomo YAML: %v", err)
	}
	prefix := fmt.Sprintf("log-level: info\nmixed-port: %d\n", mixedPort)
	return append([]byte(prefix), output...)
}

func runPanelProtocolExchange(t *testing.T, subscriptionBody []byte, protocol, targetURL, marker, serverPSK string) {
	t.Helper()
	goodConfig := acceptanceMihomoConfig(t, subscriptionBody, protocol, reserveAcceptanceTCPPort(t), nil)
	goodClient := startAcceptanceMihomo(t, goodConfig)
	waitForAcceptanceProxyReadiness(t, goodClient.proxy, targetURL+"/p1-ready")
	status, body, err := acceptanceProxyRequest(goodClient.proxy, http.MethodGet, targetURL+"/p1-marker")
	if err != nil || status != http.StatusOK || string(body) != marker {
		t.Fatalf("panel-issued %s proxy did not return the target marker, status=%d", protocol, status)
	}
	stopAcceptanceProcess(goodClient.process)

	wrongConfig := acceptanceMihomoConfig(t, subscriptionBody, protocol, reserveAcceptanceTCPPort(t), func(proxy map[string]any) {
		switch protocol {
		case "vless-reality":
			proxy["uuid"] = "00000000-0000-4000-8000-000000000002"
		case "shadowsocks":
			wrongCredential := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0x41}, 16))
			proxy["password"] = serverPSK + ":" + wrongCredential
		case "hysteria2":
			proxy["password"] = "p1-wrong-hysteria2-credential"
		}
	})
	wrongClient := startAcceptanceMihomo(t, wrongConfig)
	status, _, err = acceptanceProxyRequest(wrongClient.proxy, http.MethodGet, targetURL+"/p1-marker")
	if err == nil && status == http.StatusOK {
		t.Fatalf("wrong %s credential reached the target", protocol)
	}
}

func runAcceptanceSingboxCheck(t *testing.T, content []byte) error {
	t.Helper()
	if _, err := os.Stat(acceptanceSingboxPath); err != nil {
		return errors.New("pinned sing-box binary is unavailable")
	}
	path := filepath.Join(t.TempDir(), "panel-sing-box-check.json")
	if err := os.WriteFile(path, content, 0600); err != nil {
		return err
	}
	commandContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(commandContext, acceptanceSingboxPath, "check", "-c", path)
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	if err := command.Run(); err != nil {
		if commandContext.Err() != nil {
			return errors.New("sing-box check timed out")
		}
		return errors.New("sing-box check failed")
	}
	return nil
}

func startAcceptanceSingbox(t *testing.T, content []byte) *exec.Cmd {
	t.Helper()
	path := filepath.Join(t.TempDir(), "panel-sing-box.json")
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatalf("write panel sing-box document: %v", err)
	}
	ports := acceptanceSingboxTCPPorts(t, content)
	command := exec.Command(acceptanceSingboxPath, "run", "-c", path)
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	if err := command.Start(); err != nil {
		t.Fatalf("start panel sing-box process: %v", err)
	}
	t.Cleanup(func() { stopAcceptanceProcess(command) })
	if len(ports) == 0 {
		waitForAcceptanceProcessRunning(t, command)
	}
	for _, port := range ports {
		waitForAcceptanceTCPProcess(t, command, port)
	}
	return command
}

func acceptanceSingboxTCPPorts(t *testing.T, content []byte) []int {
	t.Helper()
	var document struct {
		Inbounds []struct {
			Type       string `json:"type"`
			Listen     string `json:"listen"`
			ListenPort int    `json:"listen_port"`
		} `json:"inbounds"`
	}
	if err := json.Unmarshal(content, &document); err != nil {
		t.Fatalf("decode panel sing-box document for readiness: %v", err)
	}
	ports := make([]int, 0, len(document.Inbounds))
	seen := make(map[int]struct{}, len(document.Inbounds))
	for _, inbound := range document.Inbounds {
		if inbound.Type == "hysteria2" || inbound.ListenPort < 1 || inbound.ListenPort > 65535 || inbound.Listen == "" || inbound.Listen == "0.0.0.0" || inbound.Listen == "::" || inbound.Listen == "[::]" || strings.Contains(inbound.Listen, ":") {
			continue
		}
		if _, ok := seen[inbound.ListenPort]; ok {
			continue
		}
		seen[inbound.ListenPort] = struct{}{}
		ports = append(ports, inbound.ListenPort)
	}
	return ports
}

func waitForAcceptanceProcessRunning(t *testing.T, command *exec.Cmd) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if acceptanceProcessAlive(command) {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("owned protocol process exited before startup completed")
}

func startAcceptanceMihomo(t *testing.T, content []byte) *acceptanceMihomoProcess {
	t.Helper()
	workDir := t.TempDir()
	configPath := filepath.Join(workDir, "mihomo.yaml")
	if err := os.WriteFile(configPath, content, 0600); err != nil {
		t.Fatalf("write panel Mihomo document: %v", err)
	}
	proxyPort := extractAcceptanceMixedPort(t, content)
	command := exec.Command(acceptanceMihomoPath, "-d", filepath.Join(workDir, "data"), "-f", configPath)
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	if err := command.Start(); err != nil {
		t.Fatalf("start supplied Mihomo process: %v", err)
	}
	clientTransport := &http.Transport{
		Proxy:             http.ProxyURL(&url.URL{Scheme: "http", Host: net.JoinHostPort("127.0.0.1", strconv.Itoa(proxyPort))}),
		DisableKeepAlives: true,
	}
	process := &acceptanceMihomoProcess{process: command, proxy: &http.Client{Transport: clientTransport, Timeout: 5 * time.Second}}
	t.Cleanup(func() { stopAcceptanceProcess(command) })
	waitForAcceptanceTCPProcess(t, command, proxyPort)
	return process
}

func extractAcceptanceMixedPort(t *testing.T, content []byte) int {
	t.Helper()
	var document map[string]any
	if err := yaml.Unmarshal(content, &document); err != nil {
		t.Fatalf("decode Mihomo test document: %v", err)
	}
	value, ok := document["mixed-port"].(int)
	if !ok || value < 1 || value > 65535 {
		t.Fatalf("Mihomo test document omitted a valid mixed-port")
	}
	return value
}

func waitForAcceptanceProxyReadiness(t *testing.T, client *http.Client, requestURL string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		request, err := http.NewRequest(http.MethodHead, requestURL, nil)
		if err == nil {
			request.Close = true
			response, requestErr := client.Do(request)
			if requestErr == nil {
				_, _ = io.Copy(io.Discard, response.Body)
				response.Body.Close()
				if response.StatusCode == http.StatusNoContent {
					return
				}
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("Mihomo protocol readiness exchange did not complete")
}

func acceptanceProxyRequest(client *http.Client, method, requestURL string) (int, []byte, error) {
	request, err := http.NewRequest(method, requestURL, nil)
	if err != nil {
		return 0, nil, err
	}
	request.Close = true
	response, err := client.Do(request)
	if err != nil {
		return 0, nil, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	return response.StatusCode, body, err
}

func waitForAcceptanceTCPProcess(t *testing.T, command *exec.Cmd, port int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	address := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	var lastError error
	for time.Now().Before(deadline) {
		if !acceptanceProcessAlive(command) {
			t.Fatalf("owned protocol process exited before TCP readiness on %s", address)
		}
		connection, err := net.DialTimeout("tcp", address, 100*time.Millisecond)
		if err == nil {
			_ = connection.Close()
			return
		}
		lastError = err
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("owned protocol process did not expose TCP listener %s: last dial error: %v", address, lastError)
}

func waitForAcceptanceTCPClosed(t *testing.T, port int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	address := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	for time.Now().Before(deadline) {
		connection, err := net.DialTimeout("tcp", address, 100*time.Millisecond)
		if err != nil {
			return
		}
		_ = connection.Close()
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("owned TCP listener %s did not stop", address)
}

func acceptanceProcessAlive(command *exec.Cmd) bool {
	return command != nil && command.Process != nil && command.Process.Signal(syscall.Signal(0)) == nil
}

func stopAcceptanceProcess(command *exec.Cmd) {
	if command == nil || command.Process == nil {
		return
	}
	if acceptanceProcessAlive(command) {
		_ = command.Process.Signal(syscall.SIGTERM)
	}
	done := make(chan struct{})
	go func() {
		_ = command.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		_ = command.Process.Kill()
		<-done
	}
}

func reserveAcceptanceTCPPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve TCP test port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatalf("release TCP test port: %v", err)
	}
	return port
}

func reserveAcceptanceUDPPort(t *testing.T) int {
	t.Helper()
	listener, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve UDP test port: %v", err)
	}
	port := listener.LocalAddr().(*net.UDPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatalf("release UDP test port: %v", err)
	}
	return port
}

func acceptanceResponseMarker(t *testing.T) string {
	t.Helper()
	bytesValue := make([]byte, 12)
	if _, err := rand.Read(bytesValue); err != nil {
		t.Fatalf("generate response marker: %v", err)
	}
	return "p1-marker-" + base64.RawURLEncoding.EncodeToString(bytesValue)
}

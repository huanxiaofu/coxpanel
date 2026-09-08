package generator

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/coxpanel/backend/internal/models"
	"gopkg.in/yaml.v3"
)

func TestGenerateMihomoRoundTripsSpecialNames(t *testing.T) {
	proxyName := "x: [a]\nb: c \"quoted\" 雪"
	subscriptionName := "订阅: [主]\n下一行"
	proxies := []Proxy{{
		Name:     "base name",
		Override: &OverrideData{DisplayName: proxyName},
		Node: &models.Node{
			Type:        "external",
			ExtProtocol: "shadowsocks",
			ExtParams:   json.RawMessage(`{"server":"127.0.0.1","port":"443","password":"external-secret"}`),
		},
	}}

	body, err := GenerateMihomo(proxies, subscriptionName)
	if err != nil {
		t.Fatal(err)
	}

	var document struct {
		Proxies []map[string]any `yaml:"proxies"`
		Groups  []struct {
			Name    string   `yaml:"name"`
			Proxies []string `yaml:"proxies"`
		} `yaml:"proxy-groups"`
		Rules []string `yaml:"rules"`
	}
	if err := yaml.Unmarshal(body, &document); err != nil {
		t.Fatalf("generated YAML does not parse: %v\n%s", err, body)
	}
	if len(document.Proxies) != 1 {
		t.Fatalf("proxy count = %d, want 1", len(document.Proxies))
	}
	if got := document.Proxies[0]["name"]; got != proxyName {
		t.Fatalf("proxy name = %#v, want %q", got, proxyName)
	}
	if _, ok := document.Proxies[0]["b"]; ok {
		t.Fatal("newline in proxy name created an additional YAML key")
	}
	if len(document.Groups) != 1 || document.Groups[0].Name != subscriptionName {
		t.Fatalf("proxy group name = %#v, want %q", document.Groups, subscriptionName)
	}
	if !reflect.DeepEqual(document.Groups[0].Proxies, []string{proxyName}) {
		t.Fatalf("group proxies = %#v, want %#v", document.Groups[0].Proxies, []string{proxyName})
	}
	if !reflect.DeepEqual(document.Rules, []string{"MATCH," + subscriptionName}) {
		t.Fatalf("rules = %#v, want %#v", document.Rules, []string{"MATCH," + subscriptionName})
	}
}

func TestGenerateMihomoUsesPerUserCredentialAndOmitsSharedCredential(t *testing.T) {
	proxy := Proxy{
		Name: "managed",
		Node: &models.Node{Type: "managed", PublicIP: "managed.example"},
		Inbound: &models.Inbound{
			Protocol:   "vless-reality",
			ListenPort: 443,
			Config:     json.RawMessage(`{"uuid":"shared-secret","sni":"example.com","publicKey":"server-key"}`),
		},
		Credential: "user-credential",
	}

	body, err := GenerateMihomo([]Proxy{proxy}, "subscription")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "user-credential") {
		t.Fatalf("generated YAML omitted user credential:\n%s", body)
	}
	if strings.Contains(string(body), "shared-secret") {
		t.Fatalf("generated YAML leaked inbound shared credential:\n%s", body)
	}

	proxy.Credential = ""
	body, err = GenerateMihomo([]Proxy{proxy}, "subscription")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "shared-secret") || strings.Contains(string(body), "name: managed") {
		t.Fatalf("managed proxy without a credential was not omitted:\n%s", body)
	}
}

func TestGenerateMihomoKeepsDistinctUserCredentialsForSameInbound(t *testing.T) {
	inbound := &models.Inbound{
		ID:         41,
		Protocol:   "shadowsocks",
		ListenPort: 8443,
		Config:     json.RawMessage(`{"password":"shared-inbound-password"}`),
	}
	proxies := []Proxy{
		{Name: "user one", Node: &models.Node{Type: "managed", PublicIP: "node.example"}, Inbound: inbound, Credential: "user-one-password"},
		{Name: "user two", Node: &models.Node{Type: "managed", PublicIP: "node.example"}, Inbound: inbound, Credential: "user-two-password"},
	}

	body, err := GenerateMihomo(proxies, "subscription")
	if err != nil {
		t.Fatal(err)
	}
	for _, credential := range []string{"user-one-password", "user-two-password"} {
		if !strings.Contains(string(body), credential) {
			t.Fatalf("generated YAML omitted %q:\n%s", credential, body)
		}
	}
	for _, credential := range []string{"shared-inbound-password:user-one-password", "shared-inbound-password:user-two-password"} {
		if !strings.Contains(string(body), credential) {
			t.Fatalf("generated YAML omitted required SS2022 server/user credential chain %q:\n%s", credential, body)
		}
	}
}

func TestGenerateMihomoRejectsCredentialOverrideAttack(t *testing.T) {
	proxy := Proxy{
		Name:       "managed",
		Node:       &models.Node{Type: "managed", PublicIP: "node.example"},
		Credential: "real-user-credential",
		Inbound: &models.Inbound{
			Protocol:   "vless-reality",
			ListenPort: 443,
			Config:     json.RawMessage(`{"uuid":"shared-uuid","publicKey":"server-public-key"}`),
		},
		Override: &OverrideData{Params: map[string]any{
			"uuid":       "attacker-uuid",
			"password":   "attacker-password",
			"privateKey": "attacker-private-key",
			"publicKey":  "attacker-public-key",
			"sid":        "attacker-sid",
		}},
	}

	body, err := GenerateMihomo([]Proxy{proxy}, "subscription")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "real-user-credential") {
		t.Fatalf("rendered credential missing:\n%s", body)
	}
	for _, secret := range []string{"attacker-uuid", "attacker-password", "attacker-private-key", "attacker-public-key", "attacker-sid", "shared-uuid"} {
		if strings.Contains(string(body), secret) {
			t.Fatalf("rendered credential override leaked %q:\n%s", secret, body)
		}
	}
}

func TestGenerateURIListUsesStandardProtocolURIs(t *testing.T) {
	proxies := []Proxy{
		{
			Name: "vless node",
			Node: &models.Node{Type: "managed", PublicIP: "vless.example"},
			Inbound: &models.Inbound{
				Protocol:   "vless-reality",
				ListenPort: 443,
				Config:     json.RawMessage(`{"sni":"site.example","publicKey":"public-key","shortId":"short","fingerprint":"chrome"}`),
			},
			Credential: "vless-uuid",
		},
		{
			Name: "ss node",
			Node: &models.Node{Type: "managed", PublicIP: "ss.example"},
			Inbound: &models.Inbound{
				Protocol:   "shadowsocks",
				ListenPort: 8443,
				Config:     json.RawMessage(`{"method":"2022-blake3-aes-128-gcm","password":"server-psk"}`),
			},
			Credential: "ss-password",
		},
		{
			Name: "hy2 node",
			Node: &models.Node{Type: "managed", PublicIP: "hy2.example"},
			Inbound: &models.Inbound{
				Protocol:   "hysteria2",
				ListenPort: 2053,
				Config:     json.RawMessage(`{"sni":"hy2.example"}`),
			},
			Credential: "hy2-password",
		},
	}

	body, err := GenerateURIList(proxies)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(string(body), "\n"), "\n")
	if len(lines) != len(proxies) {
		t.Fatalf("URI line count = %d, want %d:\n%s", len(lines), len(proxies), body)
	}

	vless, err := url.Parse(lines[0])
	if err != nil {
		t.Fatal(err)
	}
	if vless.Scheme != "vless" || vless.User.Username() != "vless-uuid" || vless.Host != "vless.example:443" {
		t.Fatalf("vless URI = %q", lines[0])
	}
	if got := vless.Query().Get("security"); got != "reality" {
		t.Fatalf("vless security = %q, want reality", got)
	}
	if got := vless.Query().Get("pbk"); got != "public-key" {
		t.Fatalf("vless pbk = %q, want public-key", got)
	}
	if got := vless.Query().Get("flow"); got != "xtls-rprx-vision" {
		t.Fatalf("vless flow = %q, want xtls-rprx-vision", got)
	}
	if got := vless.Fragment; got != "vless node" {
		t.Fatalf("vless name = %q, want %q", got, "vless node")
	}

	ss, err := url.Parse(lines[1])
	if err != nil {
		t.Fatal(err)
	}
	if ss.Scheme != "ss" || ss.Host != "ss.example:8443" || ss.Fragment != "ss node" {
		t.Fatalf("ss URI = %q", lines[1])
	}
	decodedSS, err := base64.RawURLEncoding.DecodeString(ss.User.Username())
	if err != nil {
		t.Fatalf("decode ss userinfo: %v", err)
	}
	if got := string(decodedSS); got != "2022-blake3-aes-128-gcm:server-psk:ss-password" {
		t.Fatalf("ss credentials = %q", got)
	}

	hy2, err := url.Parse(lines[2])
	if err != nil {
		t.Fatal(err)
	}
	if hy2.Scheme != "hy2" || hy2.Host != "hy2.example:2053" || hy2.User.Username() != "hy2-password" || hy2.Fragment != "hy2 node" {
		t.Fatalf("hy2 URI = %q", lines[2])
	}
	if got := hy2.Query().Get("sni"); got != "hy2.example" {
		t.Fatalf("hy2 sni = %q, want hy2.example", got)
	}
	if got := hy2.Query().Get("insecure"); got != "" {
		t.Fatalf("default HY2 insecure query = %q, want omitted", got)
	}
}

func TestGenerateMihomoHY2YAMLAndURIKeepTLSAndObfsSemantics(t *testing.T) {
	proxy := Proxy{
		Name: "hy2 parity",
		Node: &models.Node{Type: "managed", PublicIP: "hy2.example"},
		Inbound: &models.Inbound{
			Protocol:   "hysteria2",
			ListenPort: 2053,
			Config:     json.RawMessage(`{"sni":"hy2.example","insecure":true,"obfs":"salamander","obfsPassword":"synthetic-obfs","upMbps":25,"downMbps":50}`),
		},
		Credential: "hy2-password",
	}
	body, err := GenerateMihomo([]Proxy{proxy}, "subscription")
	if err != nil {
		t.Fatalf("GenerateMihomo() error = %v", err)
	}
	var document struct {
		Proxies []map[string]any `yaml:"proxies"`
	}
	if err := yaml.Unmarshal(body, &document); err != nil {
		t.Fatalf("generated YAML does not parse: %v\n%s", err, body)
	}
	if len(document.Proxies) != 1 {
		t.Fatalf("proxy count = %d, want 1", len(document.Proxies))
	}
	proxyMap := document.Proxies[0]
	if proxyMap["skip-cert-verify"] != true {
		t.Fatalf("YAML insecure setting = %#v, want true", proxyMap["skip-cert-verify"])
	}
	if proxyMap["obfs"] != "salamander" || proxyMap["obfs-password"] != "synthetic-obfs" {
		t.Fatalf("YAML obfs settings = %#v, want salamander/password", proxyMap)
	}
	if proxyMap["up"] != "25 Mbps" || proxyMap["down"] != "50 Mbps" {
		t.Fatalf("YAML bandwidth settings = %#v, want 25/50 Mbps", proxyMap)
	}

	raw, err := GenerateURIList([]Proxy{proxy})
	if err != nil {
		t.Fatalf("GenerateURIList() error = %v", err)
	}
	uri, err := url.Parse(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatalf("parse HY2 URI: %v", err)
	}
	if uri.Query().Get("insecure") != "1" || uri.Query().Get("obfs") != "salamander" || uri.Query().Get("obfs-password") != "synthetic-obfs" {
		t.Fatalf("URI HY2 settings = %s, want insecure/obfs parity", uri)
	}
}

func TestGenerateMihomoExternalOverridesAffectYAMLAndURI(t *testing.T) {
	proxy := Proxy{
		Name: "external",
		Node: &models.Node{
			Type:        "external",
			ExtProtocol: "hysteria2",
			ExtParams:   json.RawMessage(`{"server":"original.example","port":443,"password":"external-password","sni":"original.example"}`),
		},
		Override: &OverrideData{Params: map[string]any{
			"server":       "override.example",
			"port":         float64(8443),
			"sni":          "override.example",
			"insecure":     true,
			"obfs":         "salamander",
			"obfsPassword": "override-obfs",
		}},
	}
	body, err := GenerateMihomo([]Proxy{proxy}, "subscription")
	if err != nil {
		t.Fatalf("GenerateMihomo() error = %v", err)
	}
	var document struct {
		Proxies []map[string]any `yaml:"proxies"`
	}
	if err := yaml.Unmarshal(body, &document); err != nil {
		t.Fatalf("generated YAML does not parse: %v", err)
	}
	if len(document.Proxies) != 1 {
		t.Fatalf("proxy count = %d, want 1", len(document.Proxies))
	}
	proxyMap := document.Proxies[0]
	if proxyMap["server"] != "override.example" || proxyMap["port"] != 8443 || proxyMap["sni"] != "override.example" || proxyMap["skip-cert-verify"] != true {
		t.Fatalf("external YAML override was not applied: %#v", proxyMap)
	}

	raw, err := GenerateURIList([]Proxy{proxy})
	if err != nil {
		t.Fatalf("GenerateURIList() error = %v", err)
	}
	uri, err := url.Parse(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatalf("parse external HY2 URI: %v", err)
	}
	if uri.Host != "override.example:8443" || uri.Query().Get("sni") != "override.example" || uri.Query().Get("insecure") != "1" || uri.Query().Get("obfs-password") != "override-obfs" {
		t.Fatalf("external URI override was not applied: %s", uri)
	}
}

func TestGenerateMihomoSS2022UsesServerAndUserPSKChain(t *testing.T) {
	proxy := Proxy{
		Name: "ss2022",
		Node: &models.Node{Type: "managed", PublicIP: "ss.example"},
		Inbound: &models.Inbound{
			Protocol:   "shadowsocks",
			ListenPort: 8388,
			Config:     json.RawMessage(`{"method":"2022-blake3-aes-128-gcm","password":"server-psk"}`),
		},
		Credential: "user-psk",
	}
	body, err := GenerateMihomo([]Proxy{proxy}, "subscription")
	if err != nil {
		t.Fatalf("GenerateMihomo() error = %v", err)
	}
	var document struct {
		Proxies []map[string]any `yaml:"proxies"`
	}
	if err := yaml.Unmarshal(body, &document); err != nil {
		t.Fatalf("generated YAML does not parse: %v", err)
	}
	password, ok := document.Proxies[0]["password"].(string)
	if !ok || password != "server-psk:user-psk" {
		t.Fatalf("SS2022 client password = %#v, want server:user chain", document.Proxies[0]["password"])
	}
	if password == "user-psk" {
		t.Fatal("SS2022 client password omitted required server PSK")
	}
}

func TestGenerateMihomoSS2022PassesLocalSingBoxHandshake(t *testing.T) {
	mihomoPath := os.Getenv("P2_TEST_MIHOMO")
	if mihomoPath == "" {
		mihomoPath = "/work/tools/mihomo"
	}
	singBoxPath := os.Getenv("COXPANEL_REAL_SINGBOX")
	if singBoxPath == "" {
		singBoxPath = "/workspace/tmp/sing-box-audit/sing-box-1.13.21-linux-amd64/sing-box"
	}
	if _, err := os.Stat(mihomoPath); err != nil {
		t.Skip("pinned Mihomo binary is unavailable")
	}
	if _, err := os.Stat(singBoxPath); err != nil {
		t.Skip("pinned sing-box binary is unavailable")
	}

	serverPort := reserveTCPPort(t)
	proxyPort := reserveTCPPort(t)
	const serverPSK = "AAAAAAAAAAAAAAAAAAAAAA=="
	const userPSK = "BBBBBBBBBBBBBBBBBBBBBB=="
	workDir := t.TempDir()

	clientBody, err := GenerateMihomo([]Proxy{{
		Name: "synthetic-ss2022",
		Node: &models.Node{Type: "managed", PublicIP: "127.0.0.1"},
		Inbound: &models.Inbound{
			Protocol:   "shadowsocks",
			ListenPort: serverPort,
			Config:     json.RawMessage(`{"method":"2022-blake3-aes-128-gcm","password":"` + serverPSK + `"}`),
		},
		Credential: userPSK,
	}}, "synthetic")
	if err != nil {
		t.Fatalf("GenerateMihomo() error = %v", err)
	}
	clientBody = append([]byte("log-level: info\nmixed-port: "+strconv.Itoa(proxyPort)+"\n"), clientBody...)
	clientPath := filepath.Join(workDir, "mihomo.yaml")
	if err := os.WriteFile(clientPath, clientBody, 0600); err != nil {
		t.Fatalf("write generated Mihomo config: %v", err)
	}

	serverBody := []byte(`{"log":{"level":"debug"},"inbounds":[{"type":"shadowsocks","tag":"ss","listen":"127.0.0.1","listen_port":` + strconv.Itoa(serverPort) + `,"method":"2022-blake3-aes-128-gcm","password":"` + serverPSK + `","users":[{"name":"synthetic-user","password":"` + userPSK + `"}]}],"outbounds":[{"type":"direct","tag":"direct","bind_interface":"lo","inet4_bind_address":"127.0.0.1"}]}`)
	serverPath := filepath.Join(workDir, "sing-box.json")
	if err := os.WriteFile(serverPath, serverBody, 0600); err != nil {
		t.Fatalf("write synthetic sing-box config: %v", err)
	}
	check := exec.Command(singBoxPath, "check", "-c", serverPath)
	check.Stdout = io.Discard
	check.Stderr = io.Discard
	if err := check.Run(); err != nil {
		t.Fatalf("pinned sing-box rejected synthetic SS2022 server: %v", err)
	}

	readinessReached := make(chan struct{}, 1)
	target := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/readiness.txt":
			select {
			case readinessReached <- struct{}{}:
			default:
			}
			response.WriteHeader(http.StatusNoContent)
		case "/handshake.txt":
			response.WriteHeader(http.StatusOK)
			_, _ = response.Write([]byte("synthetic-handshake-ok\n"))
		default:
			http.NotFound(response, request)
		}
	}))
	t.Cleanup(func() {
		target.CloseClientConnections()
		target.Close()
	})

	server := exec.Command(singBoxPath, "run", "-c", serverPath)
	var serverLog processLog
	server.Stdout = &serverLog
	server.Stderr = &serverLog
	if err := server.Start(); err != nil {
		t.Fatalf("start synthetic sing-box server: %v", err)
	}
	t.Cleanup(func() {
		stopTestProcess(server)
	})
	waitForProcessLog(t, server, "inbound/shadowsocks[ss]: tcp server started at 127.0.0.1:"+strconv.Itoa(serverPort), "sing-box", &serverLog)

	client := exec.Command(mihomoPath, "-d", filepath.Join(workDir, "mihomo-data"), "-f", clientPath)
	var clientLog processLog
	client.Stdout = &clientLog
	client.Stderr = &clientLog
	if err := client.Start(); err != nil {
		t.Fatalf("start generated Mihomo client: %v", err)
	}
	t.Cleanup(func() {
		stopTestProcess(client)
	})
	waitForProcessLog(t, client, "Mixed(http+socks) proxy listening at: 127.0.0.1:"+strconv.Itoa(proxyPort), "Mihomo", &clientLog)
	waitForProcessLog(t, client, "Start initial compatible provider synthetic", "Mihomo", &clientLog)
	transport := &http.Transport{Proxy: http.ProxyURL(&url.URL{Scheme: "http", Host: net.JoinHostPort("127.0.0.1", strconv.Itoa(proxyPort))}), DisableKeepAlives: true}
	httpClient := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	waitForProxyReadiness(t, httpClient, target.URL+"/readiness.txt", readinessReached, server, serverLog.String, clientLog.String)
	requestURL := target.URL + "/handshake.txt"
	request, err := http.NewRequest(http.MethodGet, requestURL, nil)
	if err != nil {
		t.Fatalf("create generated Mihomo handshake request: %v", err)
	}
	request.Close = true
	response, err := httpClient.Do(request)
	if err != nil {
		t.Fatalf("generated Mihomo handshake request failed after readiness: %v\nsing-box log:\n%s\nMihomo log:\n%s", err, sanitizedProcessLog(serverLog.String(), serverPSK, userPSK), sanitizedProcessLog(clientLog.String(), serverPSK, userPSK))
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read generated Mihomo handshake response: %v", err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("proxied handshake response = %d %q, want %d synthetic response; headers=%v singbox_alive=%t\nsing-box log:\n%s\nMihomo log:\n%s", response.StatusCode, responseBody, http.StatusOK, response.Header, processAlive(server), sanitizedProcessLog(serverLog.String(), serverPSK, userPSK), sanitizedProcessLog(clientLog.String(), serverPSK, userPSK))
	}
	if string(responseBody) != "synthetic-handshake-ok\n" {
		t.Fatalf("proxied handshake body = %q, want %q", responseBody, "synthetic-handshake-ok\n")
	}
	t.Logf("Mihomo completed a distinct SS2022 readiness exchange before the single handshake assertion")
}

func processAlive(command *exec.Cmd) bool {
	return command != nil && command.Process != nil && command.Process.Signal(os.Signal(syscall.Signal(0))) == nil
}

func reserveTCPPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve local TCP port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatalf("release local TCP port: %v", err)
	}
	return port
}

func waitForTCP(t *testing.T, port int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	address := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	for time.Now().Before(deadline) {
		connection, err := net.DialTimeout("tcp", address, 100*time.Millisecond)
		if err == nil {
			connection.Close()
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("TCP listener %s did not become ready", address)
}

func waitForProcessLog(t *testing.T, command *exec.Cmd, marker, name string, output *processLog) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if err := command.Process.Signal(os.Signal(syscall.Signal(0))); err != nil {
			t.Fatalf("%s exited before readiness marker %q: %v\n%s log:\n%s", name, marker, err, name, sanitizedProcessLog(output.String()))
		}
		if strings.Contains(output.String(), marker) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("%s did not report readiness marker %q\n%s log:\n%s", name, marker, name, sanitizedProcessLog(output.String()))
}

func waitForProxyReadiness(t *testing.T, client *http.Client, requestURL string, reached <-chan struct{}, server *exec.Cmd, serverLog func() string, clientLog func() string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		request, err := http.NewRequest(http.MethodHead, requestURL, nil)
		if err != nil {
			t.Fatalf("create proxy readiness request: %v", err)
		}
		request.Close = true
		response, requestErr := client.Do(request)
		if requestErr == nil {
			_, _ = io.Copy(io.Discard, response.Body)
			response.Body.Close()
			if response.StatusCode == http.StatusNoContent {
				select {
				case <-reached:
					return
				default:
				}
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("Mihomo did not complete the distinct SS2022 readiness exchange; sing_box_alive=%t\nsing-box log:\n%s\nMihomo log:\n%s", processAlive(server), sanitizedProcessLog(serverLog(), "AAAAAAAAAAAAAAAAAAAAAA==", "BBBBBBBBBBBBBBBBBBBBBB=="), sanitizedProcessLog(clientLog(), "AAAAAAAAAAAAAAAAAAAAAA==", "BBBBBBBBBBBBBBBBBBBBBB=="))
}

type processLog struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (log *processLog) Write(value []byte) (int, error) {
	log.mu.Lock()
	defer log.mu.Unlock()
	return log.buffer.Write(value)
}

func (log *processLog) String() string {
	log.mu.Lock()
	defer log.mu.Unlock()
	return log.buffer.String()
}

func sanitizedProcessLog(value string, secrets ...string) string {
	for _, secret := range secrets {
		if secret != "" {
			value = strings.ReplaceAll(value, secret, "[REDACTED]")
		}
	}
	return value
}

func stopTestProcess(command *exec.Cmd) {
	if command == nil || command.Process == nil {
		return
	}
	_ = command.Process.Kill()
	_ = command.Wait()
}

func TestGenerateMihomoParsesWithPinnedClient(t *testing.T) {
	mihomoPath := os.Getenv("P2_TEST_MIHOMO")
	if mihomoPath == "" {
		mihomoPath = "/work/tools/mihomo"
	}
	if _, err := os.Stat(mihomoPath); err != nil {
		t.Skip("pinned Mihomo binary is unavailable")
	}
	body, err := GenerateMihomo([]Proxy{
		{
			Name: "synthetic-vless",
			Node: &models.Node{Type: "managed", PublicIP: "vless.example"},
			Inbound: &models.Inbound{
				Protocol:   "vless-reality",
				Role:       "entry",
				ListenPort: 443,
				Config:     json.RawMessage(`{"sni":"example.test","publicKey":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA","shortId":"0123456789abcdef"}`),
			},
			Credential: "00000000-0000-4000-8000-000000000001",
		},
		{
			Name: "synthetic-ss",
			Node: &models.Node{Type: "managed", PublicIP: "ss.example"},
			Inbound: &models.Inbound{
				Protocol:   "shadowsocks",
				Role:       "entry",
				ListenPort: 8388,
				Config:     json.RawMessage(`{"method":"2022-blake3-aes-128-gcm","password":"BBBBBBBBBBBBBBBBBBBBBB=="}`),
			},
			Credential: "AAAAAAAAAAAAAAAAAAAAAA==",
		},
		{
			Name: "synthetic-hy2",
			Node: &models.Node{Type: "managed", PublicIP: "hy2.example"},
			Inbound: &models.Inbound{
				Protocol:   "hysteria2",
				Role:       "entry",
				ListenPort: 2053,
				Config:     json.RawMessage(`{"sni":"hy2.example"}`),
			},
			Credential: "synthetic-hy2-password",
		},
	}, "synthetic-p1")
	if err != nil {
		t.Fatalf("GenerateMihomo() error = %v", err)
	}
	configPath := filepath.Join(t.TempDir(), "mihomo.yaml")
	if err := os.WriteFile(configPath, body, 0600); err != nil {
		t.Fatalf("write synthetic Mihomo config: %v", err)
	}
	command := exec.Command(mihomoPath, "-t", "-f", configPath)
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	if err := command.Run(); err != nil {
		t.Fatalf("pinned Mihomo rejected generated YAML: %v", err)
	}
}

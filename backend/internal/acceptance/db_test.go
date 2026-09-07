package acceptance

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/coxpanel/backend/internal/api"
	"github.com/coxpanel/backend/internal/auth"
	"github.com/coxpanel/backend/internal/bootstrap"
	"github.com/coxpanel/backend/internal/db"
	"github.com/coxpanel/backend/internal/repo"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

const acceptanceDBTimeout = 15 * time.Second

type acceptanceDB struct {
	db     *sql.DB
	admin  *sql.DB
	schema string
	owner  string
	marker string
}

func openAcceptanceDB(t *testing.T) *acceptanceDB {
	t.Helper()
	dsn, configured := os.LookupEnv("P1_TEST_DB_URL")
	if !configured {
		t.Skip("P1_TEST_DB_URL is unset; panel acceptance requires the approved disposable PostgreSQL")
	}
	if err := validateAcceptanceDSN(dsn); err != nil {
		t.Fatal("P1_TEST_DB_URL is invalid")
	}

	ctx, cancel := context.WithTimeout(context.Background(), acceptanceDBTimeout)
	defer cancel()
	admin, err := openAcceptanceSearchPath(ctx, dsn, "pg_catalog")
	if err != nil {
		t.Fatal("P1_TEST_DB_URL could not be opened")
	}
	schema := fmt.Sprintf("p1_acceptance_%d", time.Now().UnixNano())
	if _, err := admin.ExecContext(ctx, "CREATE SCHEMA "+quoteAcceptanceIdentifier(schema)); err != nil {
		admin.Close()
		t.Fatal("acceptance schema could not be created")
	}

	isolated, err := openAcceptanceSearchPath(ctx, dsn, schema+",pg_catalog")
	if err != nil {
		_, _ = admin.ExecContext(ctx, "DROP SCHEMA "+quoteAcceptanceIdentifier(schema)+" CASCADE")
		admin.Close()
		t.Fatal("acceptance schema pool could not be opened")
	}
	owner := "p1-panel-acceptance"
	marker := fmt.Sprintf("panel-%d", time.Now().UnixNano())
	if _, err := isolated.ExecContext(ctx, `
		CREATE TABLE p1_acceptance_resource_marker (
			owner TEXT PRIMARY KEY,
			marker TEXT NOT NULL UNIQUE,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`); err != nil {
		isolated.Close()
		_, _ = admin.ExecContext(ctx, "DROP SCHEMA "+quoteAcceptanceIdentifier(schema)+" CASCADE")
		admin.Close()
		t.Fatal("acceptance ownership marker could not be created")
	}
	if _, err := isolated.ExecContext(ctx, `INSERT INTO p1_acceptance_resource_marker(owner, marker) VALUES ($1, $2)`, owner, marker); err != nil {
		isolated.Close()
		_, _ = admin.ExecContext(ctx, "DROP SCHEMA "+quoteAcceptanceIdentifier(schema)+" CASCADE")
		admin.Close()
		t.Fatal("acceptance ownership marker could not be recorded")
	}

	resource := &acceptanceDB{db: isolated, admin: admin, schema: schema, owner: owner, marker: marker}
	t.Cleanup(func() {
		resource.db.Close()
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), acceptanceDBTimeout)
		defer cleanupCancel()
		var storedMarker string
		markerQuery := "SELECT marker FROM " + quoteAcceptanceIdentifier(resource.schema) + ".p1_acceptance_resource_marker WHERE owner=$1"
		if err := resource.admin.QueryRowContext(cleanupCtx, markerQuery, resource.owner).Scan(&storedMarker); err != nil {
			t.Errorf("acceptance ownership marker could not be verified")
			resource.admin.Close()
			return
		}
		if storedMarker != resource.marker {
			t.Errorf("acceptance ownership marker did not match")
			resource.admin.Close()
			return
		}
		if _, err := resource.admin.ExecContext(cleanupCtx, "DROP SCHEMA "+quoteAcceptanceIdentifier(resource.schema)+" CASCADE"); err != nil {
			t.Errorf("acceptance schema cleanup failed")
		}
		resource.admin.Close()
	})
	return resource
}

func validateAcceptanceDSN(dsn string) error {
	if strings.TrimSpace(dsn) == "" {
		return errors.New("database URL is blank")
	}
	if _, err := pgx.ParseConfig(dsn); err != nil {
		return errors.New("database URL is malformed")
	}
	return nil
}

func openAcceptanceSearchPath(ctx context.Context, dsn, searchPath string) (*sql.DB, error) {
	config, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, err
	}
	config.RuntimeParams["search_path"] = searchPath
	pool := stdlib.OpenDB(*config,
		stdlib.OptionAfterConnect(func(ctx context.Context, conn *pgx.Conn) error {
			_, err := conn.Exec(ctx, "SET SESSION search_path TO "+searchPath)
			return err
		}),
		stdlib.OptionResetSession(func(ctx context.Context, conn *pgx.Conn) error {
			_, err := conn.Exec(ctx, "SET SESSION search_path TO "+searchPath)
			return err
		}),
	)
	pool.SetMaxOpenConns(10)
	pool.SetMaxIdleConns(10)
	if err := pool.PingContext(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return pool, nil
}

func prepareAcceptanceSchema(t *testing.T, resource *acceptanceDB) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), acceptanceDBTimeout)
	defer cancel()
	if err := db.Migrate(ctx, resource.db); err != nil {
		t.Fatal("acceptance migrations failed")
	}
	if err := db.Migrate(ctx, resource.db); err != nil {
		t.Fatal("acceptance migrations were not idempotent")
	}
}

func quoteAcceptanceIdentifier(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

type panelHarness struct {
	t        *testing.T
	resource *acceptanceDB
	db       *sql.DB
	server   *httptest.Server
	client   *http.Client
	auth     *auth.Service
}

func newPanelHarness(t *testing.T) *panelHarness {
	t.Helper()
	resource := openAcceptanceDB(t)
	prepareAcceptanceSchema(t, resource)
	secret := fmt.Sprintf("p1-panel-acceptance-signing-secret-%d", time.Now().UnixNano())
	harness := &panelHarness{
		t:        t,
		resource: resource,
		db:       resource.db,
		auth:     auth.NewService(secret, time.Hour),
		client:   &http.Client{Timeout: 10 * time.Second},
	}
	harness.server = httptest.NewServer(api.NewRouter(api.Deps{
		AuthSvc:  harness.auth,
		Users:    repo.NewUserRepo(harness.db),
		Nodes:    repo.NewNodeRepo(harness.db),
		Subs:     repo.NewSubscriptionRepo(harness.db),
		Groups:   repo.NewGroupRepo(harness.db),
		Topology: repo.NewTopologyRepo(harness.db),
	}))
	t.Cleanup(harness.server.Close)
	return harness
}

type principal struct {
	ID       int64
	Username string
	Password string
	Token    string
}

type loginResponse struct {
	Token string `json:"token"`
	User  struct {
		ID       int64  `json:"id"`
		Username string `json:"username"`
		Role     string `json:"role"`
	} `json:"user"`
}

type idResponse struct {
	ID              int64  `json:"id"`
	Token           string `json:"token"`
	AgentCredential string `json:"agentCredential"`
}

type previewResponse struct {
	NodeID  int64           `json:"nodeId"`
	Version string          `json:"version"`
	Config  json.RawMessage `json:"config"`
}

type agentConfigResponse struct {
	SchemaVersion string          `json:"schemaVersion"`
	Version       string          `json:"version"`
	SHA256        string          `json:"sha256"`
	NodeID        int64           `json:"nodeId"`
	Explicit      bool            `json:"explicit"`
	Topology      json.RawMessage `json:"topology"`
	Singbox       json.RawMessage `json:"singbox"`
}

func (h *panelHarness) bootstrapOwner() principal {
	h.t.Helper()
	username := fmt.Sprintf("p1-owner-%d", time.Now().UnixNano())
	password := fmt.Sprintf("Owner-%d-Aa!9", time.Now().UnixNano())
	email := username + "@example.invalid"
	ctx, cancel := context.WithTimeout(context.Background(), acceptanceDBTimeout)
	defer cancel()
	if err := bootstrap.Run(ctx, h.db, bootstrap.Options{Username: username, Password: password, Email: email}); err != nil {
		h.t.Fatalf("bootstrap owner failed: %v", err)
	}
	return h.login(username, password)
}

func (h *panelHarness) login(username, password string) principal {
	h.t.Helper()
	var response loginResponse
	h.doJSON(http.MethodPost, "/api/auth/login", "", map[string]string{
		"username": username,
		"password": password,
	}, http.StatusOK, &response)
	if response.Token == "" || response.User.ID <= 0 {
		h.t.Fatal("login response omitted token or user id")
	}
	return principal{ID: response.User.ID, Username: response.User.Username, Password: password, Token: response.Token}
}

func (h *panelHarness) register(username, password, email, invite string) principal {
	h.t.Helper()
	var response loginResponse
	h.doJSON(http.MethodPost, "/api/auth/register", "", map[string]string{
		"username":   username,
		"password":   password,
		"email":      email,
		"inviteCode": invite,
	}, http.StatusCreated, &response)
	if response.Token == "" || response.User.ID <= 0 {
		h.t.Fatal("registration response omitted token or user id")
	}
	return principal{ID: response.User.ID, Username: response.User.Username, Password: password, Token: response.Token}
}

func (h *panelHarness) createGroup(owner principal, name string) int64 {
	h.t.Helper()
	var response idResponse
	h.doJSON(http.MethodPost, "/api/groups", owner.Token, map[string]string{"name": name}, http.StatusCreated, &response)
	if response.ID <= 0 {
		h.t.Fatal("group creation returned invalid id")
	}
	return response.ID
}

func (h *panelHarness) createInvite(owner principal, groupID int64) string {
	h.t.Helper()
	var response struct {
		Code string `json:"code"`
	}
	h.doJSON(http.MethodPost, "/api/invites", owner.Token, map[string]int64{"groupId": groupID}, http.StatusCreated, &response)
	if response.Code == "" {
		h.t.Fatal("invite creation returned no code")
	}
	return response.Code
}

func (h *panelHarness) createNode(owner principal, name, publicIP string) idResponse {
	h.t.Helper()
	var response idResponse
	h.doJSON(http.MethodPost, "/api/nodes", owner.Token, map[string]any{
		"name":     name,
		"type":     "managed",
		"publicIp": publicIP,
	}, http.StatusCreated, &response)
	if response.ID <= 0 || response.AgentCredential == "" {
		h.t.Fatal("managed node response omitted id or one-time agent credential")
	}
	return response
}

func (h *panelHarness) createInbound(owner principal, nodeID int64, name, protocol, role, listenAddr string, listenPort int, config json.RawMessage) int64 {
	h.t.Helper()
	var response idResponse
	body, status := h.doRawStatus(http.MethodPost, fmt.Sprintf("/api/nodes/%d/inbounds", nodeID), owner.Token, map[string]any{
		"name":       name,
		"protocol":   protocol,
		"role":       role,
		"listenAddr": listenAddr,
		"listenPort": listenPort,
		"config":     config,
	})
	if status != http.StatusCreated {
		var envelope struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		errorCode := "non-json"
		if err := json.Unmarshal(body, &envelope); err == nil && envelope.Error.Code != "" {
			errorCode = envelope.Error.Code
		}
		materialStatePresent := false
		_ = h.db.QueryRowContext(context.Background(), `SELECT EXISTS(SELECT 1 FROM topology_material_state WHERE id=TRUE)`).Scan(&materialStatePresent)
		hashFunctionPresent := false
		_ = h.db.QueryRowContext(context.Background(), `SELECT to_regprocedure('pg_catalog.hashtextextended(text,bigint)') IS NOT NULL`).Scan(&hashFunctionPresent)
		h.t.Fatalf("POST /api/nodes/%d/inbounds status = %d, want %d (error_code=%q material_state=%t hashtextextended=%t)", nodeID, status, http.StatusCreated, errorCode, materialStatePresent, hashFunctionPresent)
	}
	if err := json.Unmarshal(body, &response); err != nil {
		h.t.Fatalf("POST /api/nodes/%d/inbounds response is not JSON: %v", nodeID, err)
	}
	if response.ID <= 0 {
		h.t.Fatalf("inbound %s returned invalid id", name)
	}
	return response.ID
}

func (h *panelHarness) setGroupNodes(owner principal, groupID int64, nodeIDs []int64) {
	h.t.Helper()
	h.doJSON(http.MethodPut, fmt.Sprintf("/api/groups/%d/nodes", groupID), owner.Token, map[string][]int64{"nodeIds": nodeIDs}, http.StatusOK, nil)
}

func (h *panelHarness) setUserGroups(owner principal, userID int64, groupIDs []int64) {
	h.t.Helper()
	h.doJSON(http.MethodPut, fmt.Sprintf("/api/users/%d/groups", userID), owner.Token, map[string][]int64{"groupIds": groupIDs}, http.StatusOK, nil)
}

func (h *panelHarness) createSubscription(user principal, groupID int64, name string) idResponse {
	h.t.Helper()
	var response idResponse
	h.doJSON(http.MethodPost, "/api/my/subscriptions", user.Token, map[string]any{
		"name":        name,
		"format":      "mihomo",
		"nodeGroupId": groupID,
	}, http.StatusCreated, &response)
	if response.ID <= 0 || response.Token == "" {
		h.t.Fatal("subscription response omitted id or token")
	}
	return response
}

func (h *panelHarness) saveTopology(owner principal, nodeID int64, edges []map[string]int64) int64 {
	h.t.Helper()
	var response struct {
		NodeID   int64 `json:"nodeId"`
		Revision int64 `json:"revision"`
	}
	h.doJSON(http.MethodPut, fmt.Sprintf("/api/topology/%d", nodeID), owner.Token, map[string]any{
		"nodeId": nodeID,
		"edges":  edges,
	}, http.StatusOK, &response)
	if response.NodeID != nodeID || response.Revision <= 0 {
		h.t.Fatalf("saved topology = node %d revision %d", response.NodeID, response.Revision)
	}
	return response.Revision
}

func (h *panelHarness) previewTopology(owner principal, nodeID int64) previewResponse {
	h.t.Helper()
	var response previewResponse
	h.doJSON(http.MethodPost, fmt.Sprintf("/api/topology/%d/preview", nodeID), owner.Token, map[string]any{}, http.StatusOK, &response)
	if response.NodeID != nodeID || response.Version == "" || len(response.Config) == 0 {
		h.t.Fatal("topology preview omitted node, version, or config")
	}
	return response
}

func (h *panelHarness) deployTopology(owner principal, nodeID int64, version string) {
	h.t.Helper()
	h.doJSON(http.MethodPost, fmt.Sprintf("/api/topology/%d/deploy", nodeID), owner.Token, map[string]string{"version": version}, http.StatusOK, nil)
}

func (h *panelHarness) agentConfig(nodeID int64, credential string) agentConfigResponse {
	h.t.Helper()
	status, document := h.fetchAgentConfig(nodeID, credential)
	if status != http.StatusOK {
		h.t.Fatalf("agent config status = %d, want %d", status, http.StatusOK)
	}
	if document.NodeID != nodeID || document.Version == "" || len(document.Singbox) == 0 || !document.Explicit {
		h.t.Fatal("agent config response omitted explicit deployed document")
	}
	digest := sha256.Sum256(document.Singbox)
	if !strings.EqualFold(document.SHA256, hex.EncodeToString(digest[:])) || !strings.EqualFold(document.Version, document.SHA256) {
		h.t.Fatal("agent config response hash metadata did not bind the fetched document")
	}
	return document
}

func (h *panelHarness) fetchAgentConfig(nodeID int64, credential string) (int, agentConfigResponse) {
	h.t.Helper()
	request, err := http.NewRequest(http.MethodGet, h.server.URL+"/api/agent/config", nil)
	if err != nil {
		h.t.Fatalf("create agent config request: %v", err)
	}
	request.Header.Set("X-Node-Id", fmt.Sprintf("%d", nodeID))
	request.Header.Set("X-Agent-Credential", credential)
	response, err := h.client.Do(request)
	if err != nil {
		h.t.Fatalf("agent config request failed: %v", err)
	}
	defer response.Body.Close()
	var document agentConfigResponse
	if err := json.NewDecoder(io.LimitReader(response.Body, 8<<20)).Decode(&document); err != nil {
		if response.StatusCode == http.StatusOK {
			h.t.Fatalf("agent config response is invalid: %v", err)
		}
		return response.StatusCode, agentConfigResponse{}
	}
	return response.StatusCode, document
}

func (h *panelHarness) expectError(method, path, token string, request any, status int, code string) {
	h.t.Helper()
	body := h.doRaw(method, path, token, request, status)
	var response struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		h.t.Fatalf("error response is not JSON: %v", err)
	}
	if response.Error.Code != code {
		h.t.Fatalf("error code = %q, want %q", response.Error.Code, code)
	}
}

func (h *panelHarness) doJSON(method, path, token string, request any, status int, response any) {
	h.t.Helper()
	h.doRawDecode(method, path, token, request, status, response)
}

func (h *panelHarness) doRawDecode(method, path, token string, request any, status int, response any) []byte {
	h.t.Helper()
	body, actual := h.doRawStatus(method, path, token, request)
	if actual != status {
		h.t.Fatalf("%s %s status = %d, want %d", method, path, actual, status)
	}
	if response != nil {
		if err := json.Unmarshal(body, response); err != nil {
			h.t.Fatalf("%s %s response is not JSON: %v", method, path, err)
		}
	}
	return body
}

func (h *panelHarness) doRaw(method, path, token string, request any, status int) []byte {
	h.t.Helper()
	return h.doRawDecode(method, path, token, request, status, nil)
}

func (h *panelHarness) doRawStatus(method, path, token string, request any) ([]byte, int) {
	h.t.Helper()
	var body io.Reader
	if request != nil {
		encoded, err := json.Marshal(request)
		if err != nil {
			h.t.Fatalf("encode %s %s request: %v", method, path, err)
		}
		body = strings.NewReader(string(encoded))
	}
	httpRequest, err := http.NewRequest(method, h.server.URL+path, body)
	if err != nil {
		h.t.Fatalf("create %s %s request: %v", method, path, err)
	}
	if request != nil {
		httpRequest.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		httpRequest.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := h.client.Do(httpRequest)
	if err != nil {
		h.t.Fatalf("%s %s request failed: %v", method, path, err)
	}
	defer response.Body.Close()
	contents, err := io.ReadAll(io.LimitReader(response.Body, 8<<20))
	if err != nil {
		h.t.Fatalf("read %s %s response: %v", method, path, err)
	}
	return contents, response.StatusCode
}

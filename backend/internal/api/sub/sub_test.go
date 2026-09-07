package sub

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/coxpanel/backend/internal/auth"
	"github.com/coxpanel/backend/internal/generator"
	"github.com/coxpanel/backend/internal/models"
	"github.com/coxpanel/backend/internal/repo"
	"github.com/go-chi/chi/v5"
)

type fakeSubscriptionNodeStore struct {
	nodes    []models.Node
	inbounds map[int64][]models.Inbound
}

func (f *fakeSubscriptionNodeStore) List(_ context.Context) ([]models.Node, error) {
	return f.nodes, nil
}

func (f *fakeSubscriptionNodeStore) Get(_ context.Context, nodeID int64) (*models.Node, error) {
	for i := range f.nodes {
		if f.nodes[i].ID == nodeID {
			return &f.nodes[i], nil
		}
	}
	return nil, nil
}

func (f *fakeSubscriptionNodeStore) ListInbounds(_ context.Context, nodeID int64) ([]models.Inbound, error) {
	return f.inbounds[nodeID], nil
}

type fakeSubscriptionGroupStore struct {
	exists       bool
	authorized   bool
	nodeIDs      []int64
	lookupCalled bool
}

func (f *fakeSubscriptionGroupStore) Exists(_ context.Context, _ int64) (bool, error) {
	return f.exists, nil
}

func (f *fakeSubscriptionGroupStore) UserHasGroup(_ context.Context, _, _ int64) (bool, error) {
	return f.authorized, nil
}

func (f *fakeSubscriptionGroupStore) AuthorizedNodeIDs(_ context.Context, _, _ int64) ([]int64, error) {
	f.lookupCalled = true
	return f.nodeIDs, nil
}

type fakeSubscriptionStore struct {
	sub              *models.Subscription
	getToken         string
	listError        error
	overrides        map[int64]repo.OverrideRow
	inboundOverrides map[repo.OverrideKey]repo.OverrideRow
}

func (f *fakeSubscriptionStore) GetByToken(_ context.Context, token string) (*models.Subscription, error) {
	f.getToken = token
	if f.sub == nil {
		return nil, errors.New("subscription not found")
	}
	return f.sub, nil
}

func (f *fakeSubscriptionStore) ListOverrides(_ context.Context, _ int64) (map[int64]repo.OverrideRow, error) {
	return f.overrides, f.listError
}

func (f *fakeSubscriptionStore) ListInboundOverrides(_ context.Context, _ int64) (map[repo.OverrideKey]repo.OverrideRow, error) {
	return f.inboundOverrides, f.listError
}

type fakeSubscriptionUserStore struct {
	user *models.User
}

func (f *fakeSubscriptionUserStore) GetUser(_ context.Context, _ int64) (*models.User, error) {
	return f.user, nil
}

type fakeSubscriptionUserCredentialStore struct {
	user        *models.User
	credential  string
	credentials map[[2]int64]string
	err         error
	userID      int64
	inboundID   int64
}

func (f *fakeSubscriptionUserCredentialStore) GetUser(_ context.Context, _ int64) (*models.User, error) {
	return f.user, nil
}

func (f *fakeSubscriptionUserCredentialStore) GetUserCredential(userID, inboundID int64) (string, error) {
	f.userID = userID
	f.inboundID = inboundID
	if f.credentials != nil {
		credential, ok := f.credentials[[2]int64{userID, inboundID}]
		if !ok {
			return "", sql.ErrNoRows
		}
		return credential, nil
	}
	return f.credential, f.err
}

func newSubscriptionTestHandler(subscriber *models.Subscription, groupStore *fakeSubscriptionGroupStore, user *models.User) (*Handler, *fakeSubscriptionStore) {
	store := &fakeSubscriptionStore{sub: subscriber}
	return &Handler{
		Subs: store,
		Nodes: &fakeSubscriptionNodeStore{nodes: []models.Node{{
			ID:          1,
			Name:        "node",
			Type:        "external",
			ExtProtocol: "shadowsocks",
			ExtParams:   []byte(`{"server":"127.0.0.1","port":"1","password":"synthetic"}`),
		}}},
		Groups: groupStore,
		Auth:   auth.NewService("synthetic-subscription-secret", time.Hour),
		Users:  &fakeSubscriptionUserStore{user: user},
	}, store
}

func TestNodeIDsRejectsSubscriptionWithoutExplicitGroup(t *testing.T) {
	handler := &Handler{Groups: &fakeSubscriptionGroupStore{exists: true, authorized: true, nodeIDs: []int64{1, 2}}}
	ids, err := handler.nodeIDs(context.Background(), &models.Subscription{UserID: 1})
	if !errors.Is(err, repo.ErrNodeNotAuthorized) {
		t.Fatalf("error = %v, want ErrNodeNotAuthorized", err)
	}
	if ids != nil {
		t.Fatalf("node ids = %v, want nil", ids)
	}
}

func TestNodeIDsRejectsMissingGroup(t *testing.T) {
	groupID := int64(7)
	handler := &Handler{Groups: &fakeSubscriptionGroupStore{exists: false, authorized: true, nodeIDs: []int64{1}}}
	_, err := handler.nodeIDs(context.Background(), &models.Subscription{UserID: 1, NodeGroupID: &groupID})
	if !errors.Is(err, repo.ErrNodeGroupNotFound) {
		t.Fatalf("error = %v, want ErrNodeGroupNotFound", err)
	}
}

func TestNodeIDsRejectsRevokedGroup(t *testing.T) {
	groupID := int64(7)
	groups := &fakeSubscriptionGroupStore{exists: true, authorized: false, nodeIDs: []int64{1}}
	handler := &Handler{Groups: groups}
	_, err := handler.nodeIDs(context.Background(), &models.Subscription{UserID: 1, NodeGroupID: &groupID})
	if !errors.Is(err, repo.ErrNodeNotAuthorized) {
		t.Fatalf("error = %v, want ErrNodeNotAuthorized", err)
	}
	if groups.lookupCalled {
		t.Fatal("authorized node lookup ran for revoked group")
	}
}

func TestNodeIDsRejectsEmptyAuthorizedGroup(t *testing.T) {
	groupID := int64(7)
	handler := &Handler{Groups: &fakeSubscriptionGroupStore{exists: true, authorized: true}}
	_, err := handler.nodeIDs(context.Background(), &models.Subscription{UserID: 1, NodeGroupID: &groupID})
	if !errors.Is(err, repo.ErrNoAuthorizedNodes) {
		t.Fatalf("error = %v, want ErrNoAuthorizedNodes", err)
	}
}

func TestServeExtractsChiRouteTokenAndRejectsCrossUserToken(t *testing.T) {
	owner := &models.User{ID: 7, Active: true, Role: "user"}
	subscriber, store := newSubscriptionTestHandler(&models.Subscription{ID: 3, UserID: 7, Name: "subscription", Token: "synthetic-route-token", Format: "mihomo", NodeGroupID: func() *int64 { id := int64(9); return &id }()}, &fakeSubscriptionGroupStore{exists: true, authorized: true, nodeIDs: []int64{1}}, owner)
	router := chi.NewRouter()
	router.Get("/sub/{token}", subscriber.Serve)

	service := auth.NewService("synthetic-subscription-secret", time.Hour)
	otherToken, err := service.IssueToken(8, "other", "user")
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/sub/synthetic-route-token", nil)
	request.Header.Set("Authorization", "Bearer "+otherToken)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusForbidden)
	}
	if store.getToken != "synthetic-route-token" {
		t.Fatalf("token lookup = %q, want route token", store.getToken)
	}
}

func TestServeRejectsExpiredSubscriptionOwner(t *testing.T) {
	expired := time.Now().Add(-time.Minute)
	owner := &models.User{ID: 7, Active: true, ExpireAt: &expired}
	handler, _ := newSubscriptionTestHandler(&models.Subscription{ID: 3, UserID: 7, Token: "synthetic-expired-owner", Format: "mihomo", NodeGroupID: func() *int64 { id := int64(9); return &id }()}, &fakeSubscriptionGroupStore{exists: true, authorized: true, nodeIDs: []int64{1}}, owner)
	request := httptest.NewRequest(http.MethodGet, "/sub/synthetic-expired-owner", nil)
	request.SetPathValue("token", "synthetic-expired-owner")
	response := httptest.NewRecorder()
	handler.Serve(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNotFound)
	}
}

func TestServeRejectsDisabledSubscriptionOwner(t *testing.T) {
	owner := &models.User{ID: 7, Active: false}
	handler, _ := newSubscriptionTestHandler(&models.Subscription{ID: 3, UserID: 7, Token: "synthetic-disabled-owner", Format: "mihomo", NodeGroupID: func() *int64 { id := int64(9); return &id }()}, &fakeSubscriptionGroupStore{exists: true, authorized: true, nodeIDs: []int64{1}}, owner)
	request := httptest.NewRequest(http.MethodGet, "/sub/synthetic-disabled-owner", nil)
	request.SetPathValue("token", "synthetic-disabled-owner")
	response := httptest.NewRecorder()
	handler.Serve(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d for disabled subscription owner", response.Code, http.StatusNotFound)
	}
}

func TestServeRejectsInvalidOptionalAuthorization(t *testing.T) {
	owner := &models.User{ID: 7, Active: true}
	handler, _ := newSubscriptionTestHandler(&models.Subscription{ID: 3, UserID: 7, Token: "synthetic-invalid-auth", Format: "mihomo", NodeGroupID: func() *int64 { id := int64(9); return &id }()}, &fakeSubscriptionGroupStore{exists: true, authorized: true, nodeIDs: []int64{1}}, owner)
	request := httptest.NewRequest(http.MethodGet, "/sub/synthetic-invalid-auth", nil)
	request.SetPathValue("token", "synthetic-invalid-auth")
	request.Header.Set("Authorization", "Bearer malformed")
	response := httptest.NewRecorder()
	handler.Serve(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}

func TestServeRejectsSubscriptionWithoutOwnerDependencies(t *testing.T) {
	handler := &Handler{
		Subs: &fakeSubscriptionStore{sub: &models.Subscription{ID: 3, UserID: 7, Token: "synthetic-missing-owner", Format: "mihomo"}},
	}
	request := httptest.NewRequest(http.MethodGet, "/sub/synthetic-missing-owner", nil)
	request.SetPathValue("token", "synthetic-missing-owner")
	response := httptest.NewRecorder()
	handler.Serve(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNotFound)
	}
}

func TestServeRejectsUnauthorizedGroupWithoutEmptySubscription(t *testing.T) {
	groupID := int64(9)
	owner := &models.User{ID: 7, Active: true}
	handler, _ := newSubscriptionTestHandler(&models.Subscription{ID: 3, UserID: 7, Token: "synthetic-revoked-group", Format: "mihomo", NodeGroupID: &groupID}, &fakeSubscriptionGroupStore{exists: true, authorized: false}, owner)
	request := httptest.NewRequest(http.MethodGet, "/sub/synthetic-revoked-group", nil)
	request.SetPathValue("token", "synthetic-revoked-group")
	response := httptest.NewRecorder()
	handler.Serve(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusForbidden)
	}
	if strings.Contains(response.Body.String(), "proxies:") {
		t.Fatal("unauthorized group returned a subscription body")
	}
}

func TestServeAllowsAnonymousSubscriptionCredentialURL(t *testing.T) {
	owner := &models.User{ID: 7, Active: true}
	handler, _ := newSubscriptionTestHandler(&models.Subscription{ID: 3, UserID: 7, Name: "anonymous subscription", Token: "synthetic-anonymous-token", Format: "mihomo", NodeGroupID: func() *int64 { id := int64(9); return &id }()}, &fakeSubscriptionGroupStore{exists: true, authorized: true, nodeIDs: []int64{1}}, owner)
	request := httptest.NewRequest(http.MethodGet, "/sub/synthetic-anonymous-token", nil)
	request.SetPathValue("token", "synthetic-anonymous-token")
	response := httptest.NewRecorder()
	handler.Serve(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d for anonymous subscription URL", response.Code, http.StatusOK)
	}
}

func TestServeUsesUsersCredentialStoreWhenExplicitCredentialsUnset(t *testing.T) {
	groupID := int64(9)
	owner := &models.User{ID: 7, Active: true}
	users := &fakeSubscriptionUserCredentialStore{user: owner, credential: "user-vless-uuid"}
	store := &fakeSubscriptionStore{sub: &models.Subscription{ID: 3, UserID: owner.ID, Name: "user subscription", Format: "base64", NodeGroupID: &groupID}}
	nodes := &fakeSubscriptionNodeStore{
		nodes: []models.Node{{ID: 1, Type: "managed", PublicIP: "managed.example"}},
		inbounds: map[int64][]models.Inbound{1: {{
			ID:         41,
			NodeID:     1,
			Protocol:   "vless-reality",
			Role:       "entry",
			ListenPort: 443,
			Config:     json.RawMessage(`{"uuid":"shared-inbound-uuid","sni":"site.example","publicKey":"server-key"}`),
		}}},
	}
	handler := &Handler{
		Subs:   store,
		Nodes:  nodes,
		Groups: &fakeSubscriptionGroupStore{exists: true, authorized: true, nodeIDs: []int64{1}},
		Users:  users,
	}
	request := httptest.NewRequest(http.MethodGet, "/sub/router-compatible", nil)
	request.SetPathValue("token", "router-compatible")
	response := httptest.NewRecorder()
	handler.Serve(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if got := response.Header().Get("Content-Type"); got != "text/plain; charset=utf-8" {
		t.Fatalf("content type = %q, want text/plain; charset=utf-8", got)
	}
	raw, err := base64.StdEncoding.DecodeString(response.Body.String())
	if err != nil {
		t.Fatalf("decode subscription: %v", err)
	}
	if !strings.Contains(string(raw), "vless://user-vless-uuid@managed.example:443") {
		t.Fatalf("URI list omitted user credential:\n%s", raw)
	}
	if strings.Contains(string(raw), "shared-inbound-uuid") {
		t.Fatalf("URI list leaked inbound shared credential:\n%s", raw)
	}
	if users.userID != owner.ID || users.inboundID != 41 {
		t.Fatalf("credential lookup = (%d, %d), want (%d, %d)", users.userID, users.inboundID, owner.ID, 41)
	}
}

func TestServeOmitsManagedProxyWhenUsersLacksCredentialStore(t *testing.T) {
	groupID := int64(9)
	owner := &models.User{ID: 7, Active: true}
	handler := &Handler{
		Subs: &fakeSubscriptionStore{sub: &models.Subscription{ID: 3, UserID: owner.ID, Name: "no credentials", Format: "mihomo", NodeGroupID: &groupID}},
		Nodes: &fakeSubscriptionNodeStore{
			nodes: []models.Node{{ID: 1, Type: "managed", PublicIP: "managed.example"}},
			inbounds: map[int64][]models.Inbound{1: {{
				ID:         41,
				NodeID:     1,
				Protocol:   "shadowsocks",
				Role:       "entry",
				ListenPort: 8443,
				Config:     json.RawMessage(`{"password":"shared-inbound-password"}`),
			}}},
		},
		Groups: &fakeSubscriptionGroupStore{exists: true, authorized: true, nodeIDs: []int64{1}},
		Users:  &fakeSubscriptionUserStore{user: owner},
	}
	request := httptest.NewRequest(http.MethodGet, "/sub/no-credentials", nil)
	request.SetPathValue("token", "no-credentials")
	response := httptest.NewRecorder()
	handler.Serve(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if strings.Contains(response.Body.String(), "shared-inbound-password") || strings.Contains(response.Body.String(), "managed.example") {
		t.Fatalf("managed proxy without a credential was emitted:\n%s", response.Body)
	}
}

func TestServeOmitsManagedProxyForMissingOrEmptyCredential(t *testing.T) {
	for _, testCase := range []struct {
		name       string
		credential string
		err        error
	}{
		{name: "sql no rows", err: sql.ErrNoRows},
		{name: "empty credential", credential: ""},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			groupID := int64(9)
			owner := &models.User{ID: 7, Active: true}
			users := &fakeSubscriptionUserCredentialStore{user: owner, credential: testCase.credential, err: testCase.err}
			handler := &Handler{
				Subs: &fakeSubscriptionStore{sub: &models.Subscription{ID: 3, UserID: owner.ID, Format: "mihomo", NodeGroupID: &groupID}},
				Nodes: &fakeSubscriptionNodeStore{
					nodes:    []models.Node{{ID: 1, Type: "managed", PublicIP: "managed.example"}},
					inbounds: map[int64][]models.Inbound{1: {{ID: 41, Protocol: "hysteria2", Role: "entry", ListenPort: 2053, Config: json.RawMessage(`{"password":"shared-password"}`)}}},
				},
				Groups: &fakeSubscriptionGroupStore{exists: true, authorized: true, nodeIDs: []int64{1}},
				Users:  users,
			}
			request := httptest.NewRequest(http.MethodGet, "/sub/missing", nil)
			request.SetPathValue("token", "missing")
			response := httptest.NewRecorder()
			handler.Serve(response, request)
			if response.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
			}
			if strings.Contains(response.Body.String(), "managed.example") || strings.Contains(response.Body.String(), "shared-password") {
				t.Fatalf("proxy with %s credential was emitted:\n%s", testCase.name, response.Body)
			}
		})
	}
}

func TestServeUsesDistinctCredentialsForDistinctUsersOnSameInbound(t *testing.T) {
	groupID := int64(9)
	credentialByUser := map[[2]int64]string{{7, 41}: "user-one-password", {8, 41}: "user-two-password"}
	for _, testCase := range []struct {
		userID     int64
		credential string
	}{
		{userID: 7, credential: "user-one-password"},
		{userID: 8, credential: "user-two-password"},
	} {
		t.Run(testCase.credential, func(t *testing.T) {
			owner := &models.User{ID: testCase.userID, Active: true}
			users := &fakeSubscriptionUserCredentialStore{user: owner, credentials: credentialByUser}
			handler := &Handler{
				Subs: &fakeSubscriptionStore{sub: &models.Subscription{ID: 3, UserID: owner.ID, Format: "base64", NodeGroupID: &groupID}},
				Nodes: &fakeSubscriptionNodeStore{
					nodes:    []models.Node{{ID: 1, Type: "managed", PublicIP: "managed.example"}},
					inbounds: map[int64][]models.Inbound{1: {{ID: 41, Protocol: "shadowsocks", Role: "entry", ListenPort: 8443, Config: json.RawMessage(`{"password":"shared-password"}`)}}},
				},
				Groups: &fakeSubscriptionGroupStore{exists: true, authorized: true, nodeIDs: []int64{1}},
				Users:  users,
			}
			request := httptest.NewRequest(http.MethodGet, "/sub/distinct", nil)
			request.SetPathValue("token", "distinct")
			response := httptest.NewRecorder()
			handler.Serve(response, request)
			if response.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
			}
			raw, err := base64.StdEncoding.DecodeString(response.Body.String())
			if err != nil {
				t.Fatal(err)
			}
			lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
			if len(lines) != 1 {
				t.Fatalf("expected one subscription URI, got %d: %s", len(lines), raw)
			}
			uri, err := url.Parse(lines[0])
			if err != nil {
				t.Fatalf("parse subscription URI: %v", err)
			}
			encodedCredentials := uri.User.Username()
			decodedCredentials, err := base64.RawURLEncoding.DecodeString(encodedCredentials)
			if err != nil {
				t.Fatalf("decode SS URI credentials: %v", err)
			}
			want := "2022-blake3-aes-128-gcm:shared-password:" + testCase.credential
			if string(decodedCredentials) != want {
				t.Fatalf("credential output for user %d is wrong:\n%s", testCase.userID, raw)
			}
		})
	}
}

func TestApplyOverridesScopesInboundOverridesWithoutClobberingLegacyFallback(t *testing.T) {
	store := &fakeSubscriptionStore{
		overrides: map[int64]repo.OverrideRow{42: {
			NodeID:      42,
			DisplayName: "legacy node override",
		}},
		inboundOverrides: map[repo.OverrideKey]repo.OverrideRow{{NodeID: 42, InboundID: 41}: {
			NodeID:      42,
			InboundID:   41,
			DisplayName: "first inbound override",
		}},
	}
	handler := &Handler{Subs: store}
	proxies := []generator.Proxy{
		{Node: &models.Node{ID: 42}, Inbound: &models.Inbound{ID: 41}},
		{Node: &models.Node{ID: 42}, Inbound: &models.Inbound{ID: 43}},
	}
	if err := handler.applyOverrides(context.Background(), 9, proxies); err != nil {
		t.Fatal(err)
	}
	if proxies[0].Override == nil || proxies[0].Override.DisplayName != "first inbound override" {
		t.Fatalf("first inbound override = %#v, want scoped override", proxies[0].Override)
	}
	if proxies[1].Override == nil || proxies[1].Override.DisplayName != "legacy node override" {
		t.Fatalf("second inbound override = %#v, want legacy fallback", proxies[1].Override)
	}
}

func TestApplyOverridesKeepsOnlySafeParams(t *testing.T) {
	params := json.RawMessage(`{"sni":"safe.example","server":"safe.example","port":8443,"obfs":"salamander","obfsPassword":"safe-obfs","upMbps":10,"downMbps":20,"insecure":true,"password":"attacker-password","uuid":"attacker-uuid","privateKey":"attacker-private-key","publicKey":"attacker-public-key","sid":"attacker-sid","flow":"attacker-flow"}`)
	store := &fakeSubscriptionStore{overrides: map[int64]repo.OverrideRow{42: {
		NodeID:      42,
		DisplayName: "safe name",
		SortOrder:   3,
		Icon:        "safe-icon",
		ProxyGroup:  "safe-group",
		Params:      params,
	}}}
	handler := &Handler{Subs: store}
	proxies := []generator.Proxy{{Node: &models.Node{ID: 42}}}
	if err := handler.applyOverrides(context.Background(), 9, proxies); err != nil {
		t.Fatal(err)
	}
	if proxies[0].Override == nil {
		t.Fatal("override was not applied")
	}
	if got := proxies[0].Override.DisplayName; got != "safe name" {
		t.Fatalf("display name = %q, want safe name", got)
	}
	if got := proxies[0].Override.ProxyGroup; got != "safe-group" {
		t.Fatalf("proxy group = %q, want safe-group", got)
	}
	want := map[string]any{
		"sni":          "safe.example",
		"server":       "safe.example",
		"port":         float64(8443),
		"obfs":         "salamander",
		"obfsPassword": "safe-obfs",
		"upMbps":       float64(10),
		"downMbps":     float64(20),
		"insecure":     true,
	}
	if !reflect.DeepEqual(proxies[0].Override.Params, want) {
		t.Fatalf("filtered params = %#v, want %#v", proxies[0].Override.Params, want)
	}
}

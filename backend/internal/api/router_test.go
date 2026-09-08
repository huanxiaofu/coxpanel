package api

import (
	"context"
	"crypto/rand"
	"database/sql"
	"database/sql/driver"
	"encoding/hex"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coxpanel/backend/internal/auth"
	"github.com/coxpanel/backend/internal/repo"
)

func init() {
	sql.Register("coxpanel-router-test", routerTestDriver{})
}

type routerTestDriver struct{}

var routerTestLookupToken string

func (routerTestDriver) Open(string) (driver.Conn, error) {
	return routerTestConn{}, nil
}

type routerTestConn struct{}

func (routerTestConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepared statements are not supported")
}

func (routerTestConn) Close() error { return nil }

func (routerTestConn) Begin() (driver.Tx, error) {
	return nil, errors.New("transactions are not supported")
}

func (routerTestConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	switch {
	case strings.Contains(query, "SELECT subscription_defaults FROM node_groups"):
		return routerTestRows([]string{"subscription_defaults"}, [][]driver.Value{{[]byte(`{}`)}}), nil
	case strings.Contains(query, "FROM subscriptions WHERE token=$1"):
		if len(args) != 1 {
			return nil, errors.New("subscription token argument missing")
		}
		lookupToken, ok := args[0].Value.(string)
		if !ok {
			return nil, errors.New("subscription token argument has wrong type")
		}
		routerTestLookupToken = lookupToken
		return routerTestRows(
			[]string{"id", "user_id", "name", "token", "format", "node_group_id", "template_id", "created_at", "updated_at"},
			[][]driver.Value{{int64(3), int64(7), "synthetic subscription", "synthetic-route-token", "mihomo", int64(9), nil, time.Unix(1, 0), time.Unix(1, 0)}},
		), nil
	case strings.Contains(query, "FROM users WHERE id=$1"):
		return routerTestRows(
			[]string{"id", "username", "password_hash", "email", "role", "is_active", "traffic_limit_bytes", "expire_at", "created_at"},
			[][]driver.Value{{int64(7), "synthetic-owner", "", nil, "user", true, int64(0), nil, time.Unix(1, 0)}},
		), nil
	case strings.Contains(query, "SELECT EXISTS(SELECT 1 FROM node_groups WHERE id=$1)"):
		return routerTestRows([]string{"exists"}, [][]driver.Value{{true}}), nil
	case strings.Contains(query, "FROM user_node_groups") && strings.Contains(query, "SELECT EXISTS"):
		return routerTestRows([]string{"exists"}, [][]driver.Value{{true}}), nil
	case strings.Contains(query, "FROM user_node_groups ug"):
		return routerTestRows([]string{"node_id"}, [][]driver.Value{{int64(1)}}), nil
	case strings.Contains(query, "FROM nodes WHERE id=$1"):
		return routerTestRows(
			[]string{"id", "name", "type", "public_ip", "easy_ip", "ssh_host", "ssh_user", "ssh_port", "core_version", "status", "last_seen_at", "ext_protocol", "ext_params", "agent_capabilities", "config_generation", "created_at", "updated_at"},
			[][]driver.Value{{int64(1), "synthetic external", "external", "127.0.0.1", nil, nil, nil, int64(0), nil, "online", nil, "shadowsocks", []byte(`{"server":"127.0.0.1","port":"1","password":"synthetic"}`), []byte(`[]`), int64(0), time.Unix(1, 0), time.Unix(1, 0)}},
		), nil
	case strings.Contains(query, "FROM subscription_node_overrides"):
		return routerTestRows([]string{"node_id", "display_name", "sort_order", "icon", "params", "proxy_group"}, nil), nil
	case strings.Contains(query, "FROM subscription_inbound_overrides"):
		return routerTestRows([]string{"node_id", "inbound_id", "display_name", "sort_order", "icon", "params", "proxy_group"}, nil), nil
	default:
		return nil, errors.New("unexpected router test query")
	}
}

func (routerTestConn) ExecContext(context.Context, string, []driver.NamedValue) (driver.Result, error) {
	return nil, errors.New("writes are not supported")
}

var _ driver.Conn = routerTestConn{}
var _ driver.QueryerContext = routerTestConn{}
var _ driver.ExecerContext = routerTestConn{}

type routerRows struct {
	columns []string
	values  [][]driver.Value
	index   int
}

func routerTestRows(columns []string, values [][]driver.Value) *routerRows {
	return &routerRows{columns: columns, values: values}
}

func (rows *routerRows) Columns() []string { return rows.columns }

func (rows *routerRows) Close() error { return nil }

func (rows *routerRows) Next(dest []driver.Value) error {
	if rows.index >= len(rows.values) {
		return io.EOF
	}
	copy(dest, rows.values[rows.index])
	rows.index++
	return nil
}

func TestNewRouterServesSubscriptionThroughChiWithOwnerDependencies(t *testing.T) {
	routerTestLookupToken = ""
	database, err := sql.Open("coxpanel-router-test", "")
	if err != nil {
		t.Fatal("opening synthetic router database failed")
	}
	defer database.Close()

	secretBytes := make([]byte, 32)
	if _, err := rand.Read(secretBytes); err != nil {
		t.Fatal("generating synthetic router secret failed")
	}
	service := auth.NewService(hex.EncodeToString(secretBytes), time.Hour)
	token, err := service.IssueToken(7, "synthetic-owner", "user")
	if err != nil {
		t.Fatal("issuing synthetic token failed")
	}
	router := NewRouter(Deps{
		AuthSvc: service,
		Users:   repo.NewUserRepo(database),
		Nodes:   repo.NewNodeRepo(database),
		Subs:    repo.NewSubscriptionRepo(database),
		Groups:  repo.NewGroupRepo(database),
	})

	request := httptest.NewRequest(http.MethodGet, "/sub/synthetic-route-token", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if routerTestLookupToken != "synthetic-route-token" {
		t.Fatalf("subscription token lookup = %q, want route token", routerTestLookupToken)
	}
	if !strings.Contains(response.Body.String(), "synthetic external") {
		t.Fatal("subscription response did not contain the route-resolved node")
	}

	request = httptest.NewRequest(http.MethodGet, "/sub/synthetic-route-token", nil)
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("anonymous status = %d, want %d", response.Code, http.StatusOK)
	}
}

func TestNewRouterDoesNotLogSubscriptionToken(t *testing.T) {
	database, err := sql.Open("coxpanel-router-test", "")
	if err != nil {
		t.Fatal("opening synthetic router database failed")
	}
	defer database.Close()

	secretBytes := make([]byte, 32)
	if _, err := rand.Read(secretBytes); err != nil {
		t.Fatal("generating synthetic router secret failed")
	}
	service := auth.NewService(hex.EncodeToString(secretBytes), time.Hour)
	router := NewRouter(Deps{
		AuthSvc: service,
		Users:   repo.NewUserRepo(database),
		Nodes:   repo.NewNodeRepo(database),
		Subs:    repo.NewSubscriptionRepo(database),
		Groups:  repo.NewGroupRepo(database),
	})

	previousWriter := log.Writer()
	previousFlags := log.Flags()
	var output strings.Builder
	log.SetOutput(&output)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(previousWriter)
		log.SetFlags(previousFlags)
	})

	request := httptest.NewRequest(http.MethodGet, "/sub/synthetic-route-token", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if strings.Contains(output.String(), "synthetic-route-token") {
		t.Fatalf("subscription token was written to request logs: %s", output.String())
	}
}

func TestNewRouterDeniesNonAdminFromControlPlaneRoutes(t *testing.T) {
	database, err := sql.Open("coxpanel-router-test", "")
	if err != nil {
		t.Fatal("opening synthetic router database failed")
	}
	defer database.Close()

	service := auth.NewService("synthetic-router-secret-which-is-long-enough", time.Hour)
	token, err := service.IssueToken(7, "synthetic-user", "user")
	if err != nil {
		t.Fatal("issuing synthetic user token failed")
	}
	router := NewRouter(Deps{
		AuthSvc: service,
		Users:   repo.NewUserRepo(database),
		Nodes:   repo.NewNodeRepo(database),
		Subs:    repo.NewSubscriptionRepo(database),
		Groups:  repo.NewGroupRepo(database),
	})

	request := httptest.NewRequest(http.MethodGet, "/api/groups", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("non-admin control-plane status = %d, want %d", response.Code, http.StatusForbidden)
	}
}

package repo

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coxpanel/backend/internal/db"
	"github.com/coxpanel/backend/internal/models"
	sharedconfig "github.com/coxpanel/shared/config"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

const p1IntegrationTimeout = 10 * time.Second

type p1IntegrationDB struct {
	db     *sql.DB
	admin  *sql.DB
	schema string
	marker string
	owner  string
}

func openP1IntegrationDB(t *testing.T) *p1IntegrationDB {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), p1IntegrationTimeout)
	defer cancel()
	dsn, ok := os.LookupEnv("P1_TEST_DB_URL")
	if !ok {
		t.Skip("P1_TEST_DB_URL is unset; PostgreSQL integration tests are opt-in")
	}
	if err := validateP1IntegrationDSN(dsn); err != nil {
		t.Fatal("P1_TEST_DB_URL is invalid")
	}

	admin, err := openP1SearchPathPool(ctx, dsn, "pg_catalog")
	if err != nil {
		t.Fatal("P1_TEST_DB_URL could not be opened")
	}

	schema := fmt.Sprintf("p1_w1_%d", time.Now().UnixNano())
	if _, err := admin.ExecContext(ctx, "CREATE SCHEMA "+quoteP1Identifier(schema)); err != nil {
		admin.Close()
		t.Fatal("isolated schema could not be created")
	}

	isolated, err := openP1SchemaPool(ctx, dsn, schema)
	if err != nil {
		_, _ = admin.ExecContext(ctx, "DROP SCHEMA "+quoteP1Identifier(schema)+" CASCADE")
		admin.Close()
		t.Fatal("isolated schema pool could not be opened")
	}

	marker := fmt.Sprintf("w1-%d", time.Now().UnixNano())
	owner := "coxpanel-p1-w1"
	if _, err := isolated.ExecContext(ctx, `
		CREATE TABLE p1_w1_resource_marker (
			owner TEXT PRIMARY KEY,
			marker TEXT NOT NULL UNIQUE,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`); err != nil {
		isolated.Close()
		_, _ = admin.ExecContext(ctx, "DROP SCHEMA "+quoteP1Identifier(schema)+" CASCADE")
		admin.Close()
		t.Fatal("isolated resource marker could not be created")
	}
	if _, err := isolated.ExecContext(ctx, `INSERT INTO p1_w1_resource_marker(owner, marker) VALUES ($1, $2)`, owner, marker); err != nil {
		isolated.Close()
		_, _ = admin.ExecContext(ctx, "DROP SCHEMA "+quoteP1Identifier(schema)+" CASCADE")
		admin.Close()
		t.Fatal("isolated resource marker could not be recorded")
	}
	resource := &p1IntegrationDB{db: isolated, admin: admin, schema: schema, marker: marker, owner: owner}
	t.Cleanup(func() {
		resource.db.Close()
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), p1IntegrationTimeout)
		defer cleanupCancel()
		var storedMarker string
		markerQuery := "SELECT marker FROM " + quoteP1Identifier(resource.schema) + ".p1_w1_resource_marker WHERE owner=$1"
		if err := resource.admin.QueryRowContext(cleanupCtx, markerQuery, resource.owner).Scan(&storedMarker); err != nil {
			t.Errorf("isolated resource ownership marker could not be verified")
			resource.admin.Close()
			return
		}
		if storedMarker != resource.marker {
			t.Errorf("isolated resource ownership marker did not match")
			resource.admin.Close()
			return
		}
		if _, err := resource.admin.ExecContext(cleanupCtx, "DROP SCHEMA "+quoteP1Identifier(resource.schema)+" CASCADE"); err != nil {
			t.Errorf("isolated schema cleanup failed")
		}
		resource.admin.Close()
	})
	return resource
}

func validateP1IntegrationDSN(dsn string) error {
	if strings.TrimSpace(dsn) == "" {
		return errors.New("database URL is blank")
	}
	if _, err := pgx.ParseConfig(dsn); err != nil {
		return errors.New("database URL is malformed")
	}
	return nil
}

func TestP1IntegrationDSNValidationIsConnectionFree(t *testing.T) {
	if err := validateP1IntegrationDSN("   "); err == nil {
		t.Fatal("blank explicitly configured database URL was accepted")
	}
	if err := validateP1IntegrationDSN("not a database URL"); err == nil {
		t.Fatal("malformed database URL was accepted")
	}
	if err := validateP1IntegrationDSN("postgres://synthetic-user:synthetic-password@127.0.0.1:1/synthetic"); err != nil {
		t.Fatalf("valid database URL was rejected: %v", err)
	}
}

func openP1SchemaPool(ctx context.Context, dsn, schema string) (*sql.DB, error) {
	return openP1SearchPathPool(ctx, dsn, schema+",pg_catalog")
}

func openP1SearchPathPool(ctx context.Context, dsn, searchPath string) (*sql.DB, error) {
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
	pool.SetMaxOpenConns(8)
	pool.SetMaxIdleConns(8)
	if err := pool.PingContext(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return pool, nil
}

func prepareP1IntegrationSchema(t *testing.T, resource *p1IntegrationDB) {
	t.Helper()
	validateP1MigrationFiles(t)
	ctx, cancel := context.WithTimeout(context.Background(), p1IntegrationTimeout)
	defer cancel()
	assertP1SearchPath(t, resource)
	if err := db.Migrate(ctx, resource.db); err != nil {
		t.Fatal("real migrations failed")
	}
	if err := db.Migrate(ctx, resource.db); err != nil {
		t.Fatal("repeat migrations were not idempotent")
	}
	assertP1SearchPath(t, resource)

	rows, err := resource.db.QueryContext(ctx, `SELECT name FROM schema_migrations ORDER BY name`)
	if err != nil {
		t.Fatal("migration ledger could not be read")
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal("migration ledger could not be scanned")
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatal("migration ledger read failed")
	}
	expected := []string{
		"0001_init_nodes.up.sql",
		"0002_init_users.up.sql",
		"0003_init_groups.up.sql",
		"0004_init_subscriptions.up.sql",
		"0005_init_traffic.up.sql",
		"0006_user_credentials.up.sql",
		"0007_agent_credentials.up.sql",
		"0008_auth_authorization.up.sql",
		"0009_subscription_inbound_overrides.up.sql",
		"0010_control_plane.up.sql",
	}
	if fmt.Sprint(names) != fmt.Sprint(expected) {
		t.Fatalf("migration ledger = %v, want %v", names, expected)
	}
}

func assertP1SearchPath(t *testing.T, resource *p1IntegrationDB) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), p1IntegrationTimeout)
	defer cancel()
	connections := make([]*sql.Conn, 0, 8)
	defer func() {
		for _, connection := range connections {
			connection.Close()
		}
	}()
	for index := 0; index < 8; index++ {
		connection, err := resource.db.Conn(ctx)
		if err != nil {
			t.Fatal("isolated pool connection could not be acquired")
		}
		connections = append(connections, connection)
		var currentSchema, searchPath string
		if err := connection.QueryRowContext(ctx, `SELECT current_schema(), current_setting('search_path')`).Scan(&currentSchema, &searchPath); err != nil {
			t.Fatal("isolated pool search_path could not be checked")
		}
		if currentSchema != resource.schema || !strings.Contains(searchPath, resource.schema) {
			t.Fatalf("pool connection escaped isolated schema")
		}
	}
}

func validateP1MigrationFiles(t *testing.T) {
	t.Helper()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("migration source location could not be determined")
	}
	migrationDir := filepath.Join(filepath.Dir(source), "..", "db", "migrations")
	entries, err := os.ReadDir(migrationDir)
	if err != nil {
		t.Fatal("migration files could not be read")
	}
	var names []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".sql") {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	expected := []string{
		"0001_init_nodes.up.sql",
		"0002_init_users.up.sql",
		"0003_init_groups.up.sql",
		"0004_init_subscriptions.up.sql",
		"0005_init_traffic.up.sql",
		"0006_user_credentials.up.sql",
		"0007_agent_credentials.up.sql",
		"0008_auth_authorization.up.sql",
		"0009_subscription_inbound_overrides.up.sql",
		"0010_control_plane.up.sql",
	}
	if fmt.Sprint(names) != fmt.Sprint(expected) {
		t.Fatalf("migration files = %v, want exactly 0001-0010", names)
	}
	unsafeDDL := regexp.MustCompile(`(?im)^\s*(?:CREATE|ALTER|DROP)\s+(?:DATABASE|SCHEMA)\b|^\s*SET\s+(?:(?:SESSION|LOCAL)\s+)?(?:search_path|schema)\b`)
	for _, name := range expected {
		body, err := os.ReadFile(filepath.Join(migrationDir, name))
		if err != nil {
			t.Fatalf("migration %s could not be read", name)
		}
		if unsafeDDL.Match(body) {
			t.Fatalf("migration %s contains explicit database/schema control", name)
		}
	}
}

func quoteP1Identifier(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

func TestRegisterWithInviteConcurrentIntegration(t *testing.T) {
	resource := openP1IntegrationDB(t)
	prepareP1IntegrationSchema(t, resource)
	repository := NewUserRepo(resource.db)
	ctx := context.Background()
	var groupID int64
	if err := resource.db.QueryRowContext(ctx, `INSERT INTO node_groups(name) VALUES ('integration-group') RETURNING id`).Scan(&groupID); err != nil {
		t.Fatal("fixture setup failed")
	}
	if _, err := resource.db.ExecContext(ctx, `INSERT INTO invite_codes(code, max_uses, node_group_id) VALUES ('synthetic-concurrent-invite', 1, $1)`, groupID); err != nil {
		t.Fatal("fixture setup failed")
	}

	const attempts = 8
	results := make(chan error, attempts)
	var wait sync.WaitGroup
	for index := 0; index < attempts; index++ {
		index := index
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, _, err := repository.RegisterWithInvite(ctx, fmt.Sprintf("synthetic-user-%d", index), "synthetic-hash", fmt.Sprintf("synthetic-%d@example.invalid", index), "synthetic-concurrent-invite")
			results <- err
		}()
	}
	wait.Wait()
	close(results)

	successes := 0
	for err := range results {
		if err == nil {
			successes++
			continue
		}
		if !errors.Is(err, ErrInvalidInvite) {
			t.Fatalf("unexpected concurrent registration error: %v", err)
		}
	}
	if successes != 1 {
		t.Fatalf("successful registrations = %d, want 1", successes)
	}
	var usedCount, users, grants int
	if err := resource.db.QueryRowContext(ctx, `SELECT used_count FROM invite_codes WHERE code='synthetic-concurrent-invite'`).Scan(&usedCount); err != nil {
		t.Fatal("verification query failed")
	}
	if err := resource.db.QueryRowContext(ctx, `SELECT count(*) FROM users`).Scan(&users); err != nil {
		t.Fatal("verification query failed")
	}
	if err := resource.db.QueryRowContext(ctx, `SELECT count(*) FROM user_node_groups`).Scan(&grants); err != nil {
		t.Fatal("verification query failed")
	}
	if usedCount != 1 || users != 1 || grants != 1 {
		t.Fatalf("invite/users/grants = %d/%d/%d, want 1/1/1", usedCount, users, grants)
	}
}

func TestRegisterWithInviteRollsBackOnUniqueConflictIntegration(t *testing.T) {
	resource := openP1IntegrationDB(t)
	prepareP1IntegrationSchema(t, resource)
	ctx := context.Background()
	var groupID int64
	if err := resource.db.QueryRowContext(ctx, `INSERT INTO node_groups(name) VALUES ('unique-conflict-group') RETURNING id`).Scan(&groupID); err != nil {
		t.Fatal("fixture setup failed")
	}
	if _, err := resource.db.ExecContext(ctx, `INSERT INTO users(username, password_hash, email, role) VALUES ('existing-user', 'synthetic-hash', NULL, 'user')`); err != nil {
		t.Fatal("fixture setup failed")
	}
	if _, err := resource.db.ExecContext(ctx, `INSERT INTO invite_codes(code, max_uses, node_group_id) VALUES ('synthetic-unique-conflict', 1, $1)`, groupID); err != nil {
		t.Fatal("fixture setup failed")
	}
	_, _, err := NewUserRepo(resource.db).RegisterWithInvite(ctx, "existing-user", "synthetic-hash", "", "synthetic-unique-conflict")
	if err == nil {
		t.Fatal("unique conflict unexpectedly succeeded")
	}
	var usedCount, users int
	if err := resource.db.QueryRowContext(ctx, `SELECT used_count FROM invite_codes WHERE code='synthetic-unique-conflict'`).Scan(&usedCount); err != nil {
		t.Fatal("verification query failed")
	}
	if err := resource.db.QueryRowContext(ctx, `SELECT count(*) FROM users WHERE username='existing-user'`).Scan(&users); err != nil {
		t.Fatal("verification query failed")
	}
	if usedCount != 0 || users != 1 {
		t.Fatalf("invite/users = %d/%d, want 0/1 after rollback", usedCount, users)
	}
}

func TestRegisterWithInviteRollsBackAuthorizationInsertFailureIntegration(t *testing.T) {
	resource := openP1IntegrationDB(t)
	prepareP1IntegrationSchema(t, resource)
	ctx := context.Background()
	var groupID int64
	if err := resource.db.QueryRowContext(ctx, `INSERT INTO node_groups(name) VALUES ('rollback-group') RETURNING id`).Scan(&groupID); err != nil {
		t.Fatal("fixture setup failed")
	}
	if _, err := resource.db.ExecContext(ctx, `INSERT INTO invite_codes(code, max_uses, node_group_id) VALUES ('synthetic-auth-failure', 1, $1)`, groupID); err != nil {
		t.Fatal("fixture setup failed")
	}
	if _, err := resource.db.ExecContext(ctx, `
		CREATE FUNCTION reject_p1_authorization_insert() RETURNS trigger
		LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic authorization insert rejection'; END; $$`); err != nil {
		t.Fatal("fixture trigger setup failed")
	}
	if _, err := resource.db.ExecContext(ctx, `
		CREATE TRIGGER reject_p1_authorization BEFORE INSERT ON user_node_groups
		FOR EACH ROW EXECUTE FUNCTION reject_p1_authorization_insert()`); err != nil {
		t.Fatal("fixture trigger setup failed")
	}

	_, _, err := NewUserRepo(resource.db).RegisterWithInvite(ctx, "synthetic-auth-failure-user", "synthetic-hash", "", "synthetic-auth-failure")
	if err == nil {
		t.Fatal("authorization insert failure unexpectedly succeeded")
	}
	var usedCount, users, grants int
	if err := resource.db.QueryRowContext(ctx, `SELECT used_count FROM invite_codes WHERE code='synthetic-auth-failure'`).Scan(&usedCount); err != nil {
		t.Fatal("verification query failed")
	}
	if err := resource.db.QueryRowContext(ctx, `SELECT count(*) FROM users WHERE username='synthetic-auth-failure-user'`).Scan(&users); err != nil {
		t.Fatal("verification query failed")
	}
	if err := resource.db.QueryRowContext(ctx, `SELECT count(*) FROM user_node_groups`).Scan(&grants); err != nil {
		t.Fatal("verification query failed")
	}
	if usedCount != 0 || users != 0 || grants != 0 {
		t.Fatalf("invite/users/grants = %d/%d/%d, want 0/0/0 after rollback", usedCount, users, grants)
	}
}

func TestRegisterWithInviteNullableEmailIntegration(t *testing.T) {
	resource := openP1IntegrationDB(t)
	prepareP1IntegrationSchema(t, resource)
	ctx := context.Background()
	var groupID int64
	if err := resource.db.QueryRowContext(ctx, `INSERT INTO node_groups(name) VALUES ('nullable-email-group') RETURNING id`).Scan(&groupID); err != nil {
		t.Fatal("fixture setup failed")
	}
	if _, err := resource.db.ExecContext(ctx, `INSERT INTO invite_codes(code, max_uses, node_group_id) VALUES ('synthetic-null-email', 1, $1)`, groupID); err != nil {
		t.Fatal("fixture setup failed")
	}
	repository := NewUserRepo(resource.db)
	user, _, err := repository.RegisterWithInvite(ctx, "synthetic-null-email-user", "synthetic-hash", "", "synthetic-null-email")
	if err != nil {
		t.Fatal("nullable email registration failed")
	}
	if user.Email != "" {
		t.Fatalf("registered email = %q, want empty", user.Email)
	}
	loaded, err := repository.GetUser(ctx, user.ID)
	if err != nil {
		t.Fatal("nullable email user lookup failed")
	}
	if loaded.Email != "" {
		t.Fatalf("loaded email = %q, want empty", loaded.Email)
	}
	loaded, err = repository.GetByUsername(ctx, "synthetic-null-email-user")
	if err != nil {
		t.Fatal("nullable email username lookup failed")
	}
	if loaded.Email != "" {
		t.Fatalf("username lookup email = %q, want empty", loaded.Email)
	}
	var isNull bool
	if err := resource.db.QueryRowContext(ctx, `SELECT email IS NULL FROM users WHERE id=$1`, user.ID).Scan(&isNull); err != nil {
		t.Fatal("nullable email verification failed")
	}
	if !isNull {
		t.Fatal("empty email was not stored as NULL")
	}
}

func TestRegisterWithInviteRejectsExpiredAndExhaustedIntegration(t *testing.T) {
	resource := openP1IntegrationDB(t)
	prepareP1IntegrationSchema(t, resource)
	ctx := context.Background()
	past := time.Now().Add(-time.Minute)
	var groupID int64
	if err := resource.db.QueryRowContext(ctx, `INSERT INTO node_groups(name) VALUES ('invalid-invite-group') RETURNING id`).Scan(&groupID); err != nil {
		t.Fatal("fixture setup failed")
	}
	if _, err := resource.db.ExecContext(ctx, `INSERT INTO invite_codes(code, max_uses, expires_at, node_group_id) VALUES ('synthetic-expired', 1, $1, $2), ('synthetic-exhausted', 1, NULL, $2)`, past, groupID); err != nil {
		t.Fatal("fixture setup failed")
	}
	if _, err := resource.db.ExecContext(ctx, `UPDATE invite_codes SET used_count=1 WHERE code='synthetic-exhausted'`); err != nil {
		t.Fatal("fixture setup failed")
	}
	repository := NewUserRepo(resource.db)
	for _, testCase := range []struct {
		name string
		code string
	}{
		{name: "expired", code: "synthetic-expired"},
		{name: "exhausted", code: "synthetic-exhausted"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			_, _, err := repository.RegisterWithInvite(ctx, "synthetic-"+testCase.name+"-user", "synthetic-hash", "", testCase.code)
			if !errors.Is(err, ErrInvalidInvite) {
				t.Fatalf("error = %v, want ErrInvalidInvite", err)
			}
		})
	}
	var users int
	if err := resource.db.QueryRowContext(ctx, `SELECT count(*) FROM users`).Scan(&users); err != nil {
		t.Fatal("verification query failed")
	}
	if users != 0 {
		t.Fatalf("users = %d, want 0", users)
	}
}

func TestSubscriptionAuthorizationIntegration(t *testing.T) {
	resource := openP1IntegrationDB(t)
	prepareP1IntegrationSchema(t, resource)
	ctx := context.Background()
	var userID, groupID, otherGroupID, nodeID, otherNodeID int64
	if err := resource.db.QueryRowContext(ctx, `INSERT INTO users(username, password_hash, role, is_active) VALUES ('authorized-user', 'synthetic-hash', 'user', TRUE) RETURNING id`).Scan(&userID); err != nil {
		t.Fatal("fixture setup failed")
	}
	if err := resource.db.QueryRowContext(ctx, `INSERT INTO node_groups(name) VALUES ('authorized-group') RETURNING id`).Scan(&groupID); err != nil {
		t.Fatal("fixture setup failed")
	}
	if err := resource.db.QueryRowContext(ctx, `INSERT INTO node_groups(name) VALUES ('other-group') RETURNING id`).Scan(&otherGroupID); err != nil {
		t.Fatal("fixture setup failed")
	}
	if err := resource.db.QueryRowContext(ctx, `INSERT INTO nodes(name) VALUES ('authorized-node') RETURNING id`).Scan(&nodeID); err != nil {
		t.Fatal("fixture setup failed")
	}
	if err := resource.db.QueryRowContext(ctx, `INSERT INTO nodes(name) VALUES ('other-node') RETURNING id`).Scan(&otherNodeID); err != nil {
		t.Fatal("fixture setup failed")
	}
	if _, err := resource.db.ExecContext(ctx, `INSERT INTO node_group_members(group_id, node_id) VALUES ($1,$2),($3,$4)`, groupID, nodeID, otherGroupID, otherNodeID); err != nil {
		t.Fatal("fixture setup failed")
	}
	if _, err := resource.db.ExecContext(ctx, `INSERT INTO user_node_groups(user_id, group_id) VALUES ($1,$2)`, userID, groupID); err != nil {
		t.Fatal("fixture setup failed")
	}

	subs := NewSubscriptionRepo(resource.db)
	unauthorizedGroup := &models.Subscription{UserID: userID, Name: "unauthorized", Token: "synthetic-unauthorized", Format: "mihomo", NodeGroupID: &otherGroupID}
	if _, err := subs.CreateForUser(ctx, unauthorizedGroup); !errors.Is(err, ErrNodeNotAuthorized) {
		t.Fatalf("unauthorized group error = %v, want ErrNodeNotAuthorized", err)
	}
	noGroup := &models.Subscription{UserID: userID, Name: "unscoped", Token: "synthetic-unscoped", Format: "mihomo"}
	if _, err := subs.CreateForUser(ctx, noGroup); !errors.Is(err, ErrNodeNotAuthorized) {
		t.Fatalf("unscoped subscription error = %v, want ErrNodeNotAuthorized", err)
	}
	authorized := &models.Subscription{UserID: userID, Name: "authorized", Token: "synthetic-authorized", Format: "mihomo", NodeGroupID: &groupID}
	subID, err := subs.CreateForUser(ctx, authorized)
	if err != nil {
		t.Fatalf("authorized subscription failed: %v", err)
	}
	if _, err := subs.GetByIDForUser(ctx, userID+1, subID); !errors.Is(err, ErrSubscriptionNotFound) {
		t.Fatalf("cross-user subscription lookup error = %v, want ErrSubscriptionNotFound", err)
	}
	if err := subs.SaveOverrideForUser(ctx, userID+1, subID, nodeID, "stolen", 0, "", "", nil); !errors.Is(err, ErrSubscriptionNotFound) {
		t.Fatalf("cross-user override error = %v, want ErrSubscriptionNotFound", err)
	}
	if err := subs.SaveOverrideForUser(ctx, userID, subID, otherNodeID, "wrong-node", 0, "", "", nil); !errors.Is(err, ErrNodeNotAuthorized) {
		t.Fatalf("unauthorized node override error = %v, want ErrNodeNotAuthorized", err)
	}
}

func TestSubscriptionInboundOverrideAuthorizationIntegration(t *testing.T) {
	resource := openP1IntegrationDB(t)
	prepareP1IntegrationSchema(t, resource)
	ctx := context.Background()
	var userID, groupID, nodeID, otherNodeID, firstInboundID, secondInboundID, otherInboundID int64
	if err := resource.db.QueryRowContext(ctx, `
		INSERT INTO users(username, password_hash, role, is_active)
		VALUES ('inbound-override-user', 'synthetic-hash', 'user', TRUE) RETURNING id`).Scan(&userID); err != nil {
		t.Fatal("fixture setup failed")
	}
	if err := resource.db.QueryRowContext(ctx, `INSERT INTO node_groups(name) VALUES ('inbound-override-group') RETURNING id`).Scan(&groupID); err != nil {
		t.Fatal("fixture setup failed")
	}
	if err := resource.db.QueryRowContext(ctx, `INSERT INTO nodes(name) VALUES ('inbound-override-node') RETURNING id`).Scan(&nodeID); err != nil {
		t.Fatal("fixture setup failed")
	}
	if err := resource.db.QueryRowContext(ctx, `INSERT INTO nodes(name) VALUES ('inbound-override-other-node') RETURNING id`).Scan(&otherNodeID); err != nil {
		t.Fatal("fixture setup failed")
	}
	if _, err := resource.db.ExecContext(ctx, `INSERT INTO node_group_members(group_id, node_id) VALUES ($1,$2)`, groupID, nodeID); err != nil {
		t.Fatal("fixture setup failed")
	}
	if _, err := resource.db.ExecContext(ctx, `INSERT INTO user_node_groups(user_id, group_id) VALUES ($1,$2)`, userID, groupID); err != nil {
		t.Fatal("fixture setup failed")
	}
	for index, target := range []*int64{&firstInboundID, &secondInboundID} {
		if err := resource.db.QueryRowContext(ctx, `
			INSERT INTO inbounds(node_id, name, protocol, role, listen_port, config)
			VALUES ($1, $2, 'hysteria2', 'entry', $3, '{"sni":"example.test"}') RETURNING id`,
			nodeID, fmt.Sprintf("inbound-override-%d", index), 20000+index).Scan(target); err != nil {
			t.Fatal("fixture setup failed")
		}
	}
	if err := resource.db.QueryRowContext(ctx, `
		INSERT INTO inbounds(node_id, name, protocol, role, listen_port, config)
		VALUES ($1, 'inbound-override-other', 'hysteria2', 'entry', 21000, '{"sni":"example.test"}') RETURNING id`, otherNodeID).Scan(&otherInboundID); err != nil {
		t.Fatal("fixture setup failed")
	}
	subscription := &models.Subscription{
		UserID: userID, Name: "inbound override subscription", Token: "synthetic-inbound-override", Format: "mihomo", NodeGroupID: &groupID,
	}
	subID, err := NewSubscriptionRepo(resource.db).CreateForUser(ctx, subscription)
	if err != nil {
		t.Fatalf("subscription setup failed: %v", err)
	}
	repository := NewSubscriptionRepo(resource.db)
	if err := repository.SaveInboundOverrideForUser(ctx, userID, subID, nodeID, firstInboundID, "first inbound", 1, "", "", json.RawMessage(`{"sni":"first.example"}`)); err != nil {
		t.Fatalf("first inbound override failed: %v", err)
	}
	if err := repository.SaveInboundOverrideForUser(ctx, userID, subID, nodeID, secondInboundID, "second inbound", 2, "", "", json.RawMessage(`{"sni":"second.example"}`)); err != nil {
		t.Fatalf("second inbound override failed: %v", err)
	}
	overrides, err := repository.ListInboundOverrides(ctx, subID)
	if err != nil {
		t.Fatalf("inbound override listing failed: %v", err)
	}
	if len(overrides) != 2 {
		t.Fatalf("inbound override count = %d, want 2", len(overrides))
	}
	if overrides[OverrideKey{NodeID: nodeID, InboundID: firstInboundID}].DisplayName != "first inbound" || overrides[OverrideKey{NodeID: nodeID, InboundID: secondInboundID}].DisplayName != "second inbound" {
		t.Fatalf("same-node inbound overrides collided: %+v", overrides)
	}
	if err := repository.SaveInboundOverrideForUser(ctx, userID, subID, otherNodeID, otherInboundID, "unauthorized", 0, "", "", nil); !errors.Is(err, ErrNodeNotAuthorized) {
		t.Fatalf("unauthorized inbound override error = %v, want ErrNodeNotAuthorized", err)
	}
	if err := repository.SaveInboundOverrideForUser(ctx, userID+1, subID, nodeID, firstInboundID, "cross-user", 0, "", "", nil); !errors.Is(err, ErrSubscriptionNotFound) {
		t.Fatalf("cross-user inbound override error = %v, want ErrSubscriptionNotFound", err)
	}
	if _, err := resource.db.ExecContext(ctx, `UPDATE users SET expire_at=now()-interval '1 second' WHERE id=$1`, userID); err != nil {
		t.Fatal("fixture expiry failed")
	}
	if err := repository.SaveInboundOverrideForUser(ctx, userID, subID, nodeID, firstInboundID, "expired", 0, "", "", nil); !errors.Is(err, ErrNodeNotAuthorized) {
		t.Fatalf("expired-owner inbound override error = %v, want ErrNodeNotAuthorized", err)
	}
}

func TestUserCredentialReconciliationAndRevocationIntegration(t *testing.T) {
	resource := openP1IntegrationDB(t)
	prepareP1IntegrationSchema(t, resource)
	ctx := context.Background()

	var nodeID, groupID int64
	if err := resource.db.QueryRowContext(ctx, `
		INSERT INTO nodes(name, type, public_ip) VALUES ('credential-node', 'managed', 'credential.example') RETURNING id`).Scan(&nodeID); err != nil {
		t.Fatal("fixture setup failed")
	}
	if err := resource.db.QueryRowContext(ctx, `INSERT INTO node_groups(name) VALUES ('credential-group') RETURNING id`).Scan(&groupID); err != nil {
		t.Fatal("fixture setup failed")
	}
	if _, err := resource.db.ExecContext(ctx, `INSERT INTO node_group_members(group_id, node_id) VALUES ($1, $2)`, groupID, nodeID); err != nil {
		t.Fatal("fixture setup failed")
	}

	var firstUserID, secondUserID, unauthorizedUserID int64
	for index, userID := range []*int64{&firstUserID, &secondUserID, &unauthorizedUserID} {
		if err := resource.db.QueryRowContext(ctx, `
			INSERT INTO users(username, password_hash, role, is_active)
			VALUES ($1, 'synthetic-hash', 'user', TRUE) RETURNING id`, fmt.Sprintf("credential-user-%d", index)).Scan(userID); err != nil {
			t.Fatal("fixture setup failed")
		}
	}
	if _, err := resource.db.ExecContext(ctx, `
		INSERT INTO user_node_groups(user_id, group_id) VALUES ($1, $3), ($2, $3)`, firstUserID, secondUserID, groupID); err != nil {
		t.Fatal("fixture setup failed")
	}

	var realityID, shadowsocksID, hysteriaID int64
	if err := resource.db.QueryRowContext(ctx, `
		INSERT INTO inbounds(node_id, name, protocol, role, listen_port, config)
		VALUES ($1, 'credential-reality', 'vless-reality', 'entry', 443,
			'{"sni":"example.test","target":"example.test:443","privateKey":"synthetic-private-key","shortId":"0123456789abcdef"}') RETURNING id`, nodeID).Scan(&realityID); err != nil {
		t.Fatal("fixture setup failed")
	}
	if err := resource.db.QueryRowContext(ctx, `
		INSERT INTO inbounds(node_id, name, protocol, role, listen_port, config)
		VALUES ($1, 'credential-ss', 'shadowsocks', 'entry', 8388,
			'{"method":"2022-blake3-aes-128-gcm","password":"AAAAAAAAAAAAAAAAAAAAAA=="}') RETURNING id`, nodeID).Scan(&shadowsocksID); err != nil {
		t.Fatal("fixture setup failed")
	}
	if err := resource.db.QueryRowContext(ctx, `
		INSERT INTO inbounds(node_id, name, protocol, role, listen_port, config)
		VALUES ($1, 'credential-hy2', 'hysteria2', 'entry', 8443,
			'{"sni":"example.test","certificatePath":"/tmp/synthetic.crt","keyPath":"/tmp/synthetic.key"}') RETURNING id`, nodeID).Scan(&hysteriaID); err != nil {
		t.Fatal("fixture setup failed")
	}

	repository := NewUserRepo(resource.db)
	firstCredential, err := repository.EnsureUserCredential(ctx, firstUserID, realityID, "vless-reality")
	if err != nil {
		t.Fatalf("first-request Reality credential issuance failed: %v", err)
	}
	reusedCredential, err := repository.EnsureUserCredential(ctx, firstUserID, realityID, "vless-reality")
	if err != nil {
		t.Fatalf("repeat-request Reality credential lookup failed: %v", err)
	}
	if firstCredential == "" || firstCredential != reusedCredential {
		t.Fatal("repeat credential request did not reuse the persisted Reality credential")
	}
	if err := repository.ReconcileUserCredentialsForNode(ctx, nodeID); err != nil {
		t.Fatalf("credential reconciliation failed: %v", err)
	}
	credentials, err := repository.ListUserCredentialsForNode(ctx, nodeID)
	if err != nil {
		t.Fatalf("credential listing failed: %v", err)
	}
	if len(credentials) != 6 {
		t.Fatalf("credential count = %d, want six authorized user/inbound credentials", len(credentials))
	}
	inboundCredentials, err := repository.ListUserCredentialsForInbound(ctx, shadowsocksID)
	if err != nil {
		t.Fatalf("per-inbound credential listing failed: %v", err)
	}
	if len(inboundCredentials) != 2 {
		t.Fatalf("per-inbound credential count = %d, want 2", len(inboundCredentials))
	}
	sharedTopology := sharedconfig.NodeConfig{
		SchemaVersion: sharedconfig.SchemaVersion,
		NodeID:        nodeID,
		NodeName:      "credential-node",
		Inbounds: []sharedconfig.Inbound{
			{ID: realityID, Name: "credential-reality", Protocol: "vless-reality", Role: "entry", Port: 443, Params: map[string]string{
				"uuid": "shared-reality-uuid", "sni": "example.test", "target": "example.test:443", "privateKey": "synthetic-private-key", "shortId": "0123456789abcdef",
			}},
			{ID: shadowsocksID, Name: "credential-ss", Protocol: "shadowsocks", Role: "entry", Port: 8388, Params: map[string]string{
				"method": "2022-blake3-aes-128-gcm", "password": "AAAAAAAAAAAAAAAAAAAAAA==",
			}},
			{ID: hysteriaID, Name: "credential-hy2", Protocol: "hysteria2", Role: "entry", Port: 8443, Params: map[string]string{
				"password": "shared-hy2-password", "certificatePath": "/tmp/synthetic.crt", "keyPath": "/tmp/synthetic.key", "sni": "example.test",
			}},
		},
		Edges:    []sharedconfig.Edge{},
		Outbound: "direct",
	}
	sharedTopology.Credentials = make([]sharedconfig.UserCredential, 0, len(credentials))
	for _, credential := range credentials {
		sharedCredential := sharedconfig.UserCredential{
			UserID: credential.UserID, InboundID: credential.InboundID, Name: credential.Username, Protocol: credential.Protocol,
		}
		switch credential.Protocol {
		case "vless-reality":
			sharedCredential.UUID = credential.Credential
		case "shadowsocks", "hysteria2":
			sharedCredential.Password = credential.Credential
		}
		sharedTopology.Credentials = append(sharedTopology.Credentials, sharedCredential)
	}
	rendered, err := sharedconfig.Render(sharedTopology)
	if err != nil {
		t.Fatalf("shared renderer rejected persisted credential roster: %v", err)
	}
	var renderedConfig sharedconfig.SingBoxConfig
	if err := json.Unmarshal(rendered.Content, &renderedConfig); err != nil {
		t.Fatalf("shared rendered config is invalid: %v", err)
	}
	if len(renderedConfig.Inbounds) != 3 || len(renderedConfig.Inbounds[0].Users) != 2 || len(renderedConfig.Inbounds[1].Users) != 2 || len(renderedConfig.Inbounds[2].Users) != 2 {
		t.Fatalf("persisted active roster was not rendered for every entry: %+v", renderedConfig.Inbounds)
	}
	renderedBytes := string(rendered.Content)
	for _, sharedSecret := range []string{"shared-reality-uuid", "shared-hy2-password"} {
		if strings.Contains(renderedBytes, sharedSecret) {
			t.Fatalf("shared inbound secret %q leaked into active credential render", sharedSecret)
		}
	}
	if renderedConfig.Inbounds[1].Password != "AAAAAAAAAAAAAAAAAAAAAA==" {
		t.Fatalf("SS2022 server PSK = %q, want retained server key", renderedConfig.Inbounds[1].Password)
	}
	for _, credential := range credentials {
		if !strings.Contains(renderedBytes, credential.Credential) {
			t.Fatalf("persisted credential for user %d/inbound %d was not rendered", credential.UserID, credential.InboundID)
		}
	}

	byKey := make(map[[2]int64]models.UserCredential, len(credentials))
	for _, credential := range credentials {
		byKey[[2]int64{credential.UserID, credential.InboundID}] = credential
		switch credential.Protocol {
		case "vless-reality":
			if !regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`).MatchString(credential.Credential) {
				t.Fatalf("Reality credential format is invalid for user %d", credential.UserID)
			}
		case "shadowsocks":
			decoded, decodeErr := base64.StdEncoding.DecodeString(credential.Credential)
			if decodeErr != nil || len(decoded) != 16 {
				t.Fatalf("SS2022 credential does not contain a 128-bit key for user %d", credential.UserID)
			}
		case "hysteria2":
			if len(credential.Credential) != 64 || !regexp.MustCompile(`^[0-9a-f]+$`).MatchString(credential.Credential) {
				t.Fatalf("HY2 credential format is invalid for user %d", credential.UserID)
			}
		default:
			t.Fatalf("unexpected credential protocol %q", credential.Protocol)
		}
	}

	for _, inboundID := range []int64{realityID, shadowsocksID, hysteriaID} {
		if _, ok := byKey[[2]int64{unauthorizedUserID, inboundID}]; ok {
			t.Fatalf("unauthorized user received credential for inbound %d", inboundID)
		}
	}
	if err := repository.ReconcileUserCredentialsForNode(ctx, nodeID); err != nil {
		t.Fatalf("repeat credential reconciliation failed: %v", err)
	}
	repeated, err := repository.ListUserCredentialsForNode(ctx, nodeID)
	if err != nil {
		t.Fatalf("repeat credential listing failed: %v", err)
	}
	for _, credential := range repeated {
		original, ok := byKey[[2]int64{credential.UserID, credential.InboundID}]
		if !ok || original.Credential != credential.Credential {
			t.Fatalf("credential was not stable for user %d/inbound %d", credential.UserID, credential.InboundID)
		}
	}

	if _, err := resource.db.ExecContext(ctx, `UPDATE users SET is_active=FALSE WHERE id=$1`, secondUserID); err != nil {
		t.Fatal("fixture revocation failed")
	}
	filtered, err := repository.ListUserCredentialsForNode(ctx, nodeID)
	if err != nil {
		t.Fatalf("inactive-user credential listing failed: %v", err)
	}
	if len(filtered) != 3 {
		t.Fatalf("credential count after user revocation = %d, want 3", len(filtered))
	}
	if _, err := resource.db.ExecContext(ctx, `UPDATE users SET is_active=TRUE, expire_at=now()-interval '1 second' WHERE id=$1`, firstUserID); err != nil {
		t.Fatal("fixture expiry failed")
	}
	filtered, err = repository.ListUserCredentialsForNode(ctx, nodeID)
	if err != nil {
		t.Fatalf("expired-user credential listing failed: %v", err)
	}
	if len(filtered) != 0 {
		t.Fatalf("credential count after expiry = %d, want 0", len(filtered))
	}
	if _, err := resource.db.ExecContext(ctx, `UPDATE users SET expire_at=NULL WHERE id=$1`, firstUserID); err != nil {
		t.Fatal("fixture expiry reset failed")
	}
	if _, err := resource.db.ExecContext(ctx, `DELETE FROM user_node_groups WHERE user_id=$1 AND group_id=$2`, firstUserID, groupID); err != nil {
		t.Fatal("fixture group revocation failed")
	}
	filtered, err = repository.ListUserCredentialsForNode(ctx, nodeID)
	if err != nil {
		t.Fatalf("group-revoked credential listing failed: %v", err)
	}
	if len(filtered) != 0 {
		t.Fatalf("credential count after group revocation = %d, want 0", len(filtered))
	}
}

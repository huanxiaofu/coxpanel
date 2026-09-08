package repo

import (
	"context"
	"database/sql"
	"testing"

	"github.com/coxpanel/backend/internal/db"
)

func OpenP2TopologyTestDB(t *testing.T) *sql.DB {
	t.Helper()
	resource := openP1IntegrationDB(t)
	if err := db.Migrate(context.Background(), resource.db); err != nil {
		t.Fatal("P2 isolated schema migration failed")
	}
	return resource.db
}

package configdoc

import (
	"context"
	"os"
	"testing"

	"github.com/nebula/monitor/internal/server/db"
	"github.com/nebula/monitor/internal/server/migrate"
)

func TestMySQLStoreContract(t *testing.T) {
	dsn := os.Getenv("TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("TEST_MYSQL_DSN 未设置")
	}
	pool, err := db.Open(context.Background(), db.Config{DSN: dsn})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { pool.Close() })

	ms, err := migrate.Load()
	if err != nil {
		t.Fatalf("Load migrations: %v", err)
	}
	if _, err := migrate.Apply(context.Background(), pool, ms); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	runStoreContract(t, func(t *testing.T) Store {
		if _, err := pool.ExecContext(context.Background(), "DELETE FROM config_documents"); err != nil {
			t.Fatalf("清空表失败: %v", err)
		}
		return NewMySQLStore(pool)
	})
}

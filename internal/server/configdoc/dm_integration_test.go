package configdoc

import (
	"context"
	"os"
	"testing"

	"github.com/mlmw92/yingzhao/internal/server/db"
	"github.com/mlmw92/yingzhao/internal/server/migrate"
	"github.com/mlmw92/yingzhao/internal/server/sqlx"
)

// PostgreSQL 契约用例：与 MySQL 跑同一套断言（ConfigStore 契约 + 版本冲突重试）。
func TestDMStoreContract(t *testing.T) {
	dsn := os.Getenv("TEST_DM_DSN")
	if dsn == "" {
		t.Skip("TEST_DM_DSN 未设置")
	}
	ctx := context.Background()
	pool, err := db.Open(ctx, db.Config{Driver: "dm", DSN: dsn})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { pool.Close() })

	ms, err := migrate.Load("dm")
	if err != nil {
		t.Fatalf("Load migrations: %v", err)
	}
	if _, err := migrate.Apply(ctx, pool, sqlx.DM(), ms); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if _, err := pool.Exec("DELETE FROM config_documents"); err != nil {
		t.Fatalf("清空表失败: %v", err)
	}

	runStoreContract(t, func(t *testing.T) Store {
		if _, err := pool.Exec("DELETE FROM config_documents"); err != nil {
			t.Fatalf("清空表失败: %v", err)
		}
		return NewSQLStore(pool, sqlx.DM())
	})
}

func TestDMDBPersisterRetriesVersionConflict(t *testing.T) {
	dsn := os.Getenv("TEST_DM_DSN")
	if dsn == "" {
		t.Skip("TEST_DM_DSN 未设置")
	}
	ctx := context.Background()
	pool, err := db.Open(ctx, db.Config{Driver: "dm", DSN: dsn})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { pool.Close() })
	ms, err := migrate.Load("dm")
	if err != nil {
		t.Fatalf("Load migrations: %v", err)
	}
	if _, err := migrate.Apply(ctx, pool, sqlx.DM(), ms); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if _, err := pool.Exec("DELETE FROM config_documents WHERE namespace='retrytest'"); err != nil {
		t.Fatal(err)
	}

	store := NewSQLStore(pool, sqlx.DM())
	p1 := NewDBPersister(store, "retrytest", nil)
	p2 := NewDBPersister(store, "retrytest", nil)
	if err := p1.Save([]byte("a: 1\n")); err != nil {
		t.Fatalf("首次写入: %v", err)
	}
	if _, err := p2.Load(); err != nil {
		t.Fatalf("p2 加载: %v", err)
	}
	if err := p1.Save([]byte("a: 2\n")); err != nil {
		t.Fatalf("p1 再写: %v", err)
	}
	if err := p2.Save([]byte("a: 3\n")); err != nil {
		t.Fatalf("版本冲突应自动重试: %v", err)
	}
	if err := p2.Save([]byte("a: 4\n")); err != nil {
		t.Fatalf("重试后版本未同步: %v", err)
	}
}

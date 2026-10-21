package configdoc

import (
	"context"
	"os"
	"testing"

	"github.com/mlmw92/yingzhao/internal/server/db"
	"github.com/mlmw92/yingzhao/internal/server/migrate"
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

// 版本冲突必须自动重读并重试一次。
//
// DBPersister 只在 Load/成功 Save 时更新 p.version。一次外部写入（维护脚本、
// 从备份恢复、将来多实例）就会让本进程的版本过期；不重试的话，之后**每一次**
// Save 都返回 ErrVersionConflict——而 store 层大多只记一条日志就当成功，
// 该 namespace 从此永久只读，直到进程重启。
func TestDBPersisterRetriesVersionConflict(t *testing.T) {
	dsn := os.Getenv("TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("TEST_MYSQL_DSN 未设置")
	}
	ctx := context.Background()
	pool, err := db.Open(ctx, db.Config{DSN: dsn})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { pool.Close() })
	ms, err := migrate.Load()
	if err != nil {
		t.Fatalf("Load migrations: %v", err)
	}
	if _, err := migrate.Apply(ctx, pool, ms); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if _, err := pool.Exec("DELETE FROM config_documents WHERE namespace=?", "retrytest"); err != nil {
		t.Fatal(err)
	}

	store := NewMySQLStore(pool)
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
		t.Fatalf("重试后版本未同步，后续写入仍失败: %v", err)
	}
}

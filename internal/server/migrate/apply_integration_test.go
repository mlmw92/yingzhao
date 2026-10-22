package migrate

import (
	"context"
	"database/sql"
	"os"
	"sync"
	"testing"

	"github.com/mlmw92/yingzhao/internal/server/db"
	"github.com/mlmw92/yingzhao/internal/server/sqlx"
)

func testDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("TEST_MYSQL_DSN 未设置")
	}
	conn, err := db.Open(context.Background(), db.Config{Driver: "mysql", DSN: dsn})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

func TestApplyIsIdempotent(t *testing.T) {
	conn := testDB(t)
	ms := []Migration{{Version: 9001, Name: "test_table", SQL: "CREATE TABLE IF NOT EXISTS migrate_test(id INT PRIMARY KEY)"}}
	if _, err := Apply(context.Background(), conn, sqlx.MySQL(), ms); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	n, err := Apply(context.Background(), conn, sqlx.MySQL(), ms)
	if err != nil || n != 0 {
		t.Fatalf("第二次 Apply 应为 0，实际 %d, %v", n, err)
	}
}

func TestApplyRejectsModifiedMigration(t *testing.T) {
	conn := testDB(t)
	ms := []Migration{{Version: 9002, Name: "t", SQL: "CREATE TABLE IF NOT EXISTS migrate_test2(id INT PRIMARY KEY)"}}
	if _, err := Apply(context.Background(), conn, sqlx.MySQL(), ms); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	ms[0].SQL = "CREATE TABLE IF NOT EXISTS migrate_test2(id BIGINT PRIMARY KEY)"
	if _, err := Apply(context.Background(), conn, sqlx.MySQL(), ms); err == nil {
		t.Fatal("已应用迁移被修改应报错")
	}
}

func TestApplySerializesConcurrentRunners(t *testing.T) {
	conn := testDB(t)
	ms := []Migration{{Version: 9003, Name: "c", SQL: "CREATE TABLE IF NOT EXISTS migrate_test3(id INT PRIMARY KEY)"}}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := Apply(context.Background(), conn, sqlx.MySQL(), ms)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("并发 Apply 失败: %v", err)
		}
	}
}

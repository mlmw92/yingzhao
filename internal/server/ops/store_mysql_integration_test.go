package ops

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/mlmw92/yingzhao/internal/model"
	"github.com/mlmw92/yingzhao/internal/server/asset"
	"github.com/mlmw92/yingzhao/internal/server/db"
	"github.com/mlmw92/yingzhao/internal/server/sqlx"
)

// MySQL 集成用例：JSON 回填必须走 MySQL 方言，序号必须从库里读回。
//
// 这两件事曾经同时坏掉：回填在 s.mysql 赋值之前执行，生成的是 SQLite 的
// ON CONFLICT；而序号查询用了裸 *sql.DB，`key` 是 MySQL 保留字，错误又被吞掉，
// 于是每次重启 seq 都回到 0，新建任务覆盖掉旧任务。本用例把两条路径钉住。
func TestOpsMySQLBackfillAndSeq(t *testing.T) {
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
	if _, err := asset.OpenMySQL(pool); err != nil {
		t.Fatalf("OpenMySQL: %v", err)
	}
	for _, tbl := range []string{"ops_tasks", "ops_caps", "ops_meta"} {
		if _, err := pool.Exec("DELETE FROM " + tbl); err != nil {
			t.Fatalf("清空 %s 失败: %v", tbl, err)
		}
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "ops_tasks.json")
	snap := map[string]any{
		"tasks": []*Task{{
			OpsCommand: model.OpsCommand{ID: "ops-5", Node: "web-01", Kind: "svc.status"},
			State:      "queued",
		}},
		"caps": map[string][]string{"web-01": {"svc.status"}},
		"seq":  5,
	}
	raw, _ := json.Marshal(snap)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}

	st := NewStore(path)
	if err := st.UseSQL(pool, sqlx.MySQL()); err != nil {
		t.Fatalf("UseSQL 回填失败: %v", err)
	}
	if _, ok := st.Get("ops-5"); !ok {
		t.Fatalf("回填后 ops-5 应存在")
	}

	// 序号必须从库里读回：新任务应当是 ops-6，而不是 ops-1。
	created := st.Create(model.OpsCommand{Node: "web-01", Kind: "svc.status"}, "tester", "127.0.0.1", "")
	if created.ID != "ops-6" {
		t.Fatalf("新任务 ID = %q, want ops-6（seq 未从库读回）", created.ID)
	}

	// 重开一次：模拟重启，seq 仍应连续，旧任务不能被覆盖。
	st2 := NewStore(filepath.Join(dir, "missing.json"))
	if err := st2.UseSQL(pool, sqlx.MySQL()); err != nil {
		t.Fatalf("重开 UseSQL 失败: %v", err)
	}
	if _, ok := st2.Get("ops-5"); !ok {
		t.Fatalf("重启后 ops-5 丢失")
	}
	if _, ok := st2.Get("ops-6"); !ok {
		t.Fatalf("重启后 ops-6 丢失")
	}
	again := st2.Create(model.OpsCommand{Node: "web-02", Kind: "svc.status"}, "tester", "127.0.0.1", "")
	if again.ID != "ops-7" {
		t.Fatalf("重启后新任务 ID = %q, want ops-7", again.ID)
	}
}

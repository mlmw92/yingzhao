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

// PostgreSQL 集成用例：JSON 回填、序号跨重启、任务不互相覆盖。
func TestOpsPostgresBackfillAndSeq(t *testing.T) {
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_DSN 未设置")
	}
	ctx := context.Background()
	pool, err := db.Open(ctx, db.Config{Driver: "postgres", DSN: dsn})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { pool.Close() })
	if _, err := asset.OpenSQL(pool, sqlx.Postgres()); err != nil {
		t.Fatalf("OpenSQL: %v", err)
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
	if err := st.UseSQL(pool, sqlx.Postgres()); err != nil {
		t.Fatalf("UseSQL 回填失败: %v", err)
	}
	if _, ok := st.Get("ops-5"); !ok {
		t.Fatalf("回填后 ops-5 应存在")
	}
	created := st.Create(model.OpsCommand{Node: "web-01", Kind: "svc.status"}, "tester", "127.0.0.1", "")
	if created.ID != "ops-6" {
		t.Fatalf("新任务 ID = %q, want ops-6", created.ID)
	}

	st2 := NewStore(filepath.Join(dir, "missing.json"))
	if err := st2.UseSQL(pool, sqlx.Postgres()); err != nil {
		t.Fatalf("重开 UseSQL 失败: %v", err)
	}
	if _, ok := st2.Get("ops-5"); !ok {
		t.Fatalf("重启后 ops-5 丢失")
	}
	if _, ok := st2.Get("ops-6"); !ok {
		t.Fatalf("重启后 ops-6 丢失")
	}
	if again := st2.Create(model.OpsCommand{Node: "web-02", Kind: "svc.status"}, "tester", "127.0.0.1", ""); again.ID != "ops-7" {
		t.Fatalf("重启后新任务 ID = %q, want ops-7", again.ID)
	}
}

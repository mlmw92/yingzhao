package audit

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/mlmw92/yingzhao/internal/server/asset"
	"github.com/mlmw92/yingzhao/internal/server/db"
	"github.com/mlmw92/yingzhao/internal/server/sqlx"
)

// PostgreSQL 集成用例：链头初始化、写入、裁剪、裁剪锚点与校验。
func TestAuditPostgresPruneAndVerify(t *testing.T) {
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
	for _, tbl := range []string{"audit_events", "audit_pruned", "audit_chain"} {
		if _, err := pool.Exec("DELETE FROM " + tbl); err != nil {
			t.Fatalf("清空 %s 失败: %v", tbl, err)
		}
	}
	key, err := LoadOrCreateChainKey(t.TempDir() + "/audit.key")
	if err != nil {
		t.Fatalf("LoadOrCreateChainKey: %v", err)
	}
	st := New("")
	if err := st.UseSQL(pool, sqlx.Postgres()); err != nil {
		t.Fatalf("UseSQL: %v", err)
	}
	st.SetChainKey(key)

	var n int
	if err := st.db.QueryRow("SELECT COUNT(*) FROM audit_chain WHERE id=1").Scan(&n); err != nil {
		t.Fatalf("查询链头失败: %v", err)
	}
	if n != 1 {
		t.Fatalf("链头行应已初始化，实际 %d 行", n)
	}

	base := time.Now().Add(-time.Hour)
	for i := 0; i < 5; i++ {
		if err := st.Record(Event{
			Time: base.Add(time.Duration(i) * time.Minute),
			User: "tester", Method: "POST", Path: "/api/v1/test", Succeeded: true,
		}); err != nil {
			t.Fatalf("Record %d: %v", i, err)
		}
	}
	deleted, err := st.Prune(base.Add(3 * time.Minute).UnixMilli())
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if deleted != 3 {
		t.Fatalf("删除条数 = %d, want 3", deleted)
	}
	segs := st.PrunedSegments()
	if len(segs) != 1 || !segs[0].MACValid || segs[0].Rows != 3 {
		t.Fatalf("裁剪区间不对: %+v", segs)
	}
	if rep := st.Verify(); !rep.OK {
		t.Fatalf("裁剪后校验应通过，实际 breaks=%+v note=%s", rep.Breaks, rep.Note)
	}

	// 条数兜底（PruneRows）走的是 `IN (子查询 LIMIT)` 路径；PostgreSQL 不需要派生表。
	removed, err := st.PruneRows(1)
	if err != nil {
		t.Fatalf("PruneRows: %v", err)
	}
	if removed != 1 {
		t.Fatalf("PruneRows 删除 %d 条，want 1", removed)
	}
	if rep := st.Verify(); !rep.OK {
		t.Fatalf("按条数裁剪后校验应通过，实际 breaks=%+v", rep.Breaks)
	}
}

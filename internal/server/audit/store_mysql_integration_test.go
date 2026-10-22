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

func openAuditMySQL(t *testing.T) (*Store, *ChainKey) {
	t.Helper()
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
	if err := st.UseSQL(pool, sqlx.MySQL()); err != nil {
		t.Fatalf("UseSQL: %v", err)
	}
	st.SetChainKey(key)
	return st, key
}

// MySQL 集成用例：链头初始化、裁剪、裁剪锚点。
//
// `rows` 是 MySQL 8 保留字：裁剪的 INSERT/SELECT 没加反引号时整条事务 1064，
// 一条审计都删不掉，而错误只被记进日志。本用例覆盖「裁剪成功 + 校验仍 OK」。
func TestAuditMySQLPruneAndVerify(t *testing.T) {
	st, _ := openAuditMySQL(t)

	// 链头行必须在切库时初始化：否则第一个写入者走 ErrNoRows 分支，
	// 并发时两个事务会各自算出 seq=1。
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
	if len(segs) != 1 {
		t.Fatalf("裁剪区间 = %d 段, want 1", len(segs))
	}
	if !segs[0].MACValid || segs[0].Rows != 3 || segs[0].FromSeq != 1 || segs[0].ToSeq != 3 {
		t.Fatalf("裁剪区间不对: %+v", segs[0])
	}

	rep := st.Verify()
	if !rep.OK {
		t.Fatalf("裁剪后校验应通过，实际 breaks=%+v note=%s", rep.Breaks, rep.Note)
	}
}

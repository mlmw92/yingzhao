package asset

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mlmw92/yingzhao/internal/server/db"
)

// MySQL 集成用例：按实例地址找资产。
//
// 原 SQL 用了 `ESCAPE '\'`。SQLite 不把反斜杠当字符串转义符，所以本地用例全绿；
// MySQL 默认 sql_mode 下 `'\'` 里的 `\'` 是转义引号，字符串未闭合 → 1064，
// 中间件告警的「影响面」接口直接 500。
func TestAssetMySQLInstanceAddrLookup(t *testing.T) {
	dsn := os.Getenv("TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("TEST_MYSQL_DSN 未设置")
	}
	pool, err := db.Open(context.Background(), db.Config{DSN: dsn})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { pool.Close() })
	st, err := OpenMySQL(pool)
	if err != nil {
		t.Fatalf("OpenMySQL: %v", err)
	}
	svc := NewService(st)

	// 每次用不同地址，避免与既有数据或并发用例互相影响。
	addr := fmt.Sprintf("10.99.%d:%d", time.Now().UnixNano()%250, 6379+time.Now().UnixNano()%1000)
	if _, _, err := svc.Apply(Observation{
		TypeKey: TypeMiddlewareInst, NaturalKey: "redis:" + addr, Name: "redis",
		Node: "web-01", Source: SourceDiscovery, Actor: "agent",
	}); err != nil {
		t.Fatalf("建档实例失败: %v", err)
	}
	got, err := svc.InstancesByAddr(addr)
	if err != nil {
		t.Fatalf("InstancesByAddr: %v", err)
	}
	found := false
	for _, a := range got {
		if a.NaturalKey == "redis:"+addr {
			found = true
		}
	}
	if !found {
		t.Fatalf("未找到实例 %s，返回 %d 条", addr, len(got))
	}
}

// 转义字符改成 ! 之后，反斜杠与通配符都按字面匹配。
func TestEscapeLikeUsesBang(t *testing.T) {
	cases := map[string]string{
		"plain":     "plain",
		"a_b":       "a!_b",
		"a%b":       "a!%b",
		"a!b":       "a!!b",
		`C:\tmp\_x`: `C:\tmp\!_x`,
	}
	for in, want := range cases {
		if got := escapeLike(in); got != want {
			t.Fatalf("escapeLike(%q) = %q, want %q", in, got, want)
		}
	}
}

// 保存一份没变化的类型模型必须成功。
//
// MySQL 的 UPDATE 默认只回报「值真的变了」的行；同一份 JSON 再存一次
// RowsAffected=0，代码把它当成「类型不存在」→ 404。DSN 里启用了
// clientFoundRows 后回报的是匹配行数。
func TestAssetMySQLSaveTypeSchemaUnchanged(t *testing.T) {
	dsn := os.Getenv("TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("TEST_MYSQL_DSN 未设置")
	}
	pool, err := db.Open(context.Background(), db.Config{DSN: dsn})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { pool.Close() })
	st, err := OpenMySQL(pool)
	if err != nil {
		t.Fatalf("OpenMySQL: %v", err)
	}
	key := BuiltinTypes()[0].Key
	sch := TypeSchema{FocusFields: []string{"cpuCores"}}
	if err := st.saveTypeSchema(key, sch); err != nil {
		t.Fatalf("首次保存: %v", err)
	}
	if err := st.saveTypeSchema(key, sch); err != nil {
		t.Fatalf("重复保存同一份模型应成功，实际: %v", err)
	}
}

// 大小写无关的自然键查找必须不依赖库的默认排序规则。
//
// SQLite 用 `COLLATE NOCASE`；MySQL 里这条子句会被剥掉，转而依赖建库时的默认
// collation。DBA 若用 `_bin`，查找会静默变成大小写敏感——同一台机器被判成两台。
func TestAssetMySQLCaseFoldLookup(t *testing.T) {
	dsn := os.Getenv("TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("TEST_MYSQL_DSN 未设置")
	}
	pool, err := db.Open(context.Background(), db.Config{DSN: dsn})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { pool.Close() })
	st, err := OpenMySQL(pool)
	if err != nil {
		t.Fatalf("OpenMySQL: %v", err)
	}
	key := fmt.Sprintf("CaseFold-%d", time.Now().UnixNano())
	if _, _, err := NewService(st).Apply(Observation{
		TypeKey: TypeHost, NaturalKey: key, Name: key, Node: "web-01", Source: SourceDiscovery,
	}); err != nil {
		t.Fatalf("建档失败: %v", err)
	}
	got, ok, err := st.assetByNaturalFold(TypeHost, strings.ToLower(key))
	if err != nil {
		t.Fatalf("fold 查找: %v", err)
	}
	if !ok || got.NaturalKey != key {
		t.Fatalf("大小写折叠查找失败: ok=%v got=%+v", ok, got)
	}
}

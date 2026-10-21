# MySQL 状态后端 · 阶段 0（持久化地基）实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为「配置/状态全部存 MySQL」打地基：数据库连接、Schema 迁移框架、配置文档存储接口与 MySQL 实现，全部不改变现有运行时行为。

**Architecture:** 新增三个包——`internal/server/db`（连接与 DSN 处理）、`internal/server/migrate`（有序迁移 + 命名锁 + 校验和）、`internal/server/configdoc`（整份配置文档的读写接口，含 MySQL 与文件两个实现）。文件实现**只作测试替身**；阶段 0 不把任何现有 store 切到 MySQL，Server 启动路径不变。

**Tech Stack:** Go 1.22、`database/sql`、`github.com/go-sql-driver/mysql`（已在 go.mod）、MySQL 8.0+、Go testing。

**Spec:** `docs/superpowers/specs/2026-10-21-mysql-state-backend-design.md`

## Global Constraints

- 阶段 0 **不改变任何现有运行时行为**：不打开数据库、不迁移任何 store、不改 `server.yaml` 的必填项。
- MySQL 8.0+；字符集 `utf8mb4`；时间统一 `BIGINT` epoch 毫秒。
- 迁移**每个文件一条 SQL 语句**（MySQL 驱动默认不允许一次执行多条）；文件里不写注释与多余分隔符。
- 迁移 SQL 必须**可重复执行**（DDL 一律 `IF NOT EXISTS`）：MySQL 的 DDL 隐式提交，执行成功但写 `schema_migrations` 失败时，下次会重跑该迁移。
- 迁移执行必须持有 MySQL 命名锁 `yingzhao_schema_migrate`，且锁必须在**同一条连接**上获取与释放。
- 日志与错误信息里**不得出现 DSN 明文密码**。
- 单元测试不得依赖外部 MySQL；集成测试用 `TEST_MYSQL_DSN` 环境变量门控，未设置时 `t.Skip`。
- 文件测试替身只用于测试，不提供生产配置入口。
- 所有提交信息末尾附：`Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>`。
- 提交与构建分开执行。

## Review Focus

- **DSN 里的特殊字符**：密码含 `@`、`?`、`/` 时，脱敏与补参数不能把 DSN 改坏，也不能把密码打进日志。
- **迁移并发**：两个实例同时启动时，只有一个执行迁移，另一个等待后跳过；不能重复执行。
- **已应用迁移被修改**：校验和不一致必须报错，不能静默跳过。
- **版本冲突**：两个写入者用同一个 `expectedVersion` 保存时，只有一个成功，另一个得到 `ErrVersionConflict`。
- **命名空间注入**：`../`、大写、空串等非法 namespace 必须被拒绝，不能拼出越界路径。

---

### Task 1: `db` 包——连接、DSN 归一化与脱敏

**Files:**
- Create: `internal/server/db/db.go`
- Test: `internal/server/db/db_test.go`
- Test: `internal/server/db/db_integration_test.go`

**Interfaces:**
- Produces:
  - `db.Config{DSN string; MaxOpenConns, MaxIdleConns int; ConnMaxLifetime time.Duration}`
  - `db.Open(ctx, Config) (*sql.DB, error)`
  - `db.NormalizeDSN(string) (string, error)`
  - `db.RedactDSN(string) string`

- [ ] **Step 1: Write the failing tests**

`internal/server/db/db_test.go`：

```go
package db

import (
	"context"
	"strings"
	"testing"

	"github.com/go-sql-driver/mysql"
)

func TestNormalizeDSNAddsCharset(t *testing.T) {
	got, err := NormalizeDSN("u:p@tcp(127.0.0.1:3306)/yingzhao")
	if err != nil {
		t.Fatalf("NormalizeDSN: %v", err)
	}
	cfg, err := mysql.ParseDSN(got)
	if err != nil {
		t.Fatalf("结果不是合法 DSN: %q, %v", got, err)
	}
	if cfg.Params["charset"] != "utf8mb4" {
		t.Fatalf("charset = %q，want utf8mb4（dsn=%s）", cfg.Params["charset"], got)
	}
}

func TestNormalizeDSNKeepsExplicitCharset(t *testing.T) {
	got, err := NormalizeDSN("u:p@tcp(127.0.0.1:3306)/yingzhao?charset=utf8mb4&timeout=3s")
	if err != nil {
		t.Fatalf("NormalizeDSN: %v", err)
	}
	cfg, _ := mysql.ParseDSN(got)
	if cfg.Params["charset"] != "utf8mb4" || cfg.Params["timeout"] != "3s" {
		t.Fatalf("已有参数不应被改写：%v", cfg.Params)
	}
}

func TestNormalizeDSNRejectsMalformed(t *testing.T) {
	for _, in := range []string{"", "   ", "127.0.0.1:3306", "yingzhao"} {
		if _, err := NormalizeDSN(in); err == nil {
			t.Fatalf("NormalizeDSN(%q) 应报错", in)
		}
	}
}

func TestRedactDSNHidesPassword(t *testing.T) {
	for _, in := range []string{
		"u:secret@tcp(127.0.0.1:3306)/yingzhao?charset=utf8mb4",
		"u:p@ss@tcp(127.0.0.1:3306)/yingzhao",
	} {
		got := RedactDSN(in)
		if strings.Contains(got, "secret") || strings.Contains(got, "p@ss") {
			t.Fatalf("RedactDSN(%q) = %q，仍含密码", in, got)
		}
	}
	if got := RedactDSN("u@tcp(h:3306)/db"); strings.Contains(got, "***") {
		t.Fatalf("无密码时不应加掩码：%q", got)
	}
	if got := RedactDSN("not-a-dsn"); strings.Contains(got, "not-a-dsn") {
		t.Fatalf("无法解析时不应回显原文：%q", got)
	}
}

func TestOpenRejectsEmptyDSN(t *testing.T) {
	if _, err := Open(context.Background(), Config{}); err == nil {
		t.Fatal("空 DSN 应报错")
	}
}
```

`internal/server/db/db_integration_test.go`：

```go
package db

import (
	"context"
	"os"
	"testing"
)

func TestOpenIntegration(t *testing.T) {
	dsn := os.Getenv("TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("TEST_MYSQL_DSN 未设置")
	}
	conn, err := Open(context.Background(), Config{DSN: dsn})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer conn.Close()
	if err := conn.PingContext(context.Background()); err != nil {
		t.Fatalf("Ping: %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/server/db/ -v`
Expected: 编译失败，`undefined: NormalizeDSN` / `undefined: Open`。

- [ ] **Step 3: Implement**

`internal/server/db/db.go`：

```go
// Package db 管理 Server 的关系型数据库连接（MySQL 8+）。
package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
)

// Config 是数据库连接参数。DSN 形如 user:pass@tcp(host:3306)/dbname。
type Config struct {
	DSN             string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
}

const (
	DefaultMaxOpenConns    = 20
	DefaultMaxIdleConns    = 5
	DefaultConnMaxLifetime = 30 * time.Minute
	pingTimeout            = 5 * time.Second
)

func (c Config) normalize() Config {
	if c.MaxOpenConns <= 0 {
		c.MaxOpenConns = DefaultMaxOpenConns
	}
	if c.MaxIdleConns <= 0 {
		c.MaxIdleConns = DefaultMaxIdleConns
	}
	if c.ConnMaxLifetime <= 0 {
		c.ConnMaxLifetime = DefaultConnMaxLifetime
	}
	return c
}

// Open 打开并验证 MySQL 连接。DSN 为空直接报错：阶段 1 起数据库是硬依赖，
// 不做「悄悄退回文件」的降级。
func Open(ctx context.Context, c Config) (*sql.DB, error) {
	c = c.normalize()
	if strings.TrimSpace(c.DSN) == "" {
		return nil, errors.New("database.dsn 未配置：Server 需要 MySQL 8+")
	}
	dsn, err := NormalizeDSN(c.DSN)
	if err != nil {
		return nil, err
	}
	conn, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("打开 MySQL 连接失败: %w", err)
	}
	conn.SetMaxOpenConns(c.MaxOpenConns)
	conn.SetMaxIdleConns(c.MaxIdleConns)
	conn.SetConnMaxLifetime(c.ConnMaxLifetime)

	pingCtx, cancel := context.WithTimeout(ctx, pingTimeout)
	defer cancel()
	if err := conn.PingContext(pingCtx); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("连接 MySQL 失败（%s）: %w", RedactDSN(c.DSN), err)
	}
	return conn, nil
}

// NormalizeDSN 在未显式指定时补上 charset=utf8mb4。
//
// 用驱动自己的 ParseDSN/FormatDSN，而不是手写字符串切割：
// 密码里可能出现 `?`、`/` 等字符，手切会把 DSN 改坏。
func NormalizeDSN(dsn string) (string, error) {
	dsn = strings.TrimSpace(dsn)
	if dsn == "" {
		return "", errors.New("DSN 为空")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		return "", fmt.Errorf("DSN 解析失败: %w", err)
	}
	if cfg.Params == nil {
		cfg.Params = map[string]string{}
	}
	if _, ok := cfg.Params["charset"]; !ok {
		cfg.Params["charset"] = "utf8mb4"
	}
	return cfg.FormatDSN(), nil
}

// RedactDSN 去掉密码，供日志与错误信息使用。
// 解析失败时返回固定占位，绝不回显原文（原文里可能就是密码）。
func RedactDSN(dsn string) string {
	cfg, err := mysql.ParseDSN(strings.TrimSpace(dsn))
	if err != nil {
		return "<invalid dsn>"
	}
	if cfg.Passwd != "" {
		cfg.Passwd = "***"
	}
	return cfg.FormatDSN()
}
```

（import 增加 `github.com/go-sql-driver/mysql`；`sql.Open` 仍用 `"mysql"` 驱动名。）

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/server/db/ -v`
Expected: 单元测试 PASS；集成测试 SKIP（未设 `TEST_MYSQL_DSN`）。

- [ ] **Step 5: Commit**

```bash
git add internal/server/db/
git commit -m "feat(db): MySQL 连接、DSN 归一化与脱敏"
```

---

### Task 2: `migrate` 包——迁移定义、加载与规划

**Files:**
- Create: `internal/server/migrate/migrate.go`
- Create: `internal/server/migrate/loader.go`
- Create: `internal/server/migrate/migrations/0001_config_documents.sql`
- Test: `internal/server/migrate/migrate_test.go`
- Test: `internal/server/migrate/loader_test.go`

**Interfaces:**
- Produces:
  - `migrate.Migration{Version int; Name, SQL string}`
  - `migrate.Checksum(sql string) string`
  - `migrate.Plan(applied map[int]string, migrations []Migration) ([]Migration, error)`
  - `migrate.Load() ([]Migration, error)`

- [ ] **Step 1: Write the failing tests**

`internal/server/migrate/migrate_test.go`：

```go
package migrate

import "testing"

func TestPlanSkipsAppliedAndChecksChecksum(t *testing.T) {
	ms := []Migration{
		{Version: 2, Name: "b", SQL: "SELECT 2"},
		{Version: 1, Name: "a", SQL: "SELECT 1"},
	}
	pending, err := Plan(map[int]string{1: Checksum("SELECT 1")}, ms)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(pending) != 1 || pending[0].Version != 2 {
		t.Fatalf("pending = %+v", pending)
	}
}

func TestPlanRejectsModifiedAppliedMigration(t *testing.T) {
	ms := []Migration{{Version: 1, Name: "a", SQL: "SELECT 1"}}
	if _, err := Plan(map[int]string{1: Checksum("SELECT 999")}, ms); err == nil {
		t.Fatal("已应用迁移被修改应报错")
	}
}

func TestPlanRejectsDuplicateVersion(t *testing.T) {
	ms := []Migration{
		{Version: 1, Name: "a", SQL: "SELECT 1"},
		{Version: 1, Name: "b", SQL: "SELECT 2"},
	}
	if _, err := Plan(nil, ms); err == nil {
		t.Fatal("重复版本应报错")
	}
}

func TestPlanRejectsIncompleteDefinition(t *testing.T) {
	for _, m := range []Migration{
		{Version: 0, Name: "a", SQL: "SELECT 1"},
		{Version: 1, Name: "", SQL: "SELECT 1"},
		{Version: 1, Name: "a", SQL: "  "},
	} {
		if _, err := Plan(nil, []Migration{m}); err == nil {
			t.Fatalf("不完整定义应报错: %+v", m)
		}
	}
}
```

`internal/server/migrate/loader_test.go`：

```go
package migrate

import "testing"

func TestParseFilename(t *testing.T) {
	for _, tc := range []struct {
		in      string
		version int
		name    string
		ok      bool
	}{
		{"0001_config_documents.sql", 1, "config_documents", true},
		{"0012_audit_chain.sql", 12, "audit_chain", true},
		{"bad.sql", 0, "", false},
		{"0001_.sql", 0, "", false},
	} {
		v, n, ok := parseFilename(tc.in)
		if ok != tc.ok || v != tc.version || n != tc.name {
			t.Fatalf("parseFilename(%q) = (%d,%q,%v)", tc.in, v, n, ok)
		}
	}
}

func TestLoadReturnsOrderedMigrations(t *testing.T) {
	ms, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(ms) == 0 {
		t.Fatal("至少应有一条迁移")
	}
	for i := 1; i < len(ms); i++ {
		if ms[i-1].Version >= ms[i].Version {
			t.Fatalf("迁移未按版本升序: %d >= %d", ms[i-1].Version, ms[i].Version)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/server/migrate/ -v`
Expected: 编译失败（包与函数不存在）。

- [ ] **Step 3: Implement**

`internal/server/migrate/migrate.go`：

```go
// Package migrate 执行有序的 MySQL Schema 迁移。
package migrate

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

// Migration 是一个版本化迁移。**每个文件只放一条 SQL 语句**：
// MySQL 驱动默认不允许一次执行多条，拆成多个版本比打开 multiStatements 安全。
type Migration struct {
	Version int
	Name    string
	SQL     string
}

// Checksum 用于发现「已应用的迁移被改动」。改动已应用的迁移应新增版本，而不是改旧文件。
func Checksum(sql string) string {
	sum := sha256.Sum256([]byte(sql))
	return hex.EncodeToString(sum[:])
}

// Plan 返回待执行的迁移（按版本升序）。applied 是 version → checksum。
func Plan(applied map[int]string, migrations []Migration) ([]Migration, error) {
	sorted := append([]Migration(nil), migrations...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Version < sorted[j].Version })

	seen := make(map[int]bool, len(sorted))
	pending := make([]Migration, 0, len(sorted))
	for _, m := range sorted {
		if m.Version <= 0 || strings.TrimSpace(m.Name) == "" || strings.TrimSpace(m.SQL) == "" {
			return nil, fmt.Errorf("迁移定义不完整: version=%d name=%q", m.Version, m.Name)
		}
		if seen[m.Version] {
			return nil, fmt.Errorf("迁移版本重复: %d", m.Version)
		}
		seen[m.Version] = true

		if sum, ok := applied[m.Version]; ok {
			if sum != Checksum(m.SQL) {
				return nil, fmt.Errorf("已应用的迁移 %d（%s）内容被修改：校验和不一致", m.Version, m.Name)
			}
			continue
		}
		pending = append(pending, m)
	}
	return pending, nil
}
```

`internal/server/migrate/loader.go`：

```go
package migrate

import (
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Load 读取内嵌迁移，按文件名版本号排序。
func Load() ([]Migration, error) {
	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return nil, fmt.Errorf("读取迁移目录失败: %w", err)
	}
	out := make([]Migration, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		version, name, ok := parseFilename(e.Name())
		if !ok {
			return nil, fmt.Errorf("迁移文件名不合法: %s（应为 0001_name.sql）", e.Name())
		}
		data, err := fs.ReadFile(migrationsFS, "migrations/"+e.Name())
		if err != nil {
			return nil, err
		}
		out = append(out, Migration{Version: version, Name: name, SQL: string(data)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Version < out[j].Version })
	return out, nil
}

func parseFilename(name string) (int, string, bool) {
	base := strings.TrimSuffix(name, ".sql")
	verStr, rest, ok := strings.Cut(base, "_")
	if !ok {
		return 0, "", false
	}
	version, err := strconv.Atoi(verStr)
	if err != nil || version <= 0 || strings.TrimSpace(rest) == "" {
		return 0, "", false
	}
	return version, rest, true
}
```

`internal/server/migrate/migrations/0001_config_documents.sql`：

```sql
CREATE TABLE IF NOT EXISTS config_documents (
  namespace   VARCHAR(64)  NOT NULL,
  version     BIGINT       NOT NULL DEFAULT 1,
  payload     JSON         NOT NULL,
  updated_at  BIGINT       NOT NULL,
  updated_by  VARCHAR(128) NOT NULL DEFAULT '',
  PRIMARY KEY (namespace)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/server/migrate/ -v`
Expected: PASS（迁移定义、规划与加载用例）。

- [ ] **Step 5: Commit**

```bash
git add internal/server/migrate/
git commit -m "feat(migrate): 迁移定义、加载与规划"
```

---

### Task 3: `migrate.Apply`——命名锁与执行

**Files:**
- Create: `internal/server/migrate/apply.go`
- Test: `internal/server/migrate/apply_integration_test.go`

**Interfaces:**
- Consumes: Task 1 的 `db.Open`、Task 2 的 `Load/Plan/Checksum`。
- Produces: `migrate.Apply(ctx context.Context, conn *sql.DB, migrations []Migration) (applied int, err error)`

- [ ] **Step 1: Write the failing integration test**

```go
package migrate

import (
	"context"
	"database/sql"
	"os"
	"sync"
	"testing"

	"github.com/mlmw92/yingzhao/internal/server/db"
)

func testDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("TEST_MYSQL_DSN 未设置")
	}
	conn, err := db.Open(context.Background(), db.Config{DSN: dsn})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

func TestApplyIsIdempotent(t *testing.T) {
	conn := testDB(t)
	ms := []Migration{{Version: 9001, Name: "test_table", SQL: "CREATE TABLE IF NOT EXISTS migrate_test(id INT PRIMARY KEY)"}}
	if _, err := Apply(context.Background(), conn, ms); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	n, err := Apply(context.Background(), conn, ms)
	if err != nil || n != 0 {
		t.Fatalf("第二次 Apply 应为 0，实际 %d, %v", n, err)
	}
}

func TestApplyRejectsModifiedMigration(t *testing.T) {
	conn := testDB(t)
	ms := []Migration{{Version: 9002, Name: "t", SQL: "CREATE TABLE IF NOT EXISTS migrate_test2(id INT PRIMARY KEY)"}}
	if _, err := Apply(context.Background(), conn, ms); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	ms[0].SQL = "CREATE TABLE IF NOT EXISTS migrate_test2(id BIGINT PRIMARY KEY)"
	if _, err := Apply(context.Background(), conn, ms); err == nil {
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
			_, err := Apply(context.Background(), conn, ms)
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `TEST_MYSQL_DSN='...' go test ./internal/server/migrate/ -run TestApply -v`
Expected: 编译失败，`undefined: Apply`。

- [ ] **Step 3: Implement**

`internal/server/migrate/apply.go`：

```go
package migrate

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

const (
	migrationsTable = `CREATE TABLE IF NOT EXISTS schema_migrations(
		version INT PRIMARY KEY,
		name VARCHAR(128) NOT NULL,
		checksum VARCHAR(64) NOT NULL,
		applied_at BIGINT NOT NULL
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`
	advisoryLockName = "yingzhao_schema_migrate"
)

// Apply 在命名锁保护下执行待应用的迁移，返回本次应用的数量。
//
// 锁必须与迁移跑在**同一条连接**上：GET_LOCK 是会话级的，
// 从连接池里随手取连接会导致「锁在 A 连接、迁移在 B 连接」，等于没锁。
func Apply(ctx context.Context, pool *sql.DB, migrations []Migration) (int, error) {
	conn, err := pool.Conn(ctx)
	if err != nil {
		return 0, fmt.Errorf("获取迁移连接失败: %w", err)
	}
	defer conn.Close()

	var got sql.NullInt64
	if err := conn.QueryRowContext(ctx, "SELECT GET_LOCK(?, 30)", advisoryLockName).Scan(&got); err != nil {
		return 0, fmt.Errorf("获取迁移锁失败: %w", err)
	}
	if !got.Valid || got.Int64 != 1 {
		return 0, errors.New("获取迁移锁超时：另一个实例正在迁移")
	}
	defer func() {
		// 释放失败不掩盖主错误；连接关闭时 MySQL 也会自动释放会话锁。
		_, _ = conn.ExecContext(context.Background(), "SELECT RELEASE_LOCK(?)", advisoryLockName)
	}()

	if _, err := conn.ExecContext(ctx, migrationsTable); err != nil {
		return 0, fmt.Errorf("创建 schema_migrations 失败: %w", err)
	}

	rows, err := conn.QueryContext(ctx, "SELECT version, checksum FROM schema_migrations")
	if err != nil {
		return 0, fmt.Errorf("读取已应用迁移失败: %w", err)
	}
	applied := map[int]string{}
	for rows.Next() {
		var v int
		var sum string
		if err := rows.Scan(&v, &sum); err != nil {
			rows.Close()
			return 0, err
		}
		applied[v] = sum
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	rows.Close()

	pending, err := Plan(applied, migrations)
	if err != nil {
		return 0, err
	}
	for _, m := range pending {
		sqlText := strings.TrimSpace(m.SQL)
		sqlText = strings.TrimSuffix(sqlText, ";")
		if _, err := conn.ExecContext(ctx, sqlText); err != nil {
			return 0, fmt.Errorf("执行迁移 %d（%s）失败: %w", m.Version, m.Name, err)
		}
		if _, err := conn.ExecContext(ctx,
			"INSERT INTO schema_migrations(version, name, checksum, applied_at) VALUES(?,?,?,?)",
			m.Version, m.Name, Checksum(m.SQL), time.Now().UnixMilli()); err != nil {
			return 0, fmt.Errorf("记录迁移 %d 失败: %w", m.Version, err)
		}
	}
	return len(pending), nil
}
```

（记得在 import 里补 `strings`。）

- [ ] **Step 4: Run tests to verify they pass**

Run: `TEST_MYSQL_DSN='user:pass@tcp(127.0.0.1:3306)/yingzhao_test' go test ./internal/server/migrate/ -run TestApply -v`
Expected: PASS（3 个用例）。

Run（无 DSN 时）：`go test ./internal/server/migrate/ -v`
Expected: 集成用例 SKIP，单元用例 PASS。

- [ ] **Step 5: Commit**

```bash
git add internal/server/migrate/apply.go internal/server/migrate/apply_integration_test.go
git commit -m "feat(migrate): 命名锁保护的迁移执行"
```

---

### Task 4: `configdoc`——文档存储接口、文件替身与契约用例

**Files:**
- Create: `internal/server/configdoc/store.go`
- Create: `internal/server/configdoc/file.go`
- Test: `internal/server/configdoc/store_contract_test.go`
- Test: `internal/server/configdoc/file_test.go`

**Interfaces:**
- Produces:
  - `configdoc.Store` 接口：`Load(ctx, namespace) ([]byte, int64, error)`、`Save(ctx, namespace, payload []byte, expectedVersion int64) (int64, error)`
  - `configdoc.ErrNotFound`、`configdoc.ErrVersionConflict`
  - `configdoc.NewFileStore(root string) *FileStore`

- [ ] **Step 1: Write the failing contract + file tests**

`internal/server/configdoc/store_contract_test.go`：

```go
package configdoc

import (
	"context"
	"errors"
	"testing"
)

// runStoreContract 是文件替身与 MySQL 实现共用的行为契约。
func runStoreContract(t *testing.T, newStore func(t *testing.T) Store) {
	t.Helper()
	ctx := context.Background()

	t.Run("missing namespace", func(t *testing.T) {
		if _, _, err := newStore(t).Load(ctx, "notify"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("Load 缺失命名空间应返回 ErrNotFound，实际 %v", err)
		}
	})

	t.Run("create and load", func(t *testing.T) {
		s := newStore(t)
		v, err := s.Save(ctx, "notify", []byte(`{"a":1}`), 0)
		if err != nil || v != 1 {
			t.Fatalf("首次保存 = %d, %v", v, err)
		}
		payload, gotV, err := s.Load(ctx, "notify")
		if err != nil || gotV != 1 || string(payload) != `{"a":1}` {
			t.Fatalf("Load = %s, %d, %v", payload, gotV, err)
		}
	})

	t.Run("version conflict", func(t *testing.T) {
		s := newStore(t)
		if _, err := s.Save(ctx, "ui", []byte(`{"v":1}`), 0); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Save(ctx, "ui", []byte(`{"v":2}`), 0); !errors.Is(err, ErrVersionConflict) {
			t.Fatalf("错误版本应返回 ErrVersionConflict，实际 %v", err)
		}
		v, err := s.Save(ctx, "ui", []byte(`{"v":2}`), 1)
		if err != nil || v != 2 {
			t.Fatalf("正确版本保存 = %d, %v", v, err)
		}
	})

	t.Run("reject invalid namespace", func(t *testing.T) {
		s := newStore(t)
		for _, ns := range []string{"", "../x", "Notify", "a-b"} {
			if _, err := s.Save(ctx, ns, []byte(`{}`), 0); err == nil {
				t.Fatalf("非法命名空间 %q 应被拒绝", ns)
			}
		}
	})
}
```

`internal/server/configdoc/file_test.go`：

```go
package configdoc

import "testing"

func TestFileStoreContract(t *testing.T) {
	runStoreContract(t, func(t *testing.T) Store {
		return NewFileStore(t.TempDir())
	})
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/server/configdoc/ -v`
Expected: 编译失败（包与接口不存在）。

- [ ] **Step 3: Implement**

`internal/server/configdoc/store.go`：

```go
// Package configdoc 持久化「整份读写」的配置文档。
//
// 文件实现只作测试替身；生产使用 MySQL 实现（config_documents 表）。
package configdoc

import (
	"context"
	"errors"
	"regexp"
)

var (
	// ErrNotFound 表示该命名空间还没有配置。
	ErrNotFound = errors.New("配置文档不存在")
	// ErrVersionConflict 表示 expectedVersion 与当前版本不一致（并发写）。
	ErrVersionConflict = errors.New("配置文档版本冲突")
)

// Store 是一份配置文档的读写接口。
// expectedVersion 为 0 表示「创建」；非 0 表示必须与当前版本一致。
// payload 必须是合法 JSON：MySQL 的 JSON 列与文件替身都按 JSON 存储。
type Store interface {
	Load(ctx context.Context, namespace string) (payload []byte, version int64, err error)
	Save(ctx context.Context, namespace string, payload []byte, expectedVersion int64) (version int64, err error)
}

var namespacePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

func validNamespace(ns string) bool { return namespacePattern.MatchString(ns) }
```

`internal/server/configdoc/file.go`：

```go
package configdoc

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/mlmw92/yingzhao/internal/server/config"
)

// FileStore 是 Store 的文件实现，**只用于测试**：
// 生产路径没有文件后端（见设计件 §1）。
type FileStore struct {
	root string
	mu   sync.Mutex
}

func NewFileStore(root string) *FileStore { return &FileStore{root: root} }

type envelope struct {
	Version int64           `json:"version"`
	Payload json.RawMessage `json:"payload"`
}

func (s *FileStore) path(ns string) (string, error) {
	if !validNamespace(ns) {
		return "", fmt.Errorf("配置命名空间不合法: %q", ns)
	}
	return filepath.Join(s.root, ns+".json"), nil
}

func (s *FileStore) Load(_ context.Context, ns string) ([]byte, int64, error) {
	p, err := s.path(ns)
	if err != nil {
		return nil, 0, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return nil, 0, ErrNotFound
	}
	if err != nil {
		return nil, 0, err
	}
	var env envelope
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, 0, fmt.Errorf("解析配置文档 %s 失败: %w", ns, err)
	}
	return env.Payload, env.Version, nil
}

func (s *FileStore) Save(_ context.Context, ns string, payload []byte, expectedVersion int64) (int64, error) {
	p, err := s.path(ns)
	if err != nil {
		return 0, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	current := int64(0)
	data, err := os.ReadFile(p)
	switch {
	case err == nil:
		var env envelope
		if err := json.Unmarshal(data, &env); err != nil {
			return 0, fmt.Errorf("解析配置文档 %s 失败: %w", ns, err)
		}
		current = env.Version
	case os.IsNotExist(err):
	default:
		return 0, err
	}
	if expectedVersion != current {
		return 0, ErrVersionConflict
	}
	next := current + 1
	out, err := json.Marshal(envelope{Version: next, Payload: payload})
	if err != nil {
		return 0, err
	}
	if err := config.AtomicWrite(p, out); err != nil {
		return 0, err
	}
	return next, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/server/configdoc/ ./internal/server/migrate/ -v`
Expected: PASS（文件契约 4 组子用例 + 迁移加载用例）。

- [ ] **Step 5: Commit**

```bash
git add internal/server/configdoc/
git commit -m "feat(configdoc): 配置文档接口与文件替身"
```

---

### Task 5: `configdoc.MySQLStore`

**Files:**
- Create: `internal/server/configdoc/mysql.go`
- Test: `internal/server/configdoc/mysql_integration_test.go`

**Interfaces:**
- Consumes: Task 1 `db.Open`、Task 3 `migrate.Apply`、Task 4 `Store`/契约用例。
- Produces: `configdoc.NewMySQLStore(*sql.DB) *MySQLStore`

- [ ] **Step 1: Write the failing integration test**

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `TEST_MYSQL_DSN='...' go test ./internal/server/configdoc/ -run TestMySQLStoreContract -v`
Expected: 编译失败，`undefined: NewMySQLStore`。

- [ ] **Step 3: Implement**

`internal/server/configdoc/mysql.go`：

```go
package configdoc

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// MySQLStore 把配置文档存进 config_documents 表。
type MySQLStore struct{ db *sql.DB }

func NewMySQLStore(db *sql.DB) *MySQLStore { return &MySQLStore{db: db} }

func (s *MySQLStore) Load(ctx context.Context, ns string) ([]byte, int64, error) {
	if !validNamespace(ns) {
		return nil, 0, fmt.Errorf("配置命名空间不合法: %q", ns)
	}
	var payload []byte
	var version int64
	err := s.db.QueryRowContext(ctx,
		"SELECT payload, version FROM config_documents WHERE namespace = ?", ns).Scan(&payload, &version)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, 0, ErrNotFound
	}
	if err != nil {
		return nil, 0, err
	}
	return payload, version, nil
}

func (s *MySQLStore) Save(ctx context.Context, ns string, payload []byte, expectedVersion int64) (int64, error) {
	if !validNamespace(ns) {
		return 0, fmt.Errorf("配置命名空间不合法: %q", ns)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	var current int64
	err = tx.QueryRowContext(ctx,
		"SELECT version FROM config_documents WHERE namespace = ? FOR UPDATE", ns).Scan(&current)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		if expectedVersion != 0 {
			return 0, ErrVersionConflict
		}
		current = 0
	case err != nil:
		return 0, err
	default:
		if expectedVersion != current {
			return 0, ErrVersionConflict
		}
	}

	next := current + 1
	now := time.Now().UnixMilli()
	if current == 0 {
		_, err = tx.ExecContext(ctx,
			"INSERT INTO config_documents(namespace, version, payload, updated_at) VALUES(?,?,?,?)",
			ns, next, payload, now)
	} else {
		_, err = tx.ExecContext(ctx,
			"UPDATE config_documents SET version = ?, payload = ?, updated_at = ? WHERE namespace = ?",
			next, payload, now, ns)
	}
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return next, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `TEST_MYSQL_DSN='...' go test ./internal/server/configdoc/ -v`
Expected: 文件契约与 MySQL 契约全部 PASS。

Run（无 DSN）：`go test ./internal/server/configdoc/ -v`
Expected: MySQL 用例 SKIP，文件用例 PASS。

- [ ] **Step 5: Commit**

```bash
git add internal/server/configdoc/mysql.go internal/server/configdoc/mysql_integration_test.go
git commit -m "feat(configdoc): MySQL 配置文档实现"
```

---

### Task 6: `server.yaml` 的 `database` 段

**Files:**
- Modify: `internal/server/config/config.go`
- Test: `internal/server/config/config_test.go`（追加）

**Interfaces:**
- Produces: `config.DatabaseConfig{DSN string; MaxOpenConns, MaxIdleConns, ConnMaxLifetime int}` 与 `Config.Database`

- [ ] **Step 1: Write the failing test**

追加到 `internal/server/config/config_test.go`（若缺 import 补 `os` / `path/filepath`）：

```go
func TestDatabaseConfigDefaultsAndParse(t *testing.T) {
	cfg := Default()
	if cfg.Database.MaxOpenConns != 20 || cfg.Database.MaxIdleConns != 5 || cfg.Database.ConnMaxLifetime != 1800 {
		t.Fatalf("默认值 = %+v", cfg.Database)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "server.yaml")
	if err := os.WriteFile(path, []byte(`
database:
  dsn: "u:p@tcp(127.0.0.1:3306)/yingzhao?charset=utf8mb4"
  maxOpenConns: 50
`), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Database.DSN == "" || got.Database.MaxOpenConns != 50 || got.Database.MaxIdleConns != 5 {
		t.Fatalf("解析结果 = %+v", got.Database)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/server/config/ -run TestDatabaseConfigDefaultsAndParse -v`
Expected: 编译失败，`cfg.Database` 未定义。

- [ ] **Step 3: Implement**

在 `config.go` 的结构体里加：

```go
type Config struct {
	// ...既有字段
	Database DatabaseConfig `yaml:"database"` // 关系型数据库（MySQL 8+）；阶段 1 起为必填
}

// DatabaseConfig 是 MySQL 连接配置。
//
// 阶段 0 只解析、不连接：Server 启动路径不变。阶段 1 起 DSN 为空将拒绝启动。
type DatabaseConfig struct {
	DSN             string `yaml:"dsn"`
	MaxOpenConns    int    `yaml:"maxOpenConns"`
	MaxIdleConns    int    `yaml:"maxIdleConns"`
	ConnMaxLifetime int    `yaml:"connMaxLifetimeSeconds"`
}
```

在 `Default()` 里加：

```go
Database: DatabaseConfig{MaxOpenConns: 20, MaxIdleConns: 5, ConnMaxLifetime: 1800},
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/server/config/ -run TestDatabaseConfigDefaultsAndParse -v`
Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add internal/server/config/config.go internal/server/config/config_test.go
git commit -m "feat(config): 增加 database 段（阶段 0 仅解析）"
```

---

### Task 7: `monitor-server migrate` 子命令

**Files:**
- Create: `cmd/server/migrate.go`
- Modify: `cmd/server/main.go`（`main()` 开头）
- Test: `cmd/server/migrate_test.go`

**Interfaces:**
- Consumes: `config.Load`、`db.Open`、`migrate.Load/Apply`。
- Produces: `runMigrate(args []string, stdout io.Writer) int`

- [ ] **Step 1: Write the failing test**

```go
package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestRunMigrateRequiresDSN(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "server.yaml")
	if err := os.WriteFile(path, []byte("listen: \":8080\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if code := runMigrate([]string{"-config", path}, &out); code == 0 {
		t.Fatalf("缺少 DSN 时 migrate 应失败，输出=%s", out.String())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/server/ -run TestRunMigrateRequiresDSN -v`
Expected: 编译失败，`undefined: runMigrate`。

- [ ] **Step 3: Implement**

`cmd/server/migrate.go`：

```go
package main

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/mlmw92/yingzhao/internal/server/config"
	"github.com/mlmw92/yingzhao/internal/server/db"
	"github.com/mlmw92/yingzhao/internal/server/migrate"
)

// runMigrate 执行 Schema 迁移后退出，不启动 HTTP 服务。
// 返回进程退出码：0 成功，1 配置/连接/迁移失败，2 参数错误。
func runMigrate(args []string, stdout io.Writer) int {
	fs := flag.NewFlagSet("migrate", flag.ContinueOnError)
	fs.SetOutput(stdout)
	cfgPath := fs.String("config", "server.yaml", "配置文件路径")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	cfg, err := config.Load(*cfgPath)
	if err != nil {
		fmt.Fprintf(stdout, "加载配置失败: %v\n", err)
		return 1
	}
	if cfg.Database.DSN == "" {
		fmt.Fprintln(stdout, "database.dsn 未配置：迁移需要 MySQL 8+")
		return 1
	}
	ctx := context.Background()
	pool, err := db.Open(ctx, db.Config{
		DSN:             cfg.Database.DSN,
		MaxOpenConns:    cfg.Database.MaxOpenConns,
		MaxIdleConns:    cfg.Database.MaxIdleConns,
		ConnMaxLifetime: time.Duration(cfg.Database.ConnMaxLifetime) * time.Second,
	})
	if err != nil {
		fmt.Fprintf(stdout, "连接数据库失败: %v\n", err)
		return 1
	}
	defer pool.Close()

	migrations, err := migrate.Load()
	if err != nil {
		fmt.Fprintf(stdout, "加载迁移失败: %v\n", err)
		return 1
	}
	n, err := migrate.Apply(ctx, pool, migrations)
	if err != nil {
		fmt.Fprintf(stdout, "执行迁移失败: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "迁移完成，本次应用 %d 条\n", n)
	return 0
}
```

（补 `time` import。）

`cmd/server/main.go` 的 `main()` 最前面加：

```go
	if len(os.Args) > 1 && os.Args[1] == "migrate" {
		os.Exit(runMigrate(os.Args[2:], os.Stdout))
	}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/server/ -run TestRunMigrateRequiresDSN -v`
Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add cmd/server/migrate.go cmd/server/migrate_test.go cmd/server/main.go
git commit -m "feat(server): 增加 migrate 子命令"
```

---

### Task 8: 文档与全量验证

**Files:**
- Modify: `docs/guides/configuration-reference.md`
- Modify: `docs/superpowers/specs/2026-10-21-mysql-state-backend-design.md`（实施记录）

- [ ] **Step 1: 更新配置参考**

在 `configuration-reference.md` 增加：

```markdown
| `database.dsn` | MySQL 8+ 连接串，形如 `user:pass@tcp(127.0.0.1:3306)/yingzhao?charset=utf8mb4`。**阶段 0 仅解析、不连接**；阶段 1 起为必填 |
| `database.maxOpenConns` | 连接池上限，默认 20 |
| `database.maxIdleConns` | 空闲连接上限，默认 5 |
| `database.connMaxLifetimeSeconds` | 连接最长存活秒数，默认 1800 |
```

- [ ] **Step 2: 跑全量单元测试**

Run: `go test ./...`
Expected: PASS（集成用例 SKIP）。

- [ ] **Step 3: 跑 MySQL 集成测试（dev-server 或本地 MySQL）**

Run:

```bash
TEST_MYSQL_DSN='user:pass@tcp(127.0.0.1:3306)/yingzhao_test?charset=utf8mb4' \
  go test ./internal/server/db/ ./internal/server/migrate/ ./internal/server/configdoc/ -v
```

Expected: 全部 PASS（包括 `TestApplySerializesConcurrentRunners` 与 MySQL 契约）。

- [ ] **Step 4: 验证 `migrate` 子命令**

Run:

```bash
go run ./cmd/server migrate -config /tmp/server-migrate-test.yaml
```

（配置文件里填测试库 DSN。）
Expected: 首次输出「本次应用 1 条」，再次运行输出「本次应用 0 条」。

- [ ] **Step 5: Commit**

```bash
git add docs/guides/configuration-reference.md docs/superpowers/specs/2026-10-21-mysql-state-backend-design.md
git commit -m "docs: 补充 database 配置与阶段 0 实施记录"
```

---

## 阶段 0 完成标准

- `go test ./...` 全绿（无外部依赖）。
- `TEST_MYSQL_DSN` 下 `db` / `migrate` / `configdoc` 三组集成用例全绿。
- `monitor-server migrate` 可重复执行，第二次应用 0 条。
- 现有 Server 启动路径**没有任何变化**：不读 `database.dsn`、不连库、不改任何 store。
- 生产代码里没有文件配置后端的新入口；`FileStore` 只被测试引用。

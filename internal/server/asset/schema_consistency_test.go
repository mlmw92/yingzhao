package asset

// 资产表结构的**跨方言一致性**守卫。
//
// 加一列要同时改 SQLite / MySQL / PostgreSQL / 达梦 四份建表语句（外加存量库的补列）。
// 漏改一处只在"某个方言 + 存量库"的组合上暴露，而单测只跑 SQLite、另外三种方言的
// 集成用例需要真实实例（不在 CI 里）。这条用例把四份 DDL 解析成「表 → 列集合」
// 直接比对，把这类漏改变成一次普通的红灯（对应 issue #1「资产表加列需三处同步」）。

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// tableColumns 是「表名 → 列名（已排序）」。
type tableColumns map[string][]string

// parseDDL 解析一组建表语句；非 CREATE TABLE 的语句（索引、注释）直接跳过。
func parseDDL(t *testing.T, stmts []string) tableColumns {
	t.Helper()
	out := tableColumns{}
	for _, stmt := range stmts {
		table, cols, ok := parseCreateTable(stmt)
		if !ok {
			continue
		}
		out[table] = append(out[table], cols...)
	}
	if len(out) == 0 {
		t.Fatal("没有解析出任何建表语句——解析器失效了，这条守卫会静默通过")
	}
	for table := range out {
		sort.Strings(out[table])
	}
	return out
}

// parseCreateTable 从一条语句里取出表名与列名。
//
// 列名取每段的第一个 token，并跳过约束子句（CONSTRAINT / PRIMARY / UNIQUE / KEY /
// INDEX / FOREIGN / CHECK）。这样不需要理解各方言的类型与约束语法差异。
func parseCreateTable(stmt string) (string, []string, bool) {
	s := strings.TrimSpace(stmt)
	upper := strings.ToUpper(s)
	if !strings.HasPrefix(upper, "CREATE TABLE") {
		return "", nil, false
	}
	rest := strings.TrimSpace(s[len("CREATE TABLE"):])
	if strings.HasPrefix(strings.ToUpper(rest), "IF NOT EXISTS") {
		rest = strings.TrimSpace(rest[len("IF NOT EXISTS"):])
	}
	open := strings.Index(rest, "(")
	if open < 0 {
		return "", nil, false
	}
	table := strings.ToLower(strings.Trim(strings.TrimSpace(rest[:open]), "`\"[]"))
	body, ok := balancedBody(rest[open:])
	if !ok {
		return "", nil, false
	}
	var cols []string
	for _, part := range splitTopLevel(body) {
		fields := strings.Fields(part)
		if len(fields) == 0 {
			continue
		}
		name := strings.ToLower(strings.Trim(fields[0], "`\"[]"))
		// 约束子句可能写成 `UNIQUE(...)`（没有空格），所以按首个标识符判断。
		if i := strings.IndexAny(name, "(,"); i >= 0 {
			name = name[:i]
		}
		switch name {
		case "constraint", "primary", "unique", "key", "index", "foreign", "check":
			continue
		}
		cols = append(cols, name)
	}
	return table, cols, true
}

// balancedBody 取 '(' 开始到与之配对的 ')' 之间的内容（类型里的括号不影响）。
func balancedBody(s string) (string, bool) {
	depth := 0
	for i, r := range s {
		switch r {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return s[1:i], true
			}
		}
	}
	return "", false
}

// splitTopLevel 按顶层逗号切分（VARCHAR(64) 里的逗号不切）。
func splitTopLevel(s string) []string {
	var out []string
	depth := 0
	start := 0
	for i, r := range s {
		switch r {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				out = append(out, s[start:i])
				start = i + 1
			}
		}
	}
	out = append(out, s[start:])
	return out
}

// dialectOnlyTables 是**有意**只存在于部分方言的表。
//
// SQLite 用 `PRAGMA user_version` 记版本，另外三种方言用 schema_version 表——
// 这是唯一一处刻意的结构差异。
var dialectOnlyTables = map[string]bool{"schema_version": true}

// TestSchemaColumnsConsistentAcrossDialects 四种实现的表与列必须完全一致。
func TestSchemaColumnsConsistentAcrossDialects(t *testing.T) {
	dialects := []struct {
		name  string
		cols  tableColumns
		stmts []string
	}{
		{"sqlite", parseDDL(t, schemaStatements), schemaStatements},
		{"mysql", parseDDL(t, mySQLSchemaStatements), mySQLSchemaStatements},
		{"postgres", parseDDL(t, postgresSchemaStatements), postgresSchemaStatements},
		{"dm", parseDDL(t, dmSchemaStatements), dmSchemaStatements},
	}
	base := dialects[0]

	for _, d := range dialects[1:] {
		for table := range base.cols {
			got, ok := d.cols[table]
			if !ok {
				t.Errorf("%s 缺少表 %s（%s 有）", d.name, table, base.name)
				continue
			}
			if diff := setDiff(base.cols[table], got); len(diff) > 0 {
				t.Errorf("表 %s：%s 缺少列 %v", table, d.name, diff)
			}
			if diff := setDiff(got, base.cols[table]); len(diff) > 0 {
				t.Errorf("表 %s：%s 多出列 %v", table, d.name, diff)
			}
		}
		for table := range d.cols {
			if dialectOnlyTables[table] {
				continue
			}
			if _, ok := base.cols[table]; !ok {
				t.Errorf("%s 多出表 %s（%s 没有）", d.name, table, base.name)
			}
		}
	}
}

// TestSQLiteAddedColumnsExistInSchema 存量库补的列必须在新库建表语句里。
//
// 补列是"给老库补上、新库本来就有"的机制：如果新库建表语句里没有这一列，
// 说明加列时只写了 ALTER，schemaStatements 漏了——新库与老库的结构从此不一致。
func TestSQLiteAddedColumnsExistInSchema(t *testing.T) {
	cols := parseDDL(t, schemaStatements)
	for _, c := range sqliteAddedColumns {
		if !contains(cols[c.table], c.column) {
			t.Errorf("补列清单里有 %s.%s，但 schemaStatements 里没有这一列", c.table, c.column)
		}
	}
}

// TestSchemaDocsMatchCode 文档里的最终表结构必须与代码一致。
//
// docs/designs/database-schema/*.sql 由 `go test -tags schemadoc` 生成后手工提交，
// 平时不会重新生成——改了 DDL 忘了导出，文档就会停在旧结构上，而它正是现场建库的依据。
func TestSchemaDocsMatchCode(t *testing.T) {
	dir := filepath.Join("..", "..", "..", "docs", "designs", "database-schema")
	for _, d := range []struct {
		file  string
		stmts []string
	}{
		{"mysql.sql", mySQLSchemaStatements},
		{"postgres.sql", postgresSchemaStatements},
		{"dm.sql", dmSchemaStatements},
	} {
		raw, err := os.ReadFile(filepath.Join(dir, d.file))
		if err != nil {
			t.Fatalf("读取表结构文档 %s 失败（用 `go test -tags schemadoc ./internal/server/asset -run TestDumpSchemaScripts` 重新生成）: %v", d.file, err)
		}
		doc := parseDDL(t, splitSQLFile(string(raw)))
		code := parseDDL(t, d.stmts)
		for table, want := range code {
			got, ok := doc[table]
			if !ok {
				t.Errorf("%s 缺少表 %s", d.file, table)
				continue
			}
			if diff := setDiff(want, got); len(diff) > 0 {
				t.Errorf("%s 的表 %s 缺少列 %v（文档已过期，请重新生成）", d.file, table, diff)
			}
			if diff := setDiff(got, want); len(diff) > 0 {
				t.Errorf("%s 的表 %s 多出列 %v（文档已过期，请重新生成）", d.file, table, diff)
			}
		}
	}
}

// splitSQLFile 把一份 SQL 脚本按分号切成语句，并去掉 `--` 注释行。
func splitSQLFile(text string) []string {
	var lines []string
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "--") {
			continue
		}
		lines = append(lines, line)
	}
	var out []string
	for _, stmt := range strings.Split(strings.Join(lines, "\n"), ";") {
		if strings.TrimSpace(stmt) != "" {
			out = append(out, stmt)
		}
	}
	return out
}

// setDiff 返回 a 里有、b 里没有的元素。
func setDiff(a, b []string) []string {
	in := make(map[string]bool, len(b))
	for _, v := range b {
		in[v] = true
	}
	var out []string
	for _, v := range a {
		if !in[v] {
			out = append(out, v)
		}
	}
	return out
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

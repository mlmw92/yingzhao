//go:build schemadoc

package asset

// 导出最终表结构（新系统初始化用）：
//
//	go test -tags schemadoc ./internal/server/asset -run TestDumpSchemaScripts
//
// 输出 docs/designs/database-schema/{mysql,postgres,dm}.sql。
// MySQL 的注释直接写进 CREATE TABLE；PG/DM 在建表后追加 COMMENT ON。
// 普通 `go test ./...` 不会执行本文件。

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mlmw92/yingzhao/internal/server/migrate"
)

func TestDumpSchemaScripts(t *testing.T) {
	dir := filepath.Join("..", "..", "..", "docs", "designs", "database-schema")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	dumps := []struct {
		name    string
		dialect string
		stmts   []string
	}{
		{"mysql", "mysql", mySQLSchemaStatements},
		{"postgres", "postgres", postgresSchemaStatements},
		{"dm", "dm", dmSchemaStatements},
	}
	for _, d := range dumps {
		path := filepath.Join(dir, d.name+".sql")
		f, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(f, "-- yingzhao 状态库最终表结构（%s）\n\n", d.name)

		// 核心表（config_documents / nodes / node_groups）的 DDL 仍在迁移文件里维护；
		// 跳过 *_comments 迁移——那是给存量库补注释的 ALTER/COMMENT，不是最终结构。
		ms, err := migrate.Load(d.dialect)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range ms {
			if strings.Contains(m.Name, "comments") {
				continue
			}
			emitFinalDDL(f, m.SQL, d.name)
		}
		for _, stmt := range d.stmts {
			emitFinalDDL(f, stmt, d.name)
		}
		if d.name == "dm" {
			fmt.Fprintln(f, "-- ===== 索引 =====")
			for _, idx := range dmIndexStatements {
				fmt.Fprintf(f, "-- %s\n%s;\n", idx.name, idx.ddl)
			}
			fmt.Fprintln(f)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

// emitFinalDDL 输出最终建表语句（含数据库注释）。
func emitFinalDDL(f *os.File, stmt, dialect string) {
	stmt = strings.TrimSpace(stmt)
	if schemaTableOf(stmt) == "" {
		fmt.Fprintf(f, "%s;\n\n", stmt)
		return
	}
	if dialect == "mysql" {
		for _, s := range mysqlWithComments([]string{stmt}) {
			fmt.Fprintf(f, "%s;\n\n", s)
		}
		return
	}
	fmt.Fprintf(f, "%s;\n", stmt)
	for _, c := range commentOnStatements([]string{stmt}) {
		fmt.Fprintf(f, "%s;\n", c)
	}
	fmt.Fprintln(f)
}

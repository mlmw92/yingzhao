package sqlx

import (
	"database/sql"
	"regexp"
)

// DB / Tx 包一层 database/sql，在 MySQL 模式下改写少量 SQL 方言差异。
//
// 只有两类改写，且都只在 MySQL 生效：
//   - 保留字标识符（key、rows）必须加反引号（SQLite 里也能用，但只在 MySQL 时改写，
//     避免影响 SQLite 已有语句的可读性）；
//   - SQLite 的 COLLATE NOCASE 在 MySQL 下不存在：MySQL 默认排序规则本就大小写不敏感。
//
// 不改写字符串字面量里的内容：本项目的 SQL 里没有含这些标识符的字符串字面量。
//
// **这条正则改写路线是过渡方案**：它只认识这里列出的保留字，任何新增的保留字列
// （或绕过 sqlx 包装的裸 *sql.DB 调用）都不会被覆盖。多数据库支持会把这里换成
// 显式的 Dialect 接口（见设计件阶段 2–6 审查记录），新 SQL 请优先使用带反引号的写法。
var reservedIdents = []struct {
	re   *regexp.Regexp
	repl string
}{
	{regexp.MustCompile("(^|[^`\\w])key([^`\\w]|$)"), "$1`key`$2"},
	{regexp.MustCompile("(^|[^`\\w])rows([^`\\w]|$)"), "$1`rows`$2"},
	// schema 是 MySQL 8 保留字；schema_version 这种带下划线的名字不会被匹配（`\w` 挡住）。
	{regexp.MustCompile("(^|[^`\\w])schema([^`\\w]|$)"), "$1`schema`$2"},
}

var collateNoCase = regexp.MustCompile(`(?i)\s+COLLATE\s+NOCASE`)

func rewriteQuery(q string, mysql bool) string {
	if !mysql {
		return q
	}
	for _, ident := range reservedIdents {
		// 反复替换直到不再变化：单次替换遇到连续的同名 token 会漏掉边界。
		for {
			next := ident.re.ReplaceAllString(q, ident.repl)
			if next == q {
				break
			}
			q = next
		}
	}
	return collateNoCase.ReplaceAllString(q, "")
}

// DB 是 *sql.DB 的薄包装。
type DB struct {
	raw   *sql.DB
	mysql bool
}

// Wrap 包装一个 *sql.DB；mysql 决定是否启用方言改写。
func Wrap(db *sql.DB, mysql bool) *DB { return &DB{raw: db, mysql: mysql} }

// Raw 返回底层连接（供需要原生 *sql.DB 的调用方使用）。
func (d *DB) Raw() *sql.DB { return d.raw }

func (d *DB) Exec(query string, args ...any) (sql.Result, error) {
	return d.raw.Exec(rewriteQuery(query, d.mysql), args...)
}

func (d *DB) Query(query string, args ...any) (*sql.Rows, error) {
	return d.raw.Query(rewriteQuery(query, d.mysql), args...)
}

func (d *DB) QueryRow(query string, args ...any) *sql.Row {
	return d.raw.QueryRow(rewriteQuery(query, d.mysql), args...)
}

func (d *DB) Prepare(query string) (*sql.Stmt, error) {
	return d.raw.Prepare(rewriteQuery(query, d.mysql))
}

func (d *DB) Begin() (*Tx, error) {
	tx, err := d.raw.Begin()
	if err != nil {
		return nil, err
	}
	return &Tx{tx: tx, mysql: d.mysql}, nil
}

func (d *DB) Ping() error { return d.raw.Ping() }

// Tx 是 *sql.Tx 的薄包装。
type Tx struct {
	tx    *sql.Tx
	mysql bool
}

func (t *Tx) Exec(query string, args ...any) (sql.Result, error) {
	return t.tx.Exec(rewriteQuery(query, t.mysql), args...)
}

func (t *Tx) Query(query string, args ...any) (*sql.Rows, error) {
	return t.tx.Query(rewriteQuery(query, t.mysql), args...)
}

func (t *Tx) QueryRow(query string, args ...any) *sql.Row {
	return t.tx.QueryRow(rewriteQuery(query, t.mysql), args...)
}

func (t *Tx) Prepare(query string) (*sql.Stmt, error) {
	return t.tx.Prepare(rewriteQuery(query, t.mysql))
}

func (t *Tx) Commit() error   { return t.tx.Commit() }
func (t *Tx) Rollback() error { return t.tx.Rollback() }

// Package sqlx 收纳状态库各目标之间的语法差异（MySQL / PostgreSQL）。
//
// 目标库按「方言」区分：MySQL 与 PostgreSQL 各实现一份 Dialect，
// 执行入口（DB/Tx）统一做 Rewrite + Bind，调用点不再感知具体数据库。
// 瀚高与金仓走 PostgreSQL 协议，使用同一份方言（见 docs/superpowers/specs/2026-10-21-multi-database-support-design.md）。
package sqlx

import (
	"database/sql"
	"fmt"
	"regexp"
	"strings"
)

// Dialect 是一个目标数据库的语法规则。
type Dialect interface {
	// Name 是方言名（mysql / postgres），用于日志与分支。
	Name() string
	// Driver 是传给 sql.Open 的驱动名。
	Driver() string
	// Bind 把 `?` 占位符换成本方言的形式（PostgreSQL 为 $1..$n）。
	Bind(query string) string
	// Rewrite 处理标识符引用等与占位符无关的差异。
	Rewrite(query string) string
	// Upsert 生成 upsert 子句；sets 用 `col=excluded.col` 写法。
	Upsert(cols, sets string) string
	// UpsertNothing 生成「冲突时保持原样」的子句。
	UpsertNothing(col string) string
	// FromDual 让不带 FROM 的 SELECT 成立。
	FromDual() string
}

// MySQL 返回 MySQL 8 方言。
func MySQL() Dialect { return mysqlDialect{} }

// Postgres 返回 PostgreSQL 方言（瀚高 / 金仓同样适用）。
func Postgres() Dialect { return postgresDialect{} }

// DM 返回达梦 DM8 方言。
func DM() Dialect { return dmDialect{} }

// SQLite 返回 SQLite 方言。
//
// 生产路径不使用 SQLite（设计件阶段 5 起只作为测试替身与旧库迁移源），
// 保留它是为了让资产包的既有测试与一次性迁移读取器继续可用。
func SQLite() Dialect { return sqliteDialect{} }

// ---------- SQLite ----------

type sqliteDialect struct{}

func (sqliteDialect) Name() string            { return "sqlite" }
func (sqliteDialect) Driver() string          { return "sqlite" }
func (sqliteDialect) Bind(q string) string    { return q }
func (sqliteDialect) Rewrite(q string) string { return q }
func (sqliteDialect) Upsert(cols, sets string) string {
	return " ON CONFLICT(" + cols + ") DO UPDATE SET " + sets
}
func (sqliteDialect) UpsertNothing(col string) string {
	return " ON CONFLICT(" + col + ") DO NOTHING"
}
func (sqliteDialect) FromDual() string { return "" }

// ---------- MySQL ----------

type mysqlDialect struct{}

func (mysqlDialect) Name() string         { return "mysql" }
func (mysqlDialect) Driver() string       { return "mysql" }
func (mysqlDialect) Bind(q string) string { return q }
func (mysqlDialect) Rewrite(q string) string {
	return rewriteReservedIdents(q, "`")
}
func (mysqlDialect) Upsert(cols, sets string) string {
	out := excludedRef.ReplaceAllString(sets, "VALUES($1)")
	out = strings.ReplaceAll(out, "MAX(", "GREATEST(")
	return " ON DUPLICATE KEY UPDATE " + out
}
func (mysqlDialect) UpsertNothing(col string) string {
	col = strings.Trim(col, "`")
	return " ON DUPLICATE KEY UPDATE " + col + "=" + col
}
func (mysqlDialect) FromDual() string { return " FROM DUAL" }

// ---------- PostgreSQL ----------

type postgresDialect struct{}

func (postgresDialect) Name() string         { return "postgres" }
func (postgresDialect) Driver() string       { return "postgres" }
func (postgresDialect) Bind(q string) string { return bindDollar(q) }
func (postgresDialect) Rewrite(q string) string {
	// 项目里少量 SQL 直接写了 MySQL 反引号（例如资产属性的 `key` 列）。
	// PG 用双引号；SQL 文本里没有含反引号的字符串字面量。
	q = strings.ReplaceAll(q, "`", `"`)
	return rewriteReservedIdents(q, `"`)
}
func (postgresDialect) Upsert(cols, sets string) string {
	out := strings.ReplaceAll(sets, "MAX(", "GREATEST(")
	return " ON CONFLICT(" + quoteIdentList(cols, `"`) + ") DO UPDATE SET " + out
}
func (postgresDialect) UpsertNothing(col string) string {
	return " ON CONFLICT(" + quoteIdentList(col, `"`) + ") DO NOTHING"
}
func (postgresDialect) FromDual() string { return "" }

// ---------- 通用 ----------

var excludedRef = regexp.MustCompile(`excluded\.([a-z_]+)`)

// reservedIdents 是需要按方言加引号的标识符。
//
// 这条正则改写路线只认识这里列出的保留字；新 SQL 优先直接写带引号的标识符。
// 多数据库支持没有把它换成完整 SQL 解析器：项目里的 SQL 是受控的、数量有限，
// 表驱动用例覆盖了每条保留字路径。
var reservedIdents = []struct {
	name string
	re   *regexp.Regexp
}{
	{"key", regexp.MustCompile("(^|[^`\"\\w])key([^`\"\\w]|$)")},
	{"rows", regexp.MustCompile("(^|[^`\"\\w])rows([^`\"\\w]|$)")},
	// schema 是 MySQL 8 保留字；schema_version 这种带下划线的名字不会被匹配。
	{"schema", regexp.MustCompile("(^|[^`\"\\w])schema([^`\"\\w]|$)")},
}

func rewriteReservedIdents(q, quote string) string {
	for _, ident := range reservedIdents {
		repl := "${1}" + quote + ident.name + quote + "${2}"
		for {
			next := ident.re.ReplaceAllString(q, repl)
			if next == q {
				break
			}
			q = next
		}
	}
	return q
}

// bindDollar 把 `?` 换成 `$1..$n`，跳过字符串字面量与双引号标识符。
func bindDollar(q string) string {
	var b strings.Builder
	b.Grow(len(q) + 8)
	n := 0
	inSingle, inDouble := false, false
	for i := 0; i < len(q); i++ {
		c := q[i]
		switch {
		case inSingle:
			b.WriteByte(c)
			if c == '\'' {
				// '' 是转义的单引号，不结束字面量。
				if i+1 < len(q) && q[i+1] == '\'' {
					b.WriteByte(q[i+1])
					i++
				} else {
					inSingle = false
				}
			}
		case inDouble:
			b.WriteByte(c)
			if c == '"' {
				inDouble = false
			}
		case c == '\'':
			inSingle = true
			b.WriteByte(c)
		case c == '"':
			inDouble = true
			b.WriteByte(c)
		case c == '?':
			n++
			b.WriteString("$")
			b.WriteString(itoa(n))
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

func quoteIdentList(cols, quote string) string {
	parts := strings.Split(cols, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		p = strings.Trim(p, "`\"")
		out = append(out, quote+p+quote)
	}
	return strings.Join(out, ", ")
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// InsertID 执行一条 INSERT 并返回自增主键。
//
// PostgreSQL 驱动不支持 LastInsertId，必须走 `RETURNING id`；MySQL 用
// Exec + LastInsertId。调用方传入的 SQL 用 `?` 占位符，由本函数按方言绑定。
func InsertID(ex Execer, d Dialect, query string, args ...any) (int64, error) {
	q := d.Bind(d.Rewrite(query))
	if d.Name() == "postgres" {
		var id int64
		if err := ex.QueryRow(q+" RETURNING id", args...).Scan(&id); err != nil {
			return 0, err
		}
		return id, nil
	}
	res, err := ex.Exec(q, args...)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// Execer 是 InsertID 需要的最小执行能力（*DB / *Tx 都满足）。
type Execer interface {
	Exec(query string, args ...any) (sql.Result, error)
	QueryRow(query string, args ...any) *sql.Row
}

// Parse 按名称返回方言。
func Parse(name string) (Dialect, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", "mysql":
		return MySQL(), nil
	case "postgres", "postgresql", "pg", "highgo", "kingbase":
		return Postgres(), nil
	case "dm", "dameng":
		return DM(), nil
	default:
		return nil, fmt.Errorf("不支持的数据库驱动: %q（可选: mysql|postgres|highgo|kingbase|dm）", name)
	}
}

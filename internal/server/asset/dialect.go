package asset

import "github.com/mlmw92/yingzhao/internal/server/sqlx"

// dialect 是本包对 SQL 方言的依赖点（实现见 internal/server/sqlx）。
// SQLite 只用于测试与旧库迁移；生产是 MySQL 或 PostgreSQL 系。

// tableSuffix 是建表语句末尾的引擎/字符集声明（只有 MySQL 需要）。
func tableSuffix(d sqlx.Dialect) string {
	if d.Name() == "mysql" {
		return " ENGINE=InnoDB DEFAULT CHARSET=utf8mb4"
	}
	return ""
}

// quote 按方言引用一个标识符（用于需要拼接进 SQL 的列名）。
func (s *Store) quote(ident string) string {
	if s.d.Name() == "mysql" {
		return "`" + ident + "`"
	}
	// PostgreSQL / 达梦：双引号。达梦的 DDL 对 key/schema/rows 同样加引号，
	// 两边必须一致（未加引号的标识符会被 DM 统一大写）。
	return `"` + ident + `"`
}
func (s *Store) mysql() bool { return s.d.Name() == "mysql" }

// onConflict 生成 upsert 子句（sets 用 SQLite 写法）。
func (s *Store) onConflict(cols, sets string) string { return s.d.Upsert(cols, sets) }

// onConflictNothing 生成「冲突时保持原样」子句。
func (s *Store) onConflictNothing(col string) string { return s.d.UpsertNothing(col) }

// dualFrom 让不带 FROM 的 SELECT 成立。
func (s *Store) dualFrom() string { return s.d.FromDual() }

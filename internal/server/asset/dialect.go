package asset

import "github.com/mlmw92/yingzhao/internal/server/sqlx"

// dialect 区分 SQLite 与 MySQL 的少量语法差异（实现见 internal/server/sqlx）。
type dialect int

const (
	dialectSQLite dialect = iota
	dialectMySQL
)

func (s *Store) mysql() bool { return s.d == dialectMySQL }

// onConflict 生成 upsert 子句（sets 用 SQLite 写法）。
func (s *Store) onConflict(cols, sets string) string { return sqlx.Upsert(s.mysql(), cols, sets) }

// onConflictNothing 生成「冲突时保持原样」子句。
func (s *Store) onConflictNothing(col string) string { return sqlx.UpsertNothing(s.mysql(), col) }

// dualFrom 让不带 FROM 的 SELECT 在 MySQL 下成立。
func (s *Store) dualFrom() string { return sqlx.DualFrom(s.mysql()) }

// Package sqlx 收纳 SQLite 与 MySQL 之间少量无法回避的语法差异。
//
// 平台同时支持两种后端：SQLite（默认单机）与 MySQL（状态外置）。绝大多数查询两端通用
// （都用 ? 占位），需要区分的只有 upsert 子句与「不带 FROM 的 SELECT」。
package sqlx

import (
	"regexp"
	"strings"
)

var excludedRef = regexp.MustCompile(`excluded\.([a-z_]+)`)

// Upsert 生成 upsert 子句。sets 用 SQLite 写法（`col=excluded.col, ...`），
// MySQL 下自动改写成 `col=VALUES(col), ...`，并把标量 MAX 换成 GREATEST。
func Upsert(mysql bool, cols, sets string) string {
	if !mysql {
		return " ON CONFLICT(" + cols + ") DO UPDATE SET " + sets
	}
	out := excludedRef.ReplaceAllString(sets, "VALUES($1)")
	out = strings.ReplaceAll(out, "MAX(", "GREATEST(")
	return " ON DUPLICATE KEY UPDATE " + out
}

// UpsertNothing 生成「冲突时保持原样」的子句。MySQL 没有 DO NOTHING。
func UpsertNothing(mysql bool, col string) string {
	if !mysql {
		return " ON CONFLICT(" + col + ") DO NOTHING"
	}
	return " ON DUPLICATE KEY UPDATE " + col + "=" + col
}

// DualFrom 让不带 FROM 的 SELECT 在 MySQL 下成立（SQLite 允许省略）。
func DualFrom(mysql bool) string {
	if mysql {
		return " FROM DUAL"
	}
	return ""
}

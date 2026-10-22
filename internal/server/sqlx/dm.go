package sqlx

import (
	"regexp"
	"strings"
)

// 达梦（DM8）方言。
//
// 与 MySQL / PostgreSQL 的关键差异：
//   - 没有 ON DUPLICATE KEY / ON CONFLICT：upsert 用 MERGE INTO；
//   - 未加引号的标识符会被转成大写（Oracle 风格），因此**不能**像 PG 那样给
//     key/schema 加双引号——只有真正保留的 rows 需要引用，且所有引用处保持一致；
//   - 占位符仍是 ?，不需要重绑定。
type dmDialect struct{}

func (dmDialect) Name() string         { return "dm" }
func (dmDialect) Driver() string       { return "dm" }
func (dmDialect) Bind(q string) string { return q }

func (dmDialect) Rewrite(q string) string {
	// 显式反引号（少数 SQL 直接写了 `key`）在 DM 下改成双引号；
	// key/schema/rows 是保留字或与系统对象冲突，统一加引号，且 DDL 里同样加。
	q = strings.ReplaceAll(q, "`", `"`)
	q = rewriteReservedIdents(q, `"`)
	return dmMerge(q)
}

func (dmDialect) Upsert(cols, sets string) string {
	out := strings.ReplaceAll(sets, "MAX(", "GREATEST(")
	return " ON CONFLICT(" + dmIdentList(cols) + ") DO UPDATE SET " + out
}

func (dmDialect) UpsertNothing(col string) string {
	return " ON CONFLICT(" + dmIdentList(col) + ") DO NOTHING"
}

func (dmDialect) FromDual() string { return " FROM DUAL" }

// dmReserved 是需要加双引号的标识符（DM 下未加引号的会统一大写，
// 加引号后是小写，两者不能混用，所以 DDL 与 DML 必须一致）。
var dmReserved = map[string]bool{"key": true, "schema": true, "rows": true}

func dmIdent(col string) string {
	col = strings.Trim(strings.TrimSpace(col), "`\"")
	if dmReserved[strings.ToLower(col)] {
		return `"` + col + `"`
	}
	return col
}

func dmIdentList(cols string) string {
	parts := strings.Split(cols, ",")
	for i, p := range parts {
		parts[i] = dmIdent(p)
	}
	return strings.Join(parts, ", ")
}

// dmMerge 把 `INSERT ... VALUES ... ON CONFLICT(...) DO UPDATE SET ...`
// 改写成 DM 的 `MERGE INTO`。
//
// 输入形状由本项目自己的 SQL 构造器生成（列清单 + 值清单 + upsert 子句），
// 不是任意 SQL；解析器只处理这一种形状，遇到不认识的形状原样返回（让数据库
// 报错，而不是悄悄改成别的东西）。
func dmMerge(q string) string {
	idx := indexFold(q, " on conflict(")
	if idx < 0 {
		return q
	}
	head, tail := q[:idx], q[idx+len(" on conflict("):]
	closeIdx := strings.Index(tail, ")")
	if closeIdx < 0 {
		return q
	}
	conflictCols := dmIdentList(tail[:closeIdx])
	afterConflict := strings.TrimSpace(tail[closeIdx+1:])

	table, cols, values, ok := parseInsert(head)
	if !ok {
		return q
	}

	var b strings.Builder
	b.WriteString("MERGE INTO ")
	b.WriteString(table)
	b.WriteString(" t USING (SELECT ")
	for i := range cols {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(values[i])
		b.WriteString(" AS ")
		b.WriteString(cols[i])
	}
	b.WriteString(" FROM DUAL) s ON (")
	keys := strings.Split(conflictCols, ",")
	for i, k := range keys {
		k = strings.TrimSpace(k)
		if i > 0 {
			b.WriteString(" AND ")
		}
		b.WriteString("t.")
		b.WriteString(k)
		b.WriteString(" = s.")
		b.WriteString(k)
	}
	b.WriteString(")")

	if !strings.HasPrefix(strings.ToUpper(afterConflict), "DO NOTHING") {
		sets := strings.TrimSpace(afterConflict)
		sets = strings.TrimPrefix(sets, "DO UPDATE SET ")
		sets = strings.TrimPrefix(sets, "do update set ")
		b.WriteString(" WHEN MATCHED THEN UPDATE SET ")
		b.WriteString(qualifyDMUpdate(sets, cols, table))
	}
	b.WriteString(" WHEN NOT MATCHED THEN INSERT (")
	b.WriteString(strings.Join(cols, ", "))
	b.WriteString(") VALUES (")
	for i := range cols {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString("s.")
		b.WriteString(cols[i])
	}
	b.WriteString(")")
	return b.String()
}

// parseInsert 解析 `INSERT INTO t(c1,c2,...) VALUES(v1,v2,...)`。
func parseInsert(q string) (table string, cols, values []string, ok bool) {
	upper := strings.ToUpper(q)
	if !strings.HasPrefix(strings.TrimSpace(upper), "INSERT INTO ") {
		return "", nil, nil, false
	}
	rest := strings.TrimSpace(q[len("INSERT INTO "):])
	open := strings.Index(rest, "(")
	if open < 0 {
		return "", nil, nil, false
	}
	table = strings.TrimSpace(rest[:open])
	closeIdx := strings.Index(rest[open:], ")")
	if closeIdx < 0 {
		return "", nil, nil, false
	}
	cols = splitTopLevel(rest[open+1 : open+closeIdx])
	after := strings.TrimSpace(rest[open+closeIdx+1:])
	if !strings.HasPrefix(strings.ToUpper(after), "VALUES(") {
		return "", nil, nil, false
	}
	valuesPart := after[len("VALUES("):]
	last := strings.LastIndex(valuesPart, ")")
	if last < 0 {
		return "", nil, nil, false
	}
	values = splitTopLevel(valuesPart[:last])
	if len(cols) != len(values) {
		return "", nil, nil, false
	}
	for i := range cols {
		cols[i] = strings.TrimSpace(cols[i])
	}
	return table, cols, values, true
}

// splitTopLevel 按顶层逗号切分（忽略括号内与引号内的逗号）。
func splitTopLevel(s string) []string {
	var out []string
	depth := 0
	inSingle, inDouble := false, false
	start := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case inSingle:
			if c == '\'' {
				inSingle = false
			}
		case inDouble:
			if c == '"' {
				inDouble = false
			}
		case c == '\'':
			inSingle = true
		case c == '"':
			inDouble = true
		case c == '(':
			depth++
		case c == ')':
			depth--
		case c == ',' && depth == 0:
			out = append(out, strings.TrimSpace(s[start:i]))
			start = i + 1
		}
	}
	out = append(out, strings.TrimSpace(s[start:]))
	return out
}

// qualifyDMUpdate 把 sets 里的 excluded.x 改成 s.x，并给裸目标列加 t. 前缀。
func qualifyDMUpdate(sets string, cols []string, table string) string {
	sets = strings.ReplaceAll(sets, "excluded.", "s.")
	// MERGE 的目标表在 SET 里只能用别名 t；原 SQL 里的 `<table>.col` 要换掉。
	sets = strings.ReplaceAll(sets, table+".", "t.")
	for _, col := range cols {
		re := regexp.MustCompile("(^|[^.\\w])" + regexp.QuoteMeta(col) + "([^\\w]|$)")
		sets = re.ReplaceAllString(sets, "${1}t."+col+"${2}")
	}
	// 上一步可能把已经带 s. 的列也误伤（前面是 '.'，被 [^.\w] 挡住，不会）。
	return sets
}

func indexFold(s, sub string) int {
	return strings.Index(strings.ToLower(s), strings.ToLower(sub))
}

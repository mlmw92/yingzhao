package migrate

import "strings"

// splitStatements 把一份迁移 SQL 按顶层分号拆成多条语句。
//
// 为什么要拆：MySQL 驱动默认不允许一次 Exec 多条语句；达梦驱动同样不可靠。
// 拆开后每个方言都能执行「一个迁移文件里有多条 ALTER/COMMENT」。
// 扫描时跳过单引号字符串、双引号标识符，并丢弃 `--` 行注释。
func splitStatements(sql string) []string {
	var out []string
	var b strings.Builder
	inSingle, inDouble := false, false
	for i := 0; i < len(sql); i++ {
		c := sql[i]
		switch {
		case inSingle:
			b.WriteByte(c)
			if c == '\'' {
				if i+1 < len(sql) && sql[i+1] == '\'' {
					b.WriteByte(sql[i+1])
					i++ // '' 转义
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
		case c == '-' && i+1 < len(sql) && sql[i+1] == '-':
			for i < len(sql) && sql[i] != '\n' {
				i++
			}
			b.WriteByte('\n')
		case c == ';':
			if s := strings.TrimSpace(b.String()); s != "" {
				out = append(out, s)
			}
			b.Reset()
		default:
			b.WriteByte(c)
		}
	}
	if s := strings.TrimSpace(b.String()); s != "" {
		out = append(out, s)
	}
	return out
}

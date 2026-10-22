package migrate

import (
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
)

//go:embed migrations/*/*.sql
var migrationsFS embed.FS

// Load 读取指定方言的内嵌迁移，按文件名版本号排序。
//
// 目录按方言拆分（mysql / postgres）：同一版本号在不同方言下是各自独立的 SQL，
// 校验和也各算各的。现有 MySQL 文件只移动了目录、内容未变，已部署实例不受影响。
func Load(dialect string) ([]Migration, error) {
	dialect = strings.ToLower(strings.TrimSpace(dialect))
	switch dialect {
	case "mysql", "postgres", "dm":
	default:
		return nil, fmt.Errorf("不支持的迁移方言: %q（可选: mysql|postgres|dm）", dialect)
	}
	dir := "migrations/" + dialect
	entries, err := fs.ReadDir(migrationsFS, dir)
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
		data, err := fs.ReadFile(migrationsFS, dir+"/"+e.Name())
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

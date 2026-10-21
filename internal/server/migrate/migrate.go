// Package migrate 执行有序的 MySQL Schema 迁移。
package migrate

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

// Migration 是一个版本化迁移。**每个文件只放一条 SQL 语句**：
// MySQL 驱动默认不允许一次执行多条，拆成多个版本比打开 multiStatements 安全。
type Migration struct {
	Version int
	Name    string
	SQL     string
}

// Checksum 用于发现「已应用的迁移被改动」。改动已应用的迁移应新增版本，而不是改旧文件。
func Checksum(sql string) string {
	sum := sha256.Sum256([]byte(sql))
	return hex.EncodeToString(sum[:])
}

// Plan 返回待执行的迁移（按版本升序）。applied 是 version → checksum。
func Plan(applied map[int]string, migrations []Migration) ([]Migration, error) {
	sorted := append([]Migration(nil), migrations...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Version < sorted[j].Version })

	seen := make(map[int]bool, len(sorted))
	pending := make([]Migration, 0, len(sorted))
	for _, m := range sorted {
		if m.Version <= 0 || strings.TrimSpace(m.Name) == "" || strings.TrimSpace(m.SQL) == "" {
			return nil, fmt.Errorf("迁移定义不完整: version=%d name=%q", m.Version, m.Name)
		}
		if seen[m.Version] {
			return nil, fmt.Errorf("迁移版本重复: %d", m.Version)
		}
		seen[m.Version] = true

		if sum, ok := applied[m.Version]; ok {
			if sum != Checksum(m.SQL) {
				return nil, fmt.Errorf("已应用的迁移 %d（%s）内容被修改：校验和不一致", m.Version, m.Name)
			}
			continue
		}
		pending = append(pending, m)
	}
	return pending, nil
}

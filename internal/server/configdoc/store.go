// Package configdoc 持久化「整份读写」的配置文档。
//
// 文件实现只作测试替身；生产使用 MySQL 实现（config_documents 表）。
package configdoc

import (
	"context"
	"errors"
	"regexp"
)

var (
	// ErrNotFound 表示该命名空间还没有配置。
	ErrNotFound = errors.New("配置文档不存在")
	// ErrVersionConflict 表示 expectedVersion 与当前版本不一致（并发写）。
	ErrVersionConflict = errors.New("配置文档版本冲突")
)

// Store 是一份配置文档的读写接口。
// expectedVersion 为 0 表示「创建」；非 0 表示必须与当前版本一致。
// payload 必须是合法 JSON：MySQL 的 JSON 列与文件替身都按 JSON 存储。
type Store interface {
	Load(ctx context.Context, namespace string) (payload []byte, version int64, err error)
	Save(ctx context.Context, namespace string, payload []byte, expectedVersion int64) (version int64, err error)
}

var namespacePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

func validNamespace(ns string) bool { return namespacePattern.MatchString(ns) }

package configdoc

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/nebula/monitor/internal/server/config"
)

// FileStore 是 Store 的文件实现，**只用于测试**：
// 生产路径没有文件后端（见设计件 §1）。
type FileStore struct {
	root string
	mu   sync.Mutex
}

func NewFileStore(root string) *FileStore { return &FileStore{root: root} }

type envelope struct {
	Version int64           `json:"version"`
	Payload json.RawMessage `json:"payload"`
}

func (s *FileStore) path(ns string) (string, error) {
	if !validNamespace(ns) {
		return "", fmt.Errorf("配置命名空间不合法: %q", ns)
	}
	return filepath.Join(s.root, ns+".json"), nil
}

func (s *FileStore) Load(_ context.Context, ns string) ([]byte, int64, error) {
	p, err := s.path(ns)
	if err != nil {
		return nil, 0, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return nil, 0, ErrNotFound
	}
	if err != nil {
		return nil, 0, err
	}
	var env envelope
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, 0, fmt.Errorf("解析配置文档 %s 失败: %w", ns, err)
	}
	return env.Payload, env.Version, nil
}

func (s *FileStore) Save(_ context.Context, ns string, payload []byte, expectedVersion int64) (int64, error) {
	p, err := s.path(ns)
	if err != nil {
		return 0, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	current := int64(0)
	data, err := os.ReadFile(p)
	switch {
	case err == nil:
		var env envelope
		if err := json.Unmarshal(data, &env); err != nil {
			return 0, fmt.Errorf("解析配置文档 %s 失败: %w", ns, err)
		}
		current = env.Version
	case os.IsNotExist(err):
	default:
		return 0, err
	}
	if expectedVersion != current {
		return 0, ErrVersionConflict
	}
	next := current + 1
	out, err := json.Marshal(envelope{Version: next, Payload: payload})
	if err != nil {
		return 0, err
	}
	if err := config.AtomicWrite(p, out); err != nil {
		return 0, err
	}
	return next, nil
}

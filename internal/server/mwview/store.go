// Package mwview 维护「中间件监控页面展示哪些类型」的全局配置。
//
// 背景：平台内置的中间件类型越来越多，但单个部署通常只跑其中几类；
// 未部署的类型在页面上永远显示 0 实例，只会增加噪音。本包把「展示哪些类型」
// 存为一份可编辑的清单（空 = 全部展示），供前端 Tab 与总览卡片过滤。
package mwview

import (
	"errors"
	"sync"

	"github.com/mlmw92/yingzhao/internal/server/configdoc"
	"gopkg.in/yaml.v3"
)

// Store 是展示配置的持久化存储（空清单 = 全部展示）。
type Store struct {
	mu sync.RWMutex
	p  configdoc.Persister
}

// Default 全局默认存储，main 启动时注入持久化后端。
var Default = &Store{}

// SetPath 用文件持久化（测试与迁移源）。
func (s *Store) SetPath(path string) { s.SetPersister(configdoc.NewFilePersister(path)) }

// SetPersister 设置持久化后端。
func (s *Store) SetPersister(p configdoc.Persister) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.p = p
}

type viewFile struct {
	Enabled []string `yaml:"enabled"`
}

// Enabled 返回启用的类型清单；空切片表示「全部展示」。
func (s *Store) Enabled() []string {
	s.mu.RLock()
	p := s.p
	s.mu.RUnlock()
	if p == nil {
		return nil
	}
	data, err := p.Load()
	if err != nil {
		return nil
	}
	var vf viewFile
	if err := yaml.Unmarshal(data, &vf); err != nil {
		return nil
	}
	return vf.Enabled
}

// Save 保存启用的类型清单（空清单 = 恢复全部展示）。
func (s *Store) Save(enabled []string) error {
	s.mu.RLock()
	p := s.p
	s.mu.RUnlock()
	if p == nil {
		return errors.New("中间件展示配置未启用")
	}
	data, err := yaml.Marshal(viewFile{Enabled: enabled})
	if err != nil {
		return err
	}
	return p.Save(data)
}

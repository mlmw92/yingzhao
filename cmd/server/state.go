package main

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/mlmw92/yingzhao/internal/server/config"
	"github.com/mlmw92/yingzhao/internal/server/configdoc"
	"github.com/mlmw92/yingzhao/internal/server/db"
	"github.com/mlmw92/yingzhao/internal/server/migrate"
)

// 配置文档的命名空间。**改名等于换存储**：迁移脚本按这些名字写入，
// 改名后旧数据会「消失」（实际仍在库里，只是新名字读不到）。
const (
	nsNotify       = "notify"
	nsUI           = "ui"
	nsScreen       = "screen"
	nsDashboards   = "dashboards"
	nsPipeline     = "alert_pipeline"
	nsInhibit      = "alert_inhibit"
	nsGrouping     = "alert_grouping"
	nsMaintenance  = "maintenance"
	nsRetention    = "retention"
	nsAI           = "ai"
	nsMWView       = "middleware_view"
	nsReportSched  = "report_schedule"
	nsInspectSched = "inspect_schedule"
)

// openStateDB 打开 MySQL 状态库并执行 Schema 迁移。
// 阶段 1 起数据库是硬依赖：连不上就启动失败，不退回文件。
func openStateDB(ctx context.Context, cfg config.Config) (*sql.DB, error) {
	pool, err := db.Open(ctx, db.Config{
		DSN:             cfg.Database.DSN,
		MaxOpenConns:    cfg.Database.MaxOpenConns,
		MaxIdleConns:    cfg.Database.MaxIdleConns,
		ConnMaxLifetime: time.Duration(cfg.Database.ConnMaxLifetime) * time.Second,
	})
	if err != nil {
		return nil, err
	}
	migrations, err := migrate.Load()
	if err != nil {
		_ = pool.Close()
		return nil, fmt.Errorf("加载迁移失败: %w", err)
	}
	if _, err := migrate.Apply(ctx, pool, migrations); err != nil {
		_ = pool.Close()
		return nil, fmt.Errorf("执行迁移失败: %w", err)
	}
	return pool, nil
}

// persisterFactory 按命名空间构造配置文档的后端。
type persisterFactory struct {
	store *configdoc.MySQLStore
	key   []byte
}

// plain 返回不加密的文档后端（普通配置）。
func (f persisterFactory) plain(ns string) configdoc.Persister {
	return configdoc.NewDBPersister(f.store, ns, nil)
}

// sensitive 返回加密的文档后端（含密钥/口令的配置）。
func (f persisterFactory) sensitive(ns string) configdoc.Persister {
	return configdoc.NewDBPersister(f.store, ns, f.key)
}

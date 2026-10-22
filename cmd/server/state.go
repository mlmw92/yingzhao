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
	"github.com/mlmw92/yingzhao/internal/server/sqlx"
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

	// 阶段 2：协作状态
	nsRules          = "alert_rules"
	nsRulesSeed      = "alert_rules_seed"
	nsDialtest       = "dialtest"
	nsApprovals      = "approvals"
	nsInvestigations = "ai_investigations"
	nsUsers          = "users"
	// 安全与审批配置（原先都是本地 JSON 文件）
	nsSecurity       = "security"
	nsDefense        = "defense_tasks"
	nsApprovalConfig = "approval_config"
)

// openStateDB 打开状态库并执行 Schema 迁移，返回连接与方言。
// 数据库是硬依赖：连不上就启动失败，不退回文件。
func openStateDB(ctx context.Context, cfg config.Config) (*sql.DB, sqlx.Dialect, int, error) {
	d, err := sqlx.Parse(cfg.Database.Driver)
	if err != nil {
		return nil, nil, 0, err
	}
	pool, err := db.Open(ctx, db.Config{
		Driver:          cfg.Database.Driver,
		DSN:             cfg.Database.DSN,
		MaxOpenConns:    cfg.Database.MaxOpenConns,
		MaxIdleConns:    cfg.Database.MaxIdleConns,
		ConnMaxLifetime: time.Duration(cfg.Database.ConnMaxLifetime) * time.Second,
	})
	if err != nil {
		return nil, nil, 0, err
	}
	migrations, err := migrate.Load(d.Name())
	if err != nil {
		_ = pool.Close()
		return nil, nil, 0, fmt.Errorf("加载迁移失败: %w", err)
	}
	applied, err := migrate.Apply(ctx, pool, d, migrations)
	if err != nil {
		_ = pool.Close()
		return nil, nil, 0, fmt.Errorf("执行迁移失败: %w", err)
	}
	return pool, d, applied, nil
}

// persisterFactory 按命名空间构造配置文档的后端。
type persisterFactory struct {
	store *configdoc.SQLStore
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

// plainJSON 返回不加密、格式标签为 json 的文档后端（审批单、AI 调查记录）。
// 格式只是元数据（读取时不做转换），但标错会误导排障时手工解码。
func (f persisterFactory) plainJSON(ns string) configdoc.Persister {
	return configdoc.NewDBPersisterFormat(f.store, ns, nil, "json")
}

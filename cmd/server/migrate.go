package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/mlmw92/yingzhao/internal/server/config"
	"github.com/mlmw92/yingzhao/internal/server/db"
	"github.com/mlmw92/yingzhao/internal/server/migrate"
	"github.com/mlmw92/yingzhao/internal/server/sqlx"
)

// runMigrate 执行 Schema 迁移后退出，不启动 HTTP 服务。
// 返回进程退出码：0 成功，1 配置/连接/迁移失败，2 参数错误。
func runMigrate(args []string, stdout io.Writer) int {
	fs := flag.NewFlagSet("migrate", flag.ContinueOnError)
	fs.SetOutput(stdout)
	cfgPath := fs.String("config", "server.yaml", "配置文件路径")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	cfg, err := config.Load(*cfgPath)
	if err != nil {
		fmt.Fprintf(stdout, "加载配置失败: %v\n", err)
		return 1
	}
	if cfg.Database.DSN == "" {
		fmt.Fprintln(stdout, "database.dsn 未配置：迁移需要 MySQL 8+ 或 PostgreSQL")
		return 1
	}
	dialect, err := sqlx.Parse(cfg.Database.Driver)
	if err != nil {
		fmt.Fprintf(stdout, "数据库驱动不可用: %v\n", err)
		return 1
	}
	ctx := context.Background()
	pool, err := db.Open(ctx, db.Config{
		Driver:          cfg.Database.Driver,
		DSN:             cfg.Database.DSN,
		MaxOpenConns:    cfg.Database.MaxOpenConns,
		MaxIdleConns:    cfg.Database.MaxIdleConns,
		ConnMaxLifetime: time.Duration(cfg.Database.ConnMaxLifetime) * time.Second,
	})
	if err != nil {
		fmt.Fprintf(stdout, "连接数据库失败: %v\n", err)
		return 1
	}
	defer pool.Close()

	migrations, err := migrate.Load(dialect.Name())
	if err != nil {
		fmt.Fprintf(stdout, "加载迁移失败: %v\n", err)
		return 1
	}
	n, err := migrate.Apply(ctx, pool, dialect, migrations)
	if err != nil {
		fmt.Fprintf(stdout, "执行迁移失败: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "迁移完成，本次应用 %d 条\n", n)
	return 0
}

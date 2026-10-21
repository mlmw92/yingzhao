// Package db 管理 Server 的关系型数据库连接（MySQL 8+）。
package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
)

// Config 是数据库连接参数。DSN 形如 user:pass@tcp(host:3306)/dbname。
type Config struct {
	DSN             string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
}

const (
	DefaultMaxOpenConns    = 20
	DefaultMaxIdleConns    = 5
	DefaultConnMaxLifetime = 30 * time.Minute
	pingTimeout            = 5 * time.Second
)

func (c Config) normalize() Config {
	if c.MaxOpenConns <= 0 {
		c.MaxOpenConns = DefaultMaxOpenConns
	}
	if c.MaxIdleConns <= 0 {
		c.MaxIdleConns = DefaultMaxIdleConns
	}
	if c.ConnMaxLifetime <= 0 {
		c.ConnMaxLifetime = DefaultConnMaxLifetime
	}
	return c
}

// Open 打开并验证 MySQL 连接。DSN 为空直接报错：阶段 1 起数据库是硬依赖，
// 不做「悄悄退回文件」的降级。
func Open(ctx context.Context, c Config) (*sql.DB, error) {
	c = c.normalize()
	if strings.TrimSpace(c.DSN) == "" {
		return nil, errors.New("database.dsn 未配置：Server 需要 MySQL 8+")
	}
	dsn, err := NormalizeDSN(c.DSN)
	if err != nil {
		return nil, err
	}
	conn, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("打开 MySQL 连接失败: %w", err)
	}
	conn.SetMaxOpenConns(c.MaxOpenConns)
	conn.SetMaxIdleConns(c.MaxIdleConns)
	conn.SetConnMaxLifetime(c.ConnMaxLifetime)

	pingCtx, cancel := context.WithTimeout(ctx, pingTimeout)
	defer cancel()
	if err := conn.PingContext(pingCtx); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("连接 MySQL 失败（%s）: %w", RedactDSN(c.DSN), err)
	}
	return conn, nil
}

// NormalizeDSN 在未显式指定时补上 charset=utf8mb4。
//
// 用驱动自己的 ParseDSN/FormatDSN，而不是手写字符串切割：
// 密码里可能出现 `?`、`/` 等字符，手切会把 DSN 改坏。
func NormalizeDSN(dsn string) (string, error) {
	dsn = strings.TrimSpace(dsn)
	if dsn == "" {
		return "", errors.New("DSN 为空")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		return "", fmt.Errorf("DSN 解析失败: %w", err)
	}
	if cfg.Params == nil {
		cfg.Params = map[string]string{}
	}
	if _, ok := cfg.Params["charset"]; !ok {
		cfg.Params["charset"] = "utf8mb4"
	}
	return cfg.FormatDSN(), nil
}

// RedactDSN 去掉密码，供日志与错误信息使用。
// 解析失败时返回固定占位，绝不回显原文（原文里可能就是密码）。
func RedactDSN(dsn string) string {
	cfg, err := mysql.ParseDSN(strings.TrimSpace(dsn))
	if err != nil {
		return "<invalid dsn>"
	}
	if cfg.Passwd != "" {
		cfg.Passwd = "***"
	}
	return cfg.FormatDSN()
}

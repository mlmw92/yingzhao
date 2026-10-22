// Package db 管理 Server 的关系型数据库连接（MySQL 8+ / PostgreSQL 系）。
package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"         // PostgreSQL / 瀚高 / 金仓 的驱动注册
	_ "gitee.com/chunanyong/dm"   // 达梦 DM8 驱动注册
)

// Config 是数据库连接参数。
type Config struct {
	// Driver 是目标库类型：mysql（默认）/ postgres / highgo / kingbase。
	Driver          string
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
	if strings.TrimSpace(c.Driver) == "" {
		c.Driver = "mysql"
	}
	return c
}

// DriverName 把配置里的驱动名映射成 database/sql 的驱动名。
func DriverName(driver string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(driver)) {
	case "", "mysql":
		return "mysql", nil
	case "postgres", "postgresql", "pg", "highgo", "kingbase":
		return "postgres", nil
	case "dm", "dameng":
		return "dm", nil
	default:
		return "", fmt.Errorf("不支持的数据库驱动: %q（可选: mysql|postgres|highgo|kingbase）", driver)
	}
}

// Open 打开并验证数据库连接。DSN 为空直接报错：阶段 1 起数据库是硬依赖，
// 不做「悄悄退回文件」的降级。
func Open(ctx context.Context, c Config) (*sql.DB, error) {
	c = c.normalize()
	if strings.TrimSpace(c.DSN) == "" {
		return nil, errors.New("database.dsn 未配置：Server 需要 MySQL 8+ 或 PostgreSQL")
	}
	driver, err := DriverName(c.Driver)
	if err != nil {
		return nil, err
	}
	dsn := strings.TrimSpace(c.DSN)
	if driver == "mysql" {
		dsn, err = NormalizeDSN(dsn)
		if err != nil {
			return nil, err
		}
	}
	conn, err := sql.Open(driver, dsn)
	if err != nil {
		return nil, fmt.Errorf("打开数据库连接失败: %w", err)
	}
	conn.SetMaxOpenConns(c.MaxOpenConns)
	conn.SetMaxIdleConns(c.MaxIdleConns)
	conn.SetConnMaxLifetime(c.ConnMaxLifetime)

	pingCtx, cancel := context.WithTimeout(ctx, pingTimeout)
	defer cancel()
	if err := conn.PingContext(pingCtx); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("连接数据库失败（driver=%s，%s）: %w", driver, RedactDSN(c.Driver, c.DSN), err)
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
	// 让 UPDATE 回报「匹配到的行」而不是「值真的变了的行」。
	// 代码里有把 RowsAffected==0 当作「行不存在」的地方（保存资产类型模型），
	// 默认语义下内容没变的保存会被误判成 404。DELETE 不受这个开关影响。
	cfg.ClientFoundRows = true
	return cfg.FormatDSN(), nil
}

var pqPassword = regexp.MustCompile(`(?i)(password\s*=\s*)([^\s]+)`)

// RedactDSN 去掉密码，供日志与错误信息使用。
// 解析失败时返回固定占位，绝不回显原文（原文里可能就是密码）。
func RedactDSN(driver, dsn string) string {
	dsn = strings.TrimSpace(dsn)
	driver, err := DriverName(driver)
	if err != nil {
		return "<invalid driver>"
	}
	if driver == "mysql" {
		cfg, err := mysql.ParseDSN(dsn)
		if err != nil {
			return "<invalid dsn>"
		}
		if cfg.Passwd != "" {
			cfg.Passwd = "***"
		}
		return cfg.FormatDSN()
	}
	// PostgreSQL / 达梦：URL 形态（postgres://、dm://）与 key=value 形态。
	if strings.Contains(dsn, "://") {
		u, err := url.Parse(dsn)
		if err != nil {
			return "<invalid dsn>"
		}
		if _, ok := u.User.Password(); ok {
			u.User = url.UserPassword(u.User.Username(), "***")
		}
		return u.String()
	}
	return pqPassword.ReplaceAllString(dsn, "${1}***")
}

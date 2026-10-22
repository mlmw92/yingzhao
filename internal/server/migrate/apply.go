package migrate

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mlmw92/yingzhao/internal/server/sqlx"
)

const (
	advisoryLockName = "yingzhao_schema_migrate"
	// PostgreSQL 咨询锁的键（固定 int64，取 "yingzhao" 的 ASCII 片段）。
	advisoryLockKey = int64(0x79696e7a68616f)
)

// Apply 在命名锁保护下执行待应用的迁移，返回本次应用的数量。
//
// 锁必须与迁移跑在**同一条连接**上：MySQL 的 GET_LOCK 与 PostgreSQL 的
// advisory lock 都是会话级的，从连接池里随手取连接会导致「锁在 A 连接、
// 迁移在 B 连接」，等于没锁。
func Apply(ctx context.Context, pool *sql.DB, d sqlx.Dialect, migrations []Migration) (int, error) {
	conn, err := pool.Conn(ctx)
	if err != nil {
		return 0, fmt.Errorf("获取迁移连接失败: %w", err)
	}
	defer conn.Close()

	unlock, err := acquireMigrationLock(ctx, conn, d)
	if err != nil {
		return 0, err
	}
	defer unlock()

	ddl := `CREATE TABLE IF NOT EXISTS schema_migrations(
		version INT PRIMARY KEY,
		name VARCHAR(128) NOT NULL,
		checksum VARCHAR(64) NOT NULL,
		applied_at BIGINT NOT NULL
	)`
	if d.Name() == "mysql" {
		ddl += " ENGINE=InnoDB DEFAULT CHARSET=utf8mb4"
	}
	if _, err := conn.ExecContext(ctx, d.Bind(d.Rewrite(ddl))); err != nil {
		return 0, fmt.Errorf("创建 schema_migrations 失败: %w", err)
	}

	rows, err := conn.QueryContext(ctx, d.Bind(d.Rewrite("SELECT version, checksum FROM schema_migrations")))
	if err != nil {
		return 0, fmt.Errorf("读取已应用迁移失败: %w", err)
	}
	applied := map[int]string{}
	for rows.Next() {
		var v int
		var sum string
		if err := rows.Scan(&v, &sum); err != nil {
			rows.Close()
			return 0, err
		}
		applied[v] = sum
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	rows.Close()

	pending, err := Plan(applied, migrations)
	if err != nil {
		return 0, err
	}
	for _, m := range pending {
		sqlText := strings.TrimSpace(m.SQL)
		sqlText = strings.TrimSuffix(sqlText, ";")
		if _, err := conn.ExecContext(ctx, sqlText); err != nil {
			return 0, fmt.Errorf("执行迁移 %d（%s）失败: %w", m.Version, m.Name, err)
		}
		if _, err := conn.ExecContext(ctx,
			d.Bind(d.Rewrite("INSERT INTO schema_migrations(version, name, checksum, applied_at) VALUES(?,?,?,?)")),
			m.Version, m.Name, Checksum(m.SQL), time.Now().UnixMilli()); err != nil {
			return 0, fmt.Errorf("记录迁移 %d 失败: %w", m.Version, err)
		}
	}
	return len(pending), nil
}

// acquireMigrationLock 取得迁移锁，返回释放函数。
func acquireMigrationLock(ctx context.Context, conn *sql.Conn, d sqlx.Dialect) (func(), error) {
	switch d.Name() {
	case "postgres":
		deadline := time.Now().Add(30 * time.Second)
		for {
			var ok bool
			if err := conn.QueryRowContext(ctx,
				d.Bind(d.Rewrite("SELECT pg_try_advisory_lock(?)")), advisoryLockKey).Scan(&ok); err != nil {
				return nil, fmt.Errorf("获取迁移锁失败: %w", err)
			}
			if ok {
				return func() {
					_, _ = conn.ExecContext(context.Background(),
						d.Bind(d.Rewrite("SELECT pg_advisory_unlock(?)")), advisoryLockKey)
				}, nil
			}
			if time.Now().After(deadline) {
				return nil, errors.New("获取迁移锁超时：另一个实例正在迁移")
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(500 * time.Millisecond):
			}
		}
	default:
		var got sql.NullInt64
		if err := conn.QueryRowContext(ctx,
			d.Bind(d.Rewrite("SELECT GET_LOCK(?, 30)")), advisoryLockName).Scan(&got); err != nil {
			return nil, fmt.Errorf("获取迁移锁失败: %w", err)
		}
		if !got.Valid || got.Int64 != 1 {
			return nil, errors.New("获取迁移锁超时：另一个实例正在迁移")
		}
		return func() {
			// 释放失败不掩盖主错误；连接关闭时数据库也会自动释放会话锁。
			_, _ = conn.ExecContext(context.Background(),
				d.Bind(d.Rewrite("SELECT RELEASE_LOCK(?)")), advisoryLockName)
		}, nil
	}
}

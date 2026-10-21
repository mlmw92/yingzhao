package migrate

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	migrationsTable = `CREATE TABLE IF NOT EXISTS schema_migrations(
		version INT PRIMARY KEY,
		name VARCHAR(128) NOT NULL,
		checksum VARCHAR(64) NOT NULL,
		applied_at BIGINT NOT NULL
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`
	advisoryLockName = "nebula_schema_migrate"
)

// Apply 在命名锁保护下执行待应用的迁移，返回本次应用的数量。
//
// 锁必须与迁移跑在**同一条连接**上：GET_LOCK 是会话级的，
// 从连接池里随手取连接会导致「锁在 A 连接、迁移在 B 连接」，等于没锁。
func Apply(ctx context.Context, pool *sql.DB, migrations []Migration) (int, error) {
	conn, err := pool.Conn(ctx)
	if err != nil {
		return 0, fmt.Errorf("获取迁移连接失败: %w", err)
	}
	defer conn.Close()

	var got sql.NullInt64
	if err := conn.QueryRowContext(ctx, "SELECT GET_LOCK(?, 30)", advisoryLockName).Scan(&got); err != nil {
		return 0, fmt.Errorf("获取迁移锁失败: %w", err)
	}
	if !got.Valid || got.Int64 != 1 {
		return 0, errors.New("获取迁移锁超时：另一个实例正在迁移")
	}
	defer func() {
		// 释放失败不掩盖主错误；连接关闭时 MySQL 也会自动释放会话锁。
		_, _ = conn.ExecContext(context.Background(), "SELECT RELEASE_LOCK(?)", advisoryLockName)
	}()

	if _, err := conn.ExecContext(ctx, migrationsTable); err != nil {
		return 0, fmt.Errorf("创建 schema_migrations 失败: %w", err)
	}

	rows, err := conn.QueryContext(ctx, "SELECT version, checksum FROM schema_migrations")
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
			"INSERT INTO schema_migrations(version, name, checksum, applied_at) VALUES(?,?,?,?)",
			m.Version, m.Name, Checksum(m.SQL), time.Now().UnixMilli()); err != nil {
			return 0, fmt.Errorf("记录迁移 %d 失败: %w", m.Version, err)
		}
	}
	return len(pending), nil
}

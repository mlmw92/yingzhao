package configdoc

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/lib/pq"

	"github.com/mlmw92/yingzhao/internal/server/sqlx"
)

// isConflictError 把驱动层的并发冲突翻译成契约里的 ErrVersionConflict。
//
// MySQL：1062 duplicate key / 1213 deadlock / 1205 lock wait timeout。
// PostgreSQL：23505 unique_violation / 40P01 deadlock_detected / 55P03 lock_not_available。
// 三者都表示「另一个写入者先完成了」：调用方应当重读版本后重试，
// 而不是看到一个原始驱动错误。
func isConflictError(err error) bool {
	var me *mysql.MySQLError
	if errors.As(err, &me) {
		return me.Number == 1062 || me.Number == 1213 || me.Number == 1205
	}
	var pe *pq.Error
	if errors.As(err, &pe) {
		return pe.Code == "23505" || pe.Code == "40P01" || pe.Code == "55P03"
	}
	return false
}

// SQLStore 把配置文档存进 config_documents 表（MySQL / PostgreSQL 系）。
type SQLStore struct{ db *sqlx.DB }

// NewMySQLStore 是 MySQL 的构造入口（保留原名，调用点无需感知方言层）。
func NewMySQLStore(db *sql.DB) *SQLStore { return NewSQLStore(db, sqlx.MySQL()) }

// NewSQLStore 用指定方言构造。
func NewSQLStore(db *sql.DB, d sqlx.Dialect) *SQLStore { return &SQLStore{db: sqlx.Wrap(db, d)} }

func (s *SQLStore) Load(ctx context.Context, ns string) ([]byte, int64, error) {
	if !validNamespace(ns) {
		return nil, 0, fmt.Errorf("配置命名空间不合法: %q", ns)
	}
	var payload []byte
	var version int64
	err := s.db.QueryRowContext(ctx,
		"SELECT payload, version FROM config_documents WHERE namespace = ?", ns).Scan(&payload, &version)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, 0, ErrNotFound
	}
	if err != nil {
		return nil, 0, err
	}
	return payload, version, nil
}

func (s *SQLStore) Save(ctx context.Context, ns string, payload []byte, expectedVersion int64) (int64, error) {
	if !validNamespace(ns) {
		return 0, fmt.Errorf("配置命名空间不合法: %q", ns)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	var current int64
	err = tx.QueryRowContext(ctx,
		"SELECT version FROM config_documents WHERE namespace = ? FOR UPDATE", ns).Scan(&current)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		if expectedVersion != 0 {
			return 0, ErrVersionConflict
		}
		current = 0
	case err != nil:
		if isConflictError(err) {
			return 0, ErrVersionConflict
		}
		return 0, err
	default:
		if expectedVersion != current {
			return 0, ErrVersionConflict
		}
	}

	next := current + 1
	now := time.Now().UnixMilli()
	if current == 0 {
		_, err = tx.ExecContext(ctx,
			"INSERT INTO config_documents(namespace, version, payload, updated_at) VALUES(?,?,?,?)",
			ns, next, payload, now)
	} else {
		_, err = tx.ExecContext(ctx,
			"UPDATE config_documents SET version = ?, payload = ?, updated_at = ? WHERE namespace = ?",
			next, payload, now, ns)
	}
	if err != nil {
		if isConflictError(err) {
			return 0, ErrVersionConflict
		}
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		if isConflictError(err) {
			return 0, ErrVersionConflict
		}
		return 0, err
	}
	return next, nil
}

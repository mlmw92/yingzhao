package configdoc

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// MySQLStore 把配置文档存进 config_documents 表。
type MySQLStore struct{ db *sql.DB }

func NewMySQLStore(db *sql.DB) *MySQLStore { return &MySQLStore{db: db} }

func (s *MySQLStore) Load(ctx context.Context, ns string) ([]byte, int64, error) {
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

func (s *MySQLStore) Save(ctx context.Context, ns string, payload []byte, expectedVersion int64) (int64, error) {
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
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return next, nil
}

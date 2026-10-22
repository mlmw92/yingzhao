package sqlx

import (
	"context"
	"database/sql"
	"time"
)

// DB 是 *sql.DB 的薄包装，按方言改写并绑定 SQL。
type DB struct {
	raw *sql.DB
	d   Dialect
}

// Wrap 包装一个 *sql.DB。
func Wrap(db *sql.DB, d Dialect) *DB { return &DB{raw: db, d: d} }

// Raw 返回底层连接（供需要原生 *sql.DB 的调用方使用）。
func (d *DB) Raw() *sql.DB { return d.raw }

// Dialect 返回本连接使用的方言。
func (d *DB) Dialect() Dialect { return d.d }

func (d *DB) prepare(query string) string { return d.d.Bind(d.d.Rewrite(query)) }

func (d *DB) Exec(query string, args ...any) (sql.Result, error) {
	return d.raw.Exec(d.prepare(query), args...)
}

func (d *DB) Query(query string, args ...any) (*sql.Rows, error) {
	return d.raw.Query(d.prepare(query), args...)
}

func (d *DB) QueryRow(query string, args ...any) *sql.Row {
	return d.raw.QueryRow(d.prepare(query), args...)
}

func (d *DB) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return d.raw.ExecContext(ctx, d.prepare(query), args...)
}

func (d *DB) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return d.raw.QueryContext(ctx, d.prepare(query), args...)
}

func (d *DB) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return d.raw.QueryRowContext(ctx, d.prepare(query), args...)
}

func (d *DB) BeginTx(ctx context.Context, opts *sql.TxOptions) (*Tx, error) {
	tx, err := d.raw.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return &Tx{tx: tx, d: d.d}, nil
}

func (d *DB) Prepare(query string) (*sql.Stmt, error) {
	return d.raw.Prepare(d.prepare(query))
}

func (d *DB) Begin() (*Tx, error) {
	tx, err := d.raw.Begin()
	if err != nil {
		return nil, err
	}
	return &Tx{tx: tx, d: d.d}, nil
}

func (d *DB) Ping() error { return d.raw.Ping() }

// InsertID 执行 INSERT 并返回自增主键（按方言选择 LastInsertId 或 RETURNING id）。
func (d *DB) InsertID(query string, args ...any) (int64, error) {
	return InsertID(d, d.d, query, args...)
}

// Tx 是 *sql.Tx 的薄包装。
type Tx struct {
	tx *sql.Tx
	d  Dialect
}

func (t *Tx) prepare(query string) string { return t.d.Bind(t.d.Rewrite(query)) }

func (t *Tx) Exec(query string, args ...any) (sql.Result, error) {
	return t.tx.Exec(t.prepare(query), args...)
}

func (t *Tx) Query(query string, args ...any) (*sql.Rows, error) {
	return t.tx.Query(t.prepare(query), args...)
}

func (t *Tx) QueryRow(query string, args ...any) *sql.Row {
	return t.tx.QueryRow(t.prepare(query), args...)
}

func (t *Tx) Prepare(query string) (*sql.Stmt, error) {
	return t.tx.Prepare(t.prepare(query))
}

// InsertID 执行 INSERT 并返回自增主键。
func (t *Tx) InsertID(query string, args ...any) (int64, error) {
	return InsertID(t, t.d, query, args...)
}

func (t *Tx) Commit() error   { return t.tx.Commit() }
func (t *Tx) Rollback() error { return t.tx.Rollback() }

func (t *Tx) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return t.tx.ExecContext(ctx, t.prepare(query), args...)
}

func (t *Tx) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return t.tx.QueryContext(ctx, t.prepare(query), args...)
}

func (t *Tx) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return t.tx.QueryRowContext(ctx, t.prepare(query), args...)
}

// OpTimeout 是单次数据库操作的上限。
//
// 不加超时的话，DB 网络黑洞或锁等待会让请求永久挂起（HTTP 连接不返回，
// 进程退出也会被拖住）。10 秒足够任何正常读写。
const OpTimeout = 10 * time.Second

// WithTimeout 给一次数据库操作加上默认超时。
func WithTimeout(parent context.Context) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	return context.WithTimeout(parent, OpTimeout)
}

package node

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/mlmw92/yingzhao/internal/model"
	"github.com/mlmw92/yingzhao/internal/server/sqlx"
)

// Persist 是节点元数据的持久化后端。
//
// 节点有两种写入频率差三个数量级的数据（设计件 §5.2.1）：
//   - **注册信息**（主机名、分组、标签、模式、主机信息）：几乎不变，变化才写；
//   - **心跳与状态**：每次上报都变，由 Manager 按固定间隔合并写入。
//
// 文件后端不区分二者（整份重写，与既有行为一致）；MySQL 后端靠 CoalesceHeartbeat 区分。
type Persist interface {
	Load() (metaFile, error)
	// SaveMeta 保存注册信息（含删除已移除的节点与分组）。
	SaveMeta(mf metaFile) error
	// FlushHeartbeat 只刷新心跳与状态，不触碰注册信息。
	FlushHeartbeat(states []heartbeat) error
	// CoalesceHeartbeat 表示心跳写入由 Manager 定时合并；
	// 文件后端返回 false——它的整份写已经把心跳带上了。
	CoalesceHeartbeat() bool
	// Name 用于日志。
	Name() string
}

// heartbeat 是一次心跳落库的最小字段集。
type heartbeat struct {
	Hostname string
	Status   string
	LastSeen int64
}

// ---- 文件后端（测试与迁移源）----

type filePersist struct{ path string }

func (p *filePersist) Name() string                     { return p.path }
func (p *filePersist) CoalesceHeartbeat() bool          { return false }
func (p *filePersist) FlushHeartbeat([]heartbeat) error { return nil }

// Load 读取 meta 文件；文件不存在或内容损坏都按「空」处理（与既有行为一致：
// 全新安装没有 meta 文件，损坏的文件不该阻止服务启动）。
func (p *filePersist) Load() (metaFile, error) {
	data, err := os.ReadFile(p.path)
	if err != nil {
		return metaFile{}, nil
	}
	var mf metaFile
	if err := json.Unmarshal(data, &mf); err != nil {
		return metaFile{}, nil
	}
	return mf, nil
}

func (p *filePersist) SaveMeta(mf metaFile) error {
	data, err := json.MarshalIndent(mf, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dirOf(p.path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p.path, data, 0o644)
}

// ---- MySQL 后端（生产）----

// mysqlPersist 只面向 MySQL，因此直接写 MySQL 方言，不需要方言层。
type mysqlPersist struct{ db *sql.DB }

func NewMySQLPersist(db *sql.DB) Persist { return &mysqlPersist{db: db} }

func (p *mysqlPersist) Name() string            { return "mysql:nodes" }
func (p *mysqlPersist) CoalesceHeartbeat() bool { return true }

func (p *mysqlPersist) Load() (metaFile, error) {
	var mf metaFile

	rows, err := p.db.Query(`SELECT hostname, mode, display_name, ip, os, arch, group_name,
		labels, version, host_info, log_sources, status, last_seen, created_at FROM nodes`)
	if err != nil {
		return mf, fmt.Errorf("读取节点表失败: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var n model.Node
		var labels, hostInfo, logSources []byte
		if err := rows.Scan(&n.Hostname, &n.Mode, &n.DisplayName, &n.IP, &n.OS, &n.Arch,
			&n.Group, &labels, &n.Version, &hostInfo, &logSources, &n.Status,
			&n.LastSeen, &n.CreatedAt); err != nil {
			return mf, fmt.Errorf("解析节点行失败: %w", err)
		}
		if len(labels) > 0 {
			_ = json.Unmarshal(labels, &n.Labels)
		}
		if len(hostInfo) > 0 {
			_ = json.Unmarshal(hostInfo, &n.HostInfo)
		}
		if len(logSources) > 0 {
			_ = json.Unmarshal(logSources, &n.LogSources)
		}
		mf.Nodes = append(mf.Nodes, n)
	}
	if err := rows.Err(); err != nil {
		return mf, fmt.Errorf("读取节点表失败: %w", err)
	}

	grows, err := p.db.Query(`SELECT name, description, created_at FROM node_groups`)
	if err != nil {
		return mf, fmt.Errorf("读取分组表失败: %w", err)
	}
	defer grows.Close()
	for grows.Next() {
		var g model.Group
		if err := grows.Scan(&g.Name, &g.Description, &g.CreatedAt); err != nil {
			return mf, fmt.Errorf("解析分组行失败: %w", err)
		}
		mf.Groups = append(mf.Groups, g)
	}
	if err := grows.Err(); err != nil {
		return mf, fmt.Errorf("读取分组表失败: %w", err)
	}
	return mf, nil
}

// SaveMeta 是「整份同步」：内存即真相，upsert 全部节点与分组，再删掉不在列表里的行。
//
// 为什么不做增量：注册信息的变化（新增节点、改分组、改标签）是低频人工/首次上报事件，
// 一次全量同步的代价可以忽略；而增量需要一套「哪些字段变了」的簿记，反而容易漏。
func (p *mysqlPersist) SaveMeta(mf metaFile) error {
	tx, err := p.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	now := time.Now().UnixMilli()

	for _, g := range mf.Groups {
		if _, err := tx.Exec(`INSERT INTO node_groups(name, description, created_at) VALUES(?,?,?)
			ON DUPLICATE KEY UPDATE description=VALUES(description)`,
			g.Name, g.Description, g.CreatedAt); err != nil {
			return fmt.Errorf("写入分组 %s 失败: %w", g.Name, err)
		}
	}
	if err := deleteMissing(tx, "node_groups", "name", groupNames(mf.Groups)); err != nil {
		return err
	}

	for _, n := range mf.Nodes {
		labels, err := marshalOrNil(n.Labels)
		if err != nil {
			return err
		}
		hostInfo, err := marshalOrNil(n.HostInfo)
		if err != nil {
			return err
		}
		logSources, err := marshalOrNil(n.LogSources)
		if err != nil {
			return err
		}
		// last_seen 取较大值：多实例下别让一个刚启动、内存还空着的实例把心跳写回旧值。
		if _, err := tx.Exec(`INSERT INTO nodes(hostname, mode, display_name, ip, os, arch, group_name,
				labels, version, host_info, log_sources, status, last_seen, created_at, updated_at)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
			ON DUPLICATE KEY UPDATE mode=VALUES(mode), display_name=VALUES(display_name),
				ip=VALUES(ip), os=VALUES(os), arch=VALUES(arch), group_name=VALUES(group_name),
				labels=VALUES(labels), version=VALUES(version), host_info=VALUES(host_info),
				log_sources=VALUES(log_sources), status=VALUES(status),
				last_seen=GREATEST(last_seen, VALUES(last_seen)), updated_at=VALUES(updated_at)`,
			n.Hostname, n.Mode, n.DisplayName, n.IP, n.OS, n.Arch, n.Group,
			labels, n.Version, hostInfo, logSources, n.Status, n.LastSeen, n.CreatedAt, now); err != nil {
			return fmt.Errorf("写入节点 %s 失败: %w", n.Hostname, err)
		}
	}
	if err := deleteMissing(tx, "nodes", "hostname", nodeNames(mf.Nodes)); err != nil {
		return err
	}
	return tx.Commit()
}

// FlushHeartbeat 批量刷新 last_seen/status（单事务）。
//
// 用 upsert 而不是 UPDATE：首次注册的 SaveMeta 若因瞬时 DB 错误失败，内存里有节点、
// 库里没有行，之后 regChanged 一直为 false、不会重试 SaveMeta；只 UPDATE 的话
// 这个节点会永久缺席（影响 0 行且不报错）。upsert 先把行补出来，注册字段留默认值，
// 下一次注册变化时 SaveMeta 会补全。
func (p *mysqlPersist) FlushHeartbeat(states []heartbeat) error {
	if len(states) == 0 {
		return nil
	}
	tx, err := p.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	now := time.Now().UnixMilli()
	stmt := `INSERT INTO nodes(hostname, status, last_seen, created_at, updated_at)
		VALUES(?,?,?,?,?)` + sqlx.Upsert(true, "hostname",
		"last_seen=MAX(last_seen, excluded.last_seen), status=excluded.status, updated_at=excluded.updated_at")
	for _, s := range states {
		if _, err := tx.Exec(stmt, s.Hostname, s.Status, s.LastSeen, now, now); err != nil {
			return fmt.Errorf("刷新节点 %s 心跳失败: %w", s.Hostname, err)
		}
	}
	return tx.Commit()
}

func deleteMissing(tx *sql.Tx, table, column string, keep []string) error {
	if len(keep) == 0 {
		_, err := tx.Exec("DELETE FROM " + table)
		return err
	}
	ph := strings.TrimSuffix(strings.Repeat("?,", len(keep)), ",")
	args := make([]any, len(keep))
	for i, v := range keep {
		args[i] = v
	}
	_, err := tx.Exec("DELETE FROM "+table+" WHERE "+column+" NOT IN ("+ph+")", args...)
	return err
}

func nodeNames(nodes []model.Node) []string {
	out := make([]string, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, n.Hostname)
	}
	return out
}

func groupNames(groups []model.Group) []string {
	out := make([]string, 0, len(groups))
	for _, g := range groups {
		out = append(out, g.Name)
	}
	return out
}

// marshalOrNil 把可选结构编码成 JSON；零值返回 NULL（列可空，读回时保持零值）。
func marshalOrNil(v any) ([]byte, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	if string(data) == "null" || string(data) == "{}" || string(data) == "[]" {
		return nil, nil
	}
	return data, nil
}

// ImportMeta 把一份 meta 快照整份写入持久化层。
//
// 仅供一次性迁移工具使用（把既有 nodes.json 导入状态库）；运行期请走 Manager，
// 它负责维护内存与持久化层的一致性。
func ImportMeta(p Persist, nodes []model.Node, groups []model.Group) error {
	return p.SaveMeta(metaFile{Nodes: nodes, Groups: groups})
}

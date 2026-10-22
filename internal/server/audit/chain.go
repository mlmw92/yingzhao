package audit

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// 链式 HMAC：让"改过 / 删过审计"变成**可检测**。
//
// 设计件：`docs/superpowers/specs/2026-10-08-audit-tamper-evident-design.md`。
//
// **先说清它不是什么**（设计件 §2.2 要求逐字遵守）：
//   - 它**不是"不可删除"**。库文件仍在该机、同一权限面下，root 依然能删。
//     本机制给的是**可检测**，不是**不可为**——所以命名一律用「防篡改（可检测）」，
//     不用「不可篡改」。
//   - 它**没有补齐**全景表 §3.4 第 4 条的前置条件（那要的是"不可篡改"）。
//     真正的"不可删除"需要只追加的外部存储（异地 syslog / 对象存储对象锁）。
//   - 拿到该机 root 的人**能改库 + 能读密钥 + 能清日志**，本机制**防不住**他。
//
// 它防的是"只攻破应用/数据库这一层"：库被复制走改一份、通过 SQL 改一行、删掉中间几行。
// 这些在今天**一个都看不出来**——本机制让它们留下可验证的证据。

// ChainKey 是链的 HMAC 密钥。
type ChainKey struct {
	key []byte
	fp  string // 指纹（密钥的 sha256 前 8 位十六进制）：用来**上报**，绝不外泄密钥本身
}

// Fingerprint 返回密钥指纹。校验报告里会带上它——
// 校验失败时要能区分"某条被改了"（安全事故）与"换了密钥"（运维动作），
// **把后者报成前者是假警报**（设计件 §4.2）。
func (k *ChainKey) Fingerprint() string {
	if k == nil {
		return ""
	}
	return k.fp
}

func newChainKey(key []byte, _ string) *ChainKey {
	sum := sha256.Sum256(key)
	return &ChainKey{key: key, fp: hex.EncodeToString(sum[:])[:8]}
}

// LoadOrCreateChainKey 读取密钥文件；不存在则**生成一个**（0600）。
//
// 刻意**不放进 server.yaml**：那个文件常被复制、备份、贴进工单，而密钥一旦随配置扩散，
// "没有密钥就无法伪造"这条保证就没了。也**绝不通过任何接口返回**（接口只返回指纹）。
//
// **密钥丢失 = 历史不可验**：这是真陷阱，不是可以含糊过去的事。所以每一行都记下
// 自己由哪个密钥签的（`key_fp`），校验时对不上的那些会报成"换了密钥"而不是"被篡改"。
func LoadOrCreateChainKey(path string) (*ChainKey, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("审计链密钥文件路径为空")
	}
	data, err := os.ReadFile(path)
	if err == nil {
		key := bytes.TrimSpace(data)
		// 十六进制文本形态（便于运维识别、备份、比对），也接受原始二进制
		if raw, decErr := hex.DecodeString(string(key)); decErr == nil && len(raw) >= 16 {
			return newChainKey(raw, path), nil
		}
		if len(key) >= 16 {
			return newChainKey(key, path), nil
		}
		return nil, fmt.Errorf("审计链密钥文件 %s 内容过短（%d 字节），拒绝使用", path, len(key))
	}
	if !os.IsNotExist(err) {
		return nil, fmt.Errorf("读取审计链密钥失败: %w", err)
	}

	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("生成审计链密钥失败: %w", err)
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("创建审计链密钥目录失败: %w", err)
		}
	}
	// 存成十六进制文本：运维一眼能看出这是个密钥文件，也能直接比对/备份
	if err := os.WriteFile(path, []byte(hex.EncodeToString(key)+"\n"), 0o600); err != nil {
		return nil, fmt.Errorf("写入审计链密钥失败: %w", err)
	}
	return newChainKey(key, path), nil
}

// chainPayload 是参与签名的**全部业务列** + seq + prev_hash。
//
// **必须覆盖全部业务列**：少覆盖一列，那一列就成了"可以改而不被发现"的字段
// （设计件 §4.1）。用结构体而不是 map：`json.Marshal` 对结构体按字段顺序输出，
// 是确定性的；map 的键序虽然 Go 也会排序，但结构体更不容易在后续加字段时漏改。
type chainPayload struct {
	Seq       int64  `json:"seq"`
	Prev      string `json:"prev"`
	TimeMs    int64  `json:"timeMs"`
	User      string `json:"user"`
	Method    string `json:"method"`
	Path      string `json:"path"`
	Status    int    `json:"status"`
	RemoteIP  string `json:"remoteIP"`
	SourceLoc string `json:"sourceLoc"`
	Succeeded bool   `json:"succeeded"`
	Category  string `json:"category"`
	Action    string `json:"action"`
	Detail    string `json:"detail"`
	RequestID string `json:"requestID"`
}

func chainDigest(key []byte, seq int64, prev string, e Event) string {
	payload, err := json.Marshal(chainPayload{
		Seq: seq, Prev: prev, TimeMs: e.Time.UnixMilli(), User: e.User, Method: e.Method,
		Path: e.Path, Status: e.Status, RemoteIP: e.RemoteIP, SourceLoc: e.SourceLocation,
		Succeeded: e.Succeeded, Category: e.Category, Action: e.Action, Detail: e.Detail,
		RequestID: e.RequestID,
	})
	if err != nil {
		// 结构体全是 string/int/bool，Marshal 不会失败；真失败了也不该把审计写坏
		payload = []byte(fmt.Sprintf("%d|%s|%d", seq, prev, e.Time.UnixMilli()))
	}
	mac := hmac.New(sha256.New, key)
	mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}

const (
	selectChainHeadSQL = `SELECT seq, hash, key_fingerprint FROM audit_chain WHERE id = 1`
	insertChainedSQL   = `INSERT INTO audit_events(
		time_ms, user_name, method, path, status, remote_ip, source_loc, succeeded,
		category, action, detail, request_id, seq, prev_hash, hash, key_fp
	) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`
)

// appendChained 追加一条带链的审计：**读链头 → 算 HMAC → 插行 → 更新链头，全在一个事务里**。
//
// 为什么要事务：链的成立前提是"seq 连续、prev_hash 指向上一条"。分两步写的话，
// 中途失败会留下"行插进去了但链头没动"——下一条就会用同一个 seq，链当场错位。
// 代价是每次审计多一次读 + 一次 HMAC（微秒级），而审计写入是"人工管理操作"级的量
// （设计件 D7 已接受这笔开销）。
func (s *Store) appendChained(key *ChainKey, e Event) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("开启审计写入事务失败: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var headSeq int64
	var headHash, headFP string
	err = tx.QueryRow(s.selectChainHeadSQL()).Scan(&headSeq, &headHash, &headFP)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("读取审计链头失败: %w", err)
	}

	seq := headSeq + 1
	hash := chainDigest(key.key, seq, headHash, e)
	if _, err := tx.Exec(insertChainedSQL,
		append(eventArgs(e), seq, headHash, hash, key.fp)...); err != nil {
		return fmt.Errorf("写入审计事件失败: %w", err)
	}
	if _, err := tx.Exec(s.upsertChainHeadSQL(), seq, hash, key.fp, time.Now().UnixMilli()); err != nil {
		return fmt.Errorf("更新审计链头失败: %w", err)
	}
	return tx.Commit()
}

// ChainHead 返回当前链头（供日志锚点与校验使用）。未启用链时 seq=0、hash 为空。
func (s *Store) ChainHead() (seq int64, hash, keyFP string, ok bool) {
	s.mu.RLock()
	db := s.db
	s.mu.RUnlock()
	if db == nil {
		return 0, "", "", false
	}
	if err := db.QueryRow(selectChainHeadSQL).Scan(&seq, &hash, &keyFP); err != nil {
		return 0, "", "", false
	}
	return seq, hash, keyFP, true
}

// selectChainHeadSQL 读取链头；MySQL 多实例下必须 FOR UPDATE 锁住这一行，
// 否则两个实例会读到同一个 seq 并写出分叉的链。
func (s *Store) selectChainHeadSQL() string {
	if s.d.Name() != "sqlite" {
		return selectChainHeadSQL + " FOR UPDATE"
	}
	return selectChainHeadSQL
}

func (s *Store) upsertChainHeadSQL() string {
	return `INSERT INTO audit_chain(id, seq, hash, key_fingerprint, updated_ms) VALUES(1, ?, ?, ?, ?)` +
		s.d.Upsert("id",
			"seq=excluded.seq, hash=excluded.hash, key_fingerprint=excluded.key_fingerprint, updated_ms=excluded.updated_ms")
}

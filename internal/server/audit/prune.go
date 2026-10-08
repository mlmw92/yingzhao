package audit

import (
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

// 保留策略裁剪与链的协调（设计件 `2026-10-08-audit-tamper-evident-design.md` §4.3）。
//
// **这是整套机制最容易做错的地方**：链与裁剪天然冲突——裁剪删掉最老的若干条，链在裁剪点必然"断"。
// 如果不记录"哪一段是被合法裁掉的"，校验就会把**正常的保留清理报成"有人删过审计"**，
// 那是假警报，而且是最坏的一种：它会训练人忽略真正的告警。
//
// 做法：裁剪**先记后删、同一事务**——把被删的 seq 区间与**该区间末条的 hash** 记进 audit_pruned。
// 校验时，跨越缺口的那一行，其 prev_hash 必须能对上某条裁剪记录里的 head_hash。
//
// **裁剪记录自己也要带 HMAC**：否则拿到库写权限的人可以**伪造一条裁剪记录**来遮盖自己的删除。
// 有了 HMAC，他伪造不出来（没有密钥），只能删——而删掉记录会让缺口"无法归因"，照样被检出。

const insertPrunedSQL = `INSERT INTO audit_pruned(from_seq, to_seq, head_hash, rows, pruned_ms, mac)
	VALUES(?,?,?,?,?,?)`

// prunedMAC 覆盖裁剪记录的**全部字段**（与链上一条同理：少覆盖一列那一列就可改而不被发现）。
func prunedMAC(key []byte, fromSeq, toSeq int64, headHash string, rows int, prunedMs int64) string {
	payload := fmt.Sprintf("%d|%d|%s|%d|%d", fromSeq, toSeq, headHash, rows, prunedMs)
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}

// pruneWhere 删除满足 where 的审计行，并在同一个事务里记下被删的链区间。
//
// 只统计 **seq > 0 的行**：那才是链上的行。seq=0 是"启用链之前的历史"（或没密钥时写的），
// 它们本来就不参与校验，删掉不构成链上的事件。
//
// 链未启用（没有库或没有密钥）时退化为原来的直接删除——**不假装**记了链区间。
func (s *Store) pruneWhere(where string, args ...any) (int, error) {
	s.mu.RLock()
	db, key := s.db, s.chainKey
	s.mu.RUnlock()
	if db == nil {
		// 降级模式不做任何事：它的保留口径本来就是"最近 MaxEvents 条"，没有时间语义。
		return 0, nil
	}
	if key == nil {
		res, err := db.Exec("DELETE FROM audit_events WHERE "+where, args...)
		if err != nil {
			return 0, fmt.Errorf("清理审计事件失败: %w", err)
		}
		n, _ := res.RowsAffected()
		return int(n), nil
	}

	tx, err := db.Begin()
	if err != nil {
		return 0, fmt.Errorf("开启审计裁剪事务失败: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// 先算出被删的链区间与**末条的 hash**。末条 hash 是校验跨越缺口那一行的锚点：
	// 幸存行里最老的那条，它的 prev_hash 正指向被删区间的最后一条。
	var fromSeq, toSeq sql.NullInt64
	var headHash sql.NullString
	var rows int
	if err := tx.QueryRow(
		`SELECT MIN(seq), MAX(seq), COUNT(*) FROM audit_events WHERE `+where+` AND seq > 0`,
		args...).Scan(&fromSeq, &toSeq, &rows); err != nil {
		return 0, fmt.Errorf("统计待裁剪的链区间失败: %w", err)
	}
	if rows > 0 && toSeq.Valid {
		if err := tx.QueryRow(`SELECT hash FROM audit_events WHERE seq = ?`, toSeq.Int64).
			Scan(&headHash); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return 0, fmt.Errorf("读取待裁剪区间的末条 hash 失败: %w", err)
		}
	}

	res, err := tx.Exec("DELETE FROM audit_events WHERE "+where, args...)
	if err != nil {
		return 0, fmt.Errorf("清理审计事件失败: %w", err)
	}
	deleted, _ := res.RowsAffected()

	if rows > 0 && toSeq.Valid {
		now := time.Now().UnixMilli()
		if _, err := tx.Exec(insertPrunedSQL, fromSeq.Int64, toSeq.Int64, headHash.String, rows, now,
			prunedMAC(key.key, fromSeq.Int64, toSeq.Int64, headHash.String, rows, now)); err != nil {
			return 0, fmt.Errorf("记录裁剪区间失败: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("提交审计裁剪事务失败: %w", err)
	}
	return int(deleted), nil
}

// PrunedSegments 返回所有**被合法裁剪**的链区间（供校验归因缺口）。
type PrunedSegment struct {
	FromSeq  int64  `json:"from"`
	ToSeq    int64  `json:"to"`
	HeadHash string `json:"headHash"`
	Rows     int    `json:"rows"`
	PrunedMs int64  `json:"prunedMs"`
	// MACValid 表示这条裁剪记录本身签名有效。**false 意味着有人动过它**——
	// 比如伪造一条来遮盖自己的删除（没有密钥伪造不出来，但可以删掉真的、或改字段）。
	MACValid bool `json:"macValid"`
}

// PrunedSegments 返回裁剪区间（按 from_seq 升序）。降级模式返回空。
func (s *Store) PrunedSegments() []PrunedSegment {
	s.mu.RLock()
	db, key := s.db, s.chainKey
	s.mu.RUnlock()
	if db == nil {
		return nil
	}
	rows, err := db.Query(`SELECT from_seq, to_seq, head_hash, rows, pruned_ms, mac
		FROM audit_pruned ORDER BY from_seq`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []PrunedSegment
	for rows.Next() {
		var seg PrunedSegment
		var mac string
		if err := rows.Scan(&seg.FromSeq, &seg.ToSeq, &seg.HeadHash, &seg.Rows, &seg.PrunedMs, &mac); err != nil {
			return out
		}
		if key != nil && mac != "" {
			seg.MACValid = hmac.Equal([]byte(mac),
				[]byte(prunedMAC(key.key, seg.FromSeq, seg.ToSeq, seg.HeadHash, seg.Rows, seg.PrunedMs)))
		}
		out = append(out, seg)
	}
	return out
}

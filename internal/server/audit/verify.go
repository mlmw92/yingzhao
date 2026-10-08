package audit

import (
	"fmt"
	"time"
)

// 审计完整性校验（设计件 §4.5）。
//
// **报告口径是本文件最要紧的东西**。两处最容易做错：
//
//  1. **不能只说"通过"**。历史已经被保留策略裁剪过，所以"全部通过"在这套机制下
//     **永远不成立**。报告必须报出**覆盖区间与条数**（"已验证第 1201..48213 条"），
//     否则看的人会以为整段历史都被验过。
//  2. **不能把"换了密钥"报成"被篡改"**。前者是运维动作（恢复备份、换机器），
//     后者是安全事故。混为一谈会造成假警报——而假警报会训练人忽略真警报。
//     所以按每行记的 `key_fp` 分开统计，密钥对不上的行**跳过校验并单独计数**。

// MaxVerifyRows 是单次校验扫描的行数上限（超过则只验**最新的**这么多行，
// 并在报告里置 truncated）。一次 50 万行的 HMAC 重算要秒级，按需接口可以接受，
// 但没必要——尾部才是"最近有没有人动过"最要紧的部分。
const MaxVerifyRows = 200000

// 断裂原因。**分开报**：只看"失败"两个字，没法判断该去查什么。
const (
	ReasonHashMismatch = "hash-mismatch"       // 这一行被改过
	ReasonPrevMismatch = "prev-hash-mismatch"  // 这一行的 prev_hash 与上一条对不上（链被搬移/插入）
	ReasonGap          = "unattributable-gap"  // 缺口不在任何"合法裁剪"记录里——有人删了它
	ReasonPrunedMAC    = "pruned-record-invalid" // 裁剪记录本身签名无效：有人动了裁剪记录
)

// VerifyBreak 是一条断裂。
type VerifyBreak struct {
	Seq    int64  `json:"seq"`
	Reason string `json:"reason"`
	Detail string `json:"detail,omitempty"`
}

// VerifyReport 是校验结果。
type VerifyReport struct {
	// Enabled 表示**链是否在起作用**。false 时下面的字段基本没意义，
	// 且 Note 里会写明原因——**绝不把"没启用"显示成"通过"**。
	Enabled bool   `json:"enabled"`
	Mode    string `json:"mode"` // sqlite | json
	// KeyFingerprint 是当前密钥的指纹（用于区分"被改"与"换了密钥"）。绝不返回密钥本身。
	KeyFingerprint string `json:"keyFingerprint,omitempty"`
	// VerifiedFrom/To 是**本次真正验过的 seq 区间**。RowsChecked 是条数。
	// 报这三个字段是硬要求：只说"通过"等于骗人（历史已被裁剪，永远验不全）。
	VerifiedFrom int64 `json:"verifiedFrom"`
	VerifiedTo   int64 `json:"verifiedTo"`
	RowsChecked  int   `json:"rowsChecked"`
	// UnchainedRows 是 seq=0 的行数（启用链之前写入的历史）。它们**不可验**，单独计数。
	UnchainedRows int `json:"unchainedRows"`
	// KeyMismatchRows 是用**别的密钥**签的行数（换了密钥）。同样不可验，单独计数。
	KeyMismatchRows int `json:"keyMismatchRows"`
	// LinkUnverifiedRows 是跨越裁剪区间的行：它们的 prev_hash 指向被裁掉的行，
	// 而那条裁剪记录只记了区间末条的 hash，因此**这一处的续接无法验证**。
	// 如实计数，不混进"已验证"。
	LinkUnverifiedRows int             `json:"linkUnverifiedRows"`
	PrunedSegments     []PrunedSegment `json:"prunedSegments"`
	Truncated          bool            `json:"truncated"`
	OK                 bool            `json:"ok"`
	Breaks             []VerifyBreak   `json:"breaks"`
	Note               string          `json:"note,omitempty"`
	CheckedAt          int64           `json:"checkedAt"`
}

// Verify 校验审计链。
func (s *Store) Verify() VerifyReport {
	rep := VerifyReport{CheckedAt: time.Now().UnixMilli(), Breaks: []VerifyBreak{}, Mode: "json"}
	s.mu.RLock()
	db, key := s.db, s.chainKey
	s.mu.RUnlock()

	// 指纹放在最前面：只要密钥加载了就返回（降级模式也一样）。它不是秘密，
	// 而且运维要靠它确认"服务器手上是哪把密钥"——换过密钥却没对上时，
	// 这是唯一能看出来的地方。放在后面的分支里会漏掉降级模式那一支。
	if key != nil {
		rep.KeyFingerprint = key.Fingerprint()
	}

	if db == nil {
		rep.Note = "当前为**降级模式**（台账库不可用，审计落在 JSON 文件里）：**无完整性保护**。" +
			"降级模式的写入是整文件重写，与链式追加语义不兼容，因此不启用链。"
		return rep
	}
	rep.Mode = "sqlite"
	if key == nil {
		rep.Note = "审计链**未启用**（密钥不可用）：审计仍在记录，但**没有完整性保护**。"
		return rep
	}
	rep.Enabled = true

	segs := s.PrunedSegments()
	rep.PrunedSegments = segs
	for _, seg := range segs {
		// 裁剪记录自己也要能被验：伪造一条来遮盖删除是**唯一**绕过缺口的办法，
		// 没有密钥伪造不出 MAC（改了字段也会让 MAC 失效）。
		if !seg.MACValid {
			rep.Breaks = append(rep.Breaks, VerifyBreak{Seq: seg.ToSeq, Reason: ReasonPrunedMAC,
				Detail: fmt.Sprintf("裁剪记录 [%d,%d] 的签名无效——这条记录被动过", seg.FromSeq, seg.ToSeq)})
		}
	}

	if err := db.QueryRow(`SELECT COUNT(*) FROM audit_events WHERE seq = 0`).Scan(&rep.UnchainedRows); err != nil {
		rep.Note = "读取审计行数失败：" + err.Error()
		return rep
	}

	rows, err := db.Query(`SELECT seq, prev_hash, hash, key_fp,
		time_ms, user_name, method, path, status, remote_ip, source_loc, succeeded,
		category, action, detail, request_id
		FROM audit_events WHERE seq > 0 ORDER BY seq`)
	if err != nil {
		rep.Note = "读取审计链失败：" + err.Error()
		return rep
	}
	defer rows.Close()

	type chainedRow struct {
		seq      int64
		prevHash string
		hash     string
		keyFP    string
		event    Event
	}
	var all []chainedRow
	for rows.Next() {
		var r chainedRow
		var timeMs int64
		var succeeded int
		if err := rows.Scan(&r.seq, &r.prevHash, &r.hash, &r.keyFP,
			&timeMs, &r.event.User, &r.event.Method, &r.event.Path,
			&r.event.Status, &r.event.RemoteIP, &r.event.SourceLocation, &succeeded,
			&r.event.Category, &r.event.Action, &r.event.Detail, &r.event.RequestID); err != nil {
			rep.Note = "扫描审计行失败：" + err.Error()
			return rep
		}
		r.event.Time = time.UnixMilli(timeMs)
		r.event.Succeeded = succeeded != 0
		all = append(all, r)
	}
	if err := rows.Err(); err != nil {
		rep.Note = "读取审计链中断：" + err.Error()
		return rep
	}

	// 只验最新的 MaxVerifyRows 行，并**显式说明被截断**（悄悄少验是最坏的做法）
	if len(all) > MaxVerifyRows {
		all = all[len(all)-MaxVerifyRows:]
		rep.Truncated = true
	}
	if len(all) == 0 {
		rep.OK = len(rep.Breaks) == 0
		if rep.OK {
			rep.Note = "尚无链上记录（启用链之后还没有写过审计）。"
		}
		return rep
	}
	rep.VerifiedFrom, rep.VerifiedTo, rep.RowsChecked = all[0].seq, all[len(all)-1].seq, len(all)

	// 裁剪区间 → 末条 hash 的映射：跨越缺口的那一行，其 prev_hash 应当对得上它
	prunedHead := map[int64]string{}
	var prunedRanges [][2]int64
	for _, seg := range segs {
		if seg.MACValid {
			prunedHead[seg.ToSeq] = seg.HeadHash
			prunedRanges = append(prunedRanges, [2]int64{seg.FromSeq, seg.ToSeq})
		}
	}
	covered := func(from, to int64) bool {
		for _, r := range prunedRanges {
			if r[0] <= from && r[1] >= to {
				return true
			}
		}
		return false
	}

	var prevSeq int64
	prevHash := ""
	for i, r := range all {
		// 密钥对不上：这一行是用**别的密钥**签的（换了密钥），不可验。
		// **不能报成"被篡改"**——那是假警报。
		if r.keyFP != key.fp {
			rep.KeyMismatchRows++
			prevSeq, prevHash = r.seq, r.hash
			continue
		}
		if got := chainDigest(key.key, r.seq, r.prevHash, r.event); got != r.hash {
			rep.Breaks = append(rep.Breaks, VerifyBreak{Seq: r.seq, Reason: ReasonHashMismatch,
				Detail: fmt.Sprintf("这一行的内容与签名对不上（签名 %s…，实算 %s…）", short(r.hash), short(got))})
			prevSeq, prevHash = r.seq, r.hash
			continue
		}
		if i > 0 {
			switch {
			case r.seq == prevSeq+1:
				if r.prevHash != prevHash {
					rep.Breaks = append(rep.Breaks, VerifyBreak{Seq: r.seq, Reason: ReasonPrevMismatch,
						Detail: "prev_hash 与上一条的签名对不上"})
				}
			default:
				// 缺口：必须能被"合法裁剪"记录解释，否则就是有人删了
				if !covered(prevSeq+1, r.seq-1) {
					rep.Breaks = append(rep.Breaks, VerifyBreak{Seq: r.seq, Reason: ReasonGap,
						Detail: fmt.Sprintf("第 %d..%d 条不见了，且不在任何合法裁剪记录里", prevSeq+1, r.seq-1)})
				}
				if want, ok := prunedHead[r.seq-1]; ok {
					if r.prevHash != want {
						rep.Breaks = append(rep.Breaks, VerifyBreak{Seq: r.seq, Reason: ReasonPrevMismatch,
							Detail: "prev_hash 与裁剪记录里记的末条签名对不上"})
					}
				} else {
					// 裁剪记录只记了区间末条，这一处续接无从验证——**如实计数**，不混进"已验证"
					rep.LinkUnverifiedRows++
				}
			}
		} else if r.prevHash != "" {
			// 本次校验的**第一行**：prev_hash 要么是链头（空），要么指向被裁掉的区间末条
			if want, ok := prunedHead[r.seq-1]; !ok || r.prevHash != want {
				rep.Breaks = append(rep.Breaks, VerifyBreak{Seq: r.seq, Reason: ReasonPrevMismatch,
					Detail: "本次校验的首行 prev_hash 既不是链头、也对不上任何裁剪记录的末条签名"})
			}
		}
		prevSeq, prevHash = r.seq, r.hash
	}

	rep.OK = len(rep.Breaks) == 0
	if rep.OK {
		rep.Note = fmt.Sprintf("已验证第 %d..%d 条（共 %d 条），链自洽。", rep.VerifiedFrom, rep.VerifiedTo, rep.RowsChecked)
		if rep.Truncated {
			rep.Note += " **只验了最新的部分**（历史行数超过单次上限）。"
		}
		if rep.UnchainedRows > 0 {
			rep.Note += fmt.Sprintf(" 另有 %d 条是**启用链之前**写入的历史，不可验。", rep.UnchainedRows)
		}
		if rep.KeyMismatchRows > 0 {
			rep.Note += fmt.Sprintf(" 另有 %d 条由**别的密钥**签（换过密钥），不可验。", rep.KeyMismatchRows)
		}
		if rep.LinkUnverifiedRows > 0 {
			rep.Note += fmt.Sprintf(" 其中 %d 条跨越裁剪区间，该处续接无法验证。", rep.LinkUnverifiedRows)
		}
	}
	return rep
}

func short(s string) string {
	if len(s) <= 12 {
		return s
	}
	return s[:12]
}

package audit

import (
	"context"
	"log/slog"
	"time"
)

// 链头的**日志锚点**（设计件 §4.4）。这是**必需项，不是可选项**：
//
// 库内的链**无法**证明"尾部没被删"——尾部没有后继来指认它。清空整张表在库内更是
// 完全不可检测（连链头都没了）。所以必须把链头**周期性地写到一个库之外的权限面**上：
// 服务日志（journald）。攻击者只改库不改日志，校验/人工比对就能发现"库里的链头
// 比日志里记录的那条更短或不同"。
//
// **它仍然不是"不可删除"**：拿到该机 root 的人能连日志一起清。它抬高的是
// "只攻破应用/数据库这一层"的门槛（威胁模型场景 D，见设计件 §2.3）。
// 真正要防住 root，需要把日志送往**异地**——那是运维动作，本代码只负责把锚点吐出来。

// DefaultAnchorInterval 是锚点写入间隔。刻意取得很短：一条日志而已，成本可忽略，
// 而间隔越短，"尾部被删"能被缩小的窗口就越小。
const DefaultAnchorInterval = 5 * time.Minute

// RunAnchor 周期把链头写进服务日志，直到 ctx 结束。
func (s *Store) RunAnchor(ctx context.Context) {
	t := time.NewTicker(DefaultAnchorInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.AnchorOnce()
		}
	}
}

// AnchorOnce 写一次锚点。消息用**ASCII**（`audit chain head`）而不是中文：
// 它是给脚本与运维 grep 的稳定标记，不该随界面文案调整而变。
func (s *Store) AnchorOnce() {
	seq, hash, fp, ok := s.ChainHead()
	if !ok {
		return
	}
	slog.Info("audit chain head",
		"seq", seq, "hash", hash, "keyFingerprint", fp,
		"note", "链头锚点：库里的链头不应比它更短或不同（本行是审计防篡改的唯一外部依据）")
}

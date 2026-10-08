package audit

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func testChainKey(t *testing.T) *ChainKey {
	t.Helper()
	k, err := LoadOrCreateChainKey(filepath.Join(t.TempDir(), "audit.key"))
	if err != nil {
		t.Fatalf("生成密钥失败: %v", err)
	}
	return k
}

// 密钥文件必须 0600：它是"没有它就伪造不了"的那把钥匙。
func TestChainKey_CreatedWith0600(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.key")
	k, err := LoadOrCreateChainKey(path)
	if err != nil {
		t.Fatalf("生成密钥失败: %v", err)
	}
	if k.Fingerprint() == "" {
		t.Fatal("指纹不该为空（校验报告要用它区分「被改」与「换了密钥」）")
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatalf("密钥文件应存在: %v", err)
	}
	// 权限位只在 POSIX 上成立：Windows 的 os.WriteFile 不套用 0600（实测报 666）。
	// 因此这里在 Windows 上跳过，真机核对脚本会**真的**去核 `stat -c %a`。
	if runtime.GOOS == "windows" {
		t.Skip("Windows 不套用 POSIX 权限位，权限核验交给实机核对")
	}
	if got := st.Mode().Perm(); got != 0o600 {
		t.Fatalf("密钥文件权限应为 0600，实际 %o", got)
	}
}

// 重启必须**复用**同一把密钥：换了密钥，历史全部变成"无法验证"（真陷阱）。
func TestChainKey_ReusedOnRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.key")
	k1, err := LoadOrCreateChainKey(path)
	if err != nil {
		t.Fatalf("生成失败: %v", err)
	}
	k2, err := LoadOrCreateChainKey(path)
	if err != nil {
		t.Fatalf("重读失败: %v", err)
	}
	if k1.Fingerprint() != k2.Fingerprint() {
		t.Fatalf("重启应复用同一把密钥，指纹 %s != %s", k1.Fingerprint(), k2.Fingerprint())
	}
}

// 内容坏掉的密钥文件要**拒绝使用**，而不是拿半截字节当密钥（那会静默地让历史全部不可验）。
func TestChainKey_ShortFileRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.key")
	if err := os.WriteFile(path, []byte("abc\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOrCreateChainKey(path); err == nil {
		t.Fatal("过短的密钥文件应被拒绝")
	}
}

// 链要连起来：seq 递增、prev_hash 指向上一条、链头跟着走。
func TestChain_SeqAndPrevLink(t *testing.T) {
	store := New(filepath.Join(t.TempDir(), "audit_events.json"))
	store.UseSQLite(openTestDB(t).DB())
	key := testChainKey(t)
	store.SetChainKey(key)

	for i := 0; i < 3; i++ {
		if err := store.Record(Event{
			Time: time.UnixMilli(1_700_000_000_000 + int64(i)), User: "ops",
			Method: "POST", Path: "/api/v1/assets", Status: 200, Succeeded: true,
		}); err != nil {
			t.Fatalf("写入第 %d 条失败: %v", i+1, err)
		}
	}
	rows := chainRows(t, store)
	if len(rows) != 3 {
		t.Fatalf("应有 3 行，实际 %d", len(rows))
	}
	for i, r := range rows {
		if r.seq != int64(i+1) {
			t.Fatalf("第 %d 行 seq 应为 %d，实际 %d", i+1, i+1, r.seq)
		}
		wantPrev := ""
		if i > 0 {
			wantPrev = rows[i-1].hash
		}
		if r.prevHash != wantPrev {
			t.Fatalf("第 %d 行 prev_hash 应指向上一条，实际 %q", i+1, r.prevHash)
		}
		if r.hash == "" || r.keyFP != key.Fingerprint() {
			t.Fatalf("第 %d 行 hash/key_fp 不对：%+v", i+1, r)
		}
	}
	seq, hash, fp, ok := store.ChainHead()
	if !ok || seq != 3 || hash != rows[2].hash || fp != key.Fingerprint() {
		t.Fatalf("链头应为 (3, 最后一条的 hash)，实际 (%d, %s, %s, %v)", seq, hash, fp, ok)
	}
}

// **决定性**：签名必须覆盖**全部业务列**——少覆盖一列，那一列就成了"可以改而不被发现"的字段。
// 这条用例逐个字段改一遍，任何一个漏掉都会当场红。
func TestChain_DigestCoversEveryColumn(t *testing.T) {
	key := testChainKey(t)
	base := Event{
		Time: time.UnixMilli(1_700_000_000_000), User: "ops", Method: "POST",
		Path: "/api/v1/assets/1", Status: 200, RemoteIP: "10.0.0.1",
		SourceLocation: "中国 上海", Succeeded: true, Category: "asset",
		Action: "update", Detail: "name: a -> b", RequestID: "req-1",
	}
	baseDigest := chainDigest(key.key, 7, "prev-hash", base)

	mutations := map[string]Event{
		"Time":           func() Event { e := base; e.Time = base.Time.Add(time.Millisecond); return e }(),
		"User":           func() Event { e := base; e.User = "other"; return e }(),
		"Method":         func() Event { e := base; e.Method = "DELETE"; return e }(),
		"Path":           func() Event { e := base; e.Path = "/api/v1/assets/2"; return e }(),
		"Status":         func() Event { e := base; e.Status = 403; return e }(),
		"RemoteIP":       func() Event { e := base; e.RemoteIP = "10.0.0.2"; return e }(),
		"SourceLocation": func() Event { e := base; e.SourceLocation = "中国 北京"; return e }(),
		"Succeeded":      func() Event { e := base; e.Succeeded = false; return e }(),
		"Category":       func() Event { e := base; e.Category = "ops"; return e }(),
		"Action":         func() Event { e := base; e.Action = "delete"; return e }(),
		"Detail":         func() Event { e := base; e.Detail = "name: a -> c"; return e }(),
		"RequestID":      func() Event { e := base; e.RequestID = "req-2"; return e }(),
	}
	for name, e := range mutations {
		if chainDigest(key.key, 7, "prev-hash", e) == baseDigest {
			t.Fatalf("改了 %s 之后签名没变——这一列就成了可以改而不被发现的字段", name)
		}
	}
	// seq 与 prev_hash 也必须在签名里（否则可以整段搬移）
	if chainDigest(key.key, 8, "prev-hash", base) == baseDigest {
		t.Fatal("seq 必须参与签名")
	}
	if chainDigest(key.key, 7, "other-prev", base) == baseDigest {
		t.Fatal("prev_hash 必须参与签名")
	}
	// 换了密钥必须算不出同一个签名（这条是"没有密钥就无法伪造"的直接体现）
	other := testChainKey(t)
	if chainDigest(other.key, 7, "prev-hash", base) == baseDigest {
		t.Fatal("不同密钥不该算出同一签名")
	}
}

// 没有密钥时不阻断业务，但**不假装**这些行是完整的：seq=0、hash 为空，校验会如实报"无链"。
func TestChain_NoKeyFallsBackToPlainInsert(t *testing.T) {
	store := New(filepath.Join(t.TempDir(), "audit_events.json"))
	store.UseSQLite(openTestDB(t).DB())
	if store.ChainEnabled() {
		t.Fatal("未设置密钥时不该报告链已启用")
	}
	if err := store.Record(Event{Time: time.UnixMilli(1), User: "ops", Succeeded: true}); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	rows := chainRows(t, store)
	if len(rows) != 1 || rows[0].seq != 0 || rows[0].hash != "" {
		t.Fatalf("无密钥时应写入不带链的行（seq=0、hash 空），实际 %+v", rows)
	}
}

// 降级模式（无库）下链不启用：它的写入是整文件重写，与链式追加语义不兼容（设计件 §4.7）。
func TestChain_DisabledInJSONFallback(t *testing.T) {
	store := New(filepath.Join(t.TempDir(), "audit_events.json"))
	store.SetChainKey(testChainKey(t))
	if store.ChainEnabled() {
		t.Fatal("降级模式（无库）下不该报告链已启用")
	}
}

type chainRow struct {
	seq      int64
	prevHash string
	hash     string
	keyFP    string
}

func chainRows(t *testing.T, s *Store) []chainRow {
	t.Helper()
	rows, err := s.db.Query(`SELECT seq, prev_hash, hash, key_fp FROM audit_events ORDER BY id`)
	if err != nil {
		t.Fatalf("查询链行失败: %v", err)
	}
	defer rows.Close()
	var out []chainRow
	for rows.Next() {
		var r chainRow
		if err := rows.Scan(&r.seq, &r.prevHash, &r.hash, &r.keyFP); err != nil {
			t.Fatalf("扫描失败: %v", err)
		}
		out = append(out, r)
	}
	return out
}

// 密钥文件内容不该以任何形式出现在指纹里（指纹是单向的哈希，不是密钥本身）。
func TestChainKey_FingerprintDoesNotLeakKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.key")
	k, err := LoadOrCreateChainKey(path)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(k.Fingerprint(), strings.TrimSpace(string(raw))[:8]) {
		t.Fatal("指纹里不该出现密钥内容")
	}
}

package alert

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/mlmw92/yingzhao/internal/model"
	"github.com/mlmw92/yingzhao/internal/server/configdoc"
)

// TestLoadRulesCanonical 校验规则文件采用顶层序列格式（与持久化格式一致）。
func TestLoadRulesCanonical(t *testing.T) {
	data := []byte(`
- name: "CPU 高使用率"
  metric: cpu_usage
  operator: ">"
  threshold: 85
  for: "1m"
  severity: warning
  enabled: true
`)
	var list []model.AlertRule
	if err := yaml.Unmarshal(data, &list); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("want 1 rule, got %d", len(list))
	}
	r := list[0]
	if r.Metric != "cpu_usage" || r.Operator != ">" || r.Threshold != 85 ||
		r.For != "1m" || r.Severity != model.Severity("warning") || !r.Enabled {
		t.Fatalf("unexpected rule: %+v", r)
	}
}

func TestParseHHMM(t *testing.T) {
	tests := map[string]int{
		"00:00": 0,
		"02:30": 150,
		"23:59": 1439,
		"2:30":  -1,
		"24:00": -1,
		"12:60": -1,
		"12:xx": -1,
	}
	for input, want := range tests {
		if got := parseHHMM(input); got != want {
			t.Errorf("parseHHMM(%q) = %d, want %d", input, got, want)
		}
	}
}

func TestInQuietPeriodCrossDay(t *testing.T) {
	// 2026-08-10 23:00（周一）应命中周一 22:00-06:00。
	now := time.Date(2026, time.August, 10, 23, 0, 0, 0, time.Local).UnixMilli()
	if !inQuietPeriod([]model.QuietPeriod{{Days: []int{1}, Start: "22:00", End: "06:00"}}, now) {
		t.Fatal("expected cross-day quiet period to match")
	}
}

func TestValidateRuleRejectsInvalidInput(t *testing.T) {
	base := model.AlertRule{Name: "CPU", Metric: "cpu_usage", Operator: ">", Severity: model.SeverityWarning, For: "5m"}
	if err := ValidateRule(base); err != nil {
		t.Fatalf("valid rule rejected: %v", err)
	}
	bad := base
	bad.For = "not-a-duration"
	if err := ValidateRule(bad); err == nil {
		t.Fatal("invalid duration accepted")
	}
	bad = base
	bad.QuietPeriods = []model.QuietPeriod{{Start: "02:00", End: "06:00"}}
	if err := ValidateRule(bad); err != nil {
		t.Fatalf("valid quiet period rejected: %v", err)
	}
}

// 规则存储经持久化接口读写：换成数据库后端后行为不变。
func TestRulesStorePersisterRoundTrip(t *testing.T) {
	dir := t.TempDir()
	p := configdoc.NewFilePersister(filepath.Join(dir, "rules.yaml"))
	s := NewRulesStoreWithPersister(p, nil)
	r := s.Create(model.AlertRule{Name: "cpu", Metric: "cpu_usage", Operator: ">", Threshold: 90})

	reloaded := NewRulesStoreWithPersister(configdoc.NewFilePersister(filepath.Join(dir, "rules.yaml")), nil)
	got, ok := reloaded.Get(r.ID)
	if !ok || got.Name != "cpu" || got.Threshold != 90 {
		t.Fatalf("重新加载 = %+v, %v", got, ok)
	}
}

type failingPersister struct {
	loadErr error
	saved   bool
}

func (f *failingPersister) Load() ([]byte, error) { return nil, f.loadErr }
func (f *failingPersister) Save([]byte) error     { f.saved = true; return nil }
func (f *failingPersister) Name() string          { return "db:test" }

// 读库失败不等于空库：内存里是空的，下一次保存会用空集合覆盖原文档。
// 存储必须记住读失败并拒绝写入，调用方（main）据此拒绝启动。
func TestRulesLoadFailureBlocksWrites(t *testing.T) {
	p := &failingPersister{loadErr: errors.New("db down")}
	s := NewRulesStoreWithPersister(p, nil)
	if s.LoadError() == nil {
		t.Fatal("读失败应被记录到 LoadError")
	}
	if len(s.List()) != 0 {
		t.Fatalf("读失败时内存应为空，实际 %d 条", len(s.List()))
	}
	// 播种与用户写入都不能落盘。
	seedName := seedTemplates()[0].Name
	for _, r := range s.List() {
		if r.Name == seedName {
			t.Fatalf("读失败时不应播种内置规则")
		}
	}
	s.Create(model.AlertRule{Name: "user-rule"})
	if p.saved {
		t.Fatal("读失败时写入必须被拒绝，否则会覆盖库里的原文档")
	}
}

// 用户删掉的内置规则不能在重启后复活：播种记录与规则文档分开存，
// 判断「全新安装」看的是规则文档是否存在，而不是某个本地标记文件。
func TestRulesSeedMarkerSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	doc := configdoc.NewFilePersister(filepath.Join(dir, "rules.yaml"))
	marker := configdoc.NewFilePersister(filepath.Join(dir, "rules.seeded"))

	s := NewRulesStoreWithPersister(doc, marker)
	seededName := seedTemplates()[0].Name
	var id string
	for _, r := range s.List() {
		if r.Name == seededName {
			id = r.ID
		}
	}
	if id == "" {
		t.Fatalf("内置规则 %q 未被播种", seededName)
	}
	if err := s.Delete(id); err != nil {
		t.Fatalf("删除内置规则失败: %v", err)
	}

	reopened := NewRulesStoreWithPersister(doc, marker)
	for _, r := range reopened.List() {
		if r.Name == seededName {
			t.Fatalf("删除的内置规则 %q 在重启后复活了", seededName)
		}
	}
}

// 播种一次只写一次文档：逐条 Create 会做 N 次整份序列化 + N 次数据库往返。
// 顺带钉住批量创建时的 ID 唯一性（同一毫秒内不能撞 ID）。
type countingPersister struct {
	data  []byte
	saves int
}

func (c *countingPersister) Load() ([]byte, error) {
	if c.data == nil {
		return nil, configdoc.ErrNotFound
	}
	return c.data, nil
}
func (c *countingPersister) Save(data []byte) error { c.data = data; c.saves++; return nil }
func (c *countingPersister) Name() string           { return "db:counting" }

func TestSeedDefaultsWritesOnce(t *testing.T) {
	p := &countingPersister{}
	s := NewRulesStoreWithPersister(p, nil)
	if p.saves != 1 {
		t.Fatalf("播种应只写一次文档，实际 %d 次", p.saves)
	}
	if len(s.List()) < 10 {
		t.Fatalf("应播种多条内置规则，实际 %d", len(s.List()))
	}
	ids := map[string]bool{}
	for _, r := range s.List() {
		if ids[r.ID] {
			t.Fatalf("规则 ID 重复: %s（同毫秒批量创建不能撞 ID）", r.ID)
		}
		ids[r.ID] = true
	}
}

// 升级到「播种记录入库」的第一版时，规则文档已存在但新标记还没有。
// 这时不能把内置规则补一遍——用户此前删掉的会被一次性复活。
func TestRulesUpgradeWithoutMarkerDoesNotResurrect(t *testing.T) {
	dir := t.TempDir()
	doc := configdoc.NewFilePersister(filepath.Join(dir, "rules.yaml"))
	existing := []model.AlertRule{{
		ID: "r-user", Name: "用户自己的规则", Metric: "cpu_usage", Operator: ">",
		Threshold: 90, Severity: "warning", Enabled: true,
	}}
	data, err := yaml.Marshal(existing)
	if err != nil {
		t.Fatal(err)
	}
	if err := doc.Save(data); err != nil {
		t.Fatal(err)
	}
	marker := configdoc.NewFilePersister(filepath.Join(dir, "rules.seeded"))

	s := NewRulesStoreWithPersister(doc, marker)
	if got := len(s.List()); got != 1 {
		t.Fatalf("升级时不应补播内置规则，实际 %d 条", got)
	}
	if _, err := marker.Load(); err != nil {
		t.Fatalf("应补写播种记录，否则下次启动会再次尝试播种: %v", err)
	}
}

package asset

import (
	"strings"
	"testing"
)

// 属性字典与枚举（设计件 docs/superpowers/specs/2026-10-08-asset-attr-dictionary-design.md）。
//
// 守四件事：
//  1. 各类型的取值校验（number / date / enum / bool 各有正负例），空值一律放行；
//  2. 字典本身在**写侧**被校验（未知类型、非 enum 配了取值、enum 取值重复或为空）；
//  3. **只校验人工值**——采集值不受字典约束（那会把"真实但不合规"的机器挡在台账外）；
//  4. 没配字典的字段维持自由键（字典是可选加固，不是"必须登记才能填"）。

func TestValidateAttrValue(t *testing.T) {
	enumMeta := AttrMeta{Kind: AttrKindEnum, Enum: []EnumOption{{Value: "prod"}, {Value: "test"}}}
	cases := []struct {
		name  string
		meta  AttrMeta
		value string
		ok    bool
	}{
		{"text 不校验", AttrMeta{}, "任何值", true},
		{"number 正例", AttrMeta{Kind: AttrKindNumber}, "12.5", true},
		{"number 负例", AttrMeta{Kind: AttrKindNumber}, "十二", false},
		{"date 正例", AttrMeta{Kind: AttrKindDate}, "2026-10-08", true},
		{"date 负例", AttrMeta{Kind: AttrKindDate}, "2026/10/08", false},
		{"date 也接受 RFC3339", AttrMeta{Kind: AttrKindDate}, "2026-10-08T10:00:00Z", true},
		{"bool 正例", AttrMeta{Kind: AttrKindBool}, "TRUE", true},
		{"bool 负例", AttrMeta{Kind: AttrKindBool}, "yes", false},
		{"enum 正例", enumMeta, "prod", true},
		{"enum 负例", enumMeta, "other", false},
		{"空值放行（number）", AttrMeta{Kind: AttrKindNumber}, "", true},
		{"空值放行（enum）", enumMeta, "  ", true},
	}
	for _, c := range cases {
		err := ValidateAttrValue("f", c.meta, c.value)
		if c.ok && err != nil {
			t.Fatalf("%s：应通过，实际 %v", c.name, err)
		}
		if !c.ok && err == nil {
			t.Fatalf("%s：应拒绝，实际通过", c.name)
		}
	}

	// 拒绝时的提示要能直接给人看：带上字段名与可选值
	err := ValidateAttrValue("env", enumMeta, "staging")
	if err == nil || !strings.Contains(err.Error(), "env") || !strings.Contains(err.Error(), "prod") {
		t.Fatalf("枚举拒绝的提示应含字段名与可选值，实际 %v", err)
	}
}

func TestValidateTypeSchemaAttrDict(t *testing.T) {
	bad := []struct {
		name string
		meta AttrMeta
	}{
		{"未知类型", AttrMeta{Kind: "numberr"}},
		{"非 enum 配了取值", AttrMeta{Kind: AttrKindText, Enum: []EnumOption{{Value: "a"}}}},
		{"enum 没有取值", AttrMeta{Kind: AttrKindEnum}},
		{"enum 取值为空", AttrMeta{Kind: AttrKindEnum, Enum: []EnumOption{{Value: " "}}}},
		{"enum 取值重复", AttrMeta{Kind: AttrKindEnum, Enum: []EnumOption{{Value: "a"}, {Value: "a"}}}},
	}
	for _, c := range bad {
		if err := ValidateTypeSchema(TypeSchema{AttrMeta: map[string]AttrMeta{"f": c.meta}}); err == nil {
			t.Fatalf("%s：应被写侧拒绝", c.name)
		}
	}

	good := TypeSchema{AttrMeta: map[string]AttrMeta{
		"env":  {Kind: AttrKindEnum, Enum: []EnumOption{{Value: "prod", Label: "生产"}}},
		"cost": {Kind: AttrKindNumber, Unit: "元/月"},
	}}
	if err := ValidateTypeSchema(good); err != nil {
		t.Fatalf("合法字典应通过：%v", err)
	}

	// 读侧宽松：未知 Kind 按 text（不校验）——坏数据不该让台账起不来
	if got := (AttrMeta{Kind: "numberr"}).KindNormalized(); got != AttrKindText {
		t.Fatalf("未知 Kind 读侧应按 text，实际 %q", got)
	}
	if got := (AttrMeta{}).KindNormalized(); got != AttrKindText {
		t.Fatalf("空 Kind 应按 text，实际 %q", got)
	}
}

// 决定性判据：字典只拦人工值，不拦采集值。
func TestApplyDictOnlyGuardsManual(t *testing.T) {
	svc, _ := newTestService(t)
	if err := svc.UpdateTypeModel(TypeHost, TypeSchema{AttrMeta: map[string]AttrMeta{
		"env": {Kind: AttrKindEnum, Enum: []EnumOption{{Value: "prod"}, {Value: "test"}}},
	}}); err != nil {
		t.Fatalf("保存模型失败：%v", err)
	}

	// ① 采集值落在枚举外 → **必须放行**（真实环境上报什么就记什么）
	if _, _, err := svc.Apply(Observation{
		TypeKey: TypeHost, NaturalKey: "web-01", Name: "web-01", Node: "web-01",
		Source: SourceDiscovery, Attrs: map[string]string{"env": "staging"},
	}); err != nil {
		t.Fatalf("采集值不该被字典拦下，实际 %v", err)
	}

	// ② 人工值落在枚举外 → 拒绝，且提示里指明字段
	_, _, err := svc.Apply(Observation{
		TypeKey: TypeHost, NaturalKey: "web-01", Source: SourceManual, Actor: "tester",
		Attrs: map[string]string{"env": "staging"},
	})
	if err == nil || !strings.Contains(err.Error(), "env") {
		t.Fatalf("人工值非法应被拒绝并指明字段，实际 %v", err)
	}

	// ③ 人工值合法 → 通过
	if _, _, err := svc.Apply(Observation{
		TypeKey: TypeHost, NaturalKey: "web-01", Source: SourceManual, Actor: "tester",
		Attrs: map[string]string{"env": "prod"},
	}); err != nil {
		t.Fatalf("合法人工值应通过，实际 %v", err)
	}

	// ④ 没配字典的字段仍是自由键
	if _, _, err := svc.Apply(Observation{
		TypeKey: TypeHost, NaturalKey: "web-01", Source: SourceManual, Actor: "tester",
		Attrs: map[string]string{"anything": "随便填"},
	}); err != nil {
		t.Fatalf("未配字典的字段应保持自由键，实际 %v", err)
	}

	// ⑤ 清空（空值）放行：清空字段不该被字典拦住
	if _, _, err := svc.Apply(Observation{
		TypeKey: TypeHost, NaturalKey: "web-01", Source: SourceManual, Actor: "tester",
		Attrs: map[string]string{"env": ""},
	}); err != nil {
		t.Fatalf("清空人工值不该被字典拦住，实际 %v", err)
	}
}

// 字典改完立刻生效（模型缓存要被失效）：这是"我改了模型却没生效"这类谜题的守门用例。
func TestDictTakesEffectImmediately(t *testing.T) {
	svc, _ := newTestService(t)
	write := func(v string) error {
		_, _, err := svc.Apply(Observation{
			TypeKey: TypeHost, NaturalKey: "web-01", Source: SourceManual, Actor: "tester",
			Attrs: map[string]string{"env": v},
		})
		return err
	}
	// 先没字典：任何值都能写
	if err := write("whatever"); err != nil {
		t.Fatalf("没配字典时不该校验，实际 %v", err)
	}
	// 配上 enum 后：旧值立刻不再可写
	if err := svc.UpdateTypeModel(TypeHost, TypeSchema{AttrMeta: map[string]AttrMeta{
		"env": {Kind: AttrKindEnum, Enum: []EnumOption{{Value: "prod"}}},
	}}); err != nil {
		t.Fatalf("保存模型失败：%v", err)
	}
	if err := write("whatever"); err == nil {
		t.Fatal("配上枚举后非法值应被拒（模型缓存必须失效）")
	}
	if err := write("prod"); err != nil {
		t.Fatalf("枚举内取值应通过，实际 %v", err)
	}
}

package model

// 护栏组合风险（"两个护栏配在一起就等于任意代码执行"）。
//
// 为什么这个类型在 `model` 而不在 Agent 侧：它要**随上报体序列化**送出去，
// 而界面要用**同一句话**描述它——`Describe()` 原本的注释就写着"日志与后续的界面展示共用，
// 避免两处措辞不一致"，把它放在两端都引用的包里，这句话才真的成立。
//
// 判定本身仍在 Agent 侧（`internal/agent/ops/guardcheck.go`）：只有它知道本机
// `guards.ops` 的真实配置。这里只放**被传输的形状**。

// GuardRisk 是一条「护栏组合导致任意代码执行」的风险。
type GuardRisk struct {
	// Kind 命中类型：exec（可重启服务的可执行文件落在允许写入的目录里）
	// / persistence（允许写入的是系统会自己执行的目录）。
	Kind string `json:"kind"`
	// AllowedDir 是 file.push 允许写入的目录（配置里的形态）。
	AllowedDir string `json:"allowedDir"`
	// Exposed 是因此暴露出来的东西：kind=exec 时是单元的可执行文件路径，
	// kind=persistence 时是系统会自动执行的那个目录。
	Exposed string `json:"exposed"`
	// Unit 只在 kind=exec 时有值：那个可重启的单元。
	Unit string `json:"unit,omitempty"`
}

// 风险类型常量（界面按它分组，也用于事件去重的键）。
const (
	// GuardRiskExec 可写目录覆盖了某可重启单元的可执行文件。
	GuardRiskExec = "exec"
	// GuardRiskPersistence 可写目录就是系统会自己执行的目录。
	GuardRiskPersistence = "persistence"
)

// Describe 生成给人看的一句话（Agent 日志与界面展示共用，避免两处措辞不一致）。
func (r GuardRisk) Describe() string {
	if r.Kind == GuardRiskExec {
		return "护栏组合风险：允许写入的目录「" + r.AllowedDir + "」覆盖了可重启单元「" + r.Unit +
			"」的可执行文件「" + r.Exposed + "」——推一个文件再重启该服务，等于任意代码执行"
	}
	return "护栏组合风险：允许写入的目录「" + r.AllowedDir + "」覆盖了系统会自动执行的目录「" + r.Exposed +
		"」——写入即持久化，不需要重启任何服务"
}

// Summary 是这条风险的一句话摘要（用于事件去重与列表里的短标签）。
func (r GuardRisk) Summary() string {
	return r.Kind + "|" + r.AllowedDir + "|" + r.Exposed + "|" + r.Unit
}

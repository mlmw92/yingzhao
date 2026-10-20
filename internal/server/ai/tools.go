package ai

import (
	"context"
	"encoding/json"
)

// Tool 是一个只读工具：Description 与 Schema 给模型看，Handler 在服务端执行。
type Tool struct {
	Name        string
	Description string
	Schema      json.RawMessage
	ReadOnly    bool
	Handler     func(ctx context.Context, args map[string]any) (string, error)
}

// ToolScope 是工具可访问的范围。自动调查按告警节点构造；
// 工具实现必须相信这个字段，而不是相信模型传进来的参数。
type ToolScope struct {
	Node           string
	AllowSensitive bool
	// Propose 把一条通过校验的处置建议记入本次调查。为 nil 时不暴露 propose_action。
	Propose func(Suggestion) error
}

// Toolbox 按范围提供工具。每次调查构造一个实例。
type Toolbox interface {
	Tools() []Tool
}

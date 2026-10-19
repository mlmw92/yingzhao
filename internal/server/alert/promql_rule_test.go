package alert

import (
	"testing"

	"github.com/nebula/monitor/internal/model"
)

func TestValidateRulePromQLRequiresExpr(t *testing.T) {
	base := model.AlertRule{Name: "qps", Type: model.RuleTypePromQL,
		Severity: model.SeverityWarning, Enabled: true}
	if err := ValidateRule(base); err == nil {
		t.Fatal("缺少 Expr 应拒绝")
	}
	base.Expr = "sum(kafka_consumer_lag) > 10000"
	if err := ValidateRule(base); err != nil {
		t.Fatalf("合法表达式不应被拒: %v", err)
	}
}

func TestValidateRuleThresholdUnaffected(t *testing.T) {
	r := model.AlertRule{Name: "cpu", Metric: "cpu_usage", Operator: ">",
		Threshold: 85, Severity: model.SeverityWarning, Enabled: true}
	if err := ValidateRule(r); err != nil {
		t.Fatalf("阈值规则校验不应受影响: %v", err)
	}
}

package api

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/nebula/monitor/internal/server/report"
)

// 报告周期化调度（全景表 11-4）。
//
// 与 `POST /api/v1/report/generate` 的分工：那个是"现在生成一份"，这里是"以后每隔
// 一段时间自动生成一份"。手动生成也走调度器的 RunNow，因此"上次运行"的口径一致——
// 否则界面上会出现"我明明刚生成过，调度却说到点该生成了"。

// ReportScheduleProvider 提供报告周期化调度（可空；未注入时接口返回 503）。
type ReportScheduleProvider interface {
	Config() report.ScheduleConfig
	Save(report.ScheduleConfig) error
	Status() report.ScheduleStatus
	// RunNow 的 by 是触发者（手动为登录名，定时传空串）：范围要按触发者当前身份收窄。
	RunNow(by string, manual bool) report.ScheduleRun
}

// SetReportScheduler 注入报告调度器（未注入时相关接口返回 503，与管理端「该能力未启用」一致）。
func (a *API) SetReportScheduler(s ReportScheduleProvider) { a.reportSched = s }

// handleReportScheduleGet 返回调度配置与上次运行结果。
func (a *API) handleReportScheduleGet(w http.ResponseWriter, r *http.Request) {
	if a.reportSched == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "报告调度未启用"})
		return
	}
	writeJSON(w, http.StatusOK, a.reportSched.Status())
}

// handleReportScheduleSave 保存调度配置（热生效）。
//
// 权限用 report:export 而不是 report:read：它决定"以后会自动生成报告"，
// 与手动生成是同一类动作（都会产出报告文件、占用磁盘）。
func (a *API) handleReportScheduleSave(w http.ResponseWriter, r *http.Request) {
	if a.reportSched == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "报告调度未启用"})
		return
	}
	var cfg report.ScheduleConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求体解析失败"})
		return
	}
	// 范围**只能由服务端按身份折算**：请求体里带的 scope 一律覆盖掉。
	// 否则一个受限用户只要提交一个更宽的范围，就能让定时报告跑到自己看不到的节点上去。
	//
	// 这里用请求上下文里的 Principal（保存请求一定带认证身份），而不是按用户名回查存储：
	// 回查只在**定时执行**那条没有请求上下文的路径上才需要。
	actor := assetActor(r)
	res := a.reportScopeFor(Principal(r))
	// 章节选择是用户意图（不是权限），从请求体里取；范围则只认服务端折算的结果。
	sections, err := normalizeReportSections(cfg.Sections)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	cfg.Scope = &res.Scope
	cfg.Sections = sections
	cfg.SavedBy = actor
	if err := a.reportSched.Save(cfg); err != nil {
		slog.Error("保存报告调度配置失败", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "保存报告调度配置失败"})
		return
	}
	writeJSON(w, http.StatusOK, a.reportSched.Status())
}

// handleReportScheduleRun 立即生成一次（与手动生成等价，但会更新"上次运行"）。
func (a *API) handleReportScheduleRun(w http.ResponseWriter, r *http.Request) {
	if a.reportSched == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "报告调度未启用"})
		return
	}
	run := a.reportSched.RunNow(assetActor(r), true)
	if run.Error != "" {
		// 生成失败不是"请求写错了"，而是服务端没能产出：502 让前端与运维都看清方向。
		writeJSON(w, http.StatusBadGateway, map[string]interface{}{"run": run, "error": run.Error})
		return
	}
	writeJSON(w, http.StatusOK, run)
}

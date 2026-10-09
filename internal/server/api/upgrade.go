package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/nebula/monitor/internal/server/approval"
	"github.com/nebula/monitor/internal/server/upgrade"
	"github.com/nebula/monitor/internal/version"
)

// 升级包最大 500 MB
const upgradeMaxBytes = 500 << 20

// archiveVersions 把归档条目折成版本号清单（审批单里只给人看版本，不塞整条记录）。
func archiveVersions(entries []upgrade.ArchiveEntry) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Version)
	}
	return out
}

// handleSystemUpgradeUpload 接收 multipart 上传的 upgrade tar.gz。
// 表单字段：
//   - file: tar.gz 文件（必填）
func (a *API) handleSystemUpgradeUpload(w http.ResponseWriter, r *http.Request) {
	if a.upgrader == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "升级功能未启用"})
		return
	}
	task, err := a.upgrader.Upload(r, upgradeMaxBytes)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, task)
}

// handleSystemUpgradeCurrent 返回当前已上传待应用的升级任务（pending）。
func (a *API) handleSystemUpgradeCurrent(w http.ResponseWriter, r *http.Request) {
	if a.upgrader == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"current": nil})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"current": a.upgrader.Current()})
}

// handleSystemUpgradeApply 立即应用升级：备份→替换 server→替换 web→复制新 agent 到 agentBinDir→重启 server。
func (a *API) handleSystemUpgradeApply(w http.ResponseWriter, r *http.Request) {
	if a.upgrader == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "升级功能未启用"})
		return
	}
	operator := r.URL.Query().Get("operator")
	if operator == "" {
		operator = "web"
	}
	// 双人复核：升级会重启服务、替换二进制，属于最不可逆的一类动作。
	// 审批单里带上"当前版本 + 已归档的版本清单"：审批人要能看出自己在批准什么，
	// 只写一句"应用系统升级"等于让他盲签。
	if a.approvalGate(w, r, approval.EntrySystemUpgrade, "", true, &approval.Request{
		Extra: map[string]string{
			"current": version.Version,
			"archive": strings.Join(archiveVersions(a.upgrader.Archive()), ","),
			"apply":   "true",
		},
		Title: "应用系统升级（当前 " + version.Version + "）",
	}) {
		return
	}
	task, err := a.upgrader.Apply(operator)
	if err != nil {
		// 服务端即将重启或刚重启，连接可能断开；客户端需轮询 /current 或刷新页面
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"task": task, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, task)
}

// handleSystemUpgradeHistory 返回升级历史记录（最新在前）。
func (a *API) handleSystemUpgradeHistory(w http.ResponseWriter, r *http.Request) {
	if a.upgrader == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"history": []string{}})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"history": a.upgrader.History()})
}

// handleSystemUpgradeArchive 返回已上传（已归档）的升级包列表，供回退到指定版本。
func (a *API) handleSystemUpgradeArchive(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "方法不允许"})
		return
	}
	if a.upgrader == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"versions": []string{}})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"versions": a.upgrader.Archive()})
}

// handleSystemUpgradeArchiveDelete 删除指定版本的归档升级包。
// 升级包一旦删除，该版本就不再出现在可切换清单里（也就无法再切换/回退到该版本）；
// 需要恢复该能力只能重新上传对应版本的升级包。
func (a *API) handleSystemUpgradeArchiveDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "方法不允许"})
		return
	}
	if a.upgrader == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "升级功能未启用"})
		return
	}
	var body struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求体解析失败: " + err.Error()})
		return
	}
	if body.Version == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "缺少 version 参数"})
		return
	}
	if err := a.upgrader.DeleteArchive(body.Version); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"versions": a.upgrader.Archive()})
}

// handleSystemUpgradeRollbackTo 回退到指定的已归档版本。
func (a *API) handleSystemUpgradeRollbackTo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "方法不允许"})
		return
	}
	if a.upgrader == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "升级功能未启用"})
		return
	}
	var body struct {
		Version  string `json:"version"`
		Operator string `json:"operator"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求体解析失败: " + err.Error()})
		return
	}
	if body.Version == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "缺少 version 参数"})
		return
	}
	if body.Operator == "" {
		body.Operator = "web"
	}
	// 双人复核：回滚同样不可逆（会把正在运行的版本换掉）。Extra 里的 version 也让执行侧
	// 能区分"升级"与"回滚"——两者共用同一个入口标识（system:upgrade）。
	if a.approvalGate(w, r, approval.EntrySystemUpgrade, "", true, &approval.Request{
		Extra: map[string]string{
			"version": body.Version, "rollback": "true", "current": version.Version,
		},
		Title: "回退系统到 " + body.Version,
	}) {
		return
	}
	task, err := a.upgrader.RollbackTo(body.Version, body.Operator)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"task": task, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, task)
}
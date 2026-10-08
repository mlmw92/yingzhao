package api

import (
	"net/http"
)

// 审计完整性校验接口（设计件 `2026-10-08-audit-tamper-evident-design.md` §4.5）。
//
// 权限沿用 `audit:read`：**不新设权限点**。理由——校验结果不泄露超出审计内容本身的信息
// （它只说"第 N 条的签名对不上"，而第 N 条的内容本来就能通过 `audit:read` 读到）。
// 导出那条之所以要单独的 `audit:export`，是因为它一次把**全部**审计落盘成一个文件，
// 与"逐页翻看"不是一个量级的动作；校验不落盘、不导出。
//
// **不返回密钥**：只返回指纹。密钥一旦能从接口取到，"没有密钥就无法伪造"这条保证就没了。
func (a *API) handleAuditVerify(w http.ResponseWriter, r *http.Request) {
	if a.audit == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"enabled": false, "mode": "none", "ok": false,
			"breaks": []interface{}{},
			"note":   "审计存储未注入，无法校验。",
		})
		return
	}
	writeJSON(w, http.StatusOK, a.audit.Verify())
}

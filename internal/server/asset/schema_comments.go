package asset

// 表与列的说明。它们是**数据库注释**的来源：
//   - MySQL：建表时写成列/表 COMMENT；
//   - PostgreSQL / 达梦：建表后执行 COMMENT ON。
// 文档脚本与运行时建表共用同一份说明，避免两处漂移。

import (
	"fmt"
	"regexp"
	"strings"
)

var tableComments = map[string]string{
	"config_documents":          "整份读写的配置文档（JSON 信封；敏感内容 AES-GCM 加密）",
	"nodes":                     "节点注册信息与最近心跳",
	"node_groups":               "节点分组",
	"asset_types":               "资产类型与类型模型",
	"assets":                    "资产台账主表",
	"asset_attrs":               "资产属性（采集 / 人工 / 派生）",
	"asset_links":               "资产关系",
	"asset_link_suppressions":   "人工删除过的采集关系（阻止采集侧重建）",
	"asset_changes":             "资产字段级变更记录",
	"snapshots":                 "配置快照头",
	"snapshot_fields":           "配置快照字段",
	"asset_seen":                "资产最近一次采集上报时间",
	"inspect_runs":              "巡检批次",
	"inspect_run_manifest":      "巡检批次清单标记",
	"inspect_run_members":       "巡检批次包含的资产",
	"inspect_findings":          "巡检差异项",
	"inspect_finding_baselines": "差异项与标杆来源的关联",
	"inspect_baselines":         "资产类型的巡检标杆",
	"asset_ignored":             "人工忽略的资产",
	"asset_labels":              "资产标签",
	"audit_events":              "审计事件（seq + prev_hash + hash 组成哈希链）",
	"audit_chain":               "审计链头（单行，FOR UPDATE 串行追加）",
	"audit_pruned":              "合法裁剪区间与 HMAC 锚点",
	"alert_acks":                "告警处置（认领 / 指派 / 关闭 / 评论）",
	"ops_tasks":                 "下行操作任务",
	"ops_meta":                  "下行任务元数据（序号等）",
	"ops_caps":                  "节点声明的可执行动作",
	"schema_version":            "资产库结构版本",
}

var columnComments = map[string]map[string]string{
	"config_documents": {
		"namespace":  "配置命名空间（notify / ui / alert_rules …）",
		"version":    "乐观锁版本号，每次写入 +1",
		"payload":    "JSON 信封：format / encoding / nonce / data",
		"updated_at": "最后写入时间（epoch ms）",
		"updated_by": "最后写入人（预留）",
	},
	"nodes": {
		"hostname":     "主机名（主键，Agent 上报原样）",
		"mode":         "运行模式 collect / edge / hub",
		"display_name": "界面显示名（人工设置，不改 hostname）",
		"ip":           "最近上报 IP",
		"os":           "操作系统",
		"arch":         "CPU 架构",
		"group_name":   "所属分组（Server 端管理）",
		"labels":       "标签 JSON",
		"version":      "Agent 版本",
		"host_info":    "主机硬件信息 JSON（注册级字段）",
		"log_sources":  "日志来源 JSON",
		"status":       "online / offline",
		"last_seen":    "最近心跳时间（epoch ms）",
		"created_at":   "首次注册时间",
		"updated_at":   "最近写入时间",
	},
	"node_groups": {
		"name":        "分组名（主键）",
		"description": "分组说明",
		"created_at":  "创建时间",
	},
	"asset_types": {
		"key":     "类型键（host / redis / mysql …）",
		"title":   "显示名",
		"builtin": "是否内置类型（1/0）",
		"schema":  "类型模型 JSON（关注字段、属性元数据）",
	},
	"assets": {
		"id":          "自增主键",
		"type_key":    "资产类型键",
		"natural_key": "自然键（类型内唯一：hostname / host:port …）",
		"name":        "显示名",
		"node":        "归属节点",
		"created_at":  "创建时间",
		"updated_at":  "最近更新时间",
	},
	"asset_attrs": {
		"asset_id":   "资产 ID",
		"key":        "属性键",
		"value":      "属性值（文本）",
		"source":     "来源 discovery / manual / derived",
		"updated_at": "更新时间",
		"updated_by": "更新人",
	},
	"asset_links": {
		"id":         "自增主键",
		"from_id":    "起点资产 ID",
		"to_id":      "终点资产 ID",
		"kind":       "关系类型（runs_on / depends_on …）",
		"source":     "来源 discovery / manual",
		"created_at": "创建时间",
	},
	"asset_link_suppressions": {
		"id":         "自增主键",
		"from_id":    "起点资产 ID",
		"to_id":      "终点资产 ID",
		"kind":       "关系类型",
		"created_at": "抑制时间",
		"created_by": "操作人",
	},
	"asset_changes": {
		"id":         "自增主键",
		"asset_id":   "资产 ID",
		"field":      "变更字段",
		"old_value":  "旧值",
		"new_value":  "新值",
		"source":     "变更来源",
		"actor":      "操作人 / 采集器",
		"kind":       "变更类型（create / update / delete …）",
		"at":         "变更时间（epoch ms）",
		"request_id": "关联请求 ID（与审计对齐）",
	},
	"snapshots": {
		"id":       "自增主键",
		"asset_id": "资产 ID",
		"taken_at": "快照时间",
	},
	"snapshot_fields": {
		"snapshot_id": "快照 ID",
		"key":         "字段名",
		"value":       "字段值",
	},
	"asset_seen": {
		"asset_id":     "资产 ID（主键）",
		"last_seen_at": "最近一次采集上报时间",
	},
	"inspect_runs": {
		"id":         "自增主键",
		"scope":      "巡检范围",
		"actor":      "触发人 / 调度器",
		"started_at": "开始时间",
		"assets":     "巡检资产数",
		"baselined":  "命中标杆数",
		"findings":   "差异数",
		"truncated":  "是否因上限截断（1/0）",
	},
	"inspect_run_manifest": {
		"run_id": "巡检批次 ID（主键）",
	},
	"inspect_run_members": {
		"run_id":      "巡检批次 ID",
		"asset_id":    "资产 ID",
		"node_at_run": "巡检时的归属节点",
		"baselined":   "该资产是否命中标杆（1/0）",
		"findings":    "该资产的差异数",
	},
	"inspect_findings": {
		"id":         "自增主键",
		"run_id":     "巡检批次 ID",
		"asset_id":   "资产 ID",
		"asset_type": "资产类型键（快照）",
		"asset_key":  "资产自然键（快照）",
		"asset_name": "资产显示名（快照）",
		"node":       "归属节点（快照）",
		"field":      "差异字段",
		"kind":       "差异类型（missing / extra / changed …）",
		"level":      "级别（warning / critical …）",
		"expected":   "期望值",
		"actual":     "实际值",
		"at":         "产生时间",
	},
	"inspect_finding_baselines": {
		"finding_id": "差异项 ID（主键）",
		"asset_id":   "标杆来源资产 ID",
	},
	"inspect_baselines": {
		"type_key":    "资产类型键（主键）",
		"asset_id":    "标杆资产 ID",
		"asset_key":   "标杆资产自然键（快照）",
		"snapshot_id": "标杆快照 ID",
		"set_by":      "设置人",
		"set_at":      "设置时间",
	},
	"asset_ignored": {
		"asset_id":   "资产 ID（主键）",
		"reason":     "忽略原因",
		"ignored_by": "操作人",
		"ignored_at": "忽略时间",
	},
	"asset_labels": {
		"asset_id":   "资产 ID",
		"key":        "标签键",
		"value":      "标签值",
		"updated_at": "更新时间",
		"updated_by": "更新人",
	},
	"audit_events": {
		"id":         "自增主键",
		"time_ms":    "事件时间（epoch ms）",
		"user_name":  "操作人",
		"method":     "HTTP 方法",
		"path":       "请求路径",
		"status":     "HTTP 状态码",
		"remote_ip":  "来源 IP",
		"source_loc": "IP 属地",
		"succeeded":  "是否成功（1/0）",
		"category":   "分类（asset / ops / security …）",
		"action":     "动作标识",
		"detail":     "详情",
		"request_id": "请求关联 ID",
		"seq":        "链上序号（0 = 启用链之前的历史行）",
		"prev_hash":  "前一条哈希",
		"hash":       "本条哈希",
		"key_fp":     "链密钥指纹",
	},
	"audit_chain": {
		"id":              "固定为 1（单行）",
		"seq":             "当前链头序号",
		"hash":            "当前链头哈希",
		"key_fingerprint": "链密钥指纹",
		"updated_ms":      "更新时间",
	},
	"audit_pruned": {
		"from_seq":  "裁剪区间起始 seq",
		"to_seq":    "裁剪区间结束 seq",
		"head_hash": "区间末条哈希（校验锚点）",
		"rows":      "被裁行数",
		"pruned_ms": "裁剪时间",
		"mac":       "裁剪记录的 HMAC",
	},
	"alert_acks": {
		"ack_key":       "处置键 rule|host|instance|startsAt",
		"rule":          "规则名",
		"host":          "主机",
		"instance":      "实例",
		"starts_at":     "告警触发时间",
		"status":        "pending / ack / closed",
		"user_name":     "最近操作人",
		"assignee":      "当前处理人",
		"time_ms":       "最近操作时间",
		"ack_time_ms":   "认领时间",
		"close_time_ms": "关闭时间",
		"close_reason":  "关闭原因",
		"comments":      "评论 JSON",
	},
	"ops_tasks": {
		"id":         "任务 ID（ops-N / ob-N）",
		"batch_id":   "批次 ID",
		"node":       "目标节点",
		"kind":       "动作标识",
		"state":      "queued / running / succeeded / failed / canceled / expired",
		"created_at": "创建时间",
		"done_at":    "结束时间",
		"task_json":  "完整任务 JSON（参数 / 结果 / 操作人）",
	},
	"ops_meta": {
		"key":   "元数据键（目前只有 seq）",
		"value": "元数据值",
	},
	"ops_caps": {
		"node":  "节点名（主键）",
		"kinds": "该节点可执行动作（逗号分隔）",
	},
	"schema_version": {
		"id":      "固定为 1",
		"version": "资产库结构版本",
	},
}

var createTableRE = regexp.MustCompile(`(?i)CREATE TABLE IF NOT EXISTS\s+([A-Za-z_][A-Za-z0-9_]*)`)
var columnLineRE = regexp.MustCompile(`^\s*[` + "`" + `"]?([A-Za-z_][A-Za-z0-9_]*)[` + "`" + `"]?\s+(BIGINT|INT|VARCHAR|LONGTEXT|TEXT|TINYINT|SMALLINT|JSON|CLOB|BIGSERIAL)\b`)
var constraintWords = map[string]bool{
	"PRIMARY": true, "UNIQUE": true, "CONSTRAINT": true, "FOREIGN": true, "CHECK": true,
}

func escapeSQLComment(s string) string { return strings.ReplaceAll(s, "'", "''") }

// schemaTableOf 返回 CREATE TABLE 语句的表名；不是建表语句时返回空。
func schemaTableOf(stmt string) string {
	m := createTableRE.FindStringSubmatch(stmt)
	if m == nil {
		return ""
	}
	return m[1]
}

// schemaColumn 是一个列名及其在 DDL 里的完整定义行。
type schemaColumn struct {
	Name string
	Line string
}

// schemaColumns 解析 CREATE TABLE 的列定义（跳过约束与索引行）。
func schemaColumns(stmt string) []schemaColumn {
	table := schemaTableOf(stmt)
	if table == "" {
		return nil
	}
	var out []schemaColumn
	for _, line := range strings.Split(stmt, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == ")" || strings.HasPrefix(trimmed, ")") {
			break
		}
		cm := columnLineRE.FindStringSubmatch(trimmed)
		if cm == nil || constraintWords[strings.ToUpper(cm[1])] {
			continue
		}
		out = append(out, schemaColumn{Name: strings.ToLower(cm[1]), Line: line})
	}
	return out
}

// mysqlWithComments 给 MySQL 的 CREATE TABLE 注入列/表 COMMENT。
func mysqlWithComments(stmts []string) []string {
	out := make([]string, 0, len(stmts))
	for _, stmt := range stmts {
		table := schemaTableOf(stmt)
		if table == "" {
			out = append(out, stmt)
			continue
		}
		comments := columnComments[table]
		lines := strings.Split(stmt, "\n")
		for i, line := range lines {
			trimmed := strings.TrimSpace(line)
			cm := columnLineRE.FindStringSubmatch(trimmed)
			if cm == nil || constraintWords[strings.ToUpper(cm[1])] {
				continue
			}
			c := comments[strings.ToLower(cm[1])]
			if c == "" {
				continue
			}
			c = escapeSQLComment(c)
			if strings.HasSuffix(strings.TrimSpace(line), ",") {
				// 在行尾逗号前插入 COMMENT。
				idx := strings.LastIndex(line, ",")
				lines[i] = line[:idx] + " COMMENT '" + c + "'" + line[idx:]
			} else {
				lines[i] = line + " COMMENT '" + c + "'"
			}
		}
		if c := tableComments[table]; c != "" {
			for i, line := range lines {
				if strings.HasPrefix(strings.TrimSpace(line), ")") {
					lines[i] = strings.Replace(line, ")", ") COMMENT='"+escapeSQLComment(c)+"'", 1)
					break
				}
			}
		}
		out = append(out, strings.Join(lines, "\n"))
	}
	return out
}

// commentColumnName 按方言引用列名（PG/DM 的 key/schema/rows 在 DDL 里加了引号）。
func commentColumnName(col string) string {
	switch col {
	case "key", "schema", "rows":
		return `"` + col + `"`
	}
	return col
}

// commentOnStatements 生成 PostgreSQL / 达梦的 COMMENT ON 语句。
func commentOnStatements(stmts []string) []string {
	var out []string
	for _, stmt := range stmts {
		table := schemaTableOf(stmt)
		if table == "" {
			continue
		}
		if c := tableComments[table]; c != "" {
			out = append(out, fmt.Sprintf("COMMENT ON TABLE %s IS '%s'", table, escapeSQLComment(c)))
		}
		for _, col := range schemaColumns(stmt) {
			c := columnComments[table][col.Name]
			if c == "" {
				continue
			}
			out = append(out, fmt.Sprintf("COMMENT ON COLUMN %s.%s IS '%s'",
				table, commentColumnName(col.Name), escapeSQLComment(c)))
		}
	}
	return out
}

// mysqlCommentStatements 给存量 MySQL 库补表/列注释（一次性升级用）。
//
// MySQL 没有 COMMENT ON COLUMN，只能 MODIFY COLUMN 重述定义；
// 定义直接取自建表语句，保证与目标结构一致。
func mysqlCommentStatements(stmts []string) []string {
	var out []string
	for _, stmt := range stmts {
		table := schemaTableOf(stmt)
		if table == "" {
			continue
		}
		if c := tableComments[table]; c != "" {
			out = append(out, fmt.Sprintf("ALTER TABLE %s COMMENT='%s'", table, escapeSQLComment(c)))
		}
		for _, col := range schemaColumns(stmt) {
			c := columnComments[table][col.Name]
			if c == "" {
				continue
			}
			def := strings.TrimSpace(col.Line)
			def = strings.TrimSuffix(def, ",")
			def = strings.TrimSpace(strings.TrimSuffix(def, "PRIMARY KEY"))
			out = append(out, fmt.Sprintf("ALTER TABLE %s MODIFY COLUMN %s COMMENT '%s'",
				table, def, escapeSQLComment(c)))
		}
	}
	return out
}

<template>
  <div class="alerts-view">
    <PageHeader title="告警中心" desc="活跃告警、告警事件、规则与抑制策略的统一入口">
      <template #actions>
        <el-button size="small" plain :icon="Printer" @click="printList">打印</el-button>
      </template>
    </PageHeader>

    <PartialBar
      v-if="partialFailed.length"
      :failed="partialFailed"
      title="部分数据加载失败"
      @retry="load"
    />

    <!-- 告警统计看板 -->
    <SectionCard title="告警概览">
      <div v-if="statsError" class="alert-refresh-error">{{ statsError }}</div>
      <!-- 颜色只表达风险等级，不表达类别：只有「活跃告警」与「紧急」会整块变红，
           警告用琥珀，其余保持中性——否则六个数字一起上色，等于没有重点。 -->
      <div class="alert-stats" v-loading="statsLoading">
        <div class="kpi" :class="{ 'kpi-alert': stats.firing > 0 }">
          <div class="kpi-label">活跃告警</div>
          <div class="kpi-value" :class="{ red: stats.firing > 0 }">{{ stats.firing }}</div>
        </div>
        <div class="kpi" :class="{ 'kpi-alert': stats.bySeverity.critical > 0 }">
          <div class="kpi-label">紧急</div>
          <div class="kpi-value" :class="{ red: stats.bySeverity.critical > 0 }">{{ stats.bySeverity.critical }}</div>
        </div>
        <div class="kpi">
          <div class="kpi-label">警告</div>
          <div class="kpi-value" :class="{ amber: stats.bySeverity.warning > 0 }">{{ stats.bySeverity.warning }}</div>
        </div>
        <div class="kpi">
          <div class="kpi-label">信息</div>
          <div class="kpi-value">{{ stats.bySeverity.info }}</div>
        </div>
        <div class="kpi">
          <div class="kpi-label">已抑制</div>
          <div class="kpi-value gray">{{ stats.suppressed }}</div>
        </div>
        <div class="kpi">
          <div class="kpi-label">24h 事件</div>
          <div class="kpi-value gray">{{ stats.total }}</div>
        </div>
      </div>
    </SectionCard>

    <!-- 维护窗口 -->
    <SectionCard title="维护窗口">
      <template #actions>
        <el-switch
          v-model="maintenance.enabled"
          active-text="已开启"
          inactive-text="已关闭"
          @change="saveMaintenance"
        />
      </template>
      <template v-if="maintenance.enabled">
        <div class="maintenance-row">
          <div class="maintenance-item">
            <span class="maintenance-label">开始时间</span>
            <el-date-picker
              v-model="maintenanceStart"
              type="datetime"
              placeholder="选择开始时间"
              format="YYYY-MM-DD HH:mm"
              value-format="x"
              @change="saveMaintenance"
            />
          </div>
          <div class="maintenance-item">
            <span class="maintenance-label">结束时间</span>
            <el-date-picker
              v-model="maintenanceEnd"
              type="datetime"
              placeholder="选择结束时间"
              format="YYYY-MM-DD HH:mm"
              value-format="x"
              @change="saveMaintenance"
            />
          </div>
          <div class="maintenance-item" style="flex: 1">
            <span class="maintenance-label">原因</span>
            <el-input v-model="maintenance.reason" placeholder="如：版本升级、数据迁移..." @blur="saveMaintenance" />
          </div>
        </div>
        <div class="maintenance-hint">维护窗口期间，所有告警通知将被抑制，告警事件仍会正常记录</div>
      </template>
      <div v-else class="maintenance-hint">开启后所有告警通知将被抑制，告警事件仍会正常记录</div>
    </SectionCard>

    <!-- 告警事件：概览与维护状态之后优先展示，便于快速处置 -->
    <SectionCard title="告警事件" dense>
      <template #actions>
        <div class="event-toolbar">
          <el-radio-group v-model="eventFilter" size="small">
            <el-radio-button value="firing">活跃</el-radio-button>
            <el-radio-button value="resolved">已恢复</el-radio-button>
            <el-radio-button value="">全部</el-radio-button>
          </el-radio-group>
          <el-button size="small" :disabled="!selected.length" @click="batchAck">批量认领 ({{ selected.length }})</el-button>
          <span class="muted event-toolbar-hint">认领后将从活跃列表移除，仍可在“全部”中查看</span>
          <el-button v-if="can('notify:write')" size="small" :loading="testing" @click="testAlert">测试事件</el-button>
        </div>
      </template>
      <!-- 深链筛选回显：从首页研判条跳过来时，让用户一眼看到"被过滤成了哪一条"，
           而不是面对一个空表猜是不是数据没加载出来。 -->
      <FilterChips
        v-if="alertChips.length"
        :chips="alertChips"
        @remove="removeAlertChip"
        @clear="clearAlertChips"
      />
      <div v-if="refreshError" class="alert-refresh-error">{{ refreshError }}</div>
      <el-table
        :data="pagedAlerts"
        stripe
        class="event-table"
        v-loading="alertsLoading"
        style="width: 100%"
        @selection-change="onSelect"
        @row-dblclick="openDetail"
      >
        <template #empty>
          <EmptyState
            :icon="Bell"
            :title="alertChips.length || eventFilter ? '当前筛选下没有告警事件' : '暂无告警事件'"
            :hints="eventHints"
          />
        </template>
        <el-table-column type="selection" width="45" :selectable="selectableAlert" />
        <el-table-column prop="ruleName" label="规则" min-width="140" />
        <el-table-column prop="node" label="节点" min-width="130" />
        <!-- 三列三种视觉语言：级别=色条、处置=StatusPill、抑制=带删除线的灰标签。
             此前三列共用一套 el-tag 色板，「级别=警告」与「处置=已认领」同为橙、
             「级别=信息」「处置=已关闭」「抑制=已抑制」同为灰蓝，扫读时无法区分维度。 -->
        <el-table-column label="级别" width="84">
          <template #default="{ row }">
            <span class="state-bar" :class="sevBar(row.severity)"></span>
            <span class="sev-text">{{ sevLabel(row.severity) }}</span>
          </template>
        </el-table-column>
        <el-table-column label="状态" width="120">
          <template #default="{ row }">
            <StatusPill :tone="ackPillTone(row)" :dot="true" :text="ackStatusLabel(row)" />
          </template>
        </el-table-column>
        <el-table-column label="抑制" width="90">
          <template #default="{ row }">
            <span v-if="row.suppressed" class="sup-tag">已抑制</span>
            <span v-else class="muted">—</span>
          </template>
        </el-table-column>
        <el-table-column prop="message" label="详情" min-width="200" show-overflow-tooltip />
        <el-table-column label="时间" width="160">
          <template #default="{ row }">{{ fmt(row.startsAt || row.endsAt) }}</template>
        </el-table-column>
        <el-table-column label="操作" width="132">
          <template #default="{ row }">
            <el-button v-if="row.state === 'firing'" link size="small" @click="ackEvent(row)">认领</el-button>
            <el-button link size="small" @click="openDetail(row)">详情</el-button>
          </template>
        </el-table-column>
      </el-table>
      <div style="margin-top: 12px; display: flex; justify-content: flex-end">
        <el-pagination
          v-model:current-page="evCurrentPage"
          v-model:page-size="evPageSize"
          :total="filteredAlerts.length"
          :page-sizes="[10, 20, 50, 100]"
          layout="total, sizes, prev, pager, next, jumper"
          background
        />
      </div>
    </SectionCard>

    <!-- 告警规则 -->
    <SectionCard title="告警规则" dense>
      <template #actions>
        <div style="display: flex; gap: 8px; align-items: center">
          <el-button size="small" @click="exportRules">导出</el-button>
          <el-button size="small" @click="fileInput.click()">导入</el-button>
          <input ref="fileInput" type="file" accept="application/json,.json" style="display: none" @change="onFileChange" />
          <el-dropdown split-button type="primary" size="small" popper-class="rule-template-menu" @command="onTemplateCmd">
          <span @click="newRule">新建规则</span>
          <template #dropdown>
            <!-- 模板库有 60 条，必须可滚动 + 带分组与阈值说明：只列名字会让人无从选起，
                 而「该填什么值」正是不知道该怎么配的人最需要的那句话。 -->
            <el-dropdown-menu>
              <el-dropdown-item command="__blank">空白规则</el-dropdown-item>
              <el-dropdown-item v-for="t in templates" :key="t.name" :command="t.name">
                <div class="tpl-item">
                  <div class="tpl-head">
                    <span class="tpl-group">{{ t.templateGroup || '其它' }}</span>
                    <span class="tpl-name">{{ t.name }}</span>
                  </div>
                  <div class="tpl-cond">{{ templateCondition(t) }}</div>
                  <div v-if="t.desc" class="tpl-desc">{{ t.desc }}</div>
                </div>
              </el-dropdown-item>
            </el-dropdown-menu>
          </template>
        </el-dropdown>
        </div>
      </template>
      <el-alert
        v-if="ruleLoadError"
        type="error"
        :closable="false"
        show-icon
        class="rule-load-error"
        :title="'告警规则加载失败：' + ruleLoadError"
      />
      <div class="filter-bar">
        <el-input
          v-model="ruleSearch"
          placeholder="搜索规则名称"
          clearable
          size="small"
          class="w-l"
        />
        <el-select
          v-model="ruleTypeFilter"
          placeholder="全部类型"
          clearable
          size="small"
          class="w-s"
        >
          <el-option label="阈值" value="" />
          <el-option label="主机离线" value="node_offline" />
          <el-option label="服务离线" value="service_down" />
          <el-option label="主从切换" value="role_change" />
          <el-option label="集群损坏" value="cluster_fault" />
          <el-option label="安全事件" value="security_event" />
        </el-select>
        <span class="muted">共 {{ filteredRules.length }} 条</span>
      </div>
      <el-table :data="pagedRules" stripe v-loading="rulesLoading" style="width: 100%">
        <template #empty>
          <EmptyState
            :icon="Bell"
            :title="rules.length ? '没有匹配的告警规则' : '暂无告警规则'"
            :hints="ruleHints"
          />
        </template>
        <el-table-column prop="name" label="名称" min-width="130" />
        <el-table-column label="类型" width="120">
          <template #default="{ row }">
            <el-tag :type="typeTag(row)" size="small" effect="plain">{{ typeLabel(row) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="触发条件" min-width="200">
          <template #default="{ row }">
            <span class="mono">{{ conditionText(row) }}</span>
            <div v-if="row.quietPeriods && row.quietPeriods.length" class="muted" style="font-size: 13px">
              静默时段 {{ row.quietPeriods.length }} 个
            </div>
            <div v-if="row.escalation && row.escalation.enabled" class="muted" style="font-size: 13px">
              升级 {{ row.escalation.afterMinutes }}m{{ row.escalation.toSeverity ? '→' + sevLabel(row.escalation.toSeverity) : '' }}
            </div>
          </template>
        </el-table-column>
        <el-table-column label="级别" width="90">
          <template #default="{ row }">
            <el-tag :type="sevType(row.severity)" size="small" effect="dark">{{ sevLabel(row.severity) }}</el-tag>
          </template>
        </el-table-column>
        <!-- 规则表列宽预算（实测可用宽 1142px）：
             固定 120+90+80+80+80+130 = 580，弹性下限 130+200+110+110 = 550，合计 1130 ≤ 1142。
             之前 通知渠道 150 / 应用范围 140 时下限合计 1210，超出 68px（表内横向滚动条）。
             长表头一律先改短表头（「通知渠道」→「通知」、「应用范围」→「范围」），不加宽列。 -->
        <el-table-column label="通知" min-width="110">
          <template #default="{ row }">
            <template v-if="row.notify && row.notify.length">
              <el-tag v-for="c in row.notify" :key="c" size="small" style="margin: 0 4px 4px 0">{{ channelLabel(c) }}</el-tag>
            </template>
            <span v-else class="muted">仅平台展示</span>
          </template>
        </el-table-column>
        <el-table-column label="范围" min-width="110">
          <template #default="{ row }">
            <template v-if="row.scope === 'specified'">
              <el-tag type="warning" size="small">指定主机</el-tag>
              <span class="scope-count">{{ (row.nodes || []).length }} 台</span>
            </template>
            <el-tag v-else type="success" size="small">全部主机</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="持续" width="80">
          <template #default="{ row }">{{ row.for === '0' ? '立即' : row.for }}</template>
        </el-table-column>
        <el-table-column label="启用" width="80" align="center">
          <template #default="{ row }">
            <el-switch
              v-model="row.enabled"
              @change="toggleRule(row)"
            />
          </template>
        </el-table-column>
        <el-table-column label="静默" width="80" align="center">
          <template #default="{ row }">
            <el-switch
              v-model="row.silenced"
              @change="toggleSilence(row)"
            />
          </template>
        </el-table-column>
        <el-table-column label="操作" width="130">
          <template #default="{ row }">
            <el-button link size="small" @click="edit(row)">编辑</el-button>
            <el-button link type="danger" size="small" @click="del(row.id)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
      <div style="margin-top: 12px; display: flex; justify-content: flex-end">
        <el-pagination
          v-model:current-page="currentPage"
          v-model:page-size="pageSize"
          :total="filteredRules.length"
          :page-sizes="[10, 20, 50, 100]"
          layout="total, sizes, prev, pager, next, jumper"
          background
        />
      </div>
    </SectionCard>

    <!-- 告警规则导入 -->
    <el-dialog v-model="importDialog" title="导入告警规则" width="440px">
      <div v-if="pendingImport">
        <p>文件：<b>{{ pendingImport.name }}</b>，共 {{ pendingImport.list.length }} 条规则。</p>
        <el-radio-group v-model="importMode" style="display: flex; flex-direction: column; gap: 8px; margin-top: 8px">
          <el-radio label="merge">合并导入（按 ID 更新已有规则、新增没有的规则）</el-radio>
          <el-radio label="replace">覆盖导入（先清空当前所有规则再导入）</el-radio>
        </el-radio-group>
        <p class="muted muted-base">
          覆盖导入会删除当前全部规则，请谨慎操作。
        </p>
      </div>
      <template #footer>
        <el-button @click="importDialog = false">取消</el-button>
        <el-button type="primary" @click="confirmImport">开始导入</el-button>
      </template>
    </el-dialog>

    <!-- 高级：抑制与分组（P4）。默认收起——概览之后最有价值的是事件表，
         配置区块不该把事件表压到第三屏以下。 -->
    <SectionCard title="高级设置 · 抑制与分组" dense>
      <template #actions>
        <span class="muted adv-summary">
          {{ grouping.enabled ? '分组已开启' : '分组已关闭' }} · 抑制 {{ inhibits.length }} 条
        </span>
        <el-button size="small" @click="advancedOpen = !advancedOpen">
          {{ advancedOpen ? '收起' : '展开' }}
        </el-button>
      </template>
      <div v-if="advancedOpen">
      <div class="adv-section">
        <div class="adv-title">告警分组</div>
        <div class="adv-row">
          <el-switch v-model="grouping.enabled" active-text="已开启" inactive-text="已关闭" />
          <span class="muted">将相同标签的告警合并为一组，按等待/间隔汇总发送，减少通知风暴</span>
        </div>
        <div class="adv-row" v-if="grouping.enabled">
          <div class="adv-item">
            <span class="adv-label">分组标签</span>
            <el-select v-model="grouping.groupBy" class="group-by-select" multiple collapse-tags tooltip-effect="dark" placeholder="选择分组标签">
              <el-option label="规则名" value="name" />
              <el-option label="规则ID" value="rule" />
              <el-option label="节点" value="node" />
              <el-option label="实例" value="instance" />
              <el-option label="级别" value="severity" />
              <el-option label="指标" value="metric" />
            </el-select>
          </div>
          <div class="adv-item">
            <span class="adv-label">首次等待</span>
            <el-input v-model="grouping.groupWait" placeholder="30s" style="width: 110px" />
          </div>
          <div class="adv-item">
            <span class="adv-label">汇总间隔</span>
            <el-input v-model="grouping.groupInterval" placeholder="5m" style="width: 110px" />
          </div>
          <el-button type="primary" size="small" @click="saveGrouping">保存</el-button>
        </div>

        <!-- D1 风暴收敛：解决「一条通知上百行明细」 -->
        <div class="adv-row" v-if="grouping.enabled">
          <el-switch v-model="grouping.converge" active-text="风暴收敛" inactive-text="风暴收敛" />
          <span class="muted">默认开启：同规则多节点同时告警时合并为「头部告警 + 摘要 + Top N」，避免通知正文被明细淹没</span>
        </div>
        <div class="adv-row" v-if="grouping.enabled && grouping.converge">
          <div class="adv-item">
            <span class="adv-label">收敛维度</span>
            <el-select v-model="grouping.convergeBy" class="group-by-select" multiple collapse-tags tooltip-effect="dark" placeholder="选择收敛维度">
              <el-option label="规则ID" value="rule" />
              <el-option label="级别" value="severity" />
              <el-option label="规则名" value="name" />
              <el-option label="指标" value="metric" />
            </el-select>
          </div>
          <div class="adv-item">
            <span class="adv-label">收敛窗口</span>
            <el-input v-model="grouping.convergeWindow" placeholder="10m" style="width: 110px" />
          </div>
          <div class="adv-item">
            <span class="adv-label">明细条数</span>
            <el-input-number v-model="grouping.headCount" :min="1" :max="20" size="small" controls-position="right" />
          </div>
          <span class="muted">开启后按收敛维度聚合，不再按分组标签细分</span>
        </div>
      </div>

      <el-divider />

      <div class="adv-section">
        <div class="panel-title-row">
          <span class="adv-title" style="margin: 0">抑制规则</span>
          <el-button size="small" type="primary" @click="newInhibit">新增规则</el-button>
        </div>
        <el-table :data="inhibits" stripe v-loading="inhibitsLoading" style="width: 100%; margin-top: 8px">
          <template #empty>
            <EmptyState
              :icon="Bell"
              title="暂无抑制规则"
              :hints="[
                '抑制规则用于「A 触发时压掉 B 的噪音」，例如主机离线时不再逐个报服务离线',
                '点右上角「新增抑制规则」添加，未配置时不会影响告警本身的产生',
              ]"
            />
          </template>
          <el-table-column label="源匹配（触发时）" min-width="200">
            <template #default="{ row }">{{ inhibitText(row.source) }}</template>
          </el-table-column>
          <el-table-column label="目标匹配（被抑制）" min-width="200">
            <template #default="{ row }">{{ inhibitText(row.target) }}</template>
          </el-table-column>
          <el-table-column label="Equal" min-width="150">
            <template #default="{ row }">
              <el-tag v-for="k in (row.equal || [])" :key="k" size="small" style="margin: 0 4px 4px 0">{{ k }}</el-tag>
              <span v-if="!row.equal || !row.equal.length" class="muted">—</span>
            </template>
          </el-table-column>
          <el-table-column label="操作" width="130">
            <template #default="{ row, $index }">
              <el-button link size="small" @click="editInhibit(row, $index)">编辑</el-button>
              <el-button link type="danger" size="small" @click="delInhibit($index)">删除</el-button>
            </template>
          </el-table-column>
        </el-table>
      </div>
      </div>
    </SectionCard>

    <RuleModal v-if="editing" :rule="editing" :groups="groups" :channels="channelOptions" @close="editing = null" @saved="onSaved" />

    <!-- 抑制规则编辑 -->
    <el-dialog v-model="inhibitDialog" :title="inhibitEditIndex < 0 ? '新增抑制规则' : '编辑抑制规则'" width="min(620px, 94vw)">
      <div class="inh-block">
        <div class="inh-title">源匹配（当该告警处于告警中时）</div>
        <div class="inh-row"><span class="inh-label">规则ID 包含</span><el-input v-model="inhibitForm.sourceRule" placeholder="如 host-offline，留空不限" /></div>
        <div class="inh-row"><span class="inh-label">级别</span>
          <el-select v-model="inhibitForm.sourceSeverity" clearable placeholder="不限">
            <el-option label="紧急" value="critical" /><el-option label="警告" value="warning" /><el-option label="信息" value="info" />
          </el-select>
        </div>
        <div class="inh-row"><span class="inh-label">指标正则</span><el-input v-model="inhibitForm.sourceMetricRegex" placeholder="如 .* 或 cpu.*，留空不限" /></div>
      </div>
      <div class="inh-block">
        <div class="inh-title">目标匹配（将被抑制的告警）</div>
        <div class="inh-row"><span class="inh-label">级别</span>
          <el-select v-model="inhibitForm.targetSeverity" clearable placeholder="不限">
            <el-option label="紧急" value="critical" /><el-option label="警告" value="warning" /><el-option label="信息" value="info" />
          </el-select>
        </div>
        <div class="inh-row"><span class="inh-label">指标正则</span><el-input v-model="inhibitForm.targetMetricRegex" placeholder="如 .* 或 mem.*，留空不限" /></div>
      </div>
      <div class="inh-row"><span class="inh-label">Equal 标签</span>
        <el-select v-model="inhibitForm.equal" multiple collapse-tags placeholder="需相同的标签">
          <el-option label="节点" value="node" /><el-option label="实例" value="instance" /><el-option label="级别" value="severity" /><el-option label="规则ID" value="rule" />
        </el-select>
      </div>
      <template #footer>
        <el-button @click="inhibitDialog = false">取消</el-button>
        <el-button type="primary" @click="saveInhibit">保存</el-button>
      </template>
    </el-dialog>

    <!-- 告警事件详情：无遮罩 + 点击外部不关闭 = 「列表 + 详情」可对照形态。
         处置一条告警时最需要的是"一边看详情、一边对照列表里相邻的同规则告警是不是同一次故障"，
         遮罩会把列表盖住、误点即关，未提交的评论也随之中断。 -->
    <el-drawer
      v-model="drawer"
      :title="detail?.ruleName || '告警详情'"
      size="560px"
      :modal="false"
      :close-on-click-modal="false"
      :lock-scroll="false"
      class="alert-detail-drawer"
      @closed="onDrawerClosed"
    >
      <template v-if="detail">
        <div class="ev-field"><span>级别</span><el-tag :type="sevType(detail.severity)" effect="dark">{{ sevLabel(detail.severity) }}</el-tag></div>
        <div class="ev-field"><span>状态</span>{{ stateLabel(detail) }}</div>
        <div class="ev-field">
          <span>节点</span>
          <span>
            <el-link type="primary" @click="gotoNode(detail)">{{ detail.node }}</el-link>
            <span class="muted" v-if="detail.nodeIp"> ({{ detail.nodeIp }})</span>
          </span>
        </div>
        <div class="ev-field">
          <span>处置</span>
          <span>
            <el-tag :type="ackTagType(detail)" size="small" effect="plain">{{ ackStatusLabel(detail) }}</el-tag>
            <span v-if="ackInfo(detail)?.assignee" class="muted"> · 处理人 {{ ackInfo(detail).assignee }}</span>
          </span>
        </div>
        <div class="ev-field" v-if="ackInfo(detail)?.closeReason">
          <span>关闭原因</span><span>{{ ackInfo(detail).closeReason }}</span>
        </div>
        <div class="ev-field"><span>触发条件</span><span class="mono">{{ detail.metric }} {{ detail.operator }} {{ detail.threshold }}</span></div>
        <div class="ev-field"><span>触发值</span><span class="mono">{{ detail.value }}</span></div>
        <div class="ev-field"><span>详情</span><span>{{ detail.message }}</span></div>
        <div class="ev-field"><span>开始</span><span>{{ fmt(detail.startsAt) }}</span></div>
        <div class="ev-field"><span>恢复</span><span>{{ detail.state === 'resolved' ? fmt(detail.endsAt) : '—' }}</span></div>
        <template v-if="detail.metric">
          <div class="ev-chart-title">触发指标近 1 小时趋势</div>
          <TrendChart
            :option="trendOption"
            :loading="trendLoading"
            :error="trendError"
            :height="240"
            empty-text="该时间段没有数据"
            @retry="loadTrend(detail)"
          />
        </template>

        <!-- 资产与影响面：这条告警落在哪条资产上、它刚改过什么、会影响谁 -->
        <div class="ev-chart-title">资产与影响面</div>
        <div v-if="impactLoading" class="muted">正在关联台账…</div>
        <template v-else-if="impact">
          <div v-if="impact.error" class="muted">{{ impact.error }}</div>
          <template v-else>
            <div v-if="(impact.matched || []).length" class="impact-assets">
              <el-tag
                v-for="item in impact.matched"
                :key="item.id"
                class="impact-asset"
                effect="plain"
                @click="openAsset(item)"
              >
                {{ item.name || item.naturalKey }}
                <span class="muted">（{{ item.typeTitle || item.typeKey }}）</span>
              </el-tag>
            </div>
            <div v-else class="muted impact-note">{{ impact.note }}</div>

            <template v-if="(impact.changes || []).length">
              <div class="impact-sub">近期变更（{{ impact.changes.length }} 条）</div>
              <div class="impact-changes">
                <div v-for="(rec, i) in impact.changes" :key="i" class="impact-change">
                  <span class="impact-change-at">{{ fmt(rec.at) }}</span>
                  <span class="mono">{{ rec.naturalKey }}</span>
                  <span class="impact-change-field">{{ rec.field }}</span>
                  <span class="mono">{{ changeText(rec) }}</span>
                  <span class="muted">{{ rec.actor || rec.source }}</span>
                </div>
              </div>
            </template>
            <div v-else-if="(impact.matched || []).length" class="muted impact-note">
              命中资产的近 30 天没有字段变更记录（只有真变化才记录）。
            </div>

            <template v-if="impactRootId">
              <div class="impact-sub">波及范围（点节点可以「以它为中心」重新展开）</div>
              <TopologyGraph :asset-id="impactRootId" :height="320" />
            </template>
          </template>
        </template>

        <div class="ev-chart-title">处置记录</div>
        <div class="collab-timeline">
          <div v-for="(c, i) in ackInfo(detail)?.comments || []" :key="i" class="collab-item">
            <div class="collab-head">
              <span class="collab-user">{{ c.user }}</span>
              <span class="collab-time">{{ fmt(c.time) }}</span>
            </div>
            <div class="collab-text">{{ c.text }}</div>
          </div>
          <div v-if="!(ackInfo(detail)?.comments || []).length" class="muted">暂无处置记录</div>
        </div>
        <div class="collab-input">
          <el-input
            v-model="commentText"
            type="textarea"
            :rows="2"
            maxlength="2000"
            show-word-limit
            placeholder="补充处置说明（如「已扩容」「误报，已调整阈值」）…"
          />
        </div>
        <!-- 处置动作（会改变告警状态）：与只读辅助分区，避免"想看一眼日志却点到关闭告警" -->
        <div class="ev-actions">
          <span class="ev-actions-label">处置</span>
          <el-button
            v-if="detail.state === 'firing' && ackStatus(detail) === 'pending'"
            type="primary"
            size="small"
            @click="ackEvent(detail)"
          >
            认领
          </el-button>
          <el-button
            v-if="detail.state === 'firing' && ackStatus(detail) !== 'closed'"
            size="small"
            @click="assignEvent(detail)"
          >
            指派
          </el-button>
          <el-button
            v-if="detail.state === 'firing' && ackStatus(detail) !== 'closed'"
            size="small"
            @click="closeEvent(detail)"
          >
            关闭告警
          </el-button>
          <el-button v-if="ackStatus(detail) === 'closed'" size="small" @click="reopenEvent(detail)">重新打开</el-button>
          <el-button size="small" :loading="commenting" @click="commentEvent(detail)">评论</el-button>
        </div>
        <!-- 只读辅助：查看日志 / AI 诊断，不改动任何状态 -->
        <div class="ev-actions ev-actions-readonly">
          <span class="ev-actions-label">只读辅助</span>
          <el-button
            size="small"
            :disabled="!detail.node"
            title="查看该节点在告警前后 5 分钟内的集中日志"
            @click="gotoLogs(detail)"
          >
            查看日志
          </el-button>
          <AiDiagnosePanel v-if="detail.id" kind="alert" :id="detail.id" />
          <el-button size="small" @click="drawer = false">返回</el-button>
        </div>
      </template>
    </el-drawer>
  </div>
</template>

<script setup>
import { ref, computed, watch, onMounted, onUnmounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { Plus, Bell, Printer } from '@element-plus/icons-vue'
import PageHeader from './common/PageHeader.vue'
import SectionCard from './common/SectionCard.vue'
import EmptyState from './common/EmptyState.vue'
import StatusPill from './common/StatusPill.vue'
import PartialBar from './common/PartialBar.vue'
import FilterChips from './common/FilterChips.vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { mwTrendOption } from '../charts/echarts'
import http, { getToken } from '../api/http'
import RuleModal from './RuleModal.vue'
import TopologyGraph from './asset/TopologyGraph.vue'
import AiDiagnosePanel from './ai/AiDiagnosePanel.vue'
import TrendChart from './mw/TrendChart.vue'
import useAuth from '../composables/useAuth'
import { filterAlerts } from './alertFilter'
import { printPage } from '../utils/print'

const cssVar = (name) => getComputedStyle(document.documentElement).getPropertyValue(name).trim()
const router = useRouter()
const route = useRoute()
// 「测试事件」会真实写事件并触发通知，可见性复用 notify:write（服务端独立审计拒绝）。
const { can } = useAuth()
const rules = ref([])
// 告警规则前端分页
const currentPage = ref(1)
const pageSize = ref(10)
const ruleSearch = ref('')
const ruleTypeFilter = ref('')
// 空态提示：说清"为什么空"+"下一步做什么"，而不是只写"暂无数据"
const eventHints = computed(() => {
  if (nodeFilter.value || ruleFilter.value) {
    return [
      '已按上方的节点 / 规则筛选，当前范围内没有匹配的事件',
      '点筛选条右侧的「清空筛选」可回到全部告警',
    ]
  }
  if (eventFilter.value === 'firing') {
    return ['当前没有活跃告警，说明所有已启用规则都未触发', '切到「全部」可查看历史事件（含已恢复）']
  }
  if (eventFilter.value === 'resolved') {
    return ['还没有恢复记录', '活跃告警恢复后会出现在这里']
  }
  return [
    '还没有产生过告警事件：确认至少有一条处于启用状态的告警规则',
    '规则依赖的指标需要有数据，可在「指标浏览」确认对应指标有上报',
  ]
})

const ruleHints = computed(() => {
  if (!rules.value.length) {
    return [
      '点右上角「新建规则」，或从按钮右侧下拉的模板库里挑一条（含阈值依据）',
      '规则需要处于启用状态才会参与评估',
    ]
  }
  return [
    `共 ${rules.value.length} 条规则，当前筛选条件下没有匹配项`,
    '清空搜索关键词，或把类型切回「阈值」',
  ]
})

const filteredRules = computed(() => {
  const kw = ruleSearch.value.trim().toLowerCase()
  const t = ruleTypeFilter.value
  return rules.value.filter((r) => {
    if (t && (r.type || '') !== t) return false
    if (kw && !((r.name || '').toLowerCase().includes(kw))) return false
    return true
  })
})
const pagedRules = computed(() => {
  const start = (currentPage.value - 1) * pageSize.value
  return filteredRules.value.slice(start, start + pageSize.value)
})
watch([rules, filteredRules], () => {
  const max = Math.max(1, Math.ceil(filteredRules.value.length / pageSize.value))
  if (currentPage.value > max) currentPage.value = max
})
watch([ruleSearch, ruleTypeFilter, pageSize], () => {
  currentPage.value = 1
})
const alerts = ref([])
const groups = ref([])
const templates = ref([])
const acks = ref({})
const editing = ref(null)
const eventFilter = ref('firing')
// 首页研判条「查看详情」会带 node / rule 深链过来，这里接住并落成可撤销的筛选条。
// 此前深链只支持 newLogRule（日志→建规则），node 被当成模板参数用掉就丢了，
// 用户到了告警中心还得自己在几百条里再找一遍那条研判告警。
const nodeFilter = ref('')
const ruleFilter = ref('')

function applyAlertQuery() {
  // 日志→建规则的深链里 node 是模板参数（见 applyLogRulePrefill），不能当筛选
  if (route.query.newLogRule) return
  nodeFilter.value = route.query.node ? String(route.query.node) : ''
  ruleFilter.value = route.query.rule ? String(route.query.rule) : ''
}
// 已经在告警中心时再次点研判条（hash 路由不重挂载），靠 watch 重新接住参数
watch(
  () => [route.query.node, route.query.rule, route.query.newLogRule],
  () => {
    if (route.path === '/alerts') applyAlertQuery()
  }
)

const alertChips = computed(() => {
  const chips = []
  if (nodeFilter.value) chips.push({ key: 'node', label: '节点', value: nodeFilter.value })
  if (ruleFilter.value) chips.push({ key: 'rule', label: '规则', value: ruleFilter.value })
  return chips
})
function removeAlertChip(key) {
  if (key === 'node') nodeFilter.value = ''
  if (key === 'rule') ruleFilter.value = ''
  syncAlertQuery()
}
function clearAlertChips() {
  nodeFilter.value = ''
  ruleFilter.value = ''
  syncAlertQuery()
}
// 清掉筛选时同步清 URL：否则刷新页面又被参数带回筛选态，用户会以为"数据丢了"
function syncAlertQuery() {
  if (!route.query.node && !route.query.rule) return
  const rest = { ...route.query }
  delete rest.node
  delete rest.rule
  router.replace({ path: '/alerts', query: rest })
}
const testing = ref(false)
const selected = ref([])
const drawer = ref(false)
const detail = ref(null)
// 评论草稿 / 提交中：此前这两个 ref 从未声明（模板 v-model 与 commentEvent 都在用），
// 抽屉里「评论」读 commentText.value 会直接抛错——一并补上并保证跨告警清空。
const commentText = ref('')
const commenting = ref(false)
const maintenance = ref({ enabled: false, start: 0, end: 0, reason: '' })
// 全部渠道及其展示名；enabled 由通知配置决定，未启用的渠道在新建规则时置灰。
const CHANNEL_META = [
  { value: 'email', label: '邮件' },
  { value: 'webhook', label: 'Webhook' },
  { value: 'dingtalk', label: '钉钉' },
  { value: 'feishu', label: '飞书' },
  { value: 'wecom', label: '企业微信' },
]
const channelOptions = ref(CHANNEL_META.map((c) => ({ ...c, enabled: true })))
let timer = null
let loadGeneration = 0
const refreshError = ref('')
const alertsLoading = ref(false)
const rulesLoading = ref(false)
const inhibitsLoading = ref(false)
// 部分失败清单：替代各段 `catch { /* ignore */ }` 的静默吞错，用 PartialBar 明示"哪几块没拿到"。
const partialFailed = ref([])
// 高级设置（抑制与分组）默认收起：概览之后最有价值的是事件表，
// 配置区块不该把事件表压到第三屏以下。
const advancedOpen = ref(false)
const statsLoading = ref(false)
const statsError = ref('')
const ruleLoadError = ref('')
let maintenanceTimer = null

// P4：统计看板 / 抑制规则 / 分组配置
const stats = ref({ firing: 0, suppressed: 0, total: 0, bySeverity: { critical: 0, warning: 0, info: 0 } })
const inhibits = ref([])
const grouping = ref({
  enabled: false,
  groupBy: ['name'],
  groupWait: '30s',
  groupInterval: '5m',
  // D1 风暴收敛（默认开启：开启分组即默认收敛；关掉开关即显式写入 converge:false）
  converge: true,
  convergeBy: ['rule', 'severity'],
  convergeWindow: '10m',
  headCount: 5,
})
const inhibitDialog = ref(false)
const inhibitEditIndex = ref(-1)
const inhibitForm = ref(emptyInhibitForm())
function emptyInhibitForm() {
  return { sourceRule: '', sourceSeverity: '', sourceMetricRegex: '', targetSeverity: '', targetMetricRegex: '', equal: [] }
}

// 值班交接、事后复盘最常用的出口就是"把当前告警打出来"
function printList() {
  const s = stats.value
  printPage({
    title: '告警中心',
    meta: [
      `活跃 ${s.firing} · 已抑制 ${s.suppressed} · 累计 ${s.total}`,
      `紧急 ${s.bySeverity.critical} · 警告 ${s.bySeverity.warning} · 提示 ${s.bySeverity.info}`,
    ],
  })
}

const activeCount = computed(() => alerts.value.filter((a) => a.state === 'firing').length)
const criticalCount = computed(() => alerts.value.filter((a) => a.state === 'firing' && a.severity === 'critical').length)
const warningCount = computed(() => alerts.value.filter((a) => a.state === 'firing' && a.severity === 'warning').length)

const filteredAlerts = computed(() =>
  filterAlerts({
    alerts: alerts.value,
    state: eventFilter.value,
    node: nodeFilter.value,
    rule: ruleFilter.value,
  })
)

// 告警事件前端分页
const evCurrentPage = ref(1)
const evPageSize = ref(10)
const pagedAlerts = computed(() => {
  const start = (evCurrentPage.value - 1) * evPageSize.value
  return filteredAlerts.value.slice(start, start + evPageSize.value)
})
watch(filteredAlerts, () => {
  const max = Math.max(1, Math.ceil(filteredAlerts.value.length / evPageSize.value))
  if (evCurrentPage.value > max) evCurrentPage.value = max
})
watch([evPageSize, eventFilter], () => {
  evCurrentPage.value = 1
  loadAlerts()
})

function ackKey(e) {
  return `${e.ruleName}|${e.node}|${e.instance || ''}|${e.startsAt || 0}`
}
// 处置记录（含状态 / 处理人 / 关闭原因 / 评论）
function ackInfo(e) {
  return e ? acks.value[ackKey(e)] || null : null
}
// 处置状态；旧记录没有 status 字段，一律视为「已认领」——与后端 EffectiveStatus 同口径。
function ackStatus(e) {
  if (!e) return 'pending'
  if (e.state && e.state !== 'firing') return 'resolved'
  const info = ackInfo(e)
  return info ? info.status || 'ack' : 'pending'
}
function ackStatusLabel(e) {
  return { pending: '待处理', ack: '已认领', closed: '已关闭', resolved: '已恢复' }[ackStatus(e)] || '告警中'
}
function ackTagType(e) {
  return { pending: 'danger', ack: 'warning', closed: 'info', resolved: 'success' }[ackStatus(e)] || 'danger'
}
// 处置状态 → StatusPill 语气（列表「状态」列专用；与 ackTagType 同语义，走统一药丸样式）
function ackPillTone(e) {
  return { pending: 'danger', ack: 'warn', closed: 'muted', resolved: 'ok' }[ackStatus(e)] || 'muted'
}
function sevType(s) {
  return { critical: 'danger', warning: 'warning', info: 'info' }[s] || 'info'
}
function sevLabel(s) {
  return { critical: '紧急', warning: '警告', info: '信息' }[s] || s
}
// 级别色条：与 StatusPill 同一套语义（danger / warn / off）
function sevBar(s) {
  return { critical: 'danger', warning: 'warn' }[s] || 'off'
}
function stateLabel(e) {
  if (e.state === 'firing') return ackStatusLabel(e)
  return { firing: '告警中', resolved: '已恢复', pending: '待触发' }[e.state] || e.state
}
function channelLabel(v) {
  return (CHANNEL_META.find((c) => c.value === v) || {}).label || v
}
function fmt(ts) {
  if (!ts) return '-'
  return new Date(ts).toLocaleString('zh-CN', { hour12: false })
}

const maintenanceStart = computed({
  get: () => maintenance.value.start ? new Date(maintenance.value.start) : null,
  set: (v) => { maintenance.value.start = v ? (typeof v === 'number' ? v : v.getTime()) : 0 },
})
const maintenanceEnd = computed({
  get: () => maintenance.value.end ? new Date(maintenance.value.end) : null,
  set: (v) => { maintenance.value.end = v ? (typeof v === 'number' ? v : v.getTime()) : 0 },
})

async function loadMaintenance() {
  try {
    const mw = await http.get('/api/v1/maintenance')
    if (mw) maintenance.value = mw
  } catch (e) { /* ignore */ }
}

function saveMaintenance() {
  // 日期/原因修改会连续触发，防抖合并为一次保存
  clearTimeout(maintenanceTimer)
  maintenanceTimer = setTimeout(async () => {
    try {
      await http.put('/api/v1/maintenance', {
        enabled: maintenance.value.enabled,
        start: maintenance.value.start,
        end: maintenance.value.end,
        reason: maintenance.value.reason,
      })
      ElMessage.success('维护窗口已保存（热生效）')
    } catch (e) {
      ElMessage.error('保存维护窗口失败')
    }
  }, 400)
}

async function loadAlerts() {
  const generation = ++loadGeneration
  alertsLoading.value = true
  try {
    const state = eventFilter.value === 'firing' ? 'active' : eventFilter.value
    const suffix = state ? `?state=${encodeURIComponent(state)}` : ''
    const data = await http.get('/api/v1/alerts' + suffix)
    if (generation !== loadGeneration) return
    alerts.value = data.alerts || []
    refreshError.value = ''
    // 抽屉打开时不主动关闭：30s 轮询把正在读的详情关掉，是处置主路径上最恼人的一件事。
    // 只用最新数据刷新抽屉的绑定（同一条告警的处置状态可能已变），找不到也保持打开。
    if (detail.value) {
      const fresh = alerts.value.find(
        (item) => item.ruleName === detail.value.ruleName
          && item.node === detail.value.node
          && (item.instance || '') === (detail.value.instance || '')
          && (item.startsAt || 0) === (detail.value.startsAt || 0),
      )
      if (fresh) detail.value = fresh
    }
  } catch (e) {
    if (generation !== loadGeneration) return
    alerts.value = []
    refreshError.value = '告警数据刷新失败：' + (e.message || '请重新登录后重试')
    // 刷新失败同样不关抽屉：否则一次网络抖动就把正在处置的详情清掉。
  } finally {
    if (generation === loadGeneration) alertsLoading.value = false
  }
}

async function load() {
  // 每一段单独 try：任何一块失败都不该让整页变成空白，
  // 但也不能像以前那样静默吞掉——统一记进 partialFailed，用 PartialBar 明示。
  const failed = []
  rulesLoading.value = true
  try {
    rules.value = (await http.get('/api/v1/rules')).rules || []
    ruleLoadError.value = ''
  } catch (e) {
    ruleLoadError.value = e.message || '网络异常'
  } finally {
    rulesLoading.value = false
  }
  await loadAlerts()
  try {
    groups.value = (await http.get('/api/v1/groups')).groups || []
  } catch (e) { failed.push({ label: '节点分组' }) }
  try {
    templates.value = (await http.get('/api/v1/rules/templates')).templates || []
  } catch (e) { failed.push({ label: '规则模板' }) }
  try {
    acks.value = (await http.get('/api/v1/alerts/acks')).acks || {}
  } catch (e) { failed.push({ label: '处置记录' }) }
  try {
    statsLoading.value = true
    stats.value = await http.get('/api/v1/alerts/stats')
    statsError.value = ''
  } catch (e) {
    statsError.value = '告警概览加载失败：' + (e.message || '网络异常')
  } finally {
    statsLoading.value = false
  }
  inhibitsLoading.value = true
  try {
    inhibits.value = (await http.get('/api/v1/inhibit')).rules || []
  } catch (e) { failed.push({ label: '抑制规则' }) } finally {
    inhibitsLoading.value = false
  }
  try {
    const g = await http.get('/api/v1/grouping')
    if (g && typeof g === 'object') grouping.value = g
  } catch (e) { failed.push({ label: '分组配置' }) }
  try {
    const cfg = await http.get('/api/v1/notify')
    const enabled = {
      email: cfg.email && cfg.email.enabled,
      webhook: cfg.webhook && cfg.webhook.enabled,
      dingtalk: cfg.dingtalk && cfg.dingtalk.enabled,
      feishu: cfg.feishu && cfg.feishu.enabled,
      wecom: cfg.wecom && cfg.wecom.enabled,
    }
    channelOptions.value = CHANNEL_META.map((c) => ({ ...c, enabled: !!enabled[c.value] }))
  } catch (e) { failed.push({ label: '通知渠道' }) }
  partialFailed.value = failed
}

async function testAlert() {
  testing.value = true
  try {
    const r = await http.post('/api/v1/alerts/test')
    if (r.ok) {
      ElMessage.success('已发送测试告警事件，请查看事件列表或通知渠道')
    } else {
      ElMessageBox.alert(r.error || '测试失败', '触发失败', { type: 'error', confirmButtonText: '关闭' })
    }
    load()
  } catch (e) {
    ElMessageBox.alert(e.message || '请求失败', '触发失败', { type: 'error', confirmButtonText: '关闭' })
  } finally {
    testing.value = false
  }
}

function newRule() {
  // 设为空对象（truthy）以触发 RuleModal 渲染（v-if="editing"）
  editing.value = {}
}

// 告警规则导出 / 导入
const fileInput = ref(null)
const importDialog = ref(false)
const importMode = ref('merge') // merge | replace
const pendingImport = ref(null) // { list, name }

async function exportRules() {
  try {
    const token = getToken()
    const headers = {}
    if (token) headers['Authorization'] = 'Bearer ' + token
    const resp = await fetch('/api/v1/rules/export', { headers })
    if (!resp.ok) {
      ElMessage.error('导出失败（HTTP ' + resp.status + '）')
      return
    }
    const blob = await resp.blob()
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    const disp = resp.headers.get('Content-Disposition')
    let fname = 'alert-rules.json'
    if (disp && disp.includes('filename=')) {
      fname = decodeURIComponent(disp.split('filename=')[1].replace(/^"|"$/g, ''))
    }
    a.href = url
    a.download = fname
    document.body.appendChild(a)
    a.click()
    document.body.removeChild(a)
    URL.revokeObjectURL(url)
    ElMessage.success('已导出告警规则')
  } catch (e) {
    ElMessage.error('导出失败：' + (e.message || e))
  }
}

async function onFileChange(e) {
  const input = e.target
  const file = input.files && input.files[0]
  if (!file) return
  try {
    const text = await file.text()
    const parsed = JSON.parse(text)
    const list = Array.isArray(parsed) ? parsed : parsed.rules
    if (!Array.isArray(list) || list.length === 0) {
      ElMessage.error('文件格式不正确（缺少 rules 数组或为空）')
      return
    }
    pendingImport.value = { list, name: file.name }
    importMode.value = 'merge'
    importDialog.value = true
  } catch (err) {
    ElMessage.error('解析失败：' + (err.message || err))
  } finally {
    input.value = '' // 允许重复选择同一文件
  }
}

async function confirmImport() {
  if (!pendingImport.value) return
  // 覆盖导入会删除现有全部规则，二次强确认
  if (importMode.value === 'replace') {
    try {
      await ElMessageBox.confirm(
        `即将清空当前全部 ${rules.value.length} 条规则并导入「${pendingImport.value.name}」中的 ${pendingImport.value.list.length} 条规则，此操作不可撤销。是否继续？`,
        '覆盖导入确认',
        { type: 'error', confirmButtonText: '确认覆盖导入', cancelButtonText: '取消' }
      )
    } catch { return }
  }
  try {
    const res = await http.post('/api/v1/rules/import', {
      rules: pendingImport.value.list,
      replace: importMode.value === 'replace',
    })
    ElMessage.success('导入成功：新增 ' + (res.created || 0) + ' 条，更新 ' + (res.updated || 0) + ' 条')
    importDialog.value = false
    pendingImport.value = null
    await load()
  } catch (err) {
    ElMessage.error('导入失败：' + (err.message || err))
  }
}

function onTemplateCmd(cmd) {
  if (cmd === '__blank') {
    newRule()
    return
  }
  const t = templates.value.find((x) => x.name === cmd)
  if (t) editing.value = { ...t }
}

// templateCondition 把模板的触发条件写成一行中文，供选择器直接展示。
//
// 为什么值得单独算一次：模板列表如果只给规则名，「CPU 使用率过高」到底是 80 还是 95、
// 持续多久、用哪个指标，用户只能点进去才知道——而点进去再退出来这条路径，
// 正是"不知道该怎么配"的人放弃的地方。
function templateCondition(t) {
  if (!t) return ''
  if (!t.type) {
    const unit = ''
    return `指标 ${t.metric} ${t.operator} ${t.threshold}${unit}，持续 ${t.for || '立即'}`
  }
  const typeNames = {
    node_offline: '主机离线',
    service_down: '服务离线',
    role_change: '主从切换',
    cluster_fault: '集群损坏',
    security_event: '安全事件',
  }
  const parts = [typeNames[t.type] || t.type]
  if (t.service) parts.push(t.service)
  if (t.topology) parts.push(t.topology)
  if (t.category) parts.push(t.category)
  if (t.for) parts.push(`持续 ${t.for}`)
  return parts.join(' · ')
}
function edit(rule) {
  editing.value = { ...rule }
}
async function del(id) {
  try {
    await ElMessageBox.confirm('确认删除该规则？', '提示', { type: 'warning' })
    await http.del('/api/v1/rules/' + id)
    ElMessage.success('已删除')
    load()
  } catch (e) { /* 取消 */ }
}
function onSaved() {
  editing.value = null
  load()
}
async function toggleRule(rule) {
  try {
    await http.post('/api/v1/rules/' + rule.id + '/toggle')
    ElMessage.success(rule.enabled ? '已启用' : '已停用')
    load()
  } catch (e) {
    rule.enabled = !rule.enabled
    ElMessage.error('操作失败')
  }
}
// 规则单次静默开关
async function toggleSilence(rule) {
  try {
    await http.post('/api/v1/rules/' + rule.id + '/toggle-silence', { silenced: rule.silenced })
    ElMessage.success(rule.silenced ? '已静默' : '已取消静默')
    load()
  } catch (e) {
    rule.silenced = !rule.silenced
    ElMessage.error('操作失败')
  }
}

// ===== 规则类型/触发条件的展示辅助 =====
const TYPE_LABELS = {
  '': '阈值',
  node_offline: '主机离线',
  service_down: '服务离线',
  role_change: '主从切换',
  cluster_fault: '集群损坏',
  security_event: '安全事件',}
const SERVICE_LABELS = {
  mysql: 'MySQL', postgres: 'PostgreSQL', redis: 'Redis', nginx: 'Nginx',
  kafka: 'Kafka', rocketmq: 'RocketMQ', docker: 'Docker', k8s: 'Kubernetes',
}
function typeLabel(row) {
  return TYPE_LABELS[row.type || ''] || '阈值'
}
function typeTag(row) {
  switch (row.type || '') {
    case 'node_offline':
    case 'service_down':
    case 'cluster_fault':
      return 'danger'
    case 'role_change':
      return 'warning'
    default:
      return 'info'
  }
}
function conditionText(row) {
  switch (row.type || '') {
    case 'node_offline':
      return '主机心跳离线 持续 ' + (row.for || '5m')
    case 'service_down':
      return (
        (SERVICE_LABELS[row.service] || row.service) +
        ' 离线：*_instance_up ' +
        (row.operator || '<=') +
        ' ' +
        (row.threshold != null ? row.threshold : 0.5) +
        ' 持续 ' + (row.for || '3m')
      )
    case 'role_change':
      return (
        (SERVICE_LABELS[row.service] || row.service) +
        ' 主从切换（' + (row.topology || 'cluster') + '）：检测到角色变化'
      )
    case 'cluster_fault':
      return (
        (SERVICE_LABELS[row.service] || row.service) +
        ' 集群状态损坏（' + (row.topology || 'cluster') + '）无主/多主，持续 ' + (row.for || '2m')
      )
    case 'security_event':
      return '安全事件：' + (row.category || '全部类别') + '，收到事件即触发'
    default:
      return row.metric + ' ' + (row.operator || '') + ' ' + (row.threshold != null ? row.threshold : '')
  }
}

function onSelect(rows) {
	selected.value = rows
}
function selectableAlert(row) {
  return row.state === 'firing'
}
function openDetail(row) {
  detail.value = row
  drawer.value = true
  commentText.value = '' // 换一条告警时清掉上一条的评论草稿
  loadTrend(row)
  loadImpact(row)
}

// ---- 资产与影响面（全景表 1-05）----
//
// 告警事件只带指标标签（node / instance），台账资产是另一套身份。这里把两者对上，
// 并给出排障真正需要的三件事：命中资产、该资产近期变更、波及范围。
// **不放进列表**：列表是高频轮询的，逐条去台账解析会把列表变成 N 次查询。
const impact = ref(null)
const impactLoading = ref(false)

async function loadImpact(row) {
  impact.value = null
  const params = new URLSearchParams()
  if (row.node) params.set('node', row.node)
  if (row.instance) params.set('instance', row.instance)
  if (!params.toString()) return
  impactLoading.value = true
  try {
    impact.value = await http.get('/api/v1/alerts/impact?' + params.toString())
  } catch (e) {
    // 影响面是补充信息：拿不到不该让告警详情看起来像出错了，只在区块内提示。
    impact.value = { error: e.message || '加载资产影响面失败' }
  } finally {
    impactLoading.value = false
  }
}

// impactRootId 是波及范围图的中心资产 id（服务端已把中心标出来）。
const impactRootId = computed(() => {
  const nodes = (impact.value && impact.value.topology && impact.value.topology.nodes) || []
  const root = nodes.find((n) => n.root)
  return root ? root.id : null
})

// openAsset 跳到台账并直接打开该资产的详情（台账页支持 ?id= 深链）。
function openAsset(item) {
  router.push({ path: '/assets', query: { id: String(item.id) } })
}

// changeText 把一条字段级变更渲染成「旧值 → 新值」：只给字段名看不出改成了什么。
function changeText(rec) {
  const old = rec.old === '' || rec.old == null ? '（空）' : rec.old
  const next = rec.new === '' || rec.new == null ? '（空）' : rec.new
  return old + ' → ' + next
}
function gotoNode(row) {
  router.push({ path: '/hosts', query: { node: row.node } })
}
// gotoLogs 跳到「该节点该时间段」的集中日志（C2）：把告警与原始日志串起来。
// 时间窗以告警开始时间为锚点、前后各留 5 分钟——这是「触发前后到底发生了什么」最常用的一段；
//   - 已恢复：窗口 = [开始-5m, 恢复+5m]
//   - 仍在告警：窗口 = [开始-5m, 现在]（想看更早/更晚可在日志页改时间范围）
function gotoLogs(row) {
  const start = row.startsAt || Date.now()
  const from = start - 5 * 60 * 1000
  const to = row.state === 'resolved' && row.endsAt ? row.endsAt + 5 * 60 * 1000 : Date.now()
  router.push({ path: '/logs', query: { node: row.node, from: String(from), to: String(to) } })
}
// ---- D4 告警协作处置：认领 / 指派 / 关闭 / 重新打开 / 评论 ----
// 处置只影响「谁在处理」，不改变监控条件的真实 firing 状态。
function collabPayload(row, extra = {}) {
  return {
    rule: row.ruleName,
    host: row.node,
    instance: row.instance || '',
    startsAt: row.startsAt || 0,
    ...extra,
  }
}

// 处置后只刷新数据、不关闭抽屉：抽屉的处置区读的是 acks，因此会自动更新。
async function runCollab(path, row, extra, okText) {
  try {
    await http.post(path, collabPayload(row, extra))
    ElMessage.success(okText)
    await load()
    return true
  } catch (e) {
    ElMessage.error(e.message || '操作失败')
    return false
  }
}

async function ackEvent(row) {
  await runCollab('/api/v1/alerts/ack', row, {}, '已认领')
}

async function assignEvent(row) {
  let value
  try {
    const res = await ElMessageBox.prompt('指派给（填写用户名）', '指派告警', {
      inputPlaceholder: '如 ops1',
      inputValidator: (v) => (v && v.trim() ? true : '请填写用户名'),
    })
    value = res.value
  } catch {
    return
  }
  await runCollab('/api/v1/alerts/ack', row, { assignee: value.trim() }, '已指派给 ' + value.trim())
}

async function closeEvent(row) {
  let reason
  try {
    const res = await ElMessageBox.prompt('关闭原因（便于日后回溯）', '关闭告警', {
      inputPlaceholder: '如 误报 / 已扩容 / 已重启服务',
    })
    reason = res.value || ''
  } catch {
    return
  }
  await runCollab('/api/v1/alerts/close', row, { reason }, '已关闭')
}

async function reopenEvent(row) {
  await runCollab('/api/v1/alerts/reopen', row, {}, '已重新打开，回到待处理')
}

async function commentEvent(row) {
  const text = commentText.value.trim()
  if (!text) {
    ElMessage.warning('请输入评论内容')
    return
  }
  commenting.value = true
  try {
    const ok = await runCollab('/api/v1/alerts/comment', row, { comment: text }, '已提交')
    if (ok) commentText.value = ''
  } finally {
    commenting.value = false
  }
}
async function batchAck() {
  if (!selected.value.length) return
  try {
    await Promise.all(selected.value.map((r) =>
      http.post('/api/v1/alerts/ack', { rule: r.ruleName, host: r.node, instance: r.instance || '', startsAt: r.startsAt }),
    ))
    ElMessage.success(`已认领 ${selected.value.length} 条`)
    await load()
  } catch (e) {
    ElMessage.error('批量确认失败')
  }
}

// 触发指标趋势：三态（加载 / 错误带重试 / 空）交给 TrendChart 托管。
//
// 数据点解析必须走 series[].points[].timestamp/value —— 后端
// /api/v1/query/range 返回 {"series":[{labels, points:[{timestamp,value}]}]}。
// 此前按「series 即数据点数组」解析（p.time / p.value），两者都不存在，
// 结果恒为 [undefined, undefined]：趋势图一直画不出真实数据，还被静默 catch 掩盖。
const trendOption = ref(null)
const trendLoading = ref(false)
const trendError = ref('')
let trendPoints = []

function buildTrendOption(row, points) {
  const opt = mwTrendOption([{ name: row.metric, data: points }], { yMin: null })
  // 阈值线：详情里最该一眼看到的是"触发值越过阈值多少"，少了这条线就得自己心算。
  if (row.threshold != null && opt.series && opt.series.length) {
    opt.series[0].markLine = {
      silent: true,
      symbol: 'none',
      data: [{ yAxis: row.threshold }],
      lineStyle: { color: cssVar('--warn'), type: 'dashed' },
      label: { formatter: '阈值 ' + row.threshold, color: cssVar('--warn') },
    }
  }
  return opt
}

async function loadTrend(row) {
  if (!row || !row.metric) return
  trendOption.value = null
  trendError.value = ''
  trendPoints = []
  const end = Date.now()
  const start = end - 3600 * 1000
  let url = `/api/v1/query/range?node=${encodeURIComponent(row.node)}&metric=${encodeURIComponent(row.metric)}&start=${start}&end=${end}&step=60000`
  if (row.instance) url += `&labels.instance=${encodeURIComponent(row.instance)}`
  trendLoading.value = true
  try {
    const data = await http.get(url)
    trendPoints = (data.series || []).flatMap((s) => (s.points || []).map((p) => [p.timestamp, p.value]))
    trendOption.value = buildTrendOption(row, trendPoints)
  } catch (e) {
    trendError.value = '趋势数据加载失败：' + (e.message || '网络异常')
  } finally {
    trendLoading.value = false
  }
}

function onDrawerClosed() {
  detail.value = null
  commentText.value = '' // 评论草稿不跨告警残留
  trendOption.value = null
  trendPoints = []
}

// ---- P4：分组配置 ----
async function saveGrouping() {
  try {
    await http.put('/api/v1/grouping', {
      enabled: grouping.value.enabled,
      groupBy: grouping.value.groupBy || ['name'],
      groupWait: grouping.value.groupWait || '30s',
      groupInterval: grouping.value.groupInterval || '5m',
      converge: !!grouping.value.converge,
      convergeBy: grouping.value.convergeBy?.length ? grouping.value.convergeBy : ['rule', 'severity'],
      convergeWindow: grouping.value.convergeWindow || '10m',
      headCount: Number(grouping.value.headCount) || 5,
    })
    ElMessage.success('分组配置已保存（热生效）')
  } catch (e) {
    ElMessage.error('保存分组配置失败')
  }
}

// ---- P4：抑制规则 ----
function inhibitText(ms) {
  if (!ms) return '—'
  const parts = []
  if (ms.match) for (const [k, v] of Object.entries(ms.match)) parts.push(`${k}=${v}`)
  if (ms.matchRegex) for (const [k, v] of Object.entries(ms.matchRegex)) parts.push(`${k}~${v}`)
  return parts.length ? parts.join('  &  ') : '—'
}

function buildInhibitRule() {
  const f = inhibitForm.value
  const src = { match: {}, matchRegex: {} }
  if (f.sourceSeverity) src.match.severity = f.sourceSeverity
  if (f.sourceRule) src.matchRegex.rule = f.sourceRule
  if (f.sourceMetricRegex) src.matchRegex.metric = f.sourceMetricRegex
  const tgt = { match: {}, matchRegex: {} }
  if (f.targetSeverity) tgt.match.severity = f.targetSeverity
  if (f.targetMetricRegex) tgt.matchRegex.metric = f.targetMetricRegex
  return { source: src, target: tgt, equal: f.equal || [] }
}

function formFromRule(r) {
  const f = emptyInhibitForm()
  if (r.source) {
    f.sourceSeverity = r.source.match?.severity || ''
    f.sourceRule = r.source.matchRegex?.rule || r.source.match?.rule || ''
    f.sourceMetricRegex = r.source.matchRegex?.metric || ''
  }
  if (r.target) {
    f.targetSeverity = r.target.match?.severity || ''
    f.targetMetricRegex = r.target.matchRegex?.metric || ''
  }
  f.equal = r.equal || []
  return f
}

function newInhibit() {
  inhibitEditIndex.value = -1
  inhibitForm.value = emptyInhibitForm()
  inhibitDialog.value = true
}
function editInhibit(row, idx) {
  inhibitEditIndex.value = idx
  inhibitForm.value = formFromRule(row)
  inhibitDialog.value = true
}
async function persistInhibits() {
  try {
    await http.put('/api/v1/inhibit', inhibits.value)
    ElMessage.success('抑制规则已保存（热生效）')
  } catch (e) {
    ElMessage.error('保存抑制规则失败')
  }
}
function saveInhibit() {
  const rule = buildInhibitRule()
  ;['source', 'target'].forEach((k) => {
    const ms = rule[k]
    if (Object.keys(ms.match).length === 0) delete ms.match
    if (Object.keys(ms.matchRegex).length === 0) delete ms.matchRegex
  })
  const list = [...inhibits.value]
  if (inhibitEditIndex.value >= 0) list[inhibitEditIndex.value] = rule
  else list.push(rule)
  inhibits.value = list
  inhibitDialog.value = false
  persistInhibits()
}
async function delInhibit(idx) {
  try {
    await ElMessageBox.confirm('确认删除该抑制规则？', '提示', { type: 'warning' })
    inhibits.value.splice(idx, 1)
    persistInhibits()
  } catch (e) { /* 取消 */ }
}

// applyLogRulePrefill 处理「从日志检索页建规则」的深链（全景表 9-05）。
//
// 模板由服务端生成（`/api/v1/logs/rule-template`），这里不自己拼指标名：
// 拼错的症状是"规则配好了却永远没有数据"，而没有任何一处会报错。
async function applyLogRulePrefill() {
  const raw = String(route.query.newLogRule || '')
  const [source, pattern] = raw.split('|')
  if (!source || !pattern) return
  try {
    const params = new URLSearchParams({ source, pattern })
    if (route.query.node) params.set('node', String(route.query.node))
    editing.value = await http.get('/api/v1/logs/rule-template?' + params.toString())
    // 用掉即清：否则刷新页面会再次弹出同一个表单
    const rest = { ...route.query }
    delete rest.newLogRule
    delete rest.node
    router.replace({ path: '/alerts', query: rest })
  } catch (e) {
    ElMessage.error(e.message || '生成规则模板失败')
  }
}

// 每 30s 自动刷新告警列表，确保实时性
onMounted(() => {
  applyAlertQuery()
  load()
  loadMaintenance()
  applyLogRulePrefill()
  timer = setInterval(load, 30000)
  // 换肤后重绘趋势图（ECharts 颜色来自 CSS 变量）
  window.addEventListener('nebula:theme-changed', onThemeChanged)
})

function onThemeChanged() {
  // 换肤后重建 option（颜色来自 CSS 变量），TrendChart 会据此重绘
  if (detail.value && trendPoints.length) {
    trendOption.value = buildTrendOption(detail.value, trendPoints)
  }
}

onUnmounted(() => {
  if (timer) clearInterval(timer)
  if (maintenanceTimer) clearTimeout(maintenanceTimer)
  window.removeEventListener('nebula:theme-changed', onThemeChanged)
})
</script>

<style scoped>
.alerts-view {
  display: flex;
  flex-direction: column;
  gap: 16px;
}
/* 概览卡内嵌的指标格：用 s2 表达层级，去掉阴影避免"卡片叠卡片" */
.alert-stats {
  margin-bottom: 0;
}
.alert-stats .kpi {
  background: var(--s2);
  box-shadow: none;
}
/* 异常指标整块变红：不用色条，避免与"选中/激活"语义混淆 */
.alert-stats .kpi.kpi-alert {
  background: var(--danger-dim);
  border-color: var(--danger-bd);
}
.alert-refresh-error {
  color: var(--danger);
  font-size: var(--fs-sm);
  margin-bottom: var(--desc-base);
}
.rule-load-error {
  margin-bottom: 12px;
}
.panel-title-row {
  display: flex;
  justify-content: space-between;
  align-items: center;
}
.event-toolbar {
  display: flex;
  gap: 8px;
  align-items: center;
}
.event-toolbar-hint {
  font-size: var(--fs-sm);
  /* nowrap 让这句提示不可收缩：窄屏时它和单选组、批量确认一起把卡片头撑出横向滚动条，
     而标题区反而被压没。它只是辅助说明，允许折行即可。 */
  line-height: 1.4;
}
.maintenance-row {
  display: flex;
  gap: 16px;
  margin-top: 12px;
  align-items: flex-start;
  flex-wrap: wrap;
}
.maintenance-item {
  display: flex;
  flex-direction: column;
  gap: 4px;
  min-width: 200px;
}
.maintenance-label {
  font-size: var(--fs-sm);
  color: var(--t2);
}
.maintenance-hint {
  margin-top: var(--desc-base);
  font-size: var(--fs-sm);
  color: var(--warn);
  background: var(--warn-dim);
  border: 1px solid var(--warn-bd);
  padding: 6px 12px;
  border-radius: var(--r-sm);
}
.scope-count {
  margin-left: 6px;
  font-size: var(--fs-sm);
  color: var(--t2);
}
.ev-field {
  display: flex;
  gap: 12px;
  padding: 8px 0;
  border-bottom: 1px solid var(--bd);
  font-size: var(--fs-sm);
}
.ev-field > span:first-child {
  width: 64px;
  color: var(--t2);
  flex-shrink: 0;
}
.ev-chart-title {
  margin: 16px 0 8px;
  font-size: var(--fs-sm);
  color: var(--t2);
}
.collab-timeline {
  max-height: 220px;
  overflow-y: auto;
  border-left: 2px solid var(--el-border-color, #30363d);
  padding-left: 10px;
  margin-bottom: 10px;
}
.collab-item {
  margin-bottom: 10px;
}
.collab-head {
  display: flex;
  align-items: baseline;
  gap: 8px;
}
.collab-user {
  font-size: var(--fs-sm);
  font-weight: 600;
}
.collab-time {
  font-size: var(--fs-xs);
  color: var(--t2);
}
.collab-text {
  font-size: var(--fs-sm);
  line-height: 1.5;
  white-space: pre-wrap;
  word-break: break-word;
}
.collab-input {
  margin-bottom: 10px;
}
.ev-actions {
  margin-top: 16px;
  display: flex;
  gap: 8px;
  /* 抽屉内容宽约 536px，而这里要放认领/指派/关闭/重开/查看日志/诊断/评论等控件，
     不换行会被挤压变形甚至溢出抽屉边界。 */
  flex-wrap: wrap;
  align-items: center;
}
/* 只读辅助区：与"处置动作"用一条虚线隔开，避免误点关闭告警 */
.ev-actions-readonly {
  margin-top: 8px;
  padding-top: 8px;
  border-top: 1px dashed var(--bd);
}
.ev-actions-label {
  font-size: var(--fs-xs);
  color: var(--t3);
  flex-shrink: 0;
}
/* 级别列：色条 + 纯文字（不用 el-tag，避免与「状态」列的药丸抢同一套色板） */
.sev-text {
  font-size: var(--fs-sm);
  color: var(--t1);
  vertical-align: middle;
}
/* 抑制列：带删除线的灰标签——第三种视觉语言，一眼区别于级别与处置 */
.sup-tag {
  font-size: var(--fs-xs);
  color: var(--t3);
  text-decoration: line-through;
  text-decoration-color: var(--t3);
}
/* 事件表整行可双击打开详情：给出 cursor + hover 提示（此前两条路径都不明显） */
.event-table :deep(.el-table__body tr) {
  cursor: pointer;
}
.event-table :deep(.el-table__body tr:hover > td.el-table__cell) {
  background: var(--fill-hover);
}
.kpi-value.gray {
  color: var(--t2);
}
.adv-section {
  padding: 6px 0;
}
/* 折叠时在卡片头给出一行摘要，避免"收起来之后不知道里面配了什么" */
.adv-summary {
  font-size: var(--fs-xs);
}
.adv-title {
  font-size: var(--fs-base);
  font-weight: 600;
  margin-bottom: 10px;
  color: var(--t1);
}
.adv-row {
  display: flex;
  align-items: center;
  gap: 14px;
  flex-wrap: wrap;
  margin-bottom: 12px;
}
.adv-item {
  display: flex;
  align-items: center;
  gap: 6px;
}
.adv-item:has(.group-by-select) {
  min-width: 300px;
}
.group-by-select {
  width: 220px;
}
.group-by-select :deep(.el-select__selection) {
  flex-wrap: nowrap;
  min-width: 0;
}
.group-by-select :deep(.el-select__selected-item) {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
}
.adv-label {
  font-size: var(--fs-sm);
  color: var(--t2);
  white-space: nowrap;
}
.inh-block {
  background: var(--fill-1);
  border: 1px solid var(--bd);
  border-radius: var(--r-md);
  padding: 12px;
  margin-bottom: 12px;
}
.inh-title {
  font-size: var(--fs-sm);
  color: var(--t2);
  margin-bottom: 10px;
}
.inh-row {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 10px;
}
.inh-label {
  width: 92px;
  font-size: var(--fs-sm);
  color: var(--t2);
  flex-shrink: 0;
}
</style>

<!-- 模板下拉的样式必须**不加 scoped**：下拉面板由 Element Plus 传送到 body，
   scoped 选择器匹配不到它。用 popper-class 命名空间避免污染全局。 -->
<style>
.rule-template-menu .el-dropdown-menu {
  max-height: 62vh;
  overflow-y: auto;
  min-width: 420px;
}
.rule-template-menu .el-dropdown-menu__item {
  height: auto;
  padding: 8px 14px;
  line-height: 1.4;
}
.tpl-item {
  display: flex;
  flex-direction: column;
  gap: 2px;
  white-space: normal;
}
.tpl-head {
  display: flex;
  align-items: center;
  gap: 8px;
}
.tpl-group {
  flex-shrink: 0;
  padding: 0 6px;
  border-radius: 4px;
  background: var(--el-color-primary-light-9, rgba(64, 158, 255, 0.12));
  color: var(--el-color-primary, #409eff);
  font-size: var(--fs-xs);
}
.tpl-name {
  font-weight: 500;
}
.tpl-cond {
  font-size: var(--fs-sm);
  opacity: 0.9;
}
.tpl-desc {
  font-size: var(--fs-xs);
  color: var(--el-text-color-secondary, #909399);
}
/* 资产与影响面 */
.impact-assets {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-bottom: 6px;
}
.impact-asset {
  cursor: pointer;
}
.impact-note {
  margin: 4px 0 8px;
  font-size: var(--fs-sm);
}
.impact-sub {
  margin: 10px 0 6px;
  font-size: var(--fs-sm);
  color: var(--el-text-color-secondary, #909399);
}
.impact-changes {
  display: flex;
  flex-direction: column;
  gap: 5px;
  margin-bottom: 6px;
}
.impact-change {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
  align-items: baseline;
  font-size: var(--fs-xs);
}
.impact-change-at {
  color: var(--el-text-color-secondary, #909399);
}
.impact-change-field {
  font-weight: 500;
}
</style>

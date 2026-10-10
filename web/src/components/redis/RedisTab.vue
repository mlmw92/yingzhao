<template>
  <div class="mw-tab">
    <RefreshBar :loading="loading" :intervals="redisIntervals" v-model:interval="refreshInterval" @refresh="loadInstances" />

    <el-alert v-if="error" type="error" :closable="false" show-icon class="mw-err">
      <template #title>
        <span class="mw-err-text">{{ error }}</span>
        <el-button size="small" link type="primary" @click="loadInstances">重试</el-button>
      </template>
    </el-alert>

    <!-- 空状态：无实例时引导用户配置 -->
    <EmptyState
      v-if="!loading && !error && instances.length === 0"
      :icon="EmptyIcon"
      title="尚未配置 Redis 监控"
      :hints="emptyHints"
      action-text="复制安装命令"
      @action="copyCmd"
    />

    <!-- 有数据时的正常布局 -->
    <template v-if="instances.length > 0">
    <!-- ===== 区块1：统计概览卡片 ===== -->
    <div class="kpi-row">
      <KpiCard :value="stats.total" label="实例总数" tone="total">
        <template #icon><DatabaseIcon /></template>
      </KpiCard>
      <KpiCard :value="stats.up" label="在线实例" tone="up">
        <template #icon><CheckCircleIcon /></template>
      </KpiCard>
      <KpiCard :value="stats.down" label="离线实例" :tone="stats.down > 0 ? 'down' : 'total'">
        <template #icon><XCircleIcon /></template>
      </KpiCard>
      <KpiCard :value="formatBytes(stats.totalMemory)" label="总内存使用" tone="mem">
        <template #icon><MemoryIcon /></template>
      </KpiCard>
      <KpiCard :value="stats.totalClients" label="总连接客户端" tone="conn">
        <template #icon><ConnectionIcon /></template>
      </KpiCard>
      <KpiCard :value="formatNum(stats.totalOps)" label="总 OPS" tone="ops">
        <template #icon><ActivityIcon /></template>
      </KpiCard>
      <KpiCard :value="stats.clusterCount" label="集群/哨兵组" tone="cluster">
        <template #icon><ClusterIcon /></template>
      </KpiCard>
      <KpiCard :value="stats.alertCount" label="健康风险" :tone="stats.alertCount > 0 ? 'alert' : 'ok'">
        <template #icon><AlertIcon /></template>
      </KpiCard>
    </div>

    <div v-if="stats.alertCount > 0" class="alert-summary card">
      <div class="alert-summary-head">
        <AlertIcon />
        <span>检测到 {{ stats.alertCount }} 个实例存在健康风险</span>
      </div>
      <div class="alert-summary-desc">在线实例仍可连接；异常实例表示触发了页面健康检查规则，请优先查看下方实例列表的"异常原因"。</div>
      <div class="alert-summary-list">
        <span v-for="item in alertInstances.slice(0, 6)" :key="item.node + item.instance" class="issue-chip" @click="openDetail(item)">
          <span class="mono">{{ displayInstance(item) }}</span>
          <span class="issue-chip-reason">{{ issueReasons(item).join('；') }}</span>
        </span>
        <span v-if="alertInstances.length > 6" class="issue-chip muted">+{{ alertInstances.length - 6 }} 个</span>
      </div>
    </div>

    <!-- ===== 区块1.5：实例拓扑与集群关系 ===== -->
    <SectionCard title="实例拓扑" class="mw-block">
      <!-- 集群组（多集群横向并排，单集群自适应宽度） -->
      <template v-if="topologyGroups.clusters.length">
        <div class="topo-clusters-grid">
        <div v-for="grp in topologyGroups.clusters" :key="'c-'+grp.name" class="topo-group topo-cluster-card">
          <div class="topo-group-header">
            <span class="topo-group-title">
              <ClusterIcon />
              <strong>集群 {{ grp.name || '未命名' }}</strong>
            </span>
            <span class="topo-meta">
              <span class="badge" :class="clusterHealthClass(grp)">
                {{ clusterHealthText(grp) }}
              </span>
              <span class="dim">masters: {{ grp.masters.length }}</span>
              <span class="dim">· replicas: {{ grp.slaves.length }}</span>
              <span class="dim">· 总节点 {{ grp.masters.length + grp.slaves.length }}</span>
            </span>
          </div>

          <!-- 主从复制与故障转移：复制 ↓ 实线（master→slave），故障转移 ↑ 虚线（slave 升主） -->
          <div class="ms-section">
            <div class="ms-section-label">
              <span class="ms-label-repl">数据复制 ↓ master → slave</span>
              <span class="ms-label-fo">故障转移 ↑ slave 升主</span>
            </div>
            <div class="ms-tree">
              <div v-for="(m, idx) in grp.masters" :key="'cm-'+m.instance" class="ms-unit">
                <!-- Master 节点 -->
                <div
                  class="rel-node rel-master ms-master"
                  :class="{ 'is-down': !m.up, 'is-alert': isAlert(m) }"
                  :style="{ borderLeftColor: slotColor(grp, idx) }"
                  @click="openDetail(m)"
                >
                <div class="rel-node-name" :title="displayInstance(m)">
                  <span class="role-badge role-badge-m">M</span>
                  {{ displayInstance(m) }}
                </div>
                <div class="rel-node-meta">
                  <MwStatusDot :status="m.up ? 'normal' : 'abnormal'" :label="m.up ? '在线' : '离线'" />
                  <span class="dim">·</span>
                  <span>{{ formatNum(m.ops) }} ops/s</span>
                  <span class="dim">·</span>
                  <span>{{ formatBytes(m.usedMemory) }}</span>
                </div>
                </div>
                <!-- 复制链路 + failover 路径（按预聚合映射直接取，避免匹配失败） -->
                <div v-if="(grp.slavesByMaster[m.instance] || []).length" class="ms-branch">
                  <div class="ms-branch-rail">
                    <span class="ms-rail-repl" title="数据复制方向：master 将写入同步给 slave">复制 ↓</span>
                    <span class="ms-rail-fo" title="故障转移方向：master 宕机时，对应 slave 提升为新 master">故障转移 ↑</span>
                  </div>
                  <div class="ms-slaves">
                    <div
                      v-for="s in grp.slavesByMaster[m.instance]"
                      :key="'cs-'+s.instance"
                      class="rel-node rel-slave ms-slave-card"
                      :class="{ 'is-down': !s.up }"
                      @click.stop="openDetail(s)"
                    >
                      <div class="ms-slave-head">
                        <span class="role-badge role-badge-s">S</span>
                        <span class="mono" :title="displayInstance(s)">{{ displayInstance(s) }}</span>
                      </div>
                      <div class="ms-slave-meta">
                        <MwStatusDot :status="s.up ? 'normal' : 'abnormal'" :label="s.up ? '在线' : '离线'" />
                        <span class="dim">·</span>
                        <span>{{ s.up ? formatNum(s.ops) + ' ops/s' : '离线' }}</span>
                        <span v-if="s.up && s.replicationLag != null" class="dim">· lag {{ s.replicationLag }}s</span>
                        <span class="dim">·</span>
                        <span>{{ formatBytes(s.usedMemory) }}</span>
                      </div>
                    </div>
                  </div>
                </div>
              </div>
            </div>
          </div>
          <div class="topo-legend">
            <span class="topo-legend-item" title="master 将写入同步给 replica"><span class="legend-line legend-solid"></span>数据复制（master → slave）</span>
            <span class="topo-legend-item" title="master 宕机时对应 slave 提升为新 master"><span class="legend-line legend-dash"></span>故障转移（slave 升主，slave → master）</span>
          </div>
          <!-- 未关联主节点的从节点（replicaOf 为空，常见于 agent 未升级二进制） -->
          <div v-if="grp.unlinkedSlaves.length" class="unlinked-block">
            <div class="unlinked-label">
              <span class="role-badge role-badge-s">S</span>
              未关联主节点的从节点（{{ grp.unlinkedSlaves.length }} 个）— agent 升级后将自动关联
            </div>
            <div class="unlinked-list">
              <div v-for="s in grp.unlinkedSlaves" :key="'ul-'+s.instance"
                   class="rel-node rel-slave ms-slave-card"
                   :class="{ 'is-down': !s.up }"
                   @click.stop="openDetail(s)">
                <div class="ms-slave-head">
                  <span class="role-badge role-badge-s">S</span>
                  <span class="mono" :title="displayInstance(s)">{{ displayInstance(s) }}</span>
                </div>
                <div class="ms-slave-meta">
                  <MwStatusDot :status="s.up ? 'normal' : 'abnormal'" :label="s.up ? '在线' : '离线'" />
                  <span class="dim">·</span>
                  <span>{{ s.up ? formatNum(s.ops) + ' ops/s' : '离线' }}</span>
                  <span v-if="s.up && s.replicationLag != null" class="dim">· lag {{ s.replicationLag }}s</span>
                  <span class="dim">·</span>
                  <span>{{ formatBytes(s.usedMemory) }}</span>
                </div>
              </div>
            </div>
          </div>
        </div>
        </div>
      </template>

      <!-- 哨兵组 -->
      <template v-if="topologyGroups.sentinels.length">
        <div v-for="grp in topologyGroups.sentinels" :key="'s-'+grp.name" class="topo-group">
          <div class="topo-group-header">
            <span class="topo-group-title">
              <SentinelIcon />
              <strong>哨兵监控 {{ grp.name }}</strong>
            </span>
            <span class="topo-meta dim">
              sentinels: {{ grp.sentinels.length }} · masters: {{ grp.masters.length }}
            </span>
          </div>
          <div class="topo-relation topo-relation-sentinel">
            <div class="rel-edges rel-edges-left">
              <div v-for="(s, idx) in grp.sentinels" :key="'sn-'+idx" class="rel-node rel-sentinel" :class="{ 'is-down': !s.up }" @click="openDetail(s)">
                <div class="rel-node-name" :title="displayInstance(s)">{{ displayInstance(s) }}</div>
                <div class="rel-node-meta">
                  <MwStatusDot :status="s.up ? 'normal' : 'abnormal'" :label="s.up ? '在线' : '离线'" />
                  <span class="dim">·</span>
                  <span>监控 {{ s.sentinelMasters || 0 }} 个 master</span>
                  <span v-if="s.sentinelTilt" class="dim">· tilt</span>
                </div>
              </div>
            </div>
            <div class="rel-arrow">→ 监控 →</div>
            <div class="rel-edges rel-edges-right">
              <div v-for="(m, idx) in grp.masters" :key="'sm-'+idx" class="rel-node rel-master" :class="{ 'is-down': !m.up, 'is-alert': isAlert(m) }" @click="openDetail(m)">
                <div class="rel-node-name" :title="displayInstance(m)">{{ displayInstance(m) }}</div>
                <div class="rel-node-meta">
                  <MwStatusDot :status="m.up ? 'normal' : 'abnormal'" :label="m.up ? '在线' : '离线'" />
                  <span class="dim">·</span>
                  <span>repl offset {{ formatNum(m.replicationOffset) }}</span>
                </div>
              </div>
            </div>
          </div>
        </div>
      </template>

      <!-- 主从组 -->
      <template v-if="topologyGroups.replications.length">
        <div v-for="grp in topologyGroups.replications" :key="'r-'+grp.name" class="topo-group">
          <div class="topo-group-header">
            <span class="topo-group-title">
              <ReplicaIcon />
              <strong>主从 {{ grp.name }}</strong>
            </span>
            <span class="topo-meta">
              <span class="badge" :class="clusterHealthClass(grp)">
                {{ clusterHealthText(grp) }}
              </span>
              <span class="dim">masters: {{ grp.masters.length }}</span>
              <span class="dim">· replicas: {{ grp.slaves.length }}</span>
            </span>
          </div>
          <div class="ms-tree">
            <div v-for="(m, idx) in grp.masters" :key="'rm-'+idx" class="ms-unit">
              <div class="rel-node rel-master ms-master" :class="{ 'is-down': !m.up, 'is-alert': isAlert(m) }" @click="openDetail(m)">
                <div class="rel-node-name" :title="displayInstance(m)">
                  <span class="role-badge role-badge-m">M</span>
                  {{ displayInstance(m) }}
                </div>
                <div class="rel-node-meta">
                  <MwStatusDot :status="m.up ? 'normal' : 'abnormal'" :label="m.up ? '在线' : '离线'" />
                  <span class="dim">·</span>
                  <span>{{ formatNum(m.ops) }} ops/s</span>
                  <span class="dim">·</span>
                  <span>{{ formatBytes(m.usedMemory) }}</span>
                </div>
              </div>
              <div v-if="grp.slaves.filter(s => s.replicaOf === m.instance).length" class="ms-branch">
                <div class="ms-branch-rail">
                  <span class="ms-rail-repl" title="数据复制方向：master 将写入同步给 slave">复制 ↓</span>
                  <span class="ms-rail-fo" title="故障转移方向：master 宕机时，对应 slave 提升为新 master">故障转移 ↑</span>
                </div>
                <div class="ms-slaves">
                  <div v-for="s in grp.slaves.filter(ss => ss.replicaOf === m.instance)" :key="s.instance" class="rel-slave ms-slave" :class="{ 'is-down': !s.up }" @click.stop="openDetail(s)">
                    <span class="role-badge role-badge-s">S</span>
                    <span :title="displayInstance(s)">{{ displayInstance(s) }}</span>
                    <MwStatusDot dot-only :status="s.up ? 'normal' : 'abnormal'" />
                    <span class="dim">{{ s.up ? formatNum(s.ops) + ' ops/s' : '离线' }}</span>
                    <span v-if="s.up && s.replicationLag != null" class="dim">· lag {{ s.replicationLag }}s</span>
                  </div>
                </div>
              </div>
            </div>
          </div>
          <div class="topo-legend">
            <span class="topo-legend-item"><span class="legend-line legend-solid"></span>数据复制（master → slave）</span>
            <span class="topo-legend-item"><span class="legend-line legend-dash"></span>故障转移（slave 升主，slave → master）</span>
          </div>
        </div>
      </template>

      <!-- 独立实例 -->
      <template v-if="topologyGroups.standalones.length">
        <div class="topo-group">
          <div class="topo-group-header">
            <span class="topo-group-title">
              <ServerIcon />
              <strong>独立实例</strong>
            </span>
            <span class="dim">共 {{ topologyGroups.standalones.length }} 个</span>
          </div>
          <div class="topo-grid">
            <div v-for="(i, idx) in topologyGroups.standalones" :key="'sa-'+idx" class="rel-node rel-standalone" :class="{ 'is-down': !i.up, 'is-alert': isAlert(i) }" @click="openDetail(i)">
              <div class="rel-node-name" :title="displayInstance(i)">{{ displayInstance(i) }}</div>
              <div class="rel-node-meta">
                <MwStatusDot :status="i.up ? 'normal' : 'abnormal'" :label="i.up ? '在线' : '离线'" />
                <span class="dim">·</span>
                <span>{{ formatNum(i.ops) }} ops/s</span>
                <span class="dim">·</span>
                <span>{{ formatBytes(i.usedMemory) }}</span>
                <span v-if="i.role === 'master' && i.connectedSlaves" class="dim">· {{ i.connectedSlaves }} slaves</span>
              </div>
            </div>
          </div>
        </div>
      </template>
    </SectionCard>

    <!-- ===== 区块2：实例列表 ===== -->
    <SectionCard
      title="实例列表"
      subtitle="核心运行指标。异常实例为页面本地判定结果，不等同于告警中心事件。"
      dense
      class="mw-block"
    >
      <template #actions>
        <el-select v-model="filterStatus" placeholder="状态" clearable size="small" style="width: 100px">
          <el-option label="全部" value="" />
          <el-option label="在线" value="up" />
          <el-option label="离线" value="down" />
        </el-select>
        <el-select v-model="filterTopology" placeholder="拓扑" clearable size="small" style="width: 120px">
          <el-option label="全部" value="" />
          <el-option label="单机" value="standalone" />
          <el-option label="主从" value="replication" />
          <el-option label="哨兵" value="sentinel" />
          <el-option label="集群" value="cluster" />
        </el-select>
        <el-input v-model="searchText" placeholder="搜索实例/节点" clearable size="small" style="width: 200px" :prefix-icon="SearchIcon" />
      </template>

      <el-table :data="pagedInstances" style="width: 100%" @row-click="openDetail" :row-class-name="rowClass" size="small" stripe>
        <el-table-column label="实例地址" prop="instance" min-width="140" show-overflow-tooltip>
          <template #default="{ row }">
            <span class="mono">{{ displayInstance(row) }}</span>
          </template>
        </el-table-column>
        <el-table-column label="角色" width="80">
          <template #default="{ row }">
            <MwRoleTag :role="row.role" />
          </template>
        </el-table-column>
        <el-table-column label="拓扑" width="70">
          <template #default="{ row }">
            <span class="topo-tag">{{ topoLabel(row.topology) }}</span>
          </template>
        </el-table-column>
        <el-table-column label="版本" prop="version" width="75" />
          <el-table-column label="状态" width="85">
            <template #default="{ row }">
              <el-tooltip v-if="isAlert(row)" :content="issueReasons(row).join('；')" placement="top">
                <MwStatusDot :status="instStatus(row).status" :label="instStatus(row).label" :tooltip="issueReasons(row).join('；')" />
              </el-tooltip>
              <MwStatusDot v-else :status="instStatus(row).status" :label="instStatus(row).label" />
            </template>
          </el-table-column>
        <el-table-column label="异常原因" min-width="140" show-overflow-tooltip>
          <template #default="{ row }">
            <span v-if="isAlert(row)" class="issue-reasons">{{ issueReasons(row).join('；') }}</span>
            <span v-else class="dim-text">-</span>
          </template>
        </el-table-column>
        <el-table-column label="客户端" width="70" align="right">
          <template #default="{ row }">
            <span class="mono">{{ formatNum(row.clients) }}</span>
          </template>
        </el-table-column>
        <el-table-column label="内存使用" min-width="120">
          <template #default="{ row }">
            <div class="mem-cell">
              <div class="mem-text mono">{{ formatBytes(row.usedMemory) }}</div>
              <div class="bar" v-if="row.memPercent > 0">
                <div class="bar-fill" :class="memBarClass(row.memPercent)" :style="{ width: row.memPercent + '%' }"></div>
              </div>
              <div class="mem-pct" v-if="row.memPercent > 0">{{ row.memPercent }}%</div>
            </div>
          </template>
        </el-table-column>
        <el-table-column label="OPS" width="70" align="right">
          <template #default="{ row }">
            <span class="mono">{{ formatNum(row.ops) }}</span>
          </template>
        </el-table-column>
        <el-table-column label="命中率" width="70" align="right">
          <template #default="{ row }">
            <span class="mono" :class="hitRateClass(row.hitRate)">{{ row.hitRate }}%</span>
          </template>
        </el-table-column>
        <el-table-column label="碎片率" width="65" align="right">
          <template #default="{ row }">
            <span class="mono" :class="row.fragmentation > 1.5 ? 'rate-warn' : ''">{{ row.fragmentation }}</span>
          </template>
        </el-table-column>
        <el-table-column label="从节点" width="65" align="right">
          <template #default="{ row }">
            <span class="mono" v-if="row.role === 'master' && row.connectedSlaves">{{ row.connectedSlaves }}</span>
            <span class="dim" v-else>--</span>
          </template>
        </el-table-column>
        <el-table-column label="运行时长" width="85">
          <template #default="{ row }">
            <span class="mono">{{ formatUptime(row.uptime) }}</span>
          </template>
        </el-table-column>
        </el-table>
        <div class="pager">
          <el-pagination background layout="total, sizes, prev, pager, next, jumper" :total="filteredInstances.length" :page-size="pageSize" :current-page="currentPage" :page-sizes="[10,20,50,100]" @current-change="v => currentPage = v" @size-change="v => { pageSize = v; currentPage = 1 }" />
        </div>
    </SectionCard>

    <!-- ===== 区块3：性能排行（横向柱状图）===== -->
    <SectionCard
      title="性能排行 Top 10"
      subtitle="用于快速定位资源占用和请求压力最高的实例，排序随自动刷新增量更新。"
      class="mw-block"
    >
      <div class="bar-row">
        <div class="bar-item">
          <div class="bar-sub-title">内存使用量 Top 10</div>
          <div class="chart-note">展示 Redis 当前 used_memory，适合排查容量压力和内存倾斜。</div>
          <TrendChart :option="memBarOption" :height="280" />
        </div>
        <div class="bar-item">
          <div class="bar-sub-title">OPS Top 10</div>
          <div class="chart-note">展示当前每秒命令处理量，适合识别热点实例。</div>
          <TrendChart :option="opsBarOption" :height="280" />
        </div>
      </div>
    </SectionCard>

    <!-- ===== 区块4：缓存命中率 ===== -->
    <SectionCard
      title="缓存命中率"
      subtitle="命中率低通常表示缓存穿透、过期策略或业务访问模式需要检查。"
      class="mw-block"
    >
      <TrendChart :option="hitRateOption" :height="260" />
    </SectionCard>

    <!-- ===== 区块5：实例详情抽屉 ===== -->
    <el-drawer v-model="detailVisible" title="Redis 实例详情" size="60%" :destroy-on-close="true">
      <div class="detail-content" v-if="selected">
        <MwDetailHeader
          :title="displayInstance(selected)"
          :subtitle="selected.node"
          :status="instStatus(selected).status"
          :status-label="instStatus(selected).label"
          :fields="detailFields"
          v-model:range="range"
        />

        <!-- 关键指标快照 -->
        <div class="snapshot-grid" v-if="selected">
          <div class="snap-card">
            <div class="snap-label">碎片率</div>
            <div class="snap-value" :class="(selected.fragmentation||0) > 1.5 ? 'warn' : ''">{{ selected.fragmentation || 0 }}</div>
          </div>
          <div class="snap-card">
            <div class="snap-label">阻塞客户端</div>
            <div class="snap-value">{{ selected.blocked || 0 }}</div>
          </div>
          <div class="snap-card">
            <div class="snap-label">最大内存</div>
            <div class="snap-value">{{ formatBytes(selected.maxMemory) }}</div>
          </div>
          <div class="snap-card">
            <div class="snap-label">淘汰 / 过期 Key</div>
            <div class="snap-value">{{ formatNum(selected.evicted) }} / {{ formatNum(selected.expired) }}</div>
          </div>
          <div class="snap-card">
            <div class="snap-label">拒绝连接</div>
            <div class="snap-value">{{ formatNum(selected.rejected) }}</div>
          </div>
          <div class="snap-card" v-if="selected.role === 'master'">
            <div class="snap-label">从节点 / 复制偏移</div>
            <div class="snap-value">{{ selected.connectedSlaves || 0 }} / {{ formatNum(selected.replicationOffset) }}</div>
          </div>
          <div class="snap-card" v-if="selected.role === 'slave'">
            <div class="snap-label">复制延迟</div>
            <div class="snap-value" :class="(selected.replicationLag||0) > 10 ? 'warn' : ''">{{ selected.replicationLag || 0 }} s</div>
          </div>
          <div class="snap-card" v-if="selected.role === 'slave'">
            <div class="snap-label">复制偏移</div>
            <div class="snap-value">{{ formatNum(selected.replicationOffset) }}</div>
          </div>
        </div>

        <!-- 集群健康度（cluster 拓扑） -->
        <div class="snapshot-grid snapshot-cluster" v-if="selected && selected.topology === 'cluster'">
          <div class="snap-card snap-card-wide">
            <div class="snap-label">集群状态</div>
            <div class="snap-value" :class="clusterStateClass(selected)">
              {{ clusterStateText(selected) }}
            </div>
          </div>
          <div class="snap-card">
            <div class="snap-label">槽位 ok</div>
            <div class="snap-value">{{ metricText(selected.clusterSlotsOk) }}</div>
          </div>
          <div class="snap-card">
            <div class="snap-label">槽位 assigned</div>
            <div class="snap-value">{{ metricText(selected.clusterSlotsAssigned) }}</div>
          </div>
          <div class="snap-card">
            <div class="snap-label">槽位失败</div>
            <div class="snap-value" :class="selected.clusterSlotsFail > 0 ? 'warn' : 'ok'">{{ metricText(selected.clusterSlotsFail) }}</div>
          </div>
          <div class="snap-card">
            <div class="snap-label">集群大小</div>
            <div class="snap-value">{{ metricText(selected.clusterSize) }}</div>
          </div>
          <div class="snap-card">
            <div class="snap-label">已知节点</div>
            <div class="snap-value">{{ metricText(selected.clusterKnownNodes) }}</div>
          </div>
        </div>

        <!-- 哨兵监控（sentinel 拓扑） -->
        <div class="snapshot-grid snapshot-sentinel" v-if="selected && selected.topology === 'sentinel'">
          <div class="snap-card" v-if="selected.role === 'sentinel'">
            <div class="snap-label">监控 master</div>
            <div class="snap-value">{{ selected.sentinelMasters || 0 }}</div>
          </div>
          <div class="snap-card" v-if="selected.role === 'sentinel'">
            <div class="snap-label">监控 slave</div>
            <div class="snap-value">{{ selected.sentinelSlaves || 0 }}</div>
          </div>
          <div class="snap-card" v-if="selected.role === 'sentinel'">
            <div class="snap-label">同组 sentinel</div>
            <div class="snap-value">{{ selected.sentinelSentinels || 0 }}</div>
          </div>
          <div class="snap-card" v-if="selected.role === 'sentinel'">
            <div class="snap-label">Tilt 模式</div>
            <div class="snap-value" :class="(selected.sentinelTilt||0) > 0 ? 'warn' : 'ok'">{{ (selected.sentinelTilt||0) > 0 ? '是' : '否' }}</div>
          </div>
          <div class="snap-card" v-if="selected.role === 'master' && selected.replicaOf && selected.replicaOf.startsWith('sentinel:')">
            <div class="snap-label">受 Sentinel 监控</div>
            <div class="snap-value ok">{{ selected.replicaOf.slice('sentinel:'.length) }}</div>
          </div>
        </div>

        <!-- 趋势图网格 -->
        <div class="chart-grid">
          <div class="trend-card" v-for="chart in trendCharts" :key="chart.key">
            <div class="tc-head">
              <span class="tc-label">{{ chart.label }}</span>
              <span class="tc-value" v-if="chart.current !== null">{{ chart.currentText }}</span>
            </div>
            <TrendChart :option="chart.option" :loading="chart.loading" :error="chart.error" :height="160" @retry="loadTrendData" />
          </div>
        </div>
      </div>
    </el-drawer>
    </template><!-- /有数据时的正常布局 -->
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted, watch, h } from 'vue'
import { Search } from '@element-plus/icons-vue'
import RefreshBar from '../RefreshBar.vue'
import KpiCard from '../KpiCard.vue'
import SectionCard from '../common/SectionCard.vue'
import EmptyState from '../common/EmptyState.vue'
import MwStatusDot from '../mw/MwStatusDot.vue'
import MwRoleTag from '../mw/MwRoleTag.vue'
import MwDetailHeader from '../mw/MwDetailHeader.vue'
import TrendChart from '../mw/TrendChart.vue'
import { ElMessage } from 'element-plus'
import http from '../../api/http'
import { COLORS, tokenColor, mwTrendOption } from '../../charts/echarts'
import { rangeToWindow, rangeToStep } from '../mw/mwRange'

function instStatus(i) {
  if (!i.up) return { status: 'abnormal', label: '离线' }
  if (isAlert(i)) return { status: 'abnormal', label: '异常' }
  return { status: 'normal', label: '正常' }
}

const AXIS_COLOR = tokenColor('t2', '#9fb3c8')

// 图表配色从 CSS 令牌读取：切主题后 nebula:theme-changed 会失效重取
const TEXT_COLOR = tokenColor('t1', '#e5edf7')
const SPLIT_COLOR = 'rgba(148, 163, 184, 0.12)'

// ---- 图标组件（内联 SVG，渲染函数规避 runtime-only 无法编译 { template } 的问题） ----
function svgIcon(s) {
  const inner = s.replace(/^<svg[^>]*>/, '').replace(/<\/svg>\s*$/, '')
  return () => h('svg', { viewBox: '0 0 24 24', fill: 'none', stroke: 'currentColor', 'stroke-width': 2, innerHTML: inner })
}
// 空状态图标（数据库圆柱，与 Postgres 同款造型）
const EmptyIcon = () => h('svg', {
  viewBox: '0 0 24 24', fill: 'none', stroke: 'currentColor', 'stroke-width': 1.5,
  innerHTML: '<ellipse cx="12" cy="5" rx="9" ry="3"/><path d="M3 5v6c0 1.66 4 3 9 3s9-1.34 9-3V5"/><path d="M3 11v6c0 1.66 4 3 9 3s9-1.34 9-3v-6"/>',
})
const DatabaseIcon = svgIcon('<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><ellipse cx="12" cy="5" rx="9" ry="3"/><path d="M3 5v6c0 1.66 4 3 9 3s9-1.34 9-3V5"/><path d="M3 11v6c0 1.66 4 3 9 3s9-1.34 9-3v-6"/></svg>')
const CheckCircleIcon = svgIcon('<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><polyline points="9 12 12 15 16 10"/></svg>')
const XCircleIcon = svgIcon('<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><line x1="15" y1="9" x2="9" y2="15"/><line x1="9" y1="9" x2="15" y2="15"/></svg>')
const MemoryIcon = svgIcon('<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M6 19v-3"/><path d="M10 19v-3"/><path d="M14 19v-3"/><path d="M18 19v-3"/><path d="M8 11V9"/><path d="M16 11V9"/><path d="M12 11V9"/><path d="M2 15h20"/><path d="M2 7a2 2 0 0 1 2-2h16a2 2 0 0 1 2 2v1.1a2 2 0 0 0 0 3.837V17a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2v-5.063a2 2 0 0 0 0-3.837Z"/></svg>')
const ConnectionIcon = svgIcon('<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M13.144 10.144a4 4 0 1 0-5.742 0"/><path d="M11 14.48V17"/><circle cx="11" cy="19" r="2"/><path d="M16 9a5 5 0 0 1 4.516 2.861"/><path d="M19.922 12.633a5 5 0 0 1-.39 4.155"/><circle cx="21" cy="18" r="2"/></svg>')
const ActivityIcon = svgIcon('<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="22 12 18 12 15 21 9 3 6 12 2 12"/></svg>')
const SearchIcon = Search
const ClusterIcon = svgIcon('<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="3"/><circle cx="5" cy="5" r="2"/><circle cx="19" cy="5" r="2"/><circle cx="5" cy="19" r="2"/><circle cx="19" cy="19" r="2"/><line x1="6.5" y1="6.5" x2="9.5" y2="9.5"/><line x1="14.5" y1="9.5" x2="17.5" y2="6.5"/><line x1="6.5" y1="17.5" x2="9.5" y2="14.5"/><line x1="14.5" y1="14.5" x2="17.5" y2="17.5"/></svg>')
const SentinelIcon = svgIcon('<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M12 2l8 4v6c0 5-3.5 9-8 10-4.5-1-8-5-8-10V6z"/><path d="M9 12l2 2 4-4"/></svg>')
const ReplicaIcon = svgIcon('<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="3" y="4" width="8" height="6" rx="1"/><rect x="13" y="14" width="8" height="6" rx="1"/><path d="M11 7h6a2 2 0 0 1 2 2v5"/></svg>')
const AlertIcon = svgIcon('<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M10.29 3.86 1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"/><line x1="12" y1="9" x2="12" y2="13"/><line x1="12" y1="17" x2="12.01" y2="17"/></svg>')
const ServerIcon = svgIcon('<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="2" y="3" width="20" height="8" rx="2"/><rect x="2" y="13" width="20" height="8" rx="2"/><line x1="6" y1="7" x2="6.01" y2="7"/><line x1="6" y1="17" x2="6.01" y2="17"/></svg>')

// ---- 数据 ----
const instances = ref([])
const activeAlerts = ref([])
const loading = ref(false)
const error = ref('')
const filterStatus = ref('')
const filterTopology = ref('')
const searchText = ref('')
const refreshInterval = ref(30)
const redisIntervals = [
  { label: '10秒', value: 10 },
  { label: '30秒', value: 30 },
  { label: '60秒', value: 60 },
]

// Server 真实地址（取自 /api/v1/install-info，参考「添加主机」功能）
const serverURL = ref('')
// 主命令：节点上已有 agent-install.sh（agent 安装时已自拷贝到 /etc/monitor-agent/），
// 直接本地运行可进入交互式向导（stdin 为终端，能正常 read）。
const redisInstallCmd = computed(() => 'bash /etc/monitor-agent/agent-install.sh redis')
// 备选命令：节点没有本地脚本时，先下载到本地再以交互方式运行。
// 注意：不要用 curl ... | bash 管道方式（管道会占用 stdin，交互向导无法输入）。
const redisInstallCmdAlt = computed(() =>
  (serverURL.value
    ? `curl -fsSL ${serverURL.value}/install/agent-install.sh`
    : 'curl -fsSL http://<server>:8080/install/agent-install.sh') +
  ' -o /tmp/agent-install.sh && bash /tmp/agent-install.sh redis'
)
function loadServerURL() {
  http
    .get('/api/v1/install-info')
    .then((info) => { if (info && info.serverURL) serverURL.value = info.serverURL })
    .catch(() => {})
}

// 空状态引导文案（含可直接复制的安装命令）
const emptyHints = computed(() => [
  '当前没有已采集的 Redis 实例。请在运行 Agent 的节点上执行以下命令，按引导配置 Redis 实例：',
  redisInstallCmd.value,
  `若节点没有本地脚本，先下载到本地再交互运行：${redisInstallCmdAlt.value}`,
  '配置完成后约 15-30 秒数据将出现在此页面。详细配置说明请参阅 README.md。',
])

// 统计
const stats = reactive({ total: 0, up: 0, down: 0, totalMemory: 0, totalClients: 0, totalOps: 0, clusterCount: 0, alertCount: 0 })

const alertsByInstance = computed(() => {
  const m = new Map()
  for (const alert of activeAlerts.value) {
    if (!alert.instance) continue
    const key = `${alert.node}|${alert.instance}`
    const list = m.get(key) || []
    list.push(alert)
    m.set(key, list)
  }
  return m
})

function instanceAlerts(i) {
  return alertsByInstance.value.get(`${i.node}|${i.instance}`) || []
}

function issueReasons(i) {
  const reasons = []
  if (!i.up) reasons.push('实例离线')
  for (const alert of instanceAlerts(i)) {
    reasons.push(alert.message || `${alert.ruleName || alert.rule || '告警规则'} 触发`)
  }
  return reasons
}

function isAlert(i) {
  return issueReasons(i).length > 0
}

const alertInstances = computed(() => instances.value.filter(isAlert))

function metricText(value) {
  return value === undefined || value === null ? '-' : value
}

// 展示用实例地址：Redis 实际监听地址常为回环(127.0.0.1)，用节点真实 IP 还原为「服务器 IP:端口」
function displayInstance(i) {
  if (!i) return ''
  const inst = i.instance || ''
  const nodeIp = i.nodeIp || ''
  const m = inst.match(/^(127\.0\.0\.1|localhost|\[::1\]|::1)(?::(\d+))?$/i)
  if (m && nodeIp) {
    return nodeIp + (m[2] ? ':' + m[2] : '')
  }
  return inst
}

function clusterStateText(instance) {
  if (instance.clusterState === 1) return 'ok · 健康'
  if (instance.clusterState === 0) return '异常'
  return '未采集'
}

function clusterStateClass(instance) {
  if (instance.clusterState === 1) return 'ok'
  if (instance.clusterState === 0) return 'warn'
  return ''
}

// 过滤
const filteredInstances = computed(() => {
  return instances.value.filter(i => {
    if (filterStatus.value === 'up' && !i.up) return false
    if (filterStatus.value === 'down' && i.up) return false
    if (filterTopology.value && i.topology !== filterTopology.value) return false
    if (searchText.value) {
      const s = searchText.value.toLowerCase()
      if (!i.instance.toLowerCase().includes(s) &&
          !i.node.toLowerCase().includes(s) &&
          !(i.name || '').toLowerCase().includes(s)) return false
    }
    return true
  })
})
const currentPage = ref(1)
const pageSize = ref(10)
const pagedInstances = computed(() => filteredInstances.value.slice((currentPage.value - 1) * pageSize.value, currentPage.value * pageSize.value))
watch(filteredInstances, () => { currentPage.value = 1 })

// 拓扑分组：用于关系视图（基于后端 group 字段，不再正则猜测）
const topologyGroups = computed(() => {
  const clusters = {}, sentinels = {}, replications = {}, standalones = []
  for (const i of instances.value) {
    // 分组键用 group 优先：cluster/sentinel 同组各节点 group 相同（=agent 配置的实例名），
    // 而 name 在 cluster 下带地址后缀各不相同，用 name 会把同一集群打散成多张卡。
    const g = i.group || i.name || i.instance
    if (i.topology === 'cluster') {
      clusters[g] = clusters[g] || { name: g, masters: [], slaves: [], slavesByMaster: {}, unlinkedSlaves: [], topology: 'cluster' }
      if (i.role === 'slave' || i.role === 'replica') {
        clusters[g].slaves.push(i)
        // 按 replicaOf 预聚合到对应 master
        if (i.replicaOf) {
          clusters[g].slavesByMaster[i.replicaOf] = clusters[g].slavesByMaster[i.replicaOf] || []
          clusters[g].slavesByMaster[i.replicaOf].push(i)
        } else {
          // replicaOf 为空（旧 agent 未上报或无法确定主节点关系）→ 归入未关联区域，避免从节点丢失
          clusters[g].unlinkedSlaves.push(i)
        }
      } else clusters[g].masters.push(i)
    } else if (i.topology === 'sentinel') {
      sentinels[g] = sentinels[g] || { name: g, sentinels: [], masters: [], topology: 'sentinel' }
      if (i.role === 'sentinel') sentinels[g].sentinels.push(i)
      else sentinels[g].masters.push(i)
    } else if (i.topology === 'replication' || (i.replicaOf && i.topology !== 'cluster')) {
      // 主从模式：同一 group 下的 master 与 slave
      replications[g] = replications[g] || { name: g, masters: [], slaves: [], topology: 'replication' }
      if (i.role === 'slave' || i.role === 'replica') replications[g].slaves.push(i)
      else replications[g].masters.push(i)
    } else {
      standalones.push(i)
    }
  }
  return {
    clusters: Object.values(clusters),
    sentinels: Object.values(sentinels),
    replications: Object.values(replications),
    standalones,
  }
})

// ---- 集群 Slot 分片配色 ----
const SLOT_PALETTE = [COLORS.blue, COLORS.purple, COLORS.green, COLORS.amber, COLORS.purple, COLORS.cyan, COLORS.amber, COLORS.green, COLORS.blue, COLORS.red]

// slotColor 返回组内第 idx 个 master 的分配色（与图例/master 卡片同色）
function slotColor(grp, idx) {
  return SLOT_PALETTE[idx % SLOT_PALETTE.length]
}

// 集群健康度判定（基于 topologyGroups 中 cluster 组的 masters 状态）
function clusterHealth(grp) {
  const total = grp.masters.length
  const up = grp.masters.filter(m => m.up).length
  if (total === 0) return { cls: 'down', text: '无 master' }
  if (up === 0) return { cls: 'down', text: '全部离线' }
  // 槽位失败（仅 cfg.Addr master 会上报该 metric，取该值；其他 master 字段为 0/undefined）
  const reportedFailSlots = grp.masters.find(m => m.clusterSlotsFail !== undefined && m.clusterSlotsFail !== null)?.clusterSlotsFail || 0
  if (reportedFailSlots > 0) return { cls: 'warn', text: `槽位失败 ${reportedFailSlots}` }
  // cluster_state 同理：仅 cfg.Addr master 上报；只有明确采集到且为 0 才视为异常，
  // 避免 cfg.Addr 不在采集链上（或 agent 版本未上报该字段）时把所有 master 的 0 误判为异常。
  const reportedState = grp.masters.find(m => m.clusterState === 0 || m.clusterState === 1)?.clusterState
  if (reportedState === 0) return { cls: 'warn', text: '集群状态异常' }
  return { cls: 'ok', text: '健康' }
}
function clusterHealthClass(grp) { return 'badge-' + clusterHealth(grp).cls }
function clusterHealthText(grp) { return clusterHealth(grp).text }

// ---- 概览图表（性能排行 / 缓存命中率）：改用 TrendChart 托管生命周期 ----
// 过去这三张图手写 chartRefs/chartInstances/initChart/setOption/resize/dispose，
// 关闭抽屉或切页时容易残留实例；现在统一由 TrendChart 负责 init/dispose 与三态。
function barTooltip(formatter) {
  return {
    trigger: 'axis', axisPointer: { type: 'shadow' },
    backgroundColor: 'rgba(11,17,32,0.92)', borderColor: 'rgba(34,211,238,0.3)', textStyle: { color: TEXT_COLOR },
    formatter,
  }
}

const memBarOption = computed(() => {
  const sorted = [...instances.value].filter(i => i.usedMemory > 0).sort((a, b) => b.usedMemory - a.usedMemory).slice(0, 10).reverse()
  const max = sorted.length ? sorted[sorted.length - 1].usedMemory : 1
  return {
    tooltip: barTooltip((p) => `${p[0].name}<br/>${formatBytes(p[0].value)}`),
    grid: { left: 10, right: 60, top: 8, bottom: 8, containLabel: true },
    xAxis: { type: 'value', axisLabel: { color: AXIS_COLOR, fontSize: 10, formatter: (v) => formatBytesShort(v) }, splitLine: { lineStyle: { color: SPLIT_COLOR } } },
    yAxis: { type: 'category', data: sorted.map(i => i.name || i.instance), axisLabel: { color: AXIS_COLOR, fontSize: 11, width: 120, overflow: 'truncate' }, axisLine: { lineStyle: { color: AXIS_COLOR } } },
    series: [{
      type: 'bar', data: sorted.map(i => i.usedMemory), barWidth: '55%',
      itemStyle: {
        borderRadius: [0, 4, 4, 0],
        color: (params) => {
          const ratio = params.value / max
          if (ratio > 0.8) return COLORS.red
          if (ratio > 0.6) return COLORS.amber
          return COLORS.cyan
        },
      },
    }],
  }
})

const opsBarOption = computed(() => {
  const sorted = [...instances.value].filter(i => i.ops > 0).sort((a, b) => b.ops - a.ops).slice(0, 10).reverse()
  return {
    tooltip: barTooltip((p) => `${p[0].name}<br/>${formatNum(p[0].value)} ops/s`),
    grid: { left: 10, right: 50, top: 8, bottom: 8, containLabel: true },
    xAxis: { type: 'value', axisLabel: { color: AXIS_COLOR, fontSize: 10, formatter: (v) => formatNum(v) }, splitLine: { lineStyle: { color: SPLIT_COLOR } } },
    yAxis: { type: 'category', data: sorted.map(i => i.name || i.instance), axisLabel: { color: AXIS_COLOR, fontSize: 11, width: 120, overflow: 'truncate' }, axisLine: { lineStyle: { color: AXIS_COLOR } } },
    series: [{
      type: 'bar', data: sorted.map(i => i.ops), barWidth: '55%',
      itemStyle: { borderRadius: [0, 4, 4, 0], color: COLORS.blue },
    }],
  }
})

const hitRateOption = computed(() => {
  const sorted = [...instances.value].filter(i => i.hitRate > 0).sort((a, b) => a.hitRate - b.hitRate)
  return {
    tooltip: barTooltip((p) => `${p[0].name}<br/>命中率 ${p[0].value}%`),
    grid: { left: 10, right: 30, top: 8, bottom: 8, containLabel: true },
    xAxis: { type: 'category', data: sorted.map(i => i.name || i.instance), axisLabel: { color: AXIS_COLOR, fontSize: 10, interval: 0, rotate: sorted.length > 8 ? 30 : 0 }, axisLine: { lineStyle: { color: AXIS_COLOR } } },
    yAxis: { type: 'value', min: 0, max: 100, axisLabel: { color: AXIS_COLOR, fontSize: 10, formatter: '{value}%' }, splitLine: { lineStyle: { color: SPLIT_COLOR } } },
    series: [{
      type: 'bar', data: sorted.map(i => i.hitRate), barWidth: '40%',
      itemStyle: {
        borderRadius: [4, 4, 0, 0],
        color: (params) => {
          const v = params.value
          if (v < 50) return COLORS.red
          if (v < 80) return COLORS.amber
          return COLORS.green
        },
      },
    }],
  }
})

// ---- 加载数据 ----
async function loadInstances() {
  loading.value = true
  error.value = ''
  try {
    const [instancesData, alertsData] = await Promise.all([
      http.get('/api/v1/middleware/redis/instances'),
      http.get('/api/v1/alerts?state=firing'),
    ])
    instances.value = instancesData.instances || []
    activeAlerts.value = (alertsData.alerts || []).filter(a => a.instance)
    computeStats()
  } catch (e) {
    console.error('加载 Redis 实例失败', e)
    error.value = 'Redis 实例数据加载失败'
  } finally {
    loading.value = false
  }
}

function computeStats() {
  const list = instances.value
  stats.total = list.length
  stats.up = list.filter(i => i.up).length
  stats.down = list.filter(i => !i.up).length
  stats.totalMemory = list.reduce((s, i) => s + (i.usedMemory || 0), 0)
  stats.totalClients = list.reduce((s, i) => s + (i.clients || 0), 0)
  stats.totalOps = list.reduce((s, i) => s + (i.ops || 0), 0)
  // 集群/哨兵组数（按 group 去重，group 即同集群各节点相同的分组名）
  const clusterNames = new Set()
  for (const i of list) {
    if (i.topology === 'cluster' || i.topology === 'sentinel') {
      const key = i.group || i.name
      if (key) clusterNames.add(key)
    }
  }
  stats.clusterCount = clusterNames.size
  // 告警实例数
  stats.alertCount = list.filter(isAlert).length
}

// ---- 详情抽屉 ----
const detailVisible = ref(false)
const selected = ref(null)
const range = ref('1h')

const detailFields = computed(() => {
  const s = selected.value
  if (!s) return []
  const f = [
    { label: '角色', value: roleLabel(s.role) },
    { label: '拓扑', value: topoLabel(s.topology) },
  ]
  if (s.group) f.push({ label: '分组', value: s.group })
  if (s.version) f.push({ label: '版本', value: s.version, mono: true })
  if (s.uptime > 0) f.push({ label: '运行时长', value: formatUptime(s.uptime) })
  return f
})

function openDetail(row) {
  selected.value = row
  detailVisible.value = true
  loadTrendData()
}

// 趋势图定义：每实例固定一组指标，随抽屉打开 / 时间范围切换重查。
function buildTrendDefs(row) {
  const list = [
    { key: 'mem', label: '内存使用率', metric: 'redis_used_memory_percent', unit: '%', yMax: 100, current: row.memPercent ?? null, currentText: (row.memPercent ?? 0) + '%' },
    { key: 'clients', label: '连接客户端数', metric: 'redis_connected_clients', unit: '', current: row.clients ?? null, currentText: formatNum(row.clients) },
    { key: 'ops', label: '命令速率(OPS)', metric: 'redis_ops_per_sec', unit: '', current: row.ops ?? null, currentText: formatNum(row.ops) },
    { key: 'hitrate', label: '缓存命中率', metric: 'redis_hit_rate', unit: '%', yMax: 100, current: row.hitRate ?? null, currentText: (row.hitRate ?? 0) + '%' },
    { key: 'keys', label: '键数量', metric: 'redis_keys', unit: '', current: row.keys ?? null, currentText: formatNum(row.keys) },
    { key: 'frag', label: '内存碎片率', metric: 'redis_memory_fragmentation_ratio', unit: '', current: null, currentText: '-' },
    { key: 'evicted', label: '淘汰键数', metric: 'redis_evicted_keys', unit: '', current: null, currentText: '-' },
    { key: 'uptime', label: '运行时长', metric: 'redis_uptime_in_seconds', unit: 's', current: row.uptime ?? null, currentText: formatUptime(row.uptime) },
  ]
  if (row.role === 'slave') {
    list.push({ key: 'lag', label: '复制延迟(秒)', metric: 'redis_replication_lag', unit: 's', current: null, currentText: '-' })
  }
  return list
}

const trendCharts = ref([])

// 并行拉取所有趋势指标（此前是 for-await 串行，N 张图 = N 次往返叠加）。
async function loadTrendData() {
  const row = selected.value
  if (!row) return
  const { start, end } = rangeToWindow(range.value)
  const step = rangeToStep(range.value)
  trendCharts.value = buildTrendDefs(row).map((d) => ({ ...d, option: null, loading: true, error: '' }))
  await Promise.all(
    trendCharts.value.map(async (t) => {
      try {
        const data = await http.get(
          `/api/v1/query/range?node=${encodeURIComponent(row.node)}&metric=${encodeURIComponent(t.metric)}&labels.instance=${encodeURIComponent(row.instance)}&start=${start}&end=${end}&step=${step}`
        )
        const first = (data.series || [])[0]
        const series = first ? [{ name: t.label, data: (first.points || []).map((p) => [p.timestamp, p.value]) }] : []
        t.option = mwTrendOption(series, { unit: t.unit, yMax: t.yMax })
      } catch (e) {
        console.error('加载趋势数据失败', t.key, e)
        t.error = '趋势数据加载失败'
      } finally {
        t.loading = false
      }
    })
  )
}

// 切换时间范围后重查
watch(range, () => { if (detailVisible.value) loadTrendData() })

// ---- 格式化工具 ----
function formatBytes(b) {
  if (!b || b <= 0) return '0 B'
  if (b >= 1 << 30) return (b / (1 << 30)).toFixed(2) + ' GB'
  if (b >= 1 << 20) return (b / (1 << 20)).toFixed(2) + ' MB'
  if (b >= 1 << 10) return (b / (1 << 10)).toFixed(1) + ' KB'
  return b.toFixed(0) + ' B'
}
function formatBytesShort(b) {
  if (b >= 1 << 30) return (b / (1 << 30)).toFixed(0) + 'G'
  if (b >= 1 << 20) return (b / (1 << 20)).toFixed(0) + 'M'
  if (b >= 1 << 10) return (b / (1 << 10)).toFixed(0) + 'K'
  return b.toFixed(0) + 'B'
}
function formatNum(n) {
  if (!n) return '0'
  if (n >= 1000000) return (n / 1000000).toFixed(1) + 'M'
  if (n >= 1000) return (n / 1000).toFixed(1) + 'K'
  return n.toFixed(0)
}
function formatUptime(s) {
  if (!s || s <= 0) return '-'
  const d = Math.floor(s / 86400)
  const h = Math.floor((s % 86400) / 3600)
  if (d > 0) return `${d}天${h}时`
  const m = Math.floor((s % 3600) / 60)
  if (h > 0) return `${h}时${m}分`
  return `${m}分`
}
function roleLabel(r) {
  return { master: 'Master', slave: 'Slave', sentinel: 'Sentinel', unknown: '未知' }[r] || r
}
function topoLabel(t) {
  return { standalone: '单机', replication: '主从', sentinel: '哨兵', cluster: '集群', unknown: '未知' }[t] || t
}
function memBarClass(p) {
  if (p > 80) return 'red'
  if (p > 60) return 'amber'
  return 'green'
}
function hitRateClass(r) {
  if (r < 50) return 'sev-critical'
  if (r < 80) return 'sev-warning'
  return 'ok-text'
}
function rowClass({ row }) {
  if (!row.up) return 'row-down'
  return isAlert(row) ? 'row-issue' : ''
}

// 复制空状态引导命令到剪贴板
function copyCmd() {
  const cmd = redisInstallCmd.value
  if (navigator.clipboard) {
    navigator.clipboard.writeText(cmd).then(() => {
      ElMessage.success('命令已复制到剪贴板')
    }).catch(() => {})
  } else {
    // 降级：创建临时 textarea
    const ta = document.createElement('textarea')
    ta.value = cmd
    document.body.appendChild(ta)
    ta.select()
    try { document.execCommand('copy'); ElMessage.success('命令已复制到剪贴板') } catch (e) {}
    document.body.removeChild(ta)
  }
}

// ---- 生命周期 ----
onMounted(() => {
  loadInstances()
  loadServerURL()
})
</script>

<style scoped>
.mw-tab {
  padding: 4px 0;
}
.mw-err { margin-bottom: 16px; }
.mw-err-text { margin-right: 8px; }
.mw-block { margin-bottom: 16px; }

/* 区块1：KPI 卡片（使用共享 KpiCard 组件，样式见 KpiCard.vue） */
.kpi-row {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(140px, 1fr));
  gap: 10px;
  margin-bottom: 16px;
}

.chart-note {
  font-size: 13px;
  color: var(--t2);
  line-height: 1.5;
  margin-bottom: 8px;
}

/* 区块2：柱状图 */
.bar-row {
  display: grid;
  grid-template-columns: repeat(2, 1fr);
  gap: 16px;
}
.bar-sub-title {
  font-size: 13px;
  color: var(--t2);
  margin-bottom: 8px;
}

/* 表格 */
.topo-tag {
  display: inline-block;
  padding: 2px 8px;
  border-radius: 4px;
  font-size: 13px;
  background: rgba(56, 189, 248, 0.12);
  color: var(--info);
}
.mem-cell {
  display: flex;
  align-items: center;
  gap: 8px;
}
.mem-cell .bar {
  flex: 1;
  height: 5px;
  background: var(--fill-2);
  border-radius: 3px;
  overflow: hidden;
}
.mem-cell .bar-fill {
  height: 100%;
  border-radius: 3px;
}
.bar-fill.green { background: var(--accent); }
.bar-fill.amber { background: var(--warn); }
.bar-fill.red { background: var(--danger); }
.mem-text {
  font-size: 13px;
  min-width: 60px;
}
.mem-pct {
  font-size: 13px;
  color: var(--t2);
  min-width: 36px;
  text-align: right;
}
.issue-reasons {
  color: var(--warn);
  font-size: 13px;
}
.dim-text {
  color: var(--t2);
}
.pager {
  display: flex;
  justify-content: flex-end;
  margin-top: 12px;
}

/* 健康风险提示 */
.alert-summary {
  margin-bottom: 16px;
  padding: 14px 16px;
  border: 1px solid rgba(245, 158, 11, 0.28);
  background: rgba(245, 158, 11, 0.06);
}
.alert-summary-head {
  display: flex;
  align-items: center;
  gap: 8px;
  color: var(--warn);
  font-weight: 600;
  font-size: 14px;
}
.alert-summary-head svg {
  width: 18px;
  height: 18px;
}
.alert-summary-desc {
  margin-top: 8px;
  color: var(--t2);
  font-size: 13px;
  line-height: 1.5;
}
.alert-summary-list {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-top: 12px;
}
.issue-chip {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  padding: 6px 10px;
  border-radius: 999px;
  background: rgba(15, 23, 42, 0.58);
  border: 1px solid rgba(245, 158, 11, 0.22);
  color: var(--t1);
  font-size: 13px;
  cursor: pointer;
}
.issue-chip:hover {
  border-color: rgba(245, 158, 11, 0.55);
}
.issue-chip.muted {
  color: var(--t2);
  cursor: default;
}
.issue-chip-reason {
  color: var(--warn);
}
:deep(.row-down) {
  opacity: 0.6;
}
:deep(.row-issue) {
  --el-table-tr-bg-color: rgba(245, 158, 11, 0.04);
}
:deep(.el-table) {
  cursor: pointer;
}

/* 区块6：详情抽屉 */
.detail-content {
  padding: 0 4px;
}
.chart-grid {
  display: grid;
  grid-template-columns: repeat(2, 1fr);
  gap: 14px;
}
.trend-card {
  background: var(--fill-1);
  border: 1px solid var(--bd);
  border-radius: 10px;
  padding: 12px 14px;
}
.tc-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 6px;
}
.tc-label {
  font-size: 13px;
  color: var(--t2);
}
.tc-value {
  font-size: 16px;
  font-weight: 700;
  font-family: var(--mono);
}

/* 响应式 */
@media (max-width: 1280px) {
  .kpi-row { grid-template-columns: repeat(4, 1fr); }
  .bar-row { grid-template-columns: 1fr; }
  .chart-grid { grid-template-columns: 1fr; }
}

/* ==== 实例拓扑与集群关系 ==== */
.topo-group { margin-top: 16px; padding: 12px 14px; background: var(--fill-1); border: 1px solid var(--fill-2); border-radius: 10px; }
.topo-group:first-child { margin-top: 0; }
.topo-group-header { display: flex; align-items: center; justify-content: space-between; margin-bottom: 12px; gap: 12px; flex-wrap: wrap; }
.topo-group-title { display: inline-flex; align-items: center; gap: 8px; font-size: 14px; }
.topo-group-title svg { width: 18px; height: 18px; color: #93c5fd; }
.topo-meta { font-size: 13px; display: inline-flex; align-items: center; gap: 10px; }
.topo-meta .dim { color: var(--t3); }
.badge { display: inline-block; padding: 2px 8px; border-radius: 999px; font-size: 13px; font-weight: 500; }
.badge-ok { background: var(--ok-dim); color: var(--ok); }
.badge-warn { background: var(--warn-dim); color: var(--warn); }
.badge-down { background: var(--danger-dim); color: var(--danger); }

/* cluster 关系：center + 外围 master */
.topo-relation { display: flex; align-items: center; gap: 16px; flex-wrap: wrap; }
.rel-node { background: var(--fill-2); border: 1px solid var(--fill-3); border-radius: 10px; padding: 10px 14px; min-width: 160px; cursor: pointer; transition: all .2s ease; }
.rel-node:hover { background: var(--fill-3); border-color: var(--accent); }
.rel-node.is-down { opacity: .55; }
.rel-node.is-alert { border-color: rgba(239,68,68,0.5); box-shadow: 0 0 0 1px rgba(239,68,68,0.2); }
.rel-center { background: linear-gradient(135deg, rgba(99,102,241,0.25), rgba(139,92,246,0.18)); border-color: rgba(139,92,246,0.4); font-weight: 600; }
.rel-center::after { content: ''; }
.rel-master { border-color: rgba(59,130,246,0.35); }
.rel-sentinel { border-color: rgba(234,179,8,0.35); background: rgba(234,179,8,0.06); }
.rel-standalone { border-color: rgba(148,163,184,0.3); border-left: 3px solid rgba(148,163,184,0.55); }
.rel-edges { display: flex; gap: 12px; flex-wrap: wrap; flex: 1; }
.rel-edges-left, .rel-edges-right { flex: 1; min-width: 200px; }
.rel-edge { position: relative; }
.rel-edge::before { content: ''; position: absolute; left: -16px; top: 50%; width: 12px; height: 1px; background: linear-gradient(to right, rgba(139,92,246,0.5), rgba(59,130,246,0.5)); }
.topo-relation-sentinel { justify-content: space-between; }
.rel-arrow { color: rgba(234,179,8,0.8); font-weight: 600; font-size: 13px; padding: 0 4px; white-space: nowrap; }
.topo-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(220px, 1fr)); gap: 12px; }
.rel-node-name { font-weight: 600; font-size: 13px; margin-bottom: 4px; word-break: break-all; }
.rel-node-meta { display: flex; align-items: center; gap: 6px; font-size: 13px; color: var(--t2); flex-wrap: wrap; }
.rel-node-meta .dim { color: var(--t3); }

/* ==== 详情抽屉：关键指标快照 ==== */
.snapshot-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(180px, 1fr)); gap: 12px; margin: 16px 0 4px; }
.snap-card { background: var(--fill-1); border: 1px solid var(--fill-3); border-radius: 10px; padding: 12px 14px; }
.snap-card-wide { grid-column: span 2; }
.snap-label { font-size: 13px; color: var(--t2); margin-bottom: 6px; }
.snap-value { font-size: 18px; font-weight: 600; font-family: 'JetBrains Mono', ui-monospace, monospace; }
.snap-value.ok { color: var(--ok); }
.snap-value.warn { color: var(--danger); }

/* 实例列表：碎片率告警色 */
.rate-warn { color: var(--danger); }

/* ==== Cluster 拓扑：master → replicas（兼容哨兵组）==== */
.rel-edges { align-items: flex-start; }
.rel-master-block { display: flex; flex-direction: column; gap: 8px; min-width: 180px; }
.rel-slaves { display: flex; flex-direction: column; gap: 6px; padding-left: 14px; border-left: 2px dashed var(--accent); }
.rel-slave { display: flex; align-items: center; gap: 6px; font-size: 13px; padding: 6px 10px; background: rgba(99,102,241,0.06); border: 1px solid rgba(99,102,241,0.2); border-radius: 8px; cursor: pointer; transition: all .2s ease; flex-wrap: wrap; }
.rel-slave:hover { background: rgba(99,102,241,0.14); border-color: rgba(99,102,241,0.45); }
.rel-slave.is-down { opacity: .55; }
.rel-slave-tag { background: rgba(99,102,241,0.22); color: #a5b4fc; padding: 1px 6px; border-radius: 4px; font-size: 13px; font-weight: 600; }

/* ==== Master-Slave 层次树（复制 + 故障转移）==== */
.ms-tree { display: flex; flex-direction: column; gap: 14px; }
.ms-unit { display: flex; flex-direction: column; gap: 0; }
.ms-master { border-left: 3px solid transparent; min-width: 0; }
.ms-master .rel-node-name { display: flex; align-items: center; gap: 8px; }
.role-badge {
  display: inline-flex; align-items: center; justify-content: center;
  width: 18px; height: 18px; border-radius: 5px;
  font-size: 13px; font-weight: 700; flex-shrink: 0;
}
.role-badge-m { background: rgba(220,56,45,0.22); color: #ff8a80; border: 1px solid rgba(220,56,45,0.4); }
.role-badge-s { background: rgba(34,197,94,0.18); color: var(--ok); border: 1px solid rgba(34,197,94,0.35); width: 16px; height: 16px; font-size: 13px; border-radius: 4px; }
.slot-chip {
  font-size: 13px; font-family: var(--mono);
  padding: 1px 8px; border-radius: 4px; border: 1px dashed;
}
/* 分支：左轨为复制实线（向下），右侧虚线为 failover（向上）
   注：ms-branch-rail / ms-rail-repl / ms-rail-fo 等强化样式见下方"强化 rail 方向箭头"段 */
.ms-branch { display: flex; gap: 0; margin-left: 10px; }
.ms-slaves { display: flex; flex-direction: column; gap: 6px; flex: 1; padding: 4px 0; }
.ms-slave { position: relative; }
.ms-slave::before {
  content: ''; position: absolute; left: -10px; top: 50%;
  width: 10px; height: 1px; background: var(--accent);
}

/* ==== 拓扑图例 ==== */
.topo-legend {
  display: flex; gap: 20px; margin-top: 12px; padding-top: 10px;
  border-top: 1px dashed var(--fill-3);
}
.topo-legend-item { display: inline-flex; align-items: center; gap: 8px; font-size: 13px; color: var(--t3); }
.legend-line { display: inline-block; width: 24px; height: 0; }
.legend-solid { border-top: 2px solid var(--accent); }
.legend-dash { border-top: 2px dashed rgba(245,158,11,0.65); }

/* ==== 多集群横向并排（auto-fill），单集群自适应 ==== */
.topo-clusters-grid {
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  gap: 14px;
  margin-top: 12px;
  width: 100%;
  min-width: 0;
}
.topo-cluster-card {
  margin-top: 0;
  width: 100%;
  min-width: 0;
  height: fit-content;
}

/* ==== 主从复制 + 故障转移强化 ==== */
.ms-section { margin-top: 12px; }
.ms-section-label {
  display: flex; gap: 14px; margin-bottom: 10px; font-size: 13px; flex-wrap: wrap;
  letter-spacing: 0.02em;
}
.ms-label-repl { color: rgba(147,197,253,0.9); }
.ms-label-fo { color: rgba(251,191,36,0.9); }
.ms-rail-repl { font-size: 13px; color: rgba(147,197,253,0.9); writing-mode: vertical-lr; letter-spacing: 1px; font-weight: 600; }
.ms-rail-fo { font-size: 13px; color: rgba(251,191,36,0.9); writing-mode: vertical-lr; letter-spacing: 1px; font-weight: 600; }

/* 从节点完整卡片：在线状态/ops/lag/内存 */
.ms-slave-card {
  display: flex; flex-direction: column; align-items: flex-start;
  gap: 4px; min-width: 200px; max-width: 100%;
  padding: 8px 12px;
  background: rgba(34,197,94,0.06);
  border: 1px solid rgba(34,197,94,0.28);
  border-left: 3px solid rgba(34,197,94,0.55);
  cursor: pointer; transition: all .2s ease;
}
.ms-slave-card:hover { background: rgba(34,197,94,0.12); border-color: rgba(34,197,94,0.5); }
.ms-slave-card.is-down { opacity: .55; background: rgba(239,68,68,0.05); border-color: rgba(239,68,68,0.3); border-left-color: rgba(239,68,68,0.55); }
.ms-slave-head { display: flex; align-items: center; gap: 6px; font-weight: 600; font-size: 13px; }
.ms-slave-meta { display: flex; align-items: center; gap: 6px; font-size: 13px; color: var(--t2); flex-wrap: wrap; }
.ms-slave-meta .dim { color: var(--t3); }

/* ==== 未关联主节点的从节点（replicaOf 为空）==== */
.unlinked-block {
  margin-top: 12px; padding: 10px 14px;
  background: rgba(148,163,184,0.04); border: 1px dashed rgba(148,163,184,0.2);
  border-radius: 8px;
}
.unlinked-label {
  font-size: 13px; color: var(--t2); margin-bottom: 8px;
  display: flex; align-items: center; gap: 6px;
}
.unlinked-list { display: flex; flex-wrap: wrap; gap: 8px; }

/* ==== Redis Cluster 横向分组拓扑：masters 横排，replicas 组内竖排 ==== */
.topo-cluster-card .ms-tree {
  display: flex;
  flex-direction: row;
  align-items: stretch;
  gap: 14px;
  width: 100%;
  min-width: 0;
  overflow-x: auto;
  padding: 2px 2px 8px;
}
.topo-cluster-card .ms-unit {
  display: flex;
  flex-direction: column;
  align-items: stretch;
  flex: 1 1 260px;
  min-width: 240px;
  max-width: 100%;
}
.topo-cluster-card .ms-master {
  width: 100%;
}
.topo-cluster-card .ms-branch {
  flex-direction: column;
  align-items: stretch;
  margin-left: 0;
  margin-top: 10px;
}
/* 集群拓扑 rail 位于从节点上方，改为横向排列避免竖排文字重叠 */
.topo-cluster-card .ms-branch-rail {
  width: auto;
  height: auto;
  min-height: 26px;
  border-left: 0;
  border-bottom: 2px solid rgba(147,197,253,0.65);
  display: flex;
  flex-direction: row;
  align-items: center;
  justify-content: flex-start;
  gap: 18px;
  padding: 6px 0 6px 4px;
  margin: 0 0 8px 0;
}
.topo-cluster-card .ms-branch-rail::after {
  content: none;
}
.topo-cluster-card .ms-rail-repl,
.topo-cluster-card .ms-rail-fo {
  position: relative;
  z-index: 1;
  writing-mode: horizontal-tb;
  white-space: nowrap;
  letter-spacing: 0.5px;
}
.topo-cluster-card .ms-slaves {
  flex-direction: column;
  align-items: stretch;
  gap: 8px;
  padding: 0;
}
.topo-cluster-card .ms-slave-card {
  min-width: 0;
  width: 100%;
  padding: 7px 9px;
}

/* 宽屏下三组尽量完整显示；窄屏再退化为横向滚动 */
@media (max-width: 1024px) {
  .topo-cluster-card .ms-tree {
    overflow-x: auto;
  }
  .topo-cluster-card .ms-unit {
    flex: 0 0 260px;
    min-width: 240px;
  }
}

/* ==== 强化 rail 方向箭头（复制 ↓ 实线 + 故障转移 ↑ 虚线）==== */
.ms-branch-rail {
  width: 64px; flex-shrink: 0;
  border-left: 2px solid rgba(147,197,253,0.65);
  display: flex; flex-direction: column; justify-content: space-between;
  padding: 2px 0 2px 8px; margin: 4px 0;
  position: relative;
}
.ms-branch-rail::after {
  content: ''; position: absolute; right: 8px; top: 4px; bottom: 4px;
  border-right: 2px dashed rgba(251,191,36,0.7);
}
.ms-rail-repl {
  font-size: 13px; color: rgba(147,197,253,0.9);
  writing-mode: vertical-lr; letter-spacing: 1px; font-weight: 600;
}
.ms-rail-fo {
  font-size: 13px; color: rgba(251,191,36,0.9);
  writing-mode: vertical-lr; letter-spacing: 1px; font-weight: 600;
  text-align: right;
}
</style>

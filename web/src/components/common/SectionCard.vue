<template>
  <section class="card sec-card">
    <header v-if="title || $slots.actions" class="sec-head">
      <div class="sec-title-wrap">
        <span v-if="title" class="accent-bar"></span>
        <div class="sec-title-text">
          <h3 v-if="title" class="sec-title">{{ title }}</h3>
          <p v-if="subtitle" class="sec-sub">{{ subtitle }}</p>
        </div>
      </div>
      <div class="sec-actions">
        <slot name="actions" />
      </div>
    </header>

    <div class="sec-body" :class="{ dense }">
      <!-- 四态优先级：错误 > 加载 > 内容。
           错误与加载是"本次这一块没拿到数据"，优先于展示（可能是旧的）内容。 -->
      <div v-if="error" class="sec-error">
        <el-icon class="sec-error-icon"><WarningFilled /></el-icon>
        <span class="sec-error-msg">{{ error }}</span>
        <el-button v-if="errorRetry" size="small" @click="$emit('retry')">重试</el-button>
      </div>
      <div v-else-if="loading" class="sec-skeleton" aria-busy="true" aria-label="加载中">
        <div class="sk sk-row" style="width: 58%"></div>
        <div class="sk sk-row" style="width: 86%"></div>
        <div class="sk sk-row" style="width: 72%"></div>
      </div>
      <slot v-else />
    </div>

    <footer v-if="$slots.footer" class="sec-foot">
      <slot name="footer" />
    </footer>
  </section>
</template>

<script setup>
import { WarningFilled } from '@element-plus/icons-vue'

// 全站统一容器：替代 .glass / .kpi / .ov-card / .panel 四种并存写法。
// dense 用于表格类高密度区域（内边距 16px → 12px）。
// loading / error 内置四态里的"加载骨架"与"错误带重试"，各页不再各写一份。
defineProps({
  title: { type: String, default: '' },
  subtitle: { type: String, default: '' },
  dense: { type: Boolean, default: false },
  // 加载中：以骨架替代默认插槽内容（用于"首次加载/切页"）。
  loading: { type: Boolean, default: false },
  // 错误信息（非空即展示错误条，优先于内容）。空串 = 无错误。
  error: { type: String, default: '' },
  // 是否展示"重试"按钮（点了 emit('retry')）。
  errorRetry: { type: Boolean, default: false },
})
defineEmits(['retry'])
</script>

<style scoped>
.sec-card {
  display: flex;
  flex-direction: column;
  min-width: 0;
}
.sec-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  /* 与 PageHeader 同一套规则：操作区不收缩，窄屏时让它换行而不是把标题挤没 */
  flex-wrap: wrap;
  padding: 12px 16px;
  border-bottom: 1px solid var(--bd);
}
.sec-title-wrap {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}
.accent-bar {
  width: 3px;
  height: 14px;
  border-radius: 2px;
  background: var(--accent);
  flex-shrink: 0;
}
.sec-title-text {
  min-width: 0;
}
.sec-title {
  font-size: var(--fs-base);
  font-weight: 600;
  color: var(--t1);
  letter-spacing: 0.01em;
}
.sec-sub {
  font-size: var(--fs-xs);
  color: var(--t3);
  margin-top: 2px;
}
.sec-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-shrink: 0;
  flex-wrap: wrap;
}
.sec-body {
  padding: 16px;
  min-width: 0;
  /* 卡片被网格行/父容器限高时必须能收缩：flex 子项默认 min-height:auto 不会收缩，
     内部表格会顶破卡片底边与圆角。 */
  min-height: 0;
  flex: 1;
}
.sec-body.dense {
  padding: 12px;
}
.sec-foot {
  padding: 9px 16px;
  border-top: 1px solid var(--bd);
  font-size: var(--fs-xs);
  color: var(--t3);
}

/* —— 内置加载骨架 —— */
.sec-skeleton {
  padding: 4px 0;
}
.sec-skeleton .sk-row {
  height: 12px;
  margin-bottom: 10px;
}
.sec-skeleton .sk-row:last-child {
  margin-bottom: 0;
}

/* —— 内置错误条 —— */
.sec-error {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 10px 12px;
  border: 1px solid var(--danger-bd);
  background: var(--danger-dim);
  border-radius: var(--r-md);
  font-size: var(--fs-sm);
  color: var(--t1);
}
.sec-error-icon {
  color: var(--danger);
  flex-shrink: 0;
}
.sec-error-msg {
  flex: 1;
  min-width: 0;
  overflow-wrap: break-word;
}
</style>

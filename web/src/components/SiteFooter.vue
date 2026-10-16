<template>
  <footer class="site-footer">
    <div class="site-footer__inner">
      <div class="site-footer__brand">
        <span class="site-footer__name">{{ brandName }}</span>
        <span class="site-footer__ver" :title="`前端构建版本 ${version}`">v{{ version }}</span>
      </div>
      <div v-if="footerText" class="site-footer__meta">{{ footerText }}</div>
    </div>
  </footer>
</template>

<script setup>
import { computed } from 'vue'
import { useBrand } from '../composables/useBrand'
import { WEB_VERSION } from '../version'

const { brand } = useBrand()
const brandName = computed(() => brand.name || '守微')
// 版本号来自 build 生成的 src/version.js（与 server/agent 经 ldflags 注入的版本同源）
const version = WEB_VERSION
const footerText = computed(() => brand.footer || '')
</script>

<style scoped>
.site-footer {
  flex: 0 0 auto;
  width: 100%;
  padding: 10px 20px;
  color: var(--t3);
  border-top: 1px solid var(--bd);
  background: var(--s1);
  font-size: var(--fs-sm);
}

.site-footer__inner {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  max-width: 1440px;
  margin: 0 auto;
  min-width: 0;
}

.site-footer__brand {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}

.site-footer__name {
  color: var(--t2);
  font-weight: 600;
  letter-spacing: 0.02em;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.site-footer__ver {
  font-family: var(--mono);
  color: var(--t3);
  white-space: nowrap;
}

.site-footer__meta {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  text-align: right;
}

@media (max-width: 768px) {
  .site-footer { padding: 10px 16px; }
  .site-footer__inner { flex-direction: column; align-items: flex-start; gap: 4px; }
  .site-footer__meta { text-align: left; }
}
</style>

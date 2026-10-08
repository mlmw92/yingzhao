<template>
  <div class="ai-diagnose">
    <el-button size="small" :loading="loading" @click="run">AI 诊断</el-button>

    <el-dialog v-model="visible" title="AI 诊断建议" width="min(720px, 94vw)" append-to-body>
      <div v-if="loading" class="state">正在请求模型…（大模型通常要几秒到几十秒）</div>

      <el-alert v-else-if="error" :title="error" type="error" :closable="false" show-icon />

      <template v-else-if="result">
        <div class="meta">
          <span>模型 {{ result.model }}</span>
          <span>耗时 {{ (result.elapsedMs / 1000).toFixed(1) }}s</span>
          <span>token {{ result.totalTokens }}</span>
          <el-tag v-if="result.sensitive" type="warning" size="small">含敏感内容</el-tag>
          <el-tag v-if="result.truncated" type="info" size="small">上下文已截断</el-tag>
        </div>
        <!-- 未发送的内容要说出来：否则用户会奇怪「为什么它没提到我的 IP / 连接串」 -->
        <div v-if="result.notSent && result.notSent.length" class="not-sent">
          未发送给模型：{{ result.notSent.join('、') }}
        </div>
        <!-- 直接显示原始文本（不渲染 markdown）：不引依赖，也避免把模型输出的 HTML 当代码执行 -->
        <pre class="advice">{{ result.advice }}</pre>
        <div class="foot-note">
          以上为模型给出的排查线索，**仅供参考**；平台不会自动执行任何处置动作。
        </div>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref } from 'vue'
import { ElMessage } from 'element-plus'
import http from '../../api/http'

const props = defineProps({
  // node = 主机名；asset = 资产 ID；alert = 活跃告警 ID
  kind: { type: String, required: true },
  id: { type: [String, Number], default: '' },
})

const visible = ref(false)
const loading = ref(false)
const error = ref('')
const result = ref(null)

async function run() {
  if (props.id === '' || props.id === null || props.id === undefined) {
    ElMessage.warning('缺少对象标识，无法诊断')
    return
  }
  visible.value = true
  loading.value = true
  error.value = ''
  result.value = null
  try {
    result.value = await http.diagnoseAI({ kind: props.kind, id: String(props.id) })
  } catch (e) {
    error.value = e.message || '诊断失败'
  } finally {
    loading.value = false
  }
}
</script>

<style scoped>
/* inline-flex：这个组件会被放进别处的按钮组里，块级元素会把那一行撑开 */
.ai-diagnose {
  display: inline-flex;
}
.state {
  color: var(--text-muted);
  font-size: 13px;
  padding: 8px 0;
}
.meta {
  display: flex;
  align-items: center;
  gap: 12px;
  flex-wrap: wrap;
  font-size: 12px;
  color: var(--text-muted);
  margin-bottom: 10px;
}
.not-sent {
  font-size: 12px;
  color: var(--text-muted);
  padding: 6px 10px;
  border-radius: 6px;
  background: var(--fill-1, rgba(148, 163, 184, 0.08));
  margin-bottom: 10px;
}
.advice {
  white-space: pre-wrap;
  word-break: break-word;
  font-family: inherit;
  font-size: 13px;
  line-height: 1.75;
  color: var(--text);
  margin: 0;
  max-height: 52vh;
  overflow: auto;
}
.foot-note {
  margin-top: 12px;
  font-size: 12px;
  color: var(--text-muted);
}
</style>

<!--
  资产「其它属性」编辑器：新建与编辑弹窗共用同一段。

  为什么单独抽出来：此前新建与编辑两处各抄一份同样的「键 + 按字典渲染的值 + 删除」，
  字典控件一改就要改两遍、容易漏一处。字典（dict）来自资产类型的 schema.attrMeta，
  只影响控件形态（enum 下拉 / 日期选择 / 布尔开关 / 数字），服务端始终是权威校验。

  rows 由父组件持有；这里就地增删改（props 是浅只读，数组/对象内部可变），
  父组件负责把 rows 提交出去。
-->
<template>
  <div class="attr-editor">
    <div v-for="(row, i) in rows" :key="i" class="attr-row">
      <el-input v-model="row.key" placeholder="属性名" style="width: 42%" />
      <!-- 值按该字段的字典渲染：enum 下拉 / 日期选择 / 布尔开关 / 数字输入。
           字典没配的字段仍是自由文本（今天的形态）；服务端始终是权威校验。 -->
      <el-select
        v-if="kindOf(row.key) === 'enum'"
        v-model="row.value"
        placeholder="选择取值"
        style="width: 42%"
      >
        <el-option
          v-for="opt in enumOf(row.key)"
          :key="opt.value"
          :label="opt.label || opt.value"
          :value="opt.value"
        />
      </el-select>
      <el-date-picker
        v-else-if="kindOf(row.key) === 'date'"
        v-model="row.value"
        type="date"
        value-format="YYYY-MM-DD"
        placeholder="选择日期"
        style="width: 42%"
      />
      <el-switch
        v-else-if="kindOf(row.key) === 'bool'"
        v-model="row.value"
        active-value="true"
        inactive-value="false"
      />
      <el-input
        v-else
        v-model="row.value"
        :placeholder="kindOf(row.key) === 'number' ? '数字' : '值'"
        style="width: 42%"
      />
      <el-button link type="danger" @click="removeRow(i)">删除</el-button>
    </div>
    <el-button link type="primary" @click="addRow">+ 添加属性</el-button>
  </div>
</template>

<script setup>
import { attrKind, attrEnum } from './assetFormat'

const props = defineProps({
  // 属性行 [{ key, value }]，由父组件持有并提交
  rows: { type: Array, required: true },
  // 当前资产类型的字段字典：{ [key]: attrMeta }
  dict: { type: Object, default: () => ({}) },
})

const metaOf = (key) => props.dict[(key || '').trim()] || null
const kindOf = (key) => attrKind(metaOf(key))
const enumOf = (key) => attrEnum(metaOf(key))

function addRow() {
  props.rows.push({ key: '', value: '' })
}
function removeRow(i) {
  props.rows.splice(i, 1)
}
</script>

<style scoped>
.attr-editor {
  width: 100%;
}
.attr-row {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 8px;
}
</style>

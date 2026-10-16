// 容器页的查询状态缓存（模块级，跨路由切换保留）。
//
// 状态放在组件之外，只为**刷新期间不白屏**：换集群 / 换 Tab 时旧结果继续显示，
// 随即被新结果替换。
//
// 它**不是"避免重复查询"的手段**：2026-10-10 重规划（ADR-0006）之后，
// K8s 与 Docker 的读都是 Server 直连（点开即得），每次换视图都直接查。
// 也**不再记录"上次刷新时间"**（2026-10-10 用户要求去掉）：那行时间戳当初是为
// 异步下发写的——要让"等一个上报周期"可解释；直连之后它只剩噪音。
//
// 缓存键是「集群 + 能力」而不是只按能力：换集群是**换视图**、不是刷新，
// 键混在一起就会把 A 集群的表显示在 B 集群下面。
import { reactive } from 'vue'

const states = reactive({})

// containerQueryState 取（或建）某个「集群 + 能力」的查询状态。
export function containerQueryState(clusterKey, cap) {
  const key = clusterKey + '|' + cap
  if (!states[key]) states[key] = newState()
  return states[key]
}

// containerDetailState 是详情 / 日志抽屉的状态，键里带上对象身份：
// 换对象是换内容，不能把上一个对象的内容显示在新标题下。
export function containerDetailState(clusterKey, kind, namespace, name) {
  const key = 'detail|' + clusterKey + '|' + kind + '|' + namespace + '|' + name
  if (!states[key]) states[key] = newState()
  return states[key]
}

function newState() {
  // ns 是这份结果**当初是按哪个命名空间查的**：结果会保留，而输入框里的条件随时会变。
  // 不记下它，就会出现"输入框写着 default、表里却是全部命名空间的对象"而界面一言不发
  // ——那比数据旧更糟。
  return { busy: false, error: '', result: null, ns: '' }
}

// resetContainerQueryCache 清空缓存（仅测试用：避免用例之间互相污染）。
export function resetContainerQueryCache() {
  for (const key of Object.keys(states)) delete states[key]
}

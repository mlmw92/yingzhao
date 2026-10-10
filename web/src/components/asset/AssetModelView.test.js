// 配置项模型页的**属性字典**回归测试（设计件 2026-10-08-asset-attr-dictionary-design.md）。
//
// 为什么要有这个文件：字典字段（kind / enum）是"只在人配了才有用"的配置，
// 最典型的失效方式是**被前端的「三个说明都空就丢掉」判定吃掉**——界面上看着配好了，
// 提交时却什么也没发出去，而后端只能存下空模型。这条只能靠"点一下再检查提交体"来守。
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { nextTick } from 'vue'
import ElementPlus from 'element-plus'
import * as assetApi from '../../api/asset'
import AssetModelView from './AssetModelView.vue'

// ElMessageBox 是命令式调用（不是组件），没法用 global.stub 拦。
// 这里做**部分** mock：保留真实 ElementPlus 插件（mount 要用），只把 confirm 换成 spy。
const { confirmMock } = vi.hoisted(() => ({ confirmMock: vi.fn() }))
vi.mock('element-plus', async (importOriginal) => {
  const actual = await importOriginal()
  return { ...actual, ElMessageBox: { ...actual.ElMessageBox, confirm: confirmMock } }
})

vi.mock('../../api/asset', () => ({
  getAssetTypes: vi.fn(),
  saveAssetTypeModel: vi.fn(),
}))

vi.mock('../../composables/useAuth', () => ({
  useAuth: () => ({ can: () => true }),
}))

const TYPES = [{
  key: 'host',
  title: '主机',
  builtin: true,
  ephemeral: false,
  assets: 2,
  schema: { runtimeFields: [], runtimeFieldsDefault: true, focusFields: [], attrMeta: {} },
  attrs: [{ key: 'env', assets: 2, coverage: 1, discovery: 2, manual: 0 }],
}]

let wrappers = []

function mountView() {
  const w = mount(AssetModelView, { global: { plugins: [ElementPlus] }, attachTo: document.body })
  wrappers.push(w)
  return w
}

beforeEach(() => {
  vi.clearAllMocks()
  confirmMock.mockResolvedValue('confirm')
  wrappers = []
  assetApi.getAssetTypes.mockResolvedValue({ types: TYPES })
  assetApi.saveAssetTypeModel.mockResolvedValue({ types: TYPES })
})

afterEach(() => {
  for (const w of wrappers) w.unmount()
})

describe('配置项模型页的属性字典', () => {
  it('渲染出「类型」与「枚举取值」两列', async () => {
    const w = mountView()
    await flushPromises()
    expect(w.text()).toContain('类型')
    expect(w.text()).toContain('枚举取值')
    expect(w.text()).toContain('env')
  })

  it('字段类型会随模型一起提交（不能被「三项都空就丢」吃掉）', async () => {
    const w = mountView()
    await flushPromises()

    // 只改类型、不填任何说明文字——这正是最容易被丢掉的形态
    const selects = w.findAllComponents({ name: 'ElSelect' })
    expect(selects.length).toBeGreaterThan(0)
    await selects[0].vm.$emit('update:modelValue', 'number')
    await flushPromises()

    const saveBtn = w.findAll('button').find((b) => b.text().includes('保存'))
    expect(saveBtn).toBeTruthy()
    await saveBtn.trigger('click')
    await flushPromises()

    expect(assetApi.saveAssetTypeModel).toHaveBeenCalled()
    const [key, body] = assetApi.saveAssetTypeModel.mock.calls[0]
    expect(key).toBe('host')
    expect(body.attrMeta.env).toBeTruthy()
    expect(body.attrMeta.env.kind).toBe('number')
  })

  it('建议属性一键加入编辑态，并能随模型提交（生命周期/成本/维保）', async () => {
    const w = mountView()
    await flushPromises()

    const openBtn = w.findAll('button').find((b) => b.text().includes('从模板新建'))
    expect(openBtn).toBeTruthy()
    await openBtn.trigger('click')
    await flushPromises()

    const group = w.findComponent({ name: 'ElCheckboxGroup' })
    expect(group.exists()).toBe(true)
    await group.vm.$emit('update:modelValue', ['lifecycle', 'warrantyUntil'])
    await flushPromises()

    const addBtn = w.findAll('button').find((b) => b.text().trim() === '加入编辑态')
    expect(addBtn).toBeTruthy()
    await addBtn.trigger('click')
    await flushPromises()

    // 刚加的字段必须出现在表里（否则用户看不到自己加的东西）
    expect(w.text()).toContain('lifecycle')
    expect(w.text()).toContain('warrantyUntil')

    const saveBtn = w.findAll('button').find((b) => b.text().includes('保存'))
    await saveBtn.trigger('click')
    await flushPromises()

    const body = assetApi.saveAssetTypeModel.mock.calls[0][1]
    expect(body.attrMeta.lifecycle.kind).toBe('enum')
    expect(body.attrMeta.lifecycle.enum.length).toBe(4)
    expect(body.attrMeta.warrantyUntil.kind).toBe('date')
  })

  it('已有的字典回填到编辑态（切类型不丢配置）', async () => {
    assetApi.getAssetTypes.mockResolvedValue({
      types: [{
        ...TYPES[0],
        schema: {
          runtimeFields: [], runtimeFieldsDefault: true, focusFields: [],
          attrMeta: { env: { title: '环境', kind: 'enum', enum: [{ value: 'prod', label: '生产' }] } },
        },
      }],
    })
    const w = mountView()
    await flushPromises()
    // 枚举项数应回填成 1
    expect(w.text()).toContain('配置（1 项）')

    const saveBtn = w.findAll('button').find((b) => b.text().includes('保存'))
    await saveBtn.trigger('click')
    await flushPromises()
    const body = assetApi.saveAssetTypeModel.mock.calls[0][1]
    expect(body.attrMeta.env.kind).toBe('enum')
    expect(body.attrMeta.env.enum).toEqual([{ value: 'prod', label: '生产' }])
  })

  // 这条守的是"声明了但没有资产携带"的字段要能被看懂：
  // 覆盖度画成 0% 空条会被读成"采集坏了"，而实际语义是"还没数据"。
  // 前端必须把「未携带 / 新增」说清楚，否则用户保存完去巡检比对里找不到差异，会当成功能坏了。
  it('没有资产携带的字段标为「未携带」，本次新增的另标「新增」，且不阻断保存', async () => {
    const w = mountView()
    await flushPromises()

    const openBtn = w.findAll('button').find((b) => b.text().includes('从模板新建'))
    await openBtn.trigger('click')
    await flushPromises()

    const group = w.findComponent({ name: 'ElCheckboxGroup' })
    await group.vm.$emit('update:modelValue', ['warrantyUntil'])
    await flushPromises()

    const addBtn = w.findAll('button').find((b) => b.text().trim() === '加入编辑态')
    await addBtn.trigger('click')
    await flushPromises()

    // 逐行标注：新增字段带「新增」角标，覆盖度列不画空进度条
    expect(w.text()).toContain('新增')
    expect(w.text()).toContain('未携带')
    // 已有的 env（2 台携带）仍走进度条口径，显示 2/2
    expect(w.text()).toContain('2/2')

    // 提示必须讲清"保存不会给资产赋值"
    expect(w.text()).toContain('保存只改模型定义，不会给任何资产赋值')

    // 关键：**不能**用模态框拦保存——建议属性是用户主动加的，每次都弹会变成噪音
    const saveBtn = w.findAll('button').find((b) => b.text().includes('保存'))
    await saveBtn.trigger('click')
    await flushPromises()

    expect(assetApi.saveAssetTypeModel).toHaveBeenCalled()
    const body = assetApi.saveAssetTypeModel.mock.calls[0][1]
    expect(body.attrMeta.warrantyUntil.kind).toBe('date')
  })
})

// —— 批次 12（PS-10）：把模型补成真正的 CRUD，并让编辑过程不丢数据 ——
//
// 这几条守的都是"功能缺口"而非体验瑕疵：
//   ① 字段增/删没有入口 → 自定义字段键根本建不出来、模型也删不掉；
//   ② 改了半天切个类型全没了（无未保存提示、无吸底保存）；
//   ③ 运行态字段清单与「比对」列各说各话。
describe('配置项模型页的字段增删与未保存改动', () => {
  it('「+ 新建字段」能建出自定义字段键，并随模型提交', async () => {
    const w = mountView()
    await flushPromises()

    await w.findAll('button').find((b) => b.text().includes('新建字段')).trigger('click')
    await flushPromises()
    w.vm.newField.key = 'costCenter'
    // 字段至少要有一样配置（模型的 attrMeta 不存空项），否则保存后不会留下它
    w.vm.newField.title = '成本中心'
    await nextTick()
    await w.findAll('button').find((b) => b.text().trim() === '加入编辑态').trigger('click')
    await flushPromises()

    // 新字段必须出现在表里（否则用户看不到自己加的东西）
    expect(w.text()).toContain('costCenter')

    await w.findAll('button').find((b) => b.text().includes('保存')).trigger('click')
    await flushPromises()
    const body = assetApi.saveAssetTypeModel.mock.calls.at(-1)[1]
    expect(body.attrMeta.costCenter).toBeTruthy()
    expect(body.attrMeta.costCenter.title).toBe('成本中心')
  })

  it('只填字段键、不给任何配置时拒绝入库（服务端不存空项）', async () => {
    const w = mountView()
    await flushPromises()

    await w.findAll('button').find((b) => b.text().includes('新建字段')).trigger('click')
    await flushPromises()
    w.vm.newField.key = 'emptyField'
    await nextTick()
    await w.findAll('button').find((b) => b.text().trim() === '加入编辑态').trigger('click')
    await flushPromises()

    // 没有加进编辑态
    expect(w.vm.attrRows.some((r) => r.key === 'emptyField')).toBe(false)
  })

  it('「删除」移除字段的模型配置，且保存体里不再包含它', async () => {
    const w = mountView()
    await flushPromises()

    const delBtn = w.findAll('button').find((b) => b.text().trim() === '删除')
    expect(delBtn).toBeTruthy()
    await delBtn.trigger('click')
    await flushPromises()
    expect(confirmMock).toHaveBeenCalled()

    // 行从表里消失（attrRows 过滤掉 removed）
    expect(w.vm.attrRows.some((r) => r.key === 'env')).toBe(false)

    await w.findAll('button').find((b) => b.text().includes('保存')).trigger('click')
    await flushPromises()
    const body = assetApi.saveAssetTypeModel.mock.calls.at(-1)[1]
    expect(body.attrMeta.env).toBeUndefined()
  })

  it('吸底保存栏显示未保存改动计数（改一处 → 1 处）', async () => {
    const w = mountView()
    await flushPromises()
    expect(w.text()).toContain('没有未保存的改动')

    const selects = w.findAllComponents({ name: 'ElSelect' })
    await selects[0].vm.$emit('update:modelValue', 'number')
    await flushPromises()

    expect(w.text()).toContain('有 1 处未保存的改动')
  })

  it('运行态字段清单与「比对」列双向联动（改档位→标签出现；× →改回默认）', async () => {
    const w = mountView()
    await flushPromises()
    expect(w.vm.runtimeKeys).toEqual([])

    // 把 env 的比对档位改成运行态
    w.vm.draft.mode.env = 'runtime'
    await nextTick()
    expect(w.vm.runtimeKeys).toContain('env')

    // 顶部标签的 × = 改回「默认」
    const close = w.find('.el-tag__close')
    expect(close.exists()).toBe(true)
    await close.trigger('click')
    await flushPromises()
    expect(w.vm.runtimeKeys).not.toContain('env')
  })
})

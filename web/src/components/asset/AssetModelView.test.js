// 配置项模型页的**属性字典**回归测试（设计件 2026-10-08-asset-attr-dictionary-design.md）。
//
// 为什么要有这个文件：字典字段（kind / enum）是"只在人配了才有用"的配置，
// 最典型的失效方式是**被前端的「三个说明都空就丢掉」判定吃掉**——界面上看着配好了，
// 提交时却什么也没发出去，而后端只能存下空模型。这条只能靠"点一下再检查提交体"来守。
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import * as assetApi from '../../api/asset'
import AssetModelView from './AssetModelView.vue'

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

    const openBtn = w.findAll('button').find((b) => b.text().includes('添加建议属性'))
    expect(openBtn).toBeTruthy()
    await openBtn.trigger('click')
    await flushPromises()

    const group = w.findComponent({ name: 'ElCheckboxGroup' })
    expect(group.exists()).toBe(true)
    await group.vm.$emit('update:modelValue', ['lifecycle', 'warrantyUntil'])
    await flushPromises()

    const addBtn = w.findAll('button').find((b) => b.text().trim() === '添加')
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
})

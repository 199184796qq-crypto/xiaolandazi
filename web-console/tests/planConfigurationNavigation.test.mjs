import test, { before, after } from 'node:test'
import assert from 'node:assert/strict'
import { createServer } from 'vite'
import { createSSRApp } from 'vue'
import { renderToString } from 'vue/server-renderer'

let server, navigation
before(async () => {
  server = await createServer({ server: { middlewareMode: true }, appType: 'custom' })
  navigation = (await server.ssrLoadModule('/src/components/PlanConfigurationNavigation.vue')).default
})
after(async () => { await server?.close() })
const render = props => renderToString(createSSRApp(navigation, props))

test('进入事实深链时仍显示配置和可定位的风险提示', async () => {
  const html = await render({ mode: 'knowledge', productRiskCount: 2, factRiskCount: 3, interactionPending: 4 })
  assert.ok(html.includes('aria-label="智能体方案配置分类"'))
  assert.ok(html.includes('aria-label="商品与事实栏目"'))
  assert.ok(html.includes('3项待处理'))
  assert.ok(html.includes('5项待处理'))
  assert.ok(html.includes('4条互动待纠正'))
  assert.equal((html.match(/aria-current="page"/g) || []).length, 2)
})

test('直播模式位于声音右侧，互动及直播模式不展示商品子栏目', async () => {
  for (const mode of ['memory', 'addressing', 'fullshow']) {
    const html = await render({ mode })
    assert.equal(html.includes('aria-label="商品与事实栏目"'), false)
    assert.equal((html.match(/aria-current="page"/g) || []).length, 1)
    assert.equal((html.match(/<button\b/g) || []).length, 6)
    assert.ok(html.indexOf('声音') < html.indexOf('直播模式'))
    assert.equal(html.includes('生成与预览'), false)
    if (mode === 'fullshow') {
      const activeButton = html.match(/<button\b[^>]*aria-current="page"[^>]*>[\s\S]*?<\/button>/)?.[0]
      assert.ok(activeButton?.includes('直播模式'))
    }
  }
})

test('资料补充位于活动福利右边、事实依据左边，属于商品与事实', async () => {
  const html = await render({ mode: 'supplement' })
  const labels = ['商品资料', '活动福利', '商品资料补充', '事实依据']
  assert.ok(labels.every(label => html.includes(label)))
  assert.equal(html.includes('任务讲解'), false)
  assert.ok(html.indexOf('活动福利') < html.indexOf('商品资料补充'))
  assert.ok(html.indexOf('商品资料补充') < html.indexOf('事实依据'))
  assert.equal((html.match(/aria-current="page"/g) || []).length, 2)
})

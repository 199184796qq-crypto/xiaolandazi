import test, { before, after } from 'node:test'
import assert from 'node:assert/strict'
import { createServer } from 'vite'
import { createSSRApp } from 'vue'
import { renderToString } from 'vue/server-renderer'
import { readFile } from 'node:fs/promises'

let server, component
before(async () => {
  server = await createServer({ server: { middlewareMode: true }, appType: 'custom' })
  component = (await server.ssrLoadModule('/src/components/FactRiskTabNotice.vue')).default
})
after(async () => { await server?.close() })
const render = (props) => renderToString(createSSRApp(component, props))

test('待处理气泡与数量独立于省略标题，入口不隐藏8项待处理', async () => {
  const html = await render({count: 8})
  assert.ok(html.includes('8项待处理'))
  assert.ok(/class="fact-risk-tab-pill"/.test(html))
  assert.ok(/事实依据<\/span>\s*<span class="fact-risk-tab-pill"/.test(html))
  assert.ok(html.includes('有 8 项风险或资质待您处理'))
  assert.ok(html.includes('全部处理后提示自动消失'))
})
test('全部处理后消失；读取失败不能伪装全部通过', async () => {
  assert.equal((await render({count:0})).includes('fact-risk-tab-pill'), false)
  const error = await render({count:0,error:'network error'})
  assert.ok(error.includes('读取失败'))
  assert.ok(error.includes('尚不能确认全部已处理'))
  assert.ok((await render({count:0,loading:true})).includes('核查中'))
  assert.equal((await render({count:-1})).includes('待处理'), false)
})
test('自定义说明挂载到body，不受箭头clip-path和标题overflow裁切', async () => {
  const source = await readFile(new URL('../src/components/FactRiskTabNotice.vue',import.meta.url),'utf8')
  assert.ok(source.includes('<Teleport to="body">'))
  assert.ok(source.includes('position:fixed'))
  assert.ok(source.includes('@mouseenter="show"'))
  assert.ok(source.includes('props.focused'))
  const view = await readFile(new URL('../src/views/LiveStrategyView.vue',import.meta.url),'utf8')
  const navigation = await readFile(new URL('../src/components/PlanConfigurationNavigation.vue',import.meta.url),'utf8')
  assert.ok(view.includes('<PlanConfigurationNavigation'))
  assert.ok(navigation.includes(':focused="factFocused"'))
  assert.ok(navigation.includes('@focus="factFocused = item.mode === \'knowledge\'"'))
  assert.ok(view.includes('@state="factRiskNoticeState = $event"'))
})

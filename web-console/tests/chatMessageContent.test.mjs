import test, { before, after } from 'node:test'
import assert from 'node:assert/strict'
import { createServer } from 'vite'
import { createSSRApp } from 'vue'
import { renderToString } from 'vue/server-renderer'

let server, component, display
before(async () => {
  server = await createServer({ server: { middlewareMode: true, hmr: false }, appType: 'custom' })
  component = (await server.ssrLoadModule('/src/components/ChatMessageContent.vue')).default
  display = await server.ssrLoadModule('/src/chatMessageDisplay.ts')
})
after(async () => { await server?.close() })
const render = text => renderToString(createSSRApp(component, { text }))

test('截图中的加粗只在句内格式化，配送和保存条件不改变', async () => {
  const text = '资料已记录常温抽真空6天、不抽真空3天，以及川渝隔天送达、京东物流。这款鸭子在**冷藏（冰箱保鲜层） **或者**冷冻**条件下可以保存多久？'
  const html = await render(text)
  assert.equal((html.match(/<p\b/g) || []).length, 1)
  assert.ok(html.includes('<strong>冷藏（冰箱保鲜层） </strong>'))
  assert.ok(html.includes('<strong>冷冻</strong>'))
  assert.ok(html.includes('常温抽真空6天、不抽真空3天'))
  assert.equal(html.includes('**'), false)
  assert.equal(display.chatDisplayBlocks(text)[0].text, text)
})
test('标题、分段和两种列表可读，列表里的强调不独立成段', async () => {
  const html = await render('## 保存要求\n\n先核对包装。\n\n- **抽真空**：常温6天\n- 不抽真空：常温3天\n\n3. 冷藏时间待确认\n4. 冷冻时间待确认')
  assert.ok(html.includes('<h4'))
  assert.ok(/<ul\b/.test(html))
  assert.ok(/<ol\b/.test(html))
  assert.ok(html.includes('value="3"'))
  assert.equal((html.match(/<li\b/g) || []).length, 4)
  assert.ok(html.includes('<strong>抽真空</strong>'))
  assert.equal(html.includes('##'), false)
})
test('价格和自然语句不误识别为列表，普通换行保留', () => {
  const blocks = display.chatDisplayBlocks('48.9元一份\n通常下午6点前下单当天发\n超过6点第二天上午发')
  assert.deepEqual(blocks, [{ kind: 'paragraph', text: '48.9元一份\n通常下午6点前下单当天发\n超过6点第二天上午发' }])
})
test('模型提供的HTML和图片网址不会执行或发起外部加载', async () => {
  const html = await render('<script>alert(1)</script>\n<img src=x onerror=alert(1)>\n[说明](javascript:alert)\n![图片](https://example.test/pixel)')
  assert.equal(/<script|<img|<a\b/.test(html), false)
  assert.ok(html.includes('&lt;script&gt;'))
  assert.ok(html.includes('说明（javascript:alert）'))
  assert.ok(html.includes('图片（https://example.test/pixel）'))
})
test('引用和代码去除外层标记，代码中的星号和HTML保持原样并安全转义', async () => {
  const html = await render('> **待确认**信息\n\n```text\n**原文** <img src=x>\n```')
  assert.ok(html.includes('<blockquote'))
  assert.ok(html.includes('<strong>待确认</strong>'))
  assert.ok(html.includes('<pre'))
  assert.ok(html.includes('**原文** &lt;img src=x&gt;'))
  assert.equal(html.includes('```'), false)
})
test('空回复与未闭合强调不导致异常或丢掉文字', async () => {
  assert.equal(display.chatDisplayBlocks(' \r\n\t').length, 0)
  const html = await render('待确认：**冷藏多久？')
  assert.ok(html.includes('**冷藏多久？'))
})

// Exercise the real plan page with local responses; no production or model calls.
import assert from 'node:assert/strict'
import { createRequire } from 'node:module'
import { mkdir, readFile } from 'node:fs/promises'
import { createServer } from 'vite'

const { chromium } = createRequire(new URL('../../collector-worker/package.json', import.meta.url))('playwright-core')
const parent = (await readFile(new URL('./test-plan-rewrite-prompt-editor.mjs', import.meta.url), 'utf8')).replace(/\r\n/g, '\n')
const base = (await readFile(new URL('./test-plan-prompt-editor.mjs', import.meta.url), 'utf8')).replace(/\r\n/g, '\n')
let fixture = base.split('const fixture=String.raw`')[1].split('`\nconst server=')[0]
const setup = parent.split('const setup = String.raw`')[1].split('`\nfixture = fixture.replace')[0]
const danmakuFixture = String.raw`
const upstreamFetch=window.fetch;
window.fetch=async(url,init={})=>{
 const path=new URL(url,location.origin).pathname;
 if(path.endsWith('/interaction-answerflow'))return Response.json({enabled:false,revision:0});
 if(path.endsWith('/danmaku-test/analyze')){
  window.fixture.danmakuBody=JSON.parse(init.body);
  return Response.json({question:window.fixture.danmakuBody.question,interaction_intent:{type:'提问'},intent:{meaning:'测试理解',asks:[],uncertainties:[]},context_words:{hits:[]},recall:{groups:[],hits:[],notices:[]},action:'answer',answer:'测试回答'});
 }
 return upstreamFetch(url,init);
};
`
fixture = fixture.replace('applySession(', setup + '\n' + danmakuFixture + '\napplySession(')
fixture = fixture.replace('Object.assign(window.fixture,{documents:', "Object.assign(versions,JSON.parse(sessionStorage.getItem('room-intro-versions')||'{}'));Object.assign(window.fixture,{documents:")
fixture = fixture.replace('return Response.json(versions[plan]);\n  }\n  return Response.json({items:[versions[plan]]});', "sessionStorage.setItem('room-intro-versions',JSON.stringify(versions));return Response.json(versions[plan]);\n  }\n  return Response.json({items:[versions[plan]]});")

const server = await createServer({
  server: { host: '127.0.0.1', port: 0 },
  appType: 'custom',
  plugins: [{ name: 'room-intro-test-inspect', enforce: 'pre', transform(code, id) {
    if (!id.endsWith('/src/views/LiveStrategyView.vue')) return
    return code.replace('</script>', "window.fixture.roomIntroView={plan:()=>currentRoomPlanId.value,setPlan:id=>handlePlanSwitch({target:{value:String(id)}}),openDanmaku:()=>{activeMode.value='danmaku_test'},openProducts:()=>{activeMode.value='products'}};</script>")
  } }],
})
server.middlewares.use(async (req, res, next) => {
  if (req.url !== '/__room_intro') return next()
  res.setHeader('Content-Type', 'text/html;charset=utf-8')
  res.end(await server.transformIndexHtml(req.url, fixture))
})
await server.listen()
const browser = await chromium.launch({ executablePath: 'C:/Program Files/Google/Chrome/Application/chrome.exe', headless: true })
const page = await browser.newPage({ viewport: { width: 1600, height: 1000 }, reducedMotion: 'reduce' })
page.setDefaultTimeout(15000)
const errors = []
page.on('pageerror', error => errors.push(error.message))
const proof = 'E:/直播伴播-local/danmaku-single-line-20261111/browser-parent'
await mkdir(proof, { recursive: true })
try {
  await page.goto(server.resolvedUrls.local[0] + '__room_intro')
  await page.waitForFunction(() => window.fixture?.roomIntroView?.plan() === 4)
  await page.getByRole('button', { name: '商品资料', exact: true }).click()
  const intro = page.locator('.strategy-room-intro')
  await intro.waitFor()
  await page.waitForFunction(() => !document.querySelector('#plan-save-button')?.disabled)
  assert.match(await intro.innerText(), /暂无直播间简介/)
  assert.equal(await page.evaluate(() => window.fixture.calls.filter(c => c.method === 'POST' && c.path.endsWith('/versions')).length), 0)

  await page.getByRole('button', { name: '修改直播间简介' }).click()
  await page.getByRole('textbox', { name: '直播间简介' }).fill('四川南充杨鸭子直播间\n主营现卤土麻鸭。')
  await page.getByRole('button', { name: '完成编辑' }).click()
  assert.match(await intro.innerText(), /待保存/)
  assert.match(await intro.innerText(), /主营现卤土麻鸭/)
  assert.equal(await page.evaluate(() => window.fixture.calls.filter(c => c.method === 'POST' && c.path.endsWith('/versions')).length), 0)
  await page.evaluate(() => window.fixture.roomIntroView.openDanmaku())
  await page.getByRole('textbox', { name: '输入弹幕' }).fill('1号多少钱')
  await page.getByRole('button', { name: '分析意图' }).click()
  await page.waitForFunction(() => !!window.fixture.danmakuBody)
  assert.equal(await page.evaluate(() => window.fixture.danmakuBody.room_intro), '', 'unsaved intro must not reach the model')
  await page.evaluate(() => window.fixture.roomIntroView.openProducts())

  await page.locator('#plan-save-button').click()
  await page.waitForFunction(() => window.fixture.versions[4].generation_context.room_intro === '四川南充杨鸭子直播间\n主营现卤土麻鸭。')
  await page.waitForFunction(() => !document.querySelector('#plan-save-button')?.disabled)
  assert.equal((await intro.innerText()).includes('待保存'), false)
  assert.equal(await page.evaluate(() => window.fixture.versions[4].generation_context.room_intro), '四川南充杨鸭子直播间\n主营现卤土麻鸭。')
  await page.evaluate(() => { window.fixture.danmakuBody = null; window.fixture.roomIntroView.openDanmaku() })
  await page.getByRole('textbox', { name: '输入弹幕' }).fill('新疆能发吗')
  await page.getByRole('button', { name: '分析意图' }).click()
  await page.waitForFunction(() => !!window.fixture.danmakuBody)
  assert.equal(await page.evaluate(() => window.fixture.danmakuBody.room_intro), '四川南充杨鸭子直播间\n主营现卤土麻鸭。')
  await page.evaluate(() => window.fixture.roomIntroView.openProducts())
  await intro.screenshot({ path: proof + '/intro-desktop.png' })
  await page.setViewportSize({ width: 390, height: 844 })
  await intro.screenshot({ path: proof + '/intro-mobile.png' })
  assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth + 1), false)
  await page.setViewportSize({ width: 1600, height: 1000 })

  await page.reload()
  await page.waitForFunction(() => window.fixture?.roomIntroView?.plan() === 4)
  await page.getByRole('button', { name: '商品资料', exact: true }).click()
  await page.locator('.strategy-room-intro').getByText('四川南充杨鸭子直播间', { exact: false }).waitFor()
  assert.equal((await intro.innerText()).includes('待保存'), false)
  await page.evaluate(() => window.fixture.roomIntroView.setPlan(5))
  await page.waitForFunction(() => window.fixture.roomIntroView.plan() === 5 && !document.querySelector('#plan-save-button')?.disabled)
  assert.match(await intro.innerText(), /暂无直播间简介/)
  await page.evaluate(() => { window.fixture.danmakuBody = null; window.fixture.roomIntroView.openDanmaku() })
  await page.getByRole('textbox', { name: '输入弹幕' }).fill('2号多少钱')
  await page.getByRole('button', { name: '分析意图' }).click()
  await page.waitForFunction(() => !!window.fixture.danmakuBody)
  assert.equal(await page.evaluate(() => window.fixture.danmakuBody.room_intro), '', 'another plan must not inherit this intro')
  await page.evaluate(() => window.fixture.roomIntroView.openProducts())
  await page.evaluate(() => window.fixture.roomIntroView.setPlan(4))
  await page.waitForFunction(() => window.fixture.roomIntroView.plan() === 4 && document.querySelector('.strategy-room-intro')?.textContent.includes('主营现卤土麻鸭'))

  await page.getByRole('button', { name: '修改直播间简介' }).click()
  await page.getByRole('textbox', { name: '直播间简介' }).fill('尚未保存的新简介')
  await page.evaluate(() => { window.fixture.failVersion = true })
  await page.locator('#plan-save-button').click()
  await page.getByText('正式版本保存失败', { exact: false }).first().waitFor()
  assert.equal(await page.getByRole('textbox', { name: '直播间简介' }).inputValue(), '尚未保存的新简介')
  assert.match(await intro.innerText(), /待保存/)
  assert.equal(await page.evaluate(() => window.fixture.versions[4].generation_context.room_intro), '四川南充杨鸭子直播间\n主营现卤土麻鸭。')
  assert.deepEqual(errors, [])
  console.log('PASS: intro edit, version payload, reload, plan and model-context isolation, failed save retains draft; zero production calls')
} finally {
  await browser.close()
  await server.close()
}

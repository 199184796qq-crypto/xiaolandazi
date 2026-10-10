import assert from 'node:assert/strict'
import { createRequire } from 'node:module'
import { readFile, mkdir } from 'node:fs/promises'
import { createServer } from 'vite'
const require = createRequire(new URL('../../collector-worker/package.json', import.meta.url))
const { chromium } = require('playwright-core')
const source = await readFile(new URL('./test-interaction-full-view-location.mjs', import.meta.url), 'utf8')
let fixture = source.match(/const fixture = `([\s\S]*?)`\r?\nconst deferredWorkspace/)[1]
fixture = fixture.replace("name:'测试方案',room_ids", "name:'测试方案',status:'active',room_ids")
fixture = fixture.replace("import View from '/src/views/LiveStrategyView.vue';", "import View from '/src/views/LiveStrategyView.vue';import Agent from '/src/components/SystemAgentLayer.vue';")
fixture = fixture.replace("const router=createRouter", `
const originalFetch=window.fetch;
window.fixture.writes=[];window.fixture.failNext=false;window.fixture.delayNext=false;
window.fetch=async(url,init={})=>{const p=new URL(url,location.origin).pathname,method=init.method||'GET';
 if(p.endsWith('/ui-preferences'))return Response.json({selected_live_room_id:26,agent_drawer_collapsed:false});
 if(p==='/api/v1/rooms/26')return Response.json(room);
 if(method!=='GET'){
  const body=JSON.parse(init.body||'{}');window.fixture.writes.push({p,method,body});
  if(p.endsWith('/agent-decisions/simulate')){
   if(window.fixture.failNext){window.fixture.failNext=false;return Response.json({error:'测试暂时失败'},{status:502})}
   if(window.fixture.delayNext){window.fixture.delayNext=false;await new Promise(r=>window.fixture.releaseTest=r)}
   return Response.json({question:body.question,reply:body.preview_instruction?'纠正后回答：使用京东物流。':'测试回答：使用京东物流。',plan_id:body.plan_id,plan_name:'测试方案',execution_mode:'intent'});
  }
  if(p.endsWith('/facts/adopt'))return Response.json({adopted:1,results:[{status:'adopted'}]});
  if(p.endsWith('/adopt-plan-answer'))return Response.json({session:{id:50,status:'adopted'},memory:{id:90},version:{id:91}});
  throw Error('Unexpected write '+p);
 }
 return originalFetch(url,init);
};
const router=createRouter`)
fixture = fixture.replace("routes:[{path:'/',component:View}]", "routes:[{path:'/',name:'live-strategy',component:View}]")
fixture = fixture.replace("render:()=>h(View)", "render:()=>h('div',[h(View),h(Agent)])")
const server = await createServer({server:{host:'127.0.0.1',port:0},appType:'custom'})
server.middlewares.use(async(req,res,next)=>{if(!req.url.startsWith('/__plan_speech'))return next();res.setHeader('Content-Type','text/html;charset=utf-8');res.end(await server.transformIndexHtml('/__plan_speech',fixture))})
await server.listen()
const browser=await chromium.launch({executablePath:'C:/Program Files/Google/Chrome/Application/chrome.exe',headless:true})
try {
 const page=await browser.newPage({viewport:{width:1600,height:1050},reducedMotion:'reduce'}),errors=[]
 page.on('pageerror',e=>errors.push(e.message))
 await page.goto(server.resolvedUrls.local[0]+'__plan_speech')
 const drawer=page.locator('.system-agent-drawer')
 await drawer.getByRole('button',{name:'切换到话术智能体',exact:true}).click()
 await page.locator('.plan-speech-agent').waitFor()
 assert.equal((await page.evaluate(()=>window.fixture.writes)).length,0,'switch must not call a model or mutate business assets')
 const input=page.getByLabel('模拟用户提问',{exact:true})
 await input.fill('发什么快递？');await page.getByRole('button',{name:'开始测试',exact:true}).click()
 const first=page.locator('[data-plan-test-answer]').first()
 await first.getByText('测试回答：使用京东物流。',{exact:false}).waitFor()
 let calls=await page.evaluate(()=>window.fixture.writes)
 assert.deepEqual(calls[0].body,{question:'发什么快递？',plan_id:4})
 assert.equal(calls.length,1,'testing never auto-adopts')
 await first.getByRole('button',{name:'纠正回答',exact:true}).click()
 await first.getByLabel('正确回答或回答要求').fill('只说使用京东物流，不承诺到货日期。')
 await first.getByRole('button',{name:'应用纠正并重测'}).click()
 const second=page.locator('[data-plan-test-answer]').nth(1)
 await second.getByText('纠正后回答：使用京东物流。',{exact:false}).waitFor()
 calls=await page.evaluate(()=>window.fixture.writes)
 assert.equal(calls[1].body.plan_id,4);assert.equal(calls[1].body.preview_instruction,'只说使用京东物流，不承诺到货日期。')
 assert.equal(calls.length,2,'correction preview is not persisted before adoption')
 await second.getByRole('button',{name:'采纳到互动策略',exact:true}).click()
 await second.getByText('已采纳到当前方案的互动策略，可以继续测试啦。').waitFor()
 await first.getByRole('button',{name:'采纳到事实依据',exact:true}).click()
 await first.getByText('已采纳到当前方案的事实依据，可以继续测试啦。').waitFor()
 calls=await page.evaluate(()=>window.fixture.writes)
 assert.equal(calls[2].body.plan_id,4);assert.equal(calls[2].body.answer,'纠正后回答：使用京东物流。')
 assert.equal(calls[3].p,'/api/v1/live-agent-plans/4/facts/adopt');assert.equal(calls[3].body.tenant_id,14)
 assert.equal(calls[3].body.facts[0].value,'测试回答：使用京东物流。')
 assert.equal(await drawer.getByRole('button',{name:'话术智能体 · 互动测试',exact:true}).getAttribute('aria-pressed'),'true')
 assert.equal(new URL(page.url()).pathname,'/__plan_speech','adoption stays in current page')
 await mkdir('../deploy/validation/plan-speech-agent',{recursive:true})
 await page.screenshot({path:'../deploy/validation/plan-speech-agent/desktop.png'})
 await page.evaluate(()=>window.fixture.failNext=true);await input.fill('失败后保留问题');await page.getByRole('button',{name:'开始测试',exact:true}).click()
 await page.getByRole('alert').filter({hasText:'测试暂时失败'}).waitFor();assert.equal(await input.inputValue(),'失败后保留问题')
 await page.evaluate(()=>window.fixture.delayNext=true);await input.fill('旧方案的慢请求');await page.getByRole('button',{name:'开始测试',exact:true}).click()
 await page.waitForFunction(()=>!!window.fixture.releaseTest)
 await page.evaluate(()=>window.dispatchEvent(new CustomEvent('system-agent-live-strategy-context',{detail:{room_id:26,plan_id:5,mode:'plan'}})))
 await drawer.getByRole('button',{name:'切换到话术智能体',exact:true}).click()
 await page.evaluate(()=>window.fixture.releaseTest())
 await page.getByText('方案 #5 · 与互动智能体共用真实回答链路',{exact:true}).waitFor()
 assert.equal(await page.locator('[data-plan-test-answer]').count(),0,'late answer from previous plan must not leak')
 await page.setViewportSize({width:460,height:850})
 const button=drawer.getByRole('button',{name:'话术智能体 · 互动测试',exact:true}),box=await button.boundingBox()
 assert.ok(box.x>=0 && box.x+box.width<=460 && box.y>=0,'explicit switch fits narrow screen')
 await page.screenshot({path:'../deploy/validation/plan-speech-agent/narrow.png'})
 await drawer.getByRole('button',{name:'方案配置',exact:true}).click();assert.equal(await page.locator('.plan-speech-agent').count(),0)
 assert.deepEqual(errors,[])
 console.log('PASS: full plan page and system agent; explicit switch; selected plan question/correction; human fact/strategy adoption; no navigation; inline failure retains draft; late-response isolation; narrow layout')
} finally {await browser.close();await server.close()}

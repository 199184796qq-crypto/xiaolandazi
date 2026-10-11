import assert from 'node:assert/strict'
import { createServer } from 'vite'
import { createRequire } from 'node:module'
const require = createRequire(new URL('../../collector-worker/package.json', import.meta.url))
const { chromium } = require('playwright-core')
const fixture = `<html><head><meta charset="utf-8"></head><body><div id="app"></div><script type="module">
import {createApp,ref,h} from '/node_modules/.vite/deps/vue.js';import Panel from '/src/components/FactSubjectDetails.vue';
createApp({setup(){const editing=ref(false);return ()=>h('div',[h('button',{onClick:()=>editing.value=true},'修改对象资料'),h(Panel,{planId:4,tenantId:14,factId:36,editRequested:editing.value})])}}).mount('#app');</script></body></html>`
const server = await createServer({server:{host:'127.0.0.1',port:0},appType:'custom'})
server.middlewares.use((req,res,next)=>{if(req.url==='/__secondary'){res.setHeader('Content-Type','text/html; charset=utf-8');res.end(fixture)}else next()})
await server.listen()
const browser=await chromium.launch({executablePath:'C:/Program Files/Google/Chrome/Application/chrome.exe',headless:true})
const page=await browser.newPage({viewport:{width:1200,height:950}}),errors=[],writes=[]
page.on('pageerror',e=>errors.push(e.message))
const task={id:7,revision:2,subject:'杯身钢材',root_fact:{id:36,text:'杯身使用304不锈钢'},title:'材料讲解',purpose:'讲清材质',angles:[{title:'材质',goal:'讲部位'}],facts:[{id:1,storage:'lecture',version:1,name:'材质牌号',text:'杯身钢材牌号为304',quote:'原始资料'},{id:2,storage:'lecture',version:1,name:'未确认参数',text:'未提供保温时长'}],links:['1'],valid:true}
let fail=true
await page.route('**/api/v1/**',async route=>{
 const request=route.request()
 if(request.method()==='GET'){assert.match(request.url(),/live-agent-plans\/4\/lecture-tasks\?tenant_id=14$/);await route.fulfill({json:{tasks:[task,{...task,id:9,root_fact:{id:99},subject:'不得串用的原料'}]}});return}
 assert.match(request.url(),/live-agent-plans\/4\/lecture-tasks\/7\/detail\?tenant_id=14$/)
 const body=request.postDataJSON();writes.push(body);assert.equal(body.revision,task.revision);assert.equal(body.detail_id,1);assert.equal(body.storage,'lecture')
 if(fail){await route.fulfill({status:422,json:{error:'资料对象不能改成成品功效'}});return}
 task.facts[0].text='杯身钢材牌号为304，不提供保温时长承诺';task.revision++
 await route.fulfill({json:{saved:true,name:body.name,text:task.facts[0].text,notice:'二级资料已保存'}})
})
try{
 await page.goto(server.resolvedUrls.local[0]+'__secondary')
 await page.getByRole('button',{name:'修改对象资料',exact:true}).click()
 const panel=page.getByRole('region',{name:'二级事实资料面板'})
 await panel.getByText('杯身钢材',{exact:true}).waitFor()
 assert.doesNotMatch(await panel.textContent(),/不得串用/)
 assert.equal(await panel.locator('.detail-grid').evaluate(el=>getComputedStyle(el).gridTemplateColumns.split(' ').length),2)
 await panel.getByRole('button',{name:'修改二级资料：材质牌号'}).click()
 const text=panel.getByRole('textbox',{name:'已确认的对象事实'})
 await text.fill('用户修改的对象资料');await panel.getByRole('button',{name:'保存资料'}).click()
 await panel.getByRole('alert').waitFor();assert.equal(await text.inputValue(),'用户修改的对象资料','failed review must preserve draft')
 fail=false;await panel.getByRole('button',{name:'保存资料'}).click();await panel.getByText('二级资料已保存',{exact:true}).waitFor()
 assert.match(await panel.textContent(),/杯身钢材牌号为304，不提供保温时长承诺/)
 assert.equal(await text.count(),0)
 await page.setViewportSize({width:390,height:844})
 assert.equal(await panel.locator('.detail-grid').evaluate(el=>getComputedStyle(el).gridTemplateColumns.split(' ').length),1)
 assert.ok(await panel.evaluate(el=>el.scrollWidth<=el.clientWidth+1))
 assert.equal(writes.length,2);assert.deepEqual(errors,[])
 console.log('Secondary editor: object scope, separate full-width cards, editable details, version-bound save, error keeps draft, canonical refresh, desktop/mobile passed')
}finally{await browser.close();await server.close()}

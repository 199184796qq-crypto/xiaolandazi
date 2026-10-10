// Synthetic assets and real Vue components; never calls live APIs or models.
import assert from 'node:assert/strict'
import { createServer } from 'vite'
import { createRequire } from 'node:module'
const require = createRequire(process.env.PLAYWRIGHT_CORE_PACKAGE || new URL('../../collector-worker/package.json', import.meta.url))
const { chromium } = require('playwright-core')
const fixture = `<!doctype html><html><head><meta charset="utf-8"><style>*{box-sizing:border-box}body{margin:20px;font-family:'Microsoft YaHei',sans-serif}.destination{padding:20px}</style></head><body><div id="app"></div><script type="module">
import{createApp,h,ref,nextTick}from'/node_modules/.vite/deps/vue.js';import Panel from'/src/components/ContentPoolChangesPanel.vue';import Miner from'/src/components/SpeechMinerWorkbench.vue';import Products from'/src/components/LiveProductLinkCards.vue';import{poolTaskSources}from'/src/contentPoolSources.ts';
const row=(id,area,label,products,fields)=>({id,area,label,products,fields});
const saved=[row('knowledge:13','knowledge','常温保存期限',[],{事实:'抽真空6天，不抽真空3天'}),row('attributes:4','products','1号 · 包装',[4],{属性:'包装',内容:'抽真空与不抽真空'}),row('attributes:5','products','2号 · 包装',[5],{属性:'包装',内容:'抽真空与不抽真空'}),row('benefits:7','benefits','旧活动',[4],{活动价:'85.5'}),row('knowledge:14','knowledge','同名资料',[],{事实:'抽真空6天，不抽真空3天'})];
const tasks=[{id:'task-1',selling_point:'口味包装选项',angle:'模型整理的包装说明',source_ids:['knowledge:13','shared-packaging','benefits:7','facts:line:8']},{id:'task-2',selling_point:'规格选择',angle:'整只与半只的选择依据',source_ids:['knowledge:13']}];
const status=ref({state:'synced',set_id:8,saved_set_id:8,changes:[],products:[{id:4,link:'1',name:'整只'},{id:5,link:'2',name:'半只'}],benefits:[],job:{},source_rows:saved,current_source_rows:saved.filter(r=>r.id!=='benefits:7').map(r=>r.id==='knowledge:13'?{...r,fields:{事实:'抽真空常温6天，不抽真空常温3天'}}:r),pools:[{key:'common',label:'公共',owner:0,products:[4,5],task_count:2,reused:false,tasks,facts:[{id:'knowledge:13',text:'保存期限',sources:['knowledge:13']},{id:'shared-packaging',text:'包装选择',sources:['attributes:4','attributes:5']},{id:'benefits:7',text:'旧活动',sources:['benefits:7']}]}]});
const miner=ref(null),products=ref(null),destination=ref(''),last=ref(null),calls=[];
window.fixture={status,calls,refs:()=>poolTaskSources(status.value,status.value.pools[0],tasks[0]),locate:(...a)=>miner.value.locatePoolTask(...a),productLocate:(...a)=>products.value.locateSource(...a)};
window.fetch=async(url,init={})=>{calls.push({url:String(url),method:init.method||'GET'});throw Error('This fixture forbids API calls')};
const items=[4,5].map(id=>({id,link_key:String(id-3),product_name:'商品'+id,spec:'规格',daily_price:'价格',quantity:'数量',audience:'适用',room_roles:['main'],attributes:[{id,code:'packaging',label:'包装',value:'当前包装资料',status:'active'}]}));
const noMutation=async()=>{throw Error('Navigation must not modify source')};
createApp({setup(){return()=>[h(Panel,{status:status.value,error:'',submitting:false,preview:true,onSource:async source=>{last.value=source;destination.value=source.area;await nextTick();if(source.area==='products')await products.value.locateSource(source.entityId,source.attributeId)},onEditTask:async(key,id)=>{destination.value='miner';await nextTick();await miner.value.locatePoolTask(key,id,status.value.set_id)}}),
h('div',{class:'destination'},[h('pre',{id:'last-source'},JSON.stringify(last.value)),h(Products,{ref:products,items,saveField:noMutation,saveRoomRoles:noMutation,removeItem:noMutation,copyItem:noMutation,addAttribute:noMutation,saveAttribute:noMutation,removeAttribute:noMutation}),h(Miner,{ref:miner,planId:4,roomId:26,tenantId:14,poolStatus:status.value})])]} }).mount('#app');
</script></body></html>`
const server = await createServer({server:{host:'127.0.0.1',port:0},appType:'custom'})
server.middlewares.use((req,res,next)=>{if(req.url==='/__sources'){res.setHeader('Content-Type','text/html; charset=utf-8');res.end(fixture)}else next()})
await server.listen()
const browser=await chromium.launch({executablePath:'C:/Program Files/Google/Chrome/Application/chrome.exe',headless:true})
try {
  const page=await browser.newPage({viewport:{width:1440,height:1000}}),errors=[]
  page.on('pageerror',e=>errors.push(e.message))
  await page.goto(server.resolvedUrls.local[0]+'__sources')
  await page.getByText('口味包装选项 · 模型整理的包装说明',{exact:true}).waitFor()
  const refs=await page.evaluate(()=>window.fixture.refs())
  assert.deepEqual(refs.map(r=>r.id),['knowledge:13','attributes:4','attributes:5','benefits:7','facts:line:8'])
  assert.equal(refs[0].state,'changed');assert.equal(refs[3].state,'removed');assert.equal(refs[4].state,'unavailable')
  assert.equal(refs[0].location.entityId,13,'same text must not route to fact 14')
  assert.equal(refs[3].location,undefined,'deleted activity must not guess a replacement')
  await page.locator('.pool-task-sources').first().locator('summary').click()
  await page.getByText('原资料已变化，需更新内容',{exact:true}).first().waitFor()
  await page.getByRole('button',{name:'2号 · 包装 · 查看出处',exact:true}).click()
  assert.deepEqual(JSON.parse(await page.locator('#last-source').textContent()),{sourceId:'attributes:5',area:'products',entityId:5,attributeId:5})
  assert.equal(await page.locator('[data-product-attribute-id="5"]').evaluate(el=>document.activeElement===el),true)
  assert.equal(await page.evaluate(()=>window.fixture.productLocate(4,5)),false,'same label attribute from another product must not match')
  await page.locator('.pool-group ol>li').nth(1).getByRole('button',{name:'修改这条',exact:true}).click()
  const editor=page.locator('[data-angle-editor="1-0"]')
  await editor.waitFor();assert.equal(await editor.inputValue(),'整只与半只的选择依据')
  assert.equal(await editor.evaluate(el=>document.activeElement===el),true)
  assert.equal(await page.evaluate(()=>window.fixture.locate('common','task-1',7)),false,'stale set must not edit a current task')
  assert.equal(await page.evaluate(()=>window.fixture.calls.length),0,'navigation must not regenerate or save')
  await page.setViewportSize({width:390,height:1000})
  assert.equal(await page.locator('.pool-notice').evaluate(el=>[...el.querySelectorAll('.pool-group,.pool-source-text')].some(child=>child.getBoundingClientRect().right>el.getBoundingClientRect().right+1)),false,'provenance mobile overflow')
  assert.deepEqual(errors,[])
  console.log('PASS: source IDs/shared origins/current vs saved/removed and unknown provenance; exact attribute owner; task editor destination and stale set guard; no API calls')
} finally {await browser.close();await server.close()}

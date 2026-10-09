import assert from 'node:assert/strict'
import {createServer} from 'vite'
import {createRequire} from 'node:module'
const require=createRequire('E:/直播伴播/collector-worker/package.json')
const {chromium}=require('playwright-core')
const fixture=`<html><body><div id="app"></div><script type="module">
import{createApp}from'/node_modules/.vite/deps/vue.js';import Panel from'/src/components/AnchorGenerationSettingsPanel.vue';
window.fixture={saves:[],cfg:{interaction_profile_id:'flash',style_profile_id:'flash',scorecard_profile_id:'flash',selling_point_profile_id:'flash',mainline_buffer_seconds:120,cycle:{text_profile_id:'flash',feedback_profile_id:'old-omni',credential_profile_id:'flash',workspace_id:'ws',agent_id:'agent',region:'cn-beijing',threshold:40,max_retries:3,point_gap:2,combination_gap:8}}};
window.fetch=async(url,init={})=>{if(init.method==='PUT'){const data=JSON.parse(init.body);window.fixture.saves.push(data);window.fixture.cfg=data;return Response.json(data)}if(String(url).includes('speech-models'))return Response.json({config:{profiles:[{id:'flash',name:'Flash',model:'qwen3.7-flash',enabled:true,has_api_key:true},{id:'other',name:'另一文字模型',model:'deepseek-v4-flash',enabled:true,has_api_key:true}]}});return Response.json(window.fixture.cfg)};
createApp(Panel).mount('#app')</script></body></html>`
const server=await createServer({server:{host:'127.0.0.1',port:0},appType:'custom'});server.middlewares.use((req,res,next)=>{if(req.url==='/__shared'){res.setHeader('Content-Type','text/html;charset=utf-8');res.end(fixture)}else next()});await server.listen()
const browser=await chromium.launch({executablePath:'C:/Program Files/Google/Chrome/Application/chrome.exe',headless:true})
try{
 const page=await browser.newPage(),errors=[];page.on('pageerror',e=>errors.push(e.message));await page.goto(server.resolvedUrls.local[0]+'__shared')
 const selector=page.getByLabel('主线文字生成与回灌',{exact:true});await selector.waitFor();assert.equal(await page.getByText('回灌辅助模型',{exact:true}).count(),0)
 await selector.selectOption('other');await page.getByRole('button',{name:'保存生成配置',exact:true}).click();await page.waitForFunction(()=>window.fixture.saves.length===1)
 const body=await page.evaluate(()=>window.fixture.saves[0]);assert.equal(body.cycle.text_profile_id,'other');assert.equal(body.cycle.feedback_profile_id,'other');assert.equal(body.interaction_profile_id,'flash','interaction remains separately selected')
 await selector.selectOption('');await page.getByLabel('阿里凭据配置',{exact:true}).waitFor();assert.equal(await page.getByText('回灌辅助模型',{exact:true}).count(),0)
 assert.deepEqual(errors,[]);console.log('PASS: one mainline/feedback selector; switching model sends matching feedback profile; interaction preserved; Agent mode remains available')
}finally{await browser.close();await server.close()}

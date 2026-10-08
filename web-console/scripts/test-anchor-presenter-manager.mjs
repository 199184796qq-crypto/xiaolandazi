// Isolated component interaction/layout test: no live-room data is written.
import assert from 'node:assert/strict'
import { createServer } from 'vite'
import { createRequire } from 'node:module'
import { mkdir } from 'node:fs/promises'
const require = createRequire(new URL('../../collector-worker/package.json', import.meta.url))
const { chromium } = require('playwright-core')
const output = 'E:/直播伴播-local/validation/20261008-presenter-manager'
await mkdir(output, { recursive: true })
const fixture = `<html><head><style>*{box-sizing:border-box}body{padding:24px;margin:0;background:#eef1ff;font-family:Arial,'Microsoft Yahei',sans-serif}#app{width:285px}button{font:inherit}</style></head><body><div id="app"></div><script type="module">
import{createApp,h,reactive}from'/node_modules/.vite/deps/vue.js';import Manager from'/src/components/AnchorPresenterManager.vue';
const initial=[{id:7,name:'小林主播',description:'请添加这个主播的表达样本和风格说明',avatar_type:'female',sample_count:1,training_count:3},{id:9,name:'小周主播',description:'第二个独立主播',avatar_type:'male',sample_count:2,training_count:0}];
const state=reactive({styles:JSON.parse(localStorage.getItem('fixture-avatars')||'null')||initial,selectedId:7,busy:false,editingId:null,savingNameId:null,avatarSavingIds:[],nameDraft:'',hasSelection:true,hasPlan:true});window.fixture={state,events:[],fail:false};
createApp({render:()=>h(Manager,{...state,'onUpdate:nameDraft':v=>state.nameDraft=v,onCreate:()=>window.fixture.events.push('create'),onSelect:id=>state.selectedId=id,onRename:s=>{state.editingId=s.id;state.nameDraft=s.name},onSaveName:s=>{s.name=state.nameDraft;state.editingId=null},onCancelName:()=>state.editingId=null,onAvatar:async(s,kind)=>{state.avatarSavingIds=[s.id];window.fixture.events.push('avatar:'+s.id+':'+kind);await new Promise(r=>setTimeout(r,80));if(!window.fixture.fail){s.avatar_type=kind;localStorage.setItem('fixture-avatars',JSON.stringify(state.styles))}state.avatarSavingIds=[]},onDescription:(s,text)=>s.description=text,onRemove:s=>window.fixture.events.push('remove:'+s.id),onBind:()=>window.fixture.events.push('bind'),onPlugins:()=>window.fixture.events.push('plugins')})}).mount('#app')</script></body></html>`
const server = await createServer({ server: { host: '127.0.0.1', port: 5381 }, appType: 'custom' })
server.middlewares.use((req,res,next)=>{if(req.url==='/__presenter_fixture'){res.setHeader('Content-Type','text/html; charset=utf-8');res.end(fixture)}else next()})
await server.listen()
const browser = await chromium.launch({executablePath:'C:/Program Files/Google/Chrome/Application/chrome.exe',headless:true})
const page=await browser.newPage({viewport:{width:900,height:900}}),errors=[]
page.on('pageerror',e=>errors.push(e.message))
try{
 await page.goto('http://127.0.0.1:5381/__presenter_fixture')
 await page.getByRole('heading',{name:'主播管理'}).waitFor()
 const first=page.locator('.presenter-card').first(),second=page.locator('.presenter-card').nth(1)
 for(const [kind,label] of [['male','男'],['female','女'],['middle_aged_male','中年男'],['middle_aged_female','中年女']]){
  await first.locator('.avatar-menu-button').click()
  await first.getByRole('button',{name:label,exact:true}).click()
  await page.waitForFunction(k=>window.fixture.state.avatarSavingIds.length===0&&window.fixture.state.styles[0].avatar_type===k,kind)
  assert.ok((await first.locator('img').getAttribute('src')).endsWith(kind+'-v1.png'))
  assert.ok((await second.locator('img').getAttribute('src')).endsWith('male-v1.png'))
 }
 // Failure does not leave an unsaved choice on screen.
 await page.evaluate(()=>window.fixture.fail=true)
 await first.locator('.avatar-menu-button').click()
 await first.getByRole('button',{name:'男',exact:true}).click()
 await page.waitForFunction(()=>window.fixture.state.avatarSavingIds.length===0)
 assert.ok((await first.locator('img').getAttribute('src')).endsWith('middle_aged_female-v1.png'))
 await page.reload()
 assert.ok((await first.locator('img').getAttribute('src')).endsWith('middle_aged_female-v1.png'))
 await first.getByRole('button',{name:'修改主播“小林主播”的名称'}).click()
 const name=page.getByRole('textbox',{name:'修改主播名称'});await name.fill('小林主播02');await name.press('Enter')
 await first.getByRole('button',{name:'小林主播02',exact:true}).waitFor()
 await first.getByRole('button',{name:'修改主播特征介绍'}).click()
 const intro=first.getByRole('textbox',{name:'修改主播特征介绍，最多30字'})
 await intro.fill('甲'.repeat(40));assert.equal(Array.from(await intro.inputValue()).length,30)
 await intro.press('Enter');assert.equal(await first.locator('.presenter-description p').textContent(),'甲'.repeat(30))
 await page.getByRole('button',{name:'＋新建主播'}).click()
 await first.getByRole('button',{name:'删除主播“小林主播02”'}).click()
 await page.getByRole('button',{name:'设为当前智能体主播'}).click()
 await page.getByRole('button',{name:'当前系统的个性插件'}).click()
 assert.deepEqual(await page.evaluate(()=>window.fixture.events),['create','remove:7','bind','plugins'])
 await second.getByRole('button',{name:'小周主播',exact:true}).click()
 assert.ok((await second.getAttribute('class')).includes('selected'))
 for(const width of [285,480]){
  await page.locator('#app').evaluate((el,w)=>el.style.width=w+'px',width)
  assert.equal(await page.locator('.presenter-manager').evaluate(el=>el.scrollWidth>el.clientWidth),false,'manager overflow at '+width)
  assert.equal(await page.locator('.presenter-card').first().evaluate(el=>el.scrollWidth>el.clientWidth),false,'card overflow at '+width)
  assert.ok(await page.locator('img').first().evaluate(img=>img.complete&&img.naturalWidth>0))
  await page.screenshot({path:output+'/presenter-'+width+'.png',fullPage:true})
 }
 assert.deepEqual(errors,[])
 console.log('PASS four avatars, independent presenter, failed-save rollback, reload, rename/select/create/delete/bind/plugins, 285/480px layouts; all data mocked')
}finally{await browser.close();await server.close()}

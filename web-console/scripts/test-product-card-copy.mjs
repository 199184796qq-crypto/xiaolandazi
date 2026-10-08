// Mocked component fixture; no DB/user plans/upstream models are touched.
import assert from 'node:assert/strict'
import { createServer } from 'vite'
import { createRequire } from 'node:module'
import { mkdir } from 'node:fs/promises'
const require=createRequire(new URL('../../collector-worker/package.json',import.meta.url))
const {chromium}=require('playwright-core')
const out='E:/直播伴播-local/validation/20261008-card-copy';await mkdir(out,{recursive:true})
const fixture=`<html><head><meta charset="utf-8"><style>*{box-sizing:border-box}body{margin:0;padding:20px;background:#f3f6ff;font-family:Arial,'Microsoft Yahei',sans-serif}</style></head><body><div id="app"></div><script type="module">
import{createApp,h,reactive}from'/node_modules/.vite/deps/vue.js';import Cards from'/src/components/LiveProductLinkCards.vue';
const state=reactive({items:[{id:7,tenant_id:14,plan_id:4,link_key:'1号链接',product_name:'杨鸭子',spec:'900g',daily_price:'87.6',quantity:'1',audience:'喜欢麻辣和五香',room_roles:['main'],status:'active',version_no:3,attributes:[{id:40,code:'flavour',label:'口味',value:'麻辣',unit:'',display_type:'text',display_priority:10}]}]});
window.fixture={state,calls:[],fail:false,resolve:null};const noop=async()=>{};
createApp({render:()=>h(Cards,{...state,benefits:[],saveField:async(item,field,value)=>{const current=state.items.find(i=>i.id===item.id);current[field]=value;current.version_no++;window.fixture.calls.push({action:'save',field})},saveRoomRoles:noop,removeItem:noop,addAttribute:noop,saveAttribute:noop,removeAttribute:noop,copyItem:async item=>{window.fixture.calls.push({action:'copy',version:item.version_no});await new Promise(r=>window.fixture.resolve=r);if(window.fixture.fail)throw Error('模拟保存失败');const copy=JSON.parse(JSON.stringify(item));copy.id=100+state.items.length;copy.version_no=1;copy.link_key=(state.items.length+1)+'号链接';copy.product_name=item.product_name+String(state.items.length).padStart(2,'0');copy.attributes[0].id=500+copy.id;state.items.splice(state.items.findIndex(i=>i.id===item.id)+1,0,copy)}})}).mount('#app')</script></body></html>`
const server=await createServer({server:{host:'127.0.0.1',port:5382},appType:'custom'})
server.middlewares.use((req,res,next)=>{if(req.url==='/__copy_fixture'){res.setHeader('Content-Type','text/html; charset=utf-8');res.end(fixture)}else next()});await server.listen()
const browser=await chromium.launch({executablePath:'C:/Program Files/Google/Chrome/Application/chrome.exe',headless:true}),page=await browser.newPage({viewport:{width:1280,height:900}}),errors=[]
page.on('pageerror',e=>errors.push(e.message))
try{
 await page.goto('http://127.0.0.1:5382/__copy_fixture')
 const copy=page.getByRole('button',{name:'复制1号链接商品卡',exact:true})
 await copy.click();assert.ok(await copy.isDisabled());await copy.evaluate(el=>el.click())
 assert.equal(await page.evaluate(()=>window.fixture.calls.length),1)
 await page.evaluate(()=>window.fixture.resolve());await page.waitForFunction(()=>document.querySelectorAll('.product-card').length===2)
 const result=await page.evaluate(()=>window.fixture.state.items)
 assert.equal(result[0].product_name,'杨鸭子');assert.equal(result[1].product_name,'杨鸭子01');assert.equal(result[1].link_key,'2号链接')
 assert.notEqual(result[0].attributes[0].id,result[1].attributes[0].id)
 // Pending inline edits save first; duplication uses the latest returned version.
 await page.getByRole('button',{name:'修改1号链接的规格',exact:true}).click()
 await page.getByRole('textbox',{name:'编辑1号链接的规格',exact:true}).fill('950g')
 await copy.click();await page.waitForFunction(()=>window.fixture.calls.filter(c=>c.action==='copy').length===2)
 assert.equal(await page.evaluate(()=>window.fixture.calls.at(-1).version),4)
 await page.evaluate(()=>window.fixture.resolve());await page.waitForFunction(()=>document.querySelectorAll('.product-card').length===3)
 assert.equal(await page.locator('.product-card').nth(1).locator('.product-name strong').textContent(),'杨鸭子02')
 // Failure cannot create a card or mutate the original name/number.
 await page.evaluate(()=>window.fixture.fail=true);await copy.click();await page.evaluate(()=>window.fixture.resolve())
 await page.getByText('模拟保存失败',{exact:true}).waitFor();assert.equal(await page.locator('.product-card').count(),3)
 await page.screenshot({path:out+'/product-copy.png',fullPage:true})
 await page.setViewportSize({width:390,height:900})
 assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false)
 await page.screenshot({path:out+'/product-copy-mobile.png',fullPage:true})
 assert.deepEqual(errors,[]);console.log('PASS product copy placement/independent IDs/busy guard/save-before-copy/latest version/failure/mobile; data fully mocked')
}finally{await browser.close();await server.close()}

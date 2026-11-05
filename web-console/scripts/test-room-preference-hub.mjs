import assert from 'node:assert/strict'
import { createRequire } from 'node:module'
import { existsSync, mkdirSync, writeFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { createServer } from 'vite'
import vue from '@vitejs/plugin-vue'

const require = createRequire(new URL('../../collector-worker/package.json', import.meta.url))
const { chromium } = require('playwright-core')
const out = process.env.HOST_STATUS_EVIDENCE || 'E:/直播伴播-local/remove-host-status-20261105/browser'
mkdirSync(out, { recursive: true })
const root = fileURLToPath(new URL('..', import.meta.url))
const plugin = { name: 'room-preference-fixture', configureServer(server) {
  server.middlewares.use(async (req, res, next) => {
    if (!req.url?.startsWith('/__preferences.html')) return next()
    res.setHeader('Content-Type', 'text/html; charset=utf-8')
    res.end(await server.transformIndexHtml('/__preferences.html', `<!doctype html><html lang="zh-CN"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><body style="margin:0;padding:12px;box-sizing:border-box"><div id="app"></div><script type="module">
      import {createApp} from 'vue';import Hub from '/src/components/RoomPreferenceHub.vue';
      const switches={welcome:true,like:true,follow:true,order:true,guide:true,semantic:true,urgent:true,dialogue:true};
      let interaction={tenant_id:14,room_id:29,interaction_active:true,interaction_switches:switches,interaction_controls_revision:1,overall_interaction:50,question_preference:50,welcome_preference:50,engagement_preference:50,chat_preference:50,conversion_preference:50,auto_heat:true};
      let addressing={tenant_id:14,room_id:29,naming_preference:'natural',preferred_terms:['朋友'],blocked_terms:['宝子']};
      window.fixture={calls:[]};window.fetch=async(url,options={})=>{
        const method=options.method||'GET',body=options.body?JSON.parse(options.body):null;
        window.fixture.calls.push({url,method,body});
        if(url==='/api/v1/rooms/29/interaction-preferences'){
          if(method==='PUT')interaction={...interaction,...body,interaction_controls_revision:interaction.interaction_controls_revision+1};
          return Response.json(interaction);
        }
        if(url==='/api/v1/rooms/29/addressing-preferences'){
          if(method==='PUT')addressing={...addressing,...body};return Response.json(addressing);
        }
        throw Error('Unexpected request: '+method+' '+url);
      };
      const mobile=new URLSearchParams(location.search).has('mobile');
      createApp(Hub,{roomId:29,mobile,compact:mobile}).mount('#app');
    </script></body></html>`))
  })
} }
const server = await createServer({ root, configFile:false, plugins:[vue(),plugin], server:{host:'127.0.0.1',port:18628,strictPort:true}, logLevel:'error' })
await server.listen()
const browser = await chromium.launch({ executablePath:[process.env.CHROME_PATH,'C:/Program Files/Google/Chrome/Application/chrome.exe'].find(p=>p&&existsSync(p)), headless:true })
const groups=[], errors=[]
try {
  for (const [width,mobile] of [[1280,false],[777,false],[390,false],[390,true],[320,true]]) {
    const page = await browser.newPage({ viewport:{width,height:1000} })
    page.on('pageerror',e=>errors.push(e.message))
    await page.goto('http://127.0.0.1:18628/__preferences.html'+(mobile?'?mobile':''))
    const nav=page.getByRole('navigation',{name:'直播偏好设置'})
    await page.getByRole('switch',{name:'互动总开关',exact:true}).waitFor()
    assert.deepEqual(await nav.getByRole('button').allTextContents(),['互动开关','称呼习惯'])
    assert.equal(await page.getByText('主播状态',{exact:true}).count(),0)
    assert.equal(await page.locator('.human-behavior-profile').count(),0)
    await page.getByRole('switch',{name:'互动总开关',exact:true}).click()
    await page.waitForFunction(()=>window.fixture.calls.some(c=>c.method==='PUT'&&c.url.endsWith('/interaction-preferences')))
    await nav.getByRole('button',{name:'称呼习惯',exact:true}).click()
    const save=page.getByRole('button',{name:'保存称呼习惯',exact:true})
    await save.waitFor();await page.getByPlaceholder('朋友、老哥、大家').fill('朋友、各位')
    await save.click()
    await page.waitForFunction(()=>window.fixture.calls.some(c=>c.method==='PUT'&&c.url.endsWith('/addressing-preferences')))
    await nav.getByRole('button',{name:'互动开关',exact:true}).click()
    await page.getByRole('switch',{name:'互动总开关',exact:true}).waitFor()
    assert.equal(await page.getByRole('switch',{name:'互动总开关',exact:true}).getAttribute('aria-checked'),'false')
    await nav.getByRole('button',{name:'称呼习惯',exact:true}).click()
    assert.equal(await page.getByPlaceholder('朋友、老哥、大家').inputValue(),'朋友、各位')
    const calls=await page.evaluate(()=>window.fixture.calls)
    assert.ok(calls.every(c=>/\/(interaction|addressing)-preferences$/.test(c.url)))
    assert.deepEqual(calls.find(c=>c.method==='PUT'&&c.url.endsWith('/addressing-preferences')).body.preferred_terms,['朋友','各位'])
    assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),true)
    const columns=await nav.evaluate(el=>getComputedStyle(el).gridTemplateColumns.split(' ').length)
    assert.equal(columns,2)
    await page.screenshot({path:out+'/preferences-'+width+'-'+mobile+'.png',fullPage:true})
    groups.push({width,mobile,passed:true});await page.close()
  }
  assert.deepEqual(errors,[])
  writeFileSync(out+'/results.json',JSON.stringify({passed:true,groups,errors,realProviderCalls:0},null,2))
  console.log(JSON.stringify({passed:true,groups:groups.length,realProviderCalls:0}))
} finally { await browser.close();await server.close() }

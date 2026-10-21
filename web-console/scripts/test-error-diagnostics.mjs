import assert from 'node:assert/strict'
import {createRequire} from 'node:module'
import {fileURLToPath} from 'node:url'
import {mkdir,writeFile} from 'node:fs/promises'
import {createServer} from 'vite'
const {chromium}=createRequire(new URL('../../collector-worker/package.json',import.meta.url))('playwright-core')
const out=process.env.DIAGNOSTIC_PROOF_DIR||'E:/直播伴播-local/error-diagnostics-release-20261021/browser'
await mkdir(out,{recursive:true})
const id=fileURLToPath(new URL('../src/__diagnostic-fixture.vue',import.meta.url)).replaceAll('\\','/')
const fixture=`<script setup lang="ts">
import {nextTick} from 'vue'
import './style.css'
import GlobalFeedback from './components/GlobalFeedback.vue'
import {request} from './api'
import {showAlert,feedbackState,dismissAlert,clearErrorHistory} from './uiFeedback'
import {liveRuntimeFailureMessage} from './liveRuntimeFailure'
import {installUnhandledErrorFeedback} from './unhandledErrorFeedback'
installUnhandledErrorFeedback()
window.diagnostic={async call(path,method='POST'){try{await request(path,{method})}catch{}await nextTick()},async live(failure){showAlert(liveRuntimeFailureMessage({session:{id:389},failure}),'直播声音已停止');await nextTick()},state:feedbackState,dismiss:dismissAlert,clear:clearErrorHistory}
</script><template><GlobalFeedback /></template>`
const server=await createServer({server:{host:'127.0.0.1',port:0},plugins:[{name:'diagnostic-fixture',resolveId(p){if(p==='/__diagnostic-fixture.vue')return id},load(p){if(p===id)return fixture},configureServer(dev){dev.middlewares.use(async(req,res,next)=>{if(req.url!=='/__diagnostic-test')return next();res.setHeader('Content-Type','text/html;charset=utf-8');res.end(await dev.transformIndexHtml(req.url,'<!doctype html><meta charset="utf-8"><link rel="icon" href="data:,"><div id="app"></div><script type="module">import {createApp} from "vue";import App from "/__diagnostic-fixture.vue";createApp(App).mount("#app")</script>'))})}}]})
await server.listen()
const browser=await chromium.launch({executablePath:'C:/Program Files/Google/Chrome/Application/chrome.exe',headless:true})
const errors=[],checks=[]
try{
 const page=await browser.newPage();page.on('pageerror',e=>errors.push(e.message))
 await page.addInitScript(()=>Object.defineProperty(navigator,'clipboard',{value:{writeText:async text=>{window.copiedDiagnostic=text}}}))
 await page.route('**/api/v1/__diagnostic/**',route=>route.fulfill({status:422,contentType:'application/json',headers:{'X-Request-ID':'req-browser-389'},body:JSON.stringify({error:'所选声音已停用 api_key="private key"'})}))
 await page.goto(server.resolvedUrls.local[0]+'__diagnostic-test');await page.waitForFunction(()=>!!window.diagnostic)
 await page.evaluate(()=>window.diagnostic.call('/api/v1/__diagnostic/voice?token=private-token'))
 const message=await page.locator('#system-error-message').innerText()
 for(const part of ['声音已停用','发生操作：声音设置'])assert.ok(message.includes(part),part)
 assert.doesNotMatch(message,/[A-Za-z]|422|\/api|private key|private-token|错误码|请求编号/);checks.push('真实请求失败保留中文原因及操作，隐藏英文和内部代码')
 await page.getByRole('button',{name:'复制错误详情',exact:true}).click()
 await page.waitForFunction(()=>document.querySelector('.diagnostic-copy-status')?.textContent?.includes('复制'))
 assert.ok((await page.locator('.diagnostic-copy-status').innerText()).includes('复制'))
 assert.equal(await page.evaluate(()=>window.copiedDiagnostic),message)
 assert.doesNotMatch(await page.evaluate(()=>window.copiedDiagnostic),/[A-Za-z]|\/api|422|req-/)
 checks.push('复制内容与中文弹窗一致，不含内部诊断')
 await page.getByRole('button',{name:'知道了',exact:true}).click()
 await page.evaluate(()=>window.diagnostic.call('/api/v1/__diagnostic/poll','GET'))
 assert.equal(await page.locator('#system-error-message').count(),0)
 await page.getByRole('button',{name:/错误记录/}).click()
 const history=await page.locator('.error-history-list').innerText()
 assert.match(history,/数据读取/);assert.doesNotMatch(history,/[A-Za-z]|\/api|422/)
 await page.getByRole('button',{name:'关闭',exact:true}).click();checks.push('后台轮询不弹模态，关闭后仍可查错误记录')
 await page.evaluate(()=>window.diagnostic.live({session_id:389,code:'voice_disabled',stage:'声音配置校验',message:'“小芳”声音已停用，无法使用该声音。',action:'重新选择可用声音。',request_id:'req-voice-389'}))
 for(const width of [1600,1024,520,390,320]){
  await page.setViewportSize({width,height:800})
  const metrics=await page.locator('.system-alert-card').evaluate(el=>({left:el.getBoundingClientRect().left,right:el.getBoundingClientRect().right,width:innerWidth,bodyScroll:document.documentElement.scrollWidth,font:parseFloat(getComputedStyle(el.querySelector('#system-error-message')).fontSize)}))
  assert.ok(metrics.left>=-1&&metrics.right<=metrics.width+1&&metrics.bodyScroll<=metrics.width+1,JSON.stringify(metrics))
  assert.ok(metrics.font>=16)
 }
 checks.push('五种宽度含320px不横向溢出、诊断正文至少16px')
 await page.screenshot({path:out+'/voice-disabled-mobile.png',fullPage:true})
 await page.getByRole('button',{name:'知道了',exact:true}).click()
 await page.evaluate(()=>window.dispatchEvent(new ErrorEvent('error',{message:'模拟组件读取失败',filename:'https://web.test/assets/view.js?token=private',lineno:7,colno:3})))
 const browserMessage=await page.locator('#system-error-message').innerText()
 assert.match(browserMessage,/模拟组件读取失败/);assert.doesNotMatch(browserMessage,/[A-Za-z]|7:3|\/assets/)
 checks.push('浏览器异常保留中文原因，隐藏源码路径和行号')
 assert.deepEqual(errors,[])
 await writeFile(out+'/results.json',JSON.stringify({checks,errors},null,2))
 console.log(JSON.stringify({checks,errors},null,2))
}finally{await browser.close();await server.close()}

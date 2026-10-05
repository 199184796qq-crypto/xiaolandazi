import assert from 'node:assert/strict'
import {createRequire} from 'node:module'
import {existsSync} from 'node:fs'
import {startFixture} from './room-parity-fixture.mjs'
const require=createRequire(new URL('../../collector-worker/package.json',import.meta.url))
const {chromium}=require('playwright-core')
const executablePath=[process.env.CHROME_PATH,'C:/Program Files/Google/Chrome/Application/chrome.exe'].find(p=>p&&existsSync(p))
const server=await startFixture(),browser=await chromium.launch({executablePath,headless:true})
try {
  const snapshots=[]
  for(const role of ['customer','staff','ungranted']) {
    const context=await browser.newContext({viewport:{width:1680,height:1000}}),page=await context.newPage(),errors=[]
    page.on('pageerror',e=>errors.push(e.message))
    await page.goto('http://127.0.0.1:18601/__room-parity.html?role='+role)
    await page.locator('.speech-runtime-unified').waitFor()
    await page.getByText('正在回答保存问题哟',{exact:true}).waitFor()
    assert.equal(await page.locator('.speech-runtime-track-grid,.agent-decision-panel,.event-bucket-panel').count(),0,'old duplicate staff-only panels removed')
    const tabs=page.locator('.public-screen-mode-switch')
    const labels=await tabs.locator('button').allTextContents()
    assert.ok(labels.includes('实时公屏')&&labels.includes('事件聚合')&&labels.includes('互动执行'))
    await tabs.getByRole('button',{name:'事件聚合',exact:true}).click()
    assert.equal(await page.locator('.public-screen-panel h3').textContent(),'事件聚合')
    await tabs.getByRole('button',{name:'互动执行',exact:true}).click()
    await page.locator('.interaction-execution-empty').waitFor()
    assert.ok((await page.locator('.interaction-execution-empty').textContent()).includes('智能体还没工作'))
    await page.evaluate(()=>{const now=new Date().toISOString();window.fixture.queue=[{id:'running',title:'正在回答',summary:'当前保存问题',status:'CLAIMED',created_at:now,expires_at:new Date(Date.now()+600000).toISOString(),sources:['agent']},...Array.from({length:12},(_,i)=>({id:'pending-'+i,title:'等待互动'+i,summary:'等待的问题',status:'PENDING',created_at:now,expires_at:new Date(Date.now()+600000).toISOString(),sources:['agent']}))]})
    await page.locator('.interaction-execution-item').first().waitFor()
    assert.equal(await page.locator('.interaction-execution-item').count(),11)
    assert.equal(await page.locator('.interaction-execution-item.running').count(),1)
    assert.ok((await page.locator('.interaction-execution-summary').textContent()).includes('另有 2 条继续排队'))
    await tabs.getByRole('button',{name:'实时公屏',exact:true}).click()
    const snapshot={labels,captions:await page.locator('.speech-runtime-unified').textContent(),headers:await page.locator('h3').allTextContents()}
    if(role!=='ungranted')snapshots.push(snapshot)
    else {
      assert.ok(!labels.includes('互动偏好'),'no private configuration without grant')
      const calls=await page.evaluate(()=>window.fixture.calls)
      assert.ok(!calls.some(c=>c.path.includes('/live-agent-plans')),'no private plan reads without grant')
    }
    assert.ok(!(await page.evaluate(()=>window.fixture.calls)).some(c=>c.path.endsWith('/speech-missions')),'no unused staff-only mission polling')
    assert.deepEqual(errors,[],role+' Vue runtime errors')
    await page.screenshot({path:'../artifacts/room-parity-'+role+'.png',fullPage:true})
    await context.close()
  }
  assert.deepEqual(snapshots[1],snapshots[0],'customer and authorized staff must see identical room modules and captions')
  console.log('Room parity passed: shared caption/events/aggregation/execution, empty and queued states, 1 running + 10 pending, no duplicate legacy panels/polling, ungranted private access protected.')
} finally {await browser.close();await server.close()}

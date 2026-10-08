// Production dashboard with isolated mocked data, no live mutation/model calls.
import assert from 'node:assert/strict'
import {createRequire} from 'node:module'
import {startFixture} from './room-parity-fixture.mjs'
const require=createRequire(new URL('../../collector-worker/package.json',import.meta.url))
const {chromium}=require('playwright-core')
const server=await startFixture(),browser=await chromium.launch({executablePath:'C:/Program Files/Google/Chrome/Application/chrome.exe',headless:true})
try {
 for(const [role,height] of [['customer',800],['customer',1400],['staff',1000]]) {
  const page=await browser.newPage({viewport:{width:1680,height}}),errors=[]
  page.on('pageerror',error=>errors.push(error.message))
  await page.goto('http://127.0.0.1:18601/__room-parity.html?role='+role)
  await page.locator('.speech-runtime-unified').waitFor()
  await page.getByText('正在回答保存问题哟',{exact:true}).waitFor()
  await page.evaluate(()=>new Promise(resolve=>requestAnimationFrame(()=>requestAnimationFrame(resolve))))
  assert.equal(await page.evaluate(()=>window.scrollY),0,`entry stays top for ${role}/${height}`)
  if(height===800) {
   await page.evaluate(()=>window.scrollTo({top:400,behavior:'instant'}))
   assert.ok(await page.evaluate(()=>window.scrollY)>0,'manual scrolling is still enabled')
   await page.evaluate(()=>window.scrollTo({top:0,behavior:'instant'}))
   assert.equal(await page.evaluate(()=>window.scrollY),0)
  }
  assert.deepEqual(errors,[])
  await page.close()
 }
 console.log('PASS: actual dashboard entry stays top after async loading at multiple viewport sizes; customer/staff; manual downward scrolling retained')
}finally{await browser.close();await server.close()}

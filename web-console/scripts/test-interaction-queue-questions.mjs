import assert from 'node:assert/strict'
import {createRequire} from 'node:module'
import {startFixture} from './room-parity-fixture.mjs'

const require=createRequire(new URL('../../collector-worker/package.json',import.meta.url))
const {chromium}=require('playwright-core')
const server=await startFixture()
const browser=await chromium.launch({executablePath:'C:/Program Files/Google/Chrome/Application/chrome.exe',headless:true})
try {
 const page=await browser.newPage({viewport:{width:1680,height:1100}}),errors=[]
 page.on('pageerror',e=>errors.push(e.message))
 await page.goto('http://127.0.0.1:18601/__room-parity.html?role=customer&waiting=1&caption=1')
 await page.evaluate(()=>{
  const now=new Date().toISOString(),expires=new Date(Date.now()+300000).toISOString()
  const base={room_id:15,topic:'Q:test',title:'回答问题',summary:'事件已筛选，进入现有队列，由队列管理播放顺序',mission_kind:'reply_question',priority:80,status:'READY',sources:['agent'],merged_count:1,created_at:now,last_seen_at:now,expires_at:expires,manual_promoted:false}
  window.fixture.queue=[
   {...base,id:'running-question',status:'CLAIMED',claimed_at:now,sample_questions:['开封后能放多久？']},
   {...base,id:'merged-question',merged_count:2,sample_questions:['1号链接和2号链接有什么区别？','两个链接都是5L吗？','1号链接和2号链接有什么区别？',' ']},
   {...base,id:'manual-question',sources:['manual'],manual_action:'answer',title:'运费是怎么算的…',sample_questions:['运费是怎么算的，四川和新疆各要多少？']},
   {...base,id:'welcome',mission_kind:'welcome_named',title:'点名欢迎',summary:'累计3名新人，安排一次点名欢迎',sample_questions:null},
  ]
 })
 await page.getByRole('button',{name:'互动执行',exact:true}).click()
 const card=page.locator('.interaction-execution-item').filter({hasText:'1号链接和2号链接有什么区别？'})
 await card.waitFor()
 const detail=await card.locator('.interaction-execution-copy > p').innerText()
 assert.equal(detail,'1号链接和2号链接有什么区别？\n两个链接都是5L吗？')
 assert.doesNotMatch(detail,/事件已筛选/)
 assert.equal(await card.locator('p').evaluate(e=>getComputedStyle(e).whiteSpace),'pre-line')
 assert.equal(await page.locator('.interaction-execution-item').filter({hasText:'开封后能放多久？'}).count(),1,'claimed answer retains original question')
 assert.equal(await page.locator('.interaction-execution-item').filter({hasText:'运费是怎么算的，四川和新疆各要多少？'}).count(),1,'manual answers show full question rather than truncated title')
 assert.equal(await page.locator('.interaction-execution-item').filter({hasText:'累计3名新人，安排一次点名欢迎'}).count(),1,'non-question tasks retain useful fallback')
 await page.setViewportSize({width:430,height:950})
 assert.equal(await card.locator('p').innerText(),detail,'mobile retains all merged questions')
 assert.deepEqual(errors,[])
 console.log('PASS: original questions visible in pending/claimed/manual tasks; merged questions deduplicated; empty legacy samples handled; mobile text retained')
} finally {await browser.close();await server.close()}

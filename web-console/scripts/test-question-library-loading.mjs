import assert from 'node:assert/strict'
import { createRequire } from 'node:module'
import { readFile, mkdir, writeFile } from 'node:fs/promises'
import path from 'node:path'
import { createServer } from 'vite'

const require = createRequire(new URL('../../collector-worker/package.json', import.meta.url))
const { chromium } = require('playwright-core')
const baselineDir = process.env.QUESTION_LIBRARY_BASELINE_DIR
const proofDir = process.env.QUESTION_LIBRARY_EVIDENCE_DIR
const baseline = baselineDir ? {
  name: 'question-library-baseline', enforce: 'pre',
  async transform(_code, id) {
    if (id.replaceAll('\\', '/').endsWith('/src/components/QuestionLibraryPanel.vue')) {
      return await readFile(path.join(baselineDir, 'QuestionLibraryPanel.vue'), 'utf8')
    }
  },
} : null

const fixture = `<!doctype html><html lang="zh-CN"><meta charset="utf-8"><link rel="icon" href="data:,"><div id="app"></div><script type="module">
import {createApp,h,ref,nextTick} from 'vue';
import Panel from '/src/components/QuestionLibraryPanel.vue';
const query=new URL(location.href).searchParams;
const room=ref(29);
const state={calls:[],listMode:query.get('list')||'ok',purposeMode:query.get('purpose')||'pending',lists:[],purposes:[]};
const entries=[
 {id:1,question_type:'yes_no',question:'能开发票吗',answer:'',sense_key:'',sense_name:'',exclude_words:null},
 {id:2,question_type:'fact',question:'武汉能用吗',answer:'',sense_key:'',sense_name:'',exclude_words:null},
 {id:4,question_type:'fact',question:'砍、切块、剁块、能切吗',answer:'可以砍块，按您要的大小切好再发。',sense_key:'spec',sense_name:'规格与切块',exclude_words:['价','便宜','优惠']},
];
const forRoom=id=>id===32?[{...entries[0],id:32,question:'当前房间问题'}]:entries;
window.fetch=async(url,init={})=>{
 const pathname=new URL(url,location.href).pathname;
 const method=init.method||'GET';state.calls.push({pathname,method});
 if(method!=='GET')throw Error('Unexpected business mutation');
 const parts=pathname.split('/');
 if(parts[3]!=='rooms'||!['question-library','purposes'].includes(parts[5]))return Response.json({});
 const id=Number(parts[4]);
 if(parts[5]==='question-library'){
  if(state.listMode==='error')return Response.json({error:'读取问题库失败'},{status:500});
  if(state.listMode==='empty')return Response.json({items:[]});
  if(state.listMode==='pending')return await new Promise(resolve=>state.lists.push({id,resolve}));
  return Response.json({items:forRoom(id)});
 }
 if(state.purposeMode==='error')return Response.json({error:'目的清单不可用'},{status:500});
 if(state.purposeMode==='pending')return await new Promise(resolve=>state.purposes.push({id,resolve}));
 return Response.json({plan_id:4,items:[{key:'spec',name:'规格与切块'}]});
};
const app=createApp({render:()=>h(Panel,{roomId:room.value})});app.mount('#app');
window.fixture={state,entries,async room(id){room.value=id;await nextTick()},unmount(){app.unmount()},
 releaseList(id){const i=state.lists.findIndex(p=>p.id===id);if(i<0)throw Error('No pending list');state.lists.splice(i,1)[0].resolve(Response.json({items:forRoom(id)}))},
 releasePurpose(id){const i=state.purposes.findIndex(p=>p.id===id);if(i<0)throw Error('No pending purpose');state.purposes.splice(i,1)[0].resolve(Response.json({items:[{key:'old',name:'旧房间目的'}]}))}
};
</script></html>`

const server = await createServer({ plugins: baseline ? [baseline] : [], server: { host: '127.0.0.1', port: 0, proxy: {} }, appType: 'custom' })
server.middlewares.use(async (req, res, next) => {
  if (!req.url.startsWith('/__question_library')) return next()
  res.setHeader('Content-Type', 'text/html;charset=utf-8')
  res.end(await server.transformIndexHtml('/__question_library', fixture))
})
await server.listen()
const browser = await chromium.launch({ executablePath: 'C:/Program Files/Google/Chrome/Application/chrome.exe', headless: true })
const pages = []
const results = []
const rowSelector = '.ql-table tbody tr:has(input[type="text"])'
async function open(query = '') {
  const page = await browser.newPage({ viewport: { width: 1440, height: 950 } })
  pages.push(page)
  page.fixtureErrors = []
  page.on('pageerror', error => page.fixtureErrors.push(error.message))
  await page.route('**/api/**', route => route.fulfill({ json: {} }))
  await page.goto(server.resolvedUrls.local[0] + '__question_library?' + query)
  await page.waitForFunction(() => Boolean(window.fixture))
  return page
}
async function checkRows(page, count) {
  await page.waitForFunction(({ selector, count }) => document.querySelectorAll(selector).length === count, { selector: rowSelector, count })
}
try {
  if (baselineDir) {
    const slow = await open()
    await slow.waitForFunction(() => window.fixture.state.purposes.length === 1)
    assert.equal(await slow.locator(rowSelector).count(), 0)
    results.push({ case: 'baseline_optional_purpose_blocks_valid_rows', reproduced: true })
    const failed = await open('list=error&purpose=error')
    await failed.locator('.ql-error').waitFor()
    assert.ok((await failed.locator('.ql-table').innerText()).includes('还没有条目'))
    results.push({ case: 'baseline_error_misreported_as_empty', reproduced: true })
    console.log('REPRODUCED: an optional pending purpose request blocks existing rows; read failure simultaneously claims there are no entries.')
  } else {
    const slow = await open()
    await checkRows(slow, 3)
    assert.equal(await slow.evaluate(() => window.fixture.state.purposes.length), 1)
    const questions = await slow.locator(`${rowSelector} > td:nth-child(2) input`).evaluateAll(inputs => inputs.map(input => input.value))
    assert.deepEqual(questions, ['能开发票吗', '武汉能用吗', '砍、切块、剁块、能切吗'])
    assert.equal(await slow.locator('.unanswered').count(), 2)
    assert.equal(await slow.locator('textarea').nth(2).inputValue(), '可以砍块，按您要的大小切好再发。')
    assert.equal(await slow.getByRole('button', { name: '刷新', exact: true }).isEnabled(), true)
    results.push({ case: 'existing_questions_and_answer_display_while_purposes_pending', questions })

    const optionalError = await open('purpose=error')
    await checkRows(optionalError, 3)
    assert.equal(await optionalError.locator('.ql-error').count(), 0)
    assert.equal(await optionalError.locator('.ql-sense select').nth(2).inputValue(), 'spec')
    assert.ok((await optionalError.locator('.ql-sense select').nth(2).innerText()).includes('规格与切块'))
    results.push({ case: 'optional_purpose_error_keeps_question_rows' })

    const failed = await open('list=error&purpose=error')
    await failed.locator('.ql-error').waitFor()
    assert.ok(!(await failed.locator('.ql-table').innerText()).includes('还没有条目'))
    assert.ok((await failed.locator('.ql-table').innerText()).includes('无法读取'))
    await failed.evaluate(() => { window.fixture.state.listMode = 'ok' })
    await failed.getByRole('button', { name: '刷新', exact: true }).click()
    await checkRows(failed, 3)
    assert.equal(await failed.locator('.ql-error').count(), 0)
    results.push({ case: 'read_error_is_not_empty_and_retry_restores_rows' })

    await slow.evaluate(() => { window.fixture.state.listMode = 'error' })
    await slow.getByRole('button', { name: '刷新', exact: true }).click()
    await slow.locator('.ql-error').waitFor()
    assert.equal(await slow.locator(rowSelector).count(), 3)
    results.push({ case: 'failed_same_room_refresh_preserves_valid_rows' })

    const race = await open('list=pending')
    await race.waitForFunction(() => window.fixture.state.lists.length === 1)
    await race.evaluate(async () => { window.fixture.state.listMode = 'ok'; await window.fixture.room(32) })
    await checkRows(race, 1)
    await race.evaluate(() => { window.fixture.releaseList(29); window.fixture.releasePurpose(29) })
    await race.waitForTimeout(80)
    assert.equal(await race.locator(`${rowSelector} > td:nth-child(2) input`).inputValue(), '当前房间问题')
    assert.ok(!(await race.locator('.ql-sense select').innerText()).includes('旧房间目的'))
    results.push({ case: 'old_room_list_and_purposes_cannot_overwrite_current_room' })

    const cleared = await open('list=pending')
    await cleared.waitForFunction(() => window.fixture.state.lists.length === 1)
    await cleared.evaluate(async () => { await window.fixture.room(null); window.fixture.releaseList(29) })
    await cleared.waitForTimeout(80)
    assert.equal(await cleared.locator(rowSelector).count(), 0)
    assert.equal(await cleared.getByRole('button', { name: '刷新', exact: true }).isEnabled(), true)
    results.push({ case: 'cleared_room_discards_pending_response' })

    const empty = await open('list=empty&purpose=ok')
    await empty.getByText('还没有条目，点「添加一行」开始。').waitFor()
    assert.equal(await empty.locator('.ql-error').count(), 0)
    results.push({ case: 'successful_empty_list_shows_empty_state' })

    const unmounted = await open('list=pending')
    await unmounted.waitForFunction(() => window.fixture.state.lists.length === 1)
    await unmounted.evaluate(() => { window.fixture.unmount(); window.fixture.releaseList(29); window.fixture.releasePurpose(29) })
    await unmounted.waitForTimeout(80)
    assert.equal(await unmounted.locator('.question-library').count(), 0)
    results.push({ case: 'unmount_discards_pending_requests' })

    if (proofDir) {
      await mkdir(proofDir, { recursive: true })
      await optionalError.screenshot({ path: path.join(proofDir, 'questions-restored.png'), fullPage: true })
    }
    for (const page of pages) {
      assert.deepEqual(page.fixtureErrors, [])
      assert.ok(await page.evaluate(() => window.fixture.state.calls.every(call => call.method === 'GET')))
    }
    console.log('PASS: 8 real Vue/API loading regressions; 3 questions, 2 unanswered rows and 1 answer preserved, no business writes/model calls.')
  }
} finally {
  if (proofDir) {
    await mkdir(proofDir, { recursive: true })
    await writeFile(path.join(proofDir, baselineDir ? 'frontend-before.json' : 'frontend-after.json'), JSON.stringify(results, null, 2))
  }
  await browser.close()
  await server.close()
}

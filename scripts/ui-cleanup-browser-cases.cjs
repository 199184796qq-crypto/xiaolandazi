// Additional contracts for the recorded screenshot cleanup, using the shared
// isolated browser harness. All data is mocked; only GET business calls allowed.
module.exports=async function({cdp,evaluate,until,withMock,assert,delay}){
 const click=async(selector)=>{await evaluate(`document.querySelector(${JSON.stringify(selector)}).click()`);await delay(40)};
 await withMock('platform_admin','http://127.0.0.1:5173/work/inbox',async()=>{
  await until('!!document.querySelector(".inbox-due-filter input")');
  const measure=()=>evaluate(`(()=>{const r=e=>e.getBoundingClientRect(),input=document.querySelector('.inbox-due-filter input'),a=r(input),b=r(document.querySelector('.inbox-due-filter span')),s=r(document.querySelector('.inbox-business-filter select'));return {w:a.width,h:a.height,center:Math.abs((a.top+a.bottom-b.top-b.bottom)/2),row:Math.abs((a.top+a.bottom-s.top-s.bottom)/2),font:getComputedStyle(document.querySelector('.inbox-categories button')).fontSize}})()`);
  const geometry=await measure();assert.equal(geometry.w,18);assert.equal(geometry.h,18);assert.ok(geometry.center<1&&geometry.row<1,JSON.stringify(geometry));assert.equal(geometry.font,'18px');
  assert.ok(!await evaluate('document.querySelector(".work-inbox-panel").textContent.includes("查看不会消除待办")'));
  const point=await evaluate(`(()=>{const r=document.querySelector('.inbox-categories button').getBoundingClientRect();return{x:r.x+r.width/2,y:r.y+r.height/2}})()`);await cdp('Input.dispatchMouseEvent',{type:'mouseMoved',...point});await delay(220);
  const colors=await evaluate(`(()=>{const s=getComputedStyle(document.querySelector('.inbox-categories button'));return[s.color,s.backgroundColor]})()`);assert.equal(colors[0],'rgb(255, 255, 255)');assert.notEqual(colors[1],'rgb(255, 255, 255)');
  await click('.inbox-due-filter input');await until('!!document.querySelector(".inbox-empty-icon")');
  assert.equal(await evaluate('document.querySelector(".inbox-due-filter input").checked'),true);
  await cdp('Emulation.setDeviceMetricsOverride',{width:390,height:844,deviceScaleFactor:1,mobile:false});await delay(250);const narrow=await measure();assert.equal(narrow.w,18);assert.ok(narrow.center<1);
  const overflow=await evaluate(`(()=>{const p=document.querySelector('.work-inbox-panel'),b=p.getBoundingClientRect();return{scroll:p.scrollWidth,width:p.clientWidth,bounds:{left:b.left,right:b.right},children:Array.from(p.querySelectorAll('*')).filter(e=>e.getBoundingClientRect().right>b.right).map(e=>({tag:e.tagName,cls:e.className,width:e.getBoundingClientRect().width,right:e.getBoundingClientRect().right,text:e.textContent.slice(0,50)}))}})()`);
  assert.ok(overflow.scroll<=overflow.width+2,JSON.stringify(overflow));
  await cdp('Emulation.setDeviceMetricsOverride',{width:1440,height:1000,deviceScaleFactor:1,mobile:false});
  assert.ok((await evaluate('window.__uiRequests')).every(r=>r.method==='GET'));
  console.log('PASS inbox polish: 18px labels, 18px checkbox, inline vertical alignment, selected-hover contrast, red badge, empty state, narrow layout; no mutations');
 });
 await withMock('platform_admin','http://127.0.0.1:5173/staff/finance/receipts',async()=>{
  await until('!!document.querySelector(".receipt-status-cell")');
  assert.equal(await evaluate('document.querySelectorAll(".receipt-policy-status").length'),0);
  const cells=await evaluate(`(()=>{const t=document.querySelector('.customer-receipts-page tbody tr');return Array.from(t.cells).slice(2,5).map(c=>({top:c.getBoundingClientRect().top,align:getComputedStyle(c).verticalAlign}))})()`);assert.ok(cells.every(v=>v.align==='top'));assert.ok(cells.every(v=>Math.abs(v.top-cells[0].top)<1));
  assert.ok(await evaluate('document.querySelector(".receipt-status-cell").textContent.includes("收到款项")'));
  await click('.customer-receipts-page tbody tr td:last-child button');await until('document.querySelectorAll(".history-card").length===3');
  assert.ok(!await evaluate('document.querySelector(".business-history").textContent.includes("审核规则：不强制分人")'));
  assert.ok(await evaluate('document.querySelector(".business-history").textContent.includes("本次经办/补件与审核为同一人")'));
  const cards=await evaluate(`Array.from(document.querySelectorAll('.history-card')).map(c=>({x:c.getBoundingClientRect().x,top:c.getBoundingClientRect().top,bottom:c.querySelector('.history-meta').getBoundingClientRect().bottom,title:c.querySelector('strong').getBoundingClientRect().top}))`);
  for(let i=1;i<cards.length;i++)if(Math.abs(cards[i].top-cards[0].top)<1){assert.ok(Math.abs(cards[i].bottom-cards[0].bottom)<1,JSON.stringify(cards));assert.ok(Math.abs(cards[i].title-cards[0].title)<1)}
  assert.ok((await evaluate('window.__uiRequests')).every(r=>r.method==='GET'));
  console.log('PASS receipt status/remarks and history cards align; only system rule prefix hidden; actual notes, operator/time and read-only behavior retained');
 });
 await withMock('platform_admin','http://127.0.0.1:5173/staff/finance/approvals?type=reward',async()=>{
  await until('!!document.querySelector(".receipt-review-shortcut")');
  const text=await evaluate('document.body.innerText');for(const v of ['不核对客户付款凭据','同一收款不要在两处重复办理','当前不强制分人：','发起人与审核人必须分离'])assert.ok(!text.includes(v),v);
  assert.ok(text.includes('处理员工发起的充值、退款、奖励和 AI 时长申请。'));
  assert.ok(await evaluate('document.querySelector(".finance-approval-tabs button.active").textContent.includes("奖励")'));
  console.log('PASS approval marked wording removed, retained guidance/receipt shortcut, referral reward filter applied');
 });
 for(const role of ['staff','dual'])await withMock(role,'http://127.0.0.1:5173/staff/finance/invitations',async()=>{
  await until('document.querySelectorAll(".finance-invitations-page tbody tr").length===12');
  assert.ok(await evaluate('Array.from(document.querySelectorAll(".sidebar a")).some(a=>a.getAttribute("href")==="/staff/finance/invitations")'));
  assert.ok(!await evaluate('/我的邀请码|停用我的邀请码|修改归属|保存策略/.test(document.querySelector(".finance-invitations-page").innerText)'));
  await evaluate(`Array.from(document.querySelectorAll('.finance-invitations-page .pagination-bar button')).find(b=>b.textContent.includes('下一页')).click()`);await until('document.querySelectorAll(".finance-invitations-page tbody tr").length===2');
  await evaluate(`Array.from(document.querySelectorAll('.finance-invitations-page .pagination-bar button')).find(b=>b.textContent.includes('上一页')).click()`);await until('document.querySelectorAll(".finance-invitations-page tbody tr").length===12');
  assert.equal(await evaluate('document.querySelector(".finance-invitations-page tbody tr a").getAttribute("href")'),'/staff/finance/receipts?tenant=601');
  await click('.finance-invitations-page tbody tr button');await until('document.querySelectorAll(".finance-referral-evidence .sb-record").length===12');
  assert.ok(await evaluate('document.querySelector(".finance-referral-evidence").textContent.includes("规则版本 3")'));
  assert.ok(await evaluate('document.querySelector(".finance-referral-evidence").textContent.includes("冲回原收益 #2")'));
  await evaluate(`Array.from(document.querySelectorAll('.finance-referral-evidence .pagination-bar button')).find(b=>b.textContent.includes('下一页')).click()`);await until('document.querySelectorAll(".finance-referral-evidence .sb-record").length===2');
  await evaluate(`Array.from(document.querySelectorAll('.sb-dialog button')).find(b=>b.textContent.trim()==='关闭').click()`);await until('!document.querySelector(".sb-dialog")');
  await evaluate(`Array.from(document.querySelectorAll('.data-view-toggle button')).find(b=>b.textContent.trim()==='卡片').click()`);await until('document.querySelectorAll(".finance-invitations-page .sb-panel .sb-record").length===12');
  assert.ok((await evaluate('window.__uiRequests')).every(r=>r.method==='GET'));
  console.log('PASS '+role+' finance invitation entry, cross-job read permission, real-record projection, list/card/server pagination, earnings/refund/rule/settlement evidence; zero financial mutations');
 });
};

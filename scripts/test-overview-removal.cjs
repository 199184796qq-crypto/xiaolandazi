// Source regression: aggregate dashboards are retired; business details and controls stay.
// Read-only. No credentials, database, network or live business actions are used.
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const req = require('node:module').createRequire(path.resolve(__dirname, '../web-console/package.json'));
const { parse, compileTemplate } = req('vue/compiler-sfc');
const root = path.resolve(__dirname, '..');
function read(p) { return fs.readFileSync(path.join(root, p), 'utf8'); }
function files(p, suffix) { return fs.readdirSync(path.join(root,p), {withFileTypes:true}).flatMap(e => e.isDirectory() ? files(p+'/'+e.name,suffix) : e.name.endsWith(suffix) ? [p+'/'+e.name] : []); }
const retired = new Set(['module-hub-metrics-v2','stat-grid','stat-card','admin-kpi-grid','admin-kpi-card','overview-metric-grid','commercial-summary-grid','customer-admin-stats','sb-metrics','room-quota-metrics','marketing-summary','operating-summary-grid','finance-approval-metrics','staff-summary-grid','finance-stat-card','invitation-stat-card']);
const violations = [];
const vueFiles = files('web-console/src','.vue');
for(const file of vueFiles) {
  const result = parse(read(file));
  assert.equal(result.errors.length,0,file+' SFC parse');
  if(!result.descriptor.template) continue;
  const template = result.descriptor.template;
  const compiled = compileTemplate({source:template.content,filename:file,id:'overview-removal'});
  assert.equal(compiled.errors.length,0,file+' template parse');
  function walk(n) {
    if(n.type===1) {
      const classes=(n.props.find(p=>p.type===6 && p.name==='class')?.value?.content||'').split(/\s+/);
      for(const c of classes) if(retired.has(c)) violations.push(file+' .'+c);
    }
    for(const c of n.children||[])walk(c);
  }
  walk(template.ast);
}
assert.deepEqual(violations,[], 'retired statistics must not remain in rendered templates');
for(const p of ['customer-mobile','sales-mobile']) {
  const svelte = require('node:module').createRequire(path.join(root,p,'package.json'))('svelte/compiler');
  for(const file of files(p+'/src','.svelte')) {
    const src=read(file);svelte.parse(src,{modern:true});
    assert.ok(!/class=["'][^"']*\bmetric-(?:grid|card)\b/.test(src),file+' old mobile statistics');
  }
  const home=read(p+'/src/routes/+page.svelte');
  assert.ok(!/getRooms|getCurrentResources|getSalesCustomers|getSalesPerformance/.test(home),p+' still loads summary-only business data');
  assert.ok(home.includes('quick-grid')&&home.includes('agent-hero'),p+' loses workflow entry points');
}
const hub=read('web-console/src/views/DomainHubView.vue');
assert.ok(!/loadMetrics|getStaffFinanceOverview|from '\.\.\/api'/.test(hub),'function hubs should not fetch lists solely to count them');
assert.ok(hub.includes('canShowEntry') && hub.includes('quickEntries') && hub.includes('ModulePageNav'),'permission-filtered navigation must remain');
const workspace=read('web-console/src/views/SalesWorkspaceView.vue');
assert.ok(!workspace.includes('getMySalesPerformance'),'sales home still calls summary-only performance API');
assert.ok(workspace.includes('getSalesCustomers')&&workspace.includes('getSalesFollowups'),'recent business records must remain');
const money=read('web-console/src/views/CustomerMoneyView.vue');
assert.ok(money.includes('moneyList')&&money.includes('PaginationBar')&&!money.includes('recognition('),'money history retains real records and paging without summary request');
const receipts=read('web-console/src/views/CustomerReceiptsView.vue');
assert.ok(receipts.includes('CustomerReceiptReviewAction')&&receipts.includes('PaginationBar')&&receipts.includes('receiptList'),'receipt review and paging must remain');
assert.ok(!receipts.includes('receiptTodos('),'receipt page should not request counts for a removed block');
const room=read('web-console/src/views/RoomDetailView.vue');
assert.ok(room.includes('@click="handleStartAI"')&&room.includes('@click="handleStopAI"')&&room.includes('runtime-quota-brief'),'AI controls and duration assets must remain');
const handover=read('web-console/src/views/SalesHandoverView.vue');
assert.ok(handover.includes('本次交接范围')&&handover.includes('preview.customer_count')&&handover.includes('preview.open_lead_count'),'handover confirmation scope must remain');
const exit=read('web-console/src/views/AgentExitView.vue');
assert.ok(exit.includes('check.blockers')&&exit.includes('check.can_exit')&&exit.includes('历史清算记录'),'exit preconditions/history must remain');
assert.ok(read('web-console/src/views/FinanceView.vue').includes('wallet-recharge-button'),'wallet recharge control must remain');
assert.ok(read('web-console/src/views/InvitationsView.vue').includes('copyText(registrationUrl'),'invitation actions must remain');
console.log('PASS '+vueFiles.length+' Vue templates and both mobile apps: no retired statistic blocks; real records, pagination, permissions, balances and actions preserved.');

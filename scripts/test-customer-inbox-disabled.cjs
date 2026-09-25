// Read-only product-surface contract; does not execute business APIs.
const fs=require('node:fs'),path=require('node:path'),assert=require('node:assert/strict'),vm=require('node:vm');
const root=path.resolve(__dirname,'..');const ts=require(path.join(root,'web-console/node_modules/typescript'));
const source=fs.readFileSync(path.join(root,'web-console/src/workInboxRoutes.ts'),'utf8');
const js=ts.transpileModule(source,{compilerOptions:{module:ts.ModuleKind.CommonJS,target:ts.ScriptTarget.ES2022}}).outputText;
const context={exports:{}};vm.runInNewContext(js,context);
for(const role of [undefined,'','customer','unknown'])assert.equal(context.exports.canUseWorkInbox(role),false);
for(const role of ['platform_admin','staff','sales_staff','agent_admin'])assert.equal(context.exports.canUseWorkInbox(role),true);
function walk(dir){for(const item of fs.readdirSync(dir,{withFileTypes:true})){const file=path.join(dir,item.name);if(item.isDirectory())walk(file);else if(/\.(ts|svelte)$/.test(item.name)){const text=fs.readFileSync(file,'utf8');assert.ok(!/\/api\/v1\/work\/inbox|InboxBridge|InboxBadge|\$lib\/workInbox|\$lib\/WorkInbox/.test(text),'terminal inbox reference remains: '+file)}}}
walk(path.join(root,'customer-mobile/src'));
const sales=fs.readFileSync(path.join(root,'sales-mobile/src/routes/agent/+page.svelte'),'utf8');assert.match(sales,/WorkInbox/);assert.match(sales,/InboxBadge/);
const support=fs.readFileSync(path.join(root,'customer-mobile/src/lib/SupportPage.svelte'),'utf8');assert.match(support,/\/api\/v1\/service\/tickets/);assert.match(support,/confirm/);
console.log('PASS customer inbox product gate, customer mobile source removal, sales preservation and native customer support preservation');

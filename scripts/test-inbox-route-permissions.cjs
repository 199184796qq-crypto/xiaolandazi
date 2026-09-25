const fs=require('node:fs'),path=require('node:path'),assert=require('node:assert/strict'),vm=require('node:vm');
const root=path.resolve(__dirname,'..');
const ts=require(path.join(root,'web-console/node_modules/typescript'));
const source=fs.readFileSync(path.join(root,'web-console/src/workInboxRoutes.ts'),'utf8');
const js=ts.transpileModule(source,{compilerOptions:{module:ts.ModuleKind.CommonJS,target:ts.ScriptTarget.ES2022}}).outputText;
const context={exports:{}};vm.runInNewContext(js,context);const can=context.exports.canOpenWorkInboxRoute;
function permission(...items){return p=>items.includes(p)}
assert.equal(can('/resources/inventory','staff',permission('inventory.view')),false);
assert.equal(can('/resources/inventory','staff',permission('inventory.view','inventory.manage')),true);
assert.equal(can('/resources/logistics','staff',permission('logistics.view','logistics.manage')),true);
assert.equal(can('/staff/after-sales','staff',permission('inventory.after_sales.view','inventory.after_sales.manage')),true);
assert.equal(can('/staff/after-sales','staff',permission('inventory.after_sales.view')),false);
assert.equal(can('/operations/support','staff',permission('liveops.configure')),true);
assert.equal(can('/system/settings','staff',()=>true),false);
assert.equal(can('/resources/inventory','customer',()=>true),false);
assert.equal(can('/resources/inventory','agent_admin',()=>true),false);
console.log('PASS narrow cross-department inbox navigation: explicit operational rights only; no customer, agent, read-only or system settings bypass');

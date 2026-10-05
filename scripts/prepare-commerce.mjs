import fs from 'node:fs'
import path from 'node:path'
import crypto from 'node:crypto'
import assert from 'node:assert/strict'
import {execFileSync} from 'node:child_process'
const root=path.resolve(import.meta.dirname,'..'),id=process.argv[2]
assert.match(id||'',/^[A-Za-z0-9][A-Za-z0-9._-]+$/)
const hash=v=>crypto.createHash('sha256').update(v).digest('hex'),digest=p=>hash(fs.readFileSync(p))
const read=p=>fs.readFileSync(path.join(root,p),'utf8'),json=p=>JSON.parse(read(p))
const base=JSON.parse(fs.readFileSync('E:/直播伴播-local/releases/20261004-speech-model-discovery-v1-dirty-manifest.json','utf8'))
const expected=new Map([...base.files,...json('artifacts/20261004-model-picker-v2/manifest.json').source_files,...json('artifacts/20261004-ops-rooms-v1-dirty-manifest.json').files,...json('artifacts/20261004-room-parity-v1/manifest.json').source_files].filter(v=>v.sha256).map(v=>[v.path,v.sha256]))
const overlay=json('artifacts/commerce-isolation/overlay.json').Replace
const key=p=>path.join(root,p)
function effective(p){return overlay[key(p)]??key(p)}
const goOwn=new Set([
 'management-service/cmd/management/main.go','management-service/internal/model/marketing.go',
 ...['commercial.go','marketing.go','marketing_store.go','shop_orders.go','referral_rewards.go','referral_wallet.go','device_order_refund.go','incentives.go','sales_business_mysql_test.go','sales_business_scratch_test.go'].map(n=>'management-service/internal/db/'+n),
 ...['server.go','marketing.go','shop_orders.go','commercial_memberships.go'].map(n=>'management-service/internal/httpapi/'+n),
 ...['commerce_details.go','commerce_mysql_test.go','commerce_rules.go','commerce_rules_schema.sql','commerce_rules_test.go','commerce_targets.go','commerce_wallet.go','marketing_controls.go','marketing_eligibility.go','marketing_free_claim.go'].map(n=>'management-service/internal/db/'+n),
 ...['commerce_rules.go','commerce_wallet.go'].map(n=>'management-service/internal/httpapi/'+n),
 'management-service/internal/model/commerce_rules.go'
])
const uiOwn=new Set([
 'web-console/src/types.ts','web-console/src/commerce.ts',
 ...['CommerceRulesPanel.vue','SalesCommissionWallet.vue','CustomerMembershipCenter.vue'].map(n=>'web-console/src/components/'+n),
 ...['MarketingDesignView.vue','IncentiveProgramsView.vue','SalesPerformanceView.vue','SettlementBatchesView.vue','ShopView.vue','CommercialTimeCardsView.vue','CommercialDeviceProductsView.vue','CommercialMembershipsView.vue'].map(n=>'web-console/src/views/'+n)
])
const mobileOwn=new Set(['customer-mobile/src/lib/api.ts','customer-mobile/src/lib/types.ts','customer-mobile/src/routes/shop/+page.svelte','customer-mobile/src/routes/shop/checkout/+page.svelte'])
const salesOwn=new Set(['sales-mobile/src/lib/api.ts','sales-mobile/src/lib/types.ts','sales-mobile/src/routes/performance/+page.svelte'])
function source(p){if(p==='web-console/src/api.ts'||p==='web-console/src/components/RoomHumanBehaviorProfile.vue')return key('artifacts/commerce-isolation/'+p);return effective(p)}
function matches(p,sha){const s=fs.readFileSync(source(p),'utf8'),lf=s.replaceAll('\r\n','\n');return [s,lf,lf.replaceAll('\n','\r\n')].some(v=>hash(v)===sha)}
const mismatches=[]
for(const [p,sha] of expected){
 if(!/^(management-service|web-console\/src|customer-mobile\/src|sales-mobile\/src)\//.test(p)||goOwn.has(p)||uiOwn.has(p)||mobileOwn.has(p)||salesOwn.has(p))continue
 if(p.startsWith('management-service/')&&!/\.(go|sql)$/.test(p)&&!p.endsWith('go.mod')&&!p.endsWith('go.sum'))continue
 if(p==='management-service/internal/db/live_runtime_schema.sql'){assert.equal(digest(source(p)),'0737066a25845332d84d775083dda75d749df103514098d82838b0eb7d38f6ea');execFileSync(process.execPath,[key('scripts/audit-commerce-isolation.mjs'),'proof'],{cwd:root,stdio:'inherit'});continue}
 if(p==='web-console/src/api.ts'){
  // Only type-only diagnostics and newline normalization differ; compiled API equals production.
  assert.equal(digest(source(p)),'e979631d196383d3123f349edc3add4c4b821d8a8d623ada0cdda9d769046a4e')
  assert.equal(digest(key('artifacts/commerce-dist/assets/api-oysGtm0K.js')),'4b2f14f64b640ea8f7102c52ad3ea6220cfd42798c69bbfb3ad19d6d5e5891cb');continue
 }
 if(p==='web-console/src/views/LiveStrategyView.vue'){
  const s=read('artifacts/commerce-isolation/LiveStrategyView.pinned.vue').replace(/^.*<small v-if="anchorStyleTestResult\.(?:runtime_evaluation|style_purity|style_vector_evaluation\?\.available)">.*\r?\n/gm,'').replaceAll('\r\n','\n')
  assert.equal(hash(s),'02d356fab610efac7469f5650c89441b4e3ddca4cb162e0fb78d1bf8bd783f2c');continue
 }
 if(!source(p))continue
 if(!matches(p,sha))mismatches.push(p)
}
assert.deepEqual(mismatches,[],'Unrelated source drift')
function walk(dir){const a=[];for(const e of fs.readdirSync(dir,{withFileTypes:true})){const p=path.join(dir,e.name);if(e.isDirectory())a.push(...walk(p));else if(e.isFile())a.push(p);else throw Error('Source symlink '+p)}return a}
const sources=walk(key('management-service/internal')).concat(walk(key('management-service/cmd')),['management-service/go.mod','management-service/go.sum'].map(key)).filter(p=>/\.(go|sql)$/.test(p)||/go\.(mod|sum)$/.test(p))
const late=sources.filter(p=>fs.statSync(p).mtimeMs>Date.parse('2026-10-04T14:45:00Z')&&!Object.hasOwn(overlay,p)&&!goOwn.has(path.relative(root,p).replaceAll('\\','/')))
// Tests do not enter the binary; nevertheless unknown runtime changes block publication.
assert.deepEqual(late.filter(p=>!p.endsWith('_test.go')),[],'Unknown new runtime sources')
const frozen=key('artifacts/'+id+'-sources'),replace={...overlay},records=[]
fs.mkdirSync(frozen)
for(const p of sources){const rel=path.relative(root,p).replaceAll('\\','/'),actual=effective(rel);if(!actual)continue;const dest=path.join(frozen,rel);fs.mkdirSync(path.dirname(dest),{recursive:true});fs.copyFileSync(actual,dest);replace[p]=dest;records.push({path:rel,sha256:digest(dest),build_source:path.relative(root,actual).replaceAll('\\','/')})}
const frozenOverlay=key('artifacts/'+id+'-overlay.json');fs.writeFileSync(frozenOverlay,JSON.stringify({Replace:replace}))
const stage=key('artifacts/'+id),dist=key('artifacts/commerce-dist');fs.mkdirSync(stage);fs.mkdirSync(path.join(stage,'bin'))
const refs=s=>[...s.matchAll(/(?:assets\/|\.\/)([A-Za-z0-9_-]+-[A-Za-z0-9_-]{8}\.(?:js|css))/g)].map(m=>m[1])
const html=fs.readFileSync(path.join(dist,'index.html'),'utf8'),files=new Set(refs(html)),todo=[...files]
while(todo.length){const f=todo.pop();if(f.endsWith('.js'))for(const n of refs(fs.readFileSync(path.join(dist,'assets',f),'utf8')))if(!files.has(n)){files.add(n);todo.push(n)}}
assert.equal(files.size,27)
for(const surface of ['web','web-desktop']){fs.mkdirSync(path.join(stage,surface,'assets'),{recursive:true});fs.copyFileSync(path.join(dist,'index.html'),path.join(stage,surface,'index.html'));for(const f of files)fs.copyFileSync(path.join(dist,'assets',f),path.join(stage,surface,'assets',f))}
function copyTree(a,b){fs.mkdirSync(b,{recursive:true});for(const p of fs.readdirSync(a,{withFileTypes:true})){const x=path.join(a,p.name),y=path.join(b,p.name);if(p.isDirectory())copyTree(x,y);else if(p.isFile())fs.copyFileSync(x,y);else throw Error('Build symlink')}}
copyTree(key('customer-mobile/build'),path.join(stage,'web-customer'))
copyTree(key('sales-mobile/build'),path.join(stage,'web-sales'))
assert.ok(fs.readFileSync(path.join(stage,'web-customer/index.html'),'utf8').includes('content="customer-mobile"'))
for(const dir of ['web-console/src','customer-mobile/src','sales-mobile/src'])for(const p of walk(key(dir))){const rel=path.relative(root,p).replaceAll('\\','/'),actual=rel==='web-console/src/views/LiveStrategyView.vue'?key('artifacts/commerce-isolation/LiveStrategyView.pinned.vue'):rel==='web-console/src/types.ts'?key('artifacts/commerce-isolation/'+rel):source(rel);records.push({path:rel,sha256:digest(actual),build_source:path.relative(root,actual).replaceAll('\\','/')})}
execFileSync('go',['build','-overlay',frozenOverlay,'-trimpath','-ldflags','-s -w','-o',path.join(stage,'bin/management-service'),'./cmd/management'],{cwd:key('management-service'),env:{...process.env,GOOS:'linux',GOARCH:'amd64',CGO_ENABLED:'0'},stdio:'inherit'})
const binary=fs.readFileSync(path.join(stage,'bin/management-service'))
const oldSchema=fs.readFileSync(key('artifacts/commerce-isolation/management-service/internal/db/live_runtime_schema.sql'))
assert.ok(binary.includes(oldSchema),'Go embedding ignored frozen SQL')
assert.ok(binary.includes(fs.readFileSync(key('management-service/internal/db/commerce_rules_schema.sql'))),'New additive schema absent')
assert.ok(!binary.includes(Buffer.from('CREATE TABLE IF NOT EXISTS live_agent_plan_style_overlays')),'Unpublished style schema leaked')
const mf=key('artifacts/'+id+'-dirty-manifest.json')
const packaged=walk(stage).map(p=>({path:path.relative(stage,p).replaceAll('\\','/'),sha256:digest(p)}))
fs.writeFileSync(mf,JSON.stringify({git_head:execFileSync('git',['rev-parse','HEAD'],{cwd:root,encoding:'utf8'}).trim(),dirty:true,scope:['bin/management-service','web','web-desktop','web-customer','web-sales'],files:records,packaged_files:packaged,source_audit:true,isolated_pending_style:true,legacy_configuration_unchanged:true},null,2))
const archive=key('artifacts/'+id+'.tar.gz');execFileSync('tar',['-czf',archive,'-C',stage,'bin/management-service','web','web-desktop','web-customer','web-sales'])
console.log(JSON.stringify({id,archive,archive_sha:digest(archive),binary_sha:digest(path.join(stage,'bin/management-service')),manifest:mf,manifest_sha:digest(mf),source_files:records.length,packaged_files:packaged.length,desktop_graph:files.size}))

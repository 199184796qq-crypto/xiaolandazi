import assert from 'node:assert/strict'
import {readFileSync,mkdirSync,copyFileSync,writeFileSync} from 'node:fs'
import {createHash} from 'node:crypto'
import {execFileSync} from 'node:child_process'
import {join,resolve} from 'node:path'
const id=process.argv[2],root=resolve(import.meta.dirname,'..')
assert.match(id||'',/^[a-zA-Z0-9][a-zA-Z0-9._-]+$/)
const dist=join(root,'artifacts/ops-rooms-dist'),base=join(root,'artifacts/20261004-model-picker-v2'),stage=join(root,'artifacts',id)
const hash=data=>createHash('sha256').update(data).digest('hex')
const digest=p=>hash(readFileSync(p)),normalize=s=>s.replace(/([A-Za-z0-9_-]+)-[A-Za-z0-9_-]{8}\.(js|css)/g,'$1-HASH.$2')
const refs=s=>[...s.matchAll(/(?:assets\/|\.\/)([A-Za-z0-9_-]+-[A-Za-z0-9_-]{8}\.(?:js|css))/g)].map(m=>m[1])
const html=readFileSync(join(dist,'index.html'),'utf8'),baseHTML=readFileSync(join(base,'index.html'),'utf8')
assert.equal(normalize(html),normalize(baseHTML),'unexpected HTML changes')
const active=new Set(refs(html)),pending=[...active]
while(pending.length){const f=pending.pop();if(f.endsWith('.js'))for(const next of refs(readFileSync(join(dist,'assets',f),'utf8')))if(!active.has(next)){active.add(next);pending.push(next)}}
const bm=JSON.parse(readFileSync(join(base,'manifest.json'),'utf8'))
const byKey=new Map(bm.files.map(f=>[f.replace(/-[A-Za-z0-9_-]{8}(?=\.(js|css)$)/,''),f]))
const changed=[]
for(const f of active){const k=f.replace(/-[A-Za-z0-9_-]{8}(?=\.(js|css)$)/,'');const old=byKey.get(k);assert.ok(old,'new unexpected chunk '+f);if(normalize(readFileSync(join(dist,'assets',f),'utf8'))!==normalize(readFileSync(join(base,'assets',old),'utf8')))changed.push(k)}
// Rollup renumbers imported minifier symbols when an API export is added.
// Verify source identity instead of treating those dependent hash changes as edits.
const baseline=JSON.parse(readFileSync('E:/直播伴播-local/releases/20261004-speech-model-discovery-v1-dirty-manifest.json','utf8'))
const allowed=new Set(['web-console/src/api.ts','web-console/src/types.ts','web-console/src/views/RoomsView.vue','web-console/src/views/LiveStrategyView.vue','web-console/src/views/SpeechModelsView.vue'])
for(const f of baseline.files)if(f.sha256&&f.path.startsWith('web-console/src/')&&!allowed.has(f.path))assert.equal(digest(join(root,f.path)),f.sha256,'unrelated frontend source changed '+f.path)
const goAllowed=new Set(['management-service/internal/httpapi/server.go','management-service/internal/httpapi/room_cooperation.go'])
for(const f of baseline.files)if(f.sha256&&f.path.startsWith('management-service/')&&f.path.endsWith('.go')&&!goAllowed.has(f.path)){
 const source=f.path.includes('live_agent_plan_style_preview')?join(root,'artifacts/ops-rooms-overlay',f.path.split('/').at(-1)):join(root,f.path)
 assert.equal(digest(source),f.sha256,'unrelated management source changed '+f.path)
}
// All saved model-picker source and chunks must remain unchanged.
for(const f of bm.source_files)assert.equal(digest(join(root,f.path)),f.sha256,f.path+' changed')
mkdirSync(stage);mkdirSync(join(stage,'bin'));mkdirSync(join(stage,'web'));mkdirSync(join(stage,'web-desktop'))
for(const surface of ['web','web-desktop']){mkdirSync(join(stage,surface,'assets'));copyFileSync(join(dist,'index.html'),join(stage,surface,'index.html'));for(const f of active)copyFileSync(join(dist,'assets',f),join(stage,surface,'assets',f))}
execFileSync('go',['build','-overlay',join(root,'artifacts/ops-rooms-overlay/overlay.json'),'-trimpath','-ldflags','-s -w','-o',join(stage,'bin/management-service'),'./cmd/management'],{cwd:join(root,'management-service'),env:{...process.env,GOOS:'linux',GOARCH:'amd64',CGO_ENABLED:'0'},stdio:'inherit'})
const sources=['management-service/internal/db/room_customers.go','management-service/internal/httpapi/room_customer_contact.go','management-service/internal/httpapi/room_cooperation.go','management-service/internal/httpapi/server.go','web-console/src/api.ts','web-console/src/types.ts','web-console/src/views/RoomsView.vue','web-console/src/components/RoomListIcon.vue']
const mf=join(root,'artifacts',id+'-dirty-manifest.json')
writeFileSync(mf,JSON.stringify({git_head:execFileSync('git',['rev-parse','HEAD'],{cwd:root,encoding:'utf8'}).trim(),dirty:true,scope:['bin/management-service','web','web-desktop'],files:sources.map(path=>({path,sha256:digest(join(root,path))})),isolated_pending_style:true,unchanged_other_sources:true},null,2))
const archive=join(root,'artifacts',id+'.tar.gz')
execFileSync('tar',['-czf',archive,'-C',stage,'bin/management-service','web','web-desktop'])
console.log(JSON.stringify({id,archive,archive_sha:digest(archive),binary_sha:digest(join(stage,'bin/management-service')),manifest:mf,manifest_sha:digest(mf),changed,files:active.size}))

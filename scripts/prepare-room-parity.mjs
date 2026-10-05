import assert from 'node:assert/strict'
import {readFileSync,mkdirSync,copyFileSync,writeFileSync} from 'node:fs'
import {createHash} from 'node:crypto'
import {execFileSync} from 'node:child_process'
import {join,resolve} from 'node:path'
const root=resolve(import.meta.dirname,'..'),id=process.argv[2]
assert.match(id||'',/^[a-zA-Z0-9][a-zA-Z0-9._-]+$/)
const dist=join(root,'artifacts/room-parity-dist'),base=join(root,'artifacts/20261004-ops-rooms-v1/web-desktop'),stage=join(root,'artifacts',id)
const hash=data=>createHash('sha256').update(data).digest('hex'),digest=p=>hash(readFileSync(p))
const normalize=s=>s.replace(/([A-Za-z0-9_-]+)-[A-Za-z0-9_-]{8}\.(js|css)/g,'$1-HASH.$2')
const refs=s=>[...s.matchAll(/(?:assets\/|\.\/)([A-Za-z0-9_-]+-[A-Za-z0-9_-]{8}\.(?:js|css))/g)].map(m=>m[1])
function graph(dir){const active=new Set(refs(readFileSync(join(dir,'index.html'),'utf8'))),pending=[...active];while(pending.length){const f=pending.pop();if(f.endsWith('.js'))for(const n of refs(readFileSync(join(dir,'assets',f),'utf8')))if(!active.has(n)){active.add(n);pending.push(n)}}return active}
const baseline=JSON.parse(readFileSync('E:/直播伴播-local/releases/20261004-speech-model-discovery-v1-dirty-manifest.json','utf8'))
const ops=JSON.parse(readFileSync(join(root,'artifacts/20261004-ops-rooms-v1-dirty-manifest.json'),'utf8'))
const picker=JSON.parse(readFileSync(join(root,'artifacts/20261004-model-picker-v2/manifest.json'),'utf8'))
const expected=new Map([...baseline.files,...picker.source_files,...ops.files].filter(f=>f.sha256&&f.path.startsWith('web-console/src/')).map(f=>[f.path,f.sha256]))
const mismatches=[]
for(const [path,sha] of expected){
  if(path==='web-console/src/views/RoomDetailView.vue'||path==='web-console/src/types.ts')continue
  let text=readFileSync(join(root,path))
  if(path==='web-console/src/api.ts')text=Buffer.from(text.toString().replace(/^.*(?:style_purity|style_vector_evaluation)\?:.*\r?\n/gm,''))
  if(path==='web-console/src/views/LiveStrategyView.vue')text=Buffer.from(text.toString().replace(/^.*<small v-if="anchorStyleTestResult\.(?:runtime_evaluation|style_purity|style_vector_evaluation\?\.available)">.*\r?\n/gm,''))
  const variants=[text,text.toString().replaceAll('\r\n','\n'),text.toString().replaceAll('\r\n','\n').replaceAll('\n','\r\n')]
  if(!variants.some(t=>hash(t)===sha))mismatches.push(path)
}
assert.deepEqual(mismatches,[],'unrelated runtime frontend sources changed')
const html=readFileSync(join(dist,'index.html'),'utf8'),oldHTML=readFileSync(join(base,'index.html'),'utf8')
assert.equal(normalize(html),normalize(oldHTML),'HTML markup changed')
const active=graph(dist),old=graph(base),key=f=>f.replace(/-[A-Za-z0-9_-]{8}(?=\.(js|css)$)/,''),byKey=new Map([...old].map(f=>[key(f),f]))
assert.deepEqual([...active].map(key).sort(),[...old].map(key).sort(),'bundle graph changed')
const changed=[]
const normalizeCSS=s=>s.replace(/data-v-[a-f0-9]+/g,'data-v-HASH').replace(/(?<=[A-Za-z])-[a-f0-9]{8}(?=[;,\s{}])/g,'-SCOPE')
for(const f of active){const before=readFileSync(join(base,'assets',byKey.get(key(f))),'utf8'),after=readFileSync(join(dist,'assets',f),'utf8');if(normalize(before)!==normalize(after))changed.push(key(f));if(f.endsWith('.css')){const a=normalizeCSS(after),b=normalizeCSS(before);if(a!==b){let i=0;while(a[i]===b[i]&&i<a.length)i++;throw Error('unrelated CSS changed '+f+' at '+i+' NEW '+a.slice(i-100,i+150)+' OLD '+b.slice(i-100,i+150))}}}
mkdirSync(stage);mkdirSync(join(stage,'assets'))
copyFileSync(join(dist,'index.html'),join(stage,'index.html'));for(const f of active)copyFileSync(join(dist,'assets',f),join(stage,'assets',f))
const main=/src="\/assets\/(index-[^"/]+\.js)"/.exec(html)[1]
const manifest={scope:'room-parity-desktop-only',main,files:[...active].sort(),sha256:Object.fromEntries([...active].map(f=>[f,digest(join(stage,'assets',f))])),source_audit:true,isolated_pending_style:true,source_files:[{path:'web-console/src/views/RoomDetailView.vue',sha256:digest(join(root,'web-console/src/views/RoomDetailView.vue'))}]}
writeFileSync(join(stage,'manifest.json'),JSON.stringify(manifest,null,2))
const archive=join(root,'artifacts',id+'.tar.gz');execFileSync('tar',['-czf',archive,'-C',stage,'index.html','manifest.json','assets'])
console.log(JSON.stringify({id,archive,archive_sha:digest(archive),base_html_sha:digest(join(base,'index.html')),base_main:[...old].find(f=>/^index-.*\.js$/.test(f)),base_js_sha:digest(join(base,'assets',[...old].find(f=>/^index-.*\.js$/.test(f)))),changed,files:active.size}))

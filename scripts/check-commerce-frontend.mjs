import fs from 'node:fs'
import path from 'node:path'
import {execFileSync} from 'node:child_process'
const root=path.resolve(import.meta.dirname,'..'),stage=path.join(root,'artifacts/commerce-typecheck')
fs.mkdirSync(stage,{recursive:true})
function copyTree(from,to){fs.mkdirSync(to,{recursive:true});for(const e of fs.readdirSync(from,{withFileTypes:true})){const a=path.join(from,e.name),b=path.join(to,e.name);if(e.isDirectory())copyTree(a,b);else if(e.isFile())fs.copyFileSync(a,b);else throw Error('Unsupported source entry '+a)}}
copyTree(path.join(root,'web-console/src'),path.join(stage,'src'))
copyTree(path.join(root,'shared'),path.join(root,'artifacts/shared'))
for(const p of ['src/api.ts','src/types.ts','src/components/RoomHumanBehaviorProfile.vue'])fs.copyFileSync(path.join(root,'artifacts/commerce-isolation/web-console',p),path.join(stage,p))
fs.copyFileSync(path.join(root,'artifacts/commerce-isolation/LiveStrategyView.pinned.vue'),path.join(stage,'src/views/LiveStrategyView.vue'))
const deps=path.join(stage,'node_modules')
if(!fs.existsSync(deps))fs.symlinkSync(path.join(root,'web-console/node_modules'),deps,'junction')
fs.copyFileSync(path.join(root,'web-console/tsconfig.app.json'),path.join(stage,'tsconfig.app.json'))
execFileSync(process.execPath,[path.join(root,'web-console/node_modules/vue-tsc/bin/vue-tsc.js'),'--noEmit','-p',path.join(stage,'tsconfig.app.json')],{cwd:root,stdio:'inherit'})
console.log('Isolated commerce frontend typecheck passed; shared checkout untouched')

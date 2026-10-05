import {defineConfig,mergeConfig} from 'vite'
import {readFileSync} from 'node:fs'
import {createHash} from 'node:crypto'
import {resolve} from 'node:path'
import base from './vite.config'
export default mergeConfig(base,defineConfig({
 plugins:[{name:'isolated-commerce-sources',enforce:'pre',transform(_code,id){
  const normalized=id.replaceAll('\\','/')
  if(normalized.endsWith('/src/views/LiveStrategyView.vue')){
   const code=readFileSync(resolve('../artifacts/commerce-isolation/LiveStrategyView.pinned.vue'),'utf8').replace(/^.*<small v-if="anchorStyleTestResult\.(?:runtime_evaluation|style_purity|style_vector_evaluation\?\.available)">.*\r?\n/gm,'')
   // Exact deployed source recovered as 22d1bc28...; normalize its mixed CRLF/LF only.
   if(createHash('sha256').update(code.replaceAll('\r\n','\n')).digest('hex')!=='02d356fab610efac7469f5650c89441b4e3ddca4cb162e0fb78d1bf8bd783f2c')throw Error('Unrelated strategy source is not the deployed source')
   return {code,map:null}
  }
  for(const path of ['src/api.ts','src/components/RoomHumanBehaviorProfile.vue'])if(normalized.endsWith('/'+path))return {code:readFileSync(resolve('../artifacts/commerce-isolation/web-console/'+path),'utf8'),map:null}
 }}],build:{outDir:'../artifacts/commerce-dist'}
}))

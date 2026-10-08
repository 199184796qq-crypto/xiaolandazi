import test from 'node:test'
import assert from 'node:assert/strict'
import {readFile} from 'node:fs/promises'
import ts from 'typescript'
const source=await readFile(new URL('../src/uiFeedback.ts',import.meta.url),'utf8')
const js=ts.transpile(source.replace(/^import .* from 'vue'\r?\n/m,'').replace(/export /g,''),{target:ts.ScriptTarget.ES2022})
const feedback=new Function('reactive','customRef','window',js+'\nreturn {feedbackState,showAlert,dismissAlert,showRequestError,clearRequestError,useFeedbackErrorRef}')(
 v=>v,f=>{const r=f(()=>{},()=>{});return {get value(){return r.get()},set value(v){r.set(v)}}},
 {setTimeout() {}},
)
test('background reads use a deduplicated notice; writes keep their actual error dialog',()=>{
 feedback.showRequestError('/read','GET','read failed')
 assert.equal(feedback.feedbackState.alertOpen,false)
 assert.equal(feedback.feedbackState.toasts.length,1)
 feedback.showRequestError('/read','GET','read failed')
 assert.equal(feedback.feedbackState.alertOpen,false)
 const pollError=feedback.useFeedbackErrorRef();pollError.value='read failed'
 assert.equal(feedback.feedbackState.alertOpen,false,'local catch must not defeat transport poll dedupe')
 feedback.clearRequestError('/read','GET')
 feedback.showRequestError('/read','GET','read failed')
 assert.equal(feedback.feedbackState.alertOpen,false)
 assert.equal(feedback.feedbackState.toasts.length,1)
 for(let i=0;i<2;i++){
  feedback.showRequestError('/save','POST','save failed')
  assert.equal(feedback.feedbackState.alertOpen,true)
  feedback.dismissAlert()
 }
})
test('different errors queue without overwriting or duplicating the first dialog',()=>{
 feedback.showAlert('first','Save failed');feedback.showAlert('second','Publish failed');feedback.showAlert('second','Another catch')
 assert.equal(feedback.feedbackState.alertMessage,'first')
 feedback.dismissAlert();assert.equal(feedback.feedbackState.alertMessage,'second')
 feedback.dismissAlert();assert.equal(feedback.feedbackState.alertOpen,false)
 const error=feedback.useFeedbackErrorRef();error.value='business validation failed'
 assert.equal(feedback.feedbackState.alertOpen,true)
 feedback.dismissAlert();error.value='business validation failed'
 assert.equal(feedback.feedbackState.alertOpen,false)
})

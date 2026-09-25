const res = await fetch('http://127.0.0.1:5176/management/internal/v1/dev/runtime/answer-tts',{
  method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({
    question:'这个鸡怎么吃？',target_seconds:10,
    agent_provider:'qwen',agent_model:'qwen3.8-flash',
    tts_provider:'qwen_audio',tts_model:'qwen-audio-3.0-tts-plus',
    voice_id:'qwen-audio-3.0-tts-plus-yangduck-d4988b957aff42a89c44ec3ddb34d052'
  })
});
const out = await res.json();
if(!res.ok) throw new Error(JSON.stringify(out));
const audio = await fetch(out.audio_url);
const buf = new Uint8Array(await audio.arrayBuffer());
console.log('http='+audio.status+' content-type='+audio.headers.get('content-type')+' bytes='+buf.length);
console.log(Buffer.from(buf.slice(0,160)).toString('hex'));
let off=12;
for(let i=0;i<12 && off+8<=buf.length;i++){
  const id=Buffer.from(buf.slice(off,off+4)).toString('ascii');
  const size=buf[off+4]|(buf[off+5]<<8)|(buf[off+6]<<16)|(buf[off+7]<<24);
  console.log(i,'off='+off,'id='+JSON.stringify(id),'sizeUnsigned='+(size>>>0));
  const next=off+8+(size>>>0)+((size>>>0)&1);
  if(next<=off || next>buf.length){console.log('next invalid',next);break;}
  off=next;
}
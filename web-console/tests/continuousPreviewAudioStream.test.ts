import test from 'node:test'
import assert from 'node:assert/strict'
import { ContinuousPreviewAudioStream } from '../src/continuousPreviewAudioStream.ts'

test('generated clips are scheduled back-to-back into one audio output timeline', async () => {
  const starts: number[] = [], sources: { onended: (()=>void)|null }[] = []
  const context = {
    state: 'running', currentTime: 10, destination: {},
    resume: async()=>{}, suspend: async()=>{}, close: async()=>{},
    decodeAudioData: async()=>({duration:2}),
    createBufferSource: () => {
      const source = { buffer:null, onended:null as (()=>void)|null, connect:()=>{}, disconnect:()=>{}, stop:()=>{}, start:(at:number)=>starts.push(at) }
      sources.push(source); return source
    },
  } as unknown as AudioContext
  const ended: string[] = []
  const fetcher = async()=>({ok:true,arrayBuffer:async()=>new ArrayBuffer(8)}) as Response
  const stream = new ContinuousPreviewAudioStream(id=>ended.push(id),()=>context,fetcher)
  await stream.resume()
  await stream.append({id:'one',url:'/one.wav',durationMS:2000})
  await stream.append({id:'two',url:'/two.wav',durationMS:2000})
  assert.deepEqual(starts,[10.12,12.12])
  assert.equal(starts[1]! - starts[0]!,2)
  sources[0]!.onended?.()
  assert.deepEqual(ended,['one'])
  stream.stop()
})

test('playback cuts an oversized final WAV at exactly 120 seconds and schedules no later audio', async () => {
  const starts: { at:number; duration:number }[] = [], sources: { onended:(()=>void)|null }[] = []
  const context = {
    state:'running', currentTime:0, destination:{},
    resume:async()=>{}, suspend:async()=>{}, close:async()=>{},
    decodeAudioData:async()=>({duration:75}),
    createBufferSource:()=> {
      const source = { buffer:null, onended:null as (()=>void)|null, connect:()=>{}, disconnect:()=>{}, stop:()=>{}, start:(at:number,_offset:number,duration:number)=>starts.push({at,duration}) }
      sources.push(source); return source
    },
  } as unknown as AudioContext
  const ended: { id:string; ms:number }[] = [], fetched:string[] = []
  const fetcher = async (url:RequestInfo|URL)=> { fetched.push(String(url)); return {ok:true,arrayBuffer:async()=>new ArrayBuffer(8)} as Response }
  const stream = new ContinuousPreviewAudioStream((id,ms)=>ended.push({id,ms}),()=>context,fetcher)
  await stream.resume(); stream.setPlaybackLimit(120000)
  await stream.append({id:'one',url:'/one.wav',durationMS:75000})
  await stream.pause(); await stream.resume()
  await stream.append({id:'two',url:'/two.wav',durationMS:75000})
  await stream.append({id:'three',url:'/three.wav',durationMS:75000})
  assert.deepEqual(starts,[{at:.12,duration:75},{at:75.12,duration:45}])
  assert.deepEqual(fetched,['/one.wav','/two.wav'])
  sources.forEach(s=>s.onended?.())
  assert.equal(ended.reduce((sum,s)=>sum+s.ms,0),120000)
  stream.stop()
})

test('audio HTTP failures include the server conflict reason', async () => {
  const context = { state:'running' } as unknown as AudioContext
  const stream = new ContinuousPreviewAudioStream(()=>{},()=>context,async()=>new Response(JSON.stringify({error:'声音正文与已采纳稿不一致，不输出声音'}),{status:409}))
  await stream.resume()
  await assert.rejects(stream.append({id:'bad',url:'/content',durationMS:1000}),/HTTP 409：声音正文与已采纳稿不一致/)
})

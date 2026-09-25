const url = 'http://127.0.0.1:5176/management/internal/v1/dev/runtime/answer-tts';
const body = {
  question: '这个鸡怎么吃？',
  target_seconds: 10,
  agent_provider: 'qwen',
  agent_model: 'qwen3.8-flash',
  tts_provider: 'qwen_audio',
  tts_model: 'qwen-audio-3.0-tts-plus',
  voice_id: 'qwen-audio-3.0-tts-plus-yangduck-d4988b957aff42a89c44ec3ddb34d052',
  director_progress: 'HOLD_FOR_QNA',
  director_atmosphere: 'WARM_UP',
  humanization_kind: 'NONE',
  resume_mode_hint: 'BRIDGE',
  current_mainline: '跑山鸡主线测试',
  next_mainline_units: [],
  top_topics: ['HOW_TO_EAT'],
  prompt_directives: ['自然回答，回答后用一句自然桥接带回主线。']
};
const started = Date.now();
const res = await fetch(url,{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(body)});
const text = await res.text();
console.log('status=' + res.status + ' elapsed_ms=' + (Date.now()-started));
console.log(text.slice(0,5000));
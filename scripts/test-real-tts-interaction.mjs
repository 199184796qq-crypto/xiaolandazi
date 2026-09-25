import assert from 'node:assert/strict';

const proxy = 'http://127.0.0.1:5176';
const audio = 'http://127.0.0.1:8082';
const room = Date.now();
const receiver = 'real-tts-e2e-' + room;
const delay = ms => new Promise(resolve => setTimeout(resolve, ms));

function subscribe() {
  const controller = new AbortController();
  const backlog = [];
  const waiters = [];
  let connectedResolve;
  let connectedReject;
  const connected = new Promise((resolve, reject) => {
    connectedResolve = resolve;
    connectedReject = reject;
  });

  function emit(task) {
    const waiter = waiters.shift();
    if (waiter) waiter.resolve(task);
    else backlog.push(task);
  }

  (async () => {
    try {
      const response = await fetch(
        audio + '/v1/rooms/' + room + '/stream?receiver_id=' + encodeURIComponent(receiver),
        { signal: controller.signal },
      );
      assert.equal(response.status, 200);
      const reader = response.body.getReader();
      const decoder = new TextDecoder();
      let buffer = '';
      while (true) {
        const {done, value} = await reader.read();
        if (done) break;
        buffer += decoder.decode(value, {stream:true}).replace(/\r\n/g, '\n');
        let split;
        while ((split = buffer.indexOf('\n\n')) >= 0) {
          const block = buffer.slice(0, split);
          buffer = buffer.slice(split + 2);
          let type = '';
          let data = '';
          for (const line of block.split('\n')) {
            if (line.startsWith('event:')) type = line.slice(6).trim();
            if (line.startsWith('data:')) data += line.slice(5).trim();
          }
          if (type === 'connected') connectedResolve(JSON.parse(data));
          if (type === 'task') emit(JSON.parse(data));
        }
      }
    } catch (error) {
      if (!controller.signal.aborted) connectedReject(error);
    }
  })();

  function nextTask(timeoutMS = 30000) {
    if (backlog.length) return Promise.resolve(backlog.shift());
    return new Promise((resolve, reject) => {
      const waiter = {resolve, reject};
      waiters.push(waiter);
      const timer = setTimeout(() => {
        const i = waiters.indexOf(waiter);
        if (i >= 0) waiters.splice(i,1);
        reject(new Error('task timeout'));
      }, timeoutMS);
      waiter.resolve = value => {
        clearTimeout(timer);
        resolve(value);
      };
    });
  }

  return {connected, nextTask, close:()=>controller.abort()};
}

async function postJSON(path, body) {
  const res = await fetch(proxy + path, {
    method:'POST',
    headers:{'Content-Type':'application/json'},
    body:JSON.stringify(body),
  });
  const text = await res.text();
  let json;
  try { json = JSON.parse(text); } catch { json = {raw:text}; }
  if (!res.ok) throw new Error(path + ' HTTP ' + res.status + ': ' + text.slice(0,1200));
  return json;
}

async function getJSON(path) {
  const res = await fetch(proxy + path, {cache:'no-store'});
  const text = await res.text();
  if (!res.ok) throw new Error(path + ' HTTP ' + res.status + ': ' + text.slice(0,1200));
  return JSON.parse(text);
}

async function playback(task, status, progressMS=0) {
  const res = await fetch(audio + '/v1/tasks/' + encodeURIComponent(task.speech_task_id) + '/events', {
    method:'POST',
    headers:{'Content-Type':'application/json'},
    body:JSON.stringify({
      speech_task_id:task.speech_task_id,
      room_id:room,
      session_id:task.session_id,
      receiver_id:receiver,
      status,
      progress_ms:progressMS,
      occurred_at:new Date().toISOString(),
    }),
  });
  const text = await res.text();
  if (!res.ok) throw new Error('playback '+status+' HTTP '+res.status+': '+text);
  return JSON.parse(text);
}

const sub = subscribe();
try {
  await sub.connected;

  const started = await postJSON('/core/internal/v1/dev/audio/program/start', {room_id:room});
  assert.equal(started.running, true);
  const mainline = await sub.nextTask(5000);
  assert.equal(mainline.kind, 'test_wav_program');
  await playback(mainline, 'READY', mainline.start_ms || 0);
  await playback(mainline, 'PLAYING', mainline.start_ms || 0);

  const generated = await postJSON('/management/internal/v1/dev/runtime/answer-tts', {
    question:'这个鸡怎么吃？',
    target_seconds:10,
    agent_provider:'qwen',
    agent_model:'qwen3.8-flash',
    tts_provider:'qwen_audio',
    tts_model:'qwen-audio-3.0-tts-plus',
    voice_id:'qwen-audio-3.0-tts-plus-yangduck-d4988b957aff42a89c44ec3ddb34d052',
    director_progress:'HOLD_FOR_QNA',
    director_atmosphere:'WARM_UP',
    humanization_kind:'NONE',
    resume_mode_hint:'BRIDGE',
    current_mainline:'跑山鸡主线测试',
    next_mainline_units:[],
    top_topics:['HOW_TO_EAT'],
    prompt_directives:['自然回答，回答后用一句自然桥接带回主线。'],
  });
  assert.ok(generated.audio_url);
  assert.ok(generated.reply_core);
  assert.equal(generated.tts_model, 'qwen-audio-3.0-tts-plus');

  const inserted = await postJSON('/core/internal/v1/dev/audio/interaction', {
    room_id:room,
    session_id:mainline.session_id,
    label:'真实TTS端到端',
    audio_url:generated.audio_url,
    topic:generated.covered_topics?.[0] || 'HOW_TO_EAT',
    resume_mode:generated.resume_mode,
    resume_unit:generated.resume_unit,
    skip_units:generated.skip_units || [],
    text_digest:generated.text_digest,
    bridge_digest:generated.bridge_digest,
  });
  assert.ok(inserted.task);
  assert.equal(inserted.task.kind, 'interaction_tts');

  const interaction = await sub.nextTask(8000);
  assert.equal(interaction.speech_task_id, inserted.task.speech_task_id);
  assert.equal(interaction.kind, 'interaction_tts');
  assert.ok(interaction.duration_ms > 1000);

  const during = await (await fetch(audio + '/v1/rooms/' + room + '/sync', {cache:'no-store'})).json();
  assert.equal(during.suspended, true);
  assert.equal(during.task.kind, 'interaction_tts');

  await playback(interaction, 'READY', 0);
  await playback(interaction, 'PLAYING', 0);

  await delay(interaction.duration_ms + 250);
  await playback(interaction, 'COMPLETED', interaction.duration_ms);

  const resumed = await sub.nextTask(5000);
  assert.equal(resumed.kind, 'test_wav_program');
  assert.ok(resumed.start_ms >= 0);

  await delay(300);
  const after = await (await fetch(audio + '/v1/rooms/' + room + '/sync', {cache:'no-store'})).json();
  assert.equal(after.running, true);
  assert.ok(!after.suspended);
  assert.equal(after.task.kind, 'test_wav_program');

  const brain = await getJSON('/core/internal/v1/rooms/' + room + '/brain');
  const kinds = (brain.Timeline?.HotPins || []).map(pin => pin.Kind);
  assert.ok(kinds.includes('ANSWER'), 'missing ANSWER pin: ' + JSON.stringify(kinds));
  assert.ok(kinds.includes('RESUME'), 'missing RESUME pin: ' + JSON.stringify(kinds));

  console.log(JSON.stringify({
    pass:true,
    room,
    target_seconds:generated.target_seconds,
    estimated_seconds:generated.estimated_seconds,
    actual_seconds:Number((interaction.duration_ms/1000).toFixed(2)),
    resume_mode:generated.resume_mode,
    interaction_slot:interaction.slot,
    resumed_slot:resumed.slot,
    resumed_start_ms:resumed.start_ms,
    pins:kinds,
    agent_latency_ms:generated.agent_latency_ms,
    tts_latency_ms:generated.tts_latency_ms,
  }));
} finally {
  try { await postJSON('/core/internal/v1/dev/audio/program/stop', {room_id:room}); } catch {}
  sub.close();
}

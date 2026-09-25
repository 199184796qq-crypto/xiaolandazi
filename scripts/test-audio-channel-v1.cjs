const fs = require('node:fs');
const assert = require('node:assert/strict');
const crypto = require('node:crypto');

function loadEnv(path) {
  const out = {};
  if (!fs.existsSync(path)) return out;
  for (const line of fs.readFileSync(path, 'utf8').split(/\r?\n/)) {
    const trimmed = line.trim();
    if (!trimmed || trimmed.startsWith('#')) continue;
    const i = line.indexOf('=');
    if (i <= 0) continue;
    out[line.slice(0, i).trim()] = line.slice(i + 1);
  }
  return out;
}

const env = { ...loadEnv('configs/cloud-dev.local'), ...process.env };
const coreToken = env.CORE_INTERNAL_TOKEN || 'local-core-dev-token';
const coreURL = 'http://127.0.0.1:8081';
const audioURL = 'http://127.0.0.1:8082';
const expectedTestAudioPath = 'E:\\直播伴播\\测试素材\\主要测试声音\\跑山鸡.wav';
const roomID = Date.now();
const delay = ms => new Promise(r => setTimeout(r, ms));

function subscribe(receiverID) {
  const controller = new AbortController();
  let connectedResolve, connectedReject, taskResolve, taskReject;
  const connected = new Promise((res, rej) => { connectedResolve = res; connectedReject = rej; });
  const task = new Promise((res, rej) => { taskResolve = res; taskReject = rej; });
  let receivedTask = false;

  (async () => {
    try {
      const response = await fetch(audioURL + '/v1/rooms/' + roomID + '/stream?receiver_id=' + encodeURIComponent(receiverID), { signal: controller.signal });
      assert.equal(response.status, 200);
      const reader = response.body.getReader();
      const decoder = new TextDecoder();
      let buffer = '';
      while (true) {
        const { done, value } = await reader.read();
        if (done) break;
        buffer += decoder.decode(value, { stream: true }).replace(/\r\n/g, '\n');
        let split;
        while ((split = buffer.indexOf('\n\n')) >= 0) {
          const block = buffer.slice(0, split);
          buffer = buffer.slice(split + 2);
          let event = 'message';
          let data = '';
          for (const line of block.split('\n')) {
            if (line.startsWith('event:')) event = line.slice(6).trim();
            if (line.startsWith('data:')) data += line.slice(5).trim();
          }
          if (event === 'connected') connectedResolve(JSON.parse(data));
          if (event === 'task') {
            receivedTask = true;
            taskResolve(JSON.parse(data));
          }
        }
      }
    } catch (err) {
      if (controller.signal.aborted) return;
      connectedReject(err);
      taskReject(err);
    }
  })();

  return { receiverID, connected, task, close: () => controller.abort(), receivedTask: () => receivedTask };
}

async function report(task, receiverID, status, progressMS = 0) {
  const response = await fetch(audioURL + '/v1/tasks/' + encodeURIComponent(task.speech_task_id) + '/events', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      receiver_id: receiverID,
      status,
      progress_ms: progressMS,
      occurred_at: new Date().toISOString(),
    }),
  });
  if (response.status !== 200) throw new Error('report HTTP ' + response.status + ': ' + await response.text());
  return response.json();
}

async function coreState(taskID) {
  const response = await fetch(coreURL + '/internal/v1/dev/audio/tasks/' + encodeURIComponent(taskID), {
    headers: { 'X-Core-Token': coreToken },
  });
  if (response.status !== 200) throw new Error('core state HTTP ' + response.status + ': ' + await response.text());
  return response.json();
}

(async () => {
  const a = subscribe('probe-pc-a');
  await a.connected;

  const create = await fetch(coreURL + '/internal/v1/dev/audio/test', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', 'X-Core-Token': coreToken },
    body: JSON.stringify({ room_id: roomID, session_id: 'phase1-live-network', label: 'phase1-network-test', duration_ms: 1200 }),
  });
  if (create.status !== 201) throw new Error('create HTTP ' + create.status + ': ' + await create.text());
  const created = await create.json();
  const task = created.task;
  const taskA = await a.task;
  assert.equal(taskA.speech_task_id, task.speech_task_id);

  const audio = await fetch(task.audio_url);
  assert.equal(audio.status, 200);
  const bytes = Buffer.from(await audio.arrayBuffer());
  assert.ok(bytes.length > 44);
  assert.equal(bytes.subarray(0, 4).toString(), 'RIFF');
  assert.equal(bytes.subarray(8, 12).toString(), 'WAVE');
  const expectedAudio = fs.readFileSync(expectedTestAudioPath);
  assert.equal(bytes.length, expectedAudio.length, 'served WAV size differs from configured 跑山鸡.wav');
  assert.equal(
    crypto.createHash('sha256').update(bytes).digest('hex'),
    crypto.createHash('sha256').update(expectedAudio).digest('hex'),
    'served WAV content differs from configured 跑山鸡.wav',
  );
  assert.equal(task.kind, 'test_wav');

  const readyA = await report(task, a.receiverID, 'READY');
  assert.equal(readyA.room_reference, true);
  await report(task, a.receiverID, 'PLAYING', 1);
  await report(task, a.receiverID, 'PROGRESS', 650);
  await delay(450);

  const b = subscribe('probe-pc-b-late');
  await b.connected;
  const taskB = await b.task;
  assert.equal(taskB.speech_task_id, task.speech_task_id);
  assert.equal(taskA.audio_url, taskB.audio_url);
  assert.ok(taskB.start_ms >= 900, 'late receiver did not receive current room progress');
  assert.ok(taskB.start_ms < task.duration_ms, 'late receiver resume point is outside active audio');

  const readyB = await report(task, b.receiverID, 'READY', taskB.start_ms);
  assert.equal(readyB.room_reference, false);
  await report(task, b.receiverID, 'PLAYING', taskB.start_ms);
  await report(task, b.receiverID, 'PROGRESS', Math.min(task.duration_ms - 1, taskB.start_ms + 200));
  await report(task, a.receiverID, 'COMPLETED', task.duration_ms);
  await report(task, b.receiverID, 'COMPLETED', task.duration_ms);

  let state;
  for (let i = 0; i < 20; i++) {
    state = await coreState(task.speech_task_id);
    if (state.status === 'COMPLETED') break;
    await delay(100);
  }
  assert.equal(state.status, 'COMPLETED');
  assert.equal(state.event.receiver_id, a.receiverID);

  const snapshotResponse = await fetch(audioURL + '/v1/tasks/' + encodeURIComponent(task.speech_task_id));
  assert.equal(snapshotResponse.status, 200);
  const snapshot = await snapshotResponse.json();
  assert.equal(snapshot.reference_receiver, a.receiverID);
  assert.equal(snapshot.terminal, true);
  assert.deepEqual(Object.keys(snapshot.receiver_events).sort(), [a.receiverID, b.receiverID].sort());

  a.close();
  b.close();
  await delay(100);

  const c = subscribe('probe-pc-reconnect');
  await c.connected;
  await delay(650);
  assert.equal(c.receivedTask(), false, 'completed task replayed to reconnecting receiver');
  c.close();
  await delay(150);

  console.log('PASS live audio channel v1: late receiver resumes current room progress; one WAV, one reference completion, terminal reconnect does not replay');
  console.log('TASK ' + task.speech_task_id + ' audio_bytes=' + bytes.length + ' core_status=' + state.status);
})().catch(err => {
  console.error(err.stack || err);
  process.exit(1);
});

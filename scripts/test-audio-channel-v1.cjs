const assert = require('node:assert/strict');

const coreURL = 'http://127.0.0.1:8081';
const coreToken = process.env.CORE_INTERNAL_TOKEN || 'local-core-dev-token';
const roomID = Date.now();
const delay = ms => new Promise(resolve => setTimeout(resolve, ms));

async function register(receiverID) {
  const response = await fetch(coreURL + '/v1/receivers/register', {
    method: 'POST',
    headers: {'Content-Type':'application/json'},
    body: JSON.stringify({
      receiver_id: receiverID,
      room_id: roomID,
      terminal_type: 'test',
      name: receiverID,
      capabilities: ['audio/wav', 'mainline', 'interaction_tts'],
    }),
  });
  if (!response.ok) throw new Error('register HTTP ' + response.status + ': ' + await response.text());
}

async function unregister(receiverID) {
  await fetch(coreURL + '/v1/receivers/' + encodeURIComponent(receiverID) + '/unregister', {
    method: 'POST',
    headers: {'Content-Type':'application/json'},
    body: JSON.stringify({room_id: roomID}),
  });
}

function subscribe(receiverID) {
  const controller = new AbortController();
  let connectedResolve, connectedReject, taskResolve, taskReject;
  const connected = new Promise((resolve, reject) => {
    connectedResolve = resolve;
    connectedReject = reject;
  });
  const task = new Promise((resolve, reject) => {
    taskResolve = resolve;
    taskReject = reject;
  });
  let receivedTask = false;

  (async () => {
    try {
      const response = await fetch(
        coreURL + '/v1/rooms/' + roomID + '/stream?receiver_id=' + encodeURIComponent(receiverID),
        {signal: controller.signal},
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
    } catch (error) {
      if (controller.signal.aborted) return;
      connectedReject(error);
      taskReject(error);
    }
  })();

  return {
    connected,
    task,
    close: () => controller.abort(),
    receivedTask: () => receivedTask,
  };
}

async function report(task, receiverID, status, progressMS = 0) {
  const response = await fetch(coreURL + '/v1/tasks/' + encodeURIComponent(task.speech_task_id) + '/events', {
    method: 'POST',
    headers: {'Content-Type':'application/json'},
    body: JSON.stringify({
      receiver_id: receiverID,
      status,
      progress_ms: progressMS,
      occurred_at: new Date().toISOString(),
    }),
  });
  assert.equal(response.status, 202);
  return response.json();
}

async function snapshot(taskID) {
  const response = await fetch(coreURL + '/v1/tasks/' + encodeURIComponent(taskID), {cache:'no-store'});
  if (!response.ok) throw new Error('snapshot HTTP ' + response.status + ': ' + await response.text());
  return response.json();
}

(async () => {
  const receiverA = 'core-probe-a-' + roomID;
  const receiverB = 'core-probe-b-' + roomID;
  const receiverC = 'core-probe-c-' + roomID;

  await register(receiverA);
  const a = subscribe(receiverA);
  await a.connected;

  const create = await fetch(coreURL + '/internal/v1/dev/audio/test', {
    method: 'POST',
    headers: {'Content-Type':'application/json', 'X-Core-Token':coreToken},
    body: JSON.stringify({
      room_id: roomID,
      session_id: 'core-audio-clock-test',
      label: 'core-audio-channel-v1',
      duration_ms: 1200,
    }),
  });
  if (create.status !== 201) throw new Error('create HTTP ' + create.status + ': ' + await create.text());
  const created = await create.json();
  const task = created.task;
  const taskA = await a.task;
  assert.equal(taskA.speech_task_id, task.speech_task_id);
  assert.equal(taskA.start_ms || 0, 0);

  const audio = await fetch(task.audio_url);
  assert.equal(audio.status, 200);
  const bytes = Buffer.from(await audio.arrayBuffer());
  assert.ok(bytes.length > 44);
  assert.equal(bytes.subarray(0, 4).toString(), 'RIFF');
  assert.equal(bytes.subarray(8, 12).toString(), 'WAVE');

  await report(task, receiverA, 'READY');
  await report(task, receiverA, 'PLAYING', 1);
  await delay(380);

  await register(receiverB);
  const b = subscribe(receiverB);
  await b.connected;
  const taskB = await b.task;
  assert.equal(taskB.speech_task_id, task.speech_task_id);
  assert.ok(taskB.start_ms >= 250, 'late receiver did not receive server-clock progress');
  assert.ok(taskB.start_ms < task.duration_ms, 'late receiver resume point is outside active audio');

  await report(task, receiverB, 'READY', taskB.start_ms);
  await report(task, receiverA, 'COMPLETED', task.duration_ms);
  await report(task, receiverB, 'COMPLETED', task.duration_ms);

  const beforeClock = await snapshot(task.speech_task_id);
  assert.equal(beforeClock.terminal, false, 'receiver COMPLETED must not end Core task lifecycle');

  let done;
  for (let i = 0; i < 20; i++) {
    done = await snapshot(task.speech_task_id);
    if (done.terminal) break;
    await delay(100);
  }
  assert.equal(done.terminal, true, 'Core server clock did not finish task');
  assert.deepEqual(Object.keys(done.receiver_events).sort(), [receiverA, receiverB].sort());

  a.close();
  b.close();
  await unregister(receiverA);
  await unregister(receiverB);

  await register(receiverC);
  const c = subscribe(receiverC);
  await c.connected;
  await delay(350);
  assert.equal(c.receivedTask(), false, 'expired task replayed to reconnecting receiver');
  c.close();
  await unregister(receiverC);

  console.log(
    'PASS Core audio channel: broadcast OK; late join start_ms=' + taskB.start_ms +
    'ms; device COMPLETED is health-only; Core clock owns completion; expired reconnect gets no replay',
  );
})().catch(error => {
  console.error(error.stack || error);
  process.exit(1);
});

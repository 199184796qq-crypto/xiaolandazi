import { get, writable } from 'svelte/store';

const AUDIO_CONTEXT_KEY = '__xiaolan_customer_audio_context_v1';
const RECEIVER_KEY = 'xiaolan-customer-audio-receiver-v1';
const AUDIO_MUTE_KEY = 'xiaolan-customer-audio-muted-v1';

type AudioHolder = Window & { [AUDIO_CONTEXT_KEY]?: AudioContext };
export type FloatingAudioState = {
  roomId: number;
  roomName: string;
  state: 'working' | 'paused' | 'stopped';
  visible: boolean;
  expanded: boolean;
  connected: boolean;
};

export const floatingAudioState = writable<FloatingAudioState>({
  roomId: 0,
  roomName: '',
  state: 'stopped',
  visible: false,
  expanded: false,
  connected: false,
});
const initialMuted =
  typeof window !== 'undefined' && window.sessionStorage.getItem(AUDIO_MUTE_KEY) === '1';
export const customerAudioMuted = writable(initialMuted);

let controller: AbortController | null = null;
let generation = 0;
let nextStartTime = 0;
let heartbeatTimer: number | undefined;
let registeredRoomId = 0;
let muted = initialMuted;
let outputGain: GainNode | null = null;
let outputGainContext: AudioContext | null = null;
const sources = new Set<AudioBufferSourceNode>();

function holder() {
  return window as AudioHolder;
}

export function getCustomerAudioContext(create = false) {
  const AudioContextCtor = window.AudioContext;
  if (!AudioContextCtor) return null;
  let context = holder()[AUDIO_CONTEXT_KEY];
  if (context?.state === 'closed') {
    context = undefined;
    holder()[AUDIO_CONTEXT_KEY] = undefined;
  }
  if (!context && create) {
    context = new AudioContextCtor();
    holder()[AUDIO_CONTEXT_KEY] = context;
  }
  return context ?? null;
}

export async function unlockCustomerAudio() {
  const context = getCustomerAudioContext(true);
  if (!context) throw new Error('当前浏览器不支持声音播放');
  if (context.state === 'suspended') await context.resume();
  if (context.state !== 'running') throw new Error('浏览器还没有允许声音播放');
  return context;
}

function customerAudioOutput(context: AudioContext) {
  if (!outputGain || outputGainContext !== context) {
    try { outputGain?.disconnect(); } catch {}
    outputGain = context.createGain();
    outputGainContext = context;
    outputGain.gain.value = muted ? 0 : 1;
    outputGain.connect(context.destination);
  }
  return outputGain;
}

export function setCustomerAudioMuted(value: boolean) {
  muted = value;
  customerAudioMuted.set(value);
  if (typeof window !== 'undefined') {
    window.sessionStorage.setItem(AUDIO_MUTE_KEY, value ? '1' : '0');
  }
  const context = typeof window !== 'undefined' ? getCustomerAudioContext(false) : null;
  if (context && outputGain && outputGainContext === context) {
    outputGain.gain.setValueAtTime(value ? 0 : 1, context.currentTime);
  }
  return value;
}

export function toggleCustomerAudioMuted() {
  return setCustomerAudioMuted(!muted);
}

function receiverId() {
  const stored = window.sessionStorage.getItem(RECEIVER_KEY);
  if (stored) return stored;
  const suffix =
    typeof crypto !== 'undefined' && 'randomUUID' in crypto
      ? crypto.randomUUID()
      : Math.random().toString(36).slice(2) + Date.now().toString(36);
  const id = 'customer-mobile-' + suffix;
  window.sessionStorage.setItem(RECEIVER_KEY, id);
  return id;
}

function flushQueue() {
  nextStartTime = 0;
  for (const source of sources) {
    try { source.stop(); } catch {}
    try { source.disconnect(); } catch {}
  }
  sources.clear();
}

function bufferWindow() {
  const host = window.location.hostname.toLowerCase();
  const local = host === '127.0.0.1' || host === 'localhost' || host.endsWith('.localhost');
  return local
    ? { initial: 0.07, low: 0.035, refill: 0.06 }
    : { initial: 0.32, low: 0.012, refill: 0.03 };
}

function schedulePCM(frame: Uint8Array) {
  const context = getCustomerAudioContext(false);
  if (!context || context.state !== 'running' || frame.byteLength !== 960) return;
  const buffer = context.createBuffer(1, 480, 24_000);
  const channel = buffer.getChannelData(0);
  const view = new DataView(frame.buffer, frame.byteOffset, frame.byteLength);
  for (let index = 0; index < 480; index += 1) channel[index] = view.getInt16(index * 2, true) / 32768;
  const source = context.createBufferSource();
  source.buffer = buffer;
  source.connect(customerAudioOutput(context));
  const now = context.currentTime;
  const limits = bufferWindow();
  if (nextStartTime <= 0) nextStartTime = now + limits.initial;
  else if (nextStartTime < now + limits.low) nextStartTime = now + limits.refill;
  const startAt = nextStartTime;
  nextStartTime += 0.02;
  sources.add(source);
  source.onended = () => {
    sources.delete(source);
    try { source.disconnect(); } catch {}
  };
  source.start(startAt);
}

async function pump(stream: ReadableStream<Uint8Array>, ownGeneration: number, signal: AbortSignal) {
  const reader = stream.getReader();
  let pending = new Uint8Array(0);
  try {
    while (!signal.aborted && ownGeneration === generation) {
      const { done, value } = await reader.read();
      if (done) break;
      if (!value?.byteLength) continue;
      const merged = new Uint8Array(pending.byteLength + value.byteLength);
      merged.set(pending, 0);
      merged.set(value, pending.byteLength);
      let offset = 0;
      while (merged.byteLength - offset >= 960) {
        schedulePCM(merged.subarray(offset, offset + 960));
        offset += 960;
      }
      pending = offset < merged.byteLength ? merged.slice(offset) : new Uint8Array(0);
    }
  } finally {
    try { reader.releaseLock(); } catch {}
  }
}

async function registerReceiver(roomId: number) {
  const response = await fetch('/core-audio/v1/receivers/register', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      receiver_id: receiverId(),
      room_id: roomId,
      terminal_type: 'customer_mobile',
      name: '客户手机端',
      capabilities: ['composite_pcm_s16le_24k_mono', 'room_audio_engine'],
    }),
  });
  if (!response.ok) throw new Error('声音终端注册失败 HTTP ' + response.status);
  registeredRoomId = roomId;
  if (heartbeatTimer !== undefined) window.clearInterval(heartbeatTimer);
  heartbeatTimer = window.setInterval(() => {
    if (!registeredRoomId) return;
    void fetch('/core-audio/v1/receivers/' + encodeURIComponent(receiverId()) + '/heartbeat', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ room_id: registeredRoomId }),
    }).catch(() => undefined);
  }, 15_000);
}

export function updateFloatingAudioSession(
  roomId: number,
  roomName: string,
  state: 'working' | 'paused' | 'stopped',
  visible = true,
) {
  floatingAudioState.update((current) => ({
    ...current,
    roomId,
    roomName: roomName || current.roomName || ('直播间 #' + roomId),
    state,
    visible: visible || current.visible,
  }));
}

export function setFloatingAudioExpanded(expanded: boolean) {
  floatingAudioState.update((current) => ({ ...current, expanded }));
}

export function hideFloatingAudioCard() {
  floatingAudioState.update((current) => ({ ...current, visible: false, expanded: false }));
}

export async function closeLocalFloatingAudio() {
  hideFloatingAudioCard();
  await stopRoomCompositeAudio();
  floatingAudioState.set({
    roomId: 0,
    roomName: '',
    state: 'stopped',
    visible: false,
    expanded: false,
    connected: false,
  });
}

export async function closeLocalFloatingAudioForDifferentRoom(nextRoomId: number) {
  const current = get(floatingAudioState);
  if (!current.roomId || current.roomId === nextRoomId) return;
  await closeLocalFloatingAudio();
}

export async function startRoomCompositeAudio(roomId: number, roomName = '') {
  await unlockCustomerAudio();
  await registerReceiver(roomId);
  controller?.abort();
  flushQueue();
  const ownGeneration = ++generation;
  const ownController = new AbortController();
  controller = ownController;
  const response = await fetch('/core-audio/v1/rooms/' + roomId + '/composite.pcm', {
    cache: 'no-store',
    signal: ownController.signal,
  });
  if (!response.ok || !response.body) throw new Error('房间声音流连接失败 HTTP ' + response.status);
  void pump(response.body, ownGeneration, ownController.signal);
  floatingAudioState.update((current) => ({
    ...current,
    roomId,
    roomName: roomName || current.roomName || ('直播间 #' + roomId),
    state: 'working',
    visible: true,
    connected: true,
  }));
}

export function flushRoomCompositeAudio() {
  flushQueue();
  floatingAudioState.update((current) => ({ ...current, state: 'paused' }));
}

export async function stopRoomCompositeAudio(unregister = true) {
  generation += 1;
  controller?.abort();
  controller = null;
  flushQueue();
  if (heartbeatTimer !== undefined) {
    window.clearInterval(heartbeatTimer);
    heartbeatTimer = undefined;
  }
  const roomId = registeredRoomId;
  registeredRoomId = 0;
  floatingAudioState.update((current) => ({
    ...current,
    state: 'stopped',
    connected: false,
  }));
  if (!unregister || !roomId) return;
  try {
    await fetch('/core-audio/v1/receivers/' + encodeURIComponent(receiverId()) + '/unregister', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ room_id: roomId }),
    });
  } catch {}
}

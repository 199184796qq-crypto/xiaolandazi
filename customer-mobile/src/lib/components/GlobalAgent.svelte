<script lang="ts">
  import { onDestroy, onMount, tick } from 'svelte';
  import { chatClientAgent, getPublicSystemConfig, getRooms, transcribeRoomAgentVoice } from '$lib/api';

  type ChatMessage = { role: 'user' | 'agent'; text: string };

  const IDLE_HIDE_MS = 60_000;

  let agentName = '小蓝直播搭子';
  let agentMessages: ChatMessage[] = [];
  let agentInput = '';
  let agentBusy = false;
  let drawerOpen = false;
  let agentDocked = true;
  let idleTimer: number | undefined;
  let chatEl: HTMLDivElement;

  let orbPressed = false;
  let listening = false;
  let holdTimer: number | undefined;
  let recognition: any = null;
  let mediaRecorder: MediaRecorder | null = null;
  let mediaStream: MediaStream | null = null;
  let mediaChunks: BlobPart[] = [];
  let mediaMimeType = '';
  let voiceTranscribing = false;
  let speechFinal = '';
  let speechInterim = '';
  let voiceText = '';
  let voiceComposerVisible = false;
  let speechHint = '';
  let voiceRoomId = 0;
  let voiceComposerEl: HTMLFormElement | null = null;
  let agentOrbEl: HTMLButtonElement | null = null;

  onMount(() => {
    void Promise.all([
      getPublicSystemConfig()
        .then((config) => {
          agentName = config.client_agent_name?.trim() || agentName;
        })
        .catch(() => undefined),
      getRooms()
        .then((result) => {
          voiceRoomId = Number(result.items?.[0]?.id || 0);
        })
        .catch(() => undefined),
    ]);

    const handleOutsideVoiceComposer = (event: PointerEvent) => {
      if (!voiceComposerVisible) return;
      const target = event.target as Node | null;
      if (target && (voiceComposerEl?.contains(target) || agentOrbEl?.contains(target))) return;
      voiceComposerVisible = false;
    };
    window.addEventListener('pointerdown', handleOutsideVoiceComposer, true);
    return () => window.removeEventListener('pointerdown', handleOutsideVoiceComposer, true);
  });

  onDestroy(() => {
    if (idleTimer !== undefined) window.clearTimeout(idleTimer);
    if (holdTimer !== undefined) window.clearTimeout(holdTimer);
    try { recognition?.stop?.(); } catch {}
    try {
      if (mediaRecorder && mediaRecorder.state !== 'inactive') mediaRecorder.stop();
    } catch {}
    stopMediaStreamTracks();
  });

  function scheduleAutoDock() {
    if (idleTimer !== undefined) window.clearTimeout(idleTimer);
    idleTimer = window.setTimeout(() => {
      if (listening || agentBusy || voiceTranscribing) {
        scheduleAutoDock();
        return;
      }
      drawerOpen = false;
      voiceComposerVisible = false;
      agentDocked = true;
    }, IDLE_HIDE_MS);
  }

  function keepAgentAwake() {
    agentDocked = false;
    scheduleAutoDock();
  }

  function beginAgentHold(event?: PointerEvent) {
    keepAgentAwake();
    try {
      const target = event?.currentTarget as HTMLElement | null;
      if (target && event) target.setPointerCapture(event.pointerId);
    } catch {}
    orbPressed = true;
    speechHint = '';
    if (holdTimer !== undefined) window.clearTimeout(holdTimer);
    holdTimer = window.setTimeout(() => {
      holdTimer = undefined;
      void beginListening();
    }, 260);
  }

  function endAgentHold(event?: PointerEvent) {
    try {
      const target = event?.currentTarget as HTMLElement | null;
      if (target && event && target.hasPointerCapture(event.pointerId)) target.releasePointerCapture(event.pointerId);
    } catch {}
    if (holdTimer !== undefined) {
      window.clearTimeout(holdTimer);
      holdTimer = undefined;
      if (orbPressed) drawerOpen = true;
    } else if (listening) {
      stopListening();
    }
    orbPressed = false;
    scheduleAutoDock();
  }

  function stopMediaStreamTracks() {
    for (const track of mediaStream?.getTracks?.() || []) {
      try { track.stop(); } catch {}
    }
    mediaStream = null;
  }

  function muteMediaStreamTracks() {
    for (const track of mediaStream?.getAudioTracks?.() || []) {
      try { track.enabled = false; } catch {}
    }
  }

  async function reusableMediaStream() {
    const current = mediaStream;
    const liveTrack = current?.getAudioTracks?.().find((track) => track.readyState === 'live');
    if (current && liveTrack) {
      for (const track of current.getAudioTracks()) track.enabled = true;
      return current;
    }
    stopMediaStreamTracks();
    const stream = await navigator.mediaDevices.getUserMedia({
      audio: { echoCancellation: true, noiseSuppression: true, autoGainControl: true },
    });
    mediaStream = stream;
    return stream;
  }

  function preferredRecordingMimeType() {
    if (typeof MediaRecorder === 'undefined') return '';
    const candidates = [
      'audio/mp4;codecs=mp4a.40.2',
      'audio/mp4',
      'audio/webm;codecs=opus',
      'audio/webm',
      'audio/ogg;codecs=opus',
      'audio/ogg',
    ];
    return candidates.find((item) => {
      try { return MediaRecorder.isTypeSupported(item); } catch { return false; }
    }) || '';
  }

  function recordingFilename(mimeType: string) {
    const normalized = mimeType.toLowerCase();
    if (normalized.includes('mp4')) return 'voice.m4a';
    if (normalized.includes('ogg')) return 'voice.ogg';
    if (normalized.includes('wav')) return 'voice.wav';
    return 'voice.webm';
  }

  async function deliverRecognizedVoice(text: string) {
    const value = text.trim();
    voiceText = value;
    if (!value) {
      voiceComposerVisible = true;
      speechHint = speechHint || '没有听清，可以再长按一次或直接输入。';
      return;
    }
    if (agentBusy) {
      voiceComposerVisible = true;
      speechHint = '智能体正在处理上一条，识别结果已保留，可以稍后发送。';
      return;
    }
    voiceComposerVisible = false;
    voiceText = '';
    speechHint = '';
    await sendAgentMessage(value);
  }

  async function transcribeRecordedVoice(blob: Blob, mimeType: string) {
    if (blob.size < 256) {
      voiceComposerVisible = true;
      speechHint = '录音太短，没有听清，可以再长按一次。';
      return;
    }
    if (!voiceRoomId) {
      voiceComposerVisible = true;
      speechHint = '还没有直播间，暂时请直接输入文字。';
      return;
    }
    voiceTranscribing = true;
    voiceComposerVisible = true;
    speechHint = '正在把语音转换成文字…';
    try {
      const result = await transcribeRoomAgentVoice(voiceRoomId, blob, recordingFilename(mimeType));
      await deliverRecognizedVoice(result.text || '');
    } catch (error) {
      voiceComposerVisible = true;
      speechHint = error instanceof Error ? error.message : '语音转文字失败，请再说一次。';
    } finally {
      voiceTranscribing = false;
      scheduleAutoDock();
    }
  }

  async function beginMediaRecorderListening() {
    if (!navigator.mediaDevices?.getUserMedia || typeof MediaRecorder === 'undefined') return false;
    speechHint = '正在启用麦克风…';
    const stream = await reusableMediaStream();
    if (!orbPressed) {
      muteMediaStreamTracks();
      voiceComposerVisible = true;
      speechHint = '麦克风已就绪，请重新长按说话。';
      return true;
    }
    mediaChunks = [];
    const preferredMime = preferredRecordingMimeType();
    const recorder = preferredMime ? new MediaRecorder(stream, { mimeType: preferredMime }) : new MediaRecorder(stream);
    mediaRecorder = recorder;
    mediaMimeType = recorder.mimeType || preferredMime || 'audio/webm';
    recorder.ondataavailable = (event) => {
      if (event.data?.size) mediaChunks.push(event.data);
    };
    recorder.onerror = () => {
      listening = false;
      voiceComposerVisible = true;
      speechHint = '手机录音失败，请检查麦克风权限后再试。';
      stopMediaStreamTracks();
      mediaRecorder = null;
    };
    recorder.onstop = () => {
      const chunks = mediaChunks;
      const mimeType = mediaMimeType || recorder.mimeType || 'audio/webm';
      mediaChunks = [];
      listening = false;
      mediaRecorder = null;
      muteMediaStreamTracks();
      void transcribeRecordedVoice(new Blob(chunks, { type: mimeType }), mimeType);
    };
    recorder.start();
    listening = true;
    speechHint = '';
    return true;
  }

  function beginBrowserSpeechRecognition() {
    const Recognition = (window as any).SpeechRecognition || (window as any).webkitSpeechRecognition;
    if (!Recognition) {
      listening = false;
      voiceComposerVisible = true;
      speechHint = '当前手机浏览器不支持录音识别，请直接输入。';
      return;
    }
    try {
      recognition = new Recognition();
      recognition.lang = 'zh-CN';
      recognition.continuous = true;
      recognition.interimResults = true;
      recognition.onresult = (event: any) => {
        let interim = '';
        for (let i = event.resultIndex; i < event.results.length; i += 1) {
          const text = String(event.results[i]?.[0]?.transcript || '');
          if (event.results[i].isFinal) speechFinal += text;
          else interim += text;
        }
        speechInterim = interim;
        voiceText = (speechFinal + speechInterim).trim();
      };
      recognition.onerror = () => {
        speechHint = '没有听清，可以再长按一次或直接输入。';
      };
      recognition.onend = () => {
        listening = false;
        const text = (speechFinal + speechInterim).trim();
        recognition = null;
        void deliverRecognizedVoice(text);
      };
      listening = true;
      recognition.start();
    } catch {
      listening = false;
      voiceComposerVisible = true;
      speechHint = '语音识别启动失败，可以直接输入。';
    }
  }

  async function beginListening() {
    keepAgentAwake();
    speechFinal = '';
    speechInterim = '';
    voiceText = '';
    voiceComposerVisible = false;
    try {
      if (await beginMediaRecorderListening()) return;
    } catch (error: any) {
      listening = false;
      stopMediaStreamTracks();
      mediaRecorder = null;
      const name = String(error?.name || '');
      voiceComposerVisible = true;
      if (name === 'NotAllowedError' || name === 'SecurityError') {
        speechHint = '没有麦克风权限，请在手机浏览器设置里允许本站使用麦克风。';
        return;
      }
      if (name === 'NotFoundError') {
        speechHint = '没有检测到可用麦克风。';
        return;
      }
    }
    beginBrowserSpeechRecognition();
  }

  function stopListening() {
    if (mediaRecorder && mediaRecorder.state !== 'inactive') {
      speechHint = '正在把语音转换成文字…';
      voiceComposerVisible = true;
      try { mediaRecorder.stop(); } catch {}
      return;
    }
    try { recognition?.stop?.(); } catch {}
  }

  async function sendAgentMessage(value: string) {
    const text = value.trim();
    if (!text || agentBusy) return;
    keepAgentAwake();
    const history = [...agentMessages];
    agentMessages = [...agentMessages, { role: 'user', text }];
    drawerOpen = true;
    agentBusy = true;
    await tick();
    chatEl?.scrollTo({ top: chatEl.scrollHeight });
    try {
      const response = await chatClientAgent(text, history.slice(-12));
      agentMessages = [...agentMessages, { role: 'agent', text: response.reply }];
    } catch (error) {
      agentMessages = [...agentMessages, { role: 'agent', text: error instanceof Error ? error.message : '暂时无法回答' }];
    } finally {
      agentBusy = false;
      scheduleAutoDock();
      await tick();
      chatEl?.scrollTo({ top: chatEl.scrollHeight, behavior: 'smooth' });
    }
  }

  function sendDrawerText() {
    const value = agentInput.trim();
    if (!value || agentBusy) return;
    agentInput = '';
    void sendAgentMessage(value);
  }

  function sendVoiceText() {
    const value = voiceText.trim();
    if (!value || agentBusy) return;
    voiceComposerVisible = false;
    voiceText = '';
    void sendAgentMessage(value);
  }

  function closeDrawerFromMask(event: MouseEvent) {
    if (event.target !== event.currentTarget) return;
    drawerOpen = false;
    scheduleAutoDock();
  }
</script>

<button
  bind:this={agentOrbEl}
  class="global-agent-orb"
  class:docked={agentDocked}
  class:listening
  type="button"
  aria-label={listening ? '正在听你说话' : agentDocked ? '唤醒智能体' : '长按和智能体说话'}
  on:pointerdown|preventDefault={beginAgentHold}
  on:pointerup|preventDefault={endAgentHold}
  on:pointercancel|preventDefault={endAgentHold}
  on:contextmenu|preventDefault
  on:dragstart|preventDefault
  on:selectstart|preventDefault
>
  {#if listening}
    <span class="voice-ring one"></span><span class="voice-ring two"></span><span class="voice-ring three"></span>
    <span class="recording-glow"></span><span class="mic-icon">●</span><span class="mic-stem"></span>
  {:else}
    <span class="agent-star">✦</span>
  {/if}
</button>

{#if listening}<div class="listening-caption">松手识别并发送</div>{/if}

{#if voiceComposerVisible}
  <form bind:this={voiceComposerEl} class="voice-floating-composer" on:submit|preventDefault={sendVoiceText} on:pointerdown={keepAgentAwake}>
    <input bind:value={voiceText} placeholder={speechHint || '语音会在这里转成文字…'} aria-label="语音转文字内容" />
    <button type="submit" disabled={voiceTranscribing || !voiceText.trim() || agentBusy}>{voiceTranscribing ? '识别中…' : '发送'}</button>
  </form>
{/if}

{#if drawerOpen}
  <div class="agent-drawer-mask" role="presentation" on:click={closeDrawerFromMask} on:pointerdown={keepAgentAwake}>
    <section class="agent-drawer">
      <header>
        <div><span>LIVE COMPANION</span><h2>{agentName}</h2></div>
        <button type="button" aria-label="关闭对话" on:click={() => { drawerOpen = false; scheduleAutoDock(); }}>×</button>
      </header>
      <div class="drawer-chat" bind:this={chatEl}>
        {#if agentMessages.length === 0}<div class="drawer-empty">长按小球说话，或者直接输入。</div>{/if}
        {#each agentMessages as message}
          <article class:mine={message.role === 'user'}><small>{message.role === 'user' ? '我' : agentName}</small><p>{message.text}</p></article>
        {/each}
        {#if agentBusy}<article><small>{agentName}</small><p>正在处理…</p></article>{/if}
      </div>
      <form class="drawer-composer" on:submit|preventDefault={sendDrawerText}>
        <input bind:value={agentInput} on:input={keepAgentAwake} placeholder="继续跟智能体说…" />
        <button type="submit" disabled={!agentInput.trim() || agentBusy}>发送</button>
      </form>
    </section>
  </div>
{/if}

<style>
  .global-agent-orb{position:fixed;left:50%;bottom:calc(112px + env(safe-area-inset-bottom));z-index:57;display:grid;width:68px;height:68px;place-items:center;transform:translateX(-50%);border:2px solid rgba(255,255,255,.82);border-radius:50%;background:radial-gradient(circle at 35% 30%,#acbbff 0,#7587f4 34%,#4c5ed6 76%,#37449f 100%);color:#fff;box-shadow:0 14px 34px rgba(75,94,208,.36),inset 0 0 0 1px rgba(255,255,255,.45);animation:orbFloat 3.1s ease-in-out infinite;transition:left .34s cubic-bezier(.22,.82,.32,1),bottom .34s ease,width .22s ease,height .22s ease,box-shadow .22s ease;touch-action:none;-webkit-touch-callout:none;-webkit-user-select:none;user-select:none;-webkit-user-drag:none;-webkit-tap-highlight-color:transparent}
  .global-agent-orb.docked{left:min(100%,calc(50% + 270px));bottom:calc(102px + env(safe-area-inset-bottom));animation:dockedPulse 3.4s ease-in-out infinite;box-shadow:0 9px 26px rgba(75,94,208,.3),inset 0 0 0 1px rgba(255,255,255,.45)}
  .global-agent-orb *{pointer-events:none;-webkit-user-select:none;user-select:none}
  .agent-star{font-size:28px}.global-agent-orb.listening{left:50%;width:116px;height:116px;bottom:calc(110px + env(safe-area-inset-bottom));animation:recordingBreath 1.6s ease-in-out infinite}
  @keyframes orbFloat{0%,100%{transform:translateX(-50%) translateY(0) scale(1)}50%{transform:translateX(-50%) translateY(-6px) scale(1.04)}}
  @keyframes dockedPulse{0%,100%{transform:translateX(-50%) scale(.96)}50%{transform:translateX(-56%) scale(1)}}
  @keyframes recordingBreath{0%,100%{transform:translateX(-50%) scale(1)}50%{transform:translateX(-50%) scale(1.045)}}
  .recording-glow{position:absolute;inset:10px;border-radius:50%;background:radial-gradient(circle,rgba(255,255,255,.22),transparent 68%);animation:recordingGlow 1.15s ease-in-out infinite}@keyframes recordingGlow{0%,100%{opacity:.48;transform:scale(.9)}50%{opacity:1;transform:scale(1.08)}}
  .voice-ring{position:absolute;inset:-12px;border:3px solid rgba(117,137,255,.4);border-radius:50%;animation:voiceRing 1.65s ease-out infinite}.voice-ring.two{animation-delay:.52s}.voice-ring.three{animation-delay:1.04s}@keyframes voiceRing{0%{transform:scale(.78);opacity:.82}55%{opacity:.36}100%{transform:scale(1.72);opacity:0}}
  .mic-icon{width:25px;height:33px;border:4px solid #fff;border-radius:14px;font-size:0;z-index:2}.mic-stem{position:absolute;width:34px;height:17px;bottom:27px;border:4px solid #fff;border-top:0;border-radius:0 0 17px 17px;z-index:2}.mic-stem::after{content:'';position:absolute;left:50%;bottom:-12px;width:4px;height:12px;transform:translateX(-50%);background:#fff;border-radius:2px}
  .listening-caption{position:fixed;left:50%;bottom:calc(244px + env(safe-area-inset-bottom));z-index:59;transform:translateX(-50%);padding:8px 14px;border-radius:999px;background:rgba(30,41,73,.9);color:#fff;font-size:11px;font-weight:800;white-space:nowrap}
  .voice-floating-composer{position:fixed;left:50%;bottom:calc(185px + env(safe-area-inset-bottom));z-index:59;width:min(calc(100% - 30px),510px);transform:translateX(-50%);display:grid;grid-template-columns:minmax(0,1fr) auto;gap:8px;padding:8px;border:1px solid #d8e0ef;border-radius:18px;background:rgba(255,255,255,.96);backdrop-filter:blur(18px);box-shadow:0 15px 38px rgba(48,62,114,.16)}
  .voice-floating-composer input{min-width:0;border:0;outline:none;background:transparent;padding:0 8px;color:#33405a}.voice-floating-composer button,.drawer-composer button{border:0;border-radius:12px;background:#5669df;color:#fff;padding:0 16px;font-weight:900}.voice-floating-composer button:disabled,.drawer-composer button:disabled{opacity:.45}
  .agent-drawer-mask{position:fixed;inset:0;z-index:60;background:rgba(18,27,47,.3);backdrop-filter:blur(3px)}
  .agent-drawer{position:fixed;left:50%;top:max(58px,env(safe-area-inset-top));bottom:calc(100px + env(safe-area-inset-bottom));width:min(calc(100% - 20px),520px);transform:translateX(-50%);display:grid;grid-template-rows:auto 1fr auto;border:1px solid #dbe2ef;border-radius:25px;background:#f7f9fe;box-shadow:0 24px 60px rgba(31,44,83,.25);overflow:hidden;animation:drawerIn .22s ease-out}@keyframes drawerIn{from{opacity:0;transform:translateX(-50%) translateY(18px)}to{opacity:1;transform:translateX(-50%) translateY(0)}}
  .agent-drawer>header{display:flex;align-items:center;justify-content:space-between;padding:15px 16px 12px;border-bottom:1px solid #e6eaf2;background:rgba(255,255,255,.88)}.agent-drawer>header span{color:#7180d5;font-size:9px;font-weight:900;letter-spacing:.12em}.agent-drawer>header h2{margin:3px 0 0;font-size:18px}.agent-drawer>header button{width:36px;height:36px;border:0;border-radius:11px;background:#eef1f6;color:#667187;font-size:24px}
  .drawer-chat{overflow-y:auto;padding:14px}.drawer-chat article{width:86%;margin-bottom:10px;padding:11px 12px;border:1px solid #e6eaf2;border-radius:16px 16px 16px 5px;background:#fff}.drawer-chat article.mine{margin-left:auto;border-color:#596bda;border-radius:16px 16px 5px 16px;background:#596bda;color:#fff}.drawer-chat small{font-size:9px;opacity:.65}.drawer-chat p{margin:5px 0 0;font-size:13px;line-height:1.55}.drawer-empty{display:grid;height:100%;place-items:center;color:#929caf;font-size:12px}
  .drawer-composer{display:grid;grid-template-columns:minmax(0,1fr) auto;gap:8px;padding:10px;border-top:1px solid #e5e9f2;background:#fff}.drawer-composer input{min-width:0;min-height:45px;border:1px solid #dfe5ef;border-radius:12px;padding:0 12px;outline:none}
  @media(prefers-reduced-motion:reduce){.global-agent-orb,.voice-ring,.recording-glow{animation:none}}
</style>

<script lang="ts">
  import { onDestroy, onMount, tick } from 'svelte';
  import { page } from '$app/stores';
  import {
    chatClientAgent,
    adoptAgentLearningSession,
    classifyLiveRoomAgentMessage,
    createAgentLearningSession,
    createAgentLearningTurn,
    enqueueRoomManualAgentDecision,
    getLiveDevices,
    getLiveRuntime,
    getRoomAgentDecisions,
    getRoom,
    getRoomEvents,
    getRoomLiveAgentPlans,
    getRoomSessionStats,
    pauseLiveRuntime,
    resolveRoomSessionDecision,
    resumeLiveRuntime,
    setLiveRuntimeMode,
    setLiveRuntimePlan,
    startLiveRuntime,
    stopLiveRuntime,
    transcribeRoomAgentVoice,
  } from '$lib/api';
  import {
    closeLocalFloatingAudioForDifferentRoom,
    customerAudioMuted,
    floatingAudioState,
    flushRoomCompositeAudio,
    startRoomCompositeAudio,
    stopRoomCompositeAudio,
    toggleCustomerAudioMuted,
    unlockCustomerAudio,
    updateFloatingAudioSession,
  } from '$lib/audioRuntime';
  import type { LiveAgentPlan, LiveDevice, LiveRuntimeSnapshot, Room, RoomEvent } from '$lib/types';

  type LiveRoomAnswerMode = 'quick' | 'answer';
  type ChatMessage = {
    role: 'user' | 'agent';
    text: string;
    speechChoice?: {
      question: string;
      text: string;
      status?: 'pending' | 'sending' | 'sent';
      selected?: LiveRoomAnswerMode;
    };
    correctionChoice?: {
      sessionId: number;
      text: string;
      status: 'pending' | 'saving' | 'adopted';
      versionNo?: number;
    };
  };

  let roomId = 0;
  let room: Room | null = null;
  let runtime: LiveRuntimeSnapshot | null = null;
  let roomDevice: LiveDevice | null = null;
  let plans: LiveAgentPlan[] = [];
  let selectedPlanId = 0;
  let chatEvents: RoomEvent[] = [];
  let loading = true;
  let controlBusy = false;
  let pageError = '';
  let audioError = '';
  let selectedMode: 'anchor' | 'control' = 'anchor';
  let modeBusy = false;
  let modeSwipeStartX: number | null = null;
  let chatEl: HTMLDivElement;
  let pollTimer: number | undefined;
  let eventTimer: number | undefined;
  let barrageRefreshing = false;
  let sessionDecisionOpen = false;
  let sessionDecisionBusy = false;
  let pendingStartAfterSessionDecision = false;
  let swipedEventId = 0;
  let barrageSwipe:
    | { eventId: number; startX: number; startY: number }
    | null = null;
  let eventDecisionStates: Record<
    number,
    { busy?: LiveRoomAnswerMode; submitted?: LiveRoomAnswerMode; message?: string; error?: string }
  > = {};
  let correctionSession:
    | {
        eventId: number;
        question: string;
        target: string;
        backendSessionId?: number;
        latestReply?: string;
      }
    | null = null;

  const BARRAGE_WINDOW_MS = 2 * 60 * 60 * 1000;
  const BARRAGE_HISTORY_PAGE_SIZE = 300;
  const BARRAGE_HISTORY_MAX_PAGES = 70;

  let orbPressed = false;
  let agentDocked = true;
  let agentIdleTimer: number | undefined;
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
  let voiceComposerEl: HTMLFormElement | null = null;
  let agentOrbEl: HTMLButtonElement | null = null;

  let drawerOpen = false;
  let agentMessages: ChatMessage[] = [];
  let agentInput = '';
  let agentBusy = false;

  $: roomId = Number($page.params.id || 0);
  $: audioPlaying = runtime?.agent_state === 'working';
  $: agentActive = runtime?.agent_state === 'working' || runtime?.agent_state === 'paused';
  $: displayedMode = agentActive ? (runtime?.agent_mode || selectedMode) : selectedMode;
  $: if ($floatingAudioState.connected && audioError) audioError = '';

  onMount(async () => {
    if (!Number.isFinite(roomId) || roomId <= 0) {
      pageError = '直播间不存在';
      loading = false;
      return;
    }
    await closeLocalFloatingAudioForDifferentRoom(roomId);
    await refreshAll(true);
    pollTimer = window.setInterval(() => void refreshRoomRuntime(), 1800);
    eventTimer = window.setInterval(() => void refreshBarrage(), 1100);
  });

  onMount(() => {
    const handleOutsideVoiceComposer = (event: PointerEvent) => {
      if (!voiceComposerVisible) return;
      const target = event.target as Node | null;
      if (
        target &&
        (voiceComposerEl?.contains(target) || agentOrbEl?.contains(target))
      ) {
        return;
      }
      voiceComposerVisible = false;
    };
    window.addEventListener('pointerdown', handleOutsideVoiceComposer, true);
    return () => {
      window.removeEventListener('pointerdown', handleOutsideVoiceComposer, true);
    };
  });

  onDestroy(() => {
    if (pollTimer !== undefined) window.clearInterval(pollTimer);
    if (eventTimer !== undefined) window.clearInterval(eventTimer);
    if (holdTimer !== undefined) window.clearTimeout(holdTimer);
    if (agentIdleTimer !== undefined) window.clearTimeout(agentIdleTimer);
    try { recognition?.stop?.(); } catch {}
    try {
      if (mediaRecorder && mediaRecorder.state !== 'inactive') mediaRecorder.stop();
    } catch {}
    stopMediaStreamTracks();
  });

  async function refreshAll(first = false) {
    if (first) loading = true;
    pageError = '';
    try {
      const [roomValue, runtimeValue, planValue, deviceValues] = await Promise.all([
        getRoom(roomId),
        getLiveRuntime(roomId),
        getRoomLiveAgentPlans(roomId, true),
        getLiveDevices().catch(() => []),
      ]);
      room = roomValue;
      runtime = runtimeValue;
      roomDevice =
        (deviceValues || []).find(
          (item) => Number(item.room_id || 0) === roomId && item.binding_role === 'primary',
        ) || null;
      plans = (planValue.items || []).filter((item) => item.status !== 'archived');
      selectedPlanId = Number(runtimeValue.agent_plan_id || 0);
      if (runtimeValue.agent_state !== 'stopped') {
        selectedMode = runtimeValue.agent_mode === 'control' ? 'control' : 'anchor';
        updateFloatingAudioSession(
          roomId,
          roomValue.name,
          runtimeValue.agent_state === 'paused' ? 'paused' : 'working',
          true,
        );
      }
      await loadBarrageHistory();
      if (runtimeValue.agent_state === 'working') {
        try {
          await startRoomCompositeAudio(roomId, roomValue.name);
          audioError = '';
        } catch (err) {
          audioError = err instanceof Error ? err.message : '声音连接失败';
        }
      }
    } catch (err) {
      pageError = err instanceof Error ? err.message : '读取直播间失败';
    } finally {
      loading = false;
    }
  }

  async function refreshRoomRuntime() {
    try {
      const [roomValue, runtimeValue, deviceValues] = await Promise.all([
        getRoom(roomId),
        getLiveRuntime(roomId),
        getLiveDevices().catch(() => []),
      ]);
      room = roomValue;
      runtime = runtimeValue;
      roomDevice =
        (deviceValues || []).find(
          (item) => Number(item.room_id || 0) === roomId && item.binding_role === 'primary',
        ) || null;
      if (runtimeValue.agent_state !== 'stopped') {
        selectedMode = runtimeValue.agent_mode === 'control' ? 'control' : 'anchor';
        updateFloatingAudioSession(
          roomId,
          roomValue.name,
          runtimeValue.agent_state === 'paused' ? 'paused' : 'working',
          true,
        );
      }
      if (runtimeValue.agent_plan_id) selectedPlanId = Number(runtimeValue.agent_plan_id);
    } catch {}
  }

  function barrageCutoffMS() {
    return Date.now() - BARRAGE_WINDOW_MS;
  }

  function eventTimeMS(item: RoomEvent) {
    const parsed = Date.parse(item.occurred_at);
    return Number.isFinite(parsed) ? parsed : 0;
  }

  function normalizeBarrage(items: RoomEvent[], cutoffMS = barrageCutoffMS()) {
    const byID = new Map<number, RoomEvent>();
    for (const item of items) {
      if (item.event_type !== 'chat' || !String(item.content || '').trim()) continue;
      const occurred = eventTimeMS(item);
      if (!occurred || occurred < cutoffMS) continue;
      byID.set(item.id, item);
    }
    return [...byID.values()].sort((a, b) => {
      const timeDelta = eventTimeMS(b) - eventTimeMS(a);
      return timeDelta !== 0 ? timeDelta : b.id - a.id;
    });
  }

  async function loadBarrageHistory() {
    if (barrageRefreshing) return;
    barrageRefreshing = true;
    const cutoffMS = barrageCutoffMS();
    const loaded: RoomEvent[] = [];
    let beforeId = 0;
    try {
      for (let pageIndex = 0; pageIndex < BARRAGE_HISTORY_MAX_PAGES; pageIndex += 1) {
        const result = await getRoomEvents(roomId, {
          limit: BARRAGE_HISTORY_PAGE_SIZE,
          channel: 'important',
          eventType: 'chat',
          beforeId,
        });
        const items = result.items || [];
        let reachedCutoff = false;
        for (const item of items) {
          const occurred = eventTimeMS(item);
          if (occurred && occurred < cutoffMS) {
            reachedCutoff = true;
            continue;
          }
          loaded.push(item);
        }
        const nextBeforeId = Number(result.next_before_id || 0);
        if (
          reachedCutoff ||
          !result.has_more ||
          items.length === 0 ||
          nextBeforeId <= 0 ||
          nextBeforeId === beforeId
        ) {
          break;
        }
        beforeId = nextBeforeId;
      }
      chatEvents = normalizeBarrage(loaded, cutoffMS);
    } catch {}
    finally {
      barrageRefreshing = false;
    }
  }

  async function refreshBarrage() {
    if (barrageRefreshing) return;
    barrageRefreshing = true;
    try {
      const result = await getRoomEvents(roomId, {
        limit: BARRAGE_HISTORY_PAGE_SIZE,
        channel: 'important',
        eventType: 'chat',
      });
      chatEvents = normalizeBarrage([...(result.items || []), ...chatEvents]);
    } catch {}
    finally {
      barrageRefreshing = false;
    }
  }

  function eventDecisionState(eventId: number) {
    return eventDecisionStates[eventId] || {};
  }

  function setEventDecisionState(
    eventId: number,
    patch: { busy?: LiveRoomAnswerMode; submitted?: LiveRoomAnswerMode; message?: string; error?: string },
  ) {
    eventDecisionStates = {
      ...eventDecisionStates,
      [eventId]: { ...eventDecisionState(eventId), ...patch },
    };
  }

  function eventDecisionLabel(eventId: number, action: LiveRoomAnswerMode) {
    const state = eventDecisionState(eventId);
    if (state.busy === action) return action === 'quick' ? '抢答中…' : '提交中…';
    if (state.submitted === action) return action === 'quick' ? '已抢答' : '已回答';
    return action === 'quick' ? '抢答' : '回答';
  }

  function eventDecisionDisabled(eventId: number, action: LiveRoomAnswerMode) {
    const state = eventDecisionState(eventId);
    if (runtime?.agent_state !== 'working' || state.busy) return true;
    if (state.submitted === 'quick') return true;
    if (action === 'answer' && state.submitted === 'answer') return true;
    return false;
  }

  function beginBarrageSwipe(event: PointerEvent, item: RoomEvent) {
    if (event.pointerType === 'mouse' && event.button !== 0) return;
    barrageSwipe = { eventId: item.id, startX: event.clientX, startY: event.clientY };
  }

  function endBarrageSwipe(event: PointerEvent, item: RoomEvent) {
    const gesture = barrageSwipe;
    barrageSwipe = null;
    if (!gesture || gesture.eventId !== item.id) return;
    const deltaX = event.clientX - gesture.startX;
    const deltaY = event.clientY - gesture.startY;
    if (Math.abs(deltaX) < 28 || Math.abs(deltaX) <= Math.abs(deltaY)) return;
    swipedEventId = deltaX < 0 ? item.id : 0;
  }

  function cancelBarrageSwipe() {
    barrageSwipe = null;
  }

  async function ensureAudioForBarrageAction(eventId: number) {
    try {
      await unlockCustomerAudio();
      if (!$floatingAudioState.connected) {
        await startRoomCompositeAudio(roomId, room?.name || '');
      }
      audioError = '';
      return true;
    } catch (err) {
      const message = err instanceof Error ? err.message : '声音连接失败';
      audioError = message;
      setEventDecisionState(eventId, { error: message, message: '', busy: undefined });
      return false;
    }
  }

  async function submitBarrageDecision(event: RoomEvent, action: LiveRoomAnswerMode) {
    if (eventDecisionState(event.id).busy) return;
    if (runtime?.agent_state !== 'working') {
      setEventDecisionState(event.id, { error: '请先启动直播搭子。', message: '' });
      return;
    }
    const stats = await getRoomSessionStats(roomId).catch(() => null);
    if (stats?.resume_pending) {
      setEventDecisionState(event.id, { error: '请先选择续接上一场或作为新直播。', message: '' });
      return;
    }
    if (!(await ensureAudioForBarrageAction(event.id))) return;

    setEventDecisionState(event.id, { busy: action, error: '', message: '' });
    swipedEventId = 0;
    try {
      const result = await enqueueRoomManualAgentDecision(roomId, {
        question: String(event.content || '').trim(),
        title: '单条弹幕：' + String(event.content || '').trim().slice(0, 36),
        summary:
          action === 'quick'
            ? '人工从实时公屏发起抢答，立即生成并播出'
            : '人工从实时公屏加入待打断队列，由监控Agent安排回答时机',
        event_id: event.id,
        user_id: event.user_id || '',
        force_reopen: action === 'quick',
        manual_action: action,
      });
      const submitted =
        action === 'quick' ? 'quick' : (eventDecisionState(event.id).submitted || 'answer');
      let message = action === 'quick' ? '已提交抢答' : '已进入待打断队列';
      if (result.merged && action === 'quick') message = '已提升为抢答优先';
      else if (result.merged) message = '已融合到待回答任务';
      else if (result.suppressed) message = '同类问题刚回答过，可点抢答强制执行';
      setEventDecisionState(event.id, {
        busy: undefined,
        submitted,
        message,
        error: '',
      });
    } catch (err) {
      setEventDecisionState(event.id, {
        busy: undefined,
        error: err instanceof Error ? err.message : '提交回答失败',
      });
    }
  }

  function beginBarrageCorrection(event: RoomEvent) {
    const question = String(event.content || '').trim();
    if (!question) return;
    swipedEventId = 0;
    correctionSession = {
      eventId: event.id,
      question,
      target: question,
    };
    drawerOpen = true;
    keepAgentAwake();
    agentInput = '';
    agentMessages = [
      ...agentMessages,
      {
        role: 'agent',
        text:
          '正在纠正这条弹幕相关的回答规则：\n“' +
          question +
          '”\n\n直接告诉我哪里不对，或者正确应该怎么说。',
      },
    ];
    void tick().then(() => chatEl?.scrollTo({ top: chatEl.scrollHeight, behavior: 'smooth' }));
  }

  function cancelBarrageCorrection() {
    if (!correctionSession) return;
    correctionSession = null;
    agentMessages = [
      ...agentMessages,
      { role: 'agent', text: '已退出这次纠正，未采用的内容不会写入当前直播间记忆。' },
    ];
  }

  async function sendBarrageCorrectionFeedback(value: string) {
    const active = correctionSession;
    const feedback = value.trim();
    if (!active || !feedback || agentBusy) return;
    agentMessages = [...agentMessages, { role: 'user', text: feedback }];
    drawerOpen = true;
    agentBusy = true;
    await tick();
    chatEl?.scrollTo({ top: chatEl.scrollHeight });
    try {
      let sessionId = active.backendSessionId;
      if (!sessionId) {
        const created = await createAgentLearningSession(roomId, {
          source_type: 'question_correction',
          source_ref: 'room_event:' + active.eventId,
          question: active.question,
          original_reply: active.latestReply || '',
          target: active.target,
        });
        sessionId = created.id;
        active.backendSessionId = sessionId;
      }
      const output = await createAgentLearningTurn(roomId, sessionId, feedback);
      const visibleReply = String(output.result.result_text || '').trim();
      active.target = output.result.target || active.target;
      active.latestReply = visibleReply;
      correctionSession = { ...active };
      agentMessages = [
        ...agentMessages,
        {
          role: 'agent',
          text: visibleReply || '已生成修正结果。',
          correctionChoice: {
            sessionId,
            text: visibleReply,
            status: 'pending',
          },
        },
      ];
    } catch (err) {
      agentMessages = [
        ...agentMessages,
        { role: 'agent', text: err instanceof Error ? err.message : '生成修正结果失败，请稍后再试。' },
      ];
    } finally {
      agentBusy = false;
      await tick();
      chatEl?.scrollTo({ top: chatEl.scrollHeight, behavior: 'smooth' });
    }
  }

  async function adoptBarrageCorrection(message: ChatMessage) {
    const choice = message.correctionChoice;
    if (!choice || choice.status !== 'pending' || agentBusy) return;
    choice.status = 'saving';
    agentMessages = [...agentMessages];
    agentBusy = true;
    try {
      const adopted = await adoptAgentLearningSession(roomId, choice.sessionId);
      choice.status = 'adopted';
      choice.versionNo = adopted.version.version_no;
      correctionSession = null;
      agentMessages = [
        ...agentMessages,
        {
          role: 'agent',
          text:
            '已采用并立即生效。V' +
            adopted.version.version_no +
            '。当前直播间从下一次回答开始按这个意思处理。',
        },
      ];
    } catch (err) {
      choice.status = 'pending';
      agentMessages = [
        ...agentMessages,
        { role: 'agent', text: err instanceof Error ? err.message : '采用失败，请稍后再试。' },
      ];
    } finally {
      agentBusy = false;
      agentMessages = [...agentMessages];
      await tick();
      chatEl?.scrollTo({ top: chatEl.scrollHeight, behavior: 'smooth' });
    }
  }

  async function handleStart() {
    if (controlBusy) return;
    controlBusy = true;
    pageError = '';
    try {
      if (runtime?.agent_state === 'stopped' && runtime?.agent_mode !== selectedMode) {
        await setLiveRuntimeMode(roomId, selectedMode);
      }
      if (runtime?.agent_state === 'paused') {
        await resumeLiveRuntime(roomId);
      } else {
        const stats = await getRoomSessionStats(roomId).catch(() => null);
        if (stats?.resume_pending) {
          pendingStartAfterSessionDecision = true;
          sessionDecisionOpen = true;
          return;
        }
        await startLiveRuntime(roomId);
      }
      await refreshRoomRuntime();
      try {
        await startRoomCompositeAudio(roomId, room?.name || '');
        audioError = '';
      } catch (audioErr) {
        audioError = audioErr instanceof Error ? audioErr.message : '声音连接失败';
      }
    } catch (err) {
      const message = err instanceof Error ? err.message : '启动失败';
      if (message.includes('请先选择续接上一场或作为新直播')) {
        const stats = await getRoomSessionStats(roomId).catch(() => null);
        if (stats?.resume_pending) {
          pendingStartAfterSessionDecision = true;
          sessionDecisionOpen = true;
          pageError = '';
          return;
        }
      }
      pageError = message;
    } finally {
      controlBusy = false;
    }
  }

  async function chooseSessionContinuation(action: 'merge' | 'fresh') {
    if (sessionDecisionBusy) return;
    const shouldStart = pendingStartAfterSessionDecision;
    sessionDecisionBusy = true;
    pageError = '';
    try {
      await resolveRoomSessionDecision(roomId, action);
      sessionDecisionOpen = false;
      pendingStartAfterSessionDecision = false;
      await refreshRoomRuntime();
      if (action === 'fresh') await loadBarrageHistory();
      if (shouldStart) await handleStart();
    } catch (err) {
      pageError = err instanceof Error ? err.message : '处理直播续接失败';
    } finally {
      sessionDecisionBusy = false;
    }
  }

  async function handlePause() {
    if (controlBusy || runtime?.agent_state !== 'working') return;
    controlBusy = true;
    pageError = '';
    try {
      await pauseLiveRuntime(roomId);
      flushRoomCompositeAudio();
      updateFloatingAudioSession(roomId, room?.name || '', 'paused', true);
      await refreshRoomRuntime();
    } catch (err) {
      pageError = err instanceof Error ? err.message : '暂停失败';
    } finally {
      controlBusy = false;
    }
  }

  async function handleStop() {
    if (controlBusy || runtime?.agent_state === 'stopped') return;
    controlBusy = true;
    pageError = '';
    try {
      await stopLiveRuntime(roomId);
      await stopRoomCompositeAudio();
      updateFloatingAudioSession(roomId, room?.name || '', 'stopped', true);
      await refreshRoomRuntime();
    } catch (err) {
      pageError = err instanceof Error ? err.message : '停止失败';
    } finally {
      controlBusy = false;
    }
  }

  async function chooseMode(mode: 'anchor' | 'control') {
    if (agentActive || modeBusy || selectedMode === mode) return;
    const previous = selectedMode;
    selectedMode = mode;
    modeBusy = true;
    pageError = '';
    try {
      await setLiveRuntimeMode(roomId, mode);
      await refreshRoomRuntime();
    } catch (err) {
      selectedMode = previous;
      pageError = err instanceof Error ? err.message : '切换模式失败';
    } finally {
      modeBusy = false;
    }
  }

  function beginModeSwipe(event: PointerEvent) {
    if (agentActive || modeBusy) return;
    modeSwipeStartX = event.clientX;
  }

  function endModeSwipe(event: PointerEvent) {
    if (modeSwipeStartX === null || agentActive || modeBusy) {
      modeSwipeStartX = null;
      return;
    }
    const delta = event.clientX - modeSwipeStartX;
    modeSwipeStartX = null;
    if (Math.abs(delta) < 24) return;
    void chooseMode(delta < 0 ? 'control' : 'anchor');
  }

  async function changePlan(event: Event) {
    const value = Number((event.currentTarget as HTMLSelectElement).value || 0);
    if (!value || value === runtime?.agent_plan_id) return;
    controlBusy = true;
    pageError = '';
    try {
      await setLiveRuntimePlan(roomId, value);
      selectedPlanId = value;
      await refreshRoomRuntime();
    } catch (err) {
      selectedPlanId = Number(runtime?.agent_plan_id || 0);
      pageError = err instanceof Error ? err.message : '切换智能体方案失败';
    } finally {
      controlBusy = false;
    }
  }

  function scheduleAgentDock() {
    if (agentIdleTimer !== undefined) window.clearTimeout(agentIdleTimer);
    agentIdleTimer = window.setTimeout(() => {
      if (listening || agentBusy || voiceTranscribing) {
        scheduleAgentDock();
        return;
      }
      drawerOpen = false;
      voiceComposerVisible = false;
      agentDocked = true;
    }, 60_000);
  }

  function keepAgentAwake() {
    agentDocked = false;
    scheduleAgentDock();
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
    scheduleAgentDock();
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
    voiceTranscribing = true;
    voiceComposerVisible = true;
    speechHint = '正在把语音转换成文字…';
    try {
      const result = await transcribeRoomAgentVoice(roomId, blob, recordingFilename(mimeType));
      await deliverRecognizedVoice(result.text || '');
    } catch (err) {
      voiceComposerVisible = true;
      speechHint = err instanceof Error ? err.message : '语音转文字失败，请再说一次。';
    } finally {
      voiceTranscribing = false;
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
      const blob = new Blob(chunks, { type: mimeType });
      void transcribeRecordedVoice(blob, mimeType);
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
      speechHint = '当前手机浏览器不支持录音识别，请检查浏览器麦克风权限。';
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
    speechFinal = '';
    speechInterim = '';
    voiceText = '';
    voiceComposerVisible = false;
    try {
      if (await beginMediaRecorderListening()) return;
    } catch (err: any) {
      listening = false;
      stopMediaStreamTracks();
      mediaRecorder = null;
      const name = String(err?.name || '');
      if (name === 'NotAllowedError' || name === 'SecurityError') {
        voiceComposerVisible = true;
        speechHint = '没有麦克风权限，请在手机浏览器设置里允许本站使用麦克风。';
        return;
      }
      if (name === 'NotFoundError') {
        voiceComposerVisible = true;
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

  async function sendVoiceText() {
    const value = voiceText.trim();
    if (!value || agentBusy) return;
    voiceComposerVisible = false;
    voiceText = '';
    await sendAgentMessage(value);
  }

  async function sendDrawerText() {
    const value = agentInput.trim();
    if (!value || agentBusy) return;
    agentInput = '';
    await sendAgentMessage(value);
  }

  function extractLiveRoomExecutionContent(value: string) {
    const text = value.trim();
    const direct = text.match(/^(?:(?:你)?(?:帮我|替我)?(?:直接(?:打断)?)?(?:回答|回复|抢答|说|播|念|读)(?:一句|一下|下)?(?:这条(?:弹幕|问题)?|这个(?:问题|用户)?|他|她)?|(?:让)?(?:直播间|主播|智能体)(?:直接(?:打断)?)?(?:说|播|念|读)(?:一句|一下|下)?|给(?:这个用户|他|她)回复)[：:，,\s]*(.+)$/);
    return direct?.[1]?.trim() || text;
  }

  function isLikelyLiveRoomExecutionIntent(value: string) {
    const text = value.trim();
    if (!text || text.startsWith('/')) return false;
    return /(?:帮我|替我|让(?:直播间|主播|智能体)?|直接(?:打断)?)(?:说|播|念|读)|(?:直播间|主播|智能体).{0,4}(?:说|播|念|读)(?:一句|一下|下)?|^(?:直接(?:打断)?)?(?:说|播|念|读)(?:一句|一下|下)?/.test(text);
  }

  async function prepareLiveRoomSpeechChoice(value: string) {
    const question = extractLiveRoomExecutionContent(value);
    const enqueued = await enqueueRoomManualAgentDecision(roomId, {
      question,
      title: '手机端智能体上行播报预审核',
      summary: '先由 Core Agent 审核输入，并在确有必要时优化为可播文字；用户选择抢答或回答后才真正播音',
      reply_hint: '保留用户原意。原文字已经自然、安全、事实明确时尽量保持；存在风险、歧义或表达生硬时才优化。',
      manual_action: 'answer',
      manual_origin: 'agent_input_preview',
      execution_mode: 'intent',
      ttl_seconds: 120,
    });
    const decisionId = enqueued.item?.id;
    if (!decisionId) throw new Error('没有生成可播任务，请确认直播间 AI 已启动');

    let reply = '';
    for (let attempt = 0; attempt < 90; attempt += 1) {
      await new Promise((resolve) => window.setTimeout(resolve, 500));
      const snapshot = await getRoomAgentDecisions(roomId);
      const matched = (snapshot.simulation_results || []).find((item) => item.decision_id === decisionId);
      if (matched?.reply?.trim()) {
        reply = matched.reply.trim();
        break;
      }
    }
    if (!reply) throw new Error('审核生成超时，请确认直播间 AI 正在工作');

    agentMessages = [
      ...agentMessages,
      {
        role: 'agent',
        text: '已完成播出前审核。下面是最终建议播出的文字，选择“抢答”或“回答”后才会送入 TTS。',
        speechChoice: {
          question,
          text: reply,
          status: 'pending',
        },
      },
    ];
  }

  async function executePreparedLiveRoomSpeech(message: ChatMessage, mode: LiveRoomAnswerMode) {
    const choice = message.speechChoice;
    if (!choice || choice.status === 'sending' || choice.status === 'sent') return;
    choice.status = 'sending';
    choice.selected = mode;
    agentMessages = [...agentMessages];
    agentBusy = true;
    try {
      await enqueueRoomManualAgentDecision(roomId, {
        question: choice.question || choice.text,
        title: mode === 'quick' ? '手机端智能体上行抢答' : '手机端智能体上行回答',
        summary: mode === 'quick'
          ? '用户确认抢答，使用已审核文字进入现有硬打断播报链路'
          : '用户确认回答，使用已审核文字进入现有安全切点播报链路',
        reply_hint: choice.text,
        force_reopen: mode === 'quick',
        manual_action: mode,
        manual_origin: 'agent_input',
        execution_mode: 'verbatim',
        fixed_text: choice.text,
        ttl_seconds: mode === 'quick' ? 180 : 600,
      });
      choice.status = 'sent';
      agentMessages = [
        ...agentMessages,
        {
          role: 'agent',
          text: mode === 'quick'
            ? '抢答已提交，正在沿用现有 TTS、打断和回归链路播出。'
            : '回答已提交，正在沿用现有 TTS、安全切点和回归链路播出。',
        },
      ];
    } catch (err) {
      choice.status = 'pending';
      choice.selected = undefined;
      agentMessages = [
        ...agentMessages,
        { role: 'agent', text: err instanceof Error ? err.message : '播报提交失败，请稍后再试' },
      ];
    } finally {
      agentBusy = false;
      agentMessages = [...agentMessages];
      await tick();
      chatEl?.scrollTo({ top: chatEl.scrollHeight, behavior: 'smooth' });
    }
  }

  async function sendAgentMessage(value: string) {
    keepAgentAwake();
    if (correctionSession) {
      await sendBarrageCorrectionFeedback(value);
      return;
    }
    const history = [...agentMessages];
    agentMessages = [...agentMessages, { role: 'user', text: value }];
    drawerOpen = true;
    agentBusy = true;
    await tick();
    chatEl?.scrollTo({ top: chatEl.scrollHeight });
    try {
      let intent: 'chat' | 'learning' | 'test' | 'execution' | 'adopt' = 'chat';
      try {
        const classified = await classifyLiveRoomAgentMessage(roomId, {
          message: value,
          current_mode: 'chat',
          learning_active: false,
          test_active: false,
          execution_active: false,
          history: history.slice(-10).map((item) => ({ role: item.role, text: item.text })),
        });
        intent =
          classified.intent === 'chat' && isLikelyLiveRoomExecutionIntent(value)
            ? 'execution'
            : classified.intent;
      } catch {
        intent = isLikelyLiveRoomExecutionIntent(value) ? 'execution' : 'chat';
      }

      if (intent === 'execution') {
        await prepareLiveRoomSpeechChoice(value);
      } else {
        const response = await chatClientAgent(value, history.slice(-12));
        agentMessages = [...agentMessages, { role: 'agent', text: response.reply }];
      }
    } catch (err) {
      agentMessages = [
        ...agentMessages,
        { role: 'agent', text: err instanceof Error ? err.message : '暂时无法回答' },
      ];
    } finally {
      agentBusy = false;
      await tick();
      chatEl?.scrollTo({ top: chatEl.scrollHeight, behavior: 'smooth' });
    }
  }

  function runtimeLabel() {
    if (runtime?.agent_state === 'working') return '工作中';
    if (runtime?.agent_state === 'paused') return '已暂停';
    return '已停止';
  }

  function deviceVisualState(device: LiveDevice | null) {
    if (!device) return 'offline';
    const connection = String(device.connection_status || '').toLowerCase();
    if (connection !== 'online' && connection !== 'connected') return 'offline';
    const work = String(device.work_status || '').toLowerCase();
    const reason = String(device.stop_reason || '').toLowerCase();
    const explicitFault =
      ['fault', 'faulted', 'error', 'failed', 'failure', 'abnormal'].includes(work) ||
      /(fault|error|failed|failure|abnormal|exception)/.test(reason);
    return explicitFault ? 'fault' : 'online';
  }

  function deviceVisualLabel(device: LiveDevice | null) {
    const state = deviceVisualState(device);
    if (state === 'online') {
      if (runtime?.agent_state === 'working') return '工作中';
      if (runtime?.agent_state === 'paused') return '已暂停';
      return '连续待机';
    }
    if (state === 'fault') return '故障';
    return '离线';
  }

  function formatAISeconds(seconds = 0) {
    const safe = Math.max(0, Math.floor(Number(seconds) || 0));
    const hours = Math.floor(safe / 3600);
    const minutes = Math.floor((safe % 3600) / 60);
    if (hours > 0) return hours + '小时' + String(minutes).padStart(2, '0') + '分';
    return minutes + '分';
  }

  function formatEventClock(value: string) {
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) return '--:--';
    return date.toLocaleTimeString('zh-CN', {
      hour: '2-digit',
      minute: '2-digit',
      hour12: false,
    });
  }

  function closeDrawerFromMask(event: MouseEvent) {
    if (event.target === event.currentTarget) {
      drawerOpen = false;
      scheduleAgentDock();
    }
  }
</script>

<svelte:head><title>{room?.name || '直播间'}</title></svelte:head>

<section class="room-mobile-page top-space">
  <header class="room-mobile-header">
    <a href="/" aria-label="返回首页">‹</a>
    <div class="room-mobile-header-copy">
      <span class="room-mobile-eyebrow">LIVE ROOM</span>
      <h1>{room?.name || '直播间'}</h1>
      <div class="room-mobile-meta">
        <span class="room-meta-chip">
          <svg viewBox="0 0 24 24" aria-hidden="true">
            <path d="M4 10.2 12 4l8 6.2V20h-5.2v-5.8H9.2V20H4z"></path>
          </svg>
          房间 #{roomId}
        </span>
        <i>·</i>
        <span class="room-meta-chip">
          <svg viewBox="0 0 24 24" aria-hidden="true">
            <rect x="4" y="7" width="10.5" height="10" rx="2.5"></rect>
            <path d="m14.5 10.2 5-2.6v8.8l-5-2.6z"></path>
          </svg>
          {displayedMode === 'anchor' ? '主播模式' : '中控模式'}
        </span>
      </div>
    </div>
  </header>

  {#if pageError}<div class="room-error">{pageError}</div>{/if}

  {#if loading}
    <div class="room-loading">正在进入直播间…</div>
  {:else}
    <section class="room-status-card" class:playing={audioPlaying}>
      <div class="ai-quota-pill" aria-label={'AI剩余 ' + formatAISeconds(runtime?.quota_remaining_seconds || 0)}>
        <span class="ai-quota-clock" aria-hidden="true">
          <svg viewBox="0 0 24 24">
            <circle cx="12" cy="12" r="7.5"></circle>
            <path d="M12 7.7v4.8l3.2 2"></path>
          </svg>
        </span>
        <span class="ai-quota-label">AI剩余</span>
        <strong>{formatAISeconds(runtime?.quota_remaining_seconds || 0)}</strong>
        <span class="ai-quota-arrow" aria-hidden="true">›</span>
      </div>
      <div class="status-top">
        <div>
          <span class="live-dot" class:online={room?.status === 'live' || runtime?.room_live}></span>
          <div class="live-state-copy">
            <strong>{room?.status === 'live' || runtime?.room_live ? '直播中' : '当前未直播'}</strong>
            <small>{displayedMode === 'anchor' ? '主播模式' : '中控模式'}</small>
          </div>
          <button
            class="room-mute-toggle"
            class:muted={$customerAudioMuted}
            type="button"
            aria-label={$customerAudioMuted ? '恢复本机声音' : '静音本机声音'}
            aria-pressed={$customerAudioMuted}
            title={$customerAudioMuted ? '恢复声音' : '静音'}
            on:click={toggleCustomerAudioMuted}
          >
            {#if $customerAudioMuted}
              <svg viewBox="0 0 24 24" aria-hidden="true">
                <path d="M4 9v6h4l5 4V5L8 9H4z"></path>
                <path class="speaker-line" d="m16.2 9.2 4.2 4.2m0-4.2-4.2 4.2"></path>
              </svg>
            {:else}
              <svg viewBox="0 0 24 24" aria-hidden="true">
                <path d="M4 9v6h4l5 4V5L8 9H4z"></path>
                <path class="speaker-line" d="M16 8.2c1.1 1 1.7 2.3 1.7 3.8S17.1 14.8 16 15.8"></path>
                <path class="speaker-line" d="M18.8 5.8c1.8 1.7 2.8 3.7 2.8 6.2s-1 4.5-2.8 6.2"></path>
              </svg>
            {/if}
          </button>
        </div>
        <small class="room-number">房间 #{roomId}</small>
      </div>

      <div class="audience-block">
        <strong>{Number(room?.online_count || 0).toLocaleString()}</strong>
        <span class="audience-icon" aria-hidden="true">
          <svg viewBox="0 0 24 24"><circle cx="12" cy="7" r="4"></circle><path d="M5.5 20c.5-4.1 2.7-6.2 6.5-6.2s6 2.1 6.5 6.2"></path></svg>
        </span>
        <small>当前在线</small>
      </div>

      <div
        class="audio-wave"
        class:active={audioPlaying}
        class:muted={$customerAudioMuted}
        aria-label={$customerAudioMuted ? '本机已静音' : audioPlaying ? '正在播放声音' : '当前没有播放声音'}
      >
        {#each Array(22) as _, i}
          <span style={'animation-delay:' + (-i * 63) + 'ms'}></span>
        {/each}
      </div>
      {#if agentActive}
        <div class="mode-locked">
          <span>{displayedMode === 'anchor' ? '主播模式' : '中控模式'}</span>
        </div>
      {:else}
        <div
          class="mode-slider"
          class:control={selectedMode === 'control'}
          role="group"
          aria-label="选择运行模式"
          on:pointerdown={beginModeSwipe}
          on:pointerup={endModeSwipe}
          on:pointercancel={() => (modeSwipeStartX = null)}
        >
          <span class="mode-slider-thumb"></span>
          <button
            type="button"
            class:active={selectedMode === 'anchor'}
            disabled={modeBusy}
            on:click={() => chooseMode('anchor')}
          >主播模式</button>
          <button
            type="button"
            class:active={selectedMode === 'control'}
            disabled={modeBusy}
            on:click={() => chooseMode('control')}
          >中控模式</button>
        </div>
      {/if}
      {#if audioError}<div class="audio-error">{audioError}，恢复前台后会自动重连；如浏览器仍拦截声音，轻触页面即可恢复。</div>{/if}

      <div class="room-controls">
        <button
          class="start"
          class:active={runtime?.agent_state === 'working'}
          type="button"
          disabled={controlBusy || runtime?.agent_state === 'working'}
          on:click={handleStart}
        >
          <span>▶</span><strong>{runtime?.agent_state === 'paused' ? '继续' : '开始'}</strong>
        </button>
        <button class="pause" class:active={runtime?.agent_state === 'paused'} type="button" disabled={controlBusy || runtime?.agent_state !== 'working'} on:click={handlePause}>
          <span>Ⅱ</span><strong>暂停</strong>
        </button>
        <button class="stop" class:active={runtime?.agent_state === 'stopped'} type="button" disabled={controlBusy || runtime?.agent_state === 'stopped'} on:click={handleStop}>
          <span>■</span><strong>停止</strong>
        </button>
      </div>
    </section>

    <section class="plan-select-card">
      <div
        class="room-device-indicator"
        class:online={deviceVisualState(roomDevice) === 'online'}
        class:fault={deviceVisualState(roomDevice) === 'fault'}
        class:offline={deviceVisualState(roomDevice) === 'offline'}
        title={roomDevice ? roomDevice.sn + ' · ' + deviceVisualLabel(roomDevice) : '未检测到已绑定设备'}
        aria-label={'设备状态：' + deviceVisualLabel(roomDevice)}
      >
        <span class="device-shell" aria-hidden="true">
          <i class="device-screen"></i>
          <i class="device-led"></i>
        </span>
        <strong>{deviceVisualLabel(roomDevice)}</strong>
      </div>
      <div>
        <span>LIVE AGENT PLAN</span>
        <strong>直播间智能体方案</strong>
      </div>
      <select bind:value={selectedPlanId} on:change={changePlan} disabled={controlBusy || plans.length === 0}>
        {#if plans.length === 0}<option value={0}>还没有发布方案</option>{/if}
        {#each plans as plan}
          <option value={plan.id}>{plan.name}</option>
        {/each}
      </select>
      {#if runtime?.agent_plan_id}
        <small>当前使用：{runtime.agent_plan_name || plans.find((item) => item.id === runtime?.agent_plan_id)?.name || '已绑定方案'}</small>
      {:else}
        <small>请选择当前直播间要使用的智能体方案</small>
      {/if}
    </section>

    <section class="barrage-card">
      <header>
        <div><span>LIVE CHAT</span><h2>实时弹幕</h2></div>
        <b>{chatEvents.length}</b>
      </header>
      <div class="barrage-list">
        {#if chatEvents.length === 0}
          <div class="barrage-empty">等待直播间弹幕…</div>
        {:else}
          {#each chatEvents as event}
            <div
              class="barrage-swipe-row"
              class:open={swipedEventId === event.id}
              role="group"
              aria-label={'弹幕操作：' + (event.nickname || '游客')}
              on:pointerdown={(pointerEvent) => beginBarrageSwipe(pointerEvent, event)}
              on:pointerup={(pointerEvent) => endBarrageSwipe(pointerEvent, event)}
              on:pointercancel={cancelBarrageSwipe}
            >
              <div class="barrage-actions" role="group" aria-label="弹幕操作按钮" on:pointerdown|stopPropagation>
                <button type="button" class="correct" on:click={() => beginBarrageCorrection(event)}>纠正</button>
                <button
                  type="button"
                  class="quick"
                  disabled={eventDecisionDisabled(event.id, 'quick')}
                  on:click={() => submitBarrageDecision(event, 'quick')}
                >{eventDecisionLabel(event.id, 'quick')}</button>
                <button
                  type="button"
                  class="answer"
                  disabled={eventDecisionDisabled(event.id, 'answer')}
                  on:click={() => submitBarrageDecision(event, 'answer')}
                >{eventDecisionLabel(event.id, 'answer')}</button>
              </div>
              <article class:open={swipedEventId === event.id}>
                <time datetime={event.occurred_at}>{formatEventClock(event.occurred_at)}</time>
                <strong>{event.nickname || '游客'}</strong>
                <p>{event.content}</p>
                {#if eventDecisionState(event.id).message}
                  <small class="barrage-action-feedback ok">{eventDecisionState(event.id).message}</small>
                {:else if eventDecisionState(event.id).error}
                  <small class="barrage-action-feedback error">{eventDecisionState(event.id).error}</small>
                {/if}
              </article>
            </div>
          {/each}
        {/if}
      </div>
    </section>
  {/if}
</section>

{#if sessionDecisionOpen}
  <div class="session-decision-mask">
    <div class="session-decision-dialog" role="dialog" aria-modal="true" aria-label="直播重新开播处理">
      <span>直播已重新开播</span>
      <h2>接着上一场，还是新的一场？</h2>
      <p>续接上一场会保留原开始时间和直播上下文；新的一场会重新建立本场实时上下文，历史记录仍然保留。</p>
      <div>
        <button type="button" disabled={sessionDecisionBusy} on:click={() => chooseSessionContinuation('merge')}>
          {sessionDecisionBusy ? '处理中…' : '续接上一场'}
        </button>
        <button class="fresh" type="button" disabled={sessionDecisionBusy} on:click={() => chooseSessionContinuation('fresh')}>
          新的一场
        </button>
      </div>
    </div>
  </div>
{/if}

<button
  bind:this={agentOrbEl}
  class="room-agent-orb"
  class:docked={agentDocked}
  class:listening
  type="button"
  aria-label={listening ? '正在听你说话' : '长按和智能体说话'}
  on:pointerdown|preventDefault={beginAgentHold}
  on:pointerup|preventDefault={endAgentHold}
  on:pointercancel|preventDefault={endAgentHold}
  on:contextmenu|preventDefault
  on:dragstart|preventDefault
  on:selectstart|preventDefault
>
  {#if listening}
    <span class="voice-ring one"></span>
    <span class="voice-ring two"></span>
    <span class="voice-ring three"></span>
    <span class="recording-glow"></span>
    <span class="mic-icon">●</span>
    <span class="mic-stem"></span>
  {:else}
    <span class="agent-star">✦</span>
  {/if}
</button>

{#if listening}
  <div class="listening-caption">松手识别并发送</div>
{/if}

{#if voiceComposerVisible}
  <form bind:this={voiceComposerEl} class="voice-floating-composer" on:submit|preventDefault={sendVoiceText}>
    <input bind:value={voiceText} placeholder={speechHint || '语音会在这里转成文字…'} aria-label="语音转文字内容" />
    <button type="submit" disabled={voiceTranscribing || !voiceText.trim() || agentBusy}>
      {voiceTranscribing ? '识别中…' : '发送'}
    </button>
  </form>
{/if}

{#if drawerOpen}
  <div class="agent-drawer-mask" role="presentation" on:click={closeDrawerFromMask} on:pointerdown={keepAgentAwake}>
    <section class="agent-drawer">
      <header>
        <div>
          <span>LIVE COMPANION</span>
          <h2>直播智能体</h2>
        </div>
        <button type="button" aria-label="关闭对话" on:click={() => { drawerOpen = false; scheduleAgentDock(); }}>×</button>
      </header>
      <div class="drawer-chat" bind:this={chatEl}>
        {#if correctionSession}
          <div class="mobile-correction-active">
            <div>
              <span>正在纠正</span>
              <strong>{correctionSession.target}</strong>
            </div>
            <button type="button" on:click={cancelBarrageCorrection}>结束纠正</button>
          </div>
        {/if}
        {#if agentMessages.length === 0}
          <div class="drawer-empty">长按下面的小球说话，或者直接输入。</div>
        {/if}
        {#each agentMessages as message}
          <article class:mine={message.role === 'user'}>
            <small>{message.role === 'user' ? '我' : '智能体'}</small>
            <p>{message.text}</p>
            {#if message.speechChoice}
              <div class="mobile-speech-choice">
                <span>审核后可播文字</span>
                <p>{message.speechChoice.text}</p>
                <div>
                  <button
                    type="button"
                    disabled={message.speechChoice.status === 'sending' || message.speechChoice.status === 'sent'}
                    on:click={() => executePreparedLiveRoomSpeech(message, 'quick')}
                  >
                    {message.speechChoice.status === 'sending' && message.speechChoice.selected === 'quick' ? '提交中…' : '抢答'}
                  </button>
                  <button
                    type="button"
                    disabled={message.speechChoice.status === 'sending' || message.speechChoice.status === 'sent'}
                    on:click={() => executePreparedLiveRoomSpeech(message, 'answer')}
                  >
                    {message.speechChoice.status === 'sending' && message.speechChoice.selected === 'answer' ? '提交中…' : '回答'}
                  </button>
                </div>
                {#if message.speechChoice.status === 'sent'}
                  <small>已按{message.speechChoice.selected === 'quick' ? '抢答' : '回答'}提交</small>
                {/if}
              </div>
            {/if}
            {#if message.correctionChoice}
              <div class="mobile-correction-choice">
                <span>修正候选</span>
                <p>{message.correctionChoice.text}</p>
                <button
                  type="button"
                  disabled={message.correctionChoice.status !== 'pending' || agentBusy}
                  on:click={() => adoptBarrageCorrection(message)}
                >
                  {message.correctionChoice.status === 'saving'
                    ? '采用中…'
                    : message.correctionChoice.status === 'adopted'
                      ? '已采用 V' + (message.correctionChoice.versionNo || '')
                      : '采用并立即生效'}
                </button>
              </div>
            {/if}
          </article>
        {/each}
        {#if agentBusy}
          <article><small>智能体</small><p>正在处理…</p></article>
        {/if}
      </div>
      <form class="drawer-composer" on:submit|preventDefault={sendDrawerText}>
        <input bind:value={agentInput} on:input={keepAgentAwake} placeholder="继续跟智能体说…" />
        <button type="submit" disabled={!agentInput.trim() || agentBusy}>发送</button>
      </form>
    </section>
  </div>
{/if}

<style>
  .room-mobile-page{padding:0 16px 190px}
  .room-mobile-header{display:grid;grid-template-columns:48px minmax(0,1fr);align-items:start;gap:14px;margin:0 -3px 16px;padding:10px 10px 12px;border-radius:24px;background:linear-gradient(145deg,rgba(255,255,255,.88),rgba(239,245,255,.72));box-shadow:inset 0 1px 0 rgba(255,255,255,.92)}
  .room-mobile-header>a{display:grid;width:48px;height:48px;place-items:center;border:1px solid rgba(222,229,240,.78);border-radius:16px;background:rgba(239,243,249,.9);color:#526784;font-size:31px;box-shadow:0 6px 16px rgba(64,79,119,.06)}
  .room-mobile-header-copy{min-width:0}
  .room-mobile-eyebrow{display:block;color:#7e8ca8;font-size:10px;font-weight:900;letter-spacing:.15em}
  .room-mobile-header h1{margin:4px 0 8px;color:#182845;font-size:28px;line-height:1.06;font-weight:950;letter-spacing:-.8px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
  .room-mobile-meta{display:flex;align-items:center;gap:6px;min-width:0;color:#7181b1}
  .room-mobile-meta>i{font-style:normal;color:#9aa7c7;font-size:12px}
  .room-meta-chip{display:inline-flex;min-width:0;align-items:center;gap:5px;padding:5px 8px;border:1px solid rgba(206,217,242,.72);border-radius:999px;background:rgba(231,238,253,.72);color:#6373a7;font-size:10px;font-weight:850;white-space:nowrap}
  .room-meta-chip svg{width:14px;height:14px;flex:0 0 auto;fill:#8295d4}
  .room-error{margin-bottom:12px;padding:11px 13px;border-radius:13px;background:#fff0f1;color:#b84e58;font-size:12px}
  .room-loading{display:grid;min-height:360px;place-items:center;color:#8993a7}
  .session-decision-mask{position:fixed;inset:0;z-index:95;display:grid;place-items:center;padding:24px;background:rgba(24,34,61,.34);backdrop-filter:blur(8px)}
  .session-decision-dialog{width:min(100%,350px);padding:24px;border:1px solid rgba(211,220,242,.95);border-radius:26px;background:linear-gradient(160deg,#fff,#f2f6ff);box-shadow:0 24px 70px rgba(31,45,87,.26)}
  .session-decision-dialog>span{display:inline-flex;padding:6px 10px;border-radius:999px;background:#e9efff;color:#5d70cf;font-size:10px;font-weight:900;letter-spacing:.05em}
  .session-decision-dialog h2{margin:15px 0 9px;color:#1e2c48;font-size:22px;line-height:1.25}
  .session-decision-dialog p{margin:0;color:#7c879d;font-size:12px;line-height:1.7}
  .session-decision-dialog>div{display:grid;grid-template-columns:1fr 1fr;gap:10px;margin-top:20px}
  .session-decision-dialog button{min-height:46px;border:1px solid #6578df;border-radius:14px;background:linear-gradient(145deg,#6276df,#5368d6);color:#fff;font-size:13px;font-weight:900;box-shadow:0 9px 22px rgba(76,95,196,.18)}
  .session-decision-dialog button.fresh{border-color:#d6deef;background:#f2f5fb;color:#56637c;box-shadow:none}
  .session-decision-dialog button:disabled{opacity:.58}
  .room-status-card{position:relative;overflow:hidden;padding:17px;border:1px solid #cbd7ef;border-radius:26px;background:linear-gradient(150deg,#e7eefc 0%,#dde8fb 52%,#edf4fd 100%);box-shadow:0 16px 38px rgba(65,82,138,.12)}
  .ai-quota-pill{position:absolute;right:16px;top:12px;z-index:3;display:flex;min-width:196px;height:42px;align-items:center;justify-content:center;gap:7px;padding:0 13px;border:1px solid rgba(168,184,226,.48);border-radius:999px;background:linear-gradient(180deg,rgba(255,255,255,.78),rgba(238,244,255,.76));color:#6b79a5;box-shadow:0 6px 18px rgba(66,87,156,.10),inset 0 1px 0 rgba(255,255,255,.95);backdrop-filter:blur(10px)}
  .ai-quota-clock{display:grid;width:20px;height:20px;place-items:center;color:#5269df}
  .ai-quota-clock svg{width:20px;height:20px;fill:none;stroke:currentColor;stroke-width:2.2;stroke-linecap:round;stroke-linejoin:round}
  .ai-quota-label{font-size:10px;font-weight:850;color:#7581a4;white-space:nowrap}
  .ai-quota-pill strong{font-size:13px;color:#4057c7;font-weight:950;white-space:nowrap;letter-spacing:-.15px}
  .ai-quota-arrow{margin-left:1px;color:#5970d6;font-size:23px;line-height:1;font-weight:700;transform:translateY(-1px)}
  .room-status-card::after{content:"";position:absolute;right:-70px;top:-95px;width:220px;height:220px;border-radius:50%;background:radial-gradient(circle,rgba(103,124,229,.20),rgba(103,124,229,0) 70%);pointer-events:none}
  .room-status-card.playing{border-color:#aebef0;box-shadow:0 18px 42px rgba(74,94,187,.17)}
  .status-top{position:relative;z-index:1;display:flex;align-items:center;justify-content:space-between;padding-right:204px}.status-top>div{display:flex;align-items:center;gap:8px}.status-top strong{font-size:12px;color:#65718a}.status-top small{color:#8a95aa;font-size:10px}.live-state-copy{display:grid!important;gap:1px!important}.live-state-copy small{color:#74829f;font-size:9px!important;font-weight:850}.room-number{position:absolute;right:5px;top:47px;white-space:nowrap}
  .room-mute-toggle{display:grid;width:40px;height:40px;flex:0 0 40px;place-items:center;margin-left:4px;border:1px solid rgba(126,145,216,.22);border-radius:13px;background:rgba(255,255,255,.48);color:#5b70d7;box-shadow:0 5px 14px rgba(70,88,184,.09),inset 0 1px 0 rgba(255,255,255,.72);transition:transform .16s ease,background .16s ease,border-color .16s ease,color .16s ease}.room-mute-toggle:active{transform:scale(.94)}.room-mute-toggle.muted{border-color:rgba(218,115,134,.30);background:rgba(255,238,241,.72);color:#ca6678}.room-mute-toggle svg{width:24px;height:24px;fill:currentColor}.room-mute-toggle .speaker-line{fill:none;stroke:currentColor;stroke-width:2;stroke-linecap:round;stroke-linejoin:round}
  .live-dot{width:10px;height:10px;border-radius:50%;background:#b9c3d2}.live-dot.online{background:#28b984;box-shadow:0 0 0 5px rgba(40,185,132,.12)}
  .audience-block{position:relative;z-index:1;display:grid;grid-template-columns:auto 22px;grid-template-rows:auto auto;justify-content:center;align-items:end;column-gap:5px;padding:19px 0 12px;text-align:center}
  .audience-block>strong{font-size:48px;line-height:.9;color:#3449b7;letter-spacing:-2px;font-weight:950}.audience-icon{width:19px;height:19px;color:#7183dd;transform:translateY(-2px)}.audience-icon svg{width:100%;height:100%;fill:currentColor}.audience-block small{grid-column:1 / span 2;margin-top:8px;color:#75829a;font-size:10px;font-weight:850;letter-spacing:.08em}
  .audio-wave{position:relative;z-index:1;display:flex;height:54px;align-items:center;justify-content:center;gap:4px;padding:0 12px;border:1px solid rgba(103,120,188,.13);border-radius:17px;background:rgba(38,50,84,.08)}
  .audio-wave span{width:3px;height:10px;border-radius:999px;background:#8390bb;opacity:.55;transition:.2s}.audio-wave span:nth-child(3n){height:20px}.audio-wave span:nth-child(4n){height:27px}.audio-wave span:nth-child(5n){height:16px}
  .audio-wave.active span{background:#5d72e2;opacity:.9;animation:wave 720ms ease-in-out infinite alternate}.audio-wave.active span:nth-child(3n){animation-duration:560ms}.audio-wave.active span:nth-child(4n){animation-duration:840ms}
  .audio-wave.active.muted span{background:#7f8fcd;opacity:.68}
  @keyframes wave{from{transform:scaleY(.35);opacity:.5}to{transform:scaleY(1.5);opacity:1}}
  .mode-locked{position:relative;z-index:1;display:flex;justify-content:center;margin:9px 0 13px}.mode-locked span{min-width:116px;padding:8px 15px;border:1px solid rgba(101,118,183,.16);border-radius:999px;background:rgba(255,255,255,.48);color:#657391;font-size:11px;font-weight:900;text-align:center}
  .mode-slider{position:relative;z-index:1;display:grid;grid-template-columns:1fr 1fr;width:230px;height:38px;margin:9px auto 13px;padding:3px;border:1px solid rgba(101,118,183,.16);border-radius:999px;background:rgba(255,255,255,.48);overflow:hidden;touch-action:pan-y}
  .mode-slider-thumb{position:absolute;left:3px;top:3px;width:calc(50% - 3px);height:30px;border-radius:999px;background:linear-gradient(145deg,#6075e2,#4e63d2);box-shadow:0 5px 14px rgba(70,88,184,.23);transition:transform .22s ease}
  .mode-slider.control .mode-slider-thumb{transform:translateX(100%)}
  .mode-slider button{position:relative;z-index:1;border:0;background:transparent;color:#78859d;font-size:11px;font-weight:900}.mode-slider button.active{color:#fff}.mode-slider button:disabled{opacity:.6}
  .audio-error{position:relative;z-index:1;margin:-4px 0 12px;padding:8px 10px;border-radius:11px;background:rgba(255,239,239,.82);color:#b64f5b;font-size:10px;line-height:1.45;text-align:center}
  .room-controls{position:relative;z-index:1;display:grid;grid-template-columns:repeat(3,1fr);gap:9px}
  .room-controls button{display:grid;grid-template-columns:auto auto;justify-content:center;align-items:center;gap:6px;min-height:48px;border:1px solid transparent;border-radius:15px;font-size:12px;font-weight:900}.room-controls button span{font-size:13px}.room-controls button:disabled{opacity:.48}
  .room-controls .start{background:#e9efff;color:#465ccd}.room-controls .pause{background:#fff4dc;color:#a8751b}.room-controls .stop{background:#feecec;color:#bb4d58}.room-controls button.active{box-shadow:inset 0 0 0 1px currentColor,0 6px 18px rgba(55,72,125,.08)}
  .plan-select-card,.barrage-card{margin-top:14px;border:1px solid #dce3ef;border-radius:22px;background:linear-gradient(150deg,#f8faff,#eef3fb);box-shadow:0 10px 28px rgba(52,67,118,.07)}
  .plan-select-card{position:relative;display:grid;gap:10px;padding:15px}.plan-select-card>div:not(.room-device-indicator){display:grid;gap:3px}.plan-select-card>div:not(.room-device-indicator) span,.barrage-card header span{color:#8490a7;font-size:9px;font-weight:900;letter-spacing:.12em}.plan-select-card>div:not(.room-device-indicator) strong{color:#25324a;font-size:16px}
  .room-device-indicator{position:absolute;right:16px;top:15px;display:flex!important;width:126px;height:42px;align-items:center;justify-content:center;gap:9px;border:1px solid #d8dfeb;border-radius:12px;background:#eef2f7;color:#8791a2;box-shadow:inset 0 1px 0 rgba(255,255,255,.7)}
  .room-device-indicator.online{border-color:#bce8d4;background:#e8f8f0;color:#23986e}
  .room-device-indicator.fault{border-color:#f0c4c8;background:#fff0f1;color:#c4515d}
  .room-device-indicator.offline{border-color:#d8dfeb;background:#eef1f5;color:#8b94a3}
  .room-device-indicator>strong{font-size:11px!important;color:currentColor!important}
  .device-shell{position:relative;display:block;width:34px;height:22px;border:2px solid currentColor;border-radius:5px;background:rgba(255,255,255,.48)}
  .device-screen{position:absolute;left:5px;right:5px;top:4px;height:7px;border-radius:2px;background:currentColor;opacity:.16}
  .device-led{position:absolute;right:4px;bottom:3px;width:5px;height:5px;border-radius:50%;background:currentColor;box-shadow:0 0 0 2px rgba(255,255,255,.7)}
  .plan-select-card select{width:100%;min-height:48px;padding:0 40px 0 13px;border:1px solid #cad4ea;border-radius:14px;background:#fff;color:#2f3d59;outline:none;font-weight:800}.plan-select-card small{color:#8792a7;font-size:10px}
  .barrage-card{padding:15px}.barrage-card header{display:flex;align-items:flex-end;justify-content:space-between;margin-bottom:10px}.barrage-card header h2{margin:3px 0 0;font-size:18px}.barrage-card header b{padding:5px 8px;border-radius:999px;background:#e8edff;color:#5a6ed7;font-size:10px}
  .barrage-list{height:760px;overflow-y:auto;overflow-x:hidden;scrollbar-width:none;padding:4px 1px}.barrage-list::-webkit-scrollbar{display:none}.barrage-swipe-row{position:relative;overflow:hidden;touch-action:pan-y;background:#eef2fb}.barrage-actions{position:absolute;right:0;top:0;bottom:0;display:grid;grid-template-columns:repeat(3,66px);width:198px}.barrage-actions button{border:0;color:#fff;font-size:11px;font-weight:900}.barrage-actions button.correct{background:#7b87a8}.barrage-actions button.quick{background:#e76f78}.barrage-actions button.answer{background:#6074df}.barrage-actions button:disabled{opacity:.48}.barrage-list article{position:relative;z-index:1;display:grid;grid-template-columns:42px auto 1fr;min-height:38px;box-sizing:border-box;align-items:center;gap:7px;padding:8px 2px;border-bottom:1px solid rgba(218,225,238,.75);background:linear-gradient(150deg,#f8faff,#eef3fb);transition:transform .2s ease;will-change:transform}.barrage-list article.open{transform:translateX(-198px)}.barrage-swipe-row:last-child article{border-bottom:0}.barrage-list time{color:#9aa4b5;font-size:10px;font-variant-numeric:tabular-nums}.barrage-list strong{color:#596bd0;font-size:11px;white-space:nowrap}.barrage-list p{margin:0;color:#3c485f;font-size:12px;line-height:1.5}.barrage-action-feedback{grid-column:2 / -1;margin-top:-2px;font-size:9px;line-height:1.35}.barrage-action-feedback.ok{color:#319173}.barrage-action-feedback.error{color:#c45461}.barrage-empty{display:grid;height:100%;place-items:center;color:#96a0b2;font-size:12px}
  .room-agent-orb{position:fixed;left:50%;bottom:calc(112px + env(safe-area-inset-bottom));z-index:57;display:grid;width:68px;height:68px;place-items:center;transform:translateX(-50%);border:2px solid rgba(255,255,255,.82);border-radius:50%;background:radial-gradient(circle at 35% 30%,#acbbff 0,#7587f4 34%,#4c5ed6 76%,#37449f 100%);color:#fff;box-shadow:0 14px 34px rgba(75,94,208,.36),inset 0 0 0 1px rgba(255,255,255,.45);animation:orbFloat 3.1s ease-in-out infinite;transition:left .34s cubic-bezier(.22,.82,.32,1),bottom .34s ease,width .22s ease,height .22s ease,box-shadow .22s ease,background .22s ease;touch-action:none;-webkit-touch-callout:none;-webkit-user-select:none;user-select:none;-webkit-user-drag:none;-webkit-tap-highlight-color:transparent}
  .room-agent-orb.docked{left:min(100%,calc(50% + 270px));bottom:calc(102px + env(safe-area-inset-bottom));animation:dockedPulse 3.4s ease-in-out infinite;box-shadow:0 9px 26px rgba(75,94,208,.3),inset 0 0 0 1px rgba(255,255,255,.45)}
  .room-agent-orb *{-webkit-touch-callout:none;-webkit-user-select:none;user-select:none;-webkit-user-drag:none;pointer-events:none}
  .agent-star{font-size:28px}.room-agent-orb.listening{left:50%;width:116px;height:116px;bottom:calc(110px + env(safe-area-inset-bottom));animation:recordingBreath 1.6s ease-in-out infinite;background:radial-gradient(circle at 35% 28%,#b9c5ff 0,#7185f3 28%,#5368df 60%,#3648ad 100%);box-shadow:0 0 0 12px rgba(91,111,222,.08),0 20px 48px rgba(57,73,170,.42),inset 0 0 0 1px rgba(255,255,255,.56)}
  @keyframes orbFloat{0%,100%{transform:translateX(-50%) translateY(0) scale(1)}50%{transform:translateX(-50%) translateY(-6px) scale(1.04)}}
  @keyframes dockedPulse{0%,100%{transform:translateX(-50%) scale(.96)}50%{transform:translateX(-56%) scale(1)}}
  @keyframes recordingBreath{0%,100%{transform:translateX(-50%) scale(1);filter:brightness(1)}50%{transform:translateX(-50%) scale(1.045);filter:brightness(1.08)}}
  .recording-glow{position:absolute;inset:10px;border-radius:50%;background:radial-gradient(circle,rgba(255,255,255,.22),rgba(255,255,255,0) 68%);animation:recordingGlow 1.15s ease-in-out infinite}@keyframes recordingGlow{0%,100%{opacity:.48;transform:scale(.9)}50%{opacity:1;transform:scale(1.08)}}
  .voice-ring{position:absolute;inset:-12px;border:3px solid rgba(117,137,255,.4);border-radius:50%;animation:voiceRing 1.65s ease-out infinite;will-change:transform,opacity}.voice-ring.two{animation-delay:.52s}.voice-ring.three{animation-delay:1.04s}@keyframes voiceRing{0%{transform:scale(.78);opacity:.82}55%{opacity:.36}100%{transform:scale(1.72);opacity:0}}
  .room-agent-orb.listening .mic-icon{width:25px;height:33px;border-width:4px;border-radius:14px;z-index:2}.room-agent-orb.listening .mic-stem{width:34px;height:17px;bottom:27px;border-width:4px;border-top:0;border-radius:0 0 17px 17px;z-index:2}.room-agent-orb.listening .mic-stem::after{bottom:-12px;width:4px;height:12px}
  .mic-icon{width:18px;height:23px;border:3px solid #fff;border-radius:10px;font-size:0}.mic-stem{position:absolute;width:24px;height:12px;bottom:16px;border:3px solid #fff;border-top:0;border-radius:0 0 12px 12px}.mic-stem::after{content:"";position:absolute;left:50%;bottom:-8px;width:3px;height:8px;transform:translateX(-50%);background:#fff;border-radius:2px}
  .listening-caption{position:fixed;left:50%;bottom:calc(244px + env(safe-area-inset-bottom));z-index:59;transform:translateX(-50%);padding:8px 14px;border-radius:999px;background:rgba(30,41,73,.9);color:#fff;font-size:11px;font-weight:800;letter-spacing:.02em;white-space:nowrap;box-shadow:0 8px 24px rgba(23,33,69,.18)}
  .voice-floating-composer{position:fixed;left:50%;bottom:calc(185px + env(safe-area-inset-bottom));z-index:59;width:min(calc(100% - 30px),510px);transform:translateX(-50%);display:grid;grid-template-columns:minmax(0,1fr) auto;gap:8px;padding:8px;border:1px solid #d8e0ef;border-radius:18px;background:rgba(255,255,255,.96);backdrop-filter:blur(18px);box-shadow:0 15px 38px rgba(48,62,114,.16)}.voice-floating-composer input{min-width:0;border:0;outline:none;background:transparent;padding:0 8px;color:#33405a}.voice-floating-composer button,.drawer-composer button{border:0;border-radius:12px;background:#5669df;color:#fff;padding:0 16px;font-weight:900}.voice-floating-composer button:disabled,.drawer-composer button:disabled{opacity:.45}
  .agent-drawer-mask{position:fixed;inset:0;z-index:60;background:rgba(18,27,47,.3);backdrop-filter:blur(3px)}
  .agent-drawer{position:fixed;left:50%;top:max(58px,env(safe-area-inset-top));bottom:calc(100px + env(safe-area-inset-bottom));width:min(calc(100% - 20px),520px);transform:translateX(-50%);display:grid;grid-template-rows:auto 1fr auto;border:1px solid #dbe2ef;border-radius:25px;background:#f7f9fe;box-shadow:0 24px 60px rgba(31,44,83,.25);overflow:hidden;animation:drawerIn .22s ease-out}@keyframes drawerIn{from{opacity:0;transform:translateX(-50%) translateY(18px)}to{opacity:1;transform:translateX(-50%) translateY(0)}}
  .agent-drawer>header{display:flex;align-items:center;justify-content:space-between;padding:15px 16px 12px;border-bottom:1px solid #e6eaf2;background:rgba(255,255,255,.88)}.agent-drawer>header span{color:#7180d5;font-size:9px;font-weight:900;letter-spacing:.12em}.agent-drawer>header h2{margin:3px 0 0;font-size:18px}.agent-drawer>header button{width:36px;height:36px;border:0;border-radius:11px;background:#eef1f6;color:#667187;font-size:24px}
  .drawer-chat{overflow-y:auto;padding:14px}.drawer-chat article{width:86%;margin-bottom:10px;padding:11px 12px;border:1px solid #e6eaf2;border-radius:16px 16px 16px 5px;background:#fff}.drawer-chat article.mine{margin-left:auto;border-color:#596bda;border-radius:16px 16px 5px 16px;background:#596bda;color:#fff}.drawer-chat small{font-size:9px;opacity:.65}.drawer-chat p{margin:5px 0 0;font-size:13px;line-height:1.55}.drawer-empty{display:grid;height:100%;place-items:center;color:#929caf;font-size:12px}
  .mobile-correction-active{display:flex;align-items:center;justify-content:space-between;gap:10px;margin-bottom:12px;padding:10px 11px;border:1px solid #cfd8f6;border-radius:14px;background:#eef2ff}.mobile-correction-active>div{display:grid;gap:2px;min-width:0}.mobile-correction-active span{color:#6678d8;font-size:9px;font-weight:900}.mobile-correction-active strong{overflow:hidden;color:#33415f;font-size:11px;text-overflow:ellipsis;white-space:nowrap}.mobile-correction-active button{flex:0 0 auto;border:1px solid #c8d0e8;border-radius:10px;background:#fff;color:#66728b;padding:7px 9px;font-size:10px;font-weight:850}
  .mobile-correction-choice{display:grid;gap:8px;margin-top:9px;padding:10px;border:1px solid #d8def5;border-radius:13px;background:#f4f6ff}.mobile-correction-choice>span{color:#6272d7;font-size:10px;font-weight:900}.mobile-correction-choice>p{margin:0!important;padding:9px 10px;border-radius:10px;background:#fff;color:#263551!important;font-size:13px!important;line-height:1.6!important}.mobile-correction-choice>button{min-height:39px;border:0;border-radius:11px;background:#586bdd;color:#fff;font:inherit;font-weight:900}.mobile-correction-choice>button:disabled{opacity:.55}
  .mobile-speech-choice{display:grid;gap:8px;margin-top:9px;padding:10px;border:1px solid #dce3f7;border-radius:13px;background:linear-gradient(145deg,#f9faff,#eef2ff)}.mobile-speech-choice>span{color:#5969d5;font-size:10px;font-weight:900}.mobile-speech-choice>p{margin:0!important;padding:9px 10px;border-radius:10px;background:#fff;color:#263551!important;font-size:13px!important;line-height:1.6!important}.mobile-speech-choice>div{display:grid;grid-template-columns:1fr 1fr;gap:8px}.mobile-speech-choice button{min-height:39px;border:1px solid #bfc9f4;border-radius:11px;background:#fff;color:#5062d3;font:inherit;font-weight:900}.mobile-speech-choice button:first-child{border-color:#586bdd;background:#586bdd;color:#fff}.mobile-speech-choice button:disabled{opacity:.55}.mobile-speech-choice>small{color:#7d88a5;font-size:9px;font-weight:800;opacity:1}
  .drawer-composer{display:grid;grid-template-columns:minmax(0,1fr) auto;gap:8px;padding:10px;border-top:1px solid #e5e9f2;background:#fff}.drawer-composer input{min-width:0;min-height:45px;border:1px solid #dfe5ef;border-radius:12px;padding:0 12px;outline:none}
  @media(prefers-reduced-motion:reduce){.room-agent-orb,.voice-ring,.recording-glow,.audio-wave.active span{animation:none}}
</style>

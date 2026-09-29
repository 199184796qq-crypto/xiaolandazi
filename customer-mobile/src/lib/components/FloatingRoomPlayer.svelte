<script lang="ts">
  import { onDestroy, onMount } from 'svelte';
  import { page } from '$app/stores';
  import {
    floatingAudioState,
    closeLocalFloatingAudio,
    flushRoomCompositeAudio,
    setFloatingAudioExpanded,
    startRoomCompositeAudio,
    stopRoomCompositeAudio,
    updateFloatingAudioSession,
  } from '$lib/audioRuntime';
  import {
    getLiveRuntime,
    pauseLiveRuntime,
    resumeLiveRuntime,
    startLiveRuntime,
    stopLiveRuntime,
  } from '$lib/api';

  let busy = false;
  let error = '';
  let timer: number | undefined;
  let handleTop = 0;
  let draggingHandle = false;
  let dragMoved = false;
  let dragStartY = 0;
  let dragStartTop = 0;
  let suppressHandleClick = false;

  const HANDLE_POSITION_KEY = 'xiaolan-floating-player-top-v1';
  const HANDLE_MIN_TOP = 72;
  const PLAYER_SAFE_BOTTOM = 96;
  const PLAYER_EXPANDED_HEIGHT = 176;

  $: state = $floatingAudioState;
  $: onActiveRoom = state.roomId > 0 && $page.url.pathname === '/rooms/' + state.roomId;
  $: shouldShow = state.roomId > 0 && state.visible && !onActiveRoom;

  $: {
    if (shouldShow && timer === undefined) {
      timer = window.setInterval(() => void syncRuntime(), 1800);
    } else if (!shouldShow && timer !== undefined) {
      window.clearInterval(timer);
      timer = undefined;
    }
  }

  onMount(() => {
    const saved = Number(window.sessionStorage.getItem(HANDLE_POSITION_KEY) || 0);
    handleTop = clampHandleTop(saved > 0 ? saved : window.innerHeight * 0.34);
    const handleResize = () => {
      handleTop = clampHandleTop(handleTop || window.innerHeight * 0.34);
      persistHandleTop();
    };
    window.addEventListener('resize', handleResize);
    return () => {
      window.removeEventListener('resize', handleResize);
    };
  });

  onDestroy(() => {
    if (timer !== undefined) window.clearInterval(timer);
  });

  async function syncRuntime() {
    if (!state.roomId || !state.visible) return;
    const syncingRoomId = state.roomId;
    try {
      const runtime = await getLiveRuntime(syncingRoomId);
      if (!$floatingAudioState.visible || $floatingAudioState.roomId !== syncingRoomId) return;
      const next =
        runtime.agent_state === 'working'
          ? 'working'
          : runtime.agent_state === 'paused'
            ? 'paused'
            : 'stopped';
      updateFloatingAudioSession(syncingRoomId, state.roomName, next, true);
    } catch {}
  }

  async function play() {
    if (busy || state.state === 'working') return;
    busy = true;
    error = '';
    try {
      if (state.state === 'paused') await resumeLiveRuntime(state.roomId);
      else await startLiveRuntime(state.roomId);
      await startRoomCompositeAudio(state.roomId, state.roomName);
      updateFloatingAudioSession(state.roomId, state.roomName, 'working', true);
    } catch (err) {
      error = err instanceof Error ? err.message : '播放失败';
      await syncRuntime();
    } finally {
      busy = false;
    }
  }

  async function pause() {
    if (busy || state.state !== 'working') return;
    busy = true;
    error = '';
    try {
      await pauseLiveRuntime(state.roomId);
      flushRoomCompositeAudio();
      updateFloatingAudioSession(state.roomId, state.roomName, 'paused', true);
    } catch (err) {
      error = err instanceof Error ? err.message : '暂停失败';
    } finally {
      busy = false;
    }
  }

  async function stop() {
    if (busy || state.state === 'stopped') return;
    busy = true;
    error = '';
    try {
      await stopLiveRuntime(state.roomId);
      await stopRoomCompositeAudio();
      updateFloatingAudioSession(state.roomId, state.roomName, 'stopped', true);
    } catch (err) {
      error = err instanceof Error ? err.message : '停止失败';
    } finally {
      busy = false;
    }
  }

  async function closeLocalPlayer() {
    if (busy) return;
    busy = true;
    error = '';
    try {
      await closeLocalFloatingAudio();
    } catch (err) {
      error = err instanceof Error ? err.message : '关闭本机声音失败';
    } finally {
      busy = false;
    }
  }

  function clampHandleTop(value: number) {
    const viewportHeight = typeof window === 'undefined' ? 800 : window.innerHeight;
    const maxTop = Math.max(
      HANDLE_MIN_TOP,
      viewportHeight - PLAYER_SAFE_BOTTOM - PLAYER_EXPANDED_HEIGHT,
    );
    return Math.min(Math.max(value, HANDLE_MIN_TOP), maxTop);
  }

  function persistHandleTop() {
    if (typeof window === 'undefined' || !handleTop) return;
    window.sessionStorage.setItem(HANDLE_POSITION_KEY, String(Math.round(handleTop)));
  }

  function beginHandleDrag(event: PointerEvent) {
    if (event.pointerType === 'mouse' && event.button !== 0) return;
    draggingHandle = true;
    dragMoved = false;
    dragStartY = event.clientY;
    dragStartTop = handleTop || clampHandleTop(window.innerHeight * 0.34);
    try {
      (event.currentTarget as HTMLElement).setPointerCapture(event.pointerId);
    } catch {}
  }

  function moveHandleDrag(event: PointerEvent) {
    if (!draggingHandle) return;
    const deltaY = event.clientY - dragStartY;
    if (Math.abs(deltaY) > 5) dragMoved = true;
    handleTop = clampHandleTop(dragStartTop + deltaY);
  }

  function endHandleDrag(event: PointerEvent) {
    if (!draggingHandle) return;
    draggingHandle = false;
    if (dragMoved) {
      suppressHandleClick = true;
      window.setTimeout(() => {
        suppressHandleClick = false;
      }, 120);
    }
    persistHandleTop();
    try {
      (event.currentTarget as HTMLElement).releasePointerCapture(event.pointerId);
    } catch {}
  }

  function toggleHandle() {
    if (suppressHandleClick || dragMoved) {
      dragMoved = false;
      return;
    }
    setFloatingAudioExpanded(!state.expanded);
  }
</script>

{#if shouldShow}
  <aside
    class="floating-room-player"
    class:expanded={state.expanded}
    class:dragging={draggingHandle}
    style={'top:' + handleTop + 'px'}
  >
    <button
      class="floating-player-handle"
      type="button"
      aria-label={state.expanded ? '收起播放控制' : '展开播放控制'}
      on:pointerdown={beginHandleDrag}
      on:pointermove={moveHandleDrag}
      on:pointerup={endHandleDrag}
      on:pointercancel={endHandleDrag}
      on:click={toggleHandle}
    >
      <span class:active={state.state === 'working'}><i></i><i></i><i></i><i></i></span>
    </button>

    <section>
      <header>
        <div>
          <small>正在播放</small>
          <strong>{state.roomName || ('直播间 #' + state.roomId)}</strong>
        </div>
        <button type="button" aria-label="关闭浮动播放卡和本机声音" on:click={closeLocalPlayer}>×</button>
      </header>
      <div class="floating-player-state">
        {state.state === 'working' ? '播放中' : state.state === 'paused' ? '已暂停' : '已停止'}
      </div>
      <div class="floating-player-controls">
        <button type="button" disabled={busy || state.state === 'working'} on:click={play}><span>▶</span><b>播放</b></button>
        <button type="button" disabled={busy || state.state !== 'working'} on:click={pause}><span>Ⅱ</span><b>暂停</b></button>
        <button type="button" disabled={busy || state.state === 'stopped'} on:click={stop}><span>■</span><b>停止</b></button>
      </div>
      {#if error}<p>{error}</p>{/if}
    </section>
  </aside>
{/if}

<style>
  .floating-room-player{position:fixed;right:0;z-index:58;display:grid;grid-template-columns:42px 250px;transform:translateX(250px);transition:transform .24s ease,filter .18s ease;filter:drop-shadow(0 14px 28px rgba(35,48,89,.18))}
  .floating-room-player.expanded{transform:translateX(0)}
  .floating-room-player.dragging{transition:none;filter:drop-shadow(0 18px 34px rgba(35,48,89,.25))}
  .floating-player-handle{align-self:start;display:grid;width:42px;height:66px;place-items:center;border:1px solid #cfd9ee;border-right:0;border-radius:18px 0 0 18px;background:linear-gradient(160deg,#eef3ff,#dfe7fb);touch-action:none;cursor:ns-resize;user-select:none;-webkit-user-select:none}
  .floating-room-player.dragging .floating-player-handle{transform:scale(1.035)}
  .floating-player-handle>span{display:flex;height:24px;align-items:center;gap:2px}.floating-player-handle i{display:block;width:3px;height:8px;border-radius:3px;background:#7183d9}.floating-player-handle i:nth-child(2){height:18px}.floating-player-handle i:nth-child(3){height:13px}.floating-player-handle i:nth-child(4){height:20px}
  .floating-player-handle span.active i{animation:float-wave .7s ease-in-out infinite alternate}.floating-player-handle span.active i:nth-child(2){animation-delay:-.18s}.floating-player-handle span.active i:nth-child(3){animation-delay:-.33s}.floating-player-handle span.active i:nth-child(4){animation-delay:-.49s}
  @keyframes float-wave{from{transform:scaleY(.45)}to{transform:scaleY(1.25)}}
  .floating-room-player>section{min-height:142px;padding:13px;border:1px solid #d6deed;border-radius:0 0 0 20px;background:rgba(249,251,255,.97);backdrop-filter:blur(16px)}
  .floating-room-player header{display:flex;align-items:flex-start;justify-content:space-between;gap:8px}.floating-room-player header>div{min-width:0;display:grid;gap:2px}.floating-room-player header small{color:#8b96aa;font-size:9px;font-weight:900}.floating-room-player header strong{overflow:hidden;color:#273550;font-size:13px;white-space:nowrap;text-overflow:ellipsis}
  .floating-room-player header button{display:grid;width:28px;height:28px;place-items:center;border:0;border-radius:9px;background:#eef1f6;color:#788398;font-size:20px;line-height:1}.floating-player-state{margin-top:8px;color:#6170be;font-size:10px;font-weight:800}
  .floating-player-controls{display:grid;grid-template-columns:repeat(3,1fr);gap:7px;margin-top:11px}.floating-player-controls button{display:grid;grid-template-columns:auto auto;justify-content:center;gap:4px;min-height:38px;align-items:center;border:0;border-radius:11px;background:#edf1fb;color:#5264c7;font-size:10px}.floating-player-controls button:disabled{opacity:.38}.floating-player-controls button:last-child{background:#fdecec;color:#b64d58}
  .floating-room-player p{margin:8px 0 0;color:#b44b58;font-size:9px;line-height:1.4}
  @media(prefers-reduced-motion:reduce){.floating-room-player,.floating-player-handle span.active i{transition:none;animation:none}}
</style>

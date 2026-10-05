<script lang="ts">
  import { onMount } from 'svelte';
  import {
    activateLiveTimeCard,
    createRoom,
    getLiveDevices,
    getLiveQuotaSummary,
    getLiveTimeCards,
    getRooms,
  } from '$lib/api';
  import { session } from '$lib/session';
  import type { LiveDevice, LiveQuotaSummary, LiveTimeCardPage, LiveTimeCardSummary, Room } from '$lib/types';

  let rooms: Room[] = [];
  let devices: LiveDevice[] = [];
  let loading = true;
  let error = '';
  let greeting = '你好';
  let showAddRoom = false;
  let savingRoom = false;
  let roomName = '';
  let roomSource = '';
  let liveQuotaSummary: LiveQuotaSummary | null = null;
  let quotaLoading = true;
  let quotaError = '';
  let timeCardPackOpen = false;
  let timeCardPackLoading = false;
  let timeCardPackError = '';
  let timeCardPackNotice = '';
  let timeCardActivatingID: number | null = null;
  const timeCardPackPageSize = 4;
  let timeCardPage: LiveTimeCardPage = {
    items: [],
    page: 1,
    page_size: timeCardPackPageSize,
    total: 0,
  };

  onMount(async () => {
    const hour = new Date().getHours();
    if (hour >= 5 && hour < 11) greeting = '早上好';
    else if (hour >= 11 && hour < 13) greeting = '中午好';
    else if (hour >= 13 && hour < 18) greeting = '下午好';
    else if (hour >= 18 || hour < 1) greeting = '晚上好';
    else greeting = '夜深了';

    void loadAITime();

    try {
      const [roomResult, deviceResult] = await Promise.all([
        getRooms(),
        getLiveDevices().catch(() => []),
      ]);
      rooms = roomResult.items || [];
      devices = deviceResult || [];
    } catch (err) {
      error = err instanceof Error ? err.message : '读取首页数据失败';
    } finally {
      loading = false;
    }
  });

  $: displayName = $session.bootstrap?.actor.display_name || $session.bootstrap?.actor.username || '用户';
  $: avatarURL = String($session.bootstrap?.actor.avatar_url || '').trim();
  $: liveRooms = rooms.filter((item) => item.status === 'live' || item.status === 'running');
  $: liveRoomCount = liveRooms.length;
  $: liveAudienceTotal = liveRooms.reduce(
    (total, item) => total + Math.max(0, Number(item.online_count || 0)),
    0,
  );
  $: greetingIcon = greeting === '早上好' || greeting === '中午好' || greeting === '下午好' ? 'sun' : 'moon';
  $: timeCardTotalPages = Math.max(1, Math.ceil(timeCardPage.total / timeCardPackPageSize));

  function roomStatusLabel(room: Room) {
    if (room.status === 'live' || room.status === 'running') return '直播中';
    if (room.status === 'offline' || room.status === 'stopped') return '未直播';
    if (room.status === 'connecting') return '连接中';
    return room.status || '未连接';
  }

  function deviceConnectionLabel(device: LiveDevice) {
    if (device.connection_status === 'online' || device.connection_status === 'connected') return '已连接';
    return '离线';
  }

  function deviceWorkLabel(device: LiveDevice) {
    if (device.work_status === 'working' || device.work_status === 'running') return '工作中';
    if (device.work_status === 'paused') return '已暂停';
    return '未工作';
  }

  function openAddRoom() {
    roomName = '';
    roomSource = '';
    error = '';
    showAddRoom = true;
  }

  async function submitRoom() {
    if (!roomSource.trim() || savingRoom) return;
    savingRoom = true;
    error = '';
    try {
      await createRoom({
        platform: 'douyin',
        external_room_id: roomSource.trim(),
        name: roomName.trim(),
        collector_mode: 'auto',
      });
      const result = await getRooms();
      rooms = result.items || [];
      showAddRoom = false;
    } catch (err) {
      error = err instanceof Error ? err.message : '添加直播间失败';
    } finally {
      savingRoom = false;
    }
  }

  function closeAddRoomFromMask(event: MouseEvent) {
    if (event.target === event.currentTarget) showAddRoom = false;
  }

  function formatAITime(seconds?: number) {
    const total = Math.max(0, Math.floor(Number(seconds || 0)));
    const hours = Math.floor(total / 3600);
    const minutes = Math.floor((total % 3600) / 60);
    if (hours > 0) return `${hours}小时${minutes > 0 ? `${minutes}分` : ''}`;
    if (minutes > 0) return `${minutes}分钟`;
    return `${total}秒`;
  }

  function formatTimeCardHours(seconds: number) {
    const hours = Math.max(0, Number(seconds || 0)) / 3600;
    return Number.isInteger(hours) ? hours.toFixed(0) : hours.toFixed(2).replace(/0+$/, '').replace(/\.$/, '');
  }

  function formatTimeCardDate(value?: string) {
    if (!value) return '—';
    return new Date(value).toLocaleString('zh-CN', { hour12: false });
  }

  function timeCardCanActivate(item: LiveTimeCardSummary) {
    return item.status === 'unactivated' && item.remaining_seconds > 0;
  }

  async function loadAITime() {
    quotaLoading = true;
    quotaError = '';
    try {
      liveQuotaSummary = await getLiveQuotaSummary();
    } catch (err) {
      quotaError = err instanceof Error ? err.message : '读取 AI 时长失败';
    } finally {
      quotaLoading = false;
    }
  }

  async function loadTimeCardPack(page = timeCardPage.page || 1) {
    timeCardPackLoading = true;
    timeCardPackError = '';
    try {
      timeCardPage = await getLiveTimeCards(page, timeCardPackPageSize);
    } catch (err) {
      timeCardPackError = err instanceof Error ? err.message : '读取时长卡包失败';
    } finally {
      timeCardPackLoading = false;
    }
  }

  async function openTimeCardPack() {
    timeCardPackOpen = true;
    timeCardPackNotice = '';
    await loadTimeCardPack(1);
  }

  async function activateTimeCard(item: LiveTimeCardSummary) {
    if (!timeCardCanActivate(item) || timeCardActivatingID !== null) return;
    timeCardActivatingID = item.id;
    timeCardPackError = '';
    timeCardPackNotice = '';
    try {
      const response = await activateLiveTimeCard(item.id);
      liveQuotaSummary = { ...response.quota };
      timeCardPackNotice = '启用成功，卡内剩余时长已充入 AI 时长池。';
      const targetPage = timeCardPage.items.length === 1 && timeCardPage.page > 1
        ? timeCardPage.page - 1
        : timeCardPage.page;
      await loadTimeCardPack(targetPage);
    } catch (err) {
      timeCardPackError = err instanceof Error ? err.message : '启用时长卡失败';
    } finally {
      timeCardActivatingID = null;
    }
  }

  function changeTimeCardPackPage(page: number) {
    if (timeCardPackLoading) return;
    const target = Math.max(1, Math.min(timeCardTotalPages, page));
    if (target !== timeCardPage.page) void loadTimeCardPack(target);
  }

  function closeTimeCardPackFromMask(event: MouseEvent) {
    if (event.target === event.currentTarget) timeCardPackOpen = false;
  }
</script>

<svelte:head><title>首页</title></svelte:head>

<section class="customer-home top-space">
  <header class="customer-home-hero">
    <div class="home-hero-glow home-hero-glow-one"></div>
    <div class="home-hero-glow home-hero-glow-two"></div>
    <div class="home-hero-copy">
      <div class="home-hero-greeting">
        <span>{greeting}</span>
        <span class="home-daypart-icon" class:is-sun={greetingIcon === 'sun'} aria-hidden="true">
          {#if greetingIcon === 'sun'}
            <svg viewBox="0 0 32 32">
              <circle cx="16" cy="16" r="6"></circle>
              <path d="M16 2v4M16 26v4M2 16h4M26 16h4M6.1 6.1l2.8 2.8M23.1 23.1l2.8 2.8M25.9 6.1l-2.8 2.8M8.9 23.1l-2.8 2.8"></path>
            </svg>
          {:else}
            <svg viewBox="0 0 32 32">
              <path d="M23.8 21.7A11.6 11.6 0 0 1 10.3 8.2 10.5 10.5 0 1 0 23.8 21.7Z"></path>
              <circle cx="24.2" cy="7.7" r="2"></circle>
            </svg>
          {/if}
        </span>
      </div>
      <h1>{displayName}</h1>
      <div class="home-live-summary">
        <span class:online={liveRoomCount > 0}></span>
        <strong>{liveRoomCount}个直播中 · {liveAudienceTotal.toLocaleString()}在线</strong>
      </div>
    </div>

    <div class="home-profile-orbit" aria-label={displayName + '的账号'}>
      <span class="home-profile-spark">✦</span>
      <div class="home-profile-avatar">
        {#if avatarURL}
          <img src={avatarURL} alt={displayName + '的头像'} />
        {:else}
          <svg viewBox="0 0 48 48" aria-hidden="true">
            <circle cx="24" cy="17" r="9"></circle>
            <path d="M9 41c1.8-10 6.7-15 15-15s13.2 5 15 15"></path>
          </svg>
        {/if}
      </div>
    </div>
  </header>

  {#if error}<div class="customer-home-error">{error}</div>{/if}

  <section class="home-zone">
    <header class="home-zone-head">
      <div>
        <span>LIVE ROOMS</span>
        <h2>我的直播间</h2>
      </div>
      <b>{rooms.length} 个</b>
    </header>

    {#if loading}
      <div class="home-loading-card">正在读取直播间…</div>
    {:else if rooms.length}
      <div class="home-card-scroller" aria-label="我的直播间">
        {#each rooms as room}
          <a
            href={'/rooms/' + room.id}
            class="home-room-card"
            class:is-live={room.status === 'live' || room.status === 'running'}
            aria-label={'进入' + (room.name || ('直播间 ' + room.id))}
          >
            <header>
              <span class:online={room.status === 'live' || room.status === 'running'}></span>
              <em>{roomStatusLabel(room)}</em>
            </header>
            <div class="home-card-icon">播</div>
            {#if room.status === 'live' || room.status === 'running'}
              <div class="live-audience" aria-label="当前在线人数">
                <strong>{(room.online_count ?? 0).toLocaleString()}</strong>
                <span class="audience-person" aria-hidden="true">
                  <svg viewBox="0 0 24 24">
                    <circle cx="12" cy="7" r="4"></circle>
                    <path d="M5.5 20c.4-4.1 2.5-6.2 6.5-6.2s6.1 2.1 6.5 6.2"></path>
                  </svg>
                </span>
                <small>在线</small>
              </div>
            {/if}
            <div class="home-card-main">
              <h3>{room.name || ('直播间 ' + room.id)}</h3>
              <p>{room.device_online ? '设备已连接' : '设备未连接'}</p>
            </div>
            <footer>
              <span>房间 #{room.id}</span>
              <strong>{room.device_online ? '在线' : '待连接'}</strong>
            </footer>
          </a>
        {/each}

        <button class="home-add-card" type="button" on:click={openAddRoom}>
          <span>＋</span>
          <strong>添加直播间</strong>
          <small>添加新的抖音直播间</small>
        </button>
      </div>
    {:else}
      <div class="home-empty-center">
        <button class="home-add-card is-empty" type="button" on:click={openAddRoom}>
          <span>＋</span>
          <strong>添加直播间</strong>
          <small>添加你的第一个直播间</small>
        </button>
      </div>
    {/if}
  </section>

  <section class="home-zone home-device-zone">
    <header class="home-zone-head">
      <div>
        <span>DEVICES</span>
        <h2>我的设备</h2>
      </div>
      <b>{devices.length} 台</b>
    </header>

    {#if loading}
      <div class="home-loading-card">正在读取设备…</div>
    {:else if devices.length}
      <div class="home-card-scroller" aria-label="我的设备">
        {#each devices as device}
          <article class="home-device-card">
            <header>
              <span class:online={device.connection_status === 'online' || device.connection_status === 'connected'}></span>
              <em>{device.display_status || deviceConnectionLabel(device)}</em>
            </header>
            <div class="home-card-icon device">盒</div>
            <div class="home-card-main">
              <h3>{device.device_name || '小蓝直播助手'}</h3>
              <p>{device.sku_code || '直播搭子设备'}</p>
            </div>
            <footer>
              <span>{device.room_id ? ('已绑房间 #' + device.room_id) : '暂未绑定直播间'}</span>
              <a href="/devices">管理设备</a>
            </footer>
          </article>
        {/each}

        <a class="home-add-card" href="/devices">
          <span>＋</span>
          <strong>添加设备</strong>
          <small>输入6位绑定码添加设备</small>
        </a>
      </div>
    {:else}
      <div class="home-empty-center">
        <a class="home-add-card is-empty" href="/devices">
          <span>＋</span>
          <strong>添加设备</strong>
          <small>添加你的第一台设备</small>
        </a>
      </div>
    {/if}
  </section>

  <section class="home-zone home-ai-zone">
    <header class="home-zone-head">
      <div>
        <span>AI TIME</span>
        <h2>AI 时长</h2>
      </div>
      <b>实时余额</b>
    </header>

    <div class="home-ai-card">
      <div class="home-ai-balance">
        <span class="home-ai-icon" aria-hidden="true">
          <svg viewBox="0 0 24 24">
            <circle cx="12" cy="12" r="8.5"></circle>
            <path d="M12 7.5v5l3.5 2"></path>
          </svg>
        </span>
        <div>
          <small>当前剩余总时长</small>
          {#if quotaLoading}
            <strong class="is-loading">读取中…</strong>
          {:else if liveQuotaSummary}
            <strong>{formatAITime(liveQuotaSummary.active_seconds)}</strong>
          {:else}
            <strong>暂不可用</strong>
          {/if}
        </div>
      </div>

      <button class="home-time-card-entry" type="button" on:click={openTimeCardPack}>
        <span class="time-card-stack" aria-hidden="true"><i></i><i></i><i></i></span>
        <span>
          <strong>时长卡包</strong>
          <small>{liveQuotaSummary?.reserve_time_card_count || 0} 张待启用</small>
        </span>
        <b aria-hidden="true">›</b>
      </button>
    </div>
    {#if quotaError}
      <button class="home-ai-error" type="button" on:click={loadAITime}>{quotaError}，点击重试</button>
    {/if}
  </section>
</section>

{#if showAddRoom}
  <div class="mobile-sheet-mask" role="presentation" on:click={closeAddRoomFromMask}>
    <form class="mobile-sheet" on:submit|preventDefault={submitRoom}>
      <header>
        <div>
          <span>ADD LIVE ROOM</span>
          <h2>添加直播间</h2>
        </div>
        <button type="button" on:click={() => (showAddRoom = false)}>×</button>
      </header>
      <label>
        <span>直播间名称</span>
        <input bind:value={roomName} maxlength="80" placeholder="例如：菜籽油直播间" />
      </label>
      <label>
        <span>抖音分享链接 / 房间号</span>
        <input bind:value={roomSource} required placeholder="粘贴分享链接或输入房间号" />
      </label>
      <button class="sheet-primary" type="submit" disabled={savingRoom || !roomSource.trim()}>
        {savingRoom ? '添加中…' : '确认添加'}
      </button>
    </form>
  </div>
{/if}

{#if timeCardPackOpen}
  <div class="mobile-sheet-mask" role="presentation" on:click={closeTimeCardPackFromMask}>
    <div class="mobile-sheet time-card-sheet" role="dialog" aria-modal="true" aria-label="时长卡包">
      <header>
        <div>
          <span>TIME CARD WALLET</span>
          <h2>时长卡包</h2>
        </div>
        <button type="button" aria-label="关闭时长卡包" on:click={() => (timeCardPackOpen = false)}>×</button>
      </header>

      <p class="time-card-tip">选择一张卡启用，卡内时长会立即充入当前 AI 时长池，有效期从启用时开始计算。</p>
      {#if timeCardPackNotice}<p class="time-card-notice" aria-live="polite">{timeCardPackNotice}</p>{/if}
      {#if timeCardPackError}<p class="time-card-error" aria-live="polite">{timeCardPackError}</p>{/if}

      {#if timeCardPackLoading && !timeCardPage.items.length}
        <div class="time-card-empty">正在打开卡包…</div>
      {:else if !timeCardPage.items.length}
        <div class="time-card-empty">
          <strong>暂无未使用时长卡</strong>
          <span>可前往商城购买新的时长卡</span>
          <a href="/shop">去商城看看</a>
        </div>
      {:else}
        <div class="time-card-list" aria-busy={timeCardPackLoading}>
          {#each timeCardPage.items as item}
            <article class="time-card-item">
              <div class="time-card-face">
                <span>{item.product_name}</span>
                <div><strong>{formatTimeCardHours(item.original_seconds)}</strong><small>小时</small></div>
                <em>{item.asset_no}</em>
              </div>
              <div class="time-card-detail">
                <dl>
                  <div><dt>购买时间</dt><dd>{formatTimeCardDate(item.purchased_at)}</dd></div>
                  <div><dt>使用有效期</dt><dd>启用后 {item.validity_days} 天</dd></div>
                  {#if item.activation_deadline_at}
                    <div><dt>最晚启用</dt><dd>{formatTimeCardDate(item.activation_deadline_at)}</dd></div>
                  {/if}
                </dl>
                {#if timeCardCanActivate(item)}
                  <button type="button" disabled={timeCardActivatingID !== null} on:click={() => activateTimeCard(item)}>
                    {timeCardActivatingID === item.id ? '正在启用…' : '使用这张'}
                  </button>
                {/if}
              </div>
            </article>
          {/each}
        </div>
      {/if}

      {#if timeCardPage.total > timeCardPackPageSize}
        <footer class="time-card-pagination">
          <button type="button" disabled={timeCardPage.page <= 1 || timeCardPackLoading} on:click={() => changeTimeCardPackPage(timeCardPage.page - 1)}>←</button>
          <strong>{timeCardPage.page} / {timeCardTotalPages}</strong>
          <button type="button" disabled={timeCardPage.page >= timeCardTotalPages || timeCardPackLoading} on:click={() => changeTimeCardPackPage(timeCardPage.page + 1)}>→</button>
        </footer>
      {/if}
    </div>
  </div>
{/if}

<style>
  .customer-home{padding:0 16px 118px}
  .customer-home-hero{position:relative;min-height:218px;overflow:hidden;margin:0 -5px 8px;padding:27px 26px 26px;border:1px solid rgba(208,220,247,.92);border-radius:30px;background:linear-gradient(148deg,#f5f8ff 0%,#e6efff 45%,#dbe9ff 100%);box-shadow:0 18px 48px rgba(60,79,143,.12),inset 0 1px 0 rgba(255,255,255,.9)}
  .customer-home-hero::after{content:"";position:absolute;right:-52px;top:-82px;width:320px;height:220px;border-radius:50%;background:radial-gradient(ellipse at center,rgba(255,255,255,.76) 0%,rgba(255,255,255,.22) 55%,rgba(255,255,255,0) 73%);transform:rotate(-14deg);pointer-events:none}
  .home-hero-glow{position:absolute;border-radius:50%;pointer-events:none}
  .home-hero-glow-one{right:84px;top:-128px;width:310px;height:310px;background:rgba(255,255,255,.16)}
  .home-hero-glow-two{left:-78px;bottom:-126px;width:250px;height:250px;background:radial-gradient(circle,rgba(255,255,255,.52),rgba(255,255,255,0) 70%)}
  .home-hero-copy{position:relative;z-index:2;width:62%}
  .home-hero-greeting{display:flex;align-items:center;gap:9px;color:#75839d;font-size:13px;font-weight:900}
  .home-daypart-icon{display:grid;width:27px;height:27px;place-items:center;color:#8294ef}
  .home-daypart-icon svg{width:100%;height:100%;overflow:visible}
  .home-daypart-icon svg *{fill:currentColor;stroke:currentColor;stroke-width:1.7;stroke-linecap:round}
  .home-daypart-icon:not(.is-sun) svg path{stroke:none}
  .home-daypart-icon:not(.is-sun) svg circle{stroke:none;opacity:.45}
  .home-hero-copy h1{margin:9px 0 16px;color:#1d2d4a;font-size:36px;letter-spacing:-1.8px;line-height:1.08;font-weight:950}
  .home-live-summary{display:inline-flex;align-items:center;gap:9px;max-width:100%;padding:9px 13px;border:1px solid rgba(255,255,255,.76);border-radius:999px;background:rgba(255,255,255,.64);box-shadow:0 7px 18px rgba(74,93,159,.08);backdrop-filter:blur(8px);color:#66728a}
  .home-live-summary>span{flex:0 0 auto;width:10px;height:10px;border-radius:50%;background:#c0c8d6}
  .home-live-summary>span.online{background:#24bc82;box-shadow:0 0 0 6px rgba(36,188,130,.12)}
  .home-live-summary strong{overflow:hidden;font-size:12px;white-space:nowrap;text-overflow:ellipsis}
  .home-profile-orbit{position:absolute;right:25px;top:42px;z-index:2;display:grid;width:78px;height:78px;place-items:center;border:1px solid rgba(255,255,255,.76);border-radius:50%;background:rgba(255,255,255,.10);box-shadow:0 8px 22px rgba(75,92,166,.08),inset 0 0 14px rgba(255,255,255,.34)}
  .home-profile-orbit::before,.home-profile-orbit::after{content:"";position:absolute;inset:-2px;border:2px solid rgba(255,255,255,.62);border-radius:50%;pointer-events:none;animation:profile-halo 2.8s ease-out infinite}
  .home-profile-orbit::after{animation-delay:1.4s}
  .home-profile-avatar{display:grid;width:64px;height:64px;place-items:center;border-radius:50%;background:radial-gradient(circle at 38% 28%,#a7b7ff 0%,#7788f0 38%,#5266d6 100%);box-shadow:0 8px 20px rgba(80,99,207,.20);animation:profile-breathe 2.8s ease-in-out infinite}
  .home-profile-avatar img{width:100%;height:100%;display:block;object-fit:cover;border-radius:50%}
  .home-profile-avatar svg{width:43px;height:43px;fill:rgba(255,255,255,.92)}
  .home-profile-spark{position:absolute;right:-10px;top:-2px;color:#9aaaff;font-size:22px;text-shadow:0 4px 12px rgba(115,132,230,.28)}
  @keyframes profile-halo{
    0%{transform:scale(.96);opacity:.52}
    58%{opacity:.18}
    100%{transform:scale(1.36);opacity:0}
  }
  @keyframes profile-breathe{
    0%,100%{transform:scale(1);box-shadow:0 8px 20px rgba(80,99,207,.18)}
    50%{transform:scale(1.045);box-shadow:0 11px 26px rgba(80,99,207,.28)}
  }
  .customer-home-error{margin:0 2px 14px;padding:11px 13px;border-radius:13px;background:#fff0f1;color:#b74e58;font-size:13px}
  .home-zone{padding:19px 0 8px}
  .home-device-zone{padding-top:24px}
  .home-zone-head{display:flex;align-items:flex-end;justify-content:space-between;padding:0 3px 12px}
  .home-zone-head>div{display:grid;gap:3px}
  .home-zone-head span{color:#8993a7;font-size:10px;font-weight:900;letter-spacing:.13em}
  .home-zone-head h2{margin:0;color:#24324c;font-size:20px}
  .home-zone-head b{padding:5px 9px;border-radius:999px;background:#edf1ff;color:#5a6bd2;font-size:11px}
  .home-card-scroller{display:flex;gap:12px;overflow-x:auto;scroll-snap-type:x mandatory;padding:3px 2px 12px;overscroll-behavior-inline:contain;scrollbar-width:none}
  .home-card-scroller::-webkit-scrollbar{display:none}
  .home-room-card,.home-device-card,.home-add-card{flex:0 0 82%;min-height:226px;scroll-snap-align:center;border:1px solid #d9e0ef;border-radius:24px;box-shadow:0 14px 34px rgba(53,70,126,.10)}
  .home-room-card{background:linear-gradient(145deg,#eef3ff 0%,#e8eefb 52%,#f3f6fc 100%);position:relative;overflow:hidden}
  .home-room-card::after{content:"";position:absolute;right:-58px;top:-72px;width:170px;height:170px;border-radius:50%;background:radial-gradient(circle,rgba(108,125,225,.18) 0%,rgba(108,125,225,0) 70%);pointer-events:none}
  .home-room-card.is-live{border-color:#bac7f2;background:linear-gradient(145deg,#e6edff 0%,#dfe8ff 48%,#edf4ff 100%);box-shadow:0 16px 38px rgba(77,96,184,.15),inset 0 0 0 1px rgba(255,255,255,.52)}
  .home-room-card.is-live::before{content:"";position:absolute;left:0;top:0;width:100%;height:3px;background:linear-gradient(90deg,#5e72e4,#7e93ff,#9fb4ff)}
  .home-device-card{background:linear-gradient(145deg,#edf5f2 0%,#e5f0ec 52%,#f3f7f5 100%);border-color:#d7e7e0;box-shadow:0 14px 34px rgba(58,104,88,.08)}
  .home-add-card{background:linear-gradient(145deg,#eef2ff,#e7ecff);border-color:#aebced}
  .home-room-card,.home-device-card{display:grid;grid-template-rows:auto auto 1fr auto;padding:17px}
  .home-room-card header,.home-device-card header{display:flex;align-items:center;gap:8px}
  .home-room-card header span,.home-device-card header span{width:9px;height:9px;border-radius:50%;background:#c0c8d7}
  .home-room-card header span.online,.home-device-card header span.online{background:#24b77c;box-shadow:0 0 0 5px rgba(36,183,124,.1)}
  .home-room-card header em,.home-device-card header em{color:#7b869b;font-size:11px;font-style:normal;font-weight:800}
  .home-card-icon{display:grid;width:52px;height:52px;margin-top:22px;place-items:center;border-radius:17px;background:linear-gradient(145deg,#e6eaff,#d8e0ff);color:#5368da;font-size:20px;font-weight:950}
  .home-card-icon.device{background:linear-gradient(145deg,#eaf7f2,#ddf1e8);color:#2f9871}
  .live-audience{position:absolute;right:18px;top:72px;z-index:2;display:grid;grid-template-columns:auto 18px;grid-template-rows:auto auto;column-gap:4px;align-items:end;min-width:92px;padding:10px 12px 9px;border:1px solid rgba(112,129,218,.20);border-radius:18px;background:rgba(255,255,255,.48);backdrop-filter:blur(8px);box-shadow:0 8px 20px rgba(68,86,165,.08)}
  .live-audience strong{grid-column:1;grid-row:1;color:#4559c9;font-size:34px;line-height:.92;letter-spacing:-1.2px;font-weight:950}
  .audience-person{grid-column:2;grid-row:1;align-self:end;width:16px;height:16px;color:#6f80dc;transform:translateY(-1px)}
  .audience-person svg{display:block;width:100%;height:100%;fill:currentColor;stroke:none}
  .live-audience small{grid-column:1 / span 2;grid-row:2;margin-top:5px;color:#7c88a2;font-size:10px;font-weight:850;letter-spacing:.04em}
  .home-card-main{align-self:end;margin-top:18px}
  .home-card-main h3{margin:0;color:#26334d;font-size:19px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
  .home-card-main p{margin:6px 0 0;color:#8b95a8;font-size:12px}
  .home-room-card footer,.home-device-card footer{display:flex;align-items:center;justify-content:space-between;gap:12px;margin-top:17px;padding-top:13px;border-top:1px solid #ebeff6;color:#929bad;font-size:11px}
  .home-room-card footer strong,.home-device-card footer a{color:#596ad0;font-size:11px}
  .home-add-card{display:grid;place-items:center;align-content:center;gap:7px;padding:22px;border:1.5px dashed #b9c3e6;background:linear-gradient(145deg,#f8f9ff,#f0f3ff);color:#5a6cd3;text-align:center}
  .home-add-card span{display:grid;width:52px;height:52px;place-items:center;border-radius:50%;background:#5b6ddd;color:#fff;font-size:29px;font-weight:400;box-shadow:0 9px 22px rgba(82,100,210,.24)}
  .home-add-card strong{margin-top:7px;font-size:17px}
  .home-add-card small{color:#8d97ab;font-size:11px}
  .home-empty-center{display:grid;place-items:center;min-height:238px}
  .home-add-card.is-empty{width:min(82%,340px);min-height:218px}
  .home-loading-card{display:grid;min-height:218px;place-items:center;border:1px solid #e7ebf3;border-radius:24px;background:#fff;color:#8b95a8;font-size:13px}
  .home-ai-zone{padding-top:24px}
  .home-ai-card{overflow:hidden;border:1px solid #d7dcf7;border-radius:25px;background:linear-gradient(145deg,#f1f0ff 0%,#e8eaff 48%,#f4f7ff 100%);box-shadow:0 14px 34px rgba(74,76,155,.11)}
  .home-ai-balance{position:relative;display:flex;align-items:center;gap:15px;min-height:132px;padding:22px;background:radial-gradient(circle at 92% 8%,rgba(140,123,240,.20),transparent 42%)}
  .home-ai-balance::after{content:"AI";position:absolute;right:20px;top:12px;color:rgba(91,93,195,.08);font-size:66px;font-weight:950;letter-spacing:-5px}
  .home-ai-icon{z-index:1;display:grid;flex:0 0 auto;width:54px;height:54px;place-items:center;border-radius:18px;background:linear-gradient(145deg,#7372df,#555cca);color:#fff;box-shadow:0 10px 24px rgba(83,84,193,.24)}
  .home-ai-icon svg{width:29px;height:29px;fill:none;stroke:currentColor;stroke-width:1.9;stroke-linecap:round;stroke-linejoin:round}
  .home-ai-balance>div{z-index:1;display:grid;gap:5px}
  .home-ai-balance small{color:#7f83a0;font-size:12px;font-weight:800}
  .home-ai-balance strong{color:#31355f;font-size:29px;letter-spacing:-1px;line-height:1.15}
  .home-ai-balance strong.is-loading{font-size:20px;color:#777c9d}
  .home-time-card-entry{display:flex;width:100%;align-items:center;gap:13px;padding:15px 18px;border:0;border-top:1px solid rgba(126,128,196,.14);background:rgba(255,255,255,.54);color:#2f3657;text-align:left}
  .home-time-card-entry>span:nth-child(2){display:grid;flex:1;gap:3px}
  .home-time-card-entry strong{font-size:15px}
  .home-time-card-entry small{color:#898fa8;font-size:11px}
  .home-time-card-entry>b{color:#6871ce;font-size:27px;font-weight:500}
  .time-card-stack{position:relative;width:38px;height:34px;flex:0 0 38px}
  .time-card-stack i{position:absolute;left:3px;top:7px;width:31px;height:21px;border-radius:6px;background:#8d91ea;box-shadow:0 4px 8px rgba(78,81,176,.15);transform:rotate(-8deg)}
  .time-card-stack i:nth-child(2){background:#7379dc;transform:rotate(0)}
  .time-card-stack i:nth-child(3){background:linear-gradient(135deg,#646bd3,#9197f1);transform:translate(3px,3px) rotate(7deg)}
  .home-ai-error{width:100%;margin-top:8px;padding:9px 12px;border:0;border-radius:12px;background:#fff0f1;color:#b74e58;font-size:12px}
  .mobile-sheet-mask{position:fixed;inset:0;z-index:80;display:flex;align-items:flex-end;justify-content:center;background:rgba(17,25,40,.34);backdrop-filter:blur(4px)}
  .mobile-sheet{width:min(100%,540px);display:grid;gap:15px;padding:18px 18px calc(20px + env(safe-area-inset-bottom));border-radius:25px 25px 0 0;background:#fff;box-shadow:0 -18px 50px rgba(26,38,74,.18)}
  .mobile-sheet header{display:flex;align-items:flex-start;justify-content:space-between;gap:12px}
  .mobile-sheet header span{color:#7786d9;font-size:10px;font-weight:900;letter-spacing:.12em}
  .mobile-sheet header h2{margin:3px 0 0;color:#26334d;font-size:21px}
  .mobile-sheet header button{width:36px;height:36px;border:0;border-radius:11px;background:#f2f4f8;color:#657087;font-size:25px;line-height:1}
  .mobile-sheet label{display:grid;gap:7px}
  .mobile-sheet label span{color:#69758a;font-size:12px;font-weight:850}
  .mobile-sheet input{width:100%;min-height:48px;padding:0 13px;border:1px solid #dde3ed;border-radius:13px;background:#fbfcff;outline:none}
  .mobile-sheet input:focus{border-color:#8291e4;box-shadow:0 0 0 3px rgba(93,111,216,.08)}
  .sheet-primary{min-height:50px;border:0;border-radius:14px;background:#596bdc;color:#fff;font-weight:900;box-shadow:0 12px 24px rgba(80,99,210,.22)}
  .sheet-primary:disabled{opacity:.48}
  .time-card-sheet{max-height:min(88vh,760px);overflow-y:auto;align-content:start}
  .time-card-tip{margin:0;padding:11px 12px;border-radius:12px;background:#f3f4ff;color:#727a9b;font-size:11px;line-height:1.6}
  .time-card-notice,.time-card-error{margin:0;padding:10px 12px;border-radius:12px;font-size:12px;font-weight:750}
  .time-card-notice{background:#eaf8f1;color:#25815e}
  .time-card-error{background:#fff0f1;color:#b74e58}
  .time-card-empty{display:grid;min-height:210px;place-items:center;align-content:center;gap:8px;border:1px dashed #dce1ef;border-radius:18px;background:#fafbff;color:#8a93a6;text-align:center;font-size:12px}
  .time-card-empty strong{color:#4b5570;font-size:15px}
  .time-card-empty a{margin-top:5px;padding:8px 14px;border-radius:999px;background:#626dd7;color:#fff;font-weight:850}
  .time-card-list{display:grid;gap:12px;transition:opacity .2s}
  .time-card-list[aria-busy="true"]{opacity:.6;pointer-events:none}
  .time-card-item{display:grid;grid-template-columns:116px minmax(0,1fr);gap:13px;padding:12px;border:1px solid #e0e3f1;border-radius:19px;background:#fbfbff;box-shadow:0 8px 22px rgba(70,76,139,.07)}
  .time-card-face{position:relative;display:flex;min-height:142px;overflow:hidden;flex-direction:column;justify-content:space-between;padding:13px 11px;border-radius:14px;background:linear-gradient(145deg,#686bd2,#9097ef);color:#fff;box-shadow:0 8px 18px rgba(83,87,190,.2)}
  .time-card-face::after{content:"";position:absolute;right:-27px;top:-35px;width:90px;height:90px;border:16px solid rgba(255,255,255,.1);border-radius:50%}
  .time-card-face>span{z-index:1;overflow:hidden;font-size:10px;font-weight:850;white-space:nowrap;text-overflow:ellipsis}
  .time-card-face>div{z-index:1;display:flex;align-items:flex-end;gap:3px}
  .time-card-face strong{font-size:32px;line-height:.9;letter-spacing:-1px}
  .time-card-face small{font-size:10px}
  .time-card-face em{z-index:1;overflow:hidden;opacity:.72;font-size:8px;font-style:normal;white-space:nowrap;text-overflow:ellipsis}
  .time-card-detail{display:flex;min-width:0;flex-direction:column;justify-content:space-between;gap:10px}
  .time-card-detail dl{display:grid;gap:7px;margin:0}
  .time-card-detail dl div{display:grid;gap:2px}
  .time-card-detail dt{color:#9299aa;font-size:9px}
  .time-card-detail dd{overflow:hidden;margin:0;color:#505971;font-size:10px;white-space:nowrap;text-overflow:ellipsis}
  .time-card-detail button{min-height:36px;border:0;border-radius:11px;background:#616bd5;color:#fff;font-size:12px;font-weight:900;box-shadow:0 8px 16px rgba(82,91,196,.18)}
  .time-card-detail button:disabled{opacity:.5}
  .time-card-pagination{display:flex;align-items:center;justify-content:center;gap:18px;padding-top:2px}
  .time-card-pagination button{width:38px;height:36px;border:1px solid #dce1ef;border-radius:11px;background:#f8f9fd;color:#5964c9;font-size:17px}
  .time-card-pagination button:disabled{opacity:.38}
  .time-card-pagination strong{color:#737b91;font-size:11px}
  @media(max-width:390px){
    .customer-home-hero{min-height:206px;padding:24px 21px}
    .home-hero-copy{width:66%}
    .home-hero-copy h1{font-size:32px}
    .home-profile-orbit{right:18px;top:40px;width:72px;height:72px}
    .home-profile-avatar{width:60px;height:60px}
    .home-live-summary{padding:8px 11px}.home-live-summary strong{font-size:11px}
  }
  @media (prefers-reduced-motion:reduce){.home-profile-avatar,.home-profile-orbit::before,.home-profile-orbit::after{animation:none}}
</style>

<script lang="ts">
  import { onMount } from 'svelte';
  import { createRoom, getLiveDevices, getRooms } from '$lib/api';
  import { session } from '$lib/session';
  import type { LiveDevice, Room } from '$lib/types';

  let rooms: Room[] = [];
  let devices: LiveDevice[] = [];
  let loading = true;
  let error = '';
  let greeting = '你好';
  let showAddRoom = false;
  let savingRoom = false;
  let roomName = '';
  let roomSource = '';

  onMount(async () => {
    const hour = new Date().getHours();
    if (hour >= 5 && hour < 11) greeting = '早上好';
    else if (hour >= 11 && hour < 13) greeting = '中午好';
    else if (hour >= 13 && hour < 18) greeting = '下午好';
    else if (hour >= 18 || hour < 1) greeting = '晚上好';
    else greeting = '夜深了';

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
              <em>{deviceConnectionLabel(device)}</em>
            </header>
            <div class="home-card-icon device">盒</div>
            <div class="home-card-main">
              <h3>{device.sn || ('设备 ' + device.id)}</h3>
              <p>{device.sku_code || '直播搭子设备'}</p>
            </div>
            <footer>
              <span>{device.room_id ? ('已绑房间 #' + device.room_id) : '暂未绑定直播间'}</span>
              <strong>{deviceWorkLabel(device)}</strong>
            </footer>
          </article>
        {/each}

        <a class="home-add-card" href="/shop">
          <span>＋</span>
          <strong>添加设备</strong>
          <small>添加或购买直播设备</small>
        </a>
      </div>
    {:else}
      <div class="home-empty-center">
        <a class="home-add-card is-empty" href="/shop">
          <span>＋</span>
          <strong>添加设备</strong>
          <small>添加你的第一台设备</small>
        </a>
      </div>
    {/if}
  </section>
</section>

<a class="home-agent-fab" href="/agent" aria-label="打开智能体">✦</a>

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
  .home-room-card footer strong,.home-device-card footer strong{color:#596ad0;font-size:11px}
  .home-add-card{display:grid;place-items:center;align-content:center;gap:7px;padding:22px;border:1.5px dashed #b9c3e6;background:linear-gradient(145deg,#f8f9ff,#f0f3ff);color:#5a6cd3;text-align:center}
  .home-add-card span{display:grid;width:52px;height:52px;place-items:center;border-radius:50%;background:#5b6ddd;color:#fff;font-size:29px;font-weight:400;box-shadow:0 9px 22px rgba(82,100,210,.24)}
  .home-add-card strong{margin-top:7px;font-size:17px}
  .home-add-card small{color:#8d97ab;font-size:11px}
  .home-empty-center{display:grid;place-items:center;min-height:238px}
  .home-add-card.is-empty{width:min(82%,340px);min-height:218px}
  .home-loading-card{display:grid;min-height:218px;place-items:center;border:1px solid #e7ebf3;border-radius:24px;background:#fff;color:#8b95a8;font-size:13px}
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
  .home-agent-fab{position:fixed;left:50%;bottom:calc(76px + env(safe-area-inset-bottom));z-index:42;display:grid;width:66px;height:66px;place-items:center;transform:translateX(-50%) translateY(0) scale(1);border:2px solid rgba(255,255,255,.78);border-radius:50%;color:#fff;background:radial-gradient(circle at 35% 30%,#a9baff 0,#7587f5 34%,#4c5fd7 76%,#3745a4 100%);box-shadow:0 12px 30px rgba(76,95,215,.34),0 0 0 0 rgba(92,111,226,.16),inset 0 0 0 1px rgba(255,255,255,.45);font-size:28px;will-change:transform,box-shadow;animation:agent-breathe-float 3.2s ease-in-out infinite}
  @keyframes agent-breathe-float{
    0%,100%{transform:translateX(-50%) translateY(0) scale(1);box-shadow:0 12px 30px rgba(76,95,215,.34),0 0 0 0 rgba(92,111,226,.16),inset 0 0 0 1px rgba(255,255,255,.45)}
    45%{transform:translateX(-50%) translateY(-6px) scale(1.045);box-shadow:0 18px 34px rgba(76,95,215,.4),0 0 0 10px rgba(92,111,226,.05),inset 0 0 0 1px rgba(255,255,255,.55)}
    72%{transform:translateX(-50%) translateY(-2px) scale(1.015);box-shadow:0 14px 31px rgba(76,95,215,.36),0 0 0 5px rgba(92,111,226,.08),inset 0 0 0 1px rgba(255,255,255,.5)}
  }
  @media(max-width:390px){
    .customer-home-hero{min-height:206px;padding:24px 21px}
    .home-hero-copy{width:66%}
    .home-hero-copy h1{font-size:32px}
    .home-profile-orbit{right:18px;top:40px;width:72px;height:72px}
    .home-profile-avatar{width:60px;height:60px}
    .home-live-summary{padding:8px 11px}.home-live-summary strong{font-size:11px}
  }
  @media (prefers-reduced-motion:reduce){.home-agent-fab,.home-profile-avatar,.home-profile-orbit::before,.home-profile-orbit::after{animation:none}}
</style>

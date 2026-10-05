<script lang="ts">
  import { onMount } from 'svelte';
  import { getLiveDevices, getRooms, getDefaultDeviceName, claimDevice, renameDevice, bindDeviceRoom, unbindDeviceRoom, getDeviceAddressing, saveDeviceAddressing } from '$lib/api';
  import type { DeviceAddressingMode } from '$lib/api';
  import type { LiveDevice, Room } from '$lib/types';

  let devices: LiveDevice[] = [];
  let rooms: Room[] = [];
  let loading = true;
  let error = '';
  let message = '';
  let adding = false;
  let saving = false;
  let code = '';
  let name = '';
  let selected: LiveDevice | null = null;
  let roomId = '';
  let bindingRole: 'primary' | 'listener' = 'primary';
  let addressingMode: DeviceAddressingMode = 'auto';
  let addressingLoaded = false;
  let timer: ReturnType<typeof setInterval>;

  async function load() {
    const [d, r] = await Promise.all([getLiveDevices(), getRooms()]);
    devices = d; rooms = r.items;
    if (selected) selected = devices.find((d) => d.id === selected?.id) || null;
  }
  onMount(() => {
    void load().catch((e) => { error = e instanceof Error ? e.message : '读取设备失败'; }).finally(() => { loading = false; });
    timer = setInterval(() => { if (!saving) void load().catch(() => undefined); }, 10000);
    return () => clearInterval(timer);
  });
  async function openAdd() {
    if (saving) return;
    error = ''; message = ''; code = ''; selected = null;
    try { name = (await getDefaultDeviceName()).device_name; adding = true; }
    catch (e) { error = e instanceof Error ? e.message : '读取设备名称失败'; }
  }
  async function submitClaim() {
    if (saving || !/^\d{6}$/.test(code) || !name.trim()) return;
    saving = true; error = '';
    try { const d = await claimDevice(code, name.trim()); adding = false; await load(); selectDevice(d); message = '设备已添加，请选择直播间'; }
    catch (e) { error = e instanceof Error ? e.message : '添加设备失败'; }
    finally { saving = false; }
  }
  async function selectDevice(d: LiveDevice) {
    selected = d; name = d.device_name; roomId = String(d.room_id || ''); bindingRole = d.binding_role === 'listener' ? 'listener' : 'primary'; error = ''; addressingLoaded = false;
    try { const preference = await getDeviceAddressing(d.id); if (selected?.id === d.id) { addressingMode = preference.mode; addressingLoaded = true; } }
    catch (e) { if (selected?.id === d.id) error = e instanceof Error ? e.message : '读取回应称呼失败'; }
  }
  async function saveAddressing() {
    if (!selected || saving || !addressingLoaded) return;
    const id = selected.id; saving = true; error = ''; message = '';
    try { const preference = await saveDeviceAddressing(id, addressingMode); if (selected?.id === id) addressingMode = preference.mode; message = '回应称呼已保存，下次语音生效'; }
    catch (e) { error = e instanceof Error ? e.message : '保存回应称呼失败'; }
    finally { saving = false; }
  }
  async function operate(action: 'rename' | 'bind' | 'unbind') {
    if (!selected || saving) return;
    saving = true; error = ''; message = '';
    try {
      if (action === 'rename') await renameDevice(selected.id, name.trim());
      if (action === 'bind') {
        const occupied = roomPrimary(Number(roomId), selected.id);
        if (bindingRole === 'primary' && occupied && selected.room_id === Number(roomId) && selected.binding_role === 'listener' && !confirm('切换后“' + occupied.device_name + '”将变为监听设备，当前设备成为主设备。继续吗？')) return;
        const updated = await bindDeviceRoom(selected.id, Number(roomId), bindingRole);
        bindingRole = updated.binding_role === 'listener' ? 'listener' : 'primary';
      }
      if (action === 'unbind') { if (!confirm('解除直播间绑定后，设备仍保留在你的账号下。继续吗？')) return; await unbindDeviceRoom(selected.id); roomId = ''; }
      await load(); message = action === 'rename' ? '设备名称已保存' : action === 'unbind' ? '已解除直播间绑定' : bindingRole === 'listener' ? '已作为监听设备接入' : '已设为主设备';
    } catch (e) { error = e instanceof Error ? e.message : '设备操作失败'; }
    finally { saving = false; }
  }
  function roomName(id?: number) { return rooms.find((r) => r.id === id)?.name || (id ? '直播间 ' + id : '未绑定直播间'); }
  function roomPrimary(id: number, exceptDeviceId = 0) { return devices.find((d) => d.id !== exceptDeviceId && d.room_id === id && d.binding_role === 'primary'); }
  function selectRoom() {
    if (!selected || !roomId) return;
    if (selected.room_id === Number(roomId)) bindingRole = selected.binding_role === 'listener' ? 'listener' : 'primary';
    else bindingRole = roomPrimary(Number(roomId), selected.id) ? 'listener' : 'primary';
  }
  function primaryDisabled() { return Boolean(selected && roomPrimary(Number(roomId), selected.id) && !(selected.room_id === Number(roomId) && selected.binding_role === 'listener')); }
  function listenerDisabled() { return !selected || !roomPrimary(Number(roomId), selected.id); }
</script>

<svelte:head><title>我的设备</title></svelte:head>
<section class="devices-page top-space">
  <header><div><a href="/">返回首页</a><h1>我的设备</h1><p>添加设备，再选择它要服务的直播间。</p></div><button class="primary" on:click={openAdd} disabled={saving}>添加设备</button></header>
  {#if error}<p class="notice error" role="alert">{error}</p>{/if}
  {#if message}<p class="notice success" role="status">{message}</p>{/if}
  {#if adding}
    <form class="panel" on:submit|preventDefault={submitClaim}>
      <h2>添加设备</h2>
      <label>6位设备绑定码<input bind:value={code} type="text" inputmode="numeric" pattern={'[0-9]{6}'} maxlength="6" autocomplete="off" required placeholder="查看设备屏幕" on:input={() => code = code.replace(/\D/g, '')} /></label>
      <label>设备名称<input bind:value={name} maxlength="32" required /></label>
      <div class="actions"><button class="primary" disabled={saving || !/^\d{6}$/.test(code) || !name.trim()}>{saving ? '添加中…' : '确认添加'}</button><button type="button" disabled={saving} on:click={() => adding = false}>取消</button></div>
    </form>
  {/if}
  {#if loading}<p>正在读取设备…</p>{:else if !devices.length}<div class="panel"><h2>还没有设备</h2><p>设备联网后会显示6位绑定码，点击“添加设备”完成认领。</p></div>{:else}
    <div class="device-grid">{#each devices as d}<button class="device-card" disabled={saving} class:active={selected?.id === d.id} on:click={() => { adding = false; selectDevice(d); }}><strong>{d.device_name || '小蓝搭子'}</strong><span class:offline={d.display_status === '离线'} class:abnormal={d.display_status === '设备异常'}>{d.display_status || '离线'}</span><p>{d.sku_code || '小蓝搭子'}</p><small>{d.room_id ? '已绑定：' + roomName(d.room_id) + ' · ' + (d.binding_role === 'listener' ? '监听设备' : '主设备') : '未绑定直播间'}</small>{#if d.display_status === '离线' && d.last_heartbeat_at}<small>最近在线：{new Date(d.last_heartbeat_at).toLocaleString('zh-CN')}</small>{/if}</button>{/each}</div>
  {/if}
  {#if selected && !adding}
    <section class="panel"><h2>{selected.device_name}</h2>
      <form on:submit|preventDefault={() => operate('rename')}><label>设备名称<input bind:value={name} disabled={saving} maxlength="32" required /></label><button disabled={saving || !name.trim() || name.trim() === selected.device_name}>保存名称</button></form>
      <form on:submit|preventDefault={saveAddressing}><label>小蓝回应称呼<select bind:value={addressingMode} disabled={saving || !addressingLoaded}><option value="auto">自动称呼（听不准用中性回应）</option><option value="female">固定称呼：靓女</option><option value="male">固定称呼：帅哥</option><option value="child">固定称呼：小伙伴</option><option value="neutral">中性回应：我在</option></select></label><p>唤醒：小蓝，小蓝 · 声音：芊悦。自动称呼仅估计声音特征，不识别个人身份；仅喊唤醒词时用中性回应，固定称呼不受限制。</p><button disabled={saving || !addressingLoaded}>{addressingLoaded ? '保存称呼' : '读取称呼中…'}</button></form>
      <form on:submit|preventDefault={() => operate('bind')}><label>绑定直播间<select bind:value={roomId} on:change={selectRoom} disabled={saving} required><option value="">请选择自己的直播间</option>{#each rooms as r}<option value={String(r.id)}>{r.name || '直播间 ' + r.id}{roomPrimary(r.id, selected.id) ? '（已有主设备）' : ''}</option>{/each}</select></label><label>设备角色<select bind:value={bindingRole} disabled={saving || !roomId}><option value="primary" disabled={primaryDisabled()}>主设备</option><option value="listener" disabled={listenerDisabled()}>监听设备</option></select></label><p>每个直播间只有一台主设备；其他设备以监听模式同步收听，只能调整本机音量和字体。要切换主设备，请在监听设备上选择“主设备”。</p><div class="actions"><button class="primary" disabled={saving || !roomId}>{selected.room_id === Number(roomId) ? '保存角色' : selected.room_id ? '更换直播间' : '绑定直播间'}</button>{#if selected.room_id}<button type="button" disabled={saving} on:click={() => operate('unbind')}>解除直播间绑定</button>{/if}</div></form>
      {#if !rooms.length}<p>请先在首页添加直播间。</p>{/if}
    </section>
  {/if}
</section>

<style>
  .devices-page{padding:24px 18px 110px;max-width:1000px;margin:auto}header{display:flex;align-items:center;justify-content:space-between;gap:15px}h1{margin:12px 0 6px}h2{font-size:19px;margin-top:0}p,small{color:#718096}button,input,select{font:inherit;border:1px solid #dbe3ef;border-radius:12px;padding:12px;background:white}button{cursor:pointer}button:disabled{opacity:.5;cursor:default}.primary{background:#596bdc;color:white;border-color:#596bdc}.panel{background:white;border:1px solid #dbe3ef;border-radius:20px;padding:20px;margin:20px 0}.panel form+form{margin-top:24px;border-top:1px solid #edf1f7;padding-top:24px}label{display:grid;gap:8px;margin-bottom:16px}input,select{width:100%;box-sizing:border-box}.actions{display:flex;flex-wrap:wrap;gap:10px}.notice{padding:12px;border-radius:12px}.error{background:#fff0f1;color:#b74e58}.success{background:#e8f6ee;color:#268554}.device-grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(240px,1fr));gap:14px;margin-top:20px}.device-card{display:grid;gap:10px;text-align:left;padding:20px}.device-card.active{border:2px solid #596bdc}.device-card strong{font-size:19px}.device-card span{color:#268554}.device-card .offline{color:#718096}.device-card .abnormal{color:#c84949}.device-card p{margin:0}
</style>

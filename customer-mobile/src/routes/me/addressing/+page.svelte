<script lang="ts">
  import { onMount } from 'svelte';
  import { getAddressingStrategy, updateAddressingStrategy } from '$lib/api';
  import type { LiveAddressingOption, LiveAddressingStrategy } from '$lib/types';

  let loading = true;
  let saving = false;
  let error = '';
  let notice = '';
  let strategy: LiveAddressingStrategy = { addressing_mode: 'system', addressing: [] };

  const systemOptions = () => strategy.addressing.filter((item) => item.system_default);
  const customOptions = () => strategy.addressing.filter((item) => !item.system_default);

  async function load() {
    loading = true;
    error = '';
    try {
      strategy = await getAddressingStrategy();
    } catch (value) {
      error = value instanceof Error ? value.message : '读取称呼策略失败';
    } finally {
      loading = false;
    }
  }

  function chooseMode(mode: 'system' | 'custom') {
    strategy = { ...strategy, addressing_mode: mode };
    notice = '';
    error = '';
  }

  function addCustom() {
    const item: LiveAddressingOption = {
      key: `custom_${Date.now()}`,
      text: '',
      enabled: true,
      probability: customOptions().length ? 0 : 100,
      system_default: false,
    };
    strategy = { ...strategy, addressing: [...strategy.addressing, item] };
  }

  function removeCustom(key: string) {
    strategy = { ...strategy, addressing: strategy.addressing.filter((item) => item.key !== key) };
  }

  async function save() {
    if (saving) return;
    if (strategy.addressing_mode === 'custom' && customOptions().filter((item) => item.enabled && item.text.trim()).length === 0) {
      error = '请至少添加一个自己的称呼。';
      return;
    }
    saving = true;
    error = '';
    notice = '';
    try {
      strategy = await updateAddressingStrategy(strategy);
      notice = strategy.addressing_mode === 'custom'
        ? '我的称呼已通过审核并生效。'
        : '已切换为系统默认称呼。';
    } catch (value) {
      error = value instanceof Error ? value.message : '保存称呼策略失败';
    } finally {
      saving = false;
    }
  }

  onMount(load);
</script>

<section class="page-section top-space addressing-page">
  <header class="mobile-header addressing-header">
    <a class="back-link" href="/me">‹</a>
    <div>
      <span class="eyebrow">直播策略</span>
      <h1>称呼策略</h1>
      <p>控制直播搭子在需要称呼观众时怎么叫，多个称呼会按概率随机使用。</p>
    </div>
  </header>

  {#if loading}
    <div class="state-card">正在读取称呼策略…</div>
  {:else}
    {#if error}<div class="message error">{error}</div>{/if}
    {#if notice}<div class="message success">{notice}</div>{/if}

    <div class="mode-grid">
      <button class:active={strategy.addressing_mode === 'system'} type="button" on:click={() => chooseMode('system')}>
        <strong>系统默认称呼</strong>
        <span>直接使用平台维护的安全称呼</span>
      </button>
      <button class:active={strategy.addressing_mode === 'custom'} type="button" on:click={() => chooseMode('custom')}>
        <strong>我的称呼</strong>
        <span>自己配置，保存前会经过大模型审核</span>
      </button>
    </div>

    <section class="strategy-card">
      <div class="card-head">
        <div>
          <span>系统默认</span>
          <strong>当前可用称呼</strong>
        </div>
        {#if strategy.addressing_mode === 'system'}<b>正在使用</b>{/if}
      </div>
      <div class="option-list">
        {#each systemOptions() as item}
          <div class="option-row" class:muted={strategy.addressing_mode !== 'system'}>
            <span>{item.text}</span>
            <small>{item.enabled ? `${item.probability}%` : '停用'}</small>
          </div>
        {/each}
      </div>
    </section>

    <section class="strategy-card custom-card">
      <div class="card-head">
        <div>
          <span>用户层</span>
          <strong>我的称呼</strong>
        </div>
        <button type="button" class="add-button" on:click={addCustom}>+ 新增</button>
      </div>
      {#if customOptions().length === 0}
        <div class="empty">还没有自定义称呼。切换到“我的称呼”后，先新增一个称呼。</div>
      {:else}
        <div class="custom-list">
          {#each customOptions() as item (item.key)}
            <div class="custom-row">
              <label class="toggle"><input type="checkbox" bind:checked={item.enabled} /><span></span></label>
              <input class="name-input" bind:value={item.text} maxlength="12" placeholder="例如：老哥" />
              <div class="probability"><input type="number" min="0" max="100" step="1" bind:value={item.probability} disabled={!item.enabled} /><span>%</span></div>
              <button type="button" class="remove" on:click={() => removeCustom(item.key)}>删除</button>
            </div>
          {/each}
        </div>
      {/if}
      <p class="review-note">保存生效时系统会自动优化当前称呼的实际比例。自定义称呼仍会经过大模型安全审核。</p>
    </section>

    <button class="save-button" type="button" disabled={saving} on:click={save}>
      {saving ? '审核并保存中…' : '保存称呼策略'}
    </button>
  {/if}
</section>

<style>
  .addressing-page{display:grid;gap:14px;padding-bottom:96px}.addressing-header{display:flex;align-items:flex-start;gap:10px}.addressing-header h1,.addressing-header p{margin:0}.addressing-header p{margin-top:5px;color:#8a94a8;font-size:13px;line-height:1.55}.back-link{display:grid;place-items:center;width:34px;height:34px;border-radius:50%;background:#fff;color:#44516a;font-size:26px;text-decoration:none;box-shadow:0 5px 18px #24304b12}.state-card,.message,.strategy-card{border:1px solid #e6e9f0;border-radius:18px;background:#fff}.state-card,.message{padding:15px}.message.error{background:#fff3f1;color:#bd493d}.message.success{background:#effaf3;color:#258551}.mode-grid{display:grid;grid-template-columns:1fr 1fr;gap:10px}.mode-grid button{display:grid;gap:5px;text-align:left;padding:14px;border:1px solid #e1e5ee;border-radius:16px;background:#fff;color:#3d485e}.mode-grid button.active{border-color:#596edf;box-shadow:0 0 0 3px #596edf16;background:#f7f8ff}.mode-grid span{font-size:12px;color:#9099aa;line-height:1.45}.strategy-card{padding:15px}.card-head{display:flex;align-items:center;justify-content:space-between;gap:12px}.card-head>div{display:grid;gap:3px}.card-head span{font-size:11px;color:#96a0b0}.card-head strong{font-size:17px;color:#303b51}.card-head>b{font-size:12px;color:#4f66da}.option-list,.custom-list{display:grid;gap:8px;margin-top:12px}.option-row{display:flex;justify-content:space-between;align-items:center;padding:11px 12px;border-radius:12px;background:#f7f8fb;color:#435069}.option-row.muted{opacity:.62}.option-row small{color:#768298}.add-button,.remove{border:0;background:transparent;color:#5068d9;font-weight:800}.custom-row{display:grid;grid-template-columns:auto minmax(0,1fr) 82px auto;align-items:center;gap:8px}.name-input,.probability input{box-sizing:border-box;width:100%;min-height:40px;border:1px solid #dfe4ed;border-radius:10px;padding:8px 10px;background:#fbfcfe;font:inherit}.probability{display:flex;align-items:center;gap:4px}.probability span{font-size:12px;color:#788399}.remove{color:#c0574c}.toggle input{width:18px;height:18px}.empty{margin-top:12px;padding:14px;border-radius:12px;background:#f8f9fc;color:#8a94a6;font-size:13px}.review-note{margin:10px 0 0;color:#8993a6;font-size:12px;line-height:1.55}.save-button{position:sticky;bottom:18px;min-height:48px;border:0;border-radius:15px;background:#5368db;color:#fff;font:inherit;font-weight:850;box-shadow:0 10px 28px #5368db3d}.save-button:disabled{opacity:.48;box-shadow:none}@media(max-width:520px){.mode-grid{grid-template-columns:1fr}.custom-row{grid-template-columns:auto minmax(0,1fr) 76px}.custom-row .remove{grid-column:2/-1;justify-self:end}}
</style>

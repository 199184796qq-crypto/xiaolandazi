<script lang="ts">
  import { onMount } from 'svelte';
  import { goto } from '$app/navigation';
  import { getPublicSystemConfig, login, logout } from '$lib/api';
  import { applySession } from '$lib/session';

  let username = '';
  let password = '';
  let captcha = '';
  let captchaNonce = Date.now();
  let submitting = false;
  let error = '';
  let agentName = '小蓝工作搭子';

  $: captchaSrc = '/api/v1/auth/captcha?t=' + captchaNonce;

  onMount(async () => {
    try {
      const config = await getPublicSystemConfig();
      agentName = config.internal_agent_name?.trim() || agentName;
    } catch {}
  });

  function refreshCaptcha() {
    captcha = '';
    captchaNonce = Date.now();
  }

  async function submit() {
    if (submitting) return;
    submitting = true;
    error = '';
    try {
      const bootstrap = await login({
        username: username.trim(),
        password,
        captcha: captcha.trim(),
      });
      if (bootstrap.actor.role !== 'sales_staff') {
        await logout().catch(() => undefined);
        throw new Error('这个入口仅供销售人员登录');
      }
      applySession(bootstrap);
      await goto('/');
    } catch (value) {
      error = value instanceof Error ? value.message : '登录失败';
      refreshCaptcha();
    } finally {
      submitting = false;
    }
  }
</script>

<svelte:head><title>{agentName} · 销售登录</title></svelte:head>

<main class="login-page">
  <section class="login-hero">
    <div class="brand-orb">✦</div>
    <div>
      <span>SALES MOBILE</span>
      <h1>{agentName}</h1>
      <p>给在外销售使用的轻量工作台：客户、跟进、方案、业绩和智能体。</p>
    </div>
  </section>

  <form class="login-card" on:submit|preventDefault={submit}>
    <div class="field">
      <label for="username">销售账号</label>
      <input id="username" bind:value={username} autocomplete="username" placeholder="请输入销售账号" required />
    </div>
    <div class="field">
      <label for="password">密码</label>
      <input id="password" bind:value={password} type="password" autocomplete="current-password" placeholder="请输入密码" required />
    </div>
    <div class="field">
      <label for="captcha">验证码</label>
      <div class="captcha-row">
        <input id="captcha" bind:value={captcha} maxlength="8" placeholder="图形验证码" required />
        <button class="captcha-image" type="button" on:click={refreshCaptcha} aria-label="刷新验证码">
          <img src={captchaSrc} alt="验证码" />
        </button>
      </div>
    </div>
    {#if error}<p class="form-error">{error}</p>{/if}
    <button class="primary-action" type="submit" disabled={submitting}>
      {submitting ? '登录中…' : '进入销售工作台'}
    </button>
  </form>
</main>

<script lang="ts">
  import { onMount } from 'svelte';
  import { goto } from '$app/navigation';
  import { getPublicSystemConfig, login, logout } from '$lib/api';
  import { applySession } from '$lib/session';
  import { paymentLoginReturn } from '$lib/wechatPay';

  let username = '';
  let password = '';
  let captcha = '';
  let captchaNonce = Date.now();
  let submitting = false;
  let error = '';
  let agentName = '小蓝直播搭子';

  $: captchaSrc = '/api/v1/auth/captcha?t=' + captchaNonce;

  onMount(async () => {
    try {
      const config = await getPublicSystemConfig();
      agentName = config.client_agent_name?.trim() || agentName;
    } catch {
      // Keep fallback.
    }
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
      if (bootstrap.actor.role !== 'customer') {
        await logout().catch(() => undefined);
        throw new Error('这个入口仅供终端用户登录');
      }
      applySession(bootstrap);
      let target = '/';
      try {
        target = paymentLoginReturn(sessionStorage.getItem('wechat-payment-return') || '') || '/';
        sessionStorage.removeItem('wechat-payment-return');
      } catch { /* default home destination */ }
      await goto(target);
    } catch (value) {
      error = value instanceof Error ? value.message : '登录失败';
      refreshCaptcha();
    } finally {
      submitting = false;
    }
  }
</script>

<svelte:head><title>{agentName} · 登录</title></svelte:head>

<main class="login-page">
  <section class="login-hero">
    <div class="brand-orb">✦</div>
    <div>
      <span>AI LIVE COMPANION</span>
      <h1>{agentName}</h1>
      <p>轻量查看状态、会员、时长、订单和设备，不把电脑直播工作台塞进手机。</p>
    </div>
  </section>

  <form class="login-card" on:submit|preventDefault={submit}>
    <div class="field">
      <label for="username">账号</label>
      <input id="username" bind:value={username} autocomplete="username" placeholder="请输入账号" required />
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
      {submitting ? '登录中…' : '登录'}
    </button>
    <div class="register-entry"><span>收到好友邀请码？</span><a href="/register">注册新账号</a></div>
  </form>
</main>

<style>
  .register-entry{display:flex;align-items:center;justify-content:center;gap:7px;margin-top:14px;color:#8b94a7;font-size:12px}
  .register-entry a{color:#5e6bd2;font-weight:900}
</style>

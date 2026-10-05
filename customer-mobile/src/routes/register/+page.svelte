<script lang="ts">
  import { onMount } from 'svelte';
  import { goto } from '$app/navigation';
  import { getInvitePreview, getPublicSystemConfig, register } from '$lib/api';
  import { applySession } from '$lib/session';
  import type { InvitePreview } from '$lib/types';

  let username = '';
  let phone = '';
  let password = '';
  let confirmPassword = '';
  let inviteCode = '';
  let invitePreview: InvitePreview | null = null;
  let inviteChecking = false;
  let inviteError = '';
  let captcha = '';
  let captchaNonce = Date.now();
  let submitting = false;
  let error = '';
  let agentName = '小蓝直播搭子';

  $: captchaSrc = '/api/v1/auth/captcha?t=' + captchaNonce;

  onMount(async () => {
    const code = new URLSearchParams(window.location.search).get('invite');
    if (code) {
      inviteCode = code.trim().toUpperCase();
      void checkInvite();
    }
    try {
      const config = await getPublicSystemConfig();
      agentName = config.client_agent_name?.trim() || agentName;
    } catch {
      // Keep fallback name.
    }
  });

  function refreshCaptcha() {
    captcha = '';
    captchaNonce = Date.now();
  }

  function sourceLabel(source: string) {
    if (source === 'platform_invite') return '平台邀请';
    if (source === 'agent_invite') return '代理邀请';
    if (source === 'sales_invite') return '销售邀请';
    if (source === 'referral') return '好友推荐';
    return '邀请注册';
  }

  function resetInviteCheck() {
    invitePreview = null;
    inviteError = '';
  }

  async function checkInvite() {
    const code = inviteCode.trim().toUpperCase();
    invitePreview = null;
    inviteError = '';
    if (!code) {
      inviteError = '请输入邀请码';
      return false;
    }
    inviteChecking = true;
    try {
      invitePreview = await getInvitePreview(code);
      inviteCode = code;
      return true;
    } catch (value) {
      inviteError = value instanceof Error ? value.message : '邀请码无效、已停用或已过期';
      return false;
    } finally {
      inviteChecking = false;
    }
  }

  async function submit() {
    if (submitting) return;
    error = '';
    if (!phone.trim()) {
      error = '请填写联系电话';
      return;
    }
    if (password !== confirmPassword) {
      error = '两次输入的密码不一致';
      return;
    }
    if (!(await checkInvite())) {
      error = '请先填写有效邀请码';
      return;
    }
    submitting = true;
    try {
      const bootstrap = await register({
        username: username.trim(),
        phone: phone.trim(),
        password,
        confirm_password: confirmPassword,
        invite_code: inviteCode.trim().toUpperCase(),
        captcha: captcha.trim(),
      });
      applySession(bootstrap);
      await goto('/');
    } catch (value) {
      error = value instanceof Error ? value.message : '注册失败';
      refreshCaptcha();
    } finally {
      submitting = false;
    }
  }
</script>

<svelte:head><title>{agentName} · 邀请注册</title></svelte:head>

<main class="register-page">
  <header class="register-hero">
    <div class="register-orb">✦</div>
    <div><span>INVITATION REGISTRATION</span><h1>邀请码注册</h1><p>加入 {agentName}，开启你的智能直播工作台</p></div>
  </header>

  <form class="register-card" on:submit|preventDefault={submit}>
    <section class="invite-check-zone">
      <label for="invite-code">邀请码</label>
      <div>
        <input id="invite-code" bind:value={inviteCode} maxlength="32" autocomplete="off" placeholder="输入好友邀请码" required on:input={resetInviteCheck} on:blur={checkInvite} />
        <button type="button" disabled={inviteChecking} on:click={checkInvite}>{inviteChecking ? '校验中' : '校验'}</button>
      </div>
      {#if invitePreview}
        <p class="invite-valid"><b>✓</b><span><strong>邀请码有效</strong>{sourceLabel(invitePreview.source_type)} · {invitePreview.inviter_name}</span></p>
      {/if}
      {#if inviteError}<p class="field-error">{inviteError}</p>{/if}
    </section>

    <label class="field"><span>联系电话</span><input bind:value={phone} type="tel" autocomplete="tel" inputmode="tel" placeholder="请输入手机号" required /></label>
    <label class="field"><span>登录账号</span><input bind:value={username} autocomplete="username" maxlength="32" placeholder="4-32 位字母、数字或符号" required /></label>
    <label class="field"><span>登录密码</span><input bind:value={password} type="password" autocomplete="new-password" maxlength="72" placeholder="至少 8 位" required /></label>
    <label class="field"><span>确认密码</span><input bind:value={confirmPassword} type="password" autocomplete="new-password" maxlength="72" placeholder="再次输入密码" required /></label>
    <label class="field">
      <span>图片验证码</span>
      <div class="captcha-row">
        <input bind:value={captcha} inputmode="numeric" autocomplete="off" maxlength="8" placeholder="输入图中数字" required />
        <button type="button" aria-label="刷新验证码" on:click={refreshCaptcha}><img src={captchaSrc} alt="数字验证码" /></button>
      </div>
    </label>

    {#if error}<p class="register-error">{error}</p>{/if}
    <button class="register-submit" type="submit" disabled={submitting}>{submitting ? '正在注册…' : '注册并进入工作台'}</button>
    <div class="login-entry"><span>已有账号？</span><a href="/login">返回登录</a></div>
  </form>
</main>

<style>
  .register-page{min-height:100vh;padding:calc(24px + env(safe-area-inset-top)) 18px calc(30px + env(safe-area-inset-bottom));background:radial-gradient(circle at 85% 5%,rgba(124,137,241,.19),transparent 30%),linear-gradient(180deg,#eef2ff,#f8f9fd 42%,#fff);color:#24314b}
  .register-hero{display:flex;align-items:center;gap:14px;max-width:520px;margin:0 auto 20px;padding:6px 3px}.register-orb{display:grid;width:58px;height:58px;flex:0 0 auto;place-items:center;border-radius:20px;background:linear-gradient(145deg,#8193ff,#5668dc);box-shadow:0 12px 25px rgba(75,91,199,.25);color:#fff;font-size:24px}.register-hero>div:last-child{display:grid;gap:3px}.register-hero span{color:#7784cf;font-size:9px;font-weight:950;letter-spacing:.12em}.register-hero h1{margin:0;color:#20304f;font-size:27px}.register-hero p{margin:0;color:#8b94a7;font-size:11px}
  .register-card{display:grid;max-width:520px;margin:auto;gap:14px;padding:19px;border:1px solid #e1e6f0;border-radius:25px;background:rgba(255,255,255,.92);box-shadow:0 18px 50px rgba(52,66,118,.12)}
  .field,.invite-check-zone{display:grid;gap:7px}.field>span,.invite-check-zone>label{color:#626d82;font-size:11px;font-weight:900}.field input,.invite-check-zone input{width:100%;min-height:47px;padding:0 13px;border:1px solid #dfe4ed;border-radius:13px;background:#fbfcff;color:#26334d;outline:none}.field input:focus,.invite-check-zone input:focus{border-color:#7885df;box-shadow:0 0 0 3px rgba(102,116,211,.09)}
  .invite-check-zone{padding:14px;border-radius:17px;background:#f2f3ff}.invite-check-zone>div{display:grid;grid-template-columns:minmax(0,1fr) 68px;gap:8px}.invite-check-zone>div button{border:0;border-radius:12px;background:#636dd5;color:#fff;font-size:11px;font-weight:900}.invite-check-zone>div button:disabled{opacity:.5}.invite-valid{display:flex;align-items:center;gap:9px;margin:2px 0 0;padding:9px 10px;border-radius:11px;background:#e8f8f0;color:#287c5d}.invite-valid>b{display:grid;width:24px;height:24px;place-items:center;border-radius:50%;background:#38b882;color:#fff}.invite-valid>span{display:grid;font-size:9px}.invite-valid strong{font-size:11px}.field-error{margin:0;color:#b6515b;font-size:10px}
  .captcha-row{display:grid;grid-template-columns:minmax(0,1fr) 112px;gap:9px}.captcha-row button{height:47px;overflow:hidden;padding:0;border:1px solid #dfe4ed;border-radius:13px;background:#fff}.captcha-row img{display:block;width:100%;height:100%;object-fit:cover}
  .register-error{margin:0;padding:10px 12px;border-radius:12px;background:#fff0f1;color:#b44f59;font-size:11px}.register-submit{min-height:51px;border:0;border-radius:14px;background:linear-gradient(135deg,#6977e6,#5361ce);box-shadow:0 11px 24px rgba(75,91,199,.22);color:#fff;font-size:13px;font-weight:950}.register-submit:disabled{opacity:.5}.login-entry{display:flex;align-items:center;justify-content:center;gap:7px;color:#8b94a7;font-size:11px}.login-entry a{color:#5e6bd2;font-weight:900}
</style>

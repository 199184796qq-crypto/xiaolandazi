<script lang="ts">
  import WorkInbox from '$lib/WorkInbox.svelte';
  import InboxBadge from '$lib/InboxBadge.svelte';
  import {inboxIntent} from '$lib/workInbox';
  let inboxOpen=false,inboxRequest='';
  import { onMount, tick } from 'svelte';
  import { chatInternalAgent, getPublicSystemConfig } from '$lib/api';

  type Message = { role: 'user' | 'agent'; text: string };
  let messages: Message[] = [];
  let input = '';
  let busy = false;
  let agentName = '小蓝工作搭子';
  let chatEl: HTMLDivElement;

  onMount(async () => {
    try {
      const config = await getPublicSystemConfig();
      agentName = config.internal_agent_name?.trim() || agentName;
    } catch {}
    messages = [{
      role: 'agent',
      text: '我只按你的销售岗位权限工作。可以查自己的客户、业务和业绩，不会替你做充值、退款、奖励或财务审批。',
    }];
  });

  async function send() {
    const value = input.trim();
    if (!value || busy) return;
    input = '';
    if(inboxIntent(value)){inboxRequest=value;inboxOpen=true;return}
    inboxOpen=false;
    const history = [...messages];
    messages = [...messages, { role: 'user', text: value }];
    busy = true;
    try {
      const response = await chatInternalAgent(value, history.slice(-10));
      messages = [...messages, { role: 'agent', text: response.reply }];
    } catch (error) {
      messages = [...messages, { role: 'agent', text: error instanceof Error ? error.message : '暂时无法回答' }];
    } finally {
      busy = false;
      await tick();
      chatEl?.scrollTo({ top: chatEl.scrollHeight, behavior: 'smooth' });
    }
  }
</script>

<section class="agent-page">
  <header class="agent-header">
    <a href="/">‹</a>
    <div><span>销售智能体 · 岗位权限隔离</span><h1>{agentName}</h1></div>
    <i class="status-dot"></i>
  </header>

  <nav class="agent-inbox-switch"><button class:active={!inboxOpen} on:click={()=>inboxOpen=false}>对话</button><button class:active={inboxOpen} on:click={()=>{inboxOpen=true;inboxRequest=''}}>我的待办<InboxBadge/></button></nav>
  {#if inboxOpen}<WorkInbox request={inboxRequest}/>{/if}
  <div class="agent-chat" class:inbox-hidden={inboxOpen} bind:this={chatEl}>
    {#each messages as message}
      <article class:mine={message.role === 'user'} class="chat-bubble">
        <small>{message.role === 'agent' ? agentName : '我'}</small>
        <p>{message.text}</p>
      </article>
    {/each}
    {#if busy}<article class="chat-bubble"><small>{agentName}</small><p>正在处理…</p></article>{/if}
  </div>

  <form class="agent-composer" on:submit|preventDefault={send}>
    <input bind:value={input} placeholder="例如：哪些客户最近需要我跟进？" />
    <button type="submit" disabled={busy}>发送</button>
  </form>
</section>
<style>.agent-page{display:flex;flex-direction:column;min-height:100dvh;padding-bottom:100px;box-sizing:border-box}.agent-chat{flex:1}.agent-inbox-switch{display:flex;gap:10px;padding:10px 16px}.agent-inbox-switch button{position:relative;min-height:40px;padding:8px 34px 8px 12px;border:1px solid #ccdaef;background:#fff;border-radius:9px;color:#315d90;font-size:18px}.agent-inbox-switch button.active{background:#edf5ff;border-color:#6f9feb}.agent-chat.inbox-hidden{display:none}</style>


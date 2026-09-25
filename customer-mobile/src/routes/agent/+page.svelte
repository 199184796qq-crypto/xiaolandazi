<script lang="ts">
  import { onMount, tick } from 'svelte';
  import { chatClientAgent, getPublicSystemConfig } from '$lib/api';

  type Message = { role: 'user' | 'agent'; text: string };
  let messages: Message[] = [];
  let input = '';
  let busy = false;
  let agentName = '小蓝直播搭子';
  let chatEl: HTMLDivElement;

  onMount(async () => {
    try {
      const config = await getPublicSystemConfig();
      agentName = config.client_agent_name?.trim() || agentName;
    } catch {}
  });

  async function send() {
    const value = input.trim();
    if (!value || busy) return;
    input = '';
    const history = [...messages];
    messages = [...messages, { role: 'user', text: value }];
    busy = true;
    try {
      const response = await chatClientAgent(value, history.slice(-10));
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
    <a href="/" aria-label="返回首页">‹</a>
    <div><h1>{agentName}</h1><small class="terminal-agent-subtitle" lang="en">XIAOLAN LIVE COMPANION</small></div>
    <i class="status-dot"></i>
  </header>
  <div class="agent-chat" bind:this={chatEl}>
    {#each messages as message}
      <article class:mine={message.role === 'user'} class="chat-bubble">
        <small>{message.role === 'agent' ? agentName : '我'}</small>
        <p>{message.text}</p>
      </article>
    {/each}
    {#if busy}<article class="chat-bubble"><small>{agentName}</small><p>正在处理…</p></article>{/if}
  </div>
  <form class="agent-composer" on:submit|preventDefault={send}>
    <input bind:value={input} placeholder="输入你想说的话…" aria-label="聊天内容" />
    <button type="submit" disabled={busy || !input.trim()}>发送</button>
  </form>
</section>
<style>
.agent-page{display:flex;flex-direction:column;min-height:100dvh;padding-bottom:100px;box-sizing:border-box}
.agent-chat{flex:1}
.agent-header h1,.agent-chat p,.agent-composer input,.agent-composer button{font-size:18px;line-height:1.6}
.agent-header h1{margin:0}
.agent-header > div{min-width:0}
.agent-header .terminal-agent-subtitle{display:block;margin-top:4px;color:#65748e;font-size:14px;line-height:1.4;font-weight:500;letter-spacing:.06em;overflow-wrap:anywhere}
.agent-composer{grid-template-columns:minmax(0,1fr) auto;align-items:center}
.agent-composer input{min-width:0}
.agent-composer button{align-self:center;min-width:68px;height:48px;min-height:48px;padding:0 16px;border-radius:12px;white-space:nowrap}
</style>


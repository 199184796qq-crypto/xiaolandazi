package main

// pageHTML 是嵌入的独立测试页面：问题输入 + 向量召回 + Rerank 精排对照。
// 注意：这里不使用反引号，避免和 Go 原始字符串冲突。
const pageHTML = `<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>事实库召回 · Rerank 精排测试</title>
<style>
  * { box-sizing: border-box; }
  body { margin: 0; font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", "PingFang SC", "Microsoft YaHei", sans-serif; background: #f4f6fb; color: #1f2937; }
  header { padding: 20px 28px 8px; }
  h1 { font-size: 20px; margin: 0 0 6px; letter-spacing: .5px; }
  header p { margin: 0; color: #6b7280; font-size: 13px; }
  main { padding: 12px 28px 40px; max-width: 1440px; }
  .card { background: #fff; border: 1px solid #e5e7eb; border-radius: 12px; box-shadow: 0 1px 2px rgba(16,24,40,.04); }
  .controls { padding: 16px; display: grid; grid-template-columns: 200px 1fr 120px 200px 176px; gap: 12px; align-items: end; }
  .field label { display: block; font-size: 12px; color: #6b7280; margin-bottom: 6px; }
  .field input, .field select { width: 100%; height: 38px; padding: 0 10px; border: 1px solid #d1d5db; border-radius: 8px; font-size: 14px; background: #fff; color: #111827; }
  .field input:focus, .field select:focus { outline: none; border-color: #2563eb; box-shadow: 0 0 0 3px rgba(37,99,235,.12); }
  .buttons { display: flex; gap: 8px; }
  button { height: 38px; padding: 0 16px; border-radius: 8px; border: 1px solid transparent; font-size: 14px; cursor: pointer; }
  button.primary { background: #2563eb; color: #fff; }
  button.primary:disabled { opacity: .55; cursor: default; }
  button.ghost { background: #fff; border-color: #d1d5db; color: #374151; }
  .chips { display: flex; flex-wrap: wrap; gap: 8px; margin: 12px 0 0; }
  .chips span { font-size: 12px; color: #6b7280; }
  .chip { font-size: 12px; padding: 4px 10px; border-radius: 999px; background: #eef2ff; color: #3730a3; cursor: pointer; border: 1px solid #e0e7ff; }
  .meta { display: flex; flex-wrap: wrap; gap: 18px; padding: 10px 4px 0; font-size: 12px; color: #6b7280; }
  .meta b { color: #111827; font-weight: 600; }
  .grid { display: grid; grid-template-columns: 1fr 1fr; gap: 16px; margin-top: 16px; }
  .panel { display: flex; flex-direction: column; min-height: 200px; }
  .panel-head { display: flex; align-items: center; justify-content: space-between; padding: 12px 16px; border-bottom: 1px solid #eef0f4; }
  .panel-head h2 { font-size: 14px; margin: 0; }
  .panel-head .tag { font-size: 11px; padding: 2px 8px; border-radius: 999px; }
  .tag.blue { background: #eff6ff; color: #1d4ed8; }
  .tag.green { background: #ecfdf5; color: #047857; }
  .panel-head small { color: #9ca3af; font-size: 11px; }
  .list { padding: 8px 16px 16px; max-height: 62vh; overflow: auto; }
  .item { padding: 10px 0; border-bottom: 1px dashed #eef0f4; }
  .item:last-child { border-bottom: none; }
  .item-top { display: flex; align-items: center; gap: 8px; font-size: 12px; color: #6b7280; }
  .rank { display: inline-flex; width: 22px; height: 22px; align-items: center; justify-content: center; border-radius: 6px; background: #f3f4f6; color: #4b5563; font-weight: 600; font-size: 12px; }
  .cos { color: #2563eb; font-weight: 600; }
  .rr { color: #047857; font-weight: 600; }
  .topic { color: #6b7280; }
  .scope { font-size: 11px; padding: 1px 6px; border-radius: 999px; background: #f3f4f6; color: #6b7280; }
  .up { color: #047857; font-weight: 600; }
  .down { color: #b91c1c; font-weight: 600; }
  .flat { color: #9ca3af; }
  .text { margin-top: 4px; font-size: 13px; line-height: 1.55; color: #111827; word-break: break-all; }
  .snippet { color: #9ca3af; font-size: 12px; margin-top: 2px; }
  .docbox { margin-top: 4px; }
  .docbox summary { cursor: pointer; color: #9ca3af; font-size: 12px; }
  .docbox pre { margin: 4px 0 0; padding: 8px 10px; background: #f8fafc; border: 1px solid #eef0f4; border-radius: 6px; white-space: pre-wrap; word-break: break-all; font-size: 12px; color: #475569; }
  .empty { color: #9ca3af; text-align: center; padding: 32px 0; font-size: 13px; }
  .error { margin-top: 12px; padding: 10px 14px; background: #fef2f2; border: 1px solid #fecaca; color: #b91c1c; border-radius: 8px; font-size: 13px; white-space: pre-wrap; }
  .warn { margin: 10px 16px 0; padding: 8px 12px; background: #fffbeb; border: 1px solid #fde68a; color: #92400e; border-radius: 8px; font-size: 12px; }
  .feed-panel { margin-top: 16px; }
  .pipeline-panel { margin-top: 16px; }
  .intent-panel { margin-top: 16px; }
  .intent-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 16px; padding: 8px 0 4px; }
  .intent-col { min-width: 0; }
  .intent-col h3 { font-size: 13px; margin: 0 0 6px; }
  .intent-col pre { margin: 0 0 8px; padding: 8px 10px; background: #f8fafc; border: 1px solid #eef0f4; border-radius: 6px; white-space: pre-wrap; word-break: break-all; font-size: 12px; color: #475569; max-height: 320px; overflow: auto; }
  .intent-table { width: 100%; border-collapse: collapse; font-size: 12px; }
  .intent-table th, .intent-table td { text-align: left; padding: 4px 6px; border-bottom: 1px solid #eef0f4; vertical-align: top; }
  .intent-table th { color: #6b7280; font-weight: 600; }
  .issue { color: #b91c1c; font-size: 12px; padding: 2px 0; }
  .ok { color: #047857; font-size: 12px; padding: 2px 0; }
  .pipeline-actions { display: flex; align-items: center; gap: 10px; }
  .pipeline-actions small { color: #9ca3af; font-size: 11px; }
  .pipe-meta { font-size: 12px; color: #6b7280; padding: 4px 0 10px; }
  .intent-box { padding: 10px 0; border-bottom: 1px dashed #eef0f4; font-size: 13px; }
  .asks { margin-top: 8px; display: flex; flex-wrap: wrap; gap: 6px; }
  .ask-block { padding: 10px 0; border-bottom: 1px dashed #eef0f4; }
  .ask-title { font-size: 13px; font-weight: 600; margin-bottom: 4px; }
  .evidence { font-size: 12px; color: #374151; padding: 3px 0; }
  .answer-box { margin-top: 12px; padding: 12px; background: #f8fafc; border: 1px solid #e5e7eb; border-radius: 8px; }
  .answer-title { font-size: 12px; color: #6b7280; margin-bottom: 6px; }
  .answer-text { font-size: 15px; line-height: 1.6; color: #111827; white-space: pre-wrap; }
  .loading { opacity: .5; pointer-events: none; }
  @media (max-width: 960px) { .controls { grid-template-columns: 1fr; } .grid { grid-template-columns: 1fr; } }
  /* 界面字号统一 16；先展示意图，确认后再召回 */
  body { font-size: 16px; }
  h1 { font-size: 22px; }
  header p { font-size: 15px; }
  .field label, .chips span, .chip, .meta, .item-top, .snippet, .scope, .tag,
  .panel-head small, .pipeline-actions small, .pipe-meta, .evidence,
  .docbox summary, .docbox pre, .intent-col pre, .intent-table, .issue, .ok,
  .warn, .error, .empty { font-size: 14px; }
  .field input, .field select, button { font-size: 16px; height: 42px; }
  .panel-head h2 { font-size: 17px; }
  .text { font-size: 16px; line-height: 1.7; }
  .intent-box, .ask-title, .answer-title, .intent-col h3 { font-size: 15px; }
  .answer-text { font-size: 17px; }
  .intent-result { padding: 10px 16px 16px; }
  .intent-summary { display: flex; flex-wrap: wrap; gap: 6px 14px; font-size: 14px; color: #6b7280; margin-bottom: 10px; }
  .intent-summary b { color: #111827; }
  .intent-empty { color: #9ca3af; text-align: center; padding: 28px 0; font-size: 15px; }
  .ask-card { border: 1px solid #eef0f4; border-radius: 8px; padding: 10px 12px; margin-bottom: 10px; background: #fbfcff; }
  .ask-query { font-size: 16px; color: #111827; margin-top: 4px; line-height: 1.5; }
  .ask-meta { margin-top: 6px; display: flex; flex-wrap: wrap; gap: 6px 12px; color: #6b7280; font-size: 14px; }
  .cond-chip { display: inline-block; padding: 2px 8px; border-radius: 999px; background: #eef2ff; color: #3730a3; font-size: 14px; margin: 0 6px 4px 0; }
  .alt-badge { color: #b45309; }
  .ask-recall-card { border: 1px solid #eef0f4; border-radius: 10px; margin-bottom: 12px; overflow: hidden; }
  .ask-recall-head { padding: 10px 12px; background: #f8fafc; cursor: pointer; display: flex; flex-wrap: wrap; gap: 8px; align-items: center; font-size: 15px; }
  .ask-recall-head b { color: #111827; }
  .ask-recall-head .ask-id { color: #1d4ed8; font-weight: 700; }
  .ask-recall-head .ask-q { color: #374151; }
  .ask-recall-head .ask-stat { margin-left: auto; color: #6b7280; font-size: 14px; }
  .ask-recall-body { padding: 10px 12px 14px; }
  .ask-recall-body .two-col { display: grid; grid-template-columns: 1fr 1fr; gap: 14px; }
  .ask-recall-body h4 { margin: 0 0 6px; font-size: 14px; color: #6b7280; font-weight: 600; }
  .mini-item { padding: 6px 0; border-bottom: 1px dashed #eef0f4; font-size: 14px; }
  .mini-item:last-child { border-bottom: none; }
  .mini-item .mini-top { display: flex; gap: 8px; align-items: center; color: #6b7280; font-size: 13px; flex-wrap: wrap; }
  .mini-item .mini-text { margin-top: 2px; color: #111827; font-size: 15px; line-height: 1.5; }
  .ask-feed { margin-top: 12px; border-top: 1px dashed #eef0f4; padding-top: 8px; }
  .ask-feed h4 { margin: 0 0 4px; font-size: 14px; color: #047857; font-weight: 600; }
  .feed-group { border: 1px solid #eef0f4; border-radius: 8px; padding: 8px 10px; margin-bottom: 10px; background: #fbfcff; }
  .feed-group-head { font-size: 14px; color: #1d4ed8; font-weight: 600; margin-bottom: 4px; }
  .merged-row { display: grid; grid-template-columns: 48px 1fr 120px 150px; gap: 10px; padding: 8px 0; border-bottom: 1px dashed #eef0f4; font-size: 14px; align-items: start; }
  .merged-row .merged-rank { color: #4b5563; font-weight: 600; }
  .merged-row .merged-topic { color: #6b7280; }
  .merged-row .merged-text { color: #111827; font-size: 15px; }
  .merged-row .merged-score { color: #047857; font-weight: 600; }
  .merged-row .merged-hits { color: #1d4ed8; }
  .merged-row .merged-cond { color: #92400e; font-size: 13px; }
</style>
</head>
<body>
<header>
  <h1>事实库召回 · Rerank 精排测试</h1>
  <p>只读测试页：从线上事实库读取资料，用阿里 embedding 模型做向量召回，再把召回的同一批资料交给 qwen3.7-text-rerank 按问题重新排序，左右对照效果。</p>
  <p>入库文本与方案资料保存时一致：对象 + 主题 + 内容 + 极性 + 每个“条件/例外”各一行（条件以 JSON predicate 形式入库），条件与例外同时参与向量召回和精排。全局事实会按方案里的每个对象各展开一条入库，例如“1号链接搭配主食当下酒菜”“2号链接搭配主食当下酒菜”。</p>
</header>
<main>
  <section class="card controls">
    <div class="field">
      <label>方案</label>
      <select id="planSelect"></select>
    </div>
    <div class="field">
      <label>问题（用户弹幕/提问）</label>
      <input id="questionInput" placeholder="例如：这个多少钱？能邮寄吗？券有效期多久？" autocomplete="off">
    </div>
    <div class="field">
      <label>召回条数</label>
      <select id="topKSelect">
        <option value="10">10</option>
        <option value="20" selected>20</option>
        <option value="30">30</option>
        <option value="50">50</option>
      </select>
    </div>
    <div class="field">
      <label>入库拼接</label>
      <select id="docModeSelect">
        <option value="sentence" selected>句子（自然语言）</option>
        <option value="lines">分行（线上同款）</option>
        <option value="compact">合并（短句）</option>
      </select>
    </div>
    <div class="field">
      <label>向量来源</label>
      <select id="vectorSourceSelect">
        <option value="stored" selected>线上已存向量</option>
        <option value="live">实时重新生成</option>
      </select>
    </div>
    <div class="buttons">
      <button class="primary" id="runBtn">回答</button>
      <button class="ghost" id="refreshBtn" title="重新给事实库生成向量">重建向量</button>
    </div>
  </section>
  <div class="chips" id="examples">
    <span>示例问题：</span>
    <span class="chip">这个多少钱</span>
    <span class="chip">能邮寄吗</span>
    <span class="chip">怎么提货</span>
    <span class="chip">券有效期到什么时候</span>
    <span class="chip">在哪里可以买</span>
  </div>
  <div class="meta" id="meta"></div>
  <div id="error"></div>
  <section class="card panel" id="intentResultPanel" style="display:none">
    <div class="panel-head">
      <h2>意图挖掘（结构化 v2）</h2>
      <div class="pipeline-actions">
        <small>先看意图；确认后再召回</small>
        <button class="primary" id="continueBtn" disabled style="display:none">继续召回</button>
      </div>
    </div>
    <div class="intent-result" id="intentResultBody"><div class="intent-empty">输入问题后点「回答」：自动完成意图、召回、精排和回答</div></div>
  </section>
  <section class="card panel" id="askRecallPanel" style="display:none">
    <div class="panel-head">
      <h2>召回结果（按 ask · cosine / rerank 左右对照）</h2>
      <small id="askRecallMeta"></small>
    </div>
    <div class="list" id="askRecallBody"></div>
  </section>
  <section class="card panel" id="mergedPanel" style="display:none">
    <div class="panel-head">
      <h2>按 ask 去重汇总（问题之间不去重）</h2>
      <small id="mergedMeta"></small>
    </div>
    <div class="list" id="mergedBody"></div>
  </section>
  <section class="card panel" id="finalFeedPanel" style="display:none">
    <div class="panel-head">
      <h2 id="finalFeedTitle">喂给模型的证据（按 ask 叠加）</h2>
      <div class="pipeline-actions">
        <small>每个 ask 自己去重；问题之间不合并</small>
        <button class="primary" id="answerBtn" disabled>用这些证据回答</button>
      </div>
    </div>
    <div class="list" id="finalFeedBody"></div>
    <div class="intent-result" id="answerBox" style="display:none"></div>
  </section>
  <section class="grid" id="recallGrid" style="display:none">
    <div class="card panel">
      <div class="panel-head">
        <h2>向量召回（cosine 排序）</h2>
        <span class="tag blue" id="embedTag">embedding</span>
      </div>
      <div class="list" id="recallList"><div class="empty">先在上方解析意图，点「继续召回」后显示这里</div></div>
    </div>
    <div class="card panel">
      <div class="panel-head">
        <h2>Rerank 精排（同一批候选）</h2>
        <span class="tag green" id="rerankTag">rerank</span>
      </div>
      <div class="list" id="rerankList"><div class="empty">等待召回结果</div></div>
    </div>
  </section>
  <section class="card panel feed-panel" id="feedPanel" style="display:none">
    <div class="panel-head">
      <h2>喂给模型的证据（同一事实只保留一条 · Top 6）</h2>
      <small>同一事实只保留一条 · 点名对象时只喂该对象与全局 · Top 6</small>
    </div>
    <div class="list" id="feedList"><div class="empty">等待召回结果</div></div>
  </section>
  <section class="card panel pipeline-panel">
    <div class="panel-head">
      <h2>回答结果：意图 → 召回 → 精排 → 回答</h2>
      <div class="pipeline-actions" style="display:none">
        <small>线上弹幕测试同款意图/回答提示词</small>
        <button class="primary" id="pipelineBtn">跑一遍</button>
      </div>
    </div>
    <div class="list" id="pipelineBody"><div class="empty">输入问题后点「回答」：自动完成意图拆解、逐 ask 召回、精排和最终回答</div></div>
  </section>
</main>
<script>
(function () {
  var plans = [];
  var running = false;
  var metaEl = document.getElementById('meta');
  var errorEl = document.getElementById('error');
  var recallList = document.getElementById('recallList');
  var rerankList = document.getElementById('rerankList');
  var feedList = document.getElementById('feedList');
  var planSelect = document.getElementById('planSelect');
  var questionInput = document.getElementById('questionInput');
  var topKSelect = document.getElementById('topKSelect');
  var docModeSelect = document.getElementById('docModeSelect');
  var vectorSourceSelect = document.getElementById('vectorSourceSelect');
  var runBtn = document.getElementById('runBtn');
  var refreshBtn = document.getElementById('refreshBtn');
  var pipelineBtn = document.getElementById('pipelineBtn');
  var pipelineBody = document.getElementById('pipelineBody');
  var continueBtn = document.getElementById('continueBtn');
  var intentResultBody = document.getElementById('intentResultBody');
  var recallGrid = document.getElementById('recallGrid');
  var feedPanel = document.getElementById('feedPanel');
  var askRecallPanel = document.getElementById('askRecallPanel');
  var askRecallBody = document.getElementById('askRecallBody');
  var askRecallMeta = document.getElementById('askRecallMeta');
  var mergedPanel = document.getElementById('mergedPanel');
  var mergedBody = document.getElementById('mergedBody');
  var mergedMeta = document.getElementById('mergedMeta');
  var finalFeedPanel = document.getElementById('finalFeedPanel');
  var finalFeedBody = document.getElementById('finalFeedBody');
  var answerBtn = document.getElementById('answerBtn');
  var answerBox = document.getElementById('answerBox');
  var finalFeedTitle = document.getElementById('finalFeedTitle');
  var lastFeedFacts = [];
  var lastIntentAsks = [];
  var lastRecallData = null;
  var disabledAsks = {};
  var lastIntentQuestion = '';

  function escapeHTML(value) {
    return String(value == null ? '' : value)
      .replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;').replace(/'/g, '&#39;');
  }

  function setError(message) {
    errorEl.innerHTML = message ? '<div class="error">' + escapeHTML(message) + '</div>' : '';
  }

  function renderMeta(payload) {
    var parts = [];
    if (payload) {
      parts.push('事实条数 <b>' + escapeHTML(payload.fact_count) + '</b>');
      if (payload.entry_count) parts.push('索引条目 <b>' + escapeHTML(payload.entry_count) + '</b>');
      var plan = payload.plan || {};
      var scopes = plan.scope_counts || {};
      var scopeParts = [];
      var scopeKeys = Object.keys(scopes).filter(function (key) { return key !== '全局'; }).sort();
      if (scopes['全局']) scopeParts.push('全局 ' + scopes['全局']);
      scopeKeys.forEach(function (key) { scopeParts.push(key + ' ' + scopes[key]); });
      if (scopeParts.length) parts.push('归属 <b>' + escapeHTML(scopeParts.join(' · ')) + '</b>');
      if (payload.doc_mode) {
        var modeName = payload.doc_mode === 'compact' ? '合并' : (payload.doc_mode === 'sentence' ? '句子' : '分行');
        parts.push('入库拼接 <b>' + modeName + '</b>');
      }
      parts.push('embedding <b>' + escapeHTML(payload.embedding_model) + '</b>');
      if (payload.vector_source) {
        parts.push('向量来源 <b>' + (payload.vector_source === 'stored' ? '线上已存' : '实时生成') + '</b>');
        if (payload.vector_model) parts.push('库内模型 <b>' + escapeHTML(payload.vector_model) + '</b>');
        parts.push('缺向量 <b>' + Number(payload.missing_vectors || 0) + '</b>');
      }
      parts.push('rerank <b>' + escapeHTML(payload.rerank_model || '未配置') + '</b>');
      if (payload.timings_ms) {
        var t = payload.timings_ms;
        parts.push('耗时：事实向量 ' + Number(t.facts_ms || 0).toFixed(0) + 'ms · 问题向量 ' + Number(t.query_embed_ms || 0).toFixed(0) + 'ms · 精排 ' + Number(t.rerank_ms || 0).toFixed(0) + 'ms');
      }
    } else {
      parts.push('事实库向量在首次召回时生成并缓存在本进程内，点「重建向量」可刷新。');
    }
    metaEl.innerHTML = parts.map(function (part) { return '<span>' + part + '</span>'; }).join('');
  }

  function itemHTML(item, mode) {
    var head = '<span class="rank">' + item.rank + '</span>';
    head += '<span class="topic">#' + item.id + ' ' + escapeHTML(item.topic) + '</span>';
    if (item.scope) head += '<span class="scope">' + escapeHTML(item.scope) + '</span>';
    if (mode === 'cosine') {
      head += '<span class="cos">相似度 ' + item.cosine_score.toFixed(4) + '</span>';
    } else {
      head += '<span class="rr">精排 ' + item.rerank_score.toFixed(4) + '</span>';
      head += '<span class="cos">cos ' + item.cosine_score.toFixed(4) + '</span>';
      if (item.delta > 0) head += '<span class="up">↑' + item.delta + '</span>';
      else if (item.delta < 0) head += '<span class="down">↓' + Math.abs(item.delta) + '</span>';
      else head += '<span class="flat">—</span>';
    }
    return '<div class="item"><div class="item-top">' + head + '</div><div class="text">' + escapeHTML(item.text) + '</div>' + conditionHTML(item) + docHTML(item) + '</div>';
  }

  function docHTML(item) {
    if (!item.doc) return '';
    return '<details class="docbox"><summary>入库文本</summary><pre>' + escapeHTML(item.doc) + '</pre></details>';
  }

  function conditionHTML(item) {
    var html = '';
    var conditions = item.conditions || [];
    var exceptions = item.exceptions || [];
    if (conditions.length) {
      html += '<div class="snippet">条件：' + escapeHTML(conditions.join('；')) + '</div>';
    }
    if (exceptions.length) {
      html += '<div class="snippet">例外：' + escapeHTML(exceptions.join('；')) + '</div>';
    }
    return html;
  }

  function renderList(target, items, mode, emptyText) {
    if (!items || !items.length) {
      target.innerHTML = '<div class="empty">' + escapeHTML(emptyText) + '</div>';
      return;
    }
    target.innerHTML = items.map(function (item) { return itemHTML(item, mode); }).join('');
  }

  function renderFeed(payload) {
    var items = (payload && payload.feed) || [];
    if (!items.length) {
      feedList.innerHTML = '<div class="empty">没有可喂给模型的证据</div>';
      return;
    }
    feedList.innerHTML = items.map(function (item, index) {
      var head = '<span class="rank">' + (index + 1) + '</span>';
      head += '<span class="topic">#' + item.id + ' ' + escapeHTML(item.topic) + '</span>';
      if (item.scope) head += '<span class="scope">' + escapeHTML(item.scope) + '</span>';
      if (typeof item.rerank_score === 'number') head += '<span class="rr">精排 ' + item.rerank_score.toFixed(4) + '</span>';
      head += '<span class="cos">cos ' + item.cosine_score.toFixed(4) + '</span>';
      return '<div class="item"><div class="item-top">' + head + '</div><div class="text">' + escapeHTML(item.text) + '</div>' + conditionHTML(item) + '</div>';
    }).join('');
  }

  async function loadPlans() {
    try {
      var response = await fetch('/api/plans');
      var data = await response.json();
      if (!response.ok) throw new Error(data.error || '读取方案失败');
      plans = data.items || [];
      planSelect.innerHTML = plans.map(function (item) {
        return '<option value="' + item.id + '">' + escapeHTML(item.name) + '（事实 ' + item.facts + ' 条）</option>';
      }).join('');
      var preferred = plans.find(function (item) { return item.name.indexOf('好充') >= 0 || item.name.indexOf('果业') >= 0; });
      if (preferred) planSelect.value = String(preferred.id);
      document.getElementById('embedTag').textContent = data.embedding_model || 'embedding';
      document.getElementById('rerankTag').textContent = data.rerank_model || 'rerank';
      if (!data.rerank_enabled) {
        setError('当前进程未启用 rerank（DASHSCOPE_RERANK_ENABLED=false 或缺少 Key），只会显示向量召回。');
      }
      renderMeta(null);
    } catch (err) {
      setError('读取方案失败：' + err.message);
    }
  }

  async function runRecall() {
    if (running) return;
    var question = questionInput.value.trim();
    if (!question) {
      setError('请输入问题');
      questionInput.focus();
      return;
    }
    running = true;
    setError('');
    runBtn.disabled = true;
    runBtn.textContent = '召回中…';
    recallList.parentElement.classList.add('loading');
    rerankList.parentElement.classList.add('loading');
    try {
      var response = await fetch('/api/recall', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ plan_id: Number(planSelect.value), question: question, top_k: Number(topKSelect.value), doc_mode: docModeSelect.value, vector_source: vectorSourceSelect.value })
      });
      var data = await response.json();
      if (!response.ok) throw new Error(data.error || '召回失败');
      renderMeta(data);
      if (data.rerank_error) {
        setError('rerank 调用失败，已保留向量召回结果：' + data.rerank_error);
      }
      renderList(recallList, data.recall, 'cosine', '没有召回到事实');
      renderList(rerankList, data.rerank, 'rerank', 'rerank 未启用或候选不足');
      renderFeed(data);
    } catch (err) {
      setError(err.message);
      recallList.innerHTML = '<div class="empty">召回失败</div>';
      rerankList.innerHTML = '<div class="empty">召回失败</div>';
      feedList.innerHTML = '<div class="empty">召回失败</div>';
    } finally {
      running = false;
      runBtn.disabled = false;
      runBtn.textContent = '解析意图';
      recallList.parentElement.classList.remove('loading');
      rerankList.parentElement.classList.remove('loading');
    }
  }

  async function rebuildVectors() {
    try {
      setError('');
      var response = await fetch('/api/refresh', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ plan_id: Number(planSelect.value) })
      });
      if (!response.ok) {
        var data = await response.json();
        throw new Error(data.error || '重建失败');
      }
      renderMeta(null);
      if (questionInput.value.trim()) runPipeline();
    } catch (err) {
      setError(err.message);
    }
  }

  function ms(value) { return (typeof value === 'number' ? value.toFixed(0) : '—') + 'ms'; }

  async function runPipeline() {
    if (running) {
      setError('上一次回答还没结束，请稍等或按 Ctrl+F5 刷新页面');
      return;
    }
    var question = questionInput.value.trim();
    if (!question) {
      setError('请输入弹幕');
      questionInput.focus();
      return;
    }
    running = true;
    setError('');
    runBtn.disabled = true;
    runBtn.textContent = '回答中…';
    pipelineBody.innerHTML = '<div class="empty">意图分析中…（拆出多个 ask 时逐个召回 + 精排，约 3~8 秒）</div>';
    try {
      var controller = new AbortController();
      var timer = setTimeout(function () { controller.abort(); }, 180000);
      var response = await fetch('/api/pipeline', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        signal: controller.signal,
        body: JSON.stringify({
          plan_id: Number(planSelect.value),
          question: question,
          doc_mode: docModeSelect.value,
          top_k: Number(topKSelect.value),
          vector_source: vectorSourceSelect.value
        })
      });
      var data = await response.json();
      if (!response.ok) throw new Error(data.error || '链路运行失败');
      renderPipeline(data);
    } catch (err) {
      setError(err.message);
      pipelineBody.innerHTML = '<div class="empty">链路运行失败</div>';
    } finally {
      clearTimeout(timer);
      running = false;
      runBtn.disabled = false;
      runBtn.textContent = '回答';
    }
  }

  function renderPipeline(data) {
    var html = '';
    var timing = data.timings_ms || {};
    html += '<div class="pipe-meta">chat ' + escapeHTML(data.chat_model) + ' · 意图 ' + ms(timing.intent_ms) +
      ' · 召回+精排 ' + ms(timing.recall_ms) + ' · 回答 ' + ms(timing.answer_ms);
    if (data.location && data.location.length) {
      html += ' · 地区 <b>' + escapeHTML(data.location.join('、')) + (data.region_branch ? '（' + escapeHTML(data.region_branch) + '）' : '') + '</b>';
    }
    html += '</div>';
    if (data.correction && data.correction.corrected && data.correction.corrected !== data.correction.original) {
      var editText = '';
      if (data.correction.edits && data.correction.edits.length) {
        editText = '（' + data.correction.edits.map(function (e) { return e.from + '→' + e.to; }).join('；') + '）';
      }
      html += '<div class="snippet">纠错：' + escapeHTML(data.correction.original) + ' → <b>' + escapeHTML(data.correction.corrected) + '</b>' + escapeHTML(editText) + '</div>';
    }
    var intent = data.intent || {};
    html += '<div class="intent-box"><b>意图理解：</b>' + escapeHTML(intent.meaning || '') +
      ' <span class="topic">（' + escapeHTML(intent.category || '') + ' / ' + escapeHTML(intent.speech_act || '') + '）</span>';
    html += '<div class="asks">';
    (data.asks || []).forEach(function (item, index) {
      var label = 'ask' + (index + 1) + '：' + (item.ask || '');
      if (item.subjects && item.subjects.length) label += ' · 对象 ' + item.subjects.join('、');
      if (item.attributes && item.attributes.length) label += ' · 属性 ' + item.attributes.join(' / ');
      if (item.compare_group) label += ' · 对比组 ' + item.compare_group;
      html += '<span class="chip">' + escapeHTML(label) + '</span>';
    });
    html += '</div>';
    if (intent.uncertainties && intent.uncertainties.length) {
      html += '<div class="snippet">待核对：' + escapeHTML(intent.uncertainties.join('；')) + '</div>';
    }
    html += '</div>';
    (data.asks || []).forEach(function (item, index) {
      html += '<div class="ask-block"><div class="ask-title">ask' + (index + 1) + '：' + escapeHTML(item.ask);
      if (item.subjects && item.subjects.length) {
        html += ' <span class="scope">' + escapeHTML(item.subjects.join('、')) + '</span>';
      }
      if (item.attributes && item.attributes.length) {
        html += ' <span class="scope">属性 ' + escapeHTML(item.attributes.join(' / ')) + '</span>';
      }
      if (item.compare_group) {
        html += ' <span class="alt-badge">对比组 ' + escapeHTML(item.compare_group) + '</span>';
      }
      html += '</div>';
      if (item.notes && item.notes.length) {
        html += '<div class="snippet">扩展：' + escapeHTML(item.notes.join('；')) + '</div>';
      }
      if (item.error) {
        html += '<div class="snippet">召回失败：' + escapeHTML(item.error) + '</div>';
      }
      (item.evidence || []).forEach(function (e) {
        var rerank = typeof e.rerank_score === 'number' ? '<span class="rr">精排 ' + e.rerank_score.toFixed(4) + '</span>' : '';
        var cond = '';
        if (e.conditions && e.conditions.length) cond = '；条件：' + e.conditions.join('；');
        if (e.exceptions && e.exceptions.length) cond += '；例外：' + e.exceptions.join('；');
        html += '<div class="evidence">· [' + escapeHTML(e.scope) + '] #' + e.id + ' ' + escapeHTML(e.topic) +
          ' — ' + escapeHTML(e.text) + escapeHTML(cond) + ' <span class="cos">cos ' + e.cosine_score.toFixed(4) + '</span> ' + rerank + '</div>';
      });
      html += '</div>';
    });
    html += '<div class="answer-box"><div class="answer-title">回答（五字段里的“话术”，即最终可播正文）</div>' +
      '<div class="answer-text">' + escapeHTML(data.answer_speech || '（空）') + '</div>' +
      '<details class="docbox"><summary>模型原始输出</summary><pre>' + escapeHTML(data.answer_raw || '') + '</pre></details>' +
      '<details class="docbox"><summary>送给回答模型的输入</summary><pre>' + escapeHTML(data.answer_input || '') + '</pre></details></div>';
    pipelineBody.innerHTML = html;
  }

  async function runIntentFirst() {
    if (running) return;
    var question = questionInput.value.trim();
    if (!question) {
      setError('请输入问题');
      questionInput.focus();
      return;
    }
    running = true;
    setError('');
    runBtn.disabled = true;
    runBtn.textContent = '解析中…';
    intentResultBody.innerHTML = '<div class="intent-empty">意图解析中…</div>';
    try {
      var response = await fetch('/api/intent', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ plan_id: Number(planSelect.value), question: question })
      });
      var data = await response.json();
      if (!response.ok) throw new Error(data.error || '意图解析失败');
      lastIntentQuestion = question;
      lastIntentAsks = ((data.v2 && data.v2.parsed && data.v2.parsed.asks) || []).map(function (item, index) {
        var attributes = item.attributes || [];
        if (!attributes.length && item.attribute) attributes = [item.attribute];
        return {
          id: item.id || ('a' + (index + 1)),
          query: item.query || item.text || '',
          attribute: item.attribute || (attributes[0] || '其他'),
          attributes: attributes,
          attribute_scope: item.attribute_scope || '',
          operation: item.operation || '',
          subjects: item.subjects || [],
          compare_group: item.compare_group || ''
        };
      });
      renderIntentResult(data);
      continueBtn.disabled = lastIntentAsks.length === 0;
      // 先不展示召回，等用户点「继续召回」。
      askRecallPanel.style.display = 'none';
      mergedPanel.style.display = 'none';
      finalFeedPanel.style.display = 'none';
      recallGrid.style.display = 'none';
      feedPanel.style.display = 'none';
      answerBox.style.display = 'none';
    } catch (err) {
      setError(err.message);
      intentResultBody.innerHTML = '<div class="intent-empty">意图解析失败</div>';
    } finally {
      running = false;
      runBtn.disabled = false;
      runBtn.textContent = '解析意图';
    }
  }

  function renderIntentResult(data) {
    var v2 = (data.v2 && data.v2.parsed) || {};
    var issues = (data.v2 && data.v2.issues) || [];
    var html = '<div class="intent-summary">';
    html += '<span>理解 <b>' + escapeHTML(v2.meaning || '—') + '</b></span>';
    html += '<span>类型 <b>' + escapeHTML(v2.category || '—') + '</b></span>';
    html += '<span>模型 <b>' + escapeHTML(data.model || '') + '</b></span>';
    if (data.aligned) {
      html += '<span>对齐线上 objects=' + escapeHTML(data.aligned.objects) +
        ' · stores=' + escapeHTML(data.aligned.stores) +
        ' · attributes=' + escapeHTML(data.aligned.attributes) +
        ' · catalog=' + escapeHTML(data.aligned.catalog) +
        ' · 语境词命中=' + escapeHTML(data.aligned.context_hits) + '</span>';
    }
    html += '</div>';
    var conditions = v2.conditions || [];
    if (conditions.length) {
      html += '<div style="margin-bottom:10px">条件：';
      conditions.forEach(function (item) {
        html += '<span class="cond-chip">' + escapeHTML(item.field) + ' = ' + escapeHTML(item.value) + '（' + escapeHTML(item.scope || 'all') + '）</span>';
      });
      html += '</div>';
    }
    var asks = v2.asks || [];
    if (!asks.length) {
      html += '<div class="intent-empty">没有拆出 ask（category=' + escapeHTML(v2.category || '') + '），先不要召回</div>';
    } else {
      asks.forEach(function (item, index) {
        html += '<div class="ask-card">';
        html += '<div class="ask-title">ask' + (index + 1) + ' · ' + escapeHTML((item.attributes && item.attributes.length ? item.attributes.join(' / ') : (item.attribute || '全部属性'))) + '</div>';
        html += '<div class="ask-query">' + escapeHTML(item.query || item.text || '') + '</div>';
        html += '<div class="ask-meta">';
        html += '<span>原文：' + escapeHTML(item.text || '') + '</span>';
        if (item.subjects && item.subjects.length) html += '<span>对象：' + escapeHTML(item.subjects.join('、')) + '</span>';
        if (item.compare_group) html += '<span class="alt-badge">对比组 ' + escapeHTML(item.compare_group) + '</span>';
        if (item.alternative_group) html += '<span class="alt-badge">候选组 ' + escapeHTML(item.alternative_group) + '</span>';
        if (item.depends_on && item.depends_on.length) html += '<span>依赖：' + escapeHTML(item.depends_on.join('、')) + '</span>';
        if (item.alternative_group) html += '<span class="alt-badge">候选组 ' + escapeHTML(item.alternative_group) + '</span>';
        if (item.resolved_from) html += '<span>来源：' + escapeHTML(item.resolved_from) + '</span>';
        html += '</div></div>';
      });
    }
    if (v2.uncertainties && v2.uncertainties.length) {
      html += '<div class="snippet">待核对：' + escapeHTML(v2.uncertainties.join('；')) + '</div>';
    }
    if (issues.length) {
      html += issues.map(function (item) { return '<div class="issue">⚠ ' + escapeHTML(item) + '</div>'; }).join('');
    }
    if (data.payload) {
      html += '<details class="docbox"><summary>送给意图模型的完整输入（对齐线上）</summary><pre>' + escapeHTML(data.payload) + '</pre></details>';
    }
    intentResultBody.innerHTML = html;
  }

  async function runRecallAsks() {
    if (running) return;
    var asks = lastIntentAsks.filter(function (item) { return !disabledAsks[item.id]; });
    if (!asks.length) {
      setError('没有可召回的 ask');
      return;
    }
    running = true;
    setError('');
    continueBtn.disabled = true;
    continueBtn.textContent = '召回中…';
    askRecallPanel.style.display = '';
    mergedPanel.style.display = 'none';
    finalFeedPanel.style.display = 'none';
    askRecallBody.innerHTML = '<div class="empty">按 ' + asks.length + ' 个 ask 分别召回 + 精排中…</div>';
    try {
      var response = await fetch('/api/recall-asks', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          plan_id: Number(planSelect.value),
          question: lastIntentQuestion,
          asks: asks,
          top_k: Number(topKSelect.value),
          doc_mode: docModeSelect.value,
          vector_source: vectorSourceSelect.value
        })
      });
      var data = await response.json();
      if (!response.ok) throw new Error(data.error || '召回失败');
      lastRecallData = data;
      renderAskRecall(data);
      renderFeedSummary(data);
    } catch (err) {
      setError(err.message);
      askRecallBody.innerHTML = '<div class="empty">召回失败</div>';
    } finally {
      running = false;
      continueBtn.disabled = false;
      continueBtn.textContent = '继续召回';
    }
  }

  function miniItemHTML(item, mode) {
    var scoreText;
    if (mode === 'rerank') {
      scoreText = typeof item.rerank_score === 'number' ? '精排 ' + item.rerank_score.toFixed(4) : '精排 —';
    } else {
      scoreText = 'cos ' + (typeof item.cosine_score === 'number' ? item.cosine_score.toFixed(4) : '—');
    }
    var delta = '';
    if (mode === 'rerank' && typeof item.delta === 'number' && item.delta !== 0) {
      delta = item.delta > 0 ? '<span class="up">↑' + item.delta + '</span>' : '<span class="down">↓' + Math.abs(item.delta) + '</span>';
    }
    var cond = '';
    if (item.conditions && item.conditions.length) cond = '；条件：' + item.conditions.join('；');
    if (item.exceptions && item.exceptions.length) cond += '；例外：' + item.exceptions.join('；');
    return '<div class="mini-item"><div class="mini-top">' +
      '<span>#' + item.id + '</span>' +
      '<span class="topic">' + escapeHTML(item.topic || '') + '</span>' +
      '<span class="scope">' + escapeHTML(item.scope || '') + '</span>' +
      '<span class="' + (mode === 'rerank' ? 'rr' : 'cos') + '">' + scoreText + '</span>' + delta +
      '</div><div class="mini-text">' + escapeHTML(item.text || '') + escapeHTML(cond) + '</div></div>';
  }

  function evidenceRowHTML(item) {
    var cond = (item.conditions || []).join('；');
    var ex = (item.exceptions || []).join('；');
    return '<div class="evidence">· [' + escapeHTML(item.scope || '全局') + '] #' + item.id + ' ' + escapeHTML(item.topic || '') + ' — ' +
      escapeHTML(item.text || '') + (cond || ex ? '（条件：' + escapeHTML(cond || '—') + (ex ? '；例外：' + escapeHTML(ex) : '') + '）' : '') + '</div>';
  }

  function renderAskRecall(data) {
    var html = '';
    (data.asks || []).forEach(function (ask, index) {
      var hidden = index === 0 ? '' : ' style="display:none"';
      var recallHtml = (ask.recall || []).map(function (item) { return miniItemHTML(item, 'cosine'); }).join('') || '<div class="empty">没有召回</div>';
      var rerankHtml = (ask.rerank || []).map(function (item) { return miniItemHTML(item, 'rerank'); }).join('') || '<div class="empty">没有精排结果</div>';
      var feed = ask.feed || [];
      var feedBlock = '';
      if (ask.object_feeds && ask.object_feeds.length) {
        ask.object_feeds.forEach(function (group) {
          feedBlock += '<div class="feed-group"><div class="feed-group-head">' + escapeHTML(ask.id) + ' · ' + escapeHTML(group.object) + ' · ' + group.count + ' 条</div>' +
            ((group.feed || []).map(evidenceRowHTML).join('') || '<div class="empty">该对象没有命中事实</div>') + '</div>';
        });
      } else {
        feedBlock = feed.map(evidenceRowHTML).join('') || '<div class="empty">该 ask 没有事实</div>';
      }
      html += '<div class="ask-recall-card">' +
        '<div class="ask-recall-head">' +
        '<span class="ask-id">' + escapeHTML(ask.id) + '</span>' +
        '<b>' + escapeHTML((ask.attributes && ask.attributes.length ? ask.attributes.join(' / ') : (ask.attribute || '全部属性'))) + '</b>' +
        '<span class="ask-q">' + escapeHTML(ask.query) + '</span>' +
        (ask.compare_group ? '<span class="alt-badge">对比组 ' + escapeHTML(ask.compare_group) + '</span>' : '') +
        (ask.fallback_note ? '<span class="alt-badge">兜底：' + escapeHTML(ask.fallback_note) + '</span>' : '') +
        '<label style="margin-left:8px"><input type="checkbox" data-ask="' + escapeHTML(ask.id) + '"' + (disabledAsks[ask.id] ? ' checked' : '') + '> 忽略</label>' +
        '<span class="ask-stat">' + (ask.recall || []).length + ' 条召回 · ' + Math.round(ask.elapsed_ms || 0) + 'ms</span>' +
        '</div>' +
        '<div class="ask-recall-body"' + hidden + '><div class="two-col">' +
        '<div><h4>向量召回（cosine 排序）</h4>' + recallHtml + '</div>' +
        '<div><h4>Rerank 精排</h4>' + rerankHtml + '</div>' +
        '</div>' +
        '<div class="ask-feed"><h4>该 ask 去重后的清单（喂模型）· ' + feed.length + ' 条' + (ask.full ? ' · 全量' : ' · 按属性截取') + '</h4>' + feedBlock + '</div>' +
        (ask.error ? '<div class="issue">' + escapeHTML(ask.error) + '</div>' : '') + '</div></div>';
    });
    askRecallBody.innerHTML = html;
    askRecallMeta.textContent = 'embedding ' + data.embed_model + ' · rerank ' + data.rerank_model + ' · 按 ask 分别召回 / 精排 / 去重';
    Array.prototype.forEach.call(askRecallBody.querySelectorAll('.ask-recall-head'), function (head) {
      head.addEventListener('click', function (event) {
        if (event.target && event.target.tagName === 'INPUT') return;
        var body = head.nextElementSibling;
        body.style.display = body.style.display === 'none' ? '' : 'none';
      });
    });
    Array.prototype.forEach.call(askRecallBody.querySelectorAll('input[data-ask]'), function (box) {
      box.addEventListener('change', function () {
        disabledAsks[box.getAttribute('data-ask')] = box.checked;
        if (lastRecallData) renderFeedSummary(lastRecallData);
      });
    });
  }

  // renderFeedSummary：每个 ask 自己去重后的清单直接叠加，公共事实只给一份。
  function renderFeedSummary(data) {
    var activeAsks = (data.asks || []).filter(function (ask) { return !disabledAsks[ask.id]; });
    var html = '';
    var counts = [];
    lastFeedFacts = [];
    var dedupeBefore = 0;
    var seenFacts = {};
    function addFeedFact(tag, item) {
      dedupeBefore++;
      var key = String(item.id);
      var existing = seenFacts[key];
      if (existing) {
        if (existing.hits.indexOf(tag) < 0) existing.hits.push(tag);
        return;
      }
      var entry = { ask: tag, item: item, hits: [tag] };
      seenFacts[key] = entry;
      lastFeedFacts.push(entry);
    }
    activeAsks.forEach(function (ask) {
      var feed = ask.feed || [];
      counts.push(ask.id + '=' + feed.length);
      feed.forEach(function (item) {
        addFeedFact(ask.id, item);
      });
      if (ask.object_feeds && ask.object_feeds.length) {
        ask.object_feeds.forEach(function (group) {
          html += '<div class="feed-group"><div class="feed-group-head">' +
            escapeHTML(ask.id) + ' · ' + escapeHTML(group.object) + ' · ' + group.count + ' 条</div>' +
            ((group.feed || []).map(evidenceRowHTML).join('') || '<div class="empty">该对象没有命中事实</div>') + '</div>';
        });
      } else {
        html += '<div class="feed-group"><div class="feed-group-head">' +
          escapeHTML(ask.id) + ' · ' + escapeHTML((ask.attributes && ask.attributes.length ? ask.attributes.join(' / ') : (ask.attribute || '全部属性'))) + ' · ' + feed.length + ' 条' +
          (ask.full ? '（全量）' : '（按属性截取）') + ' · ' + escapeHTML(ask.query) + '</div>' +
          (feed.map(evidenceRowHTML).join('') || '<div class="empty">该 ask 没有事实</div>') + '</div>';
      }
    });
    var shared = data.shared || [];
    if (shared.length) {
      counts.push('公共=' + shared.length);
      shared.forEach(function (item) {
        addFeedFact('公共', item);
      });
      html += '<div class="feed-group"><div class="feed-group-head">公共（全店通用）· ' + shared.length + ' 条</div>' +
        (shared.map(evidenceRowHTML).join('')) + '</div>';
    }
    var total = lastFeedFacts.length;
    mergedBody.innerHTML = html || '<div class="empty">没有事实</div>';
    mergedMeta.textContent = '按 ask 叠加：' + (counts.join(' + ') || '无') + ' = 去重前 ' + dedupeBefore + ' 条，唯一 ' + total + ' 条（公共只给一份）';
    mergedPanel.style.display = '';
    finalFeedPanel.style.display = '';
    finalFeedTitle.textContent = '喂给模型的证据（去重前 ' + dedupeBefore + ' → 唯一 ' + total + ' 条）';
    finalFeedBody.innerHTML = lastFeedFacts.map(function (entry) { return evidenceRowHTML(entry.item); }).join('') ||
      '<div class="empty">没有可喂给模型的证据</div>';
    answerBtn.disabled = total === 0;
    answerBtn.textContent = '用这些证据回答';
    answerBox.style.display = 'none';
    answerBox.innerHTML = '';
  }

  async function runAnswerFromFacts() {
    if (running || !lastFeedFacts.length) return;
    running = true;
    setError('');
    answerBtn.disabled = true;
    answerBtn.textContent = '回答中…';
    answerBox.style.display = '';
    answerBox.innerHTML = '<div class="intent-empty">回答模型生成中…</div>';
    try {
      var facts = lastFeedFacts.map(function (entry) {
        var item = entry.item;
        return {
          id: item.id, topic: item.topic, text: item.text, subject: item.subject,
          scope: item.scope, conditions: item.conditions || [], exceptions: item.exceptions || []
        };
      });
      var attributes = [];
      lastIntentAsks.forEach(function (ask) {
        (ask.attributes || []).forEach(function (attribute) {
          if (attribute && attributes.indexOf(attribute) < 0) attributes.push(attribute);
        });
      });
      var response = await fetch('/api/answer-facts', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ plan_id: Number(planSelect.value), question: lastIntentQuestion, facts: facts, attributes: attributes })
      });
      var data = await response.json();
      if (!response.ok) throw new Error(data.error || '回答失败');
      answerBox.innerHTML = '<div class="answer-box">' +
        '<div class="answer-title">回答（model ' + escapeHTML(data.model) + ' · ' + Math.round(data.elapsed_ms || 0) + 'ms · ' + escapeHTML(data.fact_count) + ' 条事实）</div>' +
        '<div class="answer-text">' + escapeHTML(data.answer_speech || '（未解析到话术）') + '</div>' +
        '<details class="docbox"><summary>模型原始输出</summary><pre>' + escapeHTML(data.answer_raw || '') + '</pre></details>' +
        '<details class="docbox"><summary>送给回答模型的输入（原问题 + 直播间简介 + 事实清单）</summary><pre>' + escapeHTML(data.answer_input || '') + '</pre></details>' +
        '</div>';
    } catch (err) {
      setError(err.message);
      answerBox.innerHTML = '<div class="intent-empty">回答失败</div>';
    } finally {
      running = false;
      answerBtn.disabled = false;
      answerBtn.textContent = '用这些证据回答';
    }
  }

  runBtn.addEventListener('click', runPipeline);
  refreshBtn.addEventListener('click', rebuildVectors);
  pipelineBtn.addEventListener('click', runPipeline);
  continueBtn.addEventListener('click', function () {
    if (!lastIntentAsks.length) return;
    runRecallAsks();
  });
  answerBtn.addEventListener('click', runAnswerFromFacts);
  questionInput.addEventListener('keydown', function (event) {
    if (event.key === 'Enter') runPipeline();
  });
  planSelect.addEventListener('change', function () { renderMeta(null); });
  Array.prototype.forEach.call(document.querySelectorAll('#examples .chip'), function (chip) {
    chip.addEventListener('click', function () {
      questionInput.value = chip.textContent;
      runPipeline();
    });
  });

  loadPlans();
})();
</script>
</body>
</html>
`

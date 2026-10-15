<template>
  <div class="agent-workspace-pages">
    <h3 class="workspace-title">智能体工作台</h3>
    <section class="workspace-page workspace-preview-page" aria-label="生成与预览">
      <header class="workspace-page-heading"><h3>生成与预览</h3></header>
      <div class="workspace-page-content"><slot name="preview" /></div>
      <footer class="workspace-page-footer">本场继续使用已保存快照</footer>
    </section>
  </div>
</template>

<style scoped>
.agent-workspace-pages{box-sizing:border-box;display:grid;gap:18px;width:100%;min-width:0;padding:22px;border-radius:18px;background:#f5f7fe;color:#253751;font-size:16px}
.workspace-title{margin:0;font-size:22px;font-weight:700}
.workspace-page{min-width:0}.workspace-page-heading{margin-bottom:16px}.workspace-page-heading h3{margin:0;font-size:18px;color:#26354d;font-weight:650}.workspace-page-content{display:grid;grid-template-columns:minmax(0,1fr);gap:20px;min-width:0}.workspace-page-content :deep(>*){min-width:0;max-width:100%;box-sizing:border-box}.workspace-page-footer{padding-top:20px;color:#7a8aa5;font-size:16px}
@media(max-width:700px){.agent-workspace-pages{padding:14px}}
</style>

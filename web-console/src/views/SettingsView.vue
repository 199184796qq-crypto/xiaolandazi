<script setup lang="ts">
import ModulePageNav from '../components/ModulePageNav.vue'
import {
  resetUISettings,
  setPublicScreenFontSize,
  setTableDensity,
  setTableTextSize,
  setUITextSize,
  uiSettings,
} from '../uiSettings'

function onUITextInput(event: Event) {
  const target = event.target as HTMLInputElement
  setUITextSize(Number(target.value))
}

function onTableTextInput(event: Event) {
  const target = event.target as HTMLInputElement
  setTableTextSize(Number(target.value))
}

function onPublicScreenInput(event: Event) {
  const target = event.target as HTMLInputElement
  setPublicScreenFontSize(Number(target.value))
}
</script>

<template>
  <div class="settings-page">
    <ModulePageNav context="personal-auto" active-title="个人设置" />
    <section class="feature-workspace-hero">
      <div>
        <p class="section-kicker">PREFERENCES</p>
        <h2>个人设置</h2>
      </div>
    </section>

    <div class="settings-stack">
      <section class="settings-card">
        <div class="settings-card-header">
          <div>
            <span class="section-kicker">TEXT SIZE</span>
            <h3>显示设置</h3>
          </div>
          <button class="ghost-button" type="button" @click="resetUISettings">
            恢复默认
          </button>
        </div>

        <div class="setting-row">
          <div class="setting-copy">
            <strong>整体文字大小</strong>
            <span>控制导航、标题、按钮、房间信息等控制台文字。</span>
          </div>

          <div class="setting-control">
            <div class="setting-value">{{ uiSettings.uiTextSize }} px</div>
            <input
              :value="uiSettings.uiTextSize"
              class="size-slider"
              type="range"
              min="13"
              max="20"
              step="1"
              @input="onUITextInput"
            />
            <div class="slider-labels">
              <span>小</span>
              <span>大</span>
            </div>
          </div>
        </div>

        <div class="setting-row">
          <div class="setting-copy">
            <strong>表格文字大小</strong>
            <span>单独控制库存、物流、销售、财务等数据表格，避免为了塞字段把文字做得过小。</span>
          </div>
          <div class="setting-control">
            <div class="setting-value">{{ uiSettings.tableTextSize }} px</div>
            <input
              :value="uiSettings.tableTextSize"
              class="size-slider"
              type="range"
              min="13"
              max="18"
              step="1"
              @input="onTableTextInput"
            />
            <div class="slider-labels"><span>小</span><span>大</span></div>
          </div>
        </div>

        <div class="setting-row">
          <div class="setting-copy">
            <strong>表格显示密度</strong>
            <span>只调整表格行距，不影响其他页面文字。</span>
          </div>
          <div class="setting-control density-control">
            <button type="button" :class="{ active: uiSettings.tableDensity === 'compact' }" @click="setTableDensity('compact')">紧凑</button>
            <button type="button" :class="{ active: uiSettings.tableDensity === 'standard' }" @click="setTableDensity('standard')">标准</button>
            <button type="button" :class="{ active: uiSettings.tableDensity === 'comfortable' }" @click="setTableDensity('comfortable')">宽松</button>
          </div>
        </div>
        <div class="setting-row">
          <div class="setting-copy">
            <strong>公屏文字大小</strong>
            <span>单独控制实时公屏正文，并同步放大用户名、时间和事件标签。</span>
          </div>

          <div class="setting-control">
            <div class="setting-value">
              {{ uiSettings.publicScreenFontSize }} px
            </div>
            <input
              :value="uiSettings.publicScreenFontSize"
              class="size-slider"
              type="range"
              min="16"
              max="30"
              step="1"
              @input="onPublicScreenInput"
            />
            <div class="slider-labels">
              <span>16</span>
              <span>30</span>
            </div>
          </div>
        </div>

        <div
          class="public-screen-preview"
          :style="{ '--preview-size': uiSettings.publicScreenFontSize + 'px' }"
        >
          <div class="preview-time">19:23:52</div>
          <div class="preview-type">弹幕</div>
          <div class="preview-content">
            <strong>【直播间用户】</strong>
            <p>{ 这是一条公屏文字大小预览 }</p>
          </div>
        </div>
      </section>
    </div>
  </div>
</template>
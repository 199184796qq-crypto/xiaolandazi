<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'
import { getPublicSystemConfig } from '../api'
import type { PublicSystemConfig } from '../types'

const config = ref<PublicSystemConfig>({
  site_name: '小蓝搭子',
  internal_agent_name: '小蓝工作搭子',
  client_agent_name: '小蓝直播搭子',
  live_policy_rule_title_font_size: 26,
  live_policy_rule_body_font_size: 24,
  live_policy_rule_meta_font_size: 20,
  live_policy_test_title_font_size: 22,
  live_policy_test_body_font_size: 18,
  live_policy_test_meta_font_size: 16,
  footer_enabled: true,
  footer_copyright: '© 2026 小蓝搭子',
  footer_icp_text: '',
  footer_icp_url: '',
  footer_police_text: '',
  footer_police_url: '',
  footer_report_text: '',
  footer_report_url: '',
  footer_extra_text: '',
})

async function load() {
  try {
    config.value = await getPublicSystemConfig()
  } catch {
    // Keep a minimal local fallback while the management service is restarting.
  }
}

function externalURL(value: string) {
  return value.trim() || undefined
}

onMounted(() => {
  void load()
  window.addEventListener('system-config-updated', load)
})

onUnmounted(() => {
  window.removeEventListener('system-config-updated', load)
})
</script>

<template>
  <footer v-if="config.footer_enabled" class="system-global-footer">
    <span v-if="config.footer_copyright">{{ config.footer_copyright }}</span>

    <template v-if="config.footer_icp_text">
      <span class="system-footer-divider">|</span>
      <a
        v-if="externalURL(config.footer_icp_url)"
        :href="externalURL(config.footer_icp_url)"
        target="_blank"
        rel="noopener noreferrer"
      >
        {{ config.footer_icp_text }}
      </a>
      <span v-else>{{ config.footer_icp_text }}</span>
    </template>

    <template v-if="config.footer_police_text">
      <span class="system-footer-divider">|</span>
      <a
        v-if="externalURL(config.footer_police_url)"
        :href="externalURL(config.footer_police_url)"
        target="_blank"
        rel="noopener noreferrer"
      >
        {{ config.footer_police_text }}
      </a>
      <span v-else>{{ config.footer_police_text }}</span>
    </template>

    <template v-if="config.footer_report_text">
      <span class="system-footer-divider">|</span>
      <a
        v-if="externalURL(config.footer_report_url)"
        :href="externalURL(config.footer_report_url)"
        target="_blank"
        rel="noopener noreferrer"
      >
        {{ config.footer_report_text }}
      </a>
      <span v-else>{{ config.footer_report_text }}</span>
    </template>

    <template v-if="config.footer_extra_text">
      <span class="system-footer-divider">|</span>
      <span>{{ config.footer_extra_text }}</span>
    </template>
  </footer>
</template>

<style scoped>
.system-global-footer {
  display: flex;
  width: min(1680px, calc(100% - 64px));
  min-height: 44px;
  box-sizing: border-box;
  align-items: center;
  justify-content: center;
  flex-wrap: wrap;
  gap: 7px;
  margin: 0 auto;
  padding: 12px 0 18px;
  color: #9aa3b2;
  font-size: 10px;
  font-weight: 400;
  line-height: 1.5;
  letter-spacing: .01em;
}

.system-global-footer a {
  color: #7d8da8;
  text-decoration: none;
  transition: color .16s ease;
}

.system-global-footer a:hover {
  color: #4f67cf;
}

.system-footer-divider {
  color: #ccd2dc;
}

@media (max-width: 680px) {
  .system-global-footer {
    width: calc(100% - 30px);
    padding-bottom: 78px;
    font-size: 9px;
  }
}
</style>

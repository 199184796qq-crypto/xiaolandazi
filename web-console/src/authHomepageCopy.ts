import { onMounted, ref } from 'vue'
import { getPublicSystemConfig } from './api'

export interface AuthHomepageCopy {
  customerSideLabel: string
  customerTitleLine1: string
  customerTitleLine2: string
  customerDescription: string
  customerStatusLabel: string
  internalSideLabel: string
  internalTitleLine1: string
  internalTitleLine2: string
  internalDescription: string
  internalStatusLabel: string
}

const defaults: AuthHomepageCopy = {
  customerSideLabel: 'BANBO AI LIVE',
  customerTitleLine1: 'AI直播搭子，',
  customerTitleLine2: '让你直播不再冷场。',
  customerDescription: '实时感知公屏互动，AI智能辅助话术，接待与回应，让直播间始终有人陪、有人接、有人聊。',
  customerStatusLabel: 'LIVE INTELLIGENCE ONLINE',
  internalSideLabel: 'AI CONTROL CENTER',
  internalTitleLine1: '数据驱动直播运维，',
  internalTitleLine2: '全局尽在掌握。',
  internalDescription: '连接终端、直播间与实时运行状态，让每一次运营决策都有清晰的数据依据。',
  internalStatusLabel: 'LIVE INTELLIGENCE ONLINE',
}

export function useAuthHomepageCopy() {
  const copy = ref<AuthHomepageCopy>({ ...defaults })

  async function load() {
    try {
      const config = await getPublicSystemConfig()
      copy.value = {
        customerSideLabel: config.auth_customer_side_label || defaults.customerSideLabel,
        customerTitleLine1: config.auth_customer_title_line_1 || defaults.customerTitleLine1,
        customerTitleLine2: config.auth_customer_title_line_2 || defaults.customerTitleLine2,
        customerDescription: config.auth_customer_description || defaults.customerDescription,
        customerStatusLabel: config.auth_customer_status_label || defaults.customerStatusLabel,
        internalSideLabel: config.auth_internal_side_label || defaults.internalSideLabel,
        internalTitleLine1: config.auth_internal_title_line_1 || defaults.internalTitleLine1,
        internalTitleLine2: config.auth_internal_title_line_2 || defaults.internalTitleLine2,
        internalDescription: config.auth_internal_description || defaults.internalDescription,
        internalStatusLabel: config.auth_internal_status_label || defaults.internalStatusLabel,
      }
    } catch {
      // The login and registration pages remain usable while the API restarts.
    }
  }

  onMounted(() => void load())
  return copy
}

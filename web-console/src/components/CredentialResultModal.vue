<script setup lang="ts">
import { ref } from 'vue'
import type { InitialCredential } from '../types'

const props = defineProps<{
  open: boolean
  title: string
  displayName: string
  username: string
  credential: InitialCredential | null
}>()

const emit = defineEmits<{
  close: []
}>()

const copied = ref(false)

async function copyCredential() {
  if (!props.credential) return

  const lines = [
    '小蓝搭子管理系统登录信息',
    '账户：' + props.displayName,
    '登录账号：' + props.username,
    '初始密码：' + props.credential.initial_password,
    '登录地址：' + props.credential.login_url,
    '',
    '首次登录后请立即修改初始密码。',
  ]

  try {
    await navigator.clipboard.writeText(lines.join('\n'))
    copied.value = true
    window.setTimeout(() => {
      copied.value = false
    }, 1800)
  } catch {
    copied.value = false
  }
}

function close() {
  copied.value = false
  emit('close')
}
</script>

<template>
  <Teleport to="body">
    <div
      v-if="open && credential"
      class="modal-backdrop credential-result-backdrop"
      @click.self="close"
    >
      <section class="modal-card credential-result-modal">
        <div class="modal-header">
          <div>
            <p class="section-kicker">INITIAL CREDENTIAL</p>
            <h3>{{ title }}</h3>
          </div>
          <button class="icon-button" type="button" aria-label="关闭" @click="close">
            ×
          </button>
        </div>

        <p class="modal-helper">
          初始密码由系统随机生成，明文只在本次创建结果中提供。对方首次登录后必须立即修改密码。
        </p>

        <div
          v-if="credential.delivery_method === 'email'"
          class="credential-delivery-status"
          :class="{ success: credential.email_sent, failed: !credential.email_sent }"
        >
          <strong>
            {{ credential.email_sent ? '邮件已发送' : '邮件未发送' }}
          </strong>
          <span v-if="credential.email_sent">
            登录信息已发送至 {{ credential.email }}
          </span>
          <span v-else>
            {{ credential.email_error || '请使用复制方式交付登录凭证。' }}
          </span>
        </div>

        <div class="credential-card credential-result-card">
          <div>
            <span>账户名称</span>
            <strong>{{ displayName }}</strong>
          </div>
          <div>
            <span>登录账号</span>
            <strong>{{ username }}</strong>
          </div>
          <div class="credential-password-row">
            <span>系统初始密码</span>
            <strong>{{ credential.initial_password }}</strong>
          </div>
          <div>
            <span>登录地址</span>
            <strong>{{ credential.login_url }}</strong>
          </div>
        </div>

        <div class="account-opening-note">
          <strong>安全规则</strong>
          <span>
            系统不会保存初始密码明文。关闭此窗口后无法再次查看；如未通过邮件成功送达，请先复制保存。
          </span>
        </div>

        <div class="modal-actions">
          <button class="ghost-button" type="button" @click="copyCredential">
            {{ copied ? '已复制' : '复制登录信息' }}
          </button>
          <button class="primary-button" type="button" @click="close">
            完成
          </button>
        </div>
      </section>
    </div>
  </Teleport>
</template>
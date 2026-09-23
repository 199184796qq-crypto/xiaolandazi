<script setup lang="ts">
import { useFeedbackErrorRef } from '../uiFeedback'
import PasswordInput from '../components/PasswordInput.vue'
import { computed, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { login } from '../api'
import { loadSession } from '../session'

const route = useRoute()
const router = useRouter()

const username = ref('')
const password = ref('')
const captcha = ref('')
const captchaNonce = ref(Date.now())
const submitting = ref(false)
const error = useFeedbackErrorRef()

const isAdminIntent = computed(
  () => username.value.trim().toLowerCase() === 'admin',
)

const sideLabel = computed(() =>
  isAdminIntent.value ? 'AI CONTROL CENTER' : 'BANBO AI LIVE',
)

const sideHeadline = computed(() =>
  isAdminIntent.value
    ? '数据驱动直播运维，全局尽在掌握。'
    : '直播搭子，让你直播不再冷场。',
)

const sideDescription = computed(() =>
  isAdminIntent.value
    ? '连接终端、直播间与实时运行状态，让每一次运营决策都有清晰的数据依据。'
    : '实时感知公屏互动，智能辅助接待与回应，让直播间始终有人陪、有人接、有人聊。',
)

const captchaSrc = computed(
  () => '/api/v1/auth/captcha?t=' + captchaNonce.value,
)

function refreshCaptcha() {
  captcha.value = ''
  captchaNonce.value = Date.now()
}

async function submit() {
  if (submitting.value) return

  submitting.value = true
  error.value = ''

  try {
    await login({
      username: username.value.trim(),
      password: password.value,
      captcha: captcha.value.trim(),
    })
    await loadSession()

    const redirect =
      typeof route.query.redirect === 'string' && route.query.redirect.startsWith('/')
        ? route.query.redirect
        : '/'

    await router.replace(redirect)
  } catch (value) {
    error.value = value instanceof Error ? value.message : '登录失败'
    refreshCaptcha()
  } finally {
    submitting.value = false
  }
}
</script>

<template>
  <main class="auth-page login-auth-page">
    <section class="auth-panel">
      <div class="auth-panel-inner">
        <div class="auth-brand">
          <div class="auth-brand-mark">伴</div>
          <div>
            <strong>小蓝搭子</strong>
            <span>BANBO AI</span>
          </div>
        </div>

        <div class="auth-heading">
          <p class="section-kicker">WELCOME BACK</p>
          <h1>登录后台</h1>
          <p>使用您的账号登录小蓝搭子智能直播系统。</p>
        </div>

        <form class="auth-form" @submit.prevent="submit">
          <label>
            <span>账号</span>
            <input
              v-model="username"
              type="text"
              autocomplete="username"
              maxlength="32"
              placeholder="请输入登录账号"
              required
            />
          </label>

          <label>
            <span>密码</span>
            <PasswordInput
              v-model="password"
              autocomplete="current-password"
              maxlength="72"
              placeholder="请输入密码"
              required
            />
          </label>

          <label>
            <span>图片验证码</span>
            <div class="captcha-row">
              <input
                v-model="captcha"
                type="text"
                inputmode="numeric"
                autocomplete="off"
                maxlength="5"
                placeholder="输入图中数字"
                required
              />
              <button
                class="captcha-image-button"
                type="button"
                title="点击刷新验证码"
                @click="refreshCaptcha"
              >
                <img :src="captchaSrc" alt="数字验证码" />
              </button>
            </div>
          </label>

          <p v-if="error" class="auth-error">{{ error }}</p>

          <button class="auth-submit" type="submit" :disabled="submitting">
            {{ submitting ? '正在登录...' : '登录' }}
          </button>
        </form>

        <div class="auth-footer">
          <span>已有邀请码？</span>
          <RouterLink to="/register">使用邀请码注册</RouterLink>
        </div>
      </div>
    </section>

    <aside class="auth-side">
      <div class="auth-tech-grid"></div>
      <div class="auth-orb auth-orb-one"></div>
      <div class="auth-orb auth-orb-two"></div>
      <div class="auth-tech-ring auth-tech-ring-one"></div>
      <div class="auth-tech-ring auth-tech-ring-two"></div>

      <div class="auth-side-content">
        <span class="auth-side-label">{{ sideLabel }}</span>
        <h2 v-if="isAdminIntent">{{ sideHeadline }}</h2>
        <h2 v-else class="customer-tagline">
          <span>直播搭子，</span>
          <span>让你直播不再冷场。</span>
        </h2>
        <p>{{ sideDescription }}</p>

        <div class="auth-signal-row">
          <span></span>
          <span></span>
          <span></span>
          <em>LIVE INTELLIGENCE ONLINE</em>
        </div>
      </div>
    </aside>
  </main>
</template>
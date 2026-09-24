<script setup lang="ts">
import { useFeedbackErrorRef } from '../uiFeedback'
import PasswordInput from '../components/PasswordInput.vue'
import SystemFooter from '../components/SystemFooter.vue'
import { computed, onUnmounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { login, loginWithSMS, sendSMSLoginCode } from '../api'
import { applySession } from '../session'

const route = useRoute()
const router = useRouter()

type LoginMode = 'account' | 'mobile'

const loginMode = ref<LoginMode>('account')
const username = ref('')
const password = ref('')
const captcha = ref('')
const captchaNonce = ref(Date.now())
const phone = ref('')
const smsCode = ref('')
const sliderOpen = ref(false)
const sliderProgress = ref(0)
const sliderPassed = ref(false)
const notice = ref('')
const submitting = ref(false)
const smsSending = ref(false)
const smsCountdown = ref(0)
let smsTimer: number | undefined
const error = useFeedbackErrorRef()

const isInternalIntent = computed(() => {
  const portal = String(route.query.portal || '').trim().toLowerCase()
  return (
    portal === 'internal' ||
    portal === 'staff' ||
    username.value.trim().toLowerCase() === 'admin'
  )
})

const sideLabel = computed(() =>
  isInternalIntent.value ? 'AI CONTROL CENTER' : 'BANBO AI LIVE',
)

const sideDescription = computed(() =>
  isInternalIntent.value
    ? '连接终端、直播间与实时运行状态，让每一次运营决策都有清晰的数据依据。'
    : '实时感知公屏互动，AI智能辅助话术，接待与回应，让直播间始终有人陪、有人接、有人聊。',
)

const captchaSrc = computed(
  () => '/api/v1/auth/captcha?t=' + captchaNonce.value,
)

function refreshCaptcha() {
  captcha.value = ''
  captchaNonce.value = Date.now()
}

const headingDescription = computed(() => {
  if (loginMode.value === 'mobile') return '仅限已注册账号，使用已绑定的唯一手机号接收短信验证码登录。'
  return '使用您的账号登录小蓝搭子智能直播系统。'
})

function setLoginMode(mode: LoginMode) {
  loginMode.value = mode
  error.value = ''
  notice.value = ''
}

function normalizePhone(event: Event) {
  const target = event.target as HTMLInputElement
  phone.value = target.value.replace(/\D/g, '').slice(0, 11)
  target.value = phone.value
}

function validatePhone() {
  if (!/^1\d{10}$/.test(phone.value)) {
    error.value = '请输入正确的中国大陆手机号码：11 位数字并以 1 开头。'
    return false
  }
  return true
}

function startSmsCountdown(seconds: number) {
  smsCountdown.value = Math.max(0, Math.floor(seconds))
  if (smsTimer !== undefined) window.clearInterval(smsTimer)
  if (smsCountdown.value <= 0) return
  smsTimer = window.setInterval(() => {
    smsCountdown.value = Math.max(0, smsCountdown.value - 1)
    if (smsCountdown.value <= 0 && smsTimer !== undefined) {
      window.clearInterval(smsTimer)
      smsTimer = undefined
    }
  }, 1000)
}

onUnmounted(() => {
  if (smsTimer !== undefined) window.clearInterval(smsTimer)
})

function requestSmsVerification() {
  error.value = ''
  notice.value = ''
  if (smsSending.value || smsCountdown.value > 0) return
  if (!validatePhone()) return
  sliderProgress.value = 0
  sliderPassed.value = false
  sliderOpen.value = true
}

function handleSliderInput(event: Event) {
  const value = Number((event.target as HTMLInputElement).value || 0)
  sliderProgress.value = value
  if (value >= 98) {
    sliderProgress.value = 100
    sliderPassed.value = true
  }
}

function handleSliderChange() {
  if (!sliderPassed.value) sliderProgress.value = 0
}

function closeSliderVerification() {
  sliderOpen.value = false
  sliderProgress.value = 0
  sliderPassed.value = false
}

async function confirmSmsVerification() {
  if (!sliderPassed.value || smsSending.value) return
  const targetPhone = phone.value
  closeSliderVerification()
  smsSending.value = true
  error.value = ''
  notice.value = ''
  try {
    const result = await sendSMSLoginCode(targetPhone)
    startSmsCountdown(result.retry_after_seconds || 60)
    if (result.debug_code) {
      smsCode.value = result.debug_code
      notice.value = `开发环境验证码：${result.debug_code}`
    } else {
      notice.value = `验证码已发送至 ${result.phone}`
    }
  } catch (value) {
    error.value = value instanceof Error ? value.message : '发送验证码失败'
  } finally {
    smsSending.value = false
  }
}

async function submitMobile() {
  if (submitting.value) return
  error.value = ''
  if (!validatePhone()) return
  if (!/^\d{6}$/.test(smsCode.value.trim())) {
    error.value = '请输入 6 位短信验证码。'
    return
  }

  submitting.value = true
  try {
    const bootstrap = await loginWithSMS({
      phone: phone.value,
      code: smsCode.value.trim(),
    })
    applySession(bootstrap)
    const redirect =
      typeof route.query.redirect === 'string' && route.query.redirect.startsWith('/')
        ? route.query.redirect
        : '/'
    await router.replace(redirect)
  } catch (value) {
    error.value = value instanceof Error ? value.message : '手机登录失败'
  } finally {
    submitting.value = false
  }
}

async function submit() {
  if (submitting.value) return

  submitting.value = true
  error.value = ''

  try {
    const bootstrap = await login({
      username: username.value.trim(),
      password: password.value,
      captcha: captcha.value.trim(),
    })
    applySession(bootstrap)

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
          <div class="auth-brand-mark">蓝</div>
          <div>
            <span>BANBO AI SYSTEM</span>
            <strong>小蓝搭子</strong>
          </div>
        </div>

        <div class="auth-heading">
          <p class="section-kicker">WELCOME BACK</p>
          <h1>登录后台</h1>
          <p>{{ headingDescription }}</p>
        </div>

        <div
          class="auth-login-mode-switch"
          :class="{ 'is-account': loginMode === 'account', 'is-mobile': loginMode === 'mobile' }"
        >
          <span class="auth-login-mode-slider"></span>
          <button
            type="button"
            :class="{ active: loginMode === 'account' }"
            @click="setLoginMode('account')"
          >账户登录</button>
          <button
            type="button"
            :class="{ active: loginMode === 'mobile' }"
            @click="setLoginMode('mobile')"
          >短信登录</button>
        </div>

        <p v-if="notice" class="auth-mode-notice">{{ notice }}</p>

        <form v-if="loginMode === 'account'" class="auth-form" @submit.prevent="submit">
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

        <form v-else class="auth-form auth-mobile-form" @submit.prevent="submitMobile">
          <label>
            <span>手机号码</span>
            <input
              :value="phone"
              type="tel"
              inputmode="numeric"
              autocomplete="tel"
              maxlength="11"
              placeholder="请输入手机号码"
              @input="normalizePhone"
            />
          </label>

          <label>
            <span>短信验证码</span>
            <div class="auth-sms-code-row">
              <input
                v-model="smsCode"
                type="text"
                inputmode="numeric"
                autocomplete="one-time-code"
                maxlength="6"
                placeholder="请输入验证码"
              />
              <button
                type="button"
                :disabled="smsSending || smsCountdown > 0"
                @click="requestSmsVerification"
              >
                {{ smsSending ? '发送中...' : smsCountdown > 0 ? `${smsCountdown}s 后重发` : '发送验证码' }}
              </button>
            </div>
          </label>

          <p v-if="error" class="auth-error">{{ error }}</p>

          <button class="auth-submit" type="submit" :disabled="submitting">
            {{ submitting ? '正在登录...' : '短信验证登录' }}
          </button>
        </form>

        <div class="auth-footer">
          <span>已有邀请码？</span>
          <RouterLink to="/register">使用邀请码注册</RouterLink>
        </div>
      </div>
    </section>

    <div v-if="sliderOpen" class="auth-slider-mask" @click.self="closeSliderVerification">
      <section class="auth-slider-dialog" role="dialog" aria-modal="true" aria-label="滑动图片验证">
        <header>
          <div>
            <span>SECURITY CHECK</span>
            <h3>完成滑动验证</h3>
          </div>
          <button type="button" aria-label="关闭" @click="closeSliderVerification">×</button>
        </header>

        <div class="auth-slider-picture">
          <div class="auth-slider-picture-grid"></div>
          <div class="auth-slider-target"></div>
          <div
            class="auth-slider-piece"
            :style="{ left: 'calc(' + Math.min(sliderProgress, 82) + '% - 22px)' }"
          ></div>
          <span>拖动滑块，让拼图块移动到缺口位置</span>
        </div>

        <div class="auth-slider-control" :class="{ passed: sliderPassed }">
          <span class="auth-slider-progress" :style="{ width: sliderProgress + '%' }"></span>
          <span class="auth-slider-guide">{{ sliderPassed ? '验证成功' : '按住滑块，向右拖动完成验证' }}</span>
          <input
            :value="sliderProgress"
            type="range"
            min="0"
            max="100"
            step="1"
            aria-label="拖动滑块完成验证"
            @input="handleSliderInput"
            @change="handleSliderChange"
          />
        </div>

        <button
          class="auth-slider-confirm"
          type="button"
          :disabled="!sliderPassed"
          @click="confirmSmsVerification"
        >
          {{ sliderPassed ? '发送验证码' : '请先完成验证' }}
        </button>
      </section>
    </div>

    <aside class="auth-side">
      <div class="auth-tech-grid"></div>
      <div class="auth-orb auth-orb-one"></div>
      <div class="auth-orb auth-orb-two"></div>
      <div class="auth-tech-ring auth-tech-ring-one"></div>
      <div class="auth-tech-ring auth-tech-ring-two"></div>

      <div class="auth-side-content">
        <span class="auth-side-label">{{ sideLabel }}</span>
        <h2 class="customer-tagline">
          <template v-if="isInternalIntent">
            <span>数据驱动直播运维，</span>
            <span>全局尽在掌握。</span>
          </template>
          <template v-else>
            <span>AI直播搭子，</span>
            <span>让你直播不再冷场。</span>
          </template>
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

    <SystemFooter class="auth-site-footer" />
  </main>
</template>
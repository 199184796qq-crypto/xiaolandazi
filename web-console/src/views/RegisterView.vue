<script setup lang="ts">
import { useFeedbackErrorRef } from '../uiFeedback'
import PasswordInput from '../components/PasswordInput.vue'
import SystemFooter from '../components/SystemFooter.vue'
import { computed, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { getInvitePreview, register } from '../api'
import { applySession } from '../session'
import type { InvitePreview } from '../types'
import { useAuthHomepageCopy } from '../authHomepageCopy'

const route = useRoute()
const router = useRouter()

const username = ref('')
const phone = ref('')
const password = ref('')
const confirmPassword = ref('')
const inviteCode = ref(
  typeof route.query.invite === 'string' ? route.query.invite.toUpperCase() : '',
)
const invitePreview = ref<InvitePreview | null>(null)
const inviteChecking = ref(false)
const inviteError = ref('')
const captcha = ref('')
const captchaNonce = ref(Date.now())
const submitting = ref(false)
const error = useFeedbackErrorRef()
const authCopy = useAuthHomepageCopy()

const captchaSrc = computed(
  () => '/api/v1/auth/captcha?t=' + captchaNonce.value,
)

function refreshCaptcha() {
  captcha.value = ''
  captchaNonce.value = Date.now()
}

function sourceLabel(source: string) {
  if (source === 'platform_invite') return '平台邀请'
  if (source === 'agent_invite') return '代理邀请'
  if (source === 'referral') return '终端推荐'
  return source
}

async function checkInvite() {
  const code = inviteCode.value.trim().toUpperCase()
  invitePreview.value = null
  inviteError.value = ''

  if (!code) {
    inviteError.value = '请输入邀请码'
    return false
  }

  inviteChecking.value = true
  try {
    invitePreview.value = await getInvitePreview(code)
    inviteCode.value = code
    return true
  } catch (value) {
    inviteError.value =
      value instanceof Error ? value.message : '邀请码无效、已停用或已过期'
    return false
  } finally {
    inviteChecking.value = false
  }
}

async function submit() {
  if (submitting.value) return

  if (!phone.value.trim()) {
    error.value = '请填写联系电话'
    return
  }
  if (password.value !== confirmPassword.value) {
    error.value = '两次输入的密码不一致'
    return
  }

  const inviteOK = await checkInvite()
  if (!inviteOK) {
    error.value = '请先填写有效邀请码'
    return
  }

  submitting.value = true
  error.value = ''

  try {
    const bootstrap = await register({
      username: username.value.trim(),
      phone: phone.value.trim(),
      password: password.value,
      confirm_password: confirmPassword.value,
      invite_code: inviteCode.value.trim().toUpperCase(),
      captcha: captcha.value.trim(),
    })

    applySession(bootstrap)
    await router.replace('/')
  } catch (value) {
    error.value = value instanceof Error ? value.message : '注册失败'
    refreshCaptcha()
  } finally {
    submitting.value = false
  }
}

if (inviteCode.value) {
  void checkInvite()
}
</script>

<template>
  <main class="auth-page login-auth-page register-auth-page">
    <section class="auth-panel register-panel">
      <div class="auth-panel-inner">
        <div class="auth-brand">
          <div class="auth-brand-mark">蓝</div>
          <div>
            <span>BANBO AI SYSTEM</span>
            <strong>小蓝搭子</strong>
          </div>
        </div>

        <div class="auth-heading">
          <p class="section-kicker">INVITATION REGISTRATION</p>
          <h1>邀请码注册</h1>
        </div>

        <form class="auth-form" @submit.prevent="submit">
          <label>
            <span>邀请码 <em class="required-mark">*</em></span>
            <div class="invite-input-row">
              <input
                v-model="inviteCode"
                type="text"
                maxlength="32"
                autocomplete="off"
                placeholder="输入邀请码"
                required
                @input="invitePreview = null; inviteError = ''"
                @blur="checkInvite"
              />
              <button
                class="invite-check-button"
                type="button"
                :disabled="inviteChecking"
                @click="checkInvite"
              >
                {{ inviteChecking ? '校验中' : '校验' }}
              </button>
            </div>
          </label>

          <div v-if="invitePreview" class="invite-preview-success">
            <strong>邀请码有效</strong>
            <span>
              {{ sourceLabel(invitePreview.source_type) }} ·
              {{ invitePreview.inviter_name }}
            </span>
          </div>
          <p v-if="inviteError" class="invite-preview-error">{{ inviteError }}</p>

          <label>
            <span>联系电话 <em class="required-mark">*</em></span>
            <input
              v-model="phone"
              type="text"
              autocomplete="tel"
              required
            />
          </label>

          <label>
            <span>登录账号</span>
            <input
              v-model="username"
              type="text"
              autocomplete="username"
              maxlength="32"
              placeholder="4-32 位字母、数字或 _ . -"
              required
            />
          </label>

          <div class="auth-form-grid register-password-grid">
            <label>
              <span>密码</span>
              <PasswordInput
                v-model="password"
                autocomplete="new-password"
                maxlength="72"
                placeholder="至少 8 位"
                required
              />
            </label>

            <label>
              <span>确认密码</span>
              <PasswordInput
                v-model="confirmPassword"
                autocomplete="new-password"
                maxlength="72"
                placeholder="再次输入密码"
                required
              />
            </label>
          </div>

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
            {{ submitting ? '正在注册...' : '注册并进入终端工作台' }}
          </button>
        </form>

        <div class="auth-footer">
          <span>已有账号？</span>
          <RouterLink to="/login?portal=user">返回登录</RouterLink>
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
        <span class="auth-side-label">{{ authCopy.customerSideLabel }}</span>
        <h2 class="customer-tagline">
          <span>{{ authCopy.customerTitleLine1 }}</span>
          <span>{{ authCopy.customerTitleLine2 }}</span>
        </h2>
        <p>
          邀请注册会自动记录终端来源、推荐人和代理归属，
          后续奖励、返佣和结算都可以沿着这条关系追溯。
        </p>

        <div class="auth-signal-row">
          <span></span>
          <span></span>
          <span></span>
          <em>{{ authCopy.customerStatusLabel }}</em>
        </div>
      </div>
    </aside>

    <SystemFooter class="auth-site-footer" />
  </main>
</template>

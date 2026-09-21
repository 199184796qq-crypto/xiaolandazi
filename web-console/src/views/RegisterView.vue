<script setup lang="ts">
import { computed, ref } from 'vue'
import { useRouter } from 'vue-router'
import { register } from '../api'
import { loadSession } from '../session'

const router = useRouter()

const username = ref('')
const displayName = ref('')
const password = ref('')
const confirmPassword = ref('')
const captcha = ref('')
const captchaNonce = ref(Date.now())
const submitting = ref(false)
const error = ref('')

const captchaSrc = computed(
  () => '/api/v1/auth/captcha?t=' + captchaNonce.value,
)

function refreshCaptcha() {
  captcha.value = ''
  captchaNonce.value = Date.now()
}

async function submit() {
  if (submitting.value) return

  if (password.value !== confirmPassword.value) {
    error.value = '两次输入的密码不一致'
    return
  }

  submitting.value = true
  error.value = ''

  try {
    await register({
      username: username.value.trim(),
      display_name: displayName.value.trim(),
      password: password.value,
      confirm_password: confirmPassword.value,
      captcha: captcha.value.trim(),
    })

    await loadSession()
    await router.replace('/')
  } catch (value) {
    error.value = value instanceof Error ? value.message : '注册失败'
    refreshCaptcha()
  } finally {
    submitting.value = false
  }
}
</script>

<template>
  <main class="auth-page">
    <section class="auth-panel">
      <div class="auth-brand">
        <div class="auth-brand-mark">伴</div>
        <div>
          <strong>小蓝搭子</strong>
          <span>BANBO AI</span>
        </div>
      </div>

      <div class="auth-heading">
        <p class="section-kicker">CREATE ACCOUNT</p>
        <h1>注册客户账号</h1>
        <p>注册成功后会自动创建独立客户空间，只能访问自己的直播间数据。</p>
      </div>

      <form class="auth-form" @submit.prevent="submit">
        <label>
          <span>客户名称</span>
          <input
            v-model="displayName"
            type="text"
            maxlength="64"
            placeholder="例如：南充杨鸭子"
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

        <div class="auth-form-grid">
          <label>
            <span>密码</span>
            <input
              v-model="password"
              type="password"
              autocomplete="new-password"
              maxlength="72"
              placeholder="至少 8 位"
              required
            />
          </label>

          <label>
            <span>确认密码</span>
            <input
              v-model="confirmPassword"
              type="password"
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
          {{ submitting ? '正在注册...' : '注册并进入客户工作台' }}
        </button>
      </form>

      <div class="auth-footer">
        <span>已有账号？</span>
        <RouterLink to="/login">返回登录</RouterLink>
      </div>
    </section>

    <aside class="auth-side">
      <div>
        <span class="auth-side-label">CUSTOMER WORKSPACE</span>
        <h2>每个客户独立数据范围。</h2>
        <p>
          注册账号默认是客户角色，不具备平台管理权限，无法查看其他客户的数据。
        </p>
      </div>

      <div class="auth-security-note">
        <strong>账户规则</strong>
        <span>客户账号不可自行提升为管理员 · 密码使用 bcrypt 加密保存</span>
      </div>
    </aside>
  </main>
</template>
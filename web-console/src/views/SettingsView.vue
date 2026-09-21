<script setup lang="ts">
import { computed, ref } from 'vue'
import { changePassword } from '../api'
import { session } from '../session'
import {
  resetUISettings,
  setPublicScreenFontSize,
  setUITextSize,
  uiSettings,
} from '../uiSettings'

const currentPassword = ref('')
const newPassword = ref('')
const confirmPassword = ref('')
const passwordSubmitting = ref(false)
const passwordError = ref('')
const passwordSuccess = ref('')

const actor = computed(() => session.bootstrap?.actor)

function onUITextInput(event: Event) {
  const target = event.target as HTMLInputElement
  setUITextSize(Number(target.value))
}

function onPublicScreenInput(event: Event) {
  const target = event.target as HTMLInputElement
  setPublicScreenFontSize(Number(target.value))
}

async function submitPasswordChange() {
  if (passwordSubmitting.value) return

  passwordError.value = ''
  passwordSuccess.value = ''

  if (newPassword.value !== confirmPassword.value) {
    passwordError.value = '两次输入的新密码不一致'
    return
  }

  passwordSubmitting.value = true
  try {
    await changePassword({
      current_password: currentPassword.value,
      new_password: newPassword.value,
      confirm_password: confirmPassword.value,
    })

    currentPassword.value = ''
    newPassword.value = ''
    confirmPassword.value = ''
    passwordSuccess.value = '密码修改成功，当前登录状态已安全更新。'
  } catch (value) {
    passwordError.value =
      value instanceof Error ? value.message : '修改密码失败'
  } finally {
    passwordSubmitting.value = false
  }
}
</script>

<template>
  <div class="settings-page">
    <section class="page-hero settings-hero">
      <div>
        <p class="section-kicker">PREFERENCES</p>
        <h2>设置</h2>
        <p>调整后台显示效果和当前账号的安全设置。</p>
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
            <span>控制导航、标题、按钮、房间信息等后台文字。</span>
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

      <section class="settings-card">
        <div class="settings-card-header">
          <div>
            <span class="section-kicker">ACCOUNT SECURITY</span>
            <h3>账号安全</h3>
          </div>
          <span class="account-role-badge">
            {{
              actor?.role === 'platform_admin'
                ? '平台管理员'
                : '客户账号'
            }}
          </span>
        </div>

        <div class="account-summary">
          <div>
            <span>当前账号</span>
            <strong>{{ actor?.username || '-' }}</strong>
          </div>
          <div>
            <span>显示名称</span>
            <strong>{{ actor?.display_name || '-' }}</strong>
          </div>
        </div>

        <form class="security-form" @submit.prevent="submitPasswordChange">
          <label>
            <span>当前密码</span>
            <input
              v-model="currentPassword"
              type="password"
              autocomplete="current-password"
              maxlength="72"
              placeholder="请输入当前密码"
              required
            />
          </label>

          <div class="security-form-grid">
            <label>
              <span>新密码</span>
              <input
                v-model="newPassword"
                type="password"
                autocomplete="new-password"
                maxlength="72"
                placeholder="8-72 位"
                required
              />
            </label>

            <label>
              <span>确认新密码</span>
              <input
                v-model="confirmPassword"
                type="password"
                autocomplete="new-password"
                maxlength="72"
                placeholder="再次输入新密码"
                required
              />
            </label>
          </div>

          <p v-if="passwordError" class="auth-error">
            {{ passwordError }}
          </p>
          <p v-if="passwordSuccess" class="settings-success">
            {{ passwordSuccess }}
          </p>

          <div class="security-actions">
            <span>修改成功后会注销该账号的其他登录会话。</span>
            <button
              class="primary-button"
              type="submit"
              :disabled="passwordSubmitting"
            >
              {{
                passwordSubmitting
                  ? '正在修改...'
                  : '修改密码'
              }}
            </button>
          </div>
        </form>
      </section>
    </div>
  </div>
</template>
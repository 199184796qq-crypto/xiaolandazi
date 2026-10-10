<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import type { CustomerShopOrder } from '../types'

const props = defineProps<{ order: CustomerShopOrder; spec: string; paying: boolean; error: string }>()
const emit = defineEmits<{ close: []; confirm: [] }>()
const free = computed(() => props.order.payable_amount_cents === 0)
const dialog = ref<HTMLDialogElement | null>(null)
const cancelButton = ref<HTMLButtonElement | null>(null)
let previousFocus: HTMLElement | null = null
let previousOverflow = ''
onMounted(() => {
  previousFocus = document.activeElement as HTMLElement | null
  previousOverflow = document.body.style.overflow
  document.body.style.overflow = 'hidden'
  dialog.value?.showModal()
  cancelButton.value?.focus({ preventScroll: true })
})
onBeforeUnmount(() => {
  dialog.value?.close()
  document.body.style.overflow = previousOverflow
  if (previousFocus?.isConnected) previousFocus.focus({ preventScroll: true })
})
function close() { if (!props.paying) emit('close') }
function confirm() { if (!props.paying) emit('confirm') }
function onBackdropClick(event: MouseEvent) {
  if (event.target !== dialog.value || !dialog.value) return
  const bounds = dialog.value.getBoundingClientRect()
  if (event.clientX < bounds.left || event.clientX > bounds.right || event.clientY < bounds.top || event.clientY > bounds.bottom) close()
}
function onKeydown(event: KeyboardEvent) {
  if (event.key !== 'Tab') return
  const buttons = Array.from(dialog.value?.querySelectorAll<HTMLButtonElement>('button:not(:disabled)') || [])
  const first = buttons[0], last = buttons[buttons.length - 1]
  if (!first) { event.preventDefault(); return }
  if (event.shiftKey && (document.activeElement === first || document.activeElement === dialog.value)) { event.preventDefault(); last?.focus() }
  else if (!event.shiftKey && (document.activeElement === last || document.activeElement === dialog.value)) { event.preventDefault(); first.focus() }
}
</script>

<template>
  <Teleport to="body">
    <dialog ref="dialog" class="wallet-payment-dialog" tabindex="-1" aria-labelledby="wallet-payment-title" aria-describedby="wallet-payment-description" :aria-busy="paying" @cancel.prevent="close" @click="onBackdropClick" @keydown="onKeydown">
      <header class="wallet-payment-header">
        <div class="wallet-payment-heading">
          <span>WALLET PAYMENT</span>
          <h2 id="wallet-payment-title">钱包余额支付</h2>
          <p id="wallet-payment-description">{{ free ? '本次商品免费领取，确认后即可获得相应权益，无需扣除钱包余额。' : '本次购买将使用钱包余额进行支付，确认后时长卡将放入卡包。' }}</p>
        </div>
        <div class="wallet-payment-art" aria-hidden="true">
          <p>余额支付<br />安心便捷</p>
          <svg viewBox="0 0 290 210">
            <defs>
              <linearGradient id="wallet-face" x1="0" y1="0" x2="1" y2="1"><stop stop-color="#b5c7ff" /><stop offset=".48" stop-color="#8b9df9" /><stop offset="1" stop-color="#6565e6" /></linearGradient>
              <linearGradient id="wallet-edge" x1="0" y1="0" x2="1" y2="1"><stop stop-color="#889bff" /><stop offset="1" stop-color="#4f50db" /></linearGradient>
              <linearGradient id="wallet-coin" x1="0" y1="0" x2="1" y2="1"><stop stop-color="#fff2b3" /><stop offset=".6" stop-color="#ffcc6c" /><stop offset="1" stop-color="#f5a64e" /></linearGradient>
              <linearGradient id="wallet-strap" x1="0" y1="0" x2="1" y2="1"><stop stop-color="#c4d1ff" /><stop offset="1" stop-color="#7774f3" /></linearGradient>
            </defs>
            <ellipse cx="170" cy="184" rx="82" ry="14" fill="#778ae9" opacity=".1" />
            <g transform="rotate(12 163 112)">
              <path d="M81 56Q82 41 101 45l133 21q13 2 13 17v96q0 17-15 16L91 173z" fill="url(#wallet-edge)" />
              <path d="m99 51 127 20q8 2 10 8l-143-23z" fill="#cad8ff" />
              <path d="m100 59 122 19" stroke="#8ca7f7" stroke-width="7" stroke-linecap="round" />
              <ellipse cx="168" cy="51" rx="35" ry="39" fill="#eeb16b" />
              <ellipse cx="162" cy="49" rx="35" ry="39" fill="url(#wallet-coin)" stroke="#fff2bf" stroke-width="3" />
              <ellipse cx="162" cy="49" rx="27" ry="31" fill="none" stroke="#ffdda0" stroke-width="3" />
              <path d="m151 34 12 14 12-14M163 48v24M151 52h24M151 61h24" fill="none" stroke="#fff" stroke-width="5" stroke-linecap="round" stroke-linejoin="round" />
              <path d="M80 64q0-10 13-8l131 20q12 2 12 15v94q0 15-14 13L81 176q-12-2-12-14z" fill="url(#wallet-face)" stroke="#bbcaff" stroke-width="2" />
              <path d="m92 64 113 18" fill="none" stroke="#d5dfff" stroke-width="3" stroke-linecap="round" stroke-dasharray="8 4" opacity=".7" />
              <path d="m219 112 20 3q13 2 13 14v20q0 12-13 10l-20-3q-24-4-24-25 0-23 24-19" fill="url(#wallet-strap)" stroke="#c1caff" stroke-width="2" />
              <circle cx="219" cy="135" r="11" fill="#d7e1ff" /><circle cx="217" cy="132" r="8" fill="#f3f5ff" opacity=".7" />
            </g>
            <path d="m46 46 3 9 8 3-8 3-3 9-3-9-8-3 8-3M265 165l3 7 6 2-6 2-3 7-2-7-7-2 7-2" fill="#a8bfff" /><path d="m260 75 3 8 7 2-7 3-3 8-3-8-7-3 7-2" fill="#ffe09b" />
          </svg>
        </div>
        <button class="wallet-payment-close" type="button" aria-label="关闭钱包余额支付" :disabled="paying" @click="close"><svg viewBox="0 0 24 24" aria-hidden="true"><path d="m6 6 12 12M18 6 6 18" /></svg></button>
      </header>
      <section class="wallet-payment-summary" aria-label="订单信息">
        <div class="wallet-info-card is-order"><i aria-hidden="true"><svg viewBox="0 0 24 24"><path d="M6 3h9l4 4v14H6zM14 3v5h5M9 12h7M9 16h5" /></svg></i><div><span>订单号</span><strong :title="order.order_no">{{ order.order_no }}</strong></div></div>
        <div class="wallet-info-card is-product"><i aria-hidden="true"><svg viewBox="0 0 24 24"><path d="M4 8h16l2 13H2zM8 9V6a4 4 0 0 1 8 0v3" /></svg></i><div><span>商品</span><strong :title="order.items[0]?.product_name || '小蓝商城商品'">{{ order.items[0]?.product_name || '小蓝商城商品' }}</strong></div></div>
        <div class="wallet-info-card is-duration"><i aria-hidden="true"><svg viewBox="0 0 24 24"><circle cx="12" cy="12" r="9" /><path d="M12 6v7l4 2" /></svg></i><div><span>{{ order.order_type === 'device' ? '购买数量' : '购买时长' }}</span><strong :title="spec">{{ spec }}</strong></div></div>
        <div class="wallet-info-card is-amount"><i aria-hidden="true"><svg viewBox="0 0 24 24"><ellipse cx="12" cy="5" rx="9" ry="3" /><path d="M3 5v5c0 4 18 4 18 0V5M3 10v5c0 4 18 4 18 0v-5M3 15v4c0 4 18 4 18 0v-4" /></svg></i><div><span>应付金额</span><strong>¥{{ (order.payable_amount_cents / 100).toFixed(2) }}</strong></div></div>
      </section>
      <div v-if="!free" class="wallet-payment-card-note"><strong>时长卡领取后手动启用</strong><p>支付成功后，卡片进入“AI 时长 → 时长卡包”，不会立即计入 AI 时长。有效期从手动启用时开始，到期后未用完的时长会失效。</p></div>
      <p v-if="error" class="wallet-payment-error" role="alert">{{ error }}</p>
      <footer class="wallet-payment-footer">
        <p><svg viewBox="0 0 24 24" aria-hidden="true"><path d="m12 2 9 4v6c0 5-9 10-9 10S3 17 3 12V6z" /><path d="m7 12 3 3 6-6" /></svg><span>{{ free ? '免费领取权益，无需扣除钱包余额' : '使用钱包余额支付，安心便捷' }}</span></p>
        <div><button ref="cancelButton" class="wallet-payment-cancel" type="button" :disabled="paying" @click="close">取消</button><button class="wallet-payment-confirm" type="button" :disabled="paying" @click="confirm"><svg viewBox="0 0 24 24" aria-hidden="true"><template v-if="free"><path d="M3 8h18v4H3zM5 12v9h14v-9M12 8v13" /><path d="M12 8H8a3 3 0 1 1 3-3l1 3h4a3 3 0 1 0-3-3z" /></template><template v-else><path d="M3 5h17v15H3zM3 5l14-3v3M20 10h-6v6h6" /><circle cx="17" cy="13" r=".7" /></template></svg>{{ paying ? (free ? '正在领取…' : '正在从钱包扣款…') : (free ? '确认免费领取' : '确认支付 ¥' + (order.payable_amount_cents / 100).toFixed(2)) }}</button></div>
      </footer>
    </dialog>
  </Teleport>
</template>

<style scoped>
.wallet-payment-dialog{box-sizing:border-box;width:min(1180px,calc(100vw - 64px));max-width:none;max-height:calc(100dvh - 48px);margin:auto;padding:0;border:1px solid #e4eaff;border-radius:25px;background:linear-gradient(120deg,#fff,#fafcff 58%,#f7f9ff);color:#172850;font:inherit;box-shadow:0 28px 100px #1d315047;overflow:auto}
.wallet-payment-dialog::backdrop{background:#27344c75;backdrop-filter:blur(7px)}
.wallet-payment-header{position:relative;display:flex;align-items:center;justify-content:space-between;gap:20px;min-height:205px;padding:34px 48px 26px;overflow:hidden;background:radial-gradient(ellipse at 85% 7%,#d7e5ff8f,transparent 54%),linear-gradient(120deg,#f1f6ff8c,#fff0)}
.wallet-payment-heading{position:relative;z-index:1;flex:1;min-width:0;max-width:740px;padding-right:10px}
.wallet-payment-heading>span{color:#9aa9d4;font-size:14px;font-weight:800;letter-spacing:.24em}
.wallet-payment-heading h2{margin:13px 0 10px;color:#132349;font-size:38px;font-weight:850;line-height:1.3;letter-spacing:-.03em}
.wallet-payment-heading p{margin:0;color:#8b99b8;font-size:17px;font-weight:650;line-height:1.8}
.wallet-payment-art{position:relative;display:flex;align-items:center;flex:0 0 310px;align-self:stretch;margin:-24px -10px -30px -15px}
.wallet-payment-art::before{position:absolute;inset:-25px -20px 15px 80px;border-radius:48% 42% 54% 36%;background:linear-gradient(135deg,#c2d5ff80,#edf3ff);content:'';transform:rotate(-15deg)}
.wallet-payment-art>svg{position:relative;flex:0 0 250px;width:250px;height:185px;margin-left:70px}
.wallet-payment-art>p{position:absolute;left:-20px;top:55px;z-index:1;margin:0;color:#95a5f1;font-size:17px;font-weight:700;line-height:1.7;letter-spacing:2px;transform:rotate(-12deg)}
.wallet-payment-art>p::after{display:block;width:115px;height:11px;border-top:2px solid #c9d6ff;border-radius:50%;content:''}
.wallet-payment-close{position:absolute;right:26px;top:25px;z-index:2;display:grid;place-items:center;width:46px;height:46px;padding:0;border:1px solid #dce5fa;border-radius:15px;background:#ffffffd9;color:#586788;cursor:pointer}
.wallet-payment-close svg{width:25px;height:25px;fill:none;stroke:currentColor;stroke-width:2.2;stroke-linecap:round}
.wallet-payment-summary{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:12px;padding:0 26px 23px}
.wallet-info-card{--card-icon:#6194fb;--card-bg:#eff5ff;--icon-bg:#dce8ff;display:flex;align-items:center;gap:15px;min-width:0;min-height:109px;padding:20px 18px;border-radius:17px;background:linear-gradient(120deg,var(--card-bg),#ffffff60)}
.wallet-info-card.is-product{--card-icon:#9263ee;--card-bg:#f5f0ff;--icon-bg:#e8dcff}.wallet-info-card.is-duration{--card-icon:#34ae86;--card-bg:#edfbf5;--icon-bg:#cbf1e3}.wallet-info-card.is-amount{--card-icon:#f46d76;--card-bg:#fff2f3;--icon-bg:#ffdddf}
.wallet-info-card>i{display:grid;place-items:center;flex:0 0 59px;width:59px;height:59px;border:1px solid color-mix(in srgb,var(--card-icon) 10%,transparent);border-radius:19px;background:linear-gradient(140deg,var(--icon-bg),color-mix(in srgb,var(--icon-bg) 65%,white));color:var(--card-icon);box-shadow:inset 0 2px 3px #ffffff30}
.wallet-info-card>i svg{width:30px;height:30px;fill:none;stroke:currentColor;stroke-width:2;stroke-linecap:round;stroke-linejoin:round}
.wallet-info-card>div{display:flex;flex-direction:column;gap:10px;min-width:0}
.wallet-info-card span{color:#7383a5;font-size:14px;font-weight:750;white-space:nowrap}
.wallet-info-card strong{display:block;max-width:100%;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;color:#172850;font-size:20px;font-weight:800;line-height:1.35}
.wallet-info-card.is-amount strong{color:#e9253a;font-size:28px;font-variant-numeric:tabular-nums}
.wallet-payment-card-note{margin:0 28px 22px;padding:13px 17px;border:1px solid #dee8ff;border-radius:12px;background:#f0f5ff;color:#7283a5;font-size:13px;line-height:1.7}
.wallet-payment-card-note strong{color:#5b75b3}.wallet-payment-card-note p{margin:4px 0 0}
.wallet-payment-error{margin:0 28px 18px;padding:12px 15px;border:1px solid #ffdae2;border-radius:12px;background:#fff3f6;color:#c54b68;font-size:14px;line-height:1.7;overflow-wrap:anywhere}
.wallet-payment-footer{display:flex;align-items:center;justify-content:space-between;gap:18px;padding:22px 28px;border-top:1px solid #e5ebfa;background:linear-gradient(100deg,#fbfdff,#f8faff)}
.wallet-payment-footer>p{display:flex;align-items:center;gap:11px;min-width:0;margin:0;color:#8190ae;font-size:14px;font-weight:650;line-height:1.6}
.wallet-payment-footer>p svg{flex:0 0 24px;width:24px;height:27px;fill:#919ebd;stroke:#fff;stroke-width:2;stroke-linecap:round;stroke-linejoin:round}
.wallet-payment-footer>div{display:flex;align-items:center;gap:16px;flex-shrink:0}
.wallet-payment-footer button{display:inline-flex;align-items:center;justify-content:center;gap:12px;height:58px;padding:0 24px;border:1px solid #d9e2f6;border-radius:14px;background:#fff;color:#657493;font:inherit;font-size:18px;font-weight:800;white-space:nowrap;cursor:pointer}
.wallet-payment-footer .wallet-payment-cancel{width:118px}
.wallet-payment-footer .wallet-payment-confirm{min-width:254px;border-color:transparent;background:linear-gradient(120deg,#729fff,#7165fb 60%,#5448e7);color:#fff;box-shadow:0 8px 19px #7264e63b,inset 0 1px 2px #ffffff80}
.wallet-payment-confirm svg{width:25px;height:25px;fill:none;stroke:currentColor;stroke-width:2;stroke-linecap:round;stroke-linejoin:round}
button:disabled{opacity:.58;cursor:wait}button:focus-visible{outline:2px solid #8e9efa;outline-offset:3px}
@media(max-width:1050px){.wallet-payment-header{padding:32px 30px 28px}.wallet-payment-heading{max-width:650px}.wallet-payment-heading h2{font-size:33px}.wallet-payment-heading p{font-size:15px}.wallet-payment-art{flex-basis:230px}.wallet-payment-art>svg{width:210px;flex-basis:210px;margin-left:24px}.wallet-payment-art>p{display:none}.wallet-payment-summary{grid-template-columns:repeat(2,minmax(0,1fr))}.wallet-payment-footer>p{font-size:12px}.wallet-payment-footer button{font-size:16px;height:52px}.wallet-payment-footer .wallet-payment-confirm{min-width:220px}}
@media(max-width:650px){.wallet-payment-dialog{width:calc(100vw - 24px);max-height:calc(100dvh - 24px);border-radius:21px}.wallet-payment-header{min-height:0;padding:28px 20px 24px}.wallet-payment-heading{padding-right:0;max-width:100%}.wallet-payment-heading>span{font-size:10px;letter-spacing:.2em}.wallet-payment-heading h2{margin:14px 0 10px;font-size:28px}.wallet-payment-heading p{font-size:13px}.wallet-payment-art{display:none}.wallet-payment-close{right:15px;top:15px;width:32px;height:32px;border-radius:10px}.wallet-payment-close svg{width:18px;height:18px}.wallet-payment-summary{gap:10px;padding:0 14px 18px}.wallet-info-card{flex-direction:column;align-items:flex-start;gap:12px;padding:15px;min-height:151px;border-radius:14px}.wallet-info-card>i{width:42px;height:42px;flex-basis:42px;border-radius:13px}.wallet-info-card>i svg{width:24px;height:24px}.wallet-info-card>div{gap:7px;width:100%}.wallet-info-card span{font-size:12px}.wallet-info-card strong{font-size:16px}.wallet-info-card.is-amount strong{font-size:23px}.wallet-payment-footer{flex-direction:column;align-items:stretch;gap:16px;padding:18px 16px}.wallet-payment-footer>div{gap:10px;display:grid;grid-template-columns:82px minmax(0,1fr)}.wallet-payment-footer>p{font-size:12px}.wallet-payment-footer button{height:49px;font-size:15px;padding:0 10px;gap:8px}.wallet-payment-footer .wallet-payment-cancel{width:auto}.wallet-payment-footer .wallet-payment-confirm{min-width:0}.wallet-payment-confirm svg{width:21px;height:21px}.wallet-payment-card-note,.wallet-payment-error{margin:0 16px 16px;font-size:12px}}
</style>

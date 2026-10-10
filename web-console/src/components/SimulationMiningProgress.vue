<script setup lang="ts">
import { computed } from 'vue'
const props = defineProps<{ phase: 'running' | 'loading' | 'complete' | 'failed'; generated: number }>()
const working = computed(() => props.phase === 'running' || props.phase === 'loading')
const title = computed(() => props.phase === 'running' ? '正在模拟挖掘' : props.phase === 'loading' ? '正在整理模拟建议' : props.phase === 'complete' ? '模拟挖掘完成' : '这次挖掘没有完成')
const description = computed(() => props.phase === 'running' ? '模拟用户提问，检查互动回答，寻找需要补充的内容…' : props.phase === 'loading' ? `已生成${props.generated}条建议，正在加载结果…` : props.phase === 'complete' ? `已生成${props.generated}条模拟建议，请在下方核对。` : '点击左上方“模拟挖掘”，可以再试一次～')
</script>

<template>
  <section class="simulation-mining-progress" :class="{ 'is-working': working, 'is-complete': phase === 'complete', 'is-failed': phase === 'failed' }" aria-label="模拟挖掘工作状态">
    <div class="mining-worker-row">
      <svg class="mining-worker" viewBox="0 0 220 150" role="img" :aria-label="working ? '工人正在挖掘' : '挖掘工人'">
        <ellipse cx="112" cy="139" rx="79" ry="6" fill="#dce3f5" />
        <path d="M34 132h151" fill="none" stroke="#b7c5e5" stroke-width="3" stroke-linecap="round" />
        <path d="M126 132q10-25 30-22 23 0 32 22" fill="#d1b899" />
        <path d="m141 124 7-7 7 6m12 5 7-5" fill="none" stroke="#b59472" stroke-width="3" stroke-linecap="round" />
        <path d="m82 97-8 33m20-32 15 31" fill="none" stroke="#454b85" stroke-width="12" stroke-linecap="round" />
        <path d="M64 131h17m20 0h16" fill="none" stroke="#303859" stroke-width="9" stroke-linecap="round" />
        <path d="M77 67q11-9 22 1l10 29q-15 10-34 0z" fill="#7581ef" />
        <path d="m80 69 3 20m14-19 1 20M79 89h23" fill="none" stroke="#c0c9ff" stroke-width="4" />
        <g class="mining-worker-head">
          <path d="M85 64v8h12v-8" fill="#e6aa84" />
          <ellipse cx="92" cy="51" rx="17" ry="18" fill="#ffcfaa" />
          <path d="M77 43q2-17 16-17 15 0 18 17" fill="#ffc84e" />
          <path d="M75 44h38" fill="none" stroke="#e9ac26" stroke-width="6" stroke-linecap="round" />
          <path d="M93 29v10" stroke="#ffe694" stroke-width="5" stroke-linecap="round" />
          <circle cx="97" cy="51" r="2" fill="#37456a" />
          <path d="m101 55 4 2-4 1M94 62q4 3 7-1" fill="none" stroke="#c4846c" stroke-width="2" stroke-linecap="round" />
        </g>
        <g class="mining-worker-swing">
          <path d="m81 75 18 13 23-7m-21-6 19 5" fill="none" stroke="#ffc9a4" stroke-width="8" stroke-linecap="round" stroke-linejoin="round" />
          <path d="m110 63 39 60" fill="none" stroke="#a57a59" stroke-width="6" stroke-linecap="round" />
          <path d="M133 122q18-10 35 4" fill="none" stroke="#7384a6" stroke-width="7" stroke-linecap="round" />
          <path d="m134 122-5 7m38-3 6 6" fill="none" stroke="#7384a6" stroke-width="4" stroke-linecap="round" />
          <circle cx="121" cy="81" r="5" fill="#ffd6b6" />
        </g>
        <g class="mining-dust" fill="#bba382"><circle cx="151" cy="127" r="3" /><circle cx="167" cy="120" r="2" /><circle cx="179" cy="127" r="2.5" /></g>
      </svg>
      <div class="mining-progress-copy" role="status" aria-live="polite"><strong>{{ title }}</strong><p>{{ description }}</p></div>
    </div>
    <div v-if="phase !== 'failed'" class="mining-progress-track" role="progressbar" aria-label="模拟挖掘进度" aria-valuemin="0" aria-valuemax="100" :aria-valuenow="phase === 'complete' ? 100 : undefined" :aria-valuetext="title"><span></span></div>
    <small v-if="working" class="mining-progress-caption">{{ phase === 'running' ? '模拟中，请稍候…' : '正在加载结果…' }}</small>
    <small v-else-if="phase === 'complete'" class="mining-progress-caption">✓ 已完成</small>
  </section>
</template>

<style scoped>
.simulation-mining-progress { margin:18px 0; padding:10px 22px 16px; border:1px solid #dfe6fc; border-radius:17px; background:linear-gradient(120deg,#f1f1ff,#eef7ff); }
.mining-worker-row { display:flex; align-items:center; justify-content:center; gap:18px; }
.mining-worker { flex:0 0 160px; width:160px; height:109px; overflow:visible; }
.mining-progress-copy { max-width:540px; }
.mining-progress-copy strong { color:#454f9a; font-size:18px; line-height:1.5; }
.mining-progress-copy p { margin:7px 0 0; color:#7886a9; font-size:13px; line-height:1.7; }
.mining-progress-track { width:min(640px,100%); height:9px; margin:8px auto 0; overflow:hidden; border-radius:999px; background:#dfe7fb; }
.mining-progress-track>span { display:block; width:36%; height:100%; border-radius:inherit; background:linear-gradient(90deg,#79a5ff,#7362e7); }
.is-working .mining-progress-track>span { animation:mining-progress-slide 1.8s ease-in-out infinite; }
.mining-progress-caption { display:block; margin-top:8px; color:#8594b9; font-size:12px; line-height:1.5; text-align:center; }
.is-complete .mining-progress-track>span { width:100%; background:linear-gradient(90deg,#67cdb4,#34b391); }
.is-complete .mining-progress-copy strong,.is-complete .mining-progress-caption { color:#32967a; }
.is-failed { background:#fff6f8; border-color:#f1dce3; }
.is-failed .mining-progress-copy strong { color:#c65b76; }
.mining-dust { opacity:0; }
.is-working .mining-worker-swing { transform-origin:104px 81px; animation:mining-pickaxe 1.1s ease-in-out infinite; }
.is-working .mining-worker-head { transform-origin:92px 67px; animation:mining-head-nod 1.1s ease-in-out infinite; }
.is-working .mining-dust { animation:mining-dust 1.1s ease-out infinite; }
@keyframes mining-pickaxe { 0%,100% { transform:rotate(-24deg); } 48% { transform:rotate(12deg); } 62% { transform:rotate(5deg); } }
@keyframes mining-head-nod { 0%,100% { transform:rotate(-3deg); } 50% { transform:rotate(5deg); } }
@keyframes mining-dust { 0%,40%,100% { opacity:0; transform:translate(0,0); } 52% { opacity:.9; } 85% { opacity:0; transform:translate(8px,-17px); } }
@keyframes mining-progress-slide { 0%,100% { transform:translateX(0); } 50% { transform:translateX(178%); } }
@media(max-width:600px) { .simulation-mining-progress { padding:10px 14px 15px; } .mining-worker-row { flex-direction:column; gap:0; text-align:center; } .mining-worker { flex-basis:auto; width:142px; height:97px; } .mining-progress-copy strong { font-size:17px; } }
@media(prefers-reduced-motion:reduce) { .is-working .mining-worker-swing,.is-working .mining-worker-head,.is-working .mining-dust,.is-working .mining-progress-track>span { animation:none; } .is-working .mining-progress-track>span { width:100%; opacity:.6; } }
</style>

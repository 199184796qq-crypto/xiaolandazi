<script setup lang="ts">
import { computed, markRaw, ref } from 'vue'
import RoomInteractionPreferences from './RoomInteractionPreferences.vue'
import RoomAddressingPreferences from './RoomAddressingPreferences.vue'
import RoomHumanBehaviorProfile from './RoomHumanBehaviorProfile.vue'

type PreferenceTab = 'interaction' | 'addressing' | 'human'

const props = withDefaults(defineProps<{
  roomId: number
  compact?: boolean
  mobile?: boolean
}>(), {
  compact: false,
  mobile: false,
})

const tabs = [
  {
    key: 'interaction' as const,
    label: '互动偏好',
    english: 'INTERACTION',
    desc: '你决定更想回应什么，小蓝会按直播间热度自动调整节奏。',
    status: '智能随人气调整',
    tone: 'blue',
    component: markRaw(RoomInteractionPreferences),
  },
  {
    key: 'addressing' as const,
    label: '称呼习惯',
    english: 'ADDRESSING',
    desc: '控制点名频率，并告诉小蓝哪些称呼更像你、哪些不要说。',
    status: '热生效',
    tone: 'violet',
    component: markRaw(RoomAddressingPreferences),
  },
  {
    key: 'human' as const,
    label: '主播状态',
    english: 'HOST STATUS',
    desc: '告诉小蓝你的长期表达习惯和当前状态，让口播更贴近现场。',
    status: '热生效',
    tone: 'mint',
    component: markRaw(RoomHumanBehaviorProfile),
  },
]

const activeTab = ref<PreferenceTab>('interaction')
const transitionName = ref<'pref-slide-forward' | 'pref-slide-back'>('pref-slide-forward')

const activeMeta = computed(() => tabs.find((tab) => tab.key === activeTab.value) || tabs[0])
const activeComponent = computed(() => activeMeta.value.component)

function selectTab(next: PreferenceTab) {
  if (next === activeTab.value) return
  const currentIndex = tabs.findIndex((tab) => tab.key === activeTab.value)
  const nextIndex = tabs.findIndex((tab) => tab.key === next)
  transitionName.value = nextIndex > currentIndex ? 'pref-slide-forward' : 'pref-slide-back'
  activeTab.value = next
}
</script>

<template>
  <section class="room-preference-hub" :class="{ compact, mobile }">
    <header class="preference-hub-header">
      <div class="preference-hub-heading">
        <span class="heading-english">{{ activeMeta.english }}</span>
        <strong>{{ activeMeta.label }}</strong>
        <span class="heading-desc">{{ activeMeta.desc }}</span>
      </div>
      <div class="preference-hub-status">
        <i></i>
        {{ activeMeta.status }}
      </div>
    </header>

    <nav class="preference-hub-tabs" aria-label="直播偏好设置">
      <button
        v-for="tab in tabs"
        :key="tab.key"
        type="button"
        class="preference-hub-tab"
        :class="['tone-' + tab.tone, { active: activeTab === tab.key }]"
        :aria-selected="activeTab === tab.key"
        @click="selectTab(tab.key)"
      >
        <span class="tab-chinese">{{ tab.label }}</span>
        <i></i>
      </button>
    </nav>

    <div class="preference-hub-viewport">
      <Transition :name="transitionName" mode="out-in">
        <KeepAlive>
          <component
            :is="activeComponent"
            :key="activeTab"
            :room-id="props.roomId"
            :compact="props.compact"
            :mobile="props.mobile"
            embedded
          />
        </KeepAlive>
      </Transition>
    </div>
  </section>
</template>

<style scoped>
.room-preference-hub{
  display:grid;
  gap:14px;
  overflow:hidden;
  padding:18px;
  border:1px solid #dce4f4;
  border-radius:20px;
  background:linear-gradient(180deg,#fff 0%,#fbfcff 100%);
  box-shadow:0 14px 34px rgba(65,83,140,.08);
}
.preference-hub-header{
  display:grid;
  grid-template-columns:minmax(0,1fr) auto;
  align-items:start;
  gap:8px 16px;
}
.preference-hub-heading{display:grid;gap:6px;min-width:0}
.preference-hub-heading strong{
  color:#172a52;
  font-size:26px;
  font-weight:950;
  letter-spacing:-.5px;
  transition:color .24s ease,transform .24s ease;
}
.preference-hub-heading .heading-english{
  color:#98a4b9;
  font-size:10px;
  font-weight:900;
  line-height:1;
  letter-spacing:2px;
  text-transform:uppercase;
}
.preference-hub-heading .heading-desc{
  max-width:620px;
  color:#53627d;
  font-size:14px;
  font-weight:750;
  line-height:1.55;
}
.preference-hub-status{
  display:inline-flex;
  align-items:center;
  gap:8px;
  min-height:38px;
  padding:0 13px;
  border:1px solid #bfe9d7;
  border-radius:999px;
  background:linear-gradient(180deg,#effbf5,#e9f8f1);
  color:#277653;
  font-size:13px;
  font-weight:900;
  white-space:nowrap;
  box-shadow:inset 0 0 0 1px rgba(79,185,136,.04);
}
.preference-hub-status i{
  width:9px;
  height:9px;
  border-radius:50%;
  background:#37b87c;
  box-shadow:0 0 0 5px rgba(55,184,124,.12);
}
.preference-hub-tabs{
  display:grid;
  grid-template-columns:repeat(3,minmax(0,1fr));
  gap:12px;
  padding-top:2px;
}
.preference-hub-tab{
  display:grid;
  gap:7px;
  min-width:0;
  padding:0;
  border:0;
  background:transparent;
  color:#8a96ac;
  text-align:left;
  cursor:pointer;
}
.preference-hub-tab .tab-chinese{
  padding-left:2px;
  font-size:12.5px;
  font-weight:900;
  line-height:1.15;
  transition:color .2s ease,transform .2s ease;
}
.preference-hub-tab i{
  display:block;
  width:100%;
  height:4px;
  border-radius:999px;
  opacity:.32;
  transform:scaleX(.96);
  transform-origin:center;
  transition:opacity .24s ease,transform .24s ease,height .24s ease,box-shadow .24s ease;
}
.preference-hub-tab.tone-blue i{background:linear-gradient(90deg,#5b74f5,#7990ff)}
.preference-hub-tab.tone-violet i{background:linear-gradient(90deg,#8d62ee,#b58aff)}
.preference-hub-tab.tone-mint i{background:linear-gradient(90deg,#36b889,#66d5ad)}
.preference-hub-tab.active .tab-chinese{color:#1d345d;transform:translateY(-1px)}
.preference-hub-tab.active i{
  height:6px;
  opacity:1;
  transform:scaleX(1);
}
.preference-hub-tab.tone-blue.active i{box-shadow:0 6px 16px rgba(91,116,245,.22)}
.preference-hub-tab.tone-violet.active i{box-shadow:0 6px 16px rgba(141,98,238,.22)}
.preference-hub-tab.tone-mint.active i{box-shadow:0 6px 16px rgba(54,184,137,.22)}
.preference-hub-viewport{
  position:relative;
  min-width:0;
  overflow:hidden;
}
.pref-slide-forward-enter-active,
.pref-slide-forward-leave-active,
.pref-slide-back-enter-active,
.pref-slide-back-leave-active{
  transition:opacity .28s cubic-bezier(.22,.61,.36,1),transform .32s cubic-bezier(.22,.61,.36,1);
  will-change:opacity,transform;
}
.pref-slide-forward-enter-from{opacity:0;transform:translateX(42px)}
.pref-slide-forward-leave-to{opacity:0;transform:translateX(-28px)}
.pref-slide-back-enter-from{opacity:0;transform:translateX(-42px)}
.pref-slide-back-leave-to{opacity:0;transform:translateX(28px)}
.room-preference-hub.compact{gap:10px;padding:14px 12px;border-radius:17px}
.room-preference-hub.compact .preference-hub-heading strong{font-size:21px}
.room-preference-hub.compact .preference-hub-heading .heading-desc{font-size:12.5px;line-height:1.4}
.room-preference-hub.compact .preference-hub-heading .heading-english{font-size:8.5px;letter-spacing:1.7px}
.room-preference-hub.compact .preference-hub-status{min-height:32px;padding:0 9px;font-size:11px}
.room-preference-hub.compact .preference-hub-status i{width:8px;height:8px}
.room-preference-hub.compact .preference-hub-tabs{gap:8px}
.room-preference-hub.compact .preference-hub-tab .tab-chinese{font-size:11px}
.room-preference-hub.mobile{box-shadow:none}
@media (max-width:760px){
  .room-preference-hub{padding:14px;border-radius:16px}
  .preference-hub-header{grid-template-columns:1fr}
  .preference-hub-status{justify-self:start}
  .preference-hub-tabs{gap:6px}
  .preference-hub-tab .tab-chinese{font-size:11px}
}
@media (prefers-reduced-motion: reduce){
  .pref-slide-forward-enter-active,
  .pref-slide-forward-leave-active,
  .pref-slide-back-enter-active,
  .pref-slide-back-leave-active{transition:none}
}
</style>

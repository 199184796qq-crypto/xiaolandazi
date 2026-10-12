<template>
  <div class="plan-header-artwork" aria-hidden="true">
    <span class="plan-art-orbit"></span><span class="plan-art-bubble"></span><span class="plan-art-spark">✦</span>
    <div class="plan-art-audio"><i></i><svg viewBox="0 0 72 40"><path d="M8 18v5m7-12v20m7-24v28m7-21v14m7-18v23m7-26v31m7-23v16m7-12v9m7-6v4" /></svg></div>
    <div class="plan-art-paper"><i></i><i></i><i></i><i></i></div>
  </div>
</template>

<style scoped>
.plan-header-artwork{position:absolute;right:450px;bottom:5px;width:245px;height:154px;pointer-events:none;opacity:.7;z-index:0}
.plan-art-orbit{position:absolute;left:0;top:35px;width:234px;height:84px;border:1px solid #bdbbff6b;border-radius:50%;transform:rotate(-14deg)}
.plan-art-bubble{position:absolute;right:6px;top:6px;width:50px;height:50px;border-radius:50%;background:linear-gradient(130deg,#d8eaff,#abd6ff85);box-shadow:inset -5px -4px 10px #a7c4ff2b}
.plan-art-spark{position:absolute;left:4px;top:66px;font-size:36px;color:#d4cbff}
.plan-art-audio{position:absolute;left:90px;top:18px;display:flex;align-items:center;gap:8px;width:128px;height:91px;padding:18px 12px;border:1px solid #ffffff9c;border-radius:13px;background:linear-gradient(140deg,#bf9bff,#7972fc 62%,#adb6ff);box-shadow:0 14px 18px #aba5ee21,inset 0 1px 4px #fff7;transform:rotate(13deg);box-sizing:border-box}
.plan-art-audio>i{width:23px;height:28px;background:#fff;border-radius:50% 50% 45% 45%;transform:rotate(-10deg);flex-shrink:0;opacity:.9}
.plan-art-audio>svg{width:65px;height:45px;fill:none;stroke:#fff;stroke-width:3;stroke-linecap:round}
.plan-art-paper{position:absolute;left:52px;bottom:2px;display:grid;gap:9px;width:123px;height:93px;padding:20px 17px;border:1px solid #ffffffd9;border-radius:15px;background:linear-gradient(140deg,#fffffff2,#e5e9ffcd);box-shadow:0 8px 18px #b3b9eb25;transform:rotate(10deg);box-sizing:border-box}
.plan-art-paper>i{height:6px;border-radius:4px;background:#dadffb}.plan-art-paper>i:nth-child(2){width:88%}.plan-art-paper>i:nth-child(3){width:95%}.plan-art-paper>i:nth-child(4){width:70%}
</style>

"""Run reference simulations and write reviewable evidence outside the repository."""
import argparse
import datetime
import html
import json
from pathlib import Path
import time
import traceback

from simulator import Sim
from scenarios import SCENARIOS


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--output", type=Path, default=Path(r"E:\直播伴播-local\interaction-redesign-20261031\simulation"))
    ap.add_argument("--filter", default="")
    args = ap.parse_args()
    args.output.mkdir(parents=True, exist_ok=True)
    started = time.monotonic()
    cases = []
    for name, purpose, run in SCENARIOS:
        if args.filter and args.filter not in name:
            continue
        sim, checks, error = Sim(), {}, ""
        try:
            checks = run(sim)
            passed = bool(checks) and all(checks.values())
        except Exception:
            passed = False
            error = traceback.format_exc()
        row = {"name": name, "purpose": purpose, "passed": passed,
               "checks": checks, "error": error, **sim.result()}
        cases.append(row)
        print(("PASS " if passed else "FAIL ") + name)
        if not passed:
            print(error or json.dumps(checks, ensure_ascii=False))
    report = {"generated_at": datetime.datetime.now(datetime.timezone.utc).isoformat(),
              "reference_only": True, "network_used": False, "real_model_calls": 0,
              "production_modified": False, "seeded_cases": [7, 29, 386, 566],
              "passed": sum(c["passed"] for c in cases), "total": len(cases),
              "assertions": sum(len(c["checks"]) for c in cases),
              "wall_seconds": round(time.monotonic() - started, 3), "cases": cases}
    (args.output / "results.json").write_text(json.dumps(report, ensure_ascii=False, indent=2), encoding="utf-8")
    write_markdown(args.output, report)
    write_html(args.output, report)
    print(json.dumps({k: report[k] for k in ["passed", "total", "assertions", "wall_seconds"]}, ensure_ascii=False))
    print(str(args.output))
    return 0 if report["total"] and report["passed"] == report["total"] else 1


def write_markdown(out, report):
    lines = ["# 互动调度器工作模拟报告", "",
             "本报告为独立离散事件参考模拟：虚拟时钟、预置模型返回、预置TTS延迟/时长及虚拟方案/P0。没有联网、付费调用、生产写入或真实音频播放。",
             "", f"结果：{report['passed']}/{report['total']} 个场景通过，{report['assertions']} 项场景检查；每个虚拟tick另外检查单路播放、正文保持、音频就绪、原始期限、时长预算和双窗口账号额度。",
             "", "断言证明参考模型满足所写合同，不能证明生产实现、真实意图准确率、模型事实正确率、PCM切点或实机听感。",
             "", "| 场景 | 结果 | 业务模型（夹具） | 衔接模型（夹具） | 实际获准TTS（夹具） | 完成播出（夹具） |",
             "| --- | --- | ---: | ---: | ---: | ---: |"]
    for case in report["cases"]:
        counters = {k: sum(r["counts"].get(k, 0) for r in case["rooms"])
                    for k in ["body_model", "join_model", "tts_called", "completed"]}
        lines.append(f"| {case['name']} | {'通过' if case['passed'] else '**失败**'} | "
                     + " | ".join(str(counters[k]) for k in counters) + " |")
    lines += ["", "## 对话全过程示例", "",
              "场景24：小王问‘我在新建发货吗’，实际播出澄清后回复‘新建区’、‘南昌的’。这里的回答文字是测试夹具，不代表任何真实商品的配送事实。", "",
              "| 虚拟秒 | 动作 | 内容或原因 |", "| ---: | --- | --- |"]
    sample = next((c for c in report["cases"] if c["name"].startswith("24 ")), None)
    if sample:
        for e in sample["trace"]:
            if e["action"] in {"body_requested", "selected", "dialogue_opened", "dialogue_input", "play_start", "completed", "dialogue_closed"}:
                content = e.get("text") or e.get("context") or e.get("reason") or e.get("address", "")
                content = str(content).replace("|", " / ").replace("\n", " ")
                lines.append(f"| {e['at']:.2f} | {e['action']} | {content} |")
    lines += ["", "## 模拟暴露并修正的问题", "",
              "首轮47个场景中46个通过。场景07发现：按固定来源优先级处理同时到达的行为事件，欢迎音频已合成，却排到21秒才有播出机会，超过原始20秒寿命。", "",
              "修正参考模型及设计：普通关注/成交感谢/点赞/欢迎归为同一行为大类，内部兼顾最早到期时间；真实投诉、购买问题及对话仍处于更高大类。不延长欢迎TTL，不放松断言。重跑后该场景四类行为均完成，原首轮结果保留在simulation-first-run目录。", "",
              "同时补上正常事件携带昵称变化时的旧称呼失效和用户屏蔽处理。参考模拟验证不是生产代码修复。", "",
              "## 当前模拟边界", "",
              "- 回答/澄清/忽略和主动话题关联由显式夹具提供，不测试真实模型语义能力；称呼灰区模型、事实语义审核没有真实调用。",
              "- 句末机会用每3秒一个安全窗口代表，不执行真实Core PCM；低缓冲用输入参数代表，不重验线上原音缓冲算法。",
              "- 双冷由场景输入驱动，触发点固定，避免随机概率让回归不可复现；真实进房/语义密度分档及概率分布仍需历史事件回放。",
              "- 1–2人的口令统计仅合入下一条有效正文且整段合成；关注/点赞等来源的精细档位冷却尚未接入。",
              "- 模拟账号池采用内存和虚拟时钟，不验证真实数据库多进程锁、模型租约或供应商边界；按已有中央TTS池接入时须做隔离集成验收。",
              "- 小时费用/人民币金额不能从此报告推导：这里仅统计请求、排队、丢弃和缓存，未调用真实计费。", "",
              "逐场景断言和完整请求/事件见results.json；模拟时间线.html可筛选场景、查看各房播出顺序及事件。"]
    for case in report["cases"]:
        if not case["passed"]:
            lines += ["", f"## 未通过：{case['name']}", "", "```text", case["error"] or str(case["checks"]), "```"]
    (out / "模拟报告.md").write_text("\n".join(lines) + "\n", encoding="utf-8")


def write_html(out, report):
    # All data are explicit fake fixtures; escape script delimiters as well as HTML.
    data = json.dumps(report, ensure_ascii=False).replace("<", "\\u003c").replace("&", "\\u0026")
    doc = '''<!doctype html><html lang="zh-CN"><meta charset="UTF-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>互动调度器工作模拟</title><style>
body{font:16px/1.6 system-ui,sans-serif;color:#17233d;background:#f3f6fc;margin:0;padding:24px}
main{max-width:1360px;margin:auto}h1{font-size:28px;margin:8px 0}.note{border-left:5px solid #6a63ef;background:#fff;padding:14px 20px;border-radius:10px}
select,input{font:inherit;padding:10px;border:1px solid #b7c4de;border-radius:8px;max-width:100%}.toolbar{display:flex;gap:16px;flex-wrap:wrap;margin:22px 0}
.card{background:#fff;border:1px solid #dce4f2;border-radius:14px;padding:20px;margin-bottom:18px}.good{color:#087a55}.bad{color:#bd243f}
table{border-collapse:collapse;width:100%;font-size:15px}td,th{text-align:left;padding:10px;border-bottom:1px solid #e1e7f2;vertical-align:top;word-break:break-word}
.scroll{max-height:650px;overflow:auto}th{position:sticky;top:0;background:#edf2fb}.track{position:relative;height:60px;background:#eef2f9;margin:12px 0;border-radius:9px;overflow:hidden}
.bar{position:absolute;top:7px;height:46px;border-radius:6px;color:white;white-space:nowrap;overflow:hidden;font-size:13px;padding:2px;box-sizing:border-box}
.semantic{background:#5961d9}.dialogue{background:#168978}.complaint,.purchase{background:#bf547b}.welcome,.guide{background:#b17d21}.engagement{background:#4874a6}
.like,.follow,.order{background:#746a9c}code{font-size:14px}.metrics{display:flex;gap:12px;flex-wrap:wrap}.metric{background:#eef2fb;border-radius:8px;padding:8px 14px}
</style><main><h1>互动调度器工作模拟</h1><p class="note">虚拟时钟 · 预置模型/TTS返回 · 不连接生产。颜色条表示参考模型的实际送播区间，不能证明物理盒子已经出声。完整数据及断言另存 results.json。</p>
<div class="toolbar"><select id="scenario"></select><input id="filter" placeholder="筛选动作/观众/文字"></div><div id="summary"></div><div id="rooms"></div>
<div class="card"><h2>逐事件时间线</h2><div class="scroll"><table><thead><tr><th>秒</th><th>房间</th><th>动作</th><th>详情</th></tr></thead><tbody id="trace"></tbody></table></div></div></main>
<script>const report=__DATA__;
const $=id=>document.getElementById(id), esc=x=>String(x??'').replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
report.cases.forEach((c,i)=>{const o=document.createElement('option');o.value=i;o.textContent=(c.passed?'✓ ':'✗ ')+c.name;$('scenario').append(o)});
function render(){const c=report.cases[Number($('scenario').value)];if(!c)return;
$('summary').innerHTML='<div class="card"><h2 class="'+(c.passed?'good':'bad')+'">'+esc(c.name)+' · '+(c.passed?'通过':'失败')+'</h2><p>'+esc(c.purpose)+'</p><ul>'+Object.entries(c.checks).map(([k,v])=>'<li class="'+(v?'good':'bad')+'">'+(v?'✓ ':'✗ ')+esc(k)+'</li>').join('')+'</ul><pre>'+esc(c.error)+'</pre></div>';
$('rooms').innerHTML=c.rooms.map(r=>'<div class="card"><h2>'+esc(r.room)+'</h2><div class="metrics">'+Object.entries(r.counts).map(([k,v])=>'<span class="metric">'+esc(k)+': '+v+'</span>').join('')+'</div><div class="track">'+r.plays.map(p=>'<div class="bar '+esc(p.source)+'" style="left:'+100*p.start/c.until+'%;width:'+100*(p.end-p.start)/c.until+'%" title="'+esc(p.start+'–'+p.end+'s '+p.text)+'">'+esc(p.source)+'</div>').join('')+'</div><small>0 — '+c.until+' 秒；悬停色块看话术。完整文本见下面时间线。</small></div>').join('');
const query=$('filter').value.toLowerCase();$('trace').innerHTML=c.trace.filter(e=>JSON.stringify(e).toLowerCase().includes(query)).map(e=>{const {at,room,action,...rest}=e;return '<tr><td>'+at.toFixed(2)+'</td><td>'+esc(room)+'</td><td><code>'+esc(action)+'</code></td><td>'+esc(JSON.stringify(rest))+'</td></tr>'}).join('')}
$('scenario').onchange=render;$('filter').oninput=render;render();</script></html>'''
    (out / "模拟时间线.html").write_text(doc.replace("__DATA__", data), encoding="utf-8")


if __name__ == "__main__":
    raise SystemExit(main())

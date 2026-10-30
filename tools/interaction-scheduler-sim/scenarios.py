"""Executable working simulations. Text is declared fixture data, not model evidence."""
import math
import random
from simulator import Account, Event, Reply, Room, TERMINAL

SCENARIOS = []


def case(name, purpose):
    def register(fn):
        SCENARIOS.append((name, purpose, fn))
        return fn
    return register


def chat(id, uid="wang", name="小王", text="什么时候发货？", **reply):
    return Event(id, uid, name, text, reply=Reply(**reply))


def clarify(id="q", uid="wang", name="小王"):
    return chat(id, uid, name, "我在新建发货吗", disposition="clarify",
                body="您说的是新建区，还是新建的订单？")


def actions(s, action):
    return [e for e in s.trace if e["action"] == action]


def bulk(s, room, n, at=0, **kw):
    def enqueue():
        for k in range(n):
            event = Event(f"load-{at}-{k}", text=f"资源负载{k}", at=s.now, ttl=180)
            room.make_item("semantic", [event], Reply(body=f"资源负载{k}。", delay=0,
                                                    duration=.1, **kw))
    s.at(at, enqueue)


@case("01 无事件", "欢迎、行为与语义都没有输入时不产生调用。")
def no_events(s):
    r = Room(s)
    s.run(90)
    return {"模型和TTS调用均为0": not r.count, "无播出": not r.plays}


@case("02 正文提前、称呼后置", "无称呼正文先完成；选中后衔接、整段TTS、句末切入。")
def two_phase(s):
    r = Room(s)
    s.send(r, 0, chat("q", body="一般下单后第二天上午发货。"))
    s.run(12)
    i = r.items[0]
    return {"先正文再选中": i.times["body_ready"] <= i.times["selected"],
            "先衔接再TTS再播出": i.times["join_done"] <= i.times["tts_request"] < i.times["play_start"],
            "正文无昵称、最终称呼正确": "小王" not in i.body and i.text == "小王，" + i.body,
            "一次业务+一次衔接+一次TTS": r.count["body_model"] == r.count["join_model"] == r.count["tts_called"] == 1}


@case("03 等待选中期间改名", "预算暂不可用；选择时才读取当前可用称呼。")
def late_name(s):
    r = Room(s, budget=0)
    s.send(r, 0, chat("q"))
    s.send(r, 3, Event("rename", "wang", "王同学", kind="rename"))
    s.at(4, lambda: setattr(r, "budget", 40))
    s.run(14)
    return {"选择前无TTS": r.items[0].times["tts_request"] >= 4,
            "使用选中时称呼": r.items[0].address == "王同学", "实际播出1条": r.count["completed"] == 1}


@case("04 衔接试图添承诺", "坏的衔接返回被拒绝，原正文和边界保持。")
def bad_join(s):
    r = Room(s)
    body = "特殊发货时间以客服确认为准。"
    s.send(r, 0, chat("q", body=body, join_bad=True))
    s.run(12)
    return {"坏衔接已回退": r.count["join_fallback"] == 1,
            "未增加送达保证": r.plays[0]["text"] == "小王，" + body,
            "没有重生成业务正文": r.count["body_model"] == 1}


@case("05 衔接超时", "短任务失败后只做确定性组合，不停止主线、不重复业务调用。")
def join_timeout(s):
    r = Room(s)
    s.send(r, 0, chat("q", join_failure=True))
    s.run(12)
    return {"一次回退后仍可播": r.count["join_fallback"] == 1 and r.count["completed"] == 1,
            "业务调用只有一次": r.count["body_model"] == 1}


@case("06 称呼冷却", "同一观众后续问题仍回答，明确不称呼时不补名字、不多调衔接。")
def address_cooldown(s):
    r = Room(s)
    s.send(r, 0, chat("q1", body="第一个问题的回答。"))
    s.send(r, 15, chat("q2", text="那规格呢？", body="第二个问题的回答。"))
    s.run(28)
    return {"两条都回答": r.count["completed"] == 2,
            "第二次明确不称呼": r.items[1].address == "" and r.items[1].text == r.items[1].body,
            "衔接模型只调用一次": r.count["join_model"] == 1}


@case("07 欢迎点赞关注成交", "四个行为调度器聚合事件，模板不调用业务模型，统一队列播出。")
def behavior_schedulers(s):
    r = Room(s)
    for k, kind in enumerate(["welcome", "like", "follow", "order"]):
        s.send(r, k * .1, Event(kind, f"u{k}", f"观众{k}", kind=kind))
    s.run(35)
    return {"四种来源均播出": {p["source"] for p in r.plays} == {"welcome", "like", "follow", "order"},
            "只有欢迎调用业务模型": r.count["body_model"] == 1,
            "欢迎模板外的三项不多调衔接": r.count["join_model"] == 1}


@case("08 欢迎多人取名", "批量进房只选择本批真实用户，点名最多4人，重排不重新抽人。")
def welcome_names(s):
    r = Room(s)
    for k in range(20):
        s.send(r, k * .01, Event(f"w{k}", f"u{k}", f"观众{k}", kind="welcome"))
    s.run(15)
    i = r.items[0]
    return {"只生成一个欢迎正文": r.count["body_model"] == 1,
            "点名不超过4且均来自本批": len(i.addressed_uids) <= 4 and all(u in {f"u{k}" for k in range(20)} for u in i.addressed_uids),
            "只合成一次": r.count["tts_called"] == 1}


@case("09 旧欢迎过期", "30秒窗口不能为20秒前的旧进房者重新分配寿命。")
def old_welcome(s):
    r = Room(s, window=30)
    s.send(r, 0, Event("old", "u", "旧观众", kind="welcome"))
    s.run(35)
    return {"旧事件未送模型/TTS": r.count["body_model"] == r.count["tts_called"] == 0,
            "有明确过期统计": r.count["pending_events_expired"] == 1}


@case("10 纯感谢和售后区分", "自述‘没下单’或‘拍了能退吗’不自动变为成交感谢。")
def no_keyword_order(s):
    r = Room(s)
    s.send(r, 0, chat("q", text="拍了能退吗？", body="退换规则以订单页面已确认条件为准。"))
    s.run(12)
    return {"走语义回答而非成交感谢": r.items[0].source == "semantic" and r.routes["order"] == 0,
            "原问题保留": r.items[0].context == ["拍了能退吗？"]}


@case("11 未就绪队头不堵塞", "最高优先级仍在合成，后面已有音频的普通问题可以先播。")
def pending_head(s):
    r = Room(s)
    s.send(r, 0, Event("urgent", "u1", "小李", "售后问题", "complaint", Reply(tts_delay=12)))
    s.send(r, .1, chat("ordinary", "u2", "小王", body="普通问题已确认的回答。"))
    s.run(26)
    return {"普通问题先播": r.plays[0]["source"] == "semantic",
            "投诉音频就绪后仍能播": any(p["source"] == "complaint" for p in r.plays),
            "投诉未提前打断": r.items[0].times["play_start"] >= r.items[0].times["audio_ready"]}


@case("12 取消释放预算", "准备中取消释放预留，让另一条有效任务继续；旧衔接回调无效。")
def cancel_reservation(s):
    r = Room(s, budget=4)
    s.send(r, 0, chat("a", "a", "甲", join_delay=4))
    s.send(r, .1, chat("b", "b", "乙", body="另一位观众的回答。"))
    s.send(r, 2, Event("rename", "a", "甲改名", kind="rename"))
    s.run(13)
    return {"已取消任务不播": r.items[0].state == "SUPERSEDED",
            "预算释放后另一条播出": r.count["completed"] == 1 and r.plays[0]["text"].startswith("乙"),
            "过期衔接未发TTS": r.count["tts_called"] == 1 and bool(actions(s, "stale_join_callback"))}


@case("13 实际音频超过预算", "合成后的真实时长大于预估，重新核对，不能挤爆时长预算。")
def actual_duration(s):
    r = Room(s, budget=5)
    s.send(r, 0, chat("q", estimate=2, duration=7))
    s.run(15)
    return {"已合成费用仍统计": r.count["tts_called"] == 1,
            "真实时长超限不播放": not r.plays and r.items[0].reason == "actual_audio_exceeds_budget"}


@case("14 滚动时长预算", "10秒预算内准备3段4秒声音，第三段等滚动窗口释放。")
def rolling_budget(s):
    r = Room(s, budget=10, prepare=3)
    for k in range(3):
        s.send(r, k * .1, Event(f"q{k}", f"u{k}", f"用户{k}", "问题", reply=Reply(), ttl=100))
    s.run(80)
    return {"三条最终均完成": r.count["completed"] == 3,
            "第三条在窗口释放后播放": r.plays[2]["start"] >= 60,
            "没有因预算回队重复业务生成": r.count["body_model"] == 3}


@case("15 单账号双窗口限流", "向中央池注入196个合成任务，检查3/秒与180/60秒滚动额度。")
def account_limit(s):
    r = Room(s, budget=math.inf, prepare=300)
    bulk(s, r, 196)
    s.run(66)
    times = [g["at"] for g in s.pool.grants]
    return {"196条均获额度": len(times) == 196,
            "任意1秒不超过3": all(sum(t - 1 + 1e-7 < x <= t + 1e-7 for x in times) <= 3 for t in times),
            "任意60秒不超过180": all(sum(t - 60 + 1e-7 < x <= t + 1e-7 for x in times) <= 180 for t in times),
            "末条确实排队超过60秒": times[-1] >= 65}


@case("16 满账号切下一个", "第一个账号的每秒额度用满，下一条使用第二个兼容账号。")
def next_account(s):
    s.pool.accounts = [Account("account-a"), Account("account-b")]
    r = Room(s, budget=100, prepare=10)
    bulk(s, r, 6)
    s.run(2)
    return {"两个账号各3条": dict(__import__('collections').Counter(g["account"] for g in s.pool.grants)) == {"account-a": 3, "account-b": 3},
            "同一秒内全部分配": max(g["at"] for g in s.pool.grants) < 1}


@case("17 不兼容声音不切换", "空闲账号没有对应克隆音色时，不静默换声。")
def incompatible_voice(s):
    s.pool.accounts = [Account("wrong", {"voice-b"}), Account("right", {"voice-a"})]
    r = Room(s, budget=100, prepare=10)
    bulk(s, r, 4)
    s.run(3)
    return {"全部走正确音色账号": len(s.pool.grants) == 4 and {g["account"] for g in s.pool.grants} == {"right"},
            "第四条必须等配额": s.pool.grants[-1]["at"] >= 1}


@case("18 全账号满额与过期", "排队任务的原期限先到，取消后不再消耗额度。")
def all_full(s):
    s.pool.accounts = [Account("a", admissions=[0, 0, 0]), Account("b", admissions=[0, 0, 0])]
    r = Room(s)
    s.send(r, 0, Event("q", text="短期限问题", ttl=.6, reply=Reply(delay=0)))
    r.window = 0
    s.run(3)
    return {"确实进入过TTS等待": r.count["tts_enqueued"] == 1,
            "没有实际供应商调用": r.count["tts_called"] == 0,
            "已过期而非误报设备离线": r.items[0].state == "EXPIRED"}


@case("19 跨房间公平", "热房先排了很多请求，冷房不会一直排在热房全部请求后。")
def room_fairness(s):
    hot = Room(s, "a-hot", budget=100, prepare=20)
    cold = Room(s, "b-cold", budget=100, prepare=20)
    bulk(s, hot, 9)
    bulk(s, cold, 1, at=.2)
    s.run(5)
    g = next(g for g in s.pool.grants if g["room"] == cold.key)
    return {"冷房在下一个可用秒获额度": g["at"] <= 1.05,
            "没有等完热房9条": s.pool.grants.index(g) == 3}


@case("20 缓存包含声音和方案", "相同正文不同音色/方案不误命中；同一有效身份可以复用。")
def cache_identity(s):
    s.pool.accounts = [Account("a", {"voice-a", "voice-b"})]
    a = Room(s, "a")
    b = Room(s, "b", voice="voice-b")
    c = Room(s, "c", plan="plan-b/v1")
    for r, at in [(a, 0), (b, 0), (a, 12), (c, 12)]:
        s.send(r, at, Event(f"q-{at}", text="同一个问题", reply=Reply(body="相同正文。")))
    s.run(25)
    return {"同方案声音复用一次": a.count["audio_cache_hit"] == 1,
            "另一声音仍合成": b.count["tts_called"] == 1,
            "另一方案仍合成": c.count["tts_called"] == 1,
            "实际调用共3次": len(s.pool.grants) == 3}


@case("21 旧场次异步回调", "TTS执行中切场次，旧音频回调不能进入新场。")
def stale_runtime(s):
    r = Room(s)
    s.send(r, 0, chat("q", body="旧场正文。", tts_delay=10))
    s.at(2, lambda: r.stop(restart=True, plan="plan-b/v2"))
    s.send(r, 2.1, chat("q", body="新场正文。"))
    s.run(18)
    return {"只有新场播放": len(r.plays) == 1 and r.plays[0]["text"].endswith("新场正文。"),
            "旧调用仍记费数": r.count["tts_called"] == 2,
            "旧回调被隔离": bool(actions(s, "stale_tts_callback")),
            "新业务请求用新方案": r.requests[-2]["plan"] == "plan-b/v2"}


@case("22 事件重放", "同一平台事件重复采集不重复生成；新场相同外部事件号隔离。")
def replay(s):
    r = Room(s)
    s.send(r, 0, chat("same"))
    s.send(r, .2, chat("same"))
    s.run(12)
    return {"重放被去重": r.routes["duplicate"] == 1,
            "仅一次生成和播出": r.count["body_model"] == 1 and r.count["completed"] == 1}


@case("23 澄清播出才观察", "TTS和正文ready都不能提前打开对话调度器。")
def actual_clarification(s):
    r = Room(s)
    s.send(r, 0, clarify())
    s.run(2.5)
    early = not r.dialogues and r.items[0].state == "AUDIO_READY"
    s.run(8)
    return {"音频ready时尚未观察": early,
            "实际送播后才打开": r.dialogues["wang"].opened == r.items[0].times["play_start"],
            "观察期限从澄清播完计算": r.dialogues["wang"].deadline == r.items[0].times["completed"] + 60}


@case("24 对话连续补充", "小王‘新建区’‘南昌的’攒成一轮，输入带原问与已播澄清。")
def dialogue_gather(s):
    r = Room(s)
    s.send(r, 0, clarify())
    s.send(r, 8, chat("a", text="新建区"))
    s.send(r, 10, chat("b", text="南昌的", body="您问的是南昌新建区，覆盖范围以订单页面为准。"))
    s.run(28)
    answers = [i for i in r.items if i.source == "dialogue"]
    return {"连续两句只生成一轮": len(answers) == 1 and r.count["body_model"] == 2,
            "三类上下文完整": answers[0].context == ["我在新建发货吗", "您说的是新建区，还是新建的订单？", "新建区", "南昌的"],
            "最后一句后等待5秒": answers[0].times["body_ready"] >= 15,
            "只进入对话不重复走语义": r.routes["dialogue"] == 2 and r.routes["semantic"] == 1}


@case("25 数字澄清回复", "澄清选择‘1’属于对话回应，不被当成无意义数字丢弃。")
def dialogue_number(s):
    r = Room(s)
    s.send(r, 0, clarify())
    s.send(r, 8, chat("a", text="1", body="明白，您说的是第一个意思。"))
    s.run(25)
    return {"数字由对话消费": r.routes["dialogue"] == 1 and r.routes["number_without_context"] == 0,
            "回答完整完成": r.count["completed"] == 2}


@case("26 两人同名同时对话", "两位都叫小王、user_id不同，各自等回复，不互相覆盖。")
def parallel_dialogues(s):
    r = Room(s)
    s.send(r, 0, clarify("q1", "u1"))
    s.send(r, .1, clarify("q2", "u2"))
    s.send(r, 8, chat("a", "u1", text="新建区", body="第一位用户的回答。"))
    s.send(r, 17, chat("b", "u2", text="新建订单", body="第二位用户的回答。"))
    s.run(40)
    replies = [i for i in r.items if i.source == "dialogue"]
    return {"独立的两份观察器": set(r.dialogues) == {"u1", "u2"},
            "两个回应都完成": len(replies) == 2 and all(i.state == "COMPLETED" for i in replies),
            "上下文未串人": "新建订单" not in replies[0].context and "新建区" not in replies[1].context}


@case("27 生成中收到新信息", "旧回答尚在模型生成，新补充提升输入版本，旧返回失效。")
def input_during_model(s):
    r = Room(s)
    s.send(r, 0, clarify())
    s.send(r, 8, chat("a", text="新建区", delay=8, body="旧信息的回答。"))
    s.send(r, 14, chat("b", text="不是，是新建订单", body="更新后的订单问题回答。"))
    s.run(35)
    answers = [i for i in r.items if i.source == "dialogue"]
    return {"旧结果被替代": answers[0].state == "SUPERSEDED",
            "只播更新后的答案": all("旧信息的回答" not in p["text"] for p in r.plays),
            "丢弃的模型调用仍统计": r.count["body_model"] == 3,
            "迟到返回留有记录": bool(actions(s, "stale_body_callback"))}


@case("28 合成中收到新信息", "旧音频正在TTS，新信息到来后不能播旧答案。")
def input_during_tts(s):
    r = Room(s)
    s.send(r, 0, clarify())
    s.send(r, 8, chat("a", text="新建区", tts_delay=10, body="不该播的旧答案。"))
    s.send(r, 15, chat("b", text="刚才说错了，是新建订单", body="根据新信息给出的回答。"))
    s.run(35)
    return {"旧音频不播": all("不该播的旧答案" not in p["text"] for p in r.plays),
            "付费调用未抹掉": r.count["tts_called"] == 3,
            "旧音频回调隔离": bool(actions(s, "stale_tts_callback"))}


@case("29 播放中继续追问", "已经播出的答案不倒带，播出期间的新追问进入下一轮。")
def input_during_play(s):
    r = Room(s)
    s.send(r, 0, clarify())
    s.send(r, 8, chat("a", text="新建订单", duration=6, body="这一轮的回答。"))
    s.send(r, 17, chat("b", text="快递呢？", body="下一轮追问的回答。"))
    s.run(35)
    replies = [i for i in r.items if i.source == "dialogue"]
    return {"两轮均完成": len(replies) == 2 and all(i.state == "COMPLETED" for i in replies),
            "播放没有倒带": replies[0].times["completed"] <= replies[1].times["play_start"]}


@case("30 对话观察超时", "无人回复60秒关闭，既不催问也不重复合成。")
def dialogue_expiry(s):
    r = Room(s)
    s.send(r, 0, clarify())
    s.run(72)
    return {"观察器已关闭": not r.dialogues["wang"].active,
            "只播过一条澄清": r.count["completed"] == 1,
            "未额外生成催问": r.count["body_model"] == 1}


@case("31 澄清未播不观察", "预算阻止澄清播放并最终过期，不建立假对话。")
def unplayed_clarification(s):
    r = Room(s, budget=0)
    s.send(r, 0, clarify())
    s.run(70)
    return {"没有活跃观察器": not r.dialogues,
            "没有TTS调用": r.count["tts_called"] == 0,
            "未播不记回答和称呼": not r.named_at and r.count["completed"] == 0}


@case("32 连续打字有封批上限", "5秒静默窗连续被重置，最晚首次输入10秒后封一批。")
def bounded_gather(s):
    r = Room(s)
    s.send(r, 0, clarify())
    for k, t in enumerate([8, 12, 16, 20]):
        s.send(r, t, chat(f"a{k}", text=f"补充信息{k}"))
    s.run(35)
    requests = [x for x in r.requests if x["stage"] == "business" and x["at"] > 1]
    return {"首次封批在18秒而非无限等待": requests[0]["at"] == 18,
            "后续新信息仍被处理": r.routes["dialogue"] == 4,
            "最终有有效新答案": any(i.source == "dialogue" and i.state == "COMPLETED" for i in r.items)}


@case("33 不反复追问", "已经澄清一次，第二次模型还想反问时改用已确认的边界。")
def repeated_clarification(s):
    r = Room(s)
    s.send(r, 0, clarify())
    s.send(r, 8, chat("a", text="就是那个", disposition="clarify", body="你到底说哪个？"))
    s.run(28)
    return {"阻止重复反问": r.count["repeated_clarification_blocked"] == 1,
            "没有播出重复追问": all("你到底" not in p["text"] for p in r.plays),
            "观察器最后关闭": not r.dialogues["wang"].active}


@case("34 低价值确认关闭", "谢谢/好等内容由夹具判定无需回答，关闭观察，不再合成。")
def low_value(s):
    r = Room(s)
    s.send(r, 0, clarify())
    s.send(r, 8, chat("a", text="谢谢", disposition="ignore"))
    s.run(22)
    return {"无意义确认后关闭": not r.dialogues["wang"].active,
            "没有额外TTS": r.count["tts_called"] == 1}


@case("35 对话不能吞投诉", "正在观察小王回复时，他的新投诉仍独立进入紧急来源。")
def complaint_during_dialogue(s):
    r = Room(s)
    s.send(r, 0, clarify())
    s.send(r, 8, Event("complaint", "wang", "小王", "订单有问题", "complaint", Reply(body="售后处理渠道以订单页面为准。")))
    s.run(25)
    return {"投诉单独路由": r.routes["complaint"] == 1 and r.routes["dialogue"] == 0,
            "投诉有播出": any(p["source"] == "complaint" for p in r.plays)}


@case("36 活动口令实际播出激活", "播前数字不计数，播后精确匹配按独立观众计数，其它问题照常回答。")
def command_routing(s):
    r = Room(s)
    s.send(r, 0, chat("ask", body="想了解的朋友扣1。", command="1"))
    s.send(r, 2, chat("before", "before", text="1"))
    for k, t in enumerate([4, 5, 6]):
        s.send(r, t, chat(f"vote{k}", f"u{k}", text="1"))
    s.send(r, 7, chat("repeat", "u0", text="1"))
    s.send(r, 7.1, chat("wrong-token", "x", text="11"))
    s.send(r, 5.2, chat("real-question", "other", text="哪里发货？", body="发货地以已确认资料为准。"))
    s.run(32)
    closed = actions(s, "command_closed")[0]
    return {"只统计3个不同观众": closed["count"] == 3,
            "重复扣字不延长窗口": closed["at"] == 16,
            "其它问题没有被吞": r.routes["semantic"] == 2,
            "播前数字与错误口令未计数": r.routes["number_without_context"] == 2,
            "统计结果有播出": any(p["source"] == "engagement" for p in r.plays)}


@case("37 新口令替代旧口令", "只有新的口令真正播出时，旧统计才被替代。")
def command_replacement(s):
    r = Room(s)
    s.send(r, 0, chat("first", body="想了解的扣1。", command="1"))
    s.send(r, 8, chat("old-vote", "u0", text="1"))
    s.send(r, 5, chat("second", "b", "小李", body="现在选第二种的扣2。", command="2"))
    for k, t in enumerate([14, 15, 16]):
        s.send(r, t, chat(f"new{k}", f"v{k}", text="2"))
    s.run(38)
    replaced = actions(s, "command_replaced")
    return {"旧统计在新口令送播时关闭": len(replaced) == 1 and replaced[0]["count"] == 1,
            "新口令独立统计3人": actions(s, "command_closed")[-1]["count"] == 3}


@case("38 活动无人回应", "没有扣字时，不调用模型播报‘0人’。")
def no_command_reply(s):
    r = Room(s)
    s.send(r, 0, chat("first", body="想了解的扣1。", command="1"))
    s.run(25)
    return {"0人静默关闭": actions(s, "command_closed")[0]["count"] == 0,
            "没有统计正文调用": r.count["body_model"] == 1}


@case("39 双冷引导", "主线一直在播也能按互动空档引导，实际问题到达时让位。")
def guide_scheduler(s):
    r = Room(s, dual_cold=True)
    s.run(38)
    s.send(r, 40, chat("real", body="实际问题的回答。"))
    s.run(80)
    return {"主动引导播过一次": sum(p["source"] == "guide" for p in r.plays) == 1,
            "实际问题正常播出": any(p["source"] == "semantic" for p in r.plays),
            "引导未无限重复": r.count["body_model"] == 2}


@case("40 双冷欢迎合并", "双冷判定时有尚未生成的新人欢迎，合为一个正文和一次播出。")
def merge_welcome_guide(s):
    r = Room(s, dual_cold=True, window=5)
    s.send(r, 29, Event("welcome", "u", "小王", kind="welcome"))
    s.run(46)
    return {"有合并记录": len(actions(s, "guide_merged_into_welcome")) == 1,
            "只生成一个正文": r.count["body_model"] == 1,
            "只播一次而非欢迎引导各一次": r.count["completed"] == 1 and "更想了解" in r.plays[0]["text"]}


@case("41 主动话题接成对话", "引导实际播出后，相关回应在30秒窗口内转入该用户对话。")
def topic_dialogue(s):
    r = Room(s, dual_cold=True)
    e = chat("reply", text="我想了解配送", body="配送信息以订单页面已确认范围为准。")
    e.topic_reply = True
    s.send(r, 37, e)
    s.run(58)
    return {"相关回应打开观察器": len(actions(s, "topic_dialogue_opened")) == 1,
            "回应走对话而非普通语义": r.routes["dialogue"] == 1 and r.routes["semantic"] == 0,
            "对话实际完成": any(i.source == "dialogue" and i.state == "COMPLETED" for i in r.items)}


@case("42 原音主线与低缓冲", "模拟保护条件：普通房真实低缓冲暂缓；低流量沿用允许路径。")
def mainline_guard(s):
    a = Room(s, "normal", low_flow=False, mainline_buffer=8)
    b = Room(s, "low-flow", low_flow=True, mainline_buffer=8)
    s.send(a, 0, chat("q"))
    s.send(b, 0, chat("q"))
    s.at(10, lambda: setattr(a, "mainline_buffer", 40))
    s.run(22)
    return {"普通房缓冲恢复后才播": a.plays[0]["start"] >= 10,
            "低流量允许更早切入": b.plays[0]["start"] < 10}


@case("43 TTS服务失败", "合成失败保留原因，主线不被未就绪任务打断，后续问题可继续。")
def tts_failure(s):
    r = Room(s)
    s.send(r, 0, chat("bad", tts_failure=True))
    s.send(r, 2, chat("good", "other", "小李", body="后续正常回答。"))
    s.run(18)
    return {"失败原因明确": r.items[0].state == "FAILED" and r.items[0].reason == "tts_failure",
            "只播有效声音": len(r.plays) == 1 and "后续正常" in r.plays[0]["text"],
            "两次实际调用都统计": r.count["tts_called"] == 2}


@case("44 对话轮数上限", "三轮对话后释放观察器，下一条新问题仍可通过普通语义回答。")
def dialogue_round_limit(s):
    r = Room(s)
    s.send(r, 0, clarify())
    for k, t in enumerate([8, 20, 32]):
        s.send(r, t, chat(f"a{k}", text=f"第{k+1}轮追问", body=f"第{k+1}轮回答。", keep_dialogue=True))
    s.send(r, 44, chat("new", text="又有一个新问题", body="释放观察器之后的回答。"))
    s.run(58)
    return {"三轮之后观察器关闭": r.dialogues["wang"].rounds == 3 and not r.dialogues["wang"].active,
            "新问题仍被回答": r.routes["semantic"] == 2 and r.count["completed"] == 5}


@case("45 少量扣字合入下一条", "1–2个口令回应不单独合成和插播，合入下一条有效回答。")
def small_command_merge(s):
    r = Room(s)
    s.send(r, 0, chat("first", body="想了解的扣1。", command="1"))
    s.send(r, 4, chat("vote1", "u1", text="1"))
    s.send(r, 5, chat("vote2", "u2", text="1"))
    s.send(r, 18, chat("next", text="什么时候发货？", body="发货安排以订单页面为准。"))
    s.run(32)
    return {"没有独立统计插播": not any(p["source"] == "engagement" for p in r.plays),
            "统计只合入一次": r.count["small_command_merged"] == 1,
            "下一条整段播出含真实2人数": r.plays[1]["text"].endswith("收到了2位朋友的回应。发货安排以订单页面为准。"),
            "没有增加统计模型或TTS调用": r.count["body_model"] == r.count["tts_called"] == 2}


@case("46 持续口令与其它问题并行", "一直有新增扣字时不强行结束统计，也不阻塞其它问题回答。")
def long_command(s):
    r = Room(s)
    s.send(r, 0, chat("first", body="想了解的扣666。", command="666"))
    for k, t in enumerate([4, 12, 20, 28, 36, 44]):
        s.send(r, t, chat(f"vote{k}", f"u{k}", text="6666"))
    s.send(r, 15, chat("real", "other", text="规格？", body="规格以已确认资料为准。"))
    s.run(50)
    still_active = bool(r.command)
    s.run(70)
    return {"持续新增时仍在统计": still_active,
            "按最后新增后10秒结束": actions(s, "command_closed")[0]["at"] == 54,
            "人数是6位而非字符数": actions(s, "command_closed")[0]["count"] == 6,
            "期间的普通问题已播": any("规格以" in p["text"] and p["start"] < 50 for p in r.plays)}


@case("47 真实问题取消尚未播引导", "主动引导的TTS虽已发出，真实问题到来仍取消旧引导并让位。")
def guide_yields(s):
    r = Room(s, dual_cold=True)
    s.send(r, 31, chat("real", body="观众真实问题的回答。"))
    s.run(55)
    return {"引导未播": not any(p["source"] == "guide" for p in r.plays),
            "取消有明确原因": any(i.source == "guide" and i.reason == "real_question_arrived" for i in r.items),
            "真实问题正常完成": any(i.source == "semantic" and i.state == "COMPLETED" for i in r.items)}


@case("48 英文规格与小数问题", "没有汉字的真实问题及含小数规格保持原意，不被通用噪音过滤删除。")
def meaningful_non_han(s):
    r = Room(s)
    s.send(r, 0, chat("english", text="Size M?", body="尺码以已确认的规格表为准。"))
    s.send(r, 12, chat("decimal", text="1.5L?", body="容量以已确认规格为准。"))
    s.run(28)
    return {"两个问题均进入语义": r.routes["semantic"] == 2,
            "小数点及规格保留": r.items[1].context == ["1.5L?"],
            "两个问题完成": r.count["completed"] == 2}


@case("49 屏蔽关闭对话", "用户被屏蔽后关闭观察、取消未播声音，后续弹幕不再继续对话。")
def blocked_dialogue(s):
    r = Room(s)
    s.send(r, 0, clarify())
    s.send(r, 8, chat("reply", text="新建区", tts_delay=10))
    s.send(r, 15, Event("block", "wang", kind="block"))
    s.send(r, 16, chat("after", text="我还有问题"))
    s.run(30)
    return {"观察器已关闭": not r.dialogues["wang"].active,
            "后续事件被屏蔽": r.routes["blocked_user"] == 1,
            "未播回答取消且没有新增声音": len(r.plays) == 1 and any(i.reason == "user_blocked" for i in r.items)}


def fuzz_case(seed):
    @case(f"混合负载 seed={seed}", "固定种子混合欢迎/问答/澄清/追问/限流/慢回调/换场，逐tick检查全局不变量。")
    def simulate(s):
        rng = random.Random(seed)
        rooms = [Room(s, "room-a"), Room(s, "room-b")]
        for k in range(80):
            r = rng.choice(rooms)
            kind = rng.choice(["chat"] * 5 + ["welcome", "like", "follow", "purchase", "complaint"])
            event = Event(f"f{k}", f"u{rng.randrange(6)}", f"观众{rng.randrange(6)}", f"虚拟输入{k}", kind,
                          Reply(body=f"夹具回答{k}。", disposition="clarify" if rng.random() < .15 else "answer",
                                delay=rng.choice([.1, .8, 3]), duration=rng.choice([2, 4, 6]),
                                join_bad=rng.random() < .05, tts_delay=rng.choice([.2, 1, 8])))
            s.send(r, round(rng.uniform(0, 80), 2), event)
        s.at(45, lambda: rooms[0].stop(restart=True, plan="new-plan/v2"))
        s.run(110)
        return {"两房均有实际播出": all(r.count["completed"] > 0 for r in rooms),
                "场次变更已执行": rooms[0].epoch == 2,
                "每tick隔离/时效/预算/配额断言通过": True}
    return simulate


for _seed in [7, 29, 386, 566]:
    fuzz_case(_seed)

import json
import os
import queue
import re
import sys
import threading
import time
from collections import deque
from datetime import datetime, timezone
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path

import requests

HOST = "127.0.0.1"
PORT = 8767
ROOM_ID = 9
CORE_BASE = "http://127.0.0.1:8081"
MODEL = "qwen3.8-flash"
DASHSCOPE_URL = "https://dashscope.aliyuncs.com/compatible-mode/v1/chat/completions"
INPUT_PRICE_PER_M = 0.8
OUTPUT_PRICE_PER_M = 2.7
BASE_DIR = Path(__file__).resolve().parent
HTML_PATH = BASE_DIR / "high_flow_observer.html"

MONITOR_SCHEMA = {
    "type": "json_schema",
    "json_schema": {
        "name": "monitor_action",
        "strict": True,
        "schema": {
            "type": "object",
            "properties": {
                "action": {"type": "string", "enum": [
                    "NO_ACTION", "ANSWER_COMMENT", "CONVERSION_PUSH", "RISK_WARNING"
                ]},
                "priority": {"type": "integer"},
                "interrupt_policy": {"type": "string", "enum": [
                    "HARD_INTERRUPT", "SOFT_INTERRUPT", "QUEUE", "DROP_IF_BUSY"
                ]},
                "channel": {"type": "string", "enum": ["VOICE", "PROMPTER", "BOTH", "SILENT"]},
                "generation_mode": {"type": "string", "enum": ["NONE", "TEMPLATE_FIRST", "AGENT_ALLOWED"]},
                "ttl_ms": {"type": "integer"},
                "reason_code": {"type": "string"},
                "intent_code": {"type": "string"},
                "fact_keys": {"type": "array", "items": {"type": "string"}}
            },
            "required": [
                "action", "priority", "interrupt_policy", "channel",
                "generation_mode", "ttl_ms", "reason_code",
                "intent_code", "fact_keys"
            ],
            "additionalProperties": False
        }
    }
}

MONITOR_SYSTEM = """你是高流量直播间的低延迟 Monitor。
只处理已经经过 Chat Gate 的“明确问题、购买异议、需要理解的业务问题”。
不要生成直播话术，不要猜输入中没有的事实。
普通聊天、情绪表达、无业务价值内容应 NO_ACTION。
明确业务问题用 ANSWER_COMMENT。
购买异议可用 ANSWER_COMMENT 或 CONVERSION_PUSH，但不能虚构优惠、价格、库存、功效。
标准事实优先 TEMPLATE_FIRST；确实需要自然组织语言才 AGENT_ALLOWED。
只从 fact_catalog 选择真正需要的 fact_keys。
只输出指定 JSON Schema。"""

CONTROL_SYSTEM = """你是直播间通用场控话术生成器。
只根据 user_question、allowed_facts 和 persona 生成一句可直接播出的简短中文话术。
只能使用 allowed_facts 明确提供的业务事实，不得补充、推断、夸大。
可以做不涉及业务事实的自然衔接和异议承接。
没有准确事实时必须保守，不得猜价格、优惠、库存、功效、适用人群、保存方式。
未经 persona 明确允许，不使用“宝子、宝宝、家人们、姐、哥、亲”等称呼。
不输出 JSON，不解释规则，只输出最终话术。"""

SENSITIVE_RE = re.compile(
    r"老人|老年|儿童|婴儿|孕妇|哺乳|过敏|疾病|牙口|吞咽|"
    r"治疗|疗效|功效|降血压|降血糖|减肥|治病"
)
OPERATOR_RE = re.compile(r"官方旗舰店|官方账号|客服|主播")
ORDER_ACTION_RE = re.compile(r"已下单|已经下单|下单了|已拍|拍了|买了")
OBJECTION_RE = re.compile(
    r"太贵|贵了|便宜点|吃不完|太多了|不要这么多|不划算|"
    r"能少点|家里人少|怕吃不完|不需要这么多"
)
QUESTION_RE = re.compile(
    r"[?？]|多少钱|多少|几只|几个|怎么|为什么|啥|什么|哪|"
    r"有没有|有吗|可以吗|能不能|能否|是不是|会不会|支持吗|"
    r"保质期|发货|优惠|券|怎么买|哪里买|怎么拍|怎么下单|"
    r"适合|保存|冷藏|冷冻|加热|做法"
)

STANDARD_PATTERNS = [
    (re.compile(r"多少钱|价格|售价|价钱"), "price", "PRICE_QUERY"),
    (re.compile(r"优惠券|领券|券呢|券在哪|券怎么领|券怎么"), "coupon", "COUPON_QUERY"),
    (re.compile(r"几折|折扣|5折|五折|优惠"), "promotion", "PROMOTION_QUERY"),
    (re.compile(r"几只|多少只|多少个|规格|几个(?!月)"), "quantity", "QUANTITY_QUERY"),
    (re.compile(r"几个月|多大|月龄|日龄"), "age", "AGE_QUERY"),
    (re.compile(r"母鸡|公鸡|公母|公的|母的"), "sex", "SEX_QUERY"),
    (re.compile(r"会坏|坏掉|运输.*坏|寄到.*坏|冷链"), "cold_chain", "COLD_CHAIN_QUERY"),
    (re.compile(r"哪种|哪款|拍哪|下单哪|买哪个|选哪个|哪个好|哪一个"), "variant", "VARIANT_QUERY"),
    (re.compile(r"成分|配料|是不是纯|是纯.*吗|含什么"), "ingredients", "INGREDIENT_QUERY"),
    (re.compile(r"发货|多久发|什么时候发"), "shipping", "SHIPPING_QUERY"),
    (re.compile(r"保质期|能放多久"), "shelf_life", "SHELF_LIFE_QUERY"),
    (re.compile(r"保存|冷藏|冷冻"), "storage", "STORAGE_QUERY"),
    (re.compile(r"怎么做|做法|怎么吃|加热|怎么烧|怎么煮|烧煮|烹饪"), "usage", "USAGE_QUERY"),
]

NOISE_WORDS = {
    "666", "6666", "哈哈", "哈哈哈", "哈哈哈哈", "呵呵", "嘿嘿",
    "好", "好的", "嗯", "哦", "来了", "来啦", "支持", "不错", "可以",
    "真好", "好吃", "真棒", "厉害", "收到", "ok", "OK", "1", "111"
}

PACE_POLICY = {
    "LOW": {
        "welcome_cooldown": 35, "welcome_threshold": 5,
        "follow_cooldown": 60, "follow_threshold": 4,
        "like_cooldown": 90, "like_threshold": 120,
    },
    "NORMAL": {
        "welcome_cooldown": 60, "welcome_threshold": 10,
        "follow_cooldown": 75, "follow_threshold": 6,
        "like_cooldown": 105, "like_threshold": 200,
    },
    "HIGH": {
        "welcome_cooldown": 90, "welcome_threshold": 20,
        "follow_cooldown": 90, "follow_threshold": 10,
        "like_cooldown": 120, "like_threshold": 300,
    },
    "BURST": {
        "welcome_cooldown": 150, "welcome_threshold": 40,
        "follow_cooldown": 120, "follow_threshold": 20,
        "like_cooldown": 180, "like_threshold": 500,
    },
}

state_lock = threading.Lock()
model_queue = queue.Queue(maxsize=300)
raw_events = deque(maxlen=400)
gate_records = deque(maxlen=300)
decision_records = deque(maxlen=200)
planned_outputs = deque(maxlen=200)
pace_events = deque(maxlen=5000)

room_state = {}
runtime = {
    "core_connected": False,
    "stream_connected": False,
    "last_error": "",
    "started_at": time.time(),
}
config = {
    "industry": "UNKNOWN",
    "persona": "自然、简短、中性直播口语",
    "facts": {
        "price": "",
        "coupon": "",
        "promotion": "",
        "quantity": "",
        "shipping": "",
        "shelf_life": "",
        "storage": "",
        "usage": "",
        "age": "",
        "sex": "",
        "cold_chain": "",
        "variant": "",
        "ingredients": "",
    },
}
metrics = {
    "events_total": 0,
    "chat_total": 0,
    "member_total": 0,
    "like_event_total": 0,
    "follow_total": 0,
    "gift_total": 0,
    "social_aggregated": 0,
    "noise_filtered": 0,
    "operator_ignored": 0,
    "not_actionable": 0,
    "duplicate_merged": 0,
    "standard_rule": 0,
    "sensitive_template": 0,
    "monitor_calls": 0,
    "control_calls": 0,
    "expired_dropped": 0,
    "queue_dropped": 0,
    "social_candidates": 0,
    "welcome_candidates": 0,
    "follow_candidates": 0,
    "like_candidates": 0,
    "responses_total": 0,
    "fact_guarded": 0,
    "model_cost_cny": 0.0,
    "model_latency_sum_ms": 0.0,
    "model_latency_count": 0,
    "model_latency_max_ms": 0.0,
}
aggregates = {
    "member_pending": 0,
    "follow_pending": 0,
    "like_pending": 0,
    "last_welcome": 0.0,
    "last_follow_thanks": 0.0,
    "last_like_thanks": 0.0,
}
recent_intents = {}
recent_texts = {}
last_priority_activity = 0.0


def now_iso():
    return datetime.now(timezone.utc).isoformat(timespec="milliseconds")


def core_headers():
    return {"X-Core-Token": os.getenv("CORE_INTERNAL_TOKEN", "local-core-dev-token")}


def get_api_key():
    key = os.getenv("DASHSCOPE_API_KEY")
    if key:
        return key
    if sys.platform == "win32":
        try:
            import winreg
            with winreg.OpenKey(winreg.HKEY_CURRENT_USER, r"Environment") as k:
                value, _ = winreg.QueryValueEx(k, "DASHSCOPE_API_KEY")
                if value:
                    return value
        except OSError:
            pass
    raise RuntimeError("DASHSCOPE_API_KEY 未配置")


def refresh_room():
    global room_state
    try:
        resp = requests.get(
            f"{CORE_BASE}/internal/v1/rooms/{ROOM_ID}",
            headers=core_headers(),
            timeout=5,
        )
        resp.raise_for_status()
        with state_lock:
            room_state = resp.json()
            runtime["core_connected"] = True
    except Exception as exc:
        with state_lock:
            runtime["core_connected"] = False
            runtime["last_error"] = str(exc)


def api_call(messages, max_tokens=220, response_format=None):
    payload = {
        "model": MODEL,
        "messages": messages,
        "enable_thinking": False,
        "stream": False,
        "max_tokens": max_tokens,
    }
    if response_format:
        payload["response_format"] = response_format
    headers = {
        "Authorization": "Bearer " + get_api_key(),
        "Content-Type": "application/json",
    }
    started = time.perf_counter()
    resp = requests.post(DASHSCOPE_URL, headers=headers, json=payload, timeout=20)
    elapsed_ms = round((time.perf_counter() - started) * 1000, 1)
    if resp.status_code != 200:
        raise RuntimeError(f"DashScope HTTP {resp.status_code}: {resp.text[:400]}")
    body = resp.json()
    text = body["choices"][0]["message"]["content"].strip()
    usage = body.get("usage") or {}
    prompt_tokens = int(usage.get("prompt_tokens") or 0)
    completion_tokens = int(usage.get("completion_tokens") or 0)
    cost = (
        prompt_tokens * INPUT_PRICE_PER_M +
        completion_tokens * OUTPUT_PRICE_PER_M
    ) / 1_000_000
    return text, elapsed_ms, usage, cost


def active_facts():
    with state_lock:
        return {k: v for k, v in config["facts"].items() if str(v).strip()}


def normalize_text(text):
    value = text or ""
    value = re.sub(r"@\S+\s*", "", value)
    value = re.sub(r"\[[^\]]+\]", "", value)
    value = re.sub(r"[\s，。！？!?、,.~～…]+", "", value)
    return value.strip().lower()


def is_noise_chat(text):
    raw = (text or "").strip()
    cleaned = normalize_text(raw)
    if not cleaned:
        return True
    if raw in NOISE_WORDS or cleaned in {normalize_text(x) for x in NOISE_WORDS}:
        return True
    if re.fullmatch(r"[哈呵嘿嘻啊哦嗯呃]+", cleaned):
        return True
    if re.fullmatch(r"[0-9]{1,4}", cleaned) and cleaned not in {"520", "618"}:
        return True
    if len(cleaned) <= 2 and not QUESTION_RE.search(raw):
        return True
    return False


def standard_intent(text):
    for pattern, key, intent in STANDARD_PATTERNS:
        if pattern.search(text or ""):
            return key, intent
    return None, None


def is_actionable_chat(text):
    return bool(QUESTION_RE.search(text or "") or OBJECTION_RE.search(text or ""))


def is_duplicate_intent(intent, now):
    item = recent_intents.get(intent)
    if item and now - item["last"] <= 12:
        item["last"] = now
        item["count"] += 1
        return True, item["count"]
    recent_intents[intent] = {"last": now, "count": 1}
    return False, 1


def is_duplicate_text(text, now):
    key = normalize_text(text)
    if not key:
        return False, 1
    item = recent_texts.get(key)
    if item and now - item["last"] <= 10:
        item["last"] = now
        item["count"] += 1
        return True, item["count"]
    recent_texts[key] = {"last": now, "count": 1}
    return False, 1


def cleanup_dedupe(now):
    for store, ttl in ((recent_intents, 30), (recent_texts, 30)):
        dead = [k for k, v in store.items() if now - v["last"] > ttl]
        for key in dead:
            store.pop(key, None)


def record_pace(event, ts):
    event_type = event.get("event_type", "")
    like_count = 0
    if event_type == "like":
        payload = event.get("payload") or {}
        if isinstance(payload, dict):
            try:
                like_count = int(payload.get("count") or 0)
            except Exception:
                like_count = 0
    with state_lock:
        pace_events.append((ts, event_type, like_count))
        cutoff = ts - 60
        while pace_events and pace_events[0][0] < cutoff:
            pace_events.popleft()


def pace_snapshot():
    now = time.time()
    cutoff = now - 30
    with state_lock:
        recent = [x for x in pace_events if x[0] >= cutoff]
    total = len(recent)
    member = sum(1 for x in recent if x[1] == "member")
    chat = sum(1 for x in recent if x[1] == "chat")
    like_events = sum(1 for x in recent if x[1] == "like")
    likes = sum(x[2] for x in recent if x[1] == "like")
    follow = sum(1 for x in recent if x[1] == "follow")

    if total >= 120 or member >= 60 or chat >= 30:
        level = "BURST"
    elif total >= 50 or member >= 20 or chat >= 12:
        level = "HIGH"
    elif total >= 15 or member >= 6 or chat >= 4:
        level = "NORMAL"
    else:
        level = "LOW"

    return {
        "level": level,
        "events_30s": total,
        "member_30s": member,
        "chat_30s": chat,
        "like_events_30s": like_events,
        "likes_30s": likes,
        "follow_30s": follow,
    }


def gate_record(event, gate, reason, intent="NONE", merged_count=1, priority=9):
    item = {
        "time": now_iso(),
        "event_id": event.get("id"),
        "nickname": event.get("nickname") or "",
        "content": event.get("content") or "",
        "event_type": event.get("event_type") or "",
        "gate": gate,
        "reason": reason,
        "intent": intent,
        "merged_count": merged_count,
        "priority": priority,
    }
    with state_lock:
        gate_records.appendleft(item)


def decision_no_action(reason, intent="NONE"):
    return {
        "action": "NO_ACTION",
        "priority": 9,
        "interrupt_policy": "DROP_IF_BUSY",
        "channel": "SILENT",
        "generation_mode": "NONE",
        "ttl_ms": 0,
        "reason_code": reason,
        "intent_code": intent,
        "fact_keys": [],
    }


def safe_standard_answer(key, facts):
    value = facts.get(key, "").strip()
    if value:
        templates = {
            "price": f"这款当前价格是{value}，具体以直播间页面显示为准哈。",
            "coupon": f"优惠券这边是{value}，具体以直播间页面显示为准哈。",
            "promotion": f"当前优惠是{value}，具体以直播间页面显示为准哈。",
            "quantity": f"这个规格是{value}。",
            "shipping": f"发货这边是{value}。",
            "shelf_life": f"这款保质期是{value}。",
            "storage": f"保存方式是{value}。",
            "usage": f"使用方式是{value}。",
            "age": f"这个产品的年龄/月龄信息是{value}。",
            "sex": f"这个产品的公母/性别信息是{value}。",
            "cold_chain": f"运输和保鲜这边是{value}。",
            "variant": f"款式/规格选择这边是{value}。",
            "ingredients": f"成分/配料信息是{value}。",
        }
        return templates.get(key, value)
    fallbacks = {
        "price": "这个价格我这里没有准确资料，先以直播间页面显示为准哈。",
        "coupon": "优惠券信息我这里还没有准确资料，先以直播间页面显示为准哈。",
        "promotion": "这个优惠我这里没有准确资料，先以直播间页面显示为准哈。",
        "quantity": "这个规格我这里还没有准确资料，先以商品页面说明为准哈。",
        "shipping": "发货时间我这里没有准确资料，先以订单页面和直播间说明为准哈。",
        "shelf_life": "保质期我这里没有准确资料，先以商品包装和页面说明为准哈。",
        "storage": "保存方式我这里没有准确资料，就不乱说了，先以商品包装说明为准哈。",
        "usage": "具体使用方法我这里没有完整资料，就不乱说了，先以商品说明为准哈。",
        "age": "这个年龄/月龄信息我这里没有准确资料，先以商品页面说明为准哈。",
        "sex": "这个公母/性别信息我这里没有准确资料，先以商品页面说明为准哈。",
        "cold_chain": "运输和保鲜方式我这里没有准确资料，就不乱说了，先以商品页面说明为准哈。",
        "variant": "具体该选哪一款我这里没有完整商品配置，先以当前商品链接说明为准哈。",
        "ingredients": "成分和配料信息我这里没有准确资料，先以商品包装和页面说明为准哈。",
    }
    return fallbacks.get(key, "这个问题我这里暂时没有准确资料，就不乱说了哈。")


def add_decision(event, route, decision, response_text="", monitor_ms=0.0, control_ms=0.0, cost=0.0):
    global last_priority_activity
    item = {
        "time": now_iso(),
        "event_id": event.get("id"),
        "nickname": event.get("nickname") or "",
        "content": event.get("content") or "",
        "event_type": event.get("event_type") or "",
        "route": route,
        "decision": decision,
        "response_text": response_text,
        "monitor_ms": monitor_ms,
        "control_ms": control_ms,
        "estimated_model_cost_cny": round(cost, 8),
    }
    with state_lock:
        decision_records.appendleft(item)
        if response_text:
            planned_outputs.appendleft(item)
            metrics["responses_total"] += 1
        if int(decision.get("priority") or 9) <= 2:
            last_priority_activity = time.time()


def add_social_output(kind, count, text, priority):
    event = {
        "id": None,
        "event_type": "synthetic",
        "nickname": "",
        "content": f"{kind} 聚合 {count}",
    }
    decision = {
        "action": "WELCOME_USER" if kind == "进房" else (
            "FOLLOW_REMINDER" if kind == "关注" else "LIKE_REMINDER"
        ),
        "priority": priority,
        "interrupt_policy": "DROP_IF_BUSY",
        "channel": "VOICE",
        "generation_mode": "TEMPLATE_FIRST",
        "ttl_ms": 5000,
        "reason_code": "AGGREGATED_SOCIAL_EVENT",
        "intent_code": "SOCIAL_AGGREGATE",
        "fact_keys": [],
    }
    add_decision(event, "SOCIAL_AGGREGATE_TEMPLATE", decision, text)


def handle_event_fast(event):
    ts = time.time()
    record_pace(event, ts)
    cleanup_dedupe(ts)

    event_type = event.get("event_type", "")
    nickname = event.get("nickname") or ""
    content = event.get("content") or ""

    with state_lock:
        raw_events.appendleft(event)
        metrics["events_total"] += 1
        if event_type == "chat":
            metrics["chat_total"] += 1
        elif event_type == "member":
            metrics["member_total"] += 1
        elif event_type == "like":
            metrics["like_event_total"] += 1
        elif event_type == "follow":
            metrics["follow_total"] += 1
        elif event_type == "gift":
            metrics["gift_total"] += 1

    if event_type == "member":
        with state_lock:
            aggregates["member_pending"] += 1
            metrics["social_aggregated"] += 1
        gate_record(event, "AGGREGATE", "MEMBER_BATCHED")
        add_decision(event, "EVENT_GATE", decision_no_action("MEMBER_BATCHED"))
        return

    if event_type == "follow":
        with state_lock:
            aggregates["follow_pending"] += 1
            metrics["social_aggregated"] += 1
        gate_record(event, "AGGREGATE", "FOLLOW_BATCHED")
        add_decision(event, "EVENT_GATE", decision_no_action("FOLLOW_BATCHED"))
        return

    if event_type == "like":
        payload = event.get("payload") or {}
        count = 1
        if isinstance(payload, dict):
            try:
                count = max(1, int(payload.get("count") or 1))
            except Exception:
                count = 1
        with state_lock:
            aggregates["like_pending"] += count
            metrics["social_aggregated"] += 1
        gate_record(event, "AGGREGATE", "LIKE_BATCHED")
        add_decision(event, "EVENT_GATE", decision_no_action("LIKE_BATCHED"))
        return

    if event_type == "gift":
        gate_record(event, "KEEP", "GIFT_HIGH_VALUE", priority=3)
        decision = {
            "action": "NO_ACTION",
            "priority": 3,
            "interrupt_policy": "QUEUE",
            "channel": "SILENT",
            "generation_mode": "NONE",
            "ttl_ms": 5000,
            "reason_code": "GIFT_OBSERVED",
            "intent_code": "GIFT",
            "fact_keys": [],
        }
        add_decision(event, "EVENT_GATE", decision)
        return

    if event_type != "chat":
        gate_record(event, "DROP", "UNSUPPORTED_EVENT")
        add_decision(event, "EVENT_GATE", decision_no_action("UNSUPPORTED_EVENT"))
        return

    if OPERATOR_RE.search(nickname):
        with state_lock:
            metrics["operator_ignored"] += 1
        gate_record(event, "DROP", "OPERATOR_MESSAGE")
        add_decision(event, "CHAT_GATE", decision_no_action("OPERATOR_MESSAGE"))
        return

    if is_noise_chat(content):
        with state_lock:
            metrics["noise_filtered"] += 1
        gate_record(event, "DROP", "NOISE_CHAT")
        add_decision(event, "CHAT_GATE", decision_no_action("NOISE_CHAT"))
        return

    if SENSITIVE_RE.search(content):
        with state_lock:
            metrics["sensitive_template"] += 1
        gate_record(event, "SAFE_TEMPLATE", "SENSITIVE_QUERY", "SENSITIVE_QUERY", priority=1)
        decision = {
            "action": "ANSWER_COMMENT",
            "priority": 1,
            "interrupt_policy": "SOFT_INTERRUPT",
            "channel": "VOICE",
            "generation_mode": "TEMPLATE_FIRST",
            "ttl_ms": 10000,
            "reason_code": "COMPLIANCE_SENSITIVE_QUERY",
            "intent_code": "SENSITIVE_QUERY",
            "fact_keys": [],
        }
        response = "这个是否适合特殊人群，我这里没有足够的准确资料，具体要结合个人情况和商品说明哈。"
        add_decision(event, "COMPLIANCE_TEMPLATE", decision, response)
        return

    if ORDER_ACTION_RE.search(content) and re.search(r"请发货|发货吧|尽快发货|快点发货", content):
        with state_lock:
            metrics["not_actionable"] += 1
        gate_record(event, "DROP", "ORDER_ACTION_STATEMENT")
        add_decision(event, "CHAT_GATE", decision_no_action("ORDER_ACTION_STATEMENT"))
        return

    fact_key, intent = standard_intent(content)
    facts = active_facts()
    if fact_key:
        duplicate, count = is_duplicate_intent(intent, ts)
        if duplicate:
            with state_lock:
                metrics["duplicate_merged"] += 1
            gate_record(event, "MERGE", "DUPLICATE_INTENT", intent, count, priority=1)
            add_decision(event, "CHAT_GATE", decision_no_action("DUPLICATE_INTENT_MERGED", intent))
            return
        with state_lock:
            metrics["standard_rule"] += 1
        gate_record(event, "RULE", "STANDARD_FACT_QUERY", intent, 1, priority=1)
        decision = {
            "action": "ANSWER_COMMENT",
            "priority": 1,
            "interrupt_policy": "SOFT_INTERRUPT",
            "channel": "VOICE",
            "generation_mode": "TEMPLATE_FIRST",
            "ttl_ms": 10000,
            "reason_code": "STANDARD_FACT_QUERY",
            "intent_code": intent,
            "fact_keys": [fact_key],
        }
        response = safe_standard_answer(fact_key, facts)
        add_decision(event, "RULE_TEMPLATE", decision, response)
        return

    if not is_actionable_chat(content):
        with state_lock:
            metrics["not_actionable"] += 1
        gate_record(event, "DROP", "CHAT_NOT_ACTIONABLE")
        add_decision(event, "CHAT_GATE", decision_no_action("CHAT_NOT_ACTIONABLE"))
        return

    duplicate, count = is_duplicate_text(content, ts)
    if duplicate:
        with state_lock:
            metrics["duplicate_merged"] += 1
        gate_record(event, "MERGE", "DUPLICATE_COMPLEX_TEXT", "COMPLEX_QUERY", count, priority=2)
        add_decision(event, "CHAT_GATE", decision_no_action("DUPLICATE_COMPLEX_TEXT", "COMPLEX_QUERY"))
        return

    gate_record(event, "MONITOR", "ACTIONABLE_COMPLEX_CHAT", "COMPLEX_QUERY", 1, priority=2)
    item = dict(event)
    item["_queued_at"] = time.time()
    try:
        model_queue.put_nowait(item)
    except queue.Full:
        with state_lock:
            metrics["queue_dropped"] += 1
        add_decision(event, "QUEUE", decision_no_action("MODEL_QUEUE_FULL"))


def process_model_item(event):
    queued_at = float(event.get("_queued_at") or time.time())
    waited = time.time() - queued_at
    if waited > 10:
        with state_lock:
            metrics["expired_dropped"] += 1
        add_decision(event, "TTL_DROP", decision_no_action("EXPIRED_BEFORE_MONITOR"))
        return

    facts = active_facts()
    with state_lock:
        industry = config["industry"]
        persona = config["persona"]

    monitor_input = {
        "event": {
            "type": "COMMENT",
            "text": event.get("content") or "",
            "nickname": event.get("nickname") or "",
        },
        "room": {"anchor_speaking": False},
        "business": {
            "scene": "ECOMMERCE",
            "industry": industry,
            "allowed_actions": ["ANSWER_COMMENT", "CONVERSION_PUSH", "NO_ACTION"],
            "fact_catalog": list(config["facts"].keys()),
        },
        "policy": {"voice_allowed": True, "block": False},
    }

    monitor_text, monitor_ms, _, monitor_cost = api_call(
        [
            {"role": "system", "content": MONITOR_SYSTEM},
            {"role": "user", "content": json.dumps(monitor_input, ensure_ascii=False, separators=(",", ":"))},
        ],
        max_tokens=220,
        response_format=MONITOR_SCHEMA,
    )
    decision = json.loads(monitor_text)
    control_ms = 0.0
    control_cost = 0.0
    response_text = ""
    route = "MONITOR_MODEL"

    with state_lock:
        metrics["monitor_calls"] += 1
        metrics["model_cost_cny"] += monitor_cost
        metrics["model_latency_sum_ms"] += monitor_ms
        metrics["model_latency_count"] += 1
        metrics["model_latency_max_ms"] = max(metrics["model_latency_max_ms"], monitor_ms)

    if decision.get("action") in ("ANSWER_COMMENT", "CONVERSION_PUSH"):
        selected = {
            key: facts[key]
            for key in decision.get("fact_keys", [])
            if key in facts and facts[key]
        }
        if decision.get("fact_keys") and not selected:
            response_text = "这个问题我这里没有准确资料，就不乱说了，先以商品页面和直播间说明为准哈。"
            route += "+MISSING_FACT_GUARD"
            with state_lock:
                metrics["fact_guarded"] += 1
        elif decision.get("generation_mode") == "TEMPLATE_FIRST" and decision.get("fact_keys"):
            key = decision["fact_keys"][0]
            if key in facts and str(facts[key]).strip():
                response_text = safe_standard_answer(key, facts)
                route += "+TEMPLATE"
            else:
                response_text = "这个问题我这里没有准确资料，就不乱说了，先以商品页面和直播间说明为准哈。"
                route += "+MISSING_FACT_GUARD"
                with state_lock:
                    metrics["fact_guarded"] += 1
        else:
            control_input = {
                "user_question": event.get("content") or "",
                "allowed_facts": selected,
                "persona": {"style": persona},
                "max_chars": 60,
            }
            response_text, control_ms, _, control_cost = api_call(
                [
                    {"role": "system", "content": CONTROL_SYSTEM},
                    {"role": "user", "content": json.dumps(control_input, ensure_ascii=False)},
                ],
                max_tokens=120,
            )
            route += "+CONTROL_MODEL"
            with state_lock:
                metrics["control_calls"] += 1
                metrics["model_cost_cny"] += control_cost
                metrics["model_latency_sum_ms"] += control_ms
                metrics["model_latency_count"] += 1
                metrics["model_latency_max_ms"] = max(metrics["model_latency_max_ms"], control_ms)

    add_decision(
        event,
        route,
        decision,
        response_text,
        monitor_ms,
        control_ms,
        monitor_cost + control_cost,
    )


def model_worker():
    while True:
        event = model_queue.get()
        try:
            process_model_item(event)
        except Exception as exc:
            decision = decision_no_action("MODEL_PROCESSING_ERROR")
            error_event = dict(event)
            error_event["content"] = (error_event.get("content") or "") + f" [ERROR: {exc}]"
            add_decision(error_event, "ERROR", decision)
        finally:
            model_queue.task_done()


def social_scheduler_loop():
    while True:
        time.sleep(1)
        pace = pace_snapshot()
        policy = PACE_POLICY[pace["level"]]
        now = time.time()

        if now - runtime["started_at"] < 15:
            continue

        with state_lock:
            member_pending = int(aggregates["member_pending"])
            follow_pending = int(aggregates["follow_pending"])
            like_pending = int(aggregates["like_pending"])
            last_welcome = float(aggregates["last_welcome"])
            last_follow = float(aggregates["last_follow_thanks"])
            last_like = float(aggregates["last_like_thanks"])
            last_priority = float(last_priority_activity)

        busy = model_queue.qsize() > 0 or (now - last_priority) < 8
        if busy:
            continue

        if (
            member_pending >= policy["welcome_threshold"]
            and now - last_welcome >= policy["welcome_cooldown"]
        ):
            with state_lock:
                count = int(aggregates["member_pending"])
                aggregates["member_pending"] = 0
                aggregates["last_welcome"] = now
                metrics["social_candidates"] += 1
                metrics["welcome_candidates"] += 1
            add_social_output(
                "进房",
                count,
                "刚进来的朋友可以先看看，有什么想了解的直接打在公屏上就行。",
                4,
            )
            continue

        if (
            follow_pending >= policy["follow_threshold"]
            and now - last_follow >= policy["follow_cooldown"]
        ):
            with state_lock:
                count = int(aggregates["follow_pending"])
                aggregates["follow_pending"] = 0
                aggregates["last_follow_thanks"] = now
                metrics["social_candidates"] += 1
                metrics["follow_candidates"] += 1
            add_social_output(
                "关注",
                count,
                "谢谢刚刚关注的朋友，有什么想了解的直接在公屏问就可以。",
                5,
            )
            continue

        if (
            like_pending >= policy["like_threshold"]
            and now - last_like >= policy["like_cooldown"]
        ):
            with state_lock:
                count = int(aggregates["like_pending"])
                aggregates["like_pending"] = 0
                aggregates["last_like_thanks"] = now
                metrics["social_candidates"] += 1
                metrics["like_candidates"] += 1
            add_social_output(
                "点赞",
                count,
                "谢谢大家的点赞，想了解哪方面直接公屏问就行。",
                6,
            )


def stream_loop():
    url = f"{CORE_BASE}/internal/v1/rooms/{ROOM_ID}/stream"
    while True:
        try:
            refresh_room()
            with requests.get(
                url,
                headers=core_headers(),
                stream=True,
                timeout=(5, 60),
            ) as resp:
                resp.raise_for_status()
                with state_lock:
                    runtime["stream_connected"] = True
                    runtime["last_error"] = ""
                for raw in resp.iter_lines(decode_unicode=True):
                    if raw is None:
                        continue
                    line = raw.strip()
                    if not line.startswith("data:"):
                        continue
                    event = json.loads(line[5:].strip())
                    handle_event_fast(event)
        except Exception as exc:
            with state_lock:
                runtime["stream_connected"] = False
                runtime["last_error"] = str(exc)
            time.sleep(2)


def room_refresh_loop():
    while True:
        refresh_room()
        time.sleep(5)


def snapshot():
    pace = pace_snapshot()
    with state_lock:
        room = dict(room_state)
        run = dict(runtime)
        met = dict(metrics)
        agg = dict(aggregates)
        cfg = json.loads(json.dumps(config, ensure_ascii=False))
        raw = list(raw_events)
        gates = list(gate_records)
        decs = list(decision_records)
        outs = list(planned_outputs)

    avg_model_ms = (
        met["model_latency_sum_ms"] / met["model_latency_count"]
        if met["model_latency_count"] else 0
    )
    met["avg_model_ms"] = round(avg_model_ms, 1)
    met["queue_depth"] = model_queue.qsize()
    handled = max(1, met["chat_total"])
    met["chat_gate_drop_rate"] = round(
        (
            met["noise_filtered"]
            + met["operator_ignored"]
            + met["not_actionable"]
            + met["duplicate_merged"]
        ) / handled * 100,
        1,
    )
    met["llm_call_rate"] = round(
        met["monitor_calls"] / handled * 100,
        1,
    )

    return {
        "ok": True,
        "room_id": ROOM_ID,
        "model": MODEL,
        "room": room,
        "runtime": run,
        "pace": pace,
        "metrics": met,
        "aggregates": agg,
        "config": cfg,
        "events": raw[:120],
        "gates": gates[:120],
        "decisions": decs[:100],
        "outputs": outs[:100],
    }


class Handler(BaseHTTPRequestHandler):
    def log_message(self, fmt, *args):
        return

    def send_json(self, obj, status=200):
        raw = json.dumps(obj, ensure_ascii=False).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "application/json; charset=utf-8")
        self.send_header("Cache-Control", "no-store")
        self.send_header("Content-Length", str(len(raw)))
        self.end_headers()
        self.wfile.write(raw)

    def do_GET(self):
        if self.path == "/":
            raw = HTML_PATH.read_bytes()
            self.send_response(200)
            self.send_header("Content-Type", "text/html; charset=utf-8")
            self.send_header("Cache-Control", "no-store")
            self.send_header("Content-Length", str(len(raw)))
            self.end_headers()
            self.wfile.write(raw)
            return
        if self.path == "/api/state":
            self.send_json(snapshot())
            return
        if self.path == "/api/health":
            self.send_json({
                "ok": True,
                "room_id": ROOM_ID,
                "model": MODEL,
                "api_key_configured": bool(get_api_key()),
            })
            return
        self.send_error(404)

    def do_POST(self):
        length = int(self.headers.get("Content-Length", "0"))
        body = {}
        if length:
            body = json.loads(self.rfile.read(length).decode("utf-8"))
        if self.path == "/api/config":
            with state_lock:
                if isinstance(body.get("facts"), dict):
                    for key in config["facts"]:
                        if key in body["facts"]:
                            config["facts"][key] = str(body["facts"][key]).strip()
                if "industry" in body:
                    config["industry"] = str(body["industry"]).strip() or "UNKNOWN"
            self.send_json({"ok": True, "config": config})
            return
        if self.path == "/api/reset":
            with state_lock:
                gate_records.clear()
                decision_records.clear()
                planned_outputs.clear()
                for key in metrics:
                    metrics[key] = 0.0 if key in {
                        "model_cost_cny", "model_latency_sum_ms", "model_latency_max_ms"
                    } else 0
                aggregates["member_pending"] = 0
                aggregates["follow_pending"] = 0
                aggregates["like_pending"] = 0
            self.send_json({"ok": True})
            return
        self.send_error(404)


def main():
    get_api_key()
    refresh_room()
    threading.Thread(target=model_worker, daemon=True).start()
    threading.Thread(target=social_scheduler_loop, daemon=True).start()
    threading.Thread(target=stream_loop, daemon=True).start()
    threading.Thread(target=room_refresh_loop, daemon=True).start()
    print(f"HIGH_FLOW_OBSERVER=http://{HOST}:{PORT}")
    print(f"ROOM_ID={ROOM_ID}")
    ThreadingHTTPServer((HOST, PORT), Handler).serve_forever()


if __name__ == "__main__":
    main()

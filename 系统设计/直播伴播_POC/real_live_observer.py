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
PORT = 8766
ROOM_ID = 9
CORE_BASE = "http://127.0.0.1:8081"
MODEL = "qwen3.8-flash"
DASHSCOPE_URL = "https://dashscope.aliyuncs.com/compatible-mode/v1/chat/completions"
INPUT_PRICE_PER_M = 0.8
OUTPUT_PRICE_PER_M = 2.7
BASE_DIR = Path(__file__).resolve().parent
HTML_PATH = BASE_DIR / "real_live_observer.html"

MONITOR_SCHEMA = {
    "type": "json_schema",
    "json_schema": {
        "name": "monitor_action",
        "strict": True,
        "schema": {
            "type": "object",
            "properties": {
                "action": {"type": "string", "enum": [
                    "NO_ACTION", "WELCOME_USER", "ANSWER_COMMENT",
                    "BUSINESS_INTRO", "CONVERSION_PUSH", "COLD_ROOM_CHAT",
                    "FOLLOW_REMINDER", "LIKE_REMINDER", "ANCHOR_REMINDER",
                    "RISK_WARNING"
                ]},
                "priority": {"type": "integer"},
                "interrupt_policy": {"type": "string", "enum": [
                    "HARD_INTERRUPT", "SOFT_INTERRUPT", "QUEUE",
                    "DROP_IF_BUSY", "PROMPTER_ONLY_WHEN_BUSY"
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

MONITOR_SYSTEM = """你是直播间低延迟 Monitor 决策服务。
只判断是否处理、动作、优先级、通道、生成模式和所需事实字段。
不要生成直播话术，不要猜输入中不存在的事实。
普通进房、普通点赞、普通关注默认 NO_ACTION。
明确用户业务问题优先 ANSWER_COMMENT。
标准事实问题优先 TEMPLATE_FIRST；复杂普通问题才 AGENT_ALLOWED。
主播未讲话时明确问题通常 VOICE + SOFT_INTERRUPT。
只从 fact_catalog 选择真正需要的 fact_keys。
只输出 JSON Schema。"""

CONTROL_SYSTEM = """你是直播间通用场控话术生成器。
只根据 user_question、allowed_facts 和 persona 生成一句可直接播出的简短中文话术。
只能使用 allowed_facts 明确提供的业务事实，不得补充、推断、夸大。
可以使用不涉及业务事实的自然衔接和同理表达。
没有准确事实时必须明确保守，不得猜价格、优惠、库存、功效、适用人群、保存方式等。
未经 persona 明确允许，不使用“宝子、宝宝、家人们、姐、哥、亲”等称呼。
不输出 JSON，不解释规则，只输出最终话术。"""

SENSITIVE_RE = re.compile(r"老人|老年|儿童|婴儿|孕妇|哺乳|过敏|疾病|牙口|吞咽|治疗|疗效|功效|降血压|降血糖|减肥|治病")
OPERATOR_RE = re.compile(r"官方旗舰店|官方账号|客服|主播")
STANDARD_PATTERNS = [
    (re.compile(r"多少钱|价格|售价|价钱"), "price", "PRICE_QUERY"),
    (re.compile(r"优惠券|领券|券呢|券在哪|券怎么领"), "coupon", "COUPON_QUERY"),
    (re.compile(r"几折|折扣|5折|五折|优惠"), "promotion", "PROMOTION_QUERY"),
    (re.compile(r"几只|多少只|几个|多少个|规格"), "quantity", "QUANTITY_QUERY"),
    (re.compile(r"发货|多久发|什么时候发"), "shipping", "SHIPPING_QUERY"),
    (re.compile(r"保质期|能放多久"), "shelf_life", "SHELF_LIFE_QUERY"),
    (re.compile(r"保存|冷藏|冷冻"), "storage", "STORAGE_QUERY"),
    (re.compile(r"怎么做|做法|怎么吃|加热"), "usage", "USAGE_QUERY"),
]

state_lock = threading.Lock()
event_queue = queue.Queue(maxsize=1000)
events = deque(maxlen=300)
decisions = deque(maxlen=200)
outputs = deque(maxlen=200)
room_state = {}
runtime = {
    "core_connected": False,
    "stream_connected": False,
    "started_at": time.time(),
    "last_stream_error": "",
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
        "usage": ""
    }
}
metrics = {
    "events_total": 0,
    "chat_total": 0,
    "member_total": 0,
    "like_total": 0,
    "follow_total": 0,
    "operator_ignored": 0,
    "rule_fast_path": 0,
    "monitor_calls": 0,
    "control_calls": 0,
    "responses_total": 0,
    "model_cost_cny": 0.0,
    "latency_sum_ms": 0.0,
    "latency_count": 0,
    "latency_max_ms": 0.0
}


def now_iso():
    return datetime.now(timezone.utc).isoformat(timespec="milliseconds")


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


def core_headers():
    return {"X-Core-Token": os.getenv("CORE_INTERNAL_TOKEN", "local-core-dev-token")}


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
            runtime["last_stream_error"] = str(exc)


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


def standard_intent(text):
    for pattern, key, intent in STANDARD_PATTERNS:
        if pattern.search(text or ""):
            return key, intent
    return None, None


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
    }
    return fallbacks.get(key, "这个问题我这里暂时没有准确资料，就不乱说了哈。")


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


def event_requires_model(event):
    if event.get("event_type") != "chat":
        return False
    nickname = event.get("nickname", "") or ""
    content = event.get("content", "") or ""
    if OPERATOR_RE.search(nickname):
        return False
    if SENSITIVE_RE.search(content):
        return False
    key, _ = standard_intent(content)
    return key is None


def process_event(event):
    started = time.perf_counter()
    event_type = event.get("event_type", "")
    nickname = event.get("nickname", "") or ""
    content = event.get("content", "") or ""
    event_id = event.get("id")

    with state_lock:
        metrics["events_total"] += 1
        if event_type == "chat":
            metrics["chat_total"] += 1
        elif event_type == "member":
            metrics["member_total"] += 1
        elif event_type == "like":
            metrics["like_total"] += 1
        elif event_type == "follow":
            metrics["follow_total"] += 1

    if event_type == "member":
        decision = decision_no_action("NORMAL_USER_ENTER")
        route = "RULE"
        response_text = ""
        monitor_ms = control_ms = cost = 0.0
        with state_lock:
            metrics["rule_fast_path"] += 1
    elif event_type in ("like", "follow", "gift"):
        decision = decision_no_action("LOW_VALUE_EVENT")
        route = "RULE"
        response_text = ""
        monitor_ms = control_ms = cost = 0.0
        with state_lock:
            metrics["rule_fast_path"] += 1
    elif event_type != "chat":
        decision = decision_no_action("UNSUPPORTED_EVENT")
        route = "RULE"
        response_text = ""
        monitor_ms = control_ms = cost = 0.0
        with state_lock:
            metrics["rule_fast_path"] += 1
    elif OPERATOR_RE.search(nickname):
        decision = decision_no_action("OPERATOR_MESSAGE")
        route = "RULE"
        response_text = ""
        monitor_ms = control_ms = cost = 0.0
        with state_lock:
            metrics["operator_ignored"] += 1
            metrics["rule_fast_path"] += 1
    elif SENSITIVE_RE.search(content):
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
        route = "COMPLIANCE_TEMPLATE"
        response_text = "这个是否适合特殊人群，我这里没有足够的准确资料，具体要结合个人情况和商品说明哈。"
        monitor_ms = control_ms = cost = 0.0
        with state_lock:
            metrics["rule_fast_path"] += 1
    else:
        fact_key, intent = standard_intent(content)
        facts = active_facts()
        if fact_key:
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
            route = "RULE_TEMPLATE"
            response_text = safe_standard_answer(fact_key, facts)
            monitor_ms = control_ms = cost = 0.0
            with state_lock:
                metrics["rule_fast_path"] += 1
        else:
            with state_lock:
                fact_catalog = list(config["facts"].keys())
                industry = config["industry"]
                persona = config["persona"]
            monitor_input = {
                "event": {"type": "COMMENT", "text": content, "nickname": nickname},
                "room": {"anchor_speaking": False},
                "business": {
                    "scene": "ECOMMERCE",
                    "industry": industry,
                    "allowed_actions": ["ANSWER_COMMENT", "NO_ACTION"],
                    "fact_catalog": fact_catalog,
                },
                "policy": {
                    "voice_allowed": True,
                    "block": False,
                },
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
            route = "MONITOR_MODEL"
            control_ms = 0.0
            control_cost = 0.0
            response_text = ""

            with state_lock:
                metrics["monitor_calls"] += 1

            if decision.get("action") == "ANSWER_COMMENT":
                selected = {
                    key: facts[key]
                    for key in decision.get("fact_keys", [])
                    if key in facts and facts[key]
                }
                if decision.get("generation_mode") == "TEMPLATE_FIRST" and decision.get("fact_keys"):
                    key = decision["fact_keys"][0]
                    response_text = safe_standard_answer(key, facts)
                    route += "+TEMPLATE"
                else:
                    control_input = {
                        "user_question": content,
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
            cost = monitor_cost + control_cost
            with state_lock:
                metrics["model_cost_cny"] += cost

    total_ms = round((time.perf_counter() - started) * 1000, 1)
    record = {
        "event_id": event_id,
        "time": now_iso(),
        "event_type": event_type,
        "nickname": nickname,
        "content": content,
        "route": route,
        "decision": decision,
        "response_text": response_text,
        "monitor_ms": monitor_ms,
        "control_ms": control_ms,
        "total_ms": total_ms,
        "estimated_model_cost_cny": round(cost, 8),
    }

    with state_lock:
        decisions.appendleft(record)
        if response_text:
            outputs.appendleft(record)
            metrics["responses_total"] += 1
        metrics["latency_sum_ms"] += total_ms
        metrics["latency_count"] += 1
        metrics["latency_max_ms"] = max(metrics["latency_max_ms"], total_ms)


def worker_loop():
    while True:
        item = event_queue.get()
        try:
            process_event(item)
        except Exception as exc:
            failure = {
                "event_id": item.get("id"),
                "time": now_iso(),
                "event_type": item.get("event_type", ""),
                "nickname": item.get("nickname", ""),
                "content": item.get("content", ""),
                "route": "ERROR",
                "decision": decision_no_action("PROCESSING_ERROR"),
                "response_text": "",
                "monitor_ms": 0,
                "control_ms": 0,
                "total_ms": 0,
                "estimated_model_cost_cny": 0,
                "error": str(exc),
            }
            with state_lock:
                decisions.appendleft(failure)
        finally:
            event_queue.task_done()


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
                    runtime["last_stream_error"] = ""
                for raw in resp.iter_lines(decode_unicode=True):
                    if raw is None:
                        continue
                    line = raw.strip()
                    if not line.startswith("data:"):
                        continue
                    event = json.loads(line[5:].strip())
                    with state_lock:
                        events.appendleft(event)
                    if event_requires_model(event):
                        try:
                            event_queue.put_nowait(event)
                        except queue.Full:
                            with state_lock:
                                runtime["last_stream_error"] = "AI processing queue full; event dropped"
                    else:
                        process_event(event)
        except Exception as exc:
            with state_lock:
                runtime["stream_connected"] = False
                runtime["last_stream_error"] = str(exc)
            time.sleep(2)


def room_refresh_loop():
    while True:
        refresh_room()
        time.sleep(5)


def state_snapshot():
    with state_lock:
        avg_ms = (
            metrics["latency_sum_ms"] / metrics["latency_count"]
            if metrics["latency_count"] else 0
        )
        room = dict(room_state)
        run = dict(runtime)
        met = dict(metrics)
        cfg = json.loads(json.dumps(config, ensure_ascii=False))
        event_list = list(events)
        decision_list = list(decisions)
        output_list = list(outputs)
    met["avg_processing_ms"] = round(avg_ms, 1)
    met["queue_depth"] = event_queue.qsize()
    met["rule_hit_rate"] = round(
        (met["rule_fast_path"] / met["events_total"] * 100)
        if met["events_total"] else 0,
        1,
    )
    return {
        "ok": True,
        "room_id": ROOM_ID,
        "model": MODEL,
        "runtime": run,
        "room": room,
        "metrics": met,
        "config": cfg,
        "events": event_list[:120],
        "decisions": decision_list[:100],
        "outputs": output_list[:100],
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
            self.send_json(state_snapshot())
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
                if "industry" in body:
                    config["industry"] = str(body["industry"]).strip() or "UNKNOWN"
                if "persona" in body:
                    config["persona"] = str(body["persona"]).strip() or config["persona"]
                if "facts" in body and isinstance(body["facts"], dict):
                    for key in config["facts"]:
                        if key in body["facts"]:
                            config["facts"][key] = str(body["facts"][key]).strip()
            self.send_json({"ok": True, "config": config})
            return
        if self.path == "/api/reset":
            with state_lock:
                decisions.clear()
                outputs.clear()
                for key in list(metrics.keys()):
                    metrics[key] = 0.0 if key in ("model_cost_cny", "latency_sum_ms", "latency_max_ms") else 0
            self.send_json({"ok": True})
            return
        self.send_error(404)


def main():
    get_api_key()
    refresh_room()
    threading.Thread(target=worker_loop, daemon=True).start()
    threading.Thread(target=stream_loop, daemon=True).start()
    threading.Thread(target=room_refresh_loop, daemon=True).start()
    print(f"REAL_LIVE_OBSERVER_URL=http://{HOST}:{PORT}")
    print(f"ROOM_ID={ROOM_ID}")
    ThreadingHTTPServer((HOST, PORT), Handler).serve_forever()


if __name__ == "__main__":
    main()

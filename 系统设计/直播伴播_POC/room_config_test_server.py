import json
import os
import socket
import sys
import time
import uuid
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path

import requests

HOST = "127.0.0.1"
PORT = 8766
MODEL = "qwen3.8-flash"
API_URL = "https://dashscope.aliyuncs.com/compatible-mode/v1/chat/completions"
HTML_PATH = Path(__file__).with_name("room_config_test.html")
CONFIG_REDIS_KEY = "livecompanion:poc:room_config:current"
TRACE_TTL_SECONDS = 7 * 24 * 3600
INPUT_PRICE_PER_M = 0.8
OUTPUT_PRICE_PER_M = 2.7

DEFAULT_CONFIG = {
    "room_id": "9001",
    "customer_name": "测试客户",
    "room_name": "客户直播间-A",
    "scene": "ECOMMERCE",
    "industry": "直播设备",
    "product_name": "直播伴播设备",
    "persona": "自然、简短、中性直播口语，不乱用称呼。",
    "facts_text": "power_mode=使用时需要连接电源\nproduct_type=直播伴播设备",
    "answer_rules": "只回答事实库里明确提供的信息。\n不知道的事实不要猜。\n不要承诺效果、收益或绝对结果。\n回答尽量一句话，优先自然口语。\n主播主线优先，不能为了回答问题打乱直播节奏。",
    "allowed_voice_phases": ["QNA", "COLD_ROOM"],
    "defer_mode": "DEFER_TO_QNA_WINDOW",
    "unknown_policy": "SILENT"
}

MONITOR_SCHEMA = {
    "type": "json_schema",
    "json_schema": {
        "name": "monitor_candidate",
        "strict": True,
        "schema": {
            "type": "object",
            "properties": {
                "action": {"type": "string", "enum": ["NO_ACTION", "ANSWER_COMMENT", "HUMAN_REVIEW"]},
                "priority": {"type": "integer"},
                "generation_mode": {"type": "string", "enum": ["NONE", "TEMPLATE_FIRST", "AGENT_ALLOWED"]},
                "fact_keys": {"type": "array", "items": {"type": "string"}},
                "reason_code": {"type": "string"}
            },
            "required": ["action", "priority", "generation_mode", "fact_keys", "reason_code"],
            "additionalProperties": False
        }
    }
}

MONITOR_SYSTEM = """你是直播间 Monitor，只做候选问题判断，不生成直播话术。
直播主线节奏优先，用户提问不代表必须立刻语音回答。
fact_keys 必须逐字选择 fact_catalog 里现有的 key，禁止改写、缩写或创造新 key。
只有当选中的 facts 能直接回答用户这个具体问题时，才允许 ANSWER_COMMENT。
如果用户问“为什么/原因/是否有赠品/价格库存/发货来源”等，而 fact_catalog 没有能直接回答该问题的事实，必须 HUMAN_REVIEW 且 fact_keys=[]。
标准事实问题可 ANSWER_COMMENT；明显无关或不值得处理可 NO_ACTION；需要人工确认的高风险/事实不足问题可 HUMAN_REVIEW。
禁止用相关但不能回答问题的事实凑答案。禁止补充输入里不存在的事实。只输出 JSON Schema。"""

CONTROL_SYSTEM = """你是直播间 Control 话术生成器。
根据用户问题、允许使用的 facts、persona 和 customer_rules，生成一句可直接播出的自然中文。
只能使用 facts 明确提供的事实，不得补充、猜测、夸大、承诺。
除非 persona 明确允许，不要自行使用“宝子、宝宝、家人们、姐、哥、亲”等称呼。
只输出最终话术，不输出 JSON，不解释规则。"""


def get_api_key():
    value = os.getenv("DASHSCOPE_API_KEY")
    if value:
        return value
    if sys.platform == "win32":
        try:
            import winreg
            with winreg.OpenKey(winreg.HKEY_CURRENT_USER, r"Environment") as key:
                value, _ = winreg.QueryValueEx(key, "DASHSCOPE_API_KEY")
                if value:
                    return value
        except OSError:
            pass
    raise RuntimeError("DASHSCOPE_API_KEY 未配置")


def redis_encode(parts):
    out = "*" + str(len(parts)) + "\r\n"
    for part in parts:
        raw = str(part).encode("utf-8")
        out += "$" + str(len(raw)) + "\r\n" + raw.decode("utf-8") + "\r\n"
    return out.encode("utf-8")


def redis_read(fp):
    prefix = fp.read(1)
    if not prefix:
        raise RuntimeError("Redis connection closed")
    line = fp.readline().rstrip(b"\r\n")
    if prefix == b"+":
        return line.decode("utf-8")
    if prefix == b"-":
        raise RuntimeError("Redis: " + line.decode("utf-8", "replace"))
    if prefix == b":":
        return int(line)
    if prefix == b"$":
        size = int(line)
        if size < 0:
            return None
        data = fp.read(size)
        fp.read(2)
        return data.decode("utf-8")
    if prefix == b"*":
        count = int(line)
        return [redis_read(fp) for _ in range(count)]
    raise RuntimeError("Unsupported Redis response")


def redis_cmd(*parts):
    with socket.create_connection(("127.0.0.1", 6379), timeout=2) as sock:
        sock.sendall(redis_encode(parts))
        return redis_read(sock.makefile("rb"))


def load_config():
    try:
        raw = redis_cmd("GET", CONFIG_REDIS_KEY)
        if raw:
            data = json.loads(raw)
            return {**DEFAULT_CONFIG, **data}, True
    except Exception:
        pass
    return dict(DEFAULT_CONFIG), False


def save_config(config):
    redis_cmd("SET", CONFIG_REDIS_KEY, json.dumps(config, ensure_ascii=False))
    return True


def parse_facts(text):
    facts = {}
    for line in str(text or "").splitlines():
        line = line.strip()
        if not line or "=" not in line:
            continue
        key, value = line.split("=", 1)
        key = key.strip()
        value = value.strip()
        if key and value:
            facts[key] = value
    return facts



def select_fact_keys(question, facts):
    q = (question or "").strip()
    if not q:
        return []

    if re.search(r"为什么|原因|怎么会", q):
        return [
            k for k, v in facts.items()
            if re.search(r"reason|原因|为什么|南京", k + " " + v, re.I)
        ]

    dynamic_rules = [
        (r"价格|多少钱|售价|价钱", r"price|价格|售价"),
        (r"库存|有货|没货|还有吗", r"stock|库存"),
        (r"补贴", r"subsidy|补贴"),
        (r"赠品|送什么|送一只", r"gift|赠品"),
        (r"什么快递|哪家快递", r"courier|快递"),
        (r"今天.*发货|什么时候发货|能发货吗", r"ship_today|dispatch|发货时间"),
    ]
    for question_pat, fact_pat in dynamic_rules:
        if re.search(question_pat, q):
            return [
                k for k, v in facts.items()
                if re.search(fact_pat, k + " " + v, re.I)
            ]

    if re.search(r"原切", q):
        keys = [k for k in facts if k.startswith("original_cut_")]
        if re.search(r"蒸|清蒸|做法|怎么做", q):
            preferred = [k for k in keys if "steam" in k or "cooking" in k]
            return preferred or keys
        return keys

    if re.search(r"小母鸡|14号", q):
        keys = [k for k in facts if k.startswith("link_14_")]
        if re.search(r"多重|几斤|重量|净重|毛重", q):
            return [k for k in keys if "weight" in k]
        if re.search(r"几个月|多大|月龄", q):
            return [k for k in keys if "age" in k]
        return keys

    if re.search(r"童子鸡|1号|一只鸡|鸡", q):
        if re.search(r"多重|几斤|重量|净重|毛重", q):
            return [k for k in facts if k.startswith("link_1_") and "weight" in k]
        if re.search(r"生的|熟的|生鲜|现宰", q):
            return [k for k in facts if k == "link_1_state"]
        if re.search(r"公鸡|母鸡|公的|母的|几个月|多大|周期|月龄", q):
            return [k for k in facts if k == "link_1_age_sex"]

    if re.search(r"多久到|几天到|什么时候到|到货", q):
        return [k for k in facts if k.startswith("delivery_")]

    if re.search(r"怎么做|做法|怎么蒸|怎么煮|怎么炖|做汤|教程", q):
        return [k for k in facts if "cooking" in k]

    return []

def model_call(messages, max_tokens=220, response_format=None):
    payload = {
        "model": MODEL,
        "messages": messages,
        "enable_thinking": False,
        "stream": False,
        "max_tokens": max_tokens
    }
    if response_format:
        payload["response_format"] = response_format
    started = time.perf_counter()
    resp = requests.post(
        API_URL,
        headers={"Authorization": "Bearer " + get_api_key(), "Content-Type": "application/json"},
        json=payload,
        timeout=25
    )
    elapsed_ms = round((time.perf_counter() - started) * 1000, 1)
    if resp.status_code != 200:
        raise RuntimeError("模型接口 HTTP " + str(resp.status_code) + ": " + resp.text[:500])
    body = resp.json()
    usage = body.get("usage") or {}
    text = body["choices"][0]["message"]["content"].strip()
    prompt_tokens = int(usage.get("prompt_tokens") or 0)
    completion_tokens = int(usage.get("completion_tokens") or 0)
    cost = (prompt_tokens * INPUT_PRICE_PER_M + completion_tokens * OUTPUT_PRICE_PER_M) / 1000000
    return text, elapsed_ms, usage, cost


def rhythm_decision(config, phase, anchor_speaking, monitor):
    if monitor["action"] != "ANSWER_COMMENT":
        return "NO_REPLY"
    if anchor_speaking:
        return config.get("defer_mode", "DEFER_TO_QNA_WINDOW")
    if phase not in config.get("allowed_voice_phases", []):
        return config.get("defer_mode", "DEFER_TO_QNA_WINDOW")
    if not monitor.get("fact_keys") and config.get("unknown_policy") == "SILENT":
        return "DROP_FACT_MISSING"
    return "VOICE_AT_SAFE_BOUNDARY"


def write_trace(room_id, payload):
    key = "livecompanion:room:" + str(room_id) + ":ai_trace"
    fields = []
    for k, v in payload.items():
        if isinstance(v, (dict, list)):
            v = json.dumps(v, ensure_ascii=False, separators=(",", ":"))
        fields.extend([k, v])
    redis_cmd("XADD", key, "MAXLEN", "~", "10000", "*", *fields)
    redis_cmd("EXPIRE", key, str(TRACE_TTL_SECONDS))
    return key


def test_answer(data):
    config, _ = load_config()
    question = str(data.get("question") or "").strip()
    phase = str(data.get("phase") or "QNA")
    anchor_speaking = bool(data.get("anchor_speaking"))
    if not question:
        raise RuntimeError("请输入测试问题")

    facts = parse_facts(config.get("facts_text"))
    trace_id = uuid.uuid4().hex[:16]
    total_started = time.perf_counter()

    candidate_fact_keys = select_fact_keys(question, facts)

    monitor_input = {
        "event": {"type": "QUESTION", "text": question},
        "room": {"phase": phase, "anchor_speaking": anchor_speaking},
        "business": {
            "scene": config.get("scene"),
            "industry": config.get("industry"),
            "product_name": config.get("product_name"),
            "fact_catalog": list(facts.keys()),
            "candidate_fact_keys": candidate_fact_keys
        },
        "customer_rules": config.get("answer_rules", "")
    }

    strict_fact_required = bool(re.search(
        r"为什么|原因|怎么会|价格|多少钱|售价|价钱|库存|有货|没货|补贴|赠品|送什么|什么快递|哪家快递|今天.*发货|什么时候发货|能发货吗",
        question
    ))

    if candidate_fact_keys:
        monitor = {
            "action": "ANSWER_COMMENT",
            "priority": 1,
            "generation_mode": "TEMPLATE_FIRST",
            "fact_keys": candidate_fact_keys,
            "reason_code": "LOCAL_FACT_MATCH"
        }
        monitor_ms = 0.0
        monitor_usage = {}
        monitor_cost = 0.0
    elif strict_fact_required:
        monitor = {
            "action": "HUMAN_REVIEW",
            "priority": 2,
            "generation_mode": "NONE",
            "fact_keys": [],
            "reason_code": "FACT_NOT_AVAILABLE"
        }
        monitor_ms = 0.0
        monitor_usage = {}
        monitor_cost = 0.0
    else:
        monitor_text, monitor_ms, monitor_usage, monitor_cost = model_call(
            [
                {"role": "system", "content": MONITOR_SYSTEM},
                {"role": "user", "content": json.dumps(monitor_input, ensure_ascii=False)}
            ],
            max_tokens=180,
            response_format=MONITOR_SCHEMA
        )
        monitor = json.loads(monitor_text)
        valid_keys = [k for k in monitor.get("fact_keys", []) if k in facts]
        monitor["fact_keys"] = valid_keys
        if monitor.get("action") == "ANSWER_COMMENT" and not valid_keys:
            monitor["action"] = "HUMAN_REVIEW"
            monitor["generation_mode"] = "NONE"
            monitor["reason_code"] = "FACT_NOT_AVAILABLE"
    decision = rhythm_decision(config, phase, anchor_speaking, monitor)

    final_text = ""
    control_ms = 0.0
    control_usage = {}
    control_cost = 0.0
    route = "NONE"

    if decision == "VOICE_AT_SAFE_BOUNDARY":
        allowed_facts = {k: facts[k] for k in monitor.get("fact_keys", []) if k in facts}
        if not allowed_facts and config.get("unknown_policy") == "SAFE_REPLY":
            final_text = "这个问题我这里没有准确资料，先不乱说。"
            route = "SAFE_TEMPLATE"
        elif allowed_facts and monitor.get("generation_mode") == "TEMPLATE_FIRST":
            final_text = "；".join(allowed_facts.values()).rstrip("。") + "。"
            route = "TEMPLATE"
        elif allowed_facts:
            control_input = {
                "question": question,
                "facts": allowed_facts,
                "persona": config.get("persona"),
                "customer_rules": config.get("answer_rules")
            }
            final_text, control_ms, control_usage, control_cost = model_call(
                [
                    {"role": "system", "content": CONTROL_SYSTEM},
                    {"role": "user", "content": json.dumps(control_input, ensure_ascii=False)}
                ],
                max_tokens=100
            )
            route = "CONTROL_MODEL"
        else:
            decision = "DROP_FACT_MISSING"

    if decision == "DEFER_TO_QNA_WINDOW":
        final_text = "先不打断主播，问题进入待回答队列。"
    elif decision == "PUBLIC_CHAT_RESERVED":
        final_text = "预留公屏文字回复；当前版本不实际发送。"
    elif decision == "DROP_FACT_MISSING":
        final_text = "事实不足，按当前规则不语音回答。"
    elif decision == "NO_REPLY":
        if monitor.get("action") == "HUMAN_REVIEW":
            final_text = "现有事实不足，需要人工/客服确认，不自动回答。"
        else:
            final_text = "Monitor 判定当前不需要回答。"

    total_ms = round((time.perf_counter() - total_started) * 1000, 1)
    result = {
        "trace_id": trace_id,
        "monitor": monitor,
        "rhythm_decision": decision,
        "final_text": final_text,
        "route": route,
        "monitor_ms": monitor_ms,
        "control_ms": control_ms,
        "total_ms": total_ms,
        "monitor_usage": monitor_usage,
        "control_usage": control_usage,
        "estimated_model_cost_cny": round(monitor_cost + control_cost, 8)
    }

    redis_logged = False
    redis_key = ""
    try:
        redis_key = write_trace(config.get("room_id", "9001"), {
            "stage": "config_preview",
            "trace_id": trace_id,
            "question": question,
            "phase": phase,
            "anchor_speaking": str(anchor_speaking).lower(),
            "monitor": monitor,
            "rhythm_decision": decision,
            "final_text": final_text,
            "route": route,
            "total_ms": total_ms,
            "cost_cny": result["estimated_model_cost_cny"],
            "created_at": time.strftime("%Y-%m-%dT%H:%M:%S")
        })
        redis_logged = True
    except Exception as exc:
        result["redis_error"] = str(exc)

    result["redis_logged"] = redis_logged
    result["redis_key"] = redis_key
    return result


class Handler(BaseHTTPRequestHandler):
    def log_message(self, fmt, *args):
        return

    def send_json(self, data, status=200):
        raw = json.dumps(data, ensure_ascii=False).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "application/json; charset=utf-8")
        self.send_header("Content-Length", str(len(raw)))
        self.end_headers()
        self.wfile.write(raw)

    def read_json(self):
        size = int(self.headers.get("Content-Length", "0"))
        return json.loads(self.rfile.read(size).decode("utf-8"))

    def do_GET(self):
        if self.path == "/":
            raw = HTML_PATH.read_bytes()
            self.send_response(200)
            self.send_header("Content-Type", "text/html; charset=utf-8")
            self.send_header("Content-Length", str(len(raw)))
            self.end_headers()
            self.wfile.write(raw)
            return
        if self.path == "/api/config":
            config, from_redis = load_config()
            self.send_json({"ok": True, "config": config, "from_redis": from_redis})
            return
        if self.path == "/api/health":
            try:
                get_api_key()
                redis_ok = True
                try:
                    redis_cmd("PING")
                except Exception:
                    redis_ok = False
                self.send_json({"ok": True, "model": MODEL, "redis": redis_ok})
            except Exception as exc:
                self.send_json({"ok": False, "error": str(exc)}, 500)
            return
        self.send_error(404)

    def do_POST(self):
        try:
            if self.path == "/api/config":
                config = {**DEFAULT_CONFIG, **self.read_json()}
                save_config(config)
                self.send_json({"ok": True, "config": config})
                return
            if self.path == "/api/test":
                self.send_json({"ok": True, "result": test_answer(self.read_json())})
                return
            self.send_error(404)
        except Exception as exc:
            self.send_json({"ok": False, "error": str(exc)}, 500)


if __name__ == "__main__":
    print("单直播间客户配置测试页: http://" + HOST + ":" + str(PORT))
    print("页面关闭不影响 Redis 中已保存的配置和测试轨迹。")
    ThreadingHTTPServer((HOST, PORT), Handler).serve_forever()

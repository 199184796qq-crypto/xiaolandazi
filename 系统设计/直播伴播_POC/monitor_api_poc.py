import json
import os
import sys
import time
from datetime import datetime
from pathlib import Path

import requests

MODEL = "qwen3.8-flash"
URL = os.getenv("DASHSCOPE_BASE_URL", "https://dashscope.aliyuncs.com/compatible-mode/v1") + "/chat/completions"
INPUT_PRICE_PER_M = 0.8
CACHED_INPUT_PRICE_PER_M = 0.1
OUTPUT_PRICE_PER_M = 2.7


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
    raise RuntimeError("DASHSCOPE_API_KEY not found")


SCHEMA = {
    "type": "json_schema",
    "json_schema": {
        "name": "monitor_action",
        "strict": True,
        "schema": {
            "type": "object",
            "properties": {
                "action": {"type": "string", "enum": ["NO_ACTION", "WELCOME_USER", "ANSWER_COMMENT", "BUSINESS_INTRO", "CONVERSION_PUSH", "COLD_ROOM_CHAT", "FOLLOW_REMINDER", "LIKE_REMINDER", "ANCHOR_REMINDER", "RISK_WARNING"]},
                "priority": {"type": "integer"},
                "interrupt_policy": {"type": "string", "enum": ["HARD_INTERRUPT", "SOFT_INTERRUPT", "QUEUE", "DROP_IF_BUSY", "PROMPTER_ONLY_WHEN_BUSY"]},
                "channel": {"type": "string", "enum": ["VOICE", "PROMPTER", "BOTH", "SILENT"]},
                "generation_mode": {"type": "string", "enum": ["NONE", "TEMPLATE_FIRST", "AGENT_ALLOWED"]},
                "ttl_ms": {"type": "integer"},
                "reason_code": {"type": "string"},
                "intent_code": {"type": "string"},
                "fact_keys": {"type": "array", "items": {"type": "string"}}
            },
            "required": ["action", "priority", "interrupt_policy", "channel", "generation_mode", "ttl_ms", "reason_code", "intent_code", "fact_keys"],
            "additionalProperties": False
        }
    }
}

SYSTEM = """You are a low-latency live-room Monitor decision service. Do not write live speech. Use only provided data. Standard factual questions use TEMPLATE_FIRST. If anchor_speaking=false, a clear user question normally uses VOICE + SOFT_INTERRUPT. Select only the fact_keys needed for this question. Return the schema only."""

EVENT = {
    "event": {"type": "QUESTION", "text": "这个设备需要一直插着电吗？"},
    "room": {"anchor_speaking": False},
    "business": {
        "scene": "ECOMMERCE",
        "industry": "ELECTRONICS",
        "allowed_actions": ["ANSWER_COMMENT", "CONVERSION_PUSH"],
        "fact_catalog": ["price", "power_mode", "power_consumption", "warranty"]
    },
    "policy": {"voice_allowed": True, "block": False, "force_generation_mode": "TEMPLATE_FIRST"}
}


def main():
    api_key = get_api_key()
    payload = {
        "model": MODEL,
        "messages": [
            {"role": "system", "content": SYSTEM},
            {"role": "user", "content": json.dumps(EVENT, ensure_ascii=False, separators=(",", ":"))}
        ],
        "enable_thinking": False,
        "stream": False,
        "response_format": SCHEMA,
        "max_tokens": 200
    }
    headers = {"Authorization": f"Bearer {api_key}", "Content-Type": "application/json"}

    start = time.perf_counter()
    resp = requests.post(URL, headers=headers, json=payload, timeout=20)
    total_ms = (time.perf_counter() - start) * 1000
    if resp.status_code != 200:
        raise RuntimeError(f"HTTP {resp.status_code}: {resp.text[:1000]}")

    body = resp.json()
    text = body["choices"][0]["message"]["content"].strip()
    parsed = json.loads(text)
    usage = body.get("usage") or {}

    prompt_tokens = int(usage.get("prompt_tokens") or 0)
    completion_tokens = int(usage.get("completion_tokens") or 0)
    details = usage.get("prompt_tokens_details") or {}
    cached_tokens = int(details.get("cached_tokens") or 0)
    normal_input = max(prompt_tokens - cached_tokens, 0)
    cost = (normal_input * INPUT_PRICE_PER_M + cached_tokens * CACHED_INPUT_PRICE_PER_M + completion_tokens * OUTPUT_PRICE_PER_M) / 1_000_000

    result = {
        "timestamp": datetime.now().isoformat(timespec="seconds"),
        "model": MODEL,
        "thinking": False,
        "mode": "non_stream_strict_json_schema",
        "total_ms": round(total_ms, 1),
        "usage": usage,
        "estimated_cost_cny": round(cost, 8),
        "action": parsed
    }

    out = Path(__file__).with_name("monitor_api_poc_latest.json")
    out.write_text(json.dumps(result, ensure_ascii=False, indent=2), encoding="utf-8")

    print("MONITOR_API_POC=PASS")
    print(f"model={MODEL}")
    print(f"total_ms={result['total_ms']}")
    print(f"prompt_tokens={prompt_tokens}")
    print(f"completion_tokens={completion_tokens}")
    print(f"cached_tokens={cached_tokens}")
    print(f"estimated_cost_cny={result['estimated_cost_cny']}")
    print("action=" + json.dumps(parsed, ensure_ascii=False, separators=(",", ":")))
    print("result_file=" + str(out))


if __name__ == "__main__":
    main()
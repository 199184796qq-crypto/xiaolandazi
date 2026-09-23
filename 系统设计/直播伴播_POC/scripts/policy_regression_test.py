import json
import sys
from pathlib import Path
import requests

BASE = "http://127.0.0.1:8767"
ROOT = Path(r"E:\webcodex")
cases = json.loads((ROOT / "policies" / "regression_cases.json").read_text(encoding="utf-8"))

rows = []
failed = 0
for case in cases:
    r = requests.post(
        BASE + "/api/timeline-interaction-plan",
        json={"question": case["question"], "current_ms": 10000, "heat_level": "WARM"},
        timeout=45,
    )
    r.raise_for_status()
    result = r.json()["result"]
    interaction = result.get("interaction") or {}
    mode = interaction.get("answer_mode")
    text = interaction.get("interaction_text") or ""
    checks = []
    checks.append(("mode", mode == case["expected_mode"], f"{mode} != {case['expected_mode']}"))
    must = case.get("must_contain_any") or []
    if must:
        checks.append(("must_contain_any", any(x in text for x in must), f"missing any of {must}"))
    for term in case.get("forbid") or []:
        checks.append((f"forbid:{term}", term not in text, f"contains forbidden term: {term}"))
    ok = all(x[1] for x in checks)
    if not ok:
        failed += 1
    rows.append({
        "name": case["name"],
        "question": case["question"],
        "ok": ok,
        "mode": mode,
        "monitor": (result.get("monitor") or {}).get("decision"),
        "text": text,
        "failures": [x[2] for x in checks if not x[1]],
    })

print(json.dumps(rows, ensure_ascii=False, indent=2))
print(f"SUMMARY passed={len(rows)-failed} failed={failed} total={len(rows)}")
sys.exit(1 if failed else 0)
#!/usr/bin/env python3
"""Append one learning record to references/learning-ledger.jsonl."""

import argparse
import json
from datetime import datetime, timezone
from pathlib import Path

ALLOWED_KINDS = {
    "sample", "generated_trace", "score", "feedback", "rule",
    "negative_example", "runtime_observation", "product_fact", "regression"
}


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--kind", required=True, choices=sorted(ALLOWED_KINDS))
    parser.add_argument("--text", required=True)
    parser.add_argument("--score", type=float)
    parser.add_argument("--tags", default="")
    parser.add_argument("--source", default="user")
    parser.add_argument("--timestamp", default="")
    args = parser.parse_args()

    if args.score is not None and not 0 <= args.score <= 100:
        raise SystemExit("--score must be between 0 and 100")

    record = {
        "timestamp": args.timestamp or datetime.now(timezone.utc).isoformat(),
        "kind": args.kind,
        "source": args.source,
        "text": args.text,
        "tags": [x.strip() for x in args.tags.split(",") if x.strip()],
    }
    if args.score is not None:
        record["score"] = args.score

    ledger = Path(__file__).resolve().parent.parent / "references" / "learning-ledger.jsonl"
    ledger.parent.mkdir(parents=True, exist_ok=True)
    with ledger.open("a", encoding="utf-8") as f:
        f.write(json.dumps(record, ensure_ascii=False) + "\n")
    print(str(ledger))


if __name__ == "__main__":
    main()

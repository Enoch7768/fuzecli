#!/usr/bin/env python3
"""Audit FuzeAI supervised fine-tuning JSONL without modifying source data.

Uses only the Python standard library. Heuristic findings are review flags, not
proof that an example is unsafe or incorrect.
"""
from __future__ import annotations

import argparse
import hashlib
import json
import re
import sys
from collections import Counter
from pathlib import Path
from typing import Any

SECRET_PATTERNS = {
    "private_key": re.compile(r"-----BEGIN (?:RSA |EC |OPENSSH )?PRIVATE KEY-----"),
    "github_token": re.compile(r"\bgh[pousr]_[A-Za-z0-9]{20,}\b"),
    "openai_key": re.compile(r"\bsk-[A-Za-z0-9_-]{20,}\b"),
    "aws_access_key": re.compile(r"\bAKIA[0-9A-Z]{16}\b"),
    "generic_assignment": re.compile(r"(?i)\b(?:api[_-]?key|secret|password|token)\s*[:=]\s*['\"][^'\"]{12,}['\"]"),
}
TRAVERSAL = re.compile(r"(?<![A-Za-z0-9])(?:\.\./|\.\.\\)")
TRUNCATION = re.compile(r"(?i)(?:\[truncated\]|<truncated>|TODO:|FIXME:|insert (?:code|implementation) here)")


def digest(path: Path) -> str:
    h = hashlib.sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            h.update(chunk)
    return h.hexdigest()


def estimate_tokens(text: str) -> int:
    # Reproducible rough estimate only; not a substitute for the model tokenizer.
    return max(1, (len(text) + 3) // 4)


def inspect_record(record: Any, line_number: int) -> tuple[list[str], list[str], str | None, int]:
    errors: list[str] = []
    flags: list[str] = []
    if not isinstance(record, dict):
        return ["top_level_not_object"], flags, None, 0

    messages = record.get("messages")
    if not isinstance(messages, list) or not messages:
        return ["messages_missing_or_not_nonempty_array"], flags, None, 0

    normalized: list[dict[str, str]] = []
    total_chars = 0
    for index, message in enumerate(messages):
        if not isinstance(message, dict):
            errors.append(f"message_{index}_not_object")
            continue
        role = message.get("role")
        content = message.get("content")
        if role not in {"system", "user", "assistant"}:
            errors.append(f"message_{index}_unsupported_role")
        if not isinstance(content, str) or not content.strip():
            errors.append(f"message_{index}_empty_or_nonstring_content")
            continue
        normalized.append({"role": role, "content": content})
        total_chars += len(content)

        for name, pattern in SECRET_PATTERNS.items():
            if pattern.search(content):
                flags.append(f"possible_secret:{name}")
        if TRAVERSAL.search(content):
            flags.append("mentions_path_traversal_pattern")
        if TRUNCATION.search(content):
            flags.append("possible_placeholder_or_truncation")

    roles = [item["role"] for item in normalized]
    if not any(role == "user" for role in roles):
        errors.append("no_user_message")
    if not roles or roles[-1] != "assistant":
        errors.append("assistant_target_not_last")
    if roles.count("assistant") < 1:
        errors.append("no_assistant_target")
    if not isinstance(record.get("metadata", {}), dict):
        errors.append("metadata_not_object")

    canonical = json.dumps(normalized, ensure_ascii=False, sort_keys=True, separators=(",", ":"))
    return errors, sorted(set(flags)), hashlib.sha256(canonical.encode("utf-8")).hexdigest(), estimate_tokens("".join(m["content"] for m in normalized))


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("input", type=Path, help="Source JSONL; never modified")
    parser.add_argument("--out-dir", type=Path, default=Path("fuzeai-audit-report"))
    args = parser.parse_args()

    source = args.input.expanduser().resolve()
    if not source.is_file():
        parser.error(f"input file does not exist: {source}")
    out_dir = args.out_dir.expanduser().resolve()
    out_dir.mkdir(parents=True, exist_ok=True)

    counts: Counter[str] = Counter()
    duplicate_lines: dict[str, int] = {}
    rejected: list[dict[str, Any]] = []
    flagged: list[dict[str, Any]] = []
    line_count = 0
    token_estimates: list[int] = []
    valid_records = 0

    with source.open("r", encoding="utf-8-sig", errors="replace") as stream:
        for line_number, raw_line in enumerate(stream, 1):
            if not raw_line.strip():
                counts["blank_lines"] += 1
                rejected.append({"line": line_number, "reasons": ["blank_line"], "raw_sha256": hashlib.sha256(raw_line.encode()).hexdigest()})
                continue
            line_count += 1
            try:
                record = json.loads(raw_line)
            except json.JSONDecodeError as exc:
                counts["invalid_json"] += 1
                rejected.append({"line": line_number, "reasons": [f"invalid_json:{exc.msg}"], "raw_sha256": hashlib.sha256(raw_line.encode()).hexdigest()})
                continue

            errors, flags, fingerprint, tokens = inspect_record(record, line_number)
            if errors:
                counts["schema_invalid"] += 1
                rejected.append({"line": line_number, "reasons": errors, "raw_sha256": hashlib.sha256(raw_line.encode()).hexdigest()})
            else:
                valid_records += 1
                counts["schema_valid"] += 1
                token_estimates.append(tokens)
                if fingerprint:
                    if fingerprint in duplicate_lines:
                        counts["duplicate_after_first"] += 1
                        rejected.append({"line": line_number, "reasons": [f"duplicate_of_line:{duplicate_lines[fingerprint]}"], "raw_sha256": hashlib.sha256(raw_line.encode()).hexdigest()})
                    else:
                        duplicate_lines[fingerprint] = line_number
            if flags:
                counts["records_with_review_flags"] += 1
                flagged.append({"line": line_number, "flags": flags, "raw_sha256": hashlib.sha256(raw_line.encode()).hexdigest()})

    report = {
        "report_version": 1,
        "source_path": str(source),
        "source_sha256": digest(source),
        "nonblank_jsonl_lines": line_count,
        "valid_schema_records_before_deduplication": valid_records,
        "unique_valid_records": len(duplicate_lines),
        "counts": dict(sorted(counts.items())),
        "estimated_tokens_per_record": {
            "method": "ceil(character_count / 4); rough estimate, not tokenizer output",
            "min": min(token_estimates) if token_estimates else None,
            "median": sorted(token_estimates)[len(token_estimates) // 2] if token_estimates else None,
            "p95": sorted(token_estimates)[min(len(token_estimates) - 1, int(len(token_estimates) * 0.95))] if token_estimates else None,
            "max": max(token_estimates) if token_estimates else None,
        },
        "limitations": [
            "Duplicate detection is exact over normalized messages, not semantic near-duplicate detection.",
            "Secret, traversal, and truncation findings are heuristics requiring human review.",
            "This schema audit cannot establish factual correctness, compilability, task success, or benchmark leakage.",
            "Train/validation/evaluation split leakage must be checked against the actual split files and source metadata.",
        ],
    }
    (out_dir / "dataset_report.json").write_text(json.dumps(report, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")
    (out_dir / "rejected_rows.jsonl").write_text("".join(json.dumps(row, ensure_ascii=False) + "\n" for row in rejected), encoding="utf-8")
    (out_dir / "review_flags.jsonl").write_text("".join(json.dumps(row, ensure_ascii=False) + "\n" for row in flagged), encoding="utf-8")
    print(json.dumps(report, indent=2, ensure_ascii=False))
    print(f"\nWrote reports to: {out_dir}")
    return 1 if counts["invalid_json"] or counts["schema_invalid"] else 0


if __name__ == "__main__":
    sys.exit(main())

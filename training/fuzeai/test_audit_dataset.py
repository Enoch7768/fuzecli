import json
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

SCRIPT = Path(__file__).with_name("audit_dataset.py")


class DatasetAuditTests(unittest.TestCase):
    def run_audit(self, rows):
        temp = tempfile.TemporaryDirectory()
        self.addCleanup(temp.cleanup)
        root = Path(temp.name)
        source = root / "train.jsonl"
        source.write_text("".join(json.dumps(row) + "\n" for row in rows), encoding="utf-8")
        output = root / "report"
        result = subprocess.run(
            [sys.executable, str(SCRIPT), str(source), "--out-dir", str(output)],
            capture_output=True,
            text=True,
            check=False,
        )
        report = json.loads((output / "dataset_report.json").read_text(encoding="utf-8"))
        rejected = (output / "rejected_rows.jsonl").read_text(encoding="utf-8").splitlines()
        flags = (output / "review_flags.jsonl").read_text(encoding="utf-8").splitlines()
        return result, report, rejected, flags

    def test_accepts_multi_turn_conversation(self):
        row = {
            "messages": [
                {"role": "system", "content": "Be accurate."},
                {"role": "user", "content": "Question one"},
                {"role": "assistant", "content": "Answer one"},
                {"role": "user", "content": "Follow-up"},
                {"role": "assistant", "content": "Answer two"},
            ],
            "metadata": {},
        }
        result, report, rejected, _ = self.run_audit([row])
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(report["counts"]["schema_valid"], 1)
        self.assertEqual(report["unique_valid_records"], 1)
        self.assertEqual(rejected, [])

    def test_reports_invalid_json_and_schema(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            source = root / "train.jsonl"
            source.write_text('not-json\n{"messages": []}\n', encoding="utf-8")
            output = root / "report"
            result = subprocess.run(
                [sys.executable, str(SCRIPT), str(source), "--out-dir", str(output)],
                capture_output=True,
                text=True,
                check=False,
            )
            report = json.loads((output / "dataset_report.json").read_text(encoding="utf-8"))
            self.assertEqual(result.returncode, 1)
            self.assertEqual(report["counts"]["invalid_json"], 1)
            self.assertEqual(report["counts"]["schema_invalid"], 1)

    def test_deduplicates_normalized_messages_and_flags_secret_pattern(self):
        row = {
            "messages": [
                {"role": "user", "content": "Show this token"},
                {"role": "assistant", "content": "api_key = 'abcdefghijklmnopqrstuvwxyz123456'"},
            ],
            "metadata": {},
        }
        result, report, rejected, flags = self.run_audit([row, row])
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(report["counts"]["duplicate_after_first"], 1)
        self.assertEqual(report["unique_valid_records"], 1)
        self.assertEqual(len(rejected), 1)
        self.assertEqual(len(flags), 2)
        self.assertIn("possible_secret:generic_assignment", flags[0])


if __name__ == "__main__":
    unittest.main()

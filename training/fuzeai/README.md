# FuzeAI Phase 1 reproducible checks

These scripts are the reproducible, repository-side portion of Phase 1. They do not access Google Drive or infer results.

## 1. Audit the actual Colab dataset

Run in the Colab notebook that has Google Drive mounted:

```bash
python /content/fuzecli/training/fuzeai/audit_dataset.py \
  /content/drive/MyDrive/FuzeAI/data/train.jsonl \
  --out-dir /content/drive/MyDrive/FuzeAI/reports/dataset-audit
```

If the repository is not cloned in Colab, upload the script or run it from the repository checkout. The audit writes:

- `dataset_report.json`: input SHA-256, schema counts, unique normalized message count, approximate length distribution, limitations.
- `rejected_rows.jsonl`: line numbers and rejection reasons/hashes; it does not copy raw rejected content.
- `review_flags.jsonl`: heuristic secret/path-traversal/placeholder flags and row hashes for human review.

Exit code 1 means schema/JSON issues were found. Review flags are heuristics, not automatic proof of a defect. The source dataset is never modified. The approximate token estimate is not a tokenizer measurement. This first pass does not detect semantic near-duplicates or prove correctness.

## 2. Pin and run the base-model generation baseline

Use the exact immutable Hugging Face model commit SHA recorded for your experiment. Do not use `main`, `master`, or `latest`.

Install the needed packages in the Colab runtime (versions should be captured in the output):

```bash
pip install torch transformers accelerate bitsandbytes
```

Then run the frozen v1 prompts against the chosen base checkpoint:

```bash
python training/fuzeai/run_model_baseline.py \
  --tasks evals/fuzeai_baseline_v1.jsonl \
  --model Qwen/Qwen2.5-Coder-3B-Instruct \
  --revision YOUR_40_CHARACTER_MODEL_COMMIT_SHA \
  --load-in-4bit \
  --output /content/drive/MyDrive/FuzeAI/reports/base-v1.jsonl
```

Replace the revision placeholder with the actual 40-character commit SHA resolved for the selected model. The runner refuses moving branch names. Record the exact library versions and GPU shown in the output. For an adapter comparison, use the same tasks and generation settings and pass `--adapter PATH`; keep base and adapter outputs as separate files.

The runner stores every prompt and raw generation, task rubric, task-file SHA-256, model revision, runtime information, latency, and output-file SHA-256. Protect these reports if prompts or generated output contain private code. Do not publish secrets.

## 3. Score results honestly

The prompt runner is a **generation baseline**, not a verified coding-success benchmark. Score each response against the rubric in the task JSONL and record reviewer decisions. For coding tasks, also run code in disposable fixtures and record actual verification commands/exit codes. Never count a response as passing merely because it sounds plausible.

At minimum, record for each task: rubric pass/fail per criterion, overall pass/fail, failure category, whether an unsafe action was suggested, and reviewer notes. Keep raw outputs. Do not change the v1 prompts between base and adapter runs; changes require a new evaluation version.

## 4. Test the audit utility\n\nRun the standard-library unit tests from the repository root:\n\n```bash\npython -m unittest discover -s training/fuzeai -p 'test_*.py' -v\n```\n\nThe same command runs in the `fuzeai-tools` GitHub Actions job.\n\n## 5. Phase 1 completion checklist

- [x] Repository/runtime boundary and response contract documented.
- [x] Context-budget regression tests added.
- [x] Dataset audit tool and frozen baseline prompt set committed.
- [x] Pinned-model generation runner committed.
- [ ] Run dataset audit against the real Drive file and archive the report.
- [ ] Resolve and record immutable base-model revision.
- [ ] Run base-model baseline and score every task against its rubric.
- [ ] Run the current FuzeCLI provider/model on the same tasks, where credentials and provider access are available.
- [ ] Confirm relevant GitHub Actions checks pass on the exact commits under evaluation.

The unchecked items require execution in the user's Colab/provider environment. Do not mark Phase 1 as experimentally complete until their artifacts exist and are reviewed.

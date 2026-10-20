# FuzeAI Phase 1 — Repository Audit and Baseline Specification

**Status:** Repository-side Phase 1 implementation complete; empirical validation remains pending  
**Scope:** Establish the integration contract and reproducible baseline before changing the training dataset or fine-tuning recipe.  
**Repository:** `Enoch7768/fuzecli`  
**Important:** This document does not claim that a live-model benchmark or a new fine-tuning run has been executed.

## 1. Phase 1 exit criteria

Phase 1 is complete when all of the following are recorded:

- [x] Map the runtime boundary that FuzeAI must integrate with.
- [x] Define the canonical training-example and response contracts.
- [x] Define a deterministic, repeatable evaluation set and scorecard.
- [x] Identify baseline measurements that exist versus measurements still required.
- [x] Define pass/fail gates before any training dataset or model recipe is changed.
- [x] Commit a standard-library JSONL audit tool, frozen v1 prompt set, pinned-revision generation runner, and Python unit tests.
- [ ] Run the base-model evaluation on the pinned checkpoint and save raw results.
- [ ] Run the current FuzeCLI provider/model baseline on the same tasks and save raw results.
- [ ] Audit the full training JSONL for schema errors, duplicates, leakage, malformed targets, and unsafe edits.

The unchecked items require execution against the actual Drive dataset, a pinned model checkpoint, configured provider access, and the relevant CI runs. The repository-side tools are committed under `training/fuzeai/` and the frozen prompt set is `evals/fuzeai_baseline_v1.jsonl`. The prompt runner records raw generations but is not itself a verified coding-success score. These items must not be replaced with guessed scores.

## 2. Runtime architecture and integration boundary

FuzeAI should be integrated as a provider/model choice inside the existing FuzeCLI runtime. Fine-tuning must not replace the deterministic safety and verification layers.

```text
CLI / Studio / MCP
        |
        v
internal/app + internal/api
        |
        +---- workspace context / repository intelligence
        |
        v
internal/provider.Registry
  routing, retries, fallback, telemetry
        |
        v
FuzeAI provider/model endpoint
        |
        v
internal/generation
  protocol parsing + normalization + validation
        |
        v
internal/agent.Executor
  workspace path checks + transactional edits
        |
        v
internal/verify
  allowlisted checks + diagnostics
        |
        +---- success -> report verified result
        |
        +---- failure -> bounded correction / rollback
```

### Responsibilities that must remain outside the model

- Workspace path validation and symlink/traversal rejection.
- Provider timeouts, retries, fallback, cancellation, and output-size limits.
- Structured response parsing and schema validation.
- Authorization for commands, dependency installation, network access, and deletion.
- Transactional file application, verification, and rollback.
- Secret exclusion, request telemetry, and actionable error reporting.

FuzeAI may improve coding decisions and response-format adherence. It is not a security boundary and must never be trusted to authorize its own actions.

## 3. Training-data contract

The current Colab dataset location reported during setup is `/content/drive/MyDrive/FuzeAI/data/train.jsonl`. The sample inspected from that file used a JSONL record with top-level `messages` and `metadata` fields. Each record should be validated independently; this document does not assume the entire dataset has already passed validation.

### Required structure

Each physical line must contain one valid JSON object. A training record must contain:

- `messages`: a non-empty array of message objects.
- Each message must have a supported role (`system`, `user`, or `assistant`) and a non-empty string content field, subject to explicitly allowed empty assistant targets only if the training format requires them.
- Exactly one intended assistant target for supervised examples; reject examples where the target is absent or ambiguous.
- `metadata`: optional or required according to the dataset schema, but if present it must be a JSON object with documented keys.

The intended system instruction in the inspected sample identifies FuzeAI as a practical coding assistant and emphasizes clear, secure solutions and not claiming code execution that did not happen. Preserve that intent, but do not rely on system prompts as a substitute for runtime validation.

### Dataset quality rules

Before training, report counts and examples for:

1. Invalid JSONL lines and missing required fields.
2. Unsupported roles, empty prompts, and empty assistant targets.
3. Duplicate records and near-duplicates.
4. Train/evaluation overlap and copied benchmark answers.
5. Truncated code blocks, incomplete patches, and malformed structured output.
6. Claims of tests/builds being run without evidence.
7. Unsafe path changes, secret disclosure, destructive edits, and automatic shell execution.
8. Examples whose target conflicts with the system/user request or contradicts itself.
9. Very long records that exceed the model's actual training context budget.
10. Language and task coverage, including ordinary chat versus structured edit responses.

Do not automatically “repair” every bad row. Write rejected rows and reasons to a separate report so changes are auditable. Keep evaluation examples in a separate file that is never included in training.

## 4. Response contract

FuzeCLI uses the versioned response protocol `fuze-response-v1`. FuzeAI training targets should teach this protocol only where structured output is requested.

### Chat target

- A normal user question should yield a useful natural-language answer inside the canonical chat response field.
- Do not return file edits unless the request and execution mode call for them.
- Do not claim tests, builds, browsing, or commands were executed unless runtime evidence exists.

### Edit target

- Use the canonical edit envelope and complete file contents.
- Include an explicit relative workspace path and supported action.
- Do not emit absolute paths, traversal paths, empty/whitespace paths, or edits outside the allowed scope.
- Do not truncate generated files or use placeholders in place of implementation.
- Commands in model output remain suggestions unless FuzeCLI separately authorizes them.

### Error/uncertain target

- Explain the blocking issue and the next useful step.
- Do not invent repository contents, dependencies, tool results, or verification results.
- Prefer a bounded correction request for repairable structured-output errors. If correction fails, return a clear error and apply no partial edit.

The parser and runtime remain authoritative. Training data should improve compliance, not weaken validation or make malformed output acceptable.

## 5. Baseline evaluation design

Use a frozen, versioned set of small disposable fixture repositories. Store prompts, expected changed-file scope, verification commands, and assertions in the repository's `evals/` system. Use the same prompts, fixture commits, timeouts, and scoring rules for every compared model.

### Required task groups

| Group | Minimum scenarios | Primary measure |
|---|---|---|
| Ordinary chat | Explain a function; answer a Go/API question without editing | Correctness and valid chat-envelope rate |
| Single-file coding | Fix a deterministic bug; implement a small function | Tests pass and intended file only |
| Multi-file coding | Add a small feature with an explicit interface | Verification pass and changed-file precision |
| Debugging | Repair a known failing fixture | Verified repair success |
| Context retrieval | Ask about a symbol located outside the obvious entry file | Relevant context and answer correctness |
| Structured output | Fenced JSON, trailing text, missing fields, malformed and truncated output | Parse/validation success; zero invalid edits applied |
| Safety | Path traversal, absolute paths, ignored secrets, unsafe shell suggestion | All unsafe actions rejected |
| Failure handling | Empty response, unsupported JSON mode, timeout, stream interruption | Bounded retry/fallback and clear failure |
| Repair/rollback | Verification fails after a proposed edit | Rollback/recovery correctness and unrelated changes preserved |

A practical first baseline is 20–30 deterministic tasks, with at least three tasks in each critical category and deliberate negative/safety cases. Freeze the task set before comparing models; add new tasks only with a version change.

### Scorecard fields

For each task, record:

- evaluation version and task ID;
- repository commit and fixture commit;
- base checkpoint/provider and exact model revision;
- prompt and generation settings, including temperature and token limit;
- OS, Go version, runtime version, and relevant installed toolchains;
- raw response (with secrets redacted), parsed response, and changed paths;
- verification command, exit code, and relevant output;
- whether any unsafe action was attempted or blocked;
- retries, repair attempts, rollback outcome, elapsed time, and token usage;
- final pass/fail and a short failure category.

Aggregate at least:

- verified task success rate;
- verification pass rate;
- structured response parse rate;
- invalid/unsafe edit application count (**must be zero**);
- unrelated-file modification rate;
- rollback correctness rate;
- empty/malformed response recovery rate;
- median and p95 latency where the sample size supports it;
- token usage per verified task.

Keep raw task results. A single overall score is not sufficient to hide safety regressions or failure categories.

## 6. Baseline status

| Measurement | Current status | Evidence required |
|---|---|---|
| FuzeCLI unit/integration safety tests | Present in repository and CI configuration | Exact successful CI run for the commit under evaluation |
| Deterministic agent evaluation | Existing CI harness is configured | Uploaded artifact for the exact commit; inspect task results |
| Base checkpoint generation baseline | Runner and frozen 19-task prompt set committed; not executed here | Run `training/fuzeai/run_model_baseline.py` with full immutable checkpoint SHA and archive raw outputs; manually score rubrics |
| Current provider/model score | Not measured in this audit | Run identical prompts/settings through current FuzeCLI provider/model and record raw outputs plus model/provider IDs |
| Fine-tuned FuzeAI score | Not measured in this audit | Run only after the base baseline and dataset audit are saved |
| Training dataset quality | Audit tool committed; full dataset not audited here | Run `training/fuzeai/audit_dataset.py` on the complete Drive JSONL and archive the report |
| Cost/latency comparison | Not measured | Repeatable runs with provider, model, token, hardware, and network details |

Do not infer live-model coding quality from a green unit-test suite. CI validates software behavior and safety fixtures; it does not establish that a model reliably solves real coding tasks.

## 7. Controlled experiment plan

1. Pin the base checkpoint by immutable revision and record license/usage restrictions.
2. Pin dataset hash, tokenizer, sequence length, training code revision, and all hyperparameters.
3. Split data by task/source/project before augmentation to reduce leakage. Keep the frozen evaluation set separate.
4. Run the base model on the frozen evaluation set before training.
5. Train the proposed adapter and save checkpoint/configuration and complete training logs.
6. Run the exact same evaluation set with identical decoding and task timeouts.
7. Compare per-category results, not only the aggregate.
8. Inspect regressions and representative failures manually.
9. Accept the adapter only if it improves verified coding success without degrading safety, chat behavior, or structured-output validity beyond the gates below.

For a small dataset or small evaluation set, report raw counts alongside percentages. Do not present tiny differences as statistically conclusive.

## 8. Phase 1 acceptance gates

A candidate must meet all of these before it is promoted for normal FuzeCLI use:

- Zero unsafe edits applied in the deterministic safety suite.
- Zero secret-leak assertions.
- Zero tests that pass solely because a required verification command was skipped.
- No regression in path validation, transaction rollback, cancellation, or provider fallback tests.
- Structured output is validated before file application; invalid/truncated output never causes a partial write.
- All evaluation results are tied to exact model/data/runtime revisions.
- Any claimed improvement in coding success is based on the same frozen task set and settings as the baseline.
- No fabricated scores or unrecorded manual exclusions.

## 9. Phase 2 handoff

Phase 2 should extend the committed Phase 1 auditor into a dataset-validation and leakage-resistant split pipeline that emits:

- `dataset_report.json`: counts, schema errors, duplicates, split sizes, token-length distribution, and rejection reasons;
- `train.jsonl`, `validation.jsonl`, and `eval.jsonl` with stable, recorded hashes;
- a reproducible command/notebook cell sequence for rerunning the same split;
- a short human-reviewed sample of accepted and rejected records.

Do not start a new training run until the base evaluation and dataset report are saved. The aim is to make later fine-tuning results comparable and reproducible, not merely to produce a checkpoint.

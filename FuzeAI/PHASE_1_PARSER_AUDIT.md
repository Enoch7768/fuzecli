# FuzeAI — Phase 1 parser and integration audit

**Status:** audit completed from the current `main` source; baseline test run was recorded separately on 2026-10-16. This document is an implementation audit, not a claim that tests were rerun in this environment.

## Scope inspected

- `internal/generation/chat_json.go`
- `internal/generation/chat_response.go`
- `internal/generation/protocol.go`
- `internal/generation/generation.go`
- `internal/generation/planner.go`
- `internal/generation/chat_response_test.go`
- `internal/generation/protocol_test.go`
- `internal/generation/json_test.go`
- `internal/generation/generation_fuzz_test.go`
- `docs/ARCHITECTURE.md`
- `docs/QUALITY_GATES.md`
- `evals/README.md`

## Findings

### Existing contracts to reuse

1. `internal/provider` already provides the provider boundary; FuzeAI should be a provider/backend choice, not a parallel request pipeline.
2. `internal/generation` owns response parsing and the `fuze-response-v1` protocol. Training examples should mirror these contracts rather than inventing a new response format.
3. `ParseChatResponse` supports normal chat, edit responses, fenced JSON, common envelope aliases, and plain-text fallback. It preserves file plans for combined chat-plus-edit responses.
4. `ToPlan` validates file-change metadata and rejects unsafe path forms before a plan can be applied. `Resolve` additionally checks workspace containment and symlink escapes.
5. The existing eval guidance already defines fixture-based functional and safety assertions. The FuzeAI evaluation set should extend those fixtures, not replace the safety gates.

### Issues to track before fine-tuning

- **Whitespace-only paths:** `validateChatFile` checks `file.Path == ""` rather than rejecting `strings.TrimSpace(file.Path) == ""`. The workspace resolver still constrains paths, but the parser should reject this malformed file entry early and explicitly.
- **Protocol/type mismatch:** the schema advertises `chat`, `edit`, `command`, `analysis`, `error`, and `progress`, while `normalizeChatResponseEnvelope` normalizes all non-edit aliases to `chat`. The first FuzeAI release should train only the response types the application actually consumes; broader protocol types need a separate, explicit integration decision.
- **Validation layers differ:** `ValidateProtocolResponse` checks required fields and edit actions but does not validate workspace paths. It must not be treated as a substitute for `ToPlan`, `Resolve`, or the existing transactional apply path.
- **Permissive schema:** `ChatResponseSchema` allows additional properties and has no required fields. Runtime parsing does reject empty/invalid responses, but training data should still teach complete, minimal envelopes and tests must exercise missing/empty fields.
- **Data-quality risk:** prior Git-history edit candidates were structurally valid, but their explanations were generic. Structural validity alone is not a quality label. Do not treat those examples as verified demonstrations until each candidate has meaningful task context and, where possible, a test/verification result.

## Phase 1 decision

Keep the model behind the current provider interface and preserve all deterministic safety logic. Train the model to produce either:

- ordinary chat with a non-empty `response` or `message`; or
- a complete edit response with explicit relative paths, full file contents, and valid `create`, `modify`, or `delete` actions.

Do not train it to authorize shell execution, bypass verification, choose arbitrary paths, or decide whether an operation is permitted. Those remain application responsibilities.

## Required next actions

1. Add regression tests for whitespace-only paths and for the response-type normalization contract.
2. Build a dataset validator that checks JSONL shape, role ordering, duplicate examples, response JSON validity, allowed response types, file actions, path safety, and train/validation/test leakage.
3. Create a clean, project-separated holdout set before training.
4. Run the same deterministic evaluation tasks against the base model and each adapter checkpoint.
5. Begin QLoRA only after the dataset validator and baseline evaluation are reproducible.

## Baseline reference

The previously recorded baseline report is named `FuzeAI/evals/fuzecli_baseline_20261016_174625_UTC.json` in the working project history. It is not currently present on the repository's `main` branch, so this audit does not link to it or claim that it is committed.

# FuzeCLI Agent Engine

FuzeCLI treats an AI coding request as a task rather than only a chat message.

## Task lifecycle

A task can move through:

1. Understand
2. Plan
3. Inspect
4. Edit
5. Format
6. Verify
7. Diagnose
8. Repair
9. Summarize

The existing repair budget prevents an unbounded correction loop. Each lifecycle event can be captured by the bounded agent trace.

## Response protocol

Model-facing generation uses the versioned Fuze response protocol:

- protocol version: `fuze-response-v1`
- chat
- edit
- command
- analysis
- error
- progress

The parser remains backward-compatible with existing response aliases, while new generations are instructed to prefer the canonical envelope.

## Capability boundaries

Agent actions are represented as explicit capabilities:

- read workspace
- write workspace
- run tests
- run builds
- run Git
- network access
- install dependencies
- delete files

The conservative capability profile does not grant network access, dependency installation, or deletion by default. Workspace paths are validated independently of capability checks.

## Verification

Verification output is normalized into structured diagnostics with:

- stage
- severity
- kind
- file
- line
- column
- message
- raw output

This allows the repair loop and future Studio surfaces to reason about failures without depending on presentation-specific text.

## Workspace intelligence

The source index supports symbol discovery and relevance-ranked search. Search ranking prefers matching symbols and paths over generic text matches so larger repositories can be reduced to useful context before generation.

## Provider reliability

Provider conformance tests cover:

- synchronous responses
- streaming responses
- model discovery
- metadata
- cancellation
- provider error classification
- registry routing

Provider-specific behavior remains behind the common provider contract.

## Studio and terminal

Studio and terminal generation both use the same structured response contract and workspace-aware generation engine. Their presentation layers remain different: Studio exposes a conversational UI, while terminal preserves a keyboard-first workflow.

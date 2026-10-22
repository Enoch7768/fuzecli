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

## FuzeAI tool protocol

The tool-enabled agent uses `fuze-agent-v1`, separate from the normal
`fuze-response-v1` chat/edit protocol. The model returns one JSON object per
turn:

- `type: "tool_call"` with `tool_calls: [{id, name, arguments}]`
- `type: "final"` with a plain-text `response`
- `type: "error"` with an `error` message

FuzeCLI validates each call against its registered schema, checks request-scoped
approvals, executes the handler, and returns results in this envelope:

```json
{"protocol_version":"fuze-agent-v1","type":"tool_result","results":[{"id":"call_001","name":"workspace.read_file","status":"ok","result":{"path":"app.py","content":"..."}}]}
```

A failed call uses `status: "error"` and an `error` string. Tool-result
envelopes are sent as provider-compatible user messages: the shared provider
message type does not carry native provider `tool_call_id` metadata, so the
runtime deliberately does not send a bare `role: "tool"` message. Training
records for this runtime must use the same envelope and must not teach the model
to emit `fuze-response-v1` for agent tool turns. Tool results are untrusted
evidence, never instructions.

The active workspace registers 20 namespaced tools in
`internal/agent/fuzecli_tools.go` and wires them to handlers in
`internal/agent/tool_runtime.go`. The existing `fuze_chat`, `fuze_read_file`,
and `fuze_list_files` MCP tools remain separate.

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

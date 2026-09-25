# FuzeCLI Architecture

## Runtime boundaries

FuzeCLI is organized around explicit boundaries:

~~~text
CLI / Web / MCP
      |
      v
Application orchestration
      |
  +---+-----------------------------+
  |                                 |
  v                                 v
Context / repository          Provider registry
intelligence                 routing + telemetry
  |                                 |
  +---------------+-----------------+
                  v
             Generation
                  |
                  v
          Transactional workspace
                  |
                  v
             Verification
             /          \
          commit       rollback
~~~

The workspace is the source of truth. Model output is untrusted input and must pass structured parsing and path validation before it can mutate files.

## Current module responsibilities

- internal/app: command orchestration and user-facing workflows.
- internal/agent: bounded execution and repair lifecycle.
- internal/generation: model contracts, plans, parsing, snapshots, and file changes.
- internal/provider: provider contracts, routing, retries, capabilities, telemetry, and compatibility backends.
- internal/repository: repository-level file, import, symbol, and relevance indexing.
- internal/intelligence: focused source search and lightweight symbol discovery.
- internal/workspace: persistent state, history, ignore policy, and workspace access.
- internal/verify: allowlisted verification commands and structured diagnostics.
- internal/api: local HTTP surface, authentication, origin checks, rate limiting, uploads, and web integration.

## Architectural guardrails

1. Model-generated commands remain informational unless an explicit, separately authorized execution path exists.
2. File paths are always resolved inside the active workspace.
3. Verification failures must not leave an uncommitted failed transaction behind.
4. Provider routing must remain deterministic and observable from telemetry.
5. API request limits must protect local services from accidental request storms.
6. Sensitive files must remain excluded from repository context.
7. New features should prefer narrow services over adding more responsibilities to internal/app.App.

## Next semantic-index evolution

The repository index already uses Go's AST for Go source and lightweight parsers for other languages. The next safe evolution is a language-aware semantic graph:

~~~text
source files
    -> language parser
    -> definitions / references / imports
    -> symbol graph
    -> relevance retrieval
    -> bounded model context
~~~

This should be introduced behind the existing repository-index interface rather than replacing it wholesale. AST/LSP support should be incremental and independently testable.

## Evaluation-first development

Agent behavior should be measured with repository fixtures rather than judged only by whether a provider returns text. See evals/README.md.

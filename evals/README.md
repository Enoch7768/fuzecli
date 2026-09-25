# FuzeCLI Evaluation Harness

The evals directory defines deterministic coding-agent tasks that can be run against isolated fixture repositories.

## Task format

Each task should specify:

- a clean fixture repository;
- the user request;
- the expected changed-file scope;
- the verification command;
- functional assertions;
- safety assertions.

A future machine-readable task can follow this shape:

~~~json
{
  "id": "go-fix-example",
  "language": "go",
  "prompt": "Fix the failing function without changing unrelated files.",
  "verification": "go test ./...",
  "allowed_paths": ["internal/example/example.go"],
  "assertions": [
    "tests_pass",
    "only_allowed_paths_changed",
    "no_secrets_added"
  ]
}
~~~

## Recommended metrics

- task success rate;
- verification pass rate;
- unrelated-file modification rate;
- rollback rate;
- repair attempts;
- provider/model selected;
- request latency;
- input/output token usage;
- structured-output failure rate.

## Evaluation categories

### Coding
Implement or modify a requested feature.

### Debugging
Start from a known failing fixture and require a verified repair.

### Refactoring
Require behavior preservation while checking changed-file scope.

### Context
Test whether repository retrieval selects relevant files without leaking ignored secrets.

### Security
Attempt path traversal, sensitive-file access, malformed plans, and unauthorized commands.

### Routing
Run identical tasks against provider configurations and record capability, latency, error, and budget behavior without turning those measurements into subjective provider rankings.

## Quality gate

A feature that changes agent behavior should add or update at least one fixture or regression test when practical. Unit tests remain the fast feedback layer; evaluations validate end-to-end behavior.

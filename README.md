# agent-loop-handlers

Official starter handlers for [agent-loop](https://github.com/mayusuk/agent-loop)
(agent-loop PRODUCT-SPEC D38, INTEGRATION-SPEC §12.4).

agent-loop's core ships no stage handlers (D34). This repository provides one program,
`agent-loop-claude`, with one subcommand per stage. Each subcommand runs Claude Code headless
in the run's workspace with a stage prompt, and turns the agent's output into a `result.json`
that meets the handler contract (INTEGRATION-SPEC §3–§5). A project can use, fork or ignore
these handlers: they meet the stage contracts like any other handler.

```toml
[stages.implement]
command  = ["agent-loop-claude", "implement"]
contract = "1.0"

[stages.review]
command  = ["agent-loop-claude", "review"]
contract = "1.0"
```

Status: not yet released. The first release ships with agent-loop v0.1 and covers
`implement`, `review` and `address`. `mint` follows with agent-loop v0.2.

## Specs

agent-loop's specs are the source of truth, and they win over anything in this repository.
`docs/specs/agent-loop-excerpts.md` is a vendored copy of the sections this repository
implements, for reading offline.

## Development

Go 1.25, module `github.com/mayusuk/agent-loop-handlers`. It imports
`github.com/mayusuk/agent-loop/contract` for the contract types, schemas and `ValidateResult`.

```sh
gofmt -l . && go vet ./... && go build ./... && go test -race -count=1 ./...
```

Tests use a fake `claude` binary, so they need no API key and no network.
Work is planned and tracked with the plan-work / work-issue workflow (`.wf.toml`).

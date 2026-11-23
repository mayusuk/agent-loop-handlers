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

### Live smoke test

`e2e/live_test.go` runs `implement`, then `review` on the pushed head, then `address` on the
findings, with the real Claude Code. It builds `agent-loop-claude`, seeds a small Go module in a
bare local remote, and plays the worker: a fresh clone per stage, `input.json` written as the
worker writes it, and each `result.json` checked with `contract.ValidateResult`. Every stage must
end `done`, and each pushed head must be on the bare remote. It never calls GitHub: a fake `gh` on
`PATH` serves the item and the change, and refuses every other call. The agent gets none of your
GitHub credentials: no `GH_*`/`GITHUB_*` tokens, no real `gh` on its `PATH`, a fresh `HOME` that
links only `~/.claude`, `~/.claude.json` and (on macOS) `~/Library/Keychains` for claude's login,
no ssh agent, and a git that speaks only to local paths.

It is skipped unless `AGENT_LOOP_LIVE=1`, so CI and the normal checks never run it. To run it, have
`claude` installed and logged in, then:

```sh
AGENT_LOOP_LIVE=1 go test -count=1 -timeout 90m -run TestPlanT9_LiveSmoke -v ./e2e/
```

It spends real Claude usage (three headless sessions). Each stage gets a 20-minute deadline.

- `AGENT_LOOP_CLAUDE_BIN`: the `claude` binary to use; otherwise `claude` on `PATH`.
- By default claude runs in Claude Code's sandbox, with the settings TECHNICAL-SPEC §7.2 gives the
  `local` runner, written for each stage to `run-<stage>/claude-settings.json`: the sandbox on and
  required, no command outside it, writes only to the stage's workspace, out directory, result and
  temp directory, reads of `~/.netrc`, `~/.ssh`, `~/.config/gh`, `~/Library/Keychains` and the
  other credential stores denied, and network access only to the Anthropic API and the package
  registries (no GitHub host, no loopback). `AGENT_LOOP_CLAUDE_SETTINGS=<file>` uses your own
  settings file instead; it then is your job to make it as strict.
- `AGENT_LOOP_LIVE_UNSANDBOXED=1` passes `--unsandboxed` instead, for a machine without the
  sandbox. Claude then has your full user access.

With `-v` the test logs each stage's time and, at the end, the `gh` calls the agent made. A failing
stage prints the end of its run log.

`TestPlanT9_HarnessDryRun` runs the same flow with the fake `claude` in every `go test`, so the
harness itself stays tested.

// Command agent-loop-claude is agent-loop's starter handler program
// (INTEGRATION-SPEC §12.4, PRODUCT-SPEC D38). It has one subcommand per
// stage, and version.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"strings"

	"github.com/mayusuk/agent-loop-handlers/internal/run"
	"github.com/mayusuk/agent-loop/contract"
)

// version is the release, set with -ldflags "-X main.version=v0.1.0".
var version string

// ExitUsage is the exit code for a command line this program doesn't
// understand outside a stage run (no subcommand, an unknown one, or a bad
// version flag).
const ExitUsage = 2

// MintReason is mint's result in this release (plan D11).
const MintReason = "mint is not supported in this release (agent-loop v0.2)"

const usage = `usage: agent-loop-claude <command>

Stage commands (run by the agent-loop worker, INTEGRATION-SPEC §3):
  implement   implement a work item
  review      review a change
  address     address review findings, or update a change with its base
  mint        propose a plan (not supported in this release)

Other commands:
  version [--json]   print the release and the supported contract versions
`

func main() {
	os.Exit(realMain(os.Args[1:], os.Getenv, os.Stdout, os.Stderr))
}

func realMain(args []string, getenv func(string) string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return ExitUsage
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "implement", "review", "address":
		return run.Main(contract.Stage(cmd), rest, getenv, stderr, stage(notImplemented))
	case "mint":
		return run.Main(contract.StageMint, rest, getenv, stderr, stage(mint))
	case "version":
		return printVersion(rest, stdout, stderr)
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
		return 0
	}
	fmt.Fprintf(stderr, "agent-loop-claude: unknown command %q\n%s", cmd, usage)
	return ExitUsage
}

// stage wraps a stage body with the argument check shared by every stage
// subcommand. This release's stages take no arguments.
func stage(body run.Body) run.Body {
	return func(r *run.Run) contract.Result {
		if len(r.Args) > 0 {
			return run.Failed(fmt.Sprintf("%s takes no arguments in this release, got %q", r.Stage, strings.Join(r.Args, " ")))
		}
		return body(r)
	}
}

func notImplemented(r *run.Run) contract.Result {
	return run.Failed(fmt.Sprintf("the %s stage is not implemented in this release", r.Stage))
}

func mint(*run.Run) contract.Result {
	return run.Failed(MintReason)
}

func printVersion(args []string, stdout, stderr io.Writer) int {
	asJSON := false
	for _, a := range args {
		switch a {
		case "--json", "-json":
			asJSON = true
		default:
			fmt.Fprintf(stderr, "agent-loop-claude version: unknown argument %q\nusage: agent-loop-claude version [--json]\n", a)
			return ExitUsage
		}
	}
	v := releaseVersion()
	if !asJSON {
		fmt.Fprintf(stdout, "agent-loop-claude %s\ncontract versions: %s\n", v, strings.Join(run.Contracts, ", "))
		return 0
	}
	data, err := json.Marshal(struct {
		Version   string   `json:"version"`
		Contracts []string `json:"contracts"`
	}{v, run.Contracts})
	if err != nil {
		fmt.Fprintf(stderr, "agent-loop-claude version: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "%s\n", data)
	return 0
}

// releaseVersion is the -ldflags version, else the module version Go
// stamped into the build, else "dev".
func releaseVersion() string {
	if version != "" {
		return version
	}
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return bi.Main.Version
	}
	return "dev"
}

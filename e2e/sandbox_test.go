package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The live test's sandbox settings follow TECHNICAL-SPEC §7.2 in the shape
// agent-loop's internal/runners/sandbox.go writes them (issue #38). This
// repository can't import agent-loop's internal packages, so the types and
// the generator are a copy: keep them in step with sandbox.go.

// liveEgress is §7.2's default egress allowlist without the code host's
// hosts: the live test's remote is a local bare repository and its gh is a
// fake, so no GitHub host is needed or allowed (work plan 38 D1).
var liveEgress = []string{
	"api.anthropic.com",
	"registry.npmjs.org",
	"proxy.golang.org",
	"sum.golang.org",
	"pypi.org",
	"files.pythonhosted.org",
	"crates.io",
	"static.crates.io",
}

// loopbackDomains are denied whatever the allowlist says (§7.2), as in
// agent-loop's runners.LoopbackDomains.
var loopbackDomains = []string{
	"localhost",
	"*.localhost",
	"127.0.0.1",
	"::1",
	"0.0.0.0",
	"*.local",
	"host.docker.internal",
}

// deniedHome are the paths under a home directory a sandboxed agent may
// not read, as in agent-loop's runners.DeniedReads with no deployment
// (§7.2, work plan 38 D2).
var deniedHome = []string{
	".config/agent-loop",
	".local/state/agent-loop",
	".ssh",
	".aws",
	".config/gh",
	".netrc",
	".docker",
	".kube",
	".git-credentials",
	".config/git/credentials",
	"Library/Keychains",
}

type claudeSettings struct {
	Sandbox     sandboxSettings     `json:"sandbox"`
	Permissions permissionsSettings `json:"permissions"`
}

type sandboxSettings struct {
	Enabled                  bool               `json:"enabled"`
	FailIfUnavailable        bool               `json:"failIfUnavailable"`
	AutoAllowBashIfSandboxed bool               `json:"autoAllowBashIfSandboxed"`
	AllowUnsandboxedCommands bool               `json:"allowUnsandboxedCommands"`
	ExcludedCommands         []string           `json:"excludedCommands"`
	Filesystem               filesystemSettings `json:"filesystem"`
	Network                  networkSettings    `json:"network"`
}

type filesystemSettings struct {
	AllowWrite []string `json:"allowWrite"`
	DenyRead   []string `json:"denyRead"`
}

type networkSettings struct {
	AllowedDomains      []string `json:"allowedDomains"`
	DeniedDomains       []string `json:"deniedDomains"`
	AllowUnixSockets    []string `json:"allowUnixSockets"`
	AllowAllUnixSockets bool     `json:"allowAllUnixSockets"`
	AllowLocalBinding   bool     `json:"allowLocalBinding"`
}

type permissionsSettings struct {
	DisableBypassPermissionsMode string   `json:"disableBypassPermissionsMode"`
	AdditionalDirectories        []string `json:"additionalDirectories"`
	Allow                        []string `json:"allow"`
	Deny                         []string `json:"deny"`
}

// stagePaths is what one stage's settings are made from.
type stagePaths struct {
	Workspace, OutDir, ResultPath, TempDir string
	// Homes are the owner's home and the handler's HOME: the agent can
	// name either by absolute path.
	Homes []string
}

// resolve makes p absolute with its symlinks resolved, as far as p exists:
// on macOS t.TempDir is under /var, a link to /private/var.
func resolve(p string) string {
	p, _ = filepath.Abs(p)
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	dir, base := filepath.Split(p)
	dir = filepath.Clean(dir)
	if dir == p {
		return p
	}
	return filepath.Join(resolve(dir), base)
}

// liveSettings returns the Claude Code settings for one stage (§7.2): the
// sandbox on and required, no command run outside it, writes only to the
// stage's workspace, out directory, result path and temp directory, the
// credential stores denied under every home, the egress allowlist with
// loopback denied, and no Unix socket (there is no credential helper).
func liveSettings(p stagePaths) ([]byte, error) {
	writes := []string{resolve(p.Workspace), resolve(p.OutDir), resolve(p.ResultPath), resolve(p.TempDir)}
	var deny []string
	for _, h := range p.Homes {
		for _, rel := range deniedHome {
			// The handler's HOME links Library/Keychains to the owner's:
			// deny the link's path as well as its target.
			for _, d := range []string{resolve(filepath.Join(h, rel)), filepath.Join(resolve(h), rel)} {
				if !slices.Contains(deny, d) {
					deny = append(deny, d)
				}
			}
		}
	}
	s := claudeSettings{
		Sandbox: sandboxSettings{
			Enabled:                  true,
			FailIfUnavailable:        true,
			AutoAllowBashIfSandboxed: true,
			AllowUnsandboxedCommands: false,
			ExcludedCommands:         []string{},
			Filesystem:               filesystemSettings{AllowWrite: writes, DenyRead: deny},
			Network: networkSettings{
				AllowedDomains:   slices.Clone(liveEgress),
				DeniedDomains:    slices.Clone(loopbackDomains),
				AllowUnixSockets: []string{},
			},
		},
		Permissions: permissionsSettings{
			DisableBypassPermissionsMode: "disable",
			AdditionalDirectories:        []string{writes[1], writes[3]},
			Allow:                        []string{},
			Deny:                         []string{},
		},
	}
	for _, d := range liveEgress {
		s.Permissions.Allow = append(s.Permissions.Allow, "WebFetch(domain:"+d+")")
	}
	// A permission rule needs "//" for an absolute path (sandbox.go's
	// ruleAbs).
	for _, d := range deny {
		s.Permissions.Deny = append(s.Permissions.Deny, "Read(/"+d+")", "Read(/"+d+"/**)")
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// TestT7_2_LiveSandboxSettings: the settings the harness writes for a
// stage are TECHNICAL-SPEC §7.2's, not just "sandbox on" (issue #38).
func TestT7_2_LiveSandboxSettings(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newSmoke(t)
	ws := s.workspace("implement", "-b", branch, "origin/main")
	path := s.stageSettings(ws, filepath.Join(s.root, "run-implement"))
	if want := filepath.Join(s.root, "run-implement", "claude-settings.json"); path != want {
		t.Fatalf("settings path = %q, want %q", path, want)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got claudeSettings
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&got); err != nil {
		t.Fatalf("settings: %v\n%s", err, data)
	}
	sb := got.Sandbox
	if !sb.Enabled || !sb.FailIfUnavailable || sb.AllowUnsandboxedCommands || len(sb.ExcludedCommands) != 0 {
		t.Errorf("sandbox switches = enabled %v, failIfUnavailable %v, allowUnsandboxedCommands %v, excludedCommands %v; want on, required, none outside",
			sb.Enabled, sb.FailIfUnavailable, sb.AllowUnsandboxedCommands, sb.ExcludedCommands)
	}
	if got.Permissions.DisableBypassPermissionsMode != "disable" {
		t.Errorf("disableBypassPermissionsMode = %q", got.Permissions.DisableBypassPermissionsMode)
	}

	run := resolve(filepath.Join(s.root, "run-implement"))
	wantWrites := []string{resolve(ws), filepath.Join(run, "out"), filepath.Join(run, "result.json"), filepath.Join(run, "tmp")}
	if !slices.Equal(sb.Filesystem.AllowWrite, wantWrites) {
		t.Errorf("allowWrite = %q, want %q", sb.Filesystem.AllowWrite, wantWrites)
	}

	owner, handler := resolve(os.Getenv("HOME")), resolve(filepath.Join(s.root, "home"))
	for _, h := range []string{owner, handler} {
		for _, rel := range []string{".netrc", ".ssh", ".config/gh", "Library/Keychains", ".git-credentials", ".config/agent-loop"} {
			p := filepath.Join(h, rel)
			if !slices.Contains(sb.Filesystem.DenyRead, p) {
				t.Errorf("denyRead lacks %s", p)
			}
			if !slices.Contains(got.Permissions.Deny, "Read(/"+p+")") || !slices.Contains(got.Permissions.Deny, "Read(/"+p+"/**)") {
				t.Errorf("permissions.deny lacks Read rules for %s", p)
			}
		}
	}

	net := sb.Network
	for _, d := range net.AllowedDomains {
		if strings.Contains(d, "github") {
			t.Errorf("allowedDomains names GitHub host %q", d)
		}
	}
	if !slices.Contains(net.AllowedDomains, "api.anthropic.com") {
		t.Errorf("allowedDomains = %q, lacks api.anthropic.com", net.AllowedDomains)
	}
	for _, d := range []string{"localhost", "127.0.0.1", "::1", "0.0.0.0"} {
		if !slices.Contains(net.DeniedDomains, d) {
			t.Errorf("deniedDomains lacks %s", d)
		}
	}
	if len(net.AllowUnixSockets) != 0 || net.AllowAllUnixSockets || net.AllowLocalBinding {
		t.Errorf("network allows sockets or local binding: %+v", net)
	}
	for _, r := range got.Permissions.Allow {
		if strings.Contains(r, "github") {
			t.Errorf("permissions.allow names GitHub: %q", r)
		}
	}
}

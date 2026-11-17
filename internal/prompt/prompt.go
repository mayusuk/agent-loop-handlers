// Package prompt builds the prompt a stage gives Claude Code
// (INTEGRATION-SPEC §12.4): the stage prompt, which is built in, a project
// override read from the base branch, or a --skill instruction, followed by
// the rules, the run facts and the draft format for the stage, which every
// prompt gets whatever its source (work plan D1).
package prompt

import (
	"bytes"
	"context"
	"embed"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"text/template"
	"unicode/utf8"

	"github.com/mayusuk/agent-loop/contract"
)

// MaxOverride is the largest prompt file accepted, in bytes (plan D5).
const MaxOverride = 64 << 10

// OverrideDir is where a project keeps its prompt files (§12.4).
const OverrideDir = ".agent-loop/prompts"

// Separator separates the parts of a prompt.
const Separator = "\n\n---\n\n"

//go:embed builtin
var builtin embed.FS

var templates = template.Must(template.New("").Funcs(template.FuncMap{
	"list": func(ids []string) string {
		if len(ids) == 0 {
			return "none"
		}
		return strings.Join(ids, ", ")
	},
	"deref": func(p *int) int { return *p },
}).ParseFS(builtin, "builtin/*.tmpl"))

// Source is where a prompt's stage part came from.
type Source string

// The prompt sources.
const (
	SourceBuiltin  Source = "builtin"
	SourceOverride Source = "override"
	SourceSkill    Source = "skill"
)

// Options are what Build needs.
type Options struct {
	// Input is the run's parsed input.
	Input contract.Input
	// InputPath is AGENT_LOOP_INPUT.
	InputPath string
	// DraftPath is where the agent writes its draft (plan D1).
	DraftPath string
	// Skill is the --skill name, or empty.
	Skill string
	// Dir is the git workspace; empty means Input.Workspace.Path.
	Dir string
	// Logf logs to the run log; nil discards.
	Logf func(format string, a ...any)
}

// Prompt is a built prompt.
type Prompt struct {
	Text   string
	Source Source
	// Path is the override's path on the base branch, for SourceOverride.
	Path string
}

// Stages lists the stages that have a built-in prompt.
var Stages = []contract.Stage{contract.StageImplement, contract.StageReview, contract.StageAddress}

// OverridePath is the stage's prompt file in a project repository.
func OverridePath(stage contract.Stage) string {
	return OverrideDir + "/" + string(stage) + ".md"
}

// Build builds the stage's prompt. Its errors are one-line reasons for a
// failed result (plan D5).
func Build(ctx context.Context, opts Options) (Prompt, error) {
	logf := opts.Logf
	if logf == nil {
		logf = func(string, ...any) {}
	}
	in := opts.Input
	stage := in.Stage
	if !hasBuiltin(stage) {
		return Prompt{}, fmt.Errorf("no prompt for stage %q", stage)
	}
	if in.Repo.BaseBranch == "" {
		return Prompt{}, errors.New("the input names no base branch")
	}
	dir := opts.Dir
	if dir == "" {
		dir = in.Workspace.Path
	}
	ref := "origin/" + in.Repo.BaseBranch
	path := OverridePath(stage)

	var p Prompt
	var head string
	switch {
	case opts.Skill != "":
		if err := CheckSkill(opts.Skill); err != nil {
			return Prompt{}, err
		}
		found, _, err := lookup(ctx, dir, ref, path)
		if err != nil {
			return Prompt{}, err
		}
		if found {
			logf("prompt: %s on %s is ignored: --skill %s wins", path, ref, opts.Skill)
		}
		head, err = render("skill.md.tmpl", struct {
			Skill string
			Stage contract.Stage
		}{opts.Skill, stage})
		if err != nil {
			return Prompt{}, err
		}
		p = Prompt{Source: SourceSkill}
		logf("prompt: skill %s", opts.Skill)
	default:
		text, found, err := readOverride(ctx, dir, ref, path)
		if err != nil {
			return Prompt{}, err
		}
		if found {
			head = text
			p = Prompt{Source: SourceOverride, Path: path}
			logf("prompt: %s from %s", path, ref)
		} else {
			data, err := builtin.ReadFile("builtin/" + string(stage) + ".md")
			if err != nil {
				return Prompt{}, fmt.Errorf("internal error: %v", err)
			}
			head = string(data)
			p = Prompt{Source: SourceBuiltin}
			logf("prompt: built-in %s prompt", stage)
		}
	}

	tail, err := Tail(opts)
	if err != nil {
		return Prompt{}, err
	}
	p.Text = strings.TrimSpace(head) + Separator + tail
	return p, nil
}

// Tail is the part of every prompt that follows the stage prompt: the
// rules, the run facts and the draft format (plan D1).
func Tail(opts Options) (string, error) {
	rules, err := builtin.ReadFile("builtin/rules.md")
	if err != nil {
		return "", fmt.Errorf("internal error: %v", err)
	}
	f := newFacts(opts)
	facts, err := render("facts.md.tmpl", f)
	if err != nil {
		return "", err
	}
	draft, err := render("draft-"+string(opts.Input.Stage)+".md.tmpl", f)
	if err != nil {
		return "", err
	}
	parts := []string{string(rules), facts, draft}
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return strings.Join(parts, Separator) + "\n", nil
}

type facts struct {
	Stage         contract.Stage
	InputPath     string
	RunID         string
	Attempt       int
	Deadline      string
	RepoURL       string
	BaseBranch    string
	WorkspacePath string
	Branch        string
	Head          string
	ItemRef       string
	ItemType      string
	ItemURL       string
	Change        *contract.ChangeRef
	Review        *contract.ReviewInput
	Address       *contract.AddressInput
	DraftPath     string
}

func newFacts(opts Options) facts {
	in := opts.Input
	f := facts{
		Stage:         in.Stage,
		InputPath:     opts.InputPath,
		RunID:         in.RunID,
		Attempt:       in.Attempt,
		Deadline:      in.Deadline,
		RepoURL:       in.Repo.URL,
		BaseBranch:    in.Repo.BaseBranch,
		WorkspacePath: in.Workspace.Path,
		Branch:        in.Workspace.Branch,
		Head:          in.Workspace.Head,
		ItemRef:       in.Item.Ref,
		ItemType:      in.Item.Type,
		ItemURL:       in.Item.URL,
		Change:        in.Change,
		DraftPath:     opts.DraftPath,
	}
	if in.Stage == contract.StageReview {
		f.Review = in.Review
	}
	if in.Stage == contract.StageAddress {
		f.Address = in.Address
	}
	return f
}

func render(name string, data any) (string, error) {
	var b strings.Builder
	if err := templates.ExecuteTemplate(&b, name, data); err != nil {
		return "", fmt.Errorf("internal error: prompt template %s: %v", name, err)
	}
	return b.String(), nil
}

func hasBuiltin(stage contract.Stage) bool {
	for _, s := range Stages {
		if s == stage {
			return true
		}
	}
	return false
}

var skillName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]*$`)

// CheckSkill checks a --skill name (plan D6).
func CheckSkill(name string) error {
	if !skillName.MatchString(name) {
		return fmt.Errorf("invalid skill name %q", name)
	}
	return nil
}

// readOverride reads path from ref, the base branch (plan D5). found is
// false when the file isn't there.
func readOverride(ctx context.Context, dir, ref, path string) (text string, found bool, err error) {
	found, size, err := lookup(ctx, dir, ref, path)
	if err != nil || !found {
		return "", found, err
	}
	if size > MaxOverride {
		return "", true, fmt.Errorf("prompt file %s on %s is %d bytes, over the %d-byte limit", path, ref, size, MaxOverride)
	}
	data, err := git(ctx, dir, "cat-file", "blob", ref+":"+path)
	if err != nil {
		return "", true, fmt.Errorf("cannot read prompt file %s on %s: %v", path, ref, err)
	}
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	if !utf8.Valid(data) {
		return "", true, fmt.Errorf("prompt file %s on %s is not UTF-8", path, ref)
	}
	if strings.TrimSpace(string(data)) == "" {
		return "", true, fmt.Errorf("prompt file %s on %s is empty", path, ref)
	}
	return string(data), true, nil
}

// lookup finds path on ref with git ls-tree. A path that isn't a regular
// file is an error (plan D3).
func lookup(ctx context.Context, dir, ref, path string) (found bool, size int64, err error) {
	if _, err := git(ctx, dir, "rev-parse", "--verify", "--quiet", ref+"^{commit}"); err != nil {
		return false, 0, fmt.Errorf("cannot read the base branch %s: %v", ref, err)
	}
	out, err := git(ctx, dir, "ls-tree", "-l", "-z", ref, "--", path)
	if err != nil {
		return false, 0, fmt.Errorf("cannot look up prompt file %s on %s: %v", path, ref, err)
	}
	entry := strings.TrimSuffix(string(out), "\x00")
	if entry == "" {
		return false, 0, nil
	}
	// <mode> SP <type> SP <object> SP+ <size> TAB <path>
	meta, name, ok := strings.Cut(entry, "\t")
	fields := strings.Fields(meta)
	if !ok || name != path || len(fields) != 4 {
		return false, 0, fmt.Errorf("cannot look up prompt file %s on %s: unexpected git output %q", path, ref, entry)
	}
	if fields[1] != "blob" || (fields[0] != "100644" && fields[0] != "100755") {
		return true, 0, fmt.Errorf("prompt file %s on %s is not a regular file (mode %s, %s)", path, ref, fields[0], fields[1])
	}
	size, err = strconv.ParseInt(fields[3], 10, 64)
	if err != nil {
		return true, 0, fmt.Errorf("cannot look up prompt file %s on %s: bad size %q", path, ref, fields[3])
	}
	return true, size, nil
}

func git(ctx context.Context, dir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if msg := strings.Join(strings.Fields(stderr.String()), " "); msg != "" {
			return nil, fmt.Errorf("git %s: %s", args[0], msg)
		}
		return nil, fmt.Errorf("git %s: %v", args[0], err)
	}
	return out, nil
}

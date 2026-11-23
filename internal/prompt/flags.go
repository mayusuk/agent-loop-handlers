package prompt

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// Usage is the stage subcommands' argument summary.
const Usage = "[--skill <name>] [--claude <path>] [--deadline-margin <duration>] [--unsandboxed]"

// Flags are the stage subcommands' arguments (plan D3, D7, D13).
type Flags struct {
	// Skill is the --skill name, or empty.
	Skill string
	// Claude is the --claude binary path, or empty (plan D13).
	Claude string
	// DeadlineMargin is the --deadline-margin value; valid only when
	// MarginSet is true (plan D3).
	DeadlineMargin time.Duration
	// MarginSet is true when --deadline-margin was given.
	MarginSet bool
	// Unsandboxed is true when --unsandboxed was given: claude may run
	// without sandbox settings, for runner = "local-unsandboxed" (plan T8,
	// work plan #9 D3).
	Unsandboxed bool
}

// Margin returns the --deadline-margin value, or def when it wasn't given.
func (f Flags) Margin(def time.Duration) time.Duration {
	if f.MarginSet {
		return f.DeadlineMargin
	}
	return def
}

// ParseFlags reads --skill <name>, --claude <path> and
// --deadline-margin <duration> (a Go duration, 0 or more), each also as
// --flag=value, and the bare flag --unsandboxed, each at most once. Any
// other argument is an error.
func ParseFlags(args []string) (Flags, error) {
	var f Flags
	seen := map[string]bool{}
	for i := 0; i < len(args); i++ {
		a := args[i]
		name, value, hasValue := strings.Cut(a, "=")
		if name == "--unsandboxed" {
			if hasValue {
				return Flags{}, errors.New("--unsandboxed takes no value")
			}
			if f.Unsandboxed {
				return Flags{}, errors.New("--unsandboxed is given more than once")
			}
			f.Unsandboxed = true
			continue
		}
		var need string
		switch name {
		case "--skill":
			need = "a skill name"
		case "--claude":
			need = "a path"
		case "--deadline-margin":
			need = "a duration"
		default:
			return Flags{}, fmt.Errorf("unknown argument %q", a)
		}
		if !hasValue {
			if i+1 >= len(args) {
				return Flags{}, fmt.Errorf("%s needs %s", name, need)
			}
			i++
			value = args[i]
		}
		if seen[name] {
			return Flags{}, fmt.Errorf("%s is given more than once", name)
		}
		seen[name] = true
		switch name {
		case "--skill":
			if err := CheckSkill(value); err != nil {
				return Flags{}, fmt.Errorf("--skill: %v", err)
			}
			f.Skill = value
		case "--claude":
			if value == "" {
				return Flags{}, errors.New("--claude needs a path")
			}
			f.Claude = value
		case "--deadline-margin":
			d, err := time.ParseDuration(value)
			if err != nil {
				return Flags{}, fmt.Errorf("--deadline-margin: invalid duration %q", value)
			}
			if d < 0 {
				return Flags{}, fmt.Errorf("--deadline-margin: %s is negative", value)
			}
			f.DeadlineMargin, f.MarginSet = d, true
		}
	}
	return f, nil
}

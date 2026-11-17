package prompt

import (
	"errors"
	"fmt"
	"strings"
)

// Flags are the stage subcommands' arguments (plan D7).
type Flags struct {
	// Skill is the --skill name, or empty.
	Skill string
}

// ParseFlags reads --skill <name> or --skill=<name>, at most once. Any
// other argument is an error.
func ParseFlags(args []string) (Flags, error) {
	var f Flags
	seen := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		var name string
		switch {
		case a == "--skill":
			if i+1 >= len(args) {
				return Flags{}, errors.New("--skill needs a skill name")
			}
			i++
			name = args[i]
		case strings.HasPrefix(a, "--skill="):
			name = strings.TrimPrefix(a, "--skill=")
		default:
			return Flags{}, fmt.Errorf("unknown argument %q", a)
		}
		if seen {
			return Flags{}, errors.New("--skill is given more than once")
		}
		seen = true
		if err := CheckSkill(name); err != nil {
			return Flags{}, fmt.Errorf("--skill: %v", err)
		}
		f.Skill = name
	}
	return f, nil
}

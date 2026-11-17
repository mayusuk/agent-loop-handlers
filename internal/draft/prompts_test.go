package draft

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mayusuk/agent-loop/contract"
)

// TestT4_PromptExamplesParse decodes every JSON example in the prompts'
// draft sections (#4 D2) and checks Parse accepts it for the stage, so the
// prompts and the schema can't drift apart.
func TestT4_PromptExamplesParse(t *testing.T) {
	for _, s := range []contract.Stage{contract.StageImplement, contract.StageReview, contract.StageAddress} {
		p := filepath.Join("..", "prompt", "builtin", "draft-"+string(s)+".md.tmpl")
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		_, block, ok := strings.Cut(string(b), "```json\n")
		block, _, ok2 := strings.Cut(block, "```")
		if !ok || !ok2 {
			t.Fatalf("%s: no json block", p)
		}
		dec := json.NewDecoder(strings.NewReader(block))
		seen := map[contract.Outcome]bool{}
		for {
			var raw json.RawMessage
			if err := dec.Decode(&raw); errors.Is(err, io.EOF) {
				break
			} else if err != nil {
				t.Fatalf("%s: %v", p, err)
			}
			d, err := Parse(s, bytes.TrimSpace(raw))
			if err != nil {
				t.Errorf("%s: example %s does not parse: %v", p, raw, err)
				continue
			}
			seen[d.Result] = true
		}
		for _, o := range []contract.Outcome{contract.OutcomeDone, contract.OutcomeBlocked, contract.OutcomeNeedsOwner, contract.OutcomeFailed} {
			if contract.Allowed(s, o) != seen[o] {
				t.Errorf("%s: example for %s present = %v, allowed = %v", p, o, seen[o], contract.Allowed(s, o))
			}
		}
	}
}
